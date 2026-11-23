package app

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base32"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// The seal for shared folders (docs/features/29-computer-files.md, "End-to-end sealing").
//
// The gateway relays bytes it cannot read and cannot forge. Every request is sealed by a site
// for this computer's static X25519 key, with two Diffie-Hellman results mixed in: the site's
// ephemeral key (a fresh key per request) and the site's own static key. Only the holder of
// that static key can make a request this computer opens, which is what lets the person at
// the computer approve *a site* - not whatever the server claims a request comes from.
//
//	envelope = 0x01 | ephPub(32) | sitePub(32) | AES-256-GCM(kToComputer, nonce "req\0"+0, aad "mesh-files v1 request")
//	keys     = HKDF-SHA256(ikm = X25519(eph, computer) || X25519(site, computer),
//	                       salt = ephPub || sitePub || computerPub, info = "mesh-files v1", 64 bytes)
//	           -> kToComputer (first 32) | kToSite (last 32)
//	response = counter(8, big endian) | GCM(kToSite, nonce "rsp\0"+counter, aad "mesh-files v1 response")
//	up chunk = GCM(kToComputer, nonce "up\0\0"+n, aad = requestId(16) | n(8) | last(1))
//	dn chunk = GCM(kToSite,     nonce "dn\0\0"+n, aad = requestId(16) | n(8) | last(1))
//
// The PHP side (aMount, Computer\Seal) is the mirror image; testdata/files-vector.json pins both.

const (
	filesInfo     = "mesh-files v1"
	filesAADReq   = "mesh-files v1 request"
	filesAADResp  = "mesh-files v1 response"
	filesVersion  = 1
	filesChunkMax = 4 << 20 // plaintext bytes per chunk
)

var errBadSeal = errors.New("the request is not sealed for this computer")

// filesKeyPath: the computer's static key, kept apart from the pairing so re-pairing keeps it
// (sites pinned it; a new one would look like an attack to them).
func filesKeyPath() string { return filepath.Join(configDir(), "files.key") }

// loadFilesKey reads the computer's key, creating it (readable by this user only) on first use.
func loadFilesKey(path string) (*ecdh.PrivateKey, error) {
	if b, err := os.ReadFile(path); err == nil {
		raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(b)))
		if err != nil {
			return nil, err
		}
		return ecdh.X25519().NewPrivateKey(raw)
	}
	key, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(base64.StdEncoding.EncodeToString(key.Bytes())), 0o600); err != nil {
		return nil, err
	}
	return key, os.Rename(tmp, path)
}

func b64url(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }

// fingerprint is what the person compares with the code on the site's page: the first 60
// bits of sha256 of the public key, base32, as XXXX-XXXX-XXXX.
func fingerprint(pub []byte) string {
	sum := sha256.Sum256(pub)
	s := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(sum[:])[:12]
	return s[0:4] + "-" + s[4:8] + "-" + s[8:12]
}

// sessionKeys are one request's keys, both directions, and its id (bound into every chunk).
type sessionKeys struct {
	toComputer cipher.AEAD
	toSite     cipher.AEAD
	requestID  [16]byte
	sitePub    []byte
	respCount  uint64
}

func deriveKeys(es, ss, ephPub, sitePub, computerPub []byte) (*sessionKeys, error) {
	ikm := append(append([]byte{}, es...), ss...)
	salt := append(append(append([]byte{}, ephPub...), sitePub...), computerPub...)
	okm, err := hkdf.Key(sha256.New, ikm, salt, filesInfo, 64)
	if err != nil {
		return nil, err
	}
	a, err := gcm(okm[:32])
	if err != nil {
		return nil, err
	}
	b, err := gcm(okm[32:])
	if err != nil {
		return nil, err
	}
	return &sessionKeys{toComputer: a, toSite: b, sitePub: append([]byte{}, sitePub...)}, nil
}

func gcm(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func nonce(label string, n uint64) []byte {
	out := make([]byte, 12)
	copy(out, label)
	binary.BigEndian.PutUint64(out[4:], n)
	return out
}

// openRequest opens a site's envelope: the keys for the rest of the session, and the request.
func openRequest(computer *ecdh.PrivateKey, envelope []byte) (*sessionKeys, []byte, error) {
	if len(envelope) < 1+32+32+16 || envelope[0] != filesVersion {
		return nil, nil, errBadSeal
	}
	ephPub, sitePub := envelope[1:33], envelope[33:65]
	eph, err := ecdh.X25519().NewPublicKey(ephPub)
	if err != nil {
		return nil, nil, errBadSeal
	}
	site, err := ecdh.X25519().NewPublicKey(sitePub)
	if err != nil {
		return nil, nil, errBadSeal
	}
	es, err := computer.ECDH(eph)
	if err != nil {
		return nil, nil, errBadSeal
	}
	ss, err := computer.ECDH(site)
	if err != nil {
		return nil, nil, errBadSeal
	}
	keys, err := deriveKeys(es, ss, ephPub, sitePub, computer.PublicKey().Bytes())
	if err != nil {
		return nil, nil, err
	}
	plain, err := keys.toComputer.Open(nil, nonce("req\x00", 0), envelope[65:], []byte(filesAADReq))
	if err != nil {
		return nil, nil, errBadSeal
	}
	return keys, plain, nil
}

// bindRequestID fixes the id every chunk of this session is bound to (from the opened request).
func (k *sessionKeys) bindRequestID(id string) error {
	raw, err := hex.DecodeString(id)
	if err != nil || len(raw) != 16 {
		return errors.New("bad request id")
	}
	copy(k.requestID[:], raw)
	return nil
}

func (k *sessionKeys) chunkAAD(n uint64, last bool) []byte {
	aad := make([]byte, 16+8+1)
	copy(aad, k.requestID[:])
	binary.BigEndian.PutUint64(aad[16:], n)
	if last {
		aad[24] = 1
	}
	return aad
}

// sealResponse: each answer of a session gets the next counter, so an old answer cannot be
// replayed in place of a newer one.
func (k *sessionKeys) sealResponse(plain []byte) []byte {
	n := k.respCount
	k.respCount++
	out := make([]byte, 8, 8+len(plain)+16)
	binary.BigEndian.PutUint64(out, n)
	return k.toSite.Seal(out, nonce("rsp\x00", n), plain, []byte(filesAADResp))
}

func (k *sessionKeys) openUp(n uint64, last bool, sealed []byte) ([]byte, error) {
	plain, err := k.toComputer.Open(nil, nonce("up\x00\x00", n), sealed, k.chunkAAD(n, last))
	if err != nil {
		return nil, errors.New("a chunk failed its integrity check")
	}
	return plain, nil
}

func (k *sessionKeys) sealDown(n uint64, last bool, plain []byte) []byte {
	return k.toSite.Seal(nil, nonce("dn\x00\x00", n), plain, k.chunkAAD(n, last))
}

// --- the site's half, for tests and the shared vector -------------------------------------

// siteSeal makes the envelope a site would send: it is the reference the PHP mirror is
// checked against (testdata/files-vector.json), and what the daemon's own tests play.
func siteSeal(site, eph *ecdh.PrivateKey, computerPub *ecdh.PublicKey, plain []byte) (*sessionKeys, []byte, error) {
	es, err := eph.ECDH(computerPub)
	if err != nil {
		return nil, nil, err
	}
	ss, err := site.ECDH(computerPub)
	if err != nil {
		return nil, nil, err
	}
	ephPub, sitePub := eph.PublicKey().Bytes(), site.PublicKey().Bytes()
	keys, err := deriveKeys(es, ss, ephPub, sitePub, computerPub.Bytes())
	if err != nil {
		return nil, nil, err
	}
	env := append([]byte{filesVersion}, ephPub...)
	env = append(env, sitePub...)
	return keys, keys.toComputer.Seal(env, nonce("req\x00", 0), plain, []byte(filesAADReq)), nil
}

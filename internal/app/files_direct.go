package app

import (
	"bufio"
	"crypto/hkdf"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// The direct path for shared folders (docs/features/29-computer-files.md): when the owner
// allows it, a site on the same LAN or Docker host talks to this computer without the
// gateway, in one streaming HTTP request of length-prefixed frames carrying the very same
// sealed messages. A gateway ticket keeps strangers on the LAN from even trying; the seal and
// this computer's own list of approved sites are what authorise, here as on the relay.
//
//	frame   = type(1) | flags(1) | length(4, big endian) | payload
//	request : 'H' {ticket, mode, request(base64)}, then 'U' chunks (flags&1 = last)
//	response: 'R' sealed answer (flags&1 = done), 'D' sealed chunk (flags&1 = last), 'X' refusal

const directPath = "/mesh-files/v1"

const ticketLabel = "ai.artur.work mesh-files ticket v1"

type ticketClaims struct {
	W   int    `json:"w"`
	U   int    `json:"u"`
	M   string `json:"m"`
	Exp int64  `json:"exp"`
}

// verifyTicket checks a gateway ticket with the key both sides derive from this computer's
// token (the gateway keeps its sha256, this computer the token itself).
func verifyTicket(ticket, rawToken string, workerID int, now time.Time) (*ticketClaims, error) {
	parts := strings.Split(ticket, ".")
	if len(parts) != 2 || workerID <= 0 {
		return nil, errors.New("bad ticket")
	}
	sum := sha256.Sum256([]byte(rawToken))
	key, err := hkdf.Key(sha256.New, sum[:], nil, ticketLabel, 32)
	if err != nil {
		return nil, err
	}
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(parts[0]))
	if !hmac.Equal([]byte(base64.RawURLEncoding.EncodeToString(mac.Sum(nil))), []byte(parts[1])) {
		return nil, errors.New("bad ticket")
	}
	body, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, errors.New("bad ticket")
	}
	var c ticketClaims
	if json.Unmarshal(body, &c) != nil || c.W != workerID || c.Exp < now.Unix() {
		return nil, errors.New("bad ticket")
	}
	return &c, nil
}

func tokenHashHex(raw string) string {
	s := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(s[:])
}

func writeFrame(w io.Writer, typ byte, flags byte, payload []byte) error {
	head := []byte{typ, flags, 0, 0, 0, 0}
	binary.BigEndian.PutUint32(head[2:], uint32(len(payload)))
	if _, err := w.Write(head); err != nil {
		return err
	}
	_, err := w.Write(payload)
	return err
}

func readFrame(r io.Reader, max int) (byte, byte, []byte, error) {
	head := make([]byte, 6)
	if _, err := io.ReadFull(r, head); err != nil {
		return 0, 0, nil, err
	}
	n := int(binary.BigEndian.Uint32(head[2:]))
	if n > max {
		return 0, 0, nil, errors.New("frame too large")
	}
	payload := make([]byte, n)
	if _, err := io.ReadFull(r, payload); err != nil {
		return 0, 0, nil, err
	}
	return head[0], head[1], payload, nil
}

// directServer listens on the LAN while the owner allows it.
type directServer struct {
	mu       sync.Mutex
	srv      *http.Server
	port     int
	d        *daemon
	inflight chan struct{}
}

// sync starts, stops or moves the listener to match the settings.
func (ds *directServer) sync(cfg directConfig) {
	ds.mu.Lock()
	defer ds.mu.Unlock()
	if ds.srv != nil && (!cfg.Enabled || cfg.Port != ds.port) {
		_ = ds.srv.Close()
		ds.srv = nil
	}
	if !cfg.Enabled || ds.srv != nil {
		return
	}
	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", cfg.Port))
	if err != nil {
		log.Printf("[files] direct transfers: cannot listen on %d: %v", cfg.Port, err)
		return
	}
	if ds.inflight == nil {
		ds.inflight = make(chan struct{}, maxFileSessions)
	}
	mux := http.NewServeMux()
	mux.HandleFunc(directPath, ds.handle)
	ds.srv = &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	ds.port = cfg.Port
	go func(s *http.Server) { _ = s.Serve(ln) }(ds.srv)
	log.Printf("[files] direct transfers on the local network: port %d", cfg.Port)
}

// addresses are what the computer reports for sites to try: private addresses only.
func (ds *directServer) addresses() []string {
	ds.mu.Lock()
	defer ds.mu.Unlock()
	if ds.srv == nil {
		return []string{}
	}
	out := []string{}
	ifaces, _ := net.InterfaceAddrs()
	for _, a := range ifaces {
		ipn, ok := a.(*net.IPNet)
		if !ok || ipn.IP.IsLoopback() || !ipn.IP.IsPrivate() && !ipn.IP.IsLinkLocalUnicast() {
			continue
		}
		if ip4 := ipn.IP.To4(); ip4 != nil {
			out = append(out, fmt.Sprintf("%s:%d", ip4, ds.port))
		} else if !ipn.IP.IsLinkLocalUnicast() {
			out = append(out, fmt.Sprintf("[%s]:%d", ipn.IP, ds.port))
		}
		if len(out) == 8 {
			break
		}
	}
	return out
}

func (ds *directServer) handle(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	select {
	case ds.inflight <- struct{}{}:
		defer func() { <-ds.inflight }()
	default:
		http.Error(w, "busy", http.StatusTooManyRequests)
		return
	}
	_ = http.NewResponseController(w).EnableFullDuplex()
	body := bufio.NewReaderSize(r.Body, 64<<10)
	typ, _, payload, err := readFrame(body, 1<<20)
	if err != nil || typ != 'H' {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	var head struct {
		Ticket  string `json:"ticket"`
		Mode    string `json:"mode"`
		Request string `json:"request"`
	}
	if json.Unmarshal(payload, &head) != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	var claims *ticketClaims
	connID := ""
	for _, c := range ds.d.mgr.filesConns() {
		if cl, err := verifyTicket(head.Ticket, ds.d.mgr.tokenOf(c.ID), c.WorkerID, time.Now()); err == nil {
			claims, connID = cl, c.ID
			break
		}
	}
	if claims == nil || (head.Mode == accessWrite && claims.M != accessWrite) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	envelope, err := base64.StdEncoding.DecodeString(head.Request)
	if err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.WriteHeader(http.StatusOK)
	t := &directTransport{w: w, body: body}
	if err := ds.d.files.handle(connID, t, claims.M, envelope, fromInfo{Site: "a site on the local network"}); err != nil {
		_ = writeFrame(w, 'X', 0, []byte(err.Error()))
	}
	_, _ = io.Copy(io.Discard, body) // let a client that is still sending finish
}

type directTransport struct {
	w    http.ResponseWriter
	body io.Reader
}

func (t *directTransport) flush() {
	if f, ok := t.w.(http.Flusher); ok {
		f.Flush()
	}
}

func (t *directTransport) reply(sealed []byte, done bool) error {
	flags := byte(0)
	if done {
		flags = 1
	}
	err := writeFrame(t.w, 'R', flags, sealed)
	t.flush()
	return err
}

func (t *directTransport) nextUp(n uint64) ([]byte, bool, error) {
	typ, flags, payload, err := readFrame(t.body, filesChunkMax+4096)
	if err != nil {
		return nil, false, err
	}
	if typ != 'U' {
		return nil, false, errors.New("expected an upload chunk")
	}
	return payload, flags&1 == 1, nil
}

func (t *directTransport) putDown(n uint64, data []byte, last bool) error {
	flags := byte(0)
	if last {
		flags = 1
	}
	err := writeFrame(t.w, 'D', flags, data)
	t.flush()
	return err
}

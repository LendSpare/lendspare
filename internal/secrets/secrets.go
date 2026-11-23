// Package secrets stores the device private key and connection tokens: in the operating
// system's key store when there is one, otherwise in a private file beside the config.
package secrets

import (
	"encoding/json"
	"errors"
	"log"
	"os"
	"path/filepath"
	"sync"
)

// Where the device's private key and each connection's token live. The operating system's key
// store when there is one; otherwise a file readable by this user only, beside the config.
// A secret is never written to a log, to stdout, to a URL or to config.json.

// ErrNotFound is returned by Store.Get for an absent secret.
var ErrNotFound = errors.New("no such secret")

// Store keeps named secrets.
type Store interface {
	Get(name string) (string, error) // ErrNotFound when absent
	Set(name, value string) error
	Delete(name string) error // absent is not an error
}

// File is the documented fallback (headless Linux, a locked keyring) and what tests use.
type File struct {
	mu   sync.Mutex
	path string
}

// NewFile returns a File store kept as secrets.json in dir.
func NewFile(dir string) *File {
	return &File{path: filepath.Join(dir, "secrets.json")}
}

func (f *File) read() (map[string]string, error) {
	m := map[string]string{}
	b, err := os.ReadFile(f.path)
	if errors.Is(err, os.ErrNotExist) {
		return m, nil
	}
	if err != nil {
		return nil, err
	}
	return m, json.Unmarshal(b, &m)
}

func (f *File) write(m map[string]string) error {
	if err := os.MkdirAll(filepath.Dir(f.path), 0o700); err != nil {
		return err
	}
	b, _ := json.MarshalIndent(m, "", "  ")
	tmp := f.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, f.path)
}

func (f *File) Get(name string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	m, err := f.read()
	if err != nil {
		return "", err
	}
	v, ok := m[name]
	if !ok {
		return "", ErrNotFound
	}
	return v, nil
}

func (f *File) Set(name, value string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	m, err := f.read()
	if err != nil {
		return err
	}
	m[name] = value
	return f.write(m)
}

func (f *File) Delete(name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	m, err := f.read()
	if err != nil {
		return err
	}
	if _, ok := m[name]; !ok {
		return nil
	}
	delete(m, name)
	return f.write(m)
}

// Layered prefers the OS store and falls back to the file when the store refuses
// (no keyring daemon, a locked login keychain), so a connection is never lost to that.
type Layered struct {
	Primary, Fallback Store
}

func (l Layered) Get(name string) (string, error) {
	if v, err := l.Primary.Get(name); err == nil {
		return v, nil
	}
	return l.Fallback.Get(name)
}

func (l Layered) Set(name, value string) error {
	if err := l.Primary.Set(name, value); err != nil {
		log.Printf("[secrets] the system key store refused a write (%v) - using the private file instead", err)
		return l.Fallback.Set(name, value)
	}
	_ = l.Fallback.Delete(name) // never leave an older copy behind
	return nil
}

func (l Layered) Delete(name string) error {
	e1 := l.Primary.Delete(name)
	e2 := l.Fallback.Delete(name)
	if e1 != nil && e2 != nil {
		return e1
	}
	return nil
}

// Open picks the store for this machine; LENDSPARE_SECRETS=file forces the file.
func Open(dir string) Store {
	file := NewFile(dir)
	if os.Getenv("LENDSPARE_SECRETS") == "file" {
		return file
	}
	if sys := system(); sys != nil {
		return Layered{Primary: sys, Fallback: file}
	}
	return file
}

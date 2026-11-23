package secrets

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestFileSecretsArePrivateAndLayeredFallbackWorks(t *testing.T) {
	dir := t.TempDir()
	f := NewFile(dir)
	if _, err := f.Get("x"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("absent secret: %v", err)
	}
	if err := f.Set("x", "v"); err != nil {
		t.Fatal(err)
	}
	if v, _ := f.Get("x"); v != "v" {
		t.Fatal("round trip")
	}
	if runtime.GOOS != "windows" {
		if fi, _ := os.Stat(filepath.Join(dir, "secrets.json")); fi.Mode().Perm() != 0o600 {
			t.Fatalf("mode %v", fi.Mode().Perm())
		}
	}
	l := Layered{Primary: failingSecrets{}, Fallback: f}
	if err := l.Set("y", "w"); err != nil {
		t.Fatal(err)
	}
	if v, err := l.Get("y"); err != nil || v != "w" {
		t.Fatalf("fallback read: %q %v", v, err)
	}
	_ = l.Delete("y")
	if _, err := l.Get("y"); err == nil {
		t.Fatal("deleted secret still there")
	}
}

type failingSecrets struct{}

func (failingSecrets) Get(string) (string, error) { return "", errors.New("locked") }
func (failingSecrets) Set(string, string) error   { return errors.New("locked") }
func (failingSecrets) Delete(string) error        { return errors.New("locked") }

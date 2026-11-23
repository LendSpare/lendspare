//go:build darwin

package secrets

import (
	"bytes"
	"encoding/hex"
	"errors"
	"fmt"
	"os/exec"
	"strings"

	"github.com/LendSpare/lendspare/internal/proc"
)

// The macOS login keychain through the `security` tool. A secret is written through
// `security -i` (commands on stdin), never on the command line where `ps` would show it, and
// stored hex-encoded so no quoting can change it.

type keychain struct{}

func system() Store {
	if _, err := exec.LookPath("security"); err != nil {
		return nil
	}
	return keychain{}
}

const keychainService = "LendSpare"

func (keychain) Get(name string) (string, error) {
	out, err := proc.Command("security", "find-generic-password", "-s", keychainService, "-a", name, "-w").Output()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) && ee.ExitCode() == 44 { // errSecItemNotFound
			return "", ErrNotFound
		}
		return "", fmt.Errorf("keychain: %w", err)
	}
	raw, err := hex.DecodeString(strings.TrimSpace(string(out)))
	if err != nil {
		return "", ErrNotFound
	}
	return string(raw), nil
}

func (keychain) Set(name, value string) error {
	cmd := proc.Command("security", "-i")
	cmd.Stdin = strings.NewReader(fmt.Sprintf("add-generic-password -U -s %s -a %s -w %s\n", keychainService, name, hex.EncodeToString([]byte(value))))
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("keychain: %w %s", err, strings.TrimSpace(stderr.String()))
	}
	if strings.Contains(stderr.String(), "security:") && strings.Contains(stderr.String(), "rror") {
		return errors.New("keychain: " + strings.TrimSpace(stderr.String()))
	}
	return nil
}

func (keychain) Delete(name string) error {
	err := proc.Command("security", "delete-generic-password", "-s", keychainService, "-a", name).Run()
	var ee *exec.ExitError
	if errors.As(err, &ee) && ee.ExitCode() == 44 {
		return nil
	}
	return err
}

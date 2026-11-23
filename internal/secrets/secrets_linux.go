//go:build linux

package secrets

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/LendSpare/lendspare/internal/proc"
)

// The Secret Service (GNOME Keyring, KWallet) through `secret-tool`, when it is installed and
// a desktop session bus exists. No library: the secret goes to the tool on stdin. Without
// either, the private file beside the config is used (secrets.go).

type secretService struct{}

func system() Store {
	if os.Getenv("DBUS_SESSION_BUS_ADDRESS") == "" {
		return nil
	}
	if _, err := exec.LookPath("secret-tool"); err != nil {
		return nil
	}
	return secretService{}
}

func (secretService) Get(name string) (string, error) {
	out, err := proc.Command("secret-tool", "lookup", "service", "LendSpare", "account", name).Output()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) && ee.ExitCode() == 1 {
			return "", ErrNotFound
		}
		return "", fmt.Errorf("secret service: %w", err)
	}
	if len(out) == 0 {
		return "", ErrNotFound
	}
	return strings.TrimRight(string(out), "\r\n"), nil
}

func (secretService) Set(name, value string) error {
	cmd := proc.Command("secret-tool", "store", "--label=LendSpare", "service", "LendSpare", "account", name)
	cmd.Stdin = strings.NewReader(value)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("secret service: %w %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func (secretService) Delete(name string) error {
	_ = proc.Command("secret-tool", "clear", "service", "LendSpare", "account", name).Run()
	return nil
}

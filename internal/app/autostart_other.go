//go:build !windows && !darwin && !linux

package app

import "errors"

func autostartEnabled() bool { return false }

func setAutostart(bool) error { return errors.New("start at login is not supported on this system") }

//go:build !windows && !linux

package app

import "errors"

// macOS associates a URL scheme with an application bundle (CFBundleURLTypes in Info.plist),
// which a bare executable cannot declare. Until LendSpare is packaged as an app bundle the link
// is not registered there; connecting by typing the code works, and so does `lendspare connect`.

func registerLink() error {
	return errors.New("lendspare:// links need LendSpare to be installed as an app; use the code instead")
}

func unregisterLink() error { return nil }

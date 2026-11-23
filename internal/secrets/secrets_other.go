//go:build !windows && !darwin && !linux

package secrets

// No system key store on this platform: the private file is used.
func system() Store { return nil }

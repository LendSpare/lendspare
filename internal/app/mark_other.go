//go:build !windows && !darwin

package app

// markDownloaded: Linux desktops have no common "downloaded" mark; written files are never
// executable (0644) and program files are refused unless the folder allows them.
func markDownloaded(string, fromInfo) {}

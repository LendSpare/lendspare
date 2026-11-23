//go:build windows

package app

import "os"

// markDownloaded gives a file a site wrote the "downloaded from the internet" mark (the
// Zone.Identifier stream), so Windows warns before running or opening it like any download.
func markDownloaded(abs string, from fromInfo) {
	host := from.Site
	if host == "" {
		host = "about:internet"
	}
	before, err := os.Stat(abs)
	_ = os.WriteFile(abs+":Zone.Identifier", []byte("[ZoneTransfer]\r\nZoneId=3\r\nHostUrl="+host+"\r\n"), 0o644)
	// Writing the stream stamps the file as modified now; it keeps the time the site sent,
	// which "newer replaces older" compares against on the next write.
	if err == nil {
		_ = os.Chtimes(abs, before.ModTime(), before.ModTime())
	}
}

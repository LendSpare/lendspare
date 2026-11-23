//go:build darwin

package app

import (
	"fmt"
	"time"

	"github.com/LendSpare/lendspare/internal/proc"
)

// markDownloaded sets com.apple.quarantine on a file a site wrote, so Gatekeeper treats it as
// a download (asks before opening it).
func markDownloaded(abs string, _ fromInfo) {
	_ = proc.Command("xattr", "-w", "com.apple.quarantine", fmt.Sprintf("0081;%x;lendspare;", time.Now().Unix()), abs).Run()
}

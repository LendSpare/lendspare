package app

import (
	"path/filepath"
	"syscall"
)

var windowsNetErrnos []syscall.Errno

// isNetworkVolume: the folder (or, when it cannot be reached, the nearest folder above it
// that can) is on an SMB, AFP, NFS or WebDAV mount - a folder shared from a Windows PC
// is smbfs. macOS guards those with its own "Network Volumes" switch.
func isNetworkVolume(dir string) bool {
	for p := filepath.Clean(dir); ; p = filepath.Dir(p) {
		var st syscall.Statfs_t
		if syscall.Statfs(p, &st) == nil {
			b := make([]byte, 0, len(st.Fstypename))
			for _, c := range st.Fstypename {
				if c == 0 {
					break
				}
				b = append(b, byte(c))
			}
			switch string(b) {
			case "smbfs", "afpfs", "nfs", "webdav", "cifs":
				return true
			}
			return false
		}
		if p == filepath.Dir(p) {
			return false
		}
	}
}

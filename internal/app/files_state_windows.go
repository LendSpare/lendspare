package app

import (
	"strings"
	"syscall"
)

// ERROR_UNEXP_NET_ERR, ERROR_NETNAME_DELETED, ERROR_SEM_TIMEOUT, ERROR_NETWORK_UNREACHABLE,
// ERROR_HOST_UNREACHABLE, ERROR_CONNECTION_REFUSED, ERROR_NO_NETWORK.
var windowsNetErrnos = []syscall.Errno{59, 64, 121, 1231, 1232, 1225, 1222}

// isNetworkVolume: a UNC path. A mapped drive letter is not looked up; the state still says
// "offline" or "noaccess" for it, only the hint is the generic one.
func isNetworkVolume(dir string) bool {
	return strings.HasPrefix(dir, `\\`) || strings.HasPrefix(dir, "//")
}

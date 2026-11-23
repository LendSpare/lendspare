package app

import "sync"

// isPackaged reports whether LendSpare runs from an MSIX package (Microsoft Store). Windows then
// owns the lendspare:// link and start-at-login: both are declared in the package manifest
// (packaging/msix), and registry writes for them would be virtualized away.
var isPackaged = sync.OnceValue(runningPackaged)

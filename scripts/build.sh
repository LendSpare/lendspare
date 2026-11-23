#!/usr/bin/env bash
# Builds LendSpare for Linux (x86-64, ARM64), Windows and macOS (Apple silicon, Intel) into dist/, with a
# .sha256 next to each file. The names are the release assets lendspare.com links to.
# Nothing is written outside this repository.
#
#   bash scripts/build.sh                 # with a local Go 1.25+ (or: make dist)
#   GO_DOCKER=1 bash scripts/build.sh     # in golang:1.25 (docker.exe on Windows)
#
# The macOS binary needs cgo (its menu-bar tray is Cocoa via cgo, internal/app/tray_darwin.go) and so must
# be built natively on a Mac with Xcode's command line tools - cgo cannot cross-compile to
# macOS, so GO_DOCKER=1 falls back to a console-only lendspare-macos (no tray).
set -euo pipefail
here="$(cd "$(dirname "$0")/.." && pwd)"
pkg=./cmd/lendspare
out="$here/dist"
mkdir -p "$out"

build() { # os arch file [ldflags] [cgo]
    local flags="${4:--s -w}"
    local cgo="${5:-0}"
    if [ "${GO_DOCKER:-}" = "1" ]; then
        if [ "$1" = "darwin" ] && [ "$cgo" = "1" ]; then
            echo "note: GO_DOCKER cannot cross-compile cgo to macOS (no Xcode SDK in the container) - $3 will be console-only, no tray" >&2
        fi
        MSYS_NO_PATHCONV=1 "${DOCKER:-docker.exe}" run --rm -v "$here:/src" -w /src \
            -e CGO_ENABLED=0 -e GOOS="$1" -e GOARCH="$2" golang:1.25 go build -trimpath -ldflags="$flags" -o "/src/dist/$3" "$pkg"
    else
        (cd "$here" && CGO_ENABLED="$cgo" GOOS="$1" GOARCH="$2" go build -trimpath -ldflags="$flags" -o "$out/$3" "$pkg")
    fi
    # SIGN_CMD signs the file in place (e.g. "osslsigncode sign ... -in X -out X", or a signtool
    # wrapper) BEFORE the hash is taken: SmartScreen reputation attaches to the signed file.
    if [ -n "${SIGN_CMD:-}" ] && [ "$1" = "windows" ]; then (cd "$out" && $SIGN_CMD "$3"); fi
    (cd "$out" && sha256sum "$3" > "$3.sha256")
    echo "built $3: $(cut -d' ' -f1 "$out/$3.sha256")"
}

build linux amd64 lendspare-linux
build linux arm64 lendspare-linux-arm64
# Windows: a GUI program (tray icon, no console window on a double-click; started with flags
# from a terminal it attaches to that terminal) and NOT stripped - stripped Go binaries are a
# well-known antivirus false-positive source.
build windows amd64 lendspare-windows.exe "-H=windowsgui"
# cgo=1: the menu-bar tray (internal/app/tray_darwin.go) is Cocoa via cgo - see the note at the top.
build darwin arm64 lendspare-macos "-s -w" 1
build darwin amd64 lendspare-macos-amd64 "-s -w" 1

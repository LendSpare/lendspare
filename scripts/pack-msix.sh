#!/usr/bin/env bash
# Packs the Windows build into an MSIX for the Microsoft Store (the Store signs it; no
# certificate of your own is needed). Runs on Windows in Git Bash / MSYS2; needs makeappx.exe
# from the Windows SDK. Used by `make msix`, or directly:
#
#   scripts/pack-msix.sh [path/to/lendspare-windows.exe]      # default: dist/lendspare-windows.exe
#   scripts/pack-msix.sh --register [exe]                 # also install it locally (Developer Mode)
#
# Configuration (environment, or an untracked scripts/msix.env). The three identity values are
# shown in Partner Center under the app's Product management > Product identity:
#   MSIX_IDENTITY_NAME            Package/Identity/Name, e.g. 12345Publisher.LendSpare
#   MSIX_PUBLISHER                Package/Identity/Publisher, e.g. CN=1A2B3C4D-...
#   MSIX_PUBLISHER_DISPLAY_NAME   Package/Properties/PublisherDisplayName
#   MSIX_DISPLAY_NAME             optional: the reserved app name (default: LendSpare)
#   MSIX_VERSION                  optional: a.b.c.0 (default: daemonVersion from internal/app/main.go)
#   MAKEAPPX                      optional: makeappx.exe (default: newest Windows SDK x64 one)
# Without them the package gets a local test identity, fine for --register but not for the Store.
set -euo pipefail

# makeappx takes /flags: stop MSYS from rewriting them into Windows paths.
export MSYS_NO_PATHCONV=1 MSYS2_ARG_CONV_EXCL='*'
shopt -u patsub_replacement 2>/dev/null || true # bash 5.2: "&" in ${x//a/b} must stay literal

root="$(cd "$(dirname "$0")/.." && pwd)"
[ -f "$root/scripts/msix.env" ] && . "$root/scripts/msix.env"
die() { echo "pack-msix: $*" >&2; exit 1; }

register=0
if [ "${1:-}" = "--register" ]; then register=1; shift; fi
exe="${1:-$root/dist/lendspare-windows.exe}"
[ -f "$exe" ] || die "no $exe - run 'make dist' first (or pass the exe)"

identity="${MSIX_IDENTITY_NAME:-LendSpare.LocalTest}"
publisher="${MSIX_PUBLISHER:-CN=LendSpare Local Test}"
publisher_name="${MSIX_PUBLISHER_DISPLAY_NAME:-LendSpare Local Test}"
display_name="${MSIX_DISPLAY_NAME:-LendSpare}"
version="${MSIX_VERSION:-}"
if [ -z "$version" ]; then
    v="$(sed -n 's/^[[:space:]]*daemonVersion[[:space:]]*=[[:space:]]*"\([0-9.]*\)".*/\1/p' "$root/internal/app/main.go")"
    [ -n "$v" ] || die "cannot read daemonVersion from internal/app/main.go; set MSIX_VERSION"
    version="$v.0"
fi
[[ "$version" =~ ^[0-9]+\.[0-9]+\.[0-9]+\.0$ ]] || die "MSIX_VERSION must look like 0.0.2.0 (the Store requires the last part 0), got $version"
[ -n "${MSIX_IDENTITY_NAME:-}" ] || echo "pack-msix: no MSIX_IDENTITY_NAME - using a local test identity (not for the Store)" >&2

winpath() { cygpath -w "$1" 2>/dev/null || echo "$1"; }
xml() { local s="$1"; s="${s//&/&amp;}"; s="${s//</&lt;}"; s="${s//>/&gt;}"; s="${s//\"/&quot;}"; printf '%s' "$s"; }

makeappx="${MAKEAPPX:-$(find "/c/Program Files (x86)/Windows Kits/10/bin" -path '*/x64/makeappx.exe' 2>/dev/null | sort -V | tail -n 1)}"
[ -n "$makeappx" ] && [ -f "$makeappx" ] || die "makeappx.exe not found: install the Windows SDK, or set MAKEAPPX"

layout="$root/build/msix/layout"
rm -rf "$layout"
mkdir -p "$layout"
cp "$exe" "$layout/lendspare.exe"
cp -r "$root/packaging/msix/Assets" "$layout/Assets"

manifest="$(cat "$root/packaging/msix/AppxManifest.xml.in")"
manifest="${manifest//@IDENTITY_NAME@/$(xml "$identity")}"
manifest="${manifest//@PUBLISHER_DISPLAY_NAME@/$(xml "$publisher_name")}"
manifest="${manifest//@PUBLISHER@/$(xml "$publisher")}"
manifest="${manifest//@DISPLAY_NAME@/$(xml "$display_name")}"
manifest="${manifest//@VERSION@/$version}"
manifest="${manifest//@ARCH@/x64}"
[[ "$manifest" != *@*@* ]] || die "unfilled placeholder in the manifest"
printf '%s\n' "$manifest" > "$layout/AppxManifest.xml"

mkdir -p "$root/dist"
out="$root/dist/LendSpare_${version}_x64.msix"
"$makeappx" pack /o /h SHA256 /d "$(winpath "$layout")" /p "$(winpath "$out")"
echo "packed $out (identity $identity, version $version)"

if [ "$register" = 1 ]; then
    # Installs the unsigned layout for testing; needs Settings > System > For developers > Developer Mode.
    powershell.exe -NoProfile -Command "Add-AppxPackage -Register '$(winpath "$layout/AppxManifest.xml")'"
    echo "registered: start LendSpare from the Start menu; remove with: powershell -c \"Get-AppxPackage $identity | Remove-AppxPackage\""
fi

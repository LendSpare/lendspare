#!/usr/bin/env bash
# Signs Windows binaries with Azure Artifact Signing (formerly Trusted Signing), then verifies them.
# Runs on Windows in Git Bash / MSYS2. Used by `make dist-signed`, or directly:
#
#   scripts/sign-windows.sh dist/lendspare-windows.exe
#
# Configuration (environment, e.g. from an untracked scripts/signing.env):
#   ARTIFACT_SIGNING_ENDPOINT   account endpoint for its region, e.g. https://weu.codesigning.azure.net/
#   ARTIFACT_SIGNING_ACCOUNT    Artifact Signing account name
#   ARTIFACT_SIGNING_PROFILE    certificate profile name
#   SIGNTOOL                    optional: signtool.exe (default: newest Windows SDK x64 one)
#   ARTIFACT_SIGNING_DLIB       optional: Azure.CodeSigning.Dlib.dll (default: searched in the
#                               Artifact Signing Client Tools / NuGet install locations)
#
# Sign-in: `az login` once, or AZURE_TENANT_ID / AZURE_CLIENT_ID / AZURE_CLIENT_SECRET for a
# service principal with the "Artifact Signing Certificate Profile Signer" role.
set -euo pipefail

# signtool takes /flags: stop MSYS from rewriting them into Windows paths.
export MSYS_NO_PATHCONV=1 MSYS2_ARG_CONV_EXCL='*'

here="$(cd "$(dirname "$0")" && pwd)"
[ -f "$here/signing.env" ] && . "$here/signing.env"

die() { echo "sign-windows: $*" >&2; exit 1; }
[ $# -gt 0 ] || die "usage: $0 file.exe..."
: "${ARTIFACT_SIGNING_ENDPOINT:?set ARTIFACT_SIGNING_ENDPOINT (see the header of $0)}"
: "${ARTIFACT_SIGNING_ACCOUNT:?set ARTIFACT_SIGNING_ACCOUNT}"
: "${ARTIFACT_SIGNING_PROFILE:?set ARTIFACT_SIGNING_PROFILE}"

winpath() { cygpath -w "$1" 2>/dev/null || echo "$1"; }
unixpath() { cygpath -u "$1" 2>/dev/null || echo "$1"; }

find_signtool() {
    find "/c/Program Files (x86)/Windows Kits/10/bin" -path '*/x64/signtool.exe' 2>/dev/null | sort -V | tail -n 1
}

find_dlib() {
    local root
    for root in "${LOCALAPPDATA:-}" "${ProgramFiles:-}" "${USERPROFILE:-}/.nuget/packages"; do
        [ -n "$root" ] || continue
        root="$(unixpath "$root")"
        [ -d "$root" ] || continue
        find "$root" -maxdepth 6 -ipath '*x64*' -iname 'Azure.CodeSigning.Dlib.dll' 2>/dev/null | sort -V | tail -n 1
    done | tail -n 1
}

signtool="${SIGNTOOL:-$(find_signtool)}"
[ -n "$signtool" ] && [ -f "$(unixpath "$signtool")" ] || die "signtool.exe not found: install the Artifact Signing Client Tools or the Windows SDK, or set SIGNTOOL"
dlib="${ARTIFACT_SIGNING_DLIB:-$(find_dlib)}"
[ -n "$dlib" ] && [ -f "$(unixpath "$dlib")" ] || die "Azure.CodeSigning.Dlib.dll not found: install the Artifact Signing Client Tools, or set ARTIFACT_SIGNING_DLIB"

meta="$(mktemp -t artifact-signing-XXXXXX.json)"
trap 'rm -f "$meta"' EXIT
cat > "$meta" <<EOF
{
  "Endpoint": "$ARTIFACT_SIGNING_ENDPOINT",
  "CodeSigningAccountName": "$ARTIFACT_SIGNING_ACCOUNT",
  "CertificateProfileName": "$ARTIFACT_SIGNING_PROFILE"
}
EOF

files=()
for f in "$@"; do
    [ -f "$f" ] || die "no such file: $f"
    files+=("$(winpath "$f")")
done

# /tr: RFC 3161 timestamp, so the signature stays valid after the short-lived certificate expires.
"$(unixpath "$signtool")" sign /v /fd SHA256 /tr http://timestamp.acs.microsoft.com /td SHA256 \
    /dlib "$(winpath "$dlib")" /dmdf "$(winpath "$meta")" "${files[@]}"
"$(unixpath "$signtool")" verify /pa /q "${files[@]}"
echo "signed and verified: $*"

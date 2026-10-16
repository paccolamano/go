#!/usr/bin/env bash
# Downloads a CI tool pinned in versions.env, verifies its SHA-256 against the
# pinned value (never against the release's own checksum file) and installs
# the binary into <dest-dir>. linux/amd64 only: it is used by the workflows.
#
# Usage: scripts/install-tool.sh <trivy|gitleaks|bearer> <dest-dir>
set -euo pipefail

# shellcheck source=lib.sh
. "$(dirname "$0")/lib.sh"

[ $# -eq 2 ] || die 2 "usage: $0 <trivy|gitleaks|bearer> <dest-dir>"
tool=$1
dest=$2
REPO_ROOT=$(repo_root)
load_versions

base=${INSTALL_TOOL_BASE_URL:-https://github.com}
case "$tool" in
trivy)
	version=$TRIVY_VERSION
	want=$TRIVY_SHA256
	asset="aquasecurity/trivy/releases/download/v${version}/trivy_${version}_Linux-64bit.tar.gz"
	;;
gitleaks)
	version=$GITLEAKS_VERSION
	want=$GITLEAKS_SHA256
	asset="gitleaks/gitleaks/releases/download/v${version}/gitleaks_${version}_linux_x64.tar.gz"
	;;
bearer)
	version=$BEARER_VERSION
	want=$BEARER_SHA256
	asset="Bearer/bearer/releases/download/v${version}/bearer_${version}_linux_amd64.tar.gz"
	;;
*)
	die 2 "unknown tool '$tool' (expected trivy, gitleaks or bearer)"
	;;
esac

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

curl -fsSL --retry 3 -o "$tmp/archive.tar.gz" "$base/$asset" || die 1 "download failed: $base/$asset"
got=$(sha256_of "$tmp/archive.tar.gz")
[ "$got" = "$want" ] || die 1 "SHA-256 mismatch for $tool $version: expected $want, got $got"
tar -xzf "$tmp/archive.tar.gz" -C "$tmp" "$tool" || die 1 "binary '$tool' not found in the archive"

mkdir -p "$dest"
mv "$tmp/$tool" "$dest/$tool"
chmod +x "$dest/$tool"
log "installed $tool $version to $dest/$tool"

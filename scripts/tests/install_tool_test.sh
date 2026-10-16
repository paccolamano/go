#!/usr/bin/env bash
# Tests for scripts/install-tool.sh, using a local file:// release mirror.
# shellcheck source=helpers.sh
. "$(dirname "${BASH_SOURCE[0]}")/helpers.sh"

# setup_mirror <sha256|"real"> builds a fake gitleaks 9.9.9 release and a
# versions.env pinning it with the given hash ("real" = the archive's hash).
setup_mirror() {
	local mirror build archive sha
	REPO_ROOT=$(tmpdir)
	export REPO_ROOT
	mirror=$(tmpdir)
	build=$(tmpdir)
	mkdir -p "$mirror/gitleaks/gitleaks/releases/download/v9.9.9"
	archive="$mirror/gitleaks/gitleaks/releases/download/v9.9.9/gitleaks_9.9.9_linux_x64.tar.gz"
	printf '#!/bin/sh\necho fake gitleaks\n' >"$build/gitleaks"
	chmod +x "$build/gitleaks"
	printf 'readme\n' >"$build/README.md"
	(cd "$build" && tar -czf "$archive" gitleaks README.md)
	sha=$1
	[ "$sha" != real ] || sha=$(sha256_file "$archive")
	printf 'GITLEAKS_VERSION=9.9.9\nGITLEAKS_SHA256=%s\n' "$sha" >"$REPO_ROOT/versions.env"
	INSTALL_TOOL_BASE_URL="file://$mirror"
	export INSTALL_TOOL_BASE_URL
}

test_matching_hash_installs_only_the_binary() {
	setup_mirror real
	dest="$(tmpdir)/bin"
	assert_exit 0 "$SCRIPTS_DIR/install-tool.sh" gitleaks "$dest"
	[ -x "$dest/gitleaks" ] || fail "gitleaks is not installed as an executable"
	assert_eq "gitleaks" "$(ls "$dest")"
	assert_eq "fake gitleaks" "$("$dest/gitleaks")"
}

test_mismatched_hash_fails_and_installs_nothing() {
	zeros=0000000000000000000000000000000000000000000000000000000000000000
	setup_mirror "$zeros"
	dest="$(tmpdir)/bin"
	assert_exit 1 "$SCRIPTS_DIR/install-tool.sh" gitleaks "$dest"
	assert_contains "$OUT" "SHA-256 mismatch"
	assert_contains "$OUT" "expected $zeros"
	[ ! -e "$dest/gitleaks" ] || fail "binary installed despite hash mismatch"
}

test_unknown_tool_exits_2() {
	setup_mirror real
	assert_exit 2 "$SCRIPTS_DIR/install-tool.sh" nope "$(tmpdir)"
	assert_contains "$OUT" "unknown tool 'nope'"
}

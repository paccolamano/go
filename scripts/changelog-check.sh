#!/usr/bin/env bash
# Changelog discipline (FR-033): a pull request that changes a module's
# non-test Go sources or its go.mod must also change that module's
# CHANGELOG.md. Tests, testdata and documentation are exempt.
#
# Usage: scripts/changelog-check.sh <base> <head>
# Env:   CHANGELOG_WAIVED=true  the pull request carries the skip-changelog label
set -euo pipefail

# shellcheck source=lib.sh
. "$(dirname "$0")/lib.sh"

[ $# -eq 2 ] || die 2 "usage: $0 <base> <head>"
REPO_ROOT=$(repo_root)

roots=$(discover_modules)
files=$(git -C "$REPO_ROOT" diff --name-only "$1...$2") || die 2 "cannot compute the diff $1...$2"

needs=$(printf '%s\n' "$files" | while IFS= read -r f; do
	[ -n "$f" ] || continue
	m=$(module_for_path "$f" "$roots")
	[ -n "$m" ] || continue
	case "${f#"$m"/}" in
	*_test.go | testdata/* | */testdata/*) ;;
	go.mod | *.go) printf '%s\n' "$m" ;;
	esac
done | LC_ALL=C sort -u)

if [ -z "$needs" ]; then
	log "no module source changes require a changelog entry"
	exit 0
fi

for m in $needs; do
	if printf '%s\n' "$files" | grep -qxF -- "$m/CHANGELOG.md"; then
		summary_add PASS "$m"
	elif [ "${CHANGELOG_WAIVED:-false}" = true ]; then
		summary_add PASS "$m" "waived by label"
	else
		summary_add FAIL "$m" "CHANGELOG.md not updated"
	fi
done

summary_print changelog || exit 1

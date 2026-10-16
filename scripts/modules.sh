#!/usr/bin/env bash
# Module discovery for the workflows.
#
# Usage:
#   scripts/modules.sh list                   every module root, one per line
#   scripts/modules.sh touched <base> <head>  module roots changed since the merge base of
#                                             <base> and <head>; every module when shared
#                                             tooling changed
#   scripts/modules.sh json-go-versions       supported Go versions as a JSON array
set -euo pipefail

# shellcheck source=lib.sh
. "$(dirname "$0")/lib.sh"

# Files whose change can alter the verdict of every module's gates.
SHARED_TOOLING='^(\.golangci\.yml|versions\.env|Makefile|scripts/.*|\.github/workflows/.*|\.github/commit-scopes|\.module-exceptions|\.trivyignore|\.gitleaksignore|\.semgrepignore|bearer\.yml|bearer\.ignore)$'

touched() {
	local roots files
	roots=$(discover_modules)
	[ -n "$roots" ] || return 0
	files=$(git -C "$REPO_ROOT" diff --name-only "$1...$2") || die 2 "cannot compute the diff $1...$2"
	[ -n "$files" ] || return 0
	if printf '%s\n' "$files" | grep -Eq "$SHARED_TOOLING"; then
		printf '%s\n' "$roots"
		return 0
	fi
	printf '%s\n' "$files" | while IFS= read -r f; do
		module_for_path "$f" "$roots"
	done | LC_ALL=C sort -u
}

REPO_ROOT=$(repo_root)
case "${1:-}" in
list)
	discover_modules
	;;
touched)
	[ $# -eq 3 ] || die 2 "usage: $0 touched <base> <head>"
	touched "$2" "$3"
	;;
json-go-versions)
	load_versions
	printf '["%s","%s"]\n' "$GO_VERSION_OLDEST" "$GO_VERSION_NEWEST"
	;;
*)
	die 2 "usage: $0 list | touched <base> <head> | json-go-versions"
	;;
esac

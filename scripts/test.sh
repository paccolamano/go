#!/usr/bin/env bash
# Local gate 2: race-enabled, shuffled tests per module, with the statement
# coverage threshold (80%, or the module's .module-exceptions entry).
#
# Usage: scripts/test.sh --all | <root>...
set -euo pipefail

# shellcheck source=lib.sh
. "$(dirname "$0")/lib.sh"

REPO_ROOT=$(repo_root)
load_versions
resolve_targets "$@"
[ -n "$TARGETS" ] || exit 0

command -v go >/dev/null 2>&1 || die 2 "go not found; install Go $GO_VERSION_OLDEST or newer"
cc=$(go env CC)
command -v "$cc" >/dev/null 2>&1 ||
	die 2 "C compiler '$cc' not found, required by -race: install gcc/clang (Xcode CLT on macOS)"
parse_exceptions

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

for m in $TARGETS; do
	log "==> test $m"
	profile="$tmp/$(printf '%s' "$m" | tr / _).out"
	if ! (cd "$REPO_ROOT/$m" && GOWORK=off go test -race -shuffle=on -covermode=atomic -coverprofile="$profile" ./...); then
		summary_add FAIL "$m" "tests failed"
		continue
	fi
	if [ ! -f "$profile" ] || ! grep -qv '^mode:' "$profile"; then
		summary_add PASS "$m" "no statements"
		continue
	fi
	total=$(cd "$REPO_ROOT/$m" && GOWORK=off go tool cover -func="$profile" | awk '/^total:/ {sub(/%/, "", $NF); print $NF}')
	threshold=$(coverage_threshold "$m")
	note=""
	if [ -n "$(exception_value "$m" coverage)" ]; then
		note="  (threshold $threshold: $(exception_justification "$m" coverage))"
	fi
	if awk -v t="$total" -v min="$threshold" 'BEGIN { exit !(t + 0 >= min + 0) }'; then
		summary_add PASS "$m" "coverage $total%$note"
	else
		summary_add FAIL "$m" "coverage $total% < $threshold% threshold$note"
	fi
done

summary_print test || exit 1

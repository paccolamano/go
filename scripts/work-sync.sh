#!/usr/bin/env bash
# Regenerates go.work at the repository root from the discovered modules:
# a "go <GO_VERSION_OLDEST>.0" line, no toolchain line and one sorted "use"
# entry per module. go.work.sum is left in place. go.work is for local
# development only; no gate reads it.
#
# Usage: scripts/work-sync.sh
set -euo pipefail

# shellcheck source=lib.sh
. "$(dirname "$0")/lib.sh"

REPO_ROOT=$(repo_root)
load_versions
modules=$(discover_modules)
want="${GO_VERSION_OLDEST}.0"

cd "$REPO_ROOT"
rm -f go.work
GOWORK='' go work init
GOWORK='' go work edit -go="$want" -toolchain=none
if [ -n "$modules" ]; then
	set --
	for m in $modules; do
		set -- "$@" "./$m"
	done
	GOWORK='' go work use "$@"
fi

got=$(awk '$1 == "go" {print $2; exit}' go.work)
[ "$got" = "$want" ] ||
	die 1 "go.work now requires go $got, above the oldest supported Go $GO_VERSION_OLDEST: a module declares a newer go directive"
log "go.work lists $(printf '%s' "$modules" | grep -c . || true) module(s)"

#!/usr/bin/env bash
# Pull request gate 2: "go mod tidy -diff" reports no changes, per module.
#
# Usage: scripts/tidy-check.sh --all | <root>...
set -euo pipefail

# shellcheck source=lib.sh
. "$(dirname "$0")/lib.sh"

REPO_ROOT=$(repo_root)
resolve_targets "$@"
[ -n "$TARGETS" ] || exit 0

for m in $TARGETS; do
	log "==> tidy $m"
	if (cd "$REPO_ROOT/$m" && GOWORK=off go mod tidy -diff); then
		summary_add PASS "$m"
	else
		summary_add FAIL "$m" "go mod tidy reports changes"
	fi
done

summary_print tidy || exit 1

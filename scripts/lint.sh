#!/usr/bin/env bash
# Local gate 1: golangci-lint with the shared .golangci.yml, run from each
# module root with the workspace disabled.
#
# Usage: scripts/lint.sh --all | <root>...
# Env:   GOLANGCI_LINT  linter binary (default: golangci-lint)
set -euo pipefail

# shellcheck source=lib.sh
. "$(dirname "$0")/lib.sh"

REPO_ROOT=$(repo_root)
load_versions
resolve_targets "$@"
[ -n "$TARGETS" ] || exit 0

linter=${GOLANGCI_LINT:-golangci-lint}
want=${GOLANGCI_LINT_VERSION#v}
install_hint="curl -sSfL https://golangci-lint.run/install.sh | sh -s -- -b \"\$(go env GOPATH)/bin\" $GOLANGCI_LINT_VERSION"

command -v "$linter" >/dev/null 2>&1 ||
	die 2 "golangci-lint not found; expected version $want. Install it with: $install_hint"
got=$("$linter" version --short 2>/dev/null || true)
got=${got#v}
[ "$got" = "$want" ] ||
	die 2 "golangci-lint version mismatch: expected $want, found ${got:-unknown}. Install it with: $install_hint"

for m in $TARGETS; do
	log "==> lint $m"
	if (cd "$REPO_ROOT/$m" && GOWORK=off "$linter" run --config "$REPO_ROOT/.golangci.yml" ./...); then
		summary_add PASS "$m"
	else
		summary_add FAIL "$m" "lint findings"
	fi
done

summary_print lint || exit 1

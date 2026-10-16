#!/usr/bin/env bash
# Platform support (FR-031): every module builds and vets, without cgo, for
# linux, darwin and windows on amd64 and arm64, minus the platforms excluded by
# its "platforms" entry in .module-exceptions.
#
# Usage: scripts/cross-build.sh --all | <root>...
set -euo pipefail

# shellcheck source=lib.sh
. "$(dirname "$0")/lib.sh"

PLATFORMS="linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64 windows/arm64"

REPO_ROOT=$(repo_root)
resolve_targets "$@"
[ -n "$TARGETS" ] || exit 0
parse_exceptions

for m in $TARGETS; do
	allowed=$(exception_value "$m" platforms)
	failed=""
	count=0
	for p in $PLATFORMS; do
		case ",${allowed:-$p}," in *",$p,"*) ;; *) continue ;; esac
		count=$((count + 1))
		log "==> cross-build $m $p"
		if ! (cd "$REPO_ROOT/$m" &&
			CGO_ENABLED=0 GOOS=${p%/*} GOARCH=${p#*/} GOWORK=off go build ./... &&
			CGO_ENABLED=0 GOOS=${p%/*} GOARCH=${p#*/} GOWORK=off go vet ./...); then
			failed="${failed:+$failed }$p"
		fi
	done
	if [ -n "$failed" ]; then
		summary_add FAIL "$m" "$failed"
	else
		summary_add PASS "$m" "$count platforms"
	fi
done

summary_print cross-build || exit 1

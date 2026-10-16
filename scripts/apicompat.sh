#!/usr/bin/env bash
# Pull request gate 3: API compatibility of each module against its latest
# release of the same major version, checked with gorelease. Fails only on
# incompatible changes when that release is v1 or later; the report (with the
# suggested next version) is always printed and added to the job summary.
#
# Usage: scripts/apicompat.sh --all | <root>...
# Env:   GORELEASE  gorelease binary (default: gorelease)
set -euo pipefail

# shellcheck source=lib.sh
. "$(dirname "$0")/lib.sh"

REPO_ROOT=$(repo_root)
load_versions
resolve_targets "$@"
[ -n "$TARGETS" ] || exit 0

gorelease=${GORELEASE:-gorelease}
command -v "$gorelease" >/dev/null 2>&1 ||
	die 2 "gorelease not found; install it with: go install golang.org/x/exp/cmd/gorelease@$GORELEASE_VERSION"

# latest_release <dir> <major|"0-1"> prints the highest X.Y.Z release tagged
# <dir>/vX.Y.Z for that major (pre-releases excluded), or nothing.
latest_release() {
	git -C "$REPO_ROOT" tag -l "$1/v*" |
		cut -d/ -f2- |
		sed 's/^v//' |
		grep -E '^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$' |
		awk -F. -v major="$2" 'major == "0-1" ? ($1 == 0 || $1 == 1) : ($1 == major)' |
		sort -t. -k1,1n -k2,2n -k3,3n |
		tail -n 1 || true
}

for m in $TARGETS; do
	dir=${m%%/*}
	path=$(awk '$1 == "module" {print $2; exit}' "$REPO_ROOT/$m/go.mod")
	suffix=${path##*/}
	if is_major_dir "$suffix"; then
		release=$(latest_release "$dir" "${suffix#v}")
	else
		release=$(latest_release "$dir" 0-1)
	fi
	base=${release:+v$release}
	base=${base:-none}

	log "==> api-compat $m (base $base)"
	set +e
	report=$(cd "$REPO_ROOT/$m" && GOWORK=off "$gorelease" -base="$base" 2>&1)
	status=$?
	set -e
	printf '%s\n' "$report"
	if [ "${GITHUB_ACTIONS:-}" = true ] && [ -n "${GITHUB_STEP_SUMMARY:-}" ]; then
		# shellcheck disable=SC2016 # literal Markdown code fence, nothing to expand
		printf '### gorelease %s (base %s)\n\n```text\n%s\n```\n\n' "$m" "$base" "$report" >>"$GITHUB_STEP_SUMMARY"
	fi

	incompatible=false
	if printf '%s\n' "$report" | grep -q '^## incompatible changes'; then
		incompatible=true
	fi
	base_major=${release%%.*}
	if [ "$incompatible" = true ] && [ -n "$release" ] && [ "$base_major" -ge 1 ]; then
		summary_add FAIL "$m" "incompatible API changes since $base"
	elif [ "$status" -ne 0 ] && [ "$incompatible" = false ]; then
		summary_add FAIL "$m" "gorelease failed (exit $status)"
	else
		summary_add PASS "$m" "base $base"
	fi
done

summary_print api-compat || exit 1

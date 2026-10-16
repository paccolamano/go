#!/usr/bin/env bash
# Structural compliance (FR-028 to FR-030): directory name, no replace
# directives, README.md and CHANGELOG.md present, go directive not newer than
# the oldest supported Go version; plus a valid .module-exceptions without
# entries for modules that do not exist.
#
# Usage: scripts/structure-check.sh --all | <root>...
set -euo pipefail

# shellcheck source=lib.sh
. "$(dirname "$0")/lib.sh"

REPO_ROOT=$(repo_root)
load_versions
resolve_targets "$@"
parse_exceptions

DIR_NAME='^[a-z0-9][a-z0-9._-]*$'

for m in $TARGETS; do
	problems=""
	add() { problems="${problems:+$problems; }$1"; }
	mod="$REPO_ROOT/$m"

	printf '%s\n' "${m%%/*}" | grep -Eq "$DIR_NAME" || add "directory name does not match $DIR_NAME"
	if awk '/^[[:space:]]*replace([[:space:]]|\()/ {found = 1} END {exit !found}' "$mod/go.mod"; then
		add "replace directive"
	fi
	[ -f "$mod/README.md" ] || add "missing README.md"
	[ -f "$mod/CHANGELOG.md" ] || add "missing CHANGELOG.md"
	gov=$(awk '$1 == "go" {print $2; exit}' "$mod/go.mod")
	if [ -z "$gov" ]; then
		add "missing go directive"
	elif ! awk -v v="$gov" -v max="$GO_VERSION_OLDEST" 'BEGIN {
		split(v, a, "."); split(max, b, ".")
		exit !(a[1] + 0 < b[1] + 0 || (a[1] + 0 == b[1] + 0 && a[2] + 0 <= b[2] + 0))
	}'; then
		add "go directive $gov exceeds oldest supported Go $GO_VERSION_OLDEST"
	fi

	if [ -n "$problems" ]; then
		summary_add FAIL "$m" "$problems"
	else
		summary_add PASS "$m"
	fi
done

all=$(discover_modules)
if [ -f "$(exceptions_file)" ]; then
	while IFS= read -r m; do
		[ -n "$m" ] || continue
		printf '%s\n' "$all" | grep -qxF -- "$m" ||
			summary_add FAIL .module-exceptions "exception for unknown module '$m'"
	done <<EOF
$(awk '!/^[[:space:]]*(#|$)/ {print $1}' "$(exceptions_file)" | LC_ALL=C sort -u)
EOF
fi

summary_print structure || exit 1

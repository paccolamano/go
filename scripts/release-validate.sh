#!/usr/bin/env bash
# Validates a module release tag before the release workflow publishes it
# (FR-034, FR-035). Checks run in order and the first failure stops:
#   1. tag grammar  <dir>/vMAJOR.MINOR.PATCH[-PRERELEASE]
#   2. the directory (or <dir>/vN for N >= 2) holds a module
#   3. the module path matches the tag's major version
#   4. the tagged commit is reachable from main
#   5. <root>/CHANGELOG.md has a non-empty "## [X.Y.Z]" section
# On success it prints dir=, root=, version=, prerelease= and asset= lines and
# writes the changelog section to $NOTES_FILE.
#
# Usage: scripts/release-validate.sh <tag>
# Env:   NOTES_FILE        release notes output (default: release-notes.md)
#        RELEASE_MAIN_REF  ref the commit must be reachable from (default: origin/main)
#        MODULE_PREFIX     module path prefix (default: github.com/paccolamano/go)
set -euo pipefail

# shellcheck source=lib.sh
. "$(dirname "$0")/lib.sh"

[ $# -eq 1 ] || die 2 "usage: $0 <tag>"
tag=$1
REPO_ROOT=$(repo_root)
prefix=${MODULE_PREFIX:-github.com/paccolamano/go}
main_ref=${RELEASE_MAIN_REF:-origin/main}
notes=${NOTES_FILE:-release-notes.md}

reject() {
	die 1 "$tag: $*"
}

TAG_GRAMMAR='^([a-z0-9][a-z0-9._-]*)/v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z.-]+)?$'
printf '%s\n' "$tag" | grep -Eq "$TAG_GRAMMAR" ||
	reject "invalid tag, expected <dir>/vMAJOR.MINOR.PATCH[-PRERELEASE] (e.g. retry/v1.2.0)"

dir=${tag%%/*}
version=${tag#*/v}
major=${version%%.*}
prerelease=false
case "$version" in *-*) prerelease=true ;; esac

root=$dir
if [ "$major" -ge 2 ] && [ -f "$REPO_ROOT/$dir/v$major/go.mod" ]; then
	root="$dir/v$major"
fi
[ -f "$REPO_ROOT/$root/go.mod" ] || reject "no module in directory '$root'"

expected="$prefix/$dir"
[ "$major" -lt 2 ] || expected="$expected/v$major"
actual=$(awk '$1 == "module" {print $2; exit}' "$REPO_ROOT/$root/go.mod")
[ "$actual" = "$expected" ] ||
	reject "major version mismatch: expected module path $expected, found $actual"

commit=$(git -C "$REPO_ROOT" rev-parse --verify --quiet "$tag^{commit}") || reject "tag not found in the repository"
git -C "$REPO_ROOT" merge-base --is-ancestor "$commit" "$main_ref" ||
	reject "commit ${commit:0:12} is not on main ($main_ref)"

changelog="$REPO_ROOT/$root/CHANGELOG.md"
[ -f "$changelog" ] || reject "missing $root/CHANGELOG.md"
awk -v heading="## [$version]" '
	function is_heading(line) {
		return index(line, heading) == 1 &&
			(length(line) == length(heading) || substr(line, length(heading) + 1, 3) == " - ")
	}
	found && /^## / { exit }
	found { lines[++n] = $0 }
	!found && is_heading($0) { found = 1 }
	END {
		first = 1; last = n
		while (first <= last && lines[first] ~ /^[[:space:]]*$/) first++
		while (last >= first && lines[last] ~ /^[[:space:]]*$/) last--
		for (i = first; i <= last; i++) print lines[i]
	}
' "$changelog" >"$notes"
[ -s "$notes" ] || reject "no non-empty '## [$version]' section in $root/CHANGELOG.md"

printf 'dir=%s\nroot=%s\nversion=%s\nprerelease=%s\nasset=%s\n' \
	"$dir" "$root" "$version" "$prerelease" "$dir-v$version.cdx.json"

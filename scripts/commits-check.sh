#!/usr/bin/env bash
# Commit messages (FR-032). Pull requests are squash-merged, so the pull
# request title becomes the only commit message on main; branch commits are
# not checked. The title must follow Conventional Commits, its scope must be a
# module directory or a scope listed in .github/commit-scopes, and a pull
# request whose changes are confined to one module must use that module's
# directory as scope.
#
# Usage: scripts/commits-check.sh <base> <head>
# Env:   PR_TITLE  pull request title (required)
set -euo pipefail

# shellcheck source=lib.sh
. "$(dirname "$0")/lib.sh"

[ $# -eq 2 ] || die 2 "usage: $0 <base> <head>"
[ -n "${PR_TITLE+x}" ] || die 2 "PR_TITLE is required"
base=$1
head=$2
title=$PR_TITLE
REPO_ROOT=$(repo_root)

SUBJECT='^(feat|fix|docs|style|refactor|perf|test|build|ci|chore|revert)(\([a-z0-9][a-z0-9._-]*\))?!?: .+'
SCOPE_NAME='^[a-z0-9][a-z0-9._-]*$'

# module_dirs_at <rev> prints the top-level directories holding a module.
module_dirs_at() {
	git -C "$REPO_ROOT" ls-tree -r --name-only "$1" |
		awk -F/ '$NF == "go.mod" && (NF == 2 || (NF == 3 && $2 ~ /^v[1-9][0-9]*$/)) {print $1}'
}
modules=$({ module_dirs_at "$base" && module_dirs_at "$head"; } | LC_ALL=C sort -u)

scopes=""
scopes_file="$REPO_ROOT/.github/commit-scopes"
if [ -f "$scopes_file" ]; then
	scopes=$(awk '!/^[[:space:]]*(#|$)/ {print $1}' "$scopes_file")
fi
for s in $scopes; do
	printf '%s\n' "$s" | grep -Eq "$SCOPE_NAME" || die 2 ".github/commit-scopes: invalid scope '$s' (expected $SCOPE_NAME)"
	! printf '%s\n' "$modules" | grep -qxF -- "$s" ||
		die 2 ".github/commit-scopes: scope '$s' collides with module directory '$s'"
done

# confined_module prints the module directory when every file changed by the
# pull request belongs to that single module.
confined_module() {
	git -C "$REPO_ROOT" diff --name-only "$base...$head" |
		awk -v modules="$(printf '%s\n' "$modules" | tr '\n' ' ')" '
			BEGIN { n = split(modules, list, " "); for (i = 1; i <= n; i++) mod[list[i]] = 1 }
			{
				top = $0
				sub(/\/.*/, "", top)
				owner = (top != $0 && top in mod) ? top : "(repository)"
				seen[owner] = 1
			}
			END {
				for (o in seen) { count++; last = o }
				if (count == 1 && last != "(repository)") print last
			}
		'
}

# title_problem prints why the title is invalid, or nothing.
title_problem() {
	local scope module
	case "$title" in
	'Revert "'*'"') return 0 ;;
	esac
	if ! printf '%s\n' "$title" | grep -Eq "$SUBJECT"; then
		echo "not a Conventional Commit subject, expected '<type>(<scope>)!: <description>'"
		return 0
	fi
	scope=$(printf '%s\n' "$title" | sed -n 's/^[a-z]*(\([^)]*\)).*/\1/p')
	if [ -n "$scope" ] &&
		! printf '%s\n' "$modules" | grep -qxF -- "$scope" &&
		! printf '%s\n' "$scopes" | grep -qxF -- "$scope"; then
		echo "unknown scope '$scope' (use a module directory or a scope from .github/commit-scopes)"
		return 0
	fi
	module=$(confined_module)
	if [ -n "$module" ] && [ "$scope" != "$module" ]; then
		echo "the pull request changes only module '$module', so it must use scope '$module'"
	fi
}

problem=$(title_problem)
[ -z "$problem" ] || die 1 "PR title '$title': $problem"
log "PR title '$title' follows Conventional Commits (it becomes the squash commit message)"

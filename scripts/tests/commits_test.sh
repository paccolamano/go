#!/usr/bin/env bash
# Tests for scripts/commits-check.sh (FR-032): pull requests are squash-merged,
# so only the pull request title (the squash commit message) is checked.
# shellcheck source=helpers.sh
. "$(dirname "${BASH_SOURCE[0]}")/helpers.sh"

setup_commits() {
	REPO=$(make_repo)
	write_versions "$REPO"
	mkdir -p "$REPO/.github"
	printf '# repository scopes\nci\n' >"$REPO/.github/commit-scopes"
	make_module "$REPO" alpha github.com/paccolamano/go/alpha
	make_module "$REPO" alpha/v2 github.com/paccolamano/go/alpha/v2
	make_module "$REPO" beta github.com/paccolamano/go/beta
	commit_all "$REPO" "chore: add fixtures"
	BASE=$(git -C "$REPO" rev-parse HEAD)
	git -C "$REPO" checkout -q -b feature
	cd "$REPO"
}

# commit_change <subject> <file...> modifies the files and commits them.
commit_change() {
	local subject=$1 f
	shift
	for f in "$@"; do
		mkdir -p "$(dirname "$REPO/$f")"
		printf 'changed\n' >>"$REPO/$f"
	done
	commit_all "$REPO" "$subject"
}

# check <title> runs the check for the feature branch with that PR title.
check() {
	PR_TITLE=$1 "$SCRIPTS_DIR/commits-check.sh" "$BASE" HEAD
}

test_module_scoped_title_passes_and_branch_commits_are_not_checked() {
	setup_commits
	commit_change "wip" alpha/x.go
	commit_change "fixup! wip" alpha/v2/x.go
	assert_exit 0 check "feat(alpha): add x"
}

test_breaking_change_markers_pass() {
	setup_commits
	commit_change "x" alpha/x.go
	assert_exit 0 check "feat(alpha)!: break the API"
	git -C "$REPO" checkout -q -B feature "$BASE"
	commit_change "x" Makefile
	assert_exit 0 check "feat!: drop the old layout"
}

test_changes_outside_modules_use_no_scope_or_a_repository_scope() {
	setup_commits
	commit_change "x" Makefile
	assert_exit 0 check "ci: tweak the workflow"
	assert_exit 0 check "chore: tidy up"
}

test_pull_request_touching_several_modules_may_use_any_of_them() {
	setup_commits
	commit_change "x" alpha/x.go beta/x.go
	assert_exit 0 check "refactor(alpha): share code with beta"
}

test_git_revert_title_is_accepted() {
	setup_commits
	commit_change "x" alpha/x.go
	assert_exit 0 check 'Revert "feat(alpha): add x"'
}

test_non_conventional_title_fails() {
	setup_commits
	commit_change "x" alpha/x.go
	assert_exit 1 check "fixed stuff"
	assert_contains "$OUT" "PR title 'fixed stuff'"
	assert_contains "$OUT" "not a Conventional Commit subject"
}

test_unknown_scope_fails() {
	setup_commits
	commit_change "x" Makefile
	assert_exit 1 check "feat(unknown): x"
	assert_contains "$OUT" "unknown scope 'unknown'"
}

test_pull_request_confined_to_one_module_must_use_its_scope() {
	setup_commits
	commit_change "x" alpha/x.go
	assert_exit 1 check "ci: change alpha"
	assert_contains "$OUT" "must use scope 'alpha'"
	git -C "$REPO" checkout -q -B feature "$BASE"
	commit_change "x" alpha/v2/x.go
	assert_exit 1 check "feat: change alpha"
	assert_contains "$OUT" "must use scope 'alpha'"
}

test_missing_title_exits_2() {
	setup_commits
	commit_change "x" alpha/x.go
	unset PR_TITLE
	assert_exit 2 "$SCRIPTS_DIR/commits-check.sh" "$BASE" HEAD
	assert_contains "$OUT" "PR_TITLE"
}

test_repository_scope_colliding_with_module_exits_2() {
	setup_commits
	printf 'alpha\n' >>"$REPO/.github/commit-scopes"
	commit_change "x" alpha/x.go
	assert_exit 2 check "feat(alpha): add x"
	assert_contains "$OUT" "collides with module directory 'alpha'"
}

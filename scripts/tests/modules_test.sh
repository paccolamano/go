#!/usr/bin/env bash
# Tests for scripts/modules.sh (list, touched, json-go-versions).
# shellcheck source=helpers.sh
. "$(dirname "${BASH_SOURCE[0]}")/helpers.sh"

setup_modules() {
	REPO=$(make_repo)
	write_versions "$REPO"
	make_module "$REPO" alpha github.com/paccolamano/go/alpha
	make_module "$REPO" alpha/v2 github.com/paccolamano/go/alpha/v2
	make_module "$REPO" beta github.com/paccolamano/go/beta
	commit_all "$REPO" "chore: add fixtures"
	BASE=$(git -C "$REPO" rev-parse HEAD)
	git -C "$REPO" checkout -q -b feature
	cd "$REPO"
}

# change <file...> appends a line to each file (creating it) and commits.
change() {
	local f
	for f in "$@"; do
		mkdir -p "$(dirname "$REPO/$f")"
		printf 'changed\n' >>"$REPO/$f"
	done
	commit_all "$REPO" "chore: change $*"
}

touched() {
	"$SCRIPTS_DIR/modules.sh" touched "$BASE" HEAD
}

test_list_prints_every_module() {
	setup_modules
	out=$("$SCRIPTS_DIR/modules.sh" list)
	assert_eq "$(printf 'alpha\nalpha/v2\nbeta')" "$out"
}

test_json_go_versions_reads_versions_env() {
	setup_modules
	out=$("$SCRIPTS_DIR/modules.sh" json-go-versions)
	assert_eq '["1.26","1.27"]' "$out"
}

test_file_in_module_maps_to_that_module() {
	setup_modules
	change alpha/doc.go
	out=$(touched)
	assert_eq "alpha" "$out"
}

test_file_in_major_subdirectory_maps_to_that_module_only() {
	setup_modules
	change alpha/v2/doc.go
	out=$(touched)
	assert_eq "alpha/v2" "$out"
}

test_root_documentation_touches_nothing() {
	setup_modules
	change README.md
	out=$(touched)
	assert_eq "" "$out"
}

test_shared_tooling_touches_every_module() {
	setup_modules
	all=$(printf 'alpha\nalpha/v2\nbeta')
	for f in .golangci.yml versions.env Makefile scripts/lib.sh .github/workflows/pr.yml \
		.github/commit-scopes .module-exceptions .trivyignore .gitleaksignore .semgrepignore \
		bearer.yml bearer.ignore; do
		git -C "$REPO" checkout -q -B feature "$BASE"
		change "$f"
		out=$(touched)
		assert_eq "$all" "$out" "shared tooling file $f"
	done
}

test_workspace_file_does_not_touch_every_module() {
	setup_modules
	change go.work
	out=$(touched)
	assert_eq "" "$out"
}

test_commits_on_main_after_branch_point_are_ignored() {
	setup_modules
	change alpha/doc.go
	git -C "$REPO" checkout -q main
	change beta/doc.go
	main_tip=$(git -C "$REPO" rev-parse HEAD)
	git -C "$REPO" checkout -q feature
	out=$("$SCRIPTS_DIR/modules.sh" touched "$main_tip" HEAD)
	assert_eq "alpha" "$out"
}

test_deleted_module_is_not_listed() {
	setup_modules
	git -C "$REPO" rm -q -r beta
	commit_all "$REPO" "chore: remove beta"
	out=$(touched)
	assert_eq "" "$out"
}

#!/usr/bin/env bash
# Tests for scripts/changelog-check.sh (FR-033).
# shellcheck source=helpers.sh
. "$(dirname "${BASH_SOURCE[0]}")/helpers.sh"

setup_changelog() {
	REPO=$(make_repo)
	write_versions "$REPO"
	make_module "$REPO" alpha github.com/paccolamano/go/alpha
	make_module "$REPO" beta github.com/paccolamano/go/beta
	commit_all "$REPO" "chore: add fixtures"
	BASE=$(git -C "$REPO" rev-parse HEAD)
	git -C "$REPO" checkout -q -b feature
	unset CHANGELOG_WAIVED
	cd "$REPO"
}

change() {
	local f
	for f in "$@"; do
		mkdir -p "$(dirname "$REPO/$f")"
		printf '// changed\n' >>"$REPO/$f"
	done
	commit_all "$REPO" "chore: change $*"
}

check() {
	"$SCRIPTS_DIR/changelog-check.sh" "$BASE" HEAD
}

test_source_change_without_changelog_fails() {
	setup_changelog
	change alpha/doc.go
	assert_exit 1 check
	assert_contains "$OUT" "FAIL  alpha  CHANGELOG.md not updated"
}

test_source_change_with_changelog_passes() {
	setup_changelog
	change alpha/doc.go alpha/CHANGELOG.md
	assert_exit 0 check
	assert_contains "$OUT" "PASS  alpha"
}

test_tests_testdata_and_docs_do_not_require_changelog() {
	setup_changelog
	change alpha/hello_test.go alpha/testdata/input.go alpha/README.md
	assert_exit 0 check
	assert_not_contains "$OUT" "FAIL"
}

test_module_definition_change_requires_changelog() {
	setup_changelog
	change alpha/go.mod
	assert_exit 1 check
	assert_contains "$OUT" "FAIL  alpha"
}

test_waiver_label_turns_failure_into_pass() {
	setup_changelog
	change alpha/doc.go
	CHANGELOG_WAIVED=true assert_exit 0 check
	assert_contains "$OUT" "PASS  alpha  waived by label"
}

test_each_module_is_evaluated_separately() {
	setup_changelog
	change alpha/doc.go alpha/CHANGELOG.md beta/doc.go
	assert_exit 1 check
	assert_contains "$OUT" "PASS  alpha"
	assert_contains "$OUT" "FAIL  beta  CHANGELOG.md not updated"
}

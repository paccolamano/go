#!/usr/bin/env bash
# Tests for scripts/structure-check.sh (FR-028 to FR-030, exceptions file).
# shellcheck source=helpers.sh
. "$(dirname "${BASH_SOURCE[0]}")/helpers.sh"

setup_structure() {
	REPO=$(make_repo)
	write_versions "$REPO"
	make_module "$REPO" alpha github.com/paccolamano/go/alpha
	make_module "$REPO" beta github.com/paccolamano/go/beta
	cd "$REPO"
}

check() {
	"$SCRIPTS_DIR/structure-check.sh" --all
}

test_compliant_modules_pass() {
	setup_structure
	assert_exit 0 check
	assert_contains "$OUT" "PASS  alpha"
	assert_contains "$OUT" "PASS  beta"
}

test_no_modules_passes() {
	REPO=$(make_repo)
	write_versions "$REPO"
	cd "$REPO"
	assert_exit 0 check
	assert_contains "$OUT" "no modules found"
}

test_single_line_replace_fails() {
	setup_structure
	printf '\nreplace example.com/x => ../x\n' >>"$REPO/alpha/go.mod"
	assert_exit 1 check
	assert_contains "$OUT" "FAIL  alpha  replace directive"
	assert_contains "$OUT" "PASS  beta"
}

test_block_replace_fails() {
	setup_structure
	printf '\nreplace (\n\texample.com/x => ../x\n)\n' >>"$REPO/beta/go.mod"
	assert_exit 1 check
	assert_contains "$OUT" "FAIL  beta  replace directive"
}

test_missing_readme_fails() {
	setup_structure
	rm "$REPO/alpha/README.md"
	assert_exit 1 check
	assert_contains "$OUT" "FAIL  alpha  missing README.md"
}

test_missing_changelog_fails() {
	setup_structure
	rm "$REPO/alpha/CHANGELOG.md"
	assert_exit 1 check
	assert_contains "$OUT" "FAIL  alpha  missing CHANGELOG.md"
}

test_go_directive_newer_than_oldest_supported_fails() {
	setup_structure
	make_module "$REPO" gamma github.com/paccolamano/go/gamma 1.27.0
	assert_exit 1 check
	assert_contains "$OUT" "FAIL  gamma  go directive 1.27.0 exceeds oldest supported Go 1.26"
}

test_patch_release_of_oldest_supported_go_passes() {
	setup_structure
	make_module "$REPO" gamma github.com/paccolamano/go/gamma 1.26.3
	assert_exit 0 check
	assert_contains "$OUT" "PASS  gamma"
}

test_invalid_directory_name_fails() {
	setup_structure
	make_module "$REPO" Bad github.com/paccolamano/go/Bad
	assert_exit 1 check
	assert_contains "$OUT" "FAIL  Bad  directory name does not match ^[a-z0-9][a-z0-9._-]*\$"
}

test_exception_for_unknown_module_fails() {
	setup_structure
	printf 'ghost coverage 50 left over from a removed module\n' >"$REPO/.module-exceptions"
	assert_exit 1 check
	assert_contains "$OUT" "FAIL  .module-exceptions  exception for unknown module 'ghost'"
}

test_malformed_exception_exits_2() {
	setup_structure
	printf 'alpha coverage 50\n' >"$REPO/.module-exceptions"
	assert_exit 2 check
	assert_contains "$OUT" "missing justification"
}

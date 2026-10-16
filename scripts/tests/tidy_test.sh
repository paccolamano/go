#!/usr/bin/env bash
# Tests for scripts/tidy-check.sh (FR-016), using a go shim on PATH.
# shellcheck disable=SC2016 # shim bodies and expected output are literal text
# shellcheck source=helpers.sh
. "$(dirname "${BASH_SOURCE[0]}")/helpers.sh"

setup_tidy() {
	REPO=$(make_repo)
	write_versions "$REPO"
	make_module "$REPO" alpha github.com/paccolamano/go/alpha
	make_module "$REPO" beta github.com/paccolamano/go/beta
	make_shim "$TEST_TMP/shims" go '
if [ "$*" = "mod tidy -diff" ] && [ "${PWD##*/}" = "${SHIM_UNTIDY:-none}" ] && [ "$GOWORK" = off ]; then
	printf -- "--- go.mod\n+++ go.mod\n+require example.com/x v1.0.0\n"
	exit 1
fi'
	PATH="$TEST_TMP/shims:$PATH"
	cd "$REPO"
}

test_tidy_modules_pass() {
	setup_tidy
	assert_exit 0 "$SCRIPTS_DIR/tidy-check.sh" --all
	assert_contains "$OUT" "PASS  alpha"
	assert_contains "$OUT" "PASS  beta"
}

test_untidy_module_fails_with_diff_and_others_are_checked() {
	setup_tidy
	SHIM_UNTIDY=alpha assert_exit 1 "$SCRIPTS_DIR/tidy-check.sh" --all
	assert_contains "$OUT" "+require example.com/x v1.0.0"
	assert_contains "$OUT" "FAIL  alpha  go mod tidy reports changes"
	assert_contains "$OUT" "PASS  beta"
}

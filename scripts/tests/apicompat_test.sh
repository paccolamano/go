#!/usr/bin/env bash
# Tests for scripts/apicompat.sh, using a gorelease shim selected via
# $GORELEASE that logs its arguments and prints a canned report.
# shellcheck disable=SC2016 # shim bodies and expected output are literal text
# shellcheck source=helpers.sh
. "$(dirname "${BASH_SOURCE[0]}")/helpers.sh"

setup_apicompat() {
	REPO=$(make_repo)
	write_versions "$REPO"
	make_module "$REPO" alpha github.com/paccolamano/go/alpha
	make_module "$REPO" gamma/v2 github.com/paccolamano/go/gamma/v2
	commit_all "$REPO" "chore: add fixtures"
	GORELEASE_LOG="$TEST_TMP/gorelease.log"
	: >"$GORELEASE_LOG"
	export GORELEASE_LOG
	make_shim "$TEST_TMP/shims" gorelease '
printf "%s|%s\n" "${PWD##*/}" "$*" >>"$GORELEASE_LOG"
if [ -n "${SHIM_INCOMPATIBLE:-}" ]; then
	printf "# example\n## incompatible changes\nHello: removed\n\n# summary\nSuggested version: v2.0.0\n"
else
	printf "# example\n## compatible changes\nBye: added\n\n# summary\nSuggested version: v1.1.0\n"
fi'
	GORELEASE="$TEST_TMP/shims/gorelease"
	export GORELEASE
	cd "$REPO"
}

tag() {
	git -C "$REPO" tag "$1"
}

test_module_without_tags_uses_base_none_and_passes() {
	setup_apicompat
	assert_exit 0 "$SCRIPTS_DIR/apicompat.sh" alpha
	assert_eq "alpha|-base=none" "$(cat "$GORELEASE_LOG")"
	assert_contains "$OUT" "PASS  alpha"
}

test_base_is_highest_release_in_semver_order() {
	setup_apicompat
	tag alpha/v1.2.0
	tag alpha/v1.10.0
	tag alpha/v1.11.0-rc.1
	tag alpha/v0.9.0
	assert_exit 0 "$SCRIPTS_DIR/apicompat.sh" alpha
	assert_eq "alpha|-base=v1.10.0" "$(cat "$GORELEASE_LOG")"
}

test_major_suffix_selects_only_same_major_tags() {
	setup_apicompat
	tag gamma/v1.9.0
	tag gamma/v2.0.1
	tag gamma/v3.0.0
	assert_exit 0 "$SCRIPTS_DIR/apicompat.sh" gamma/v2
	assert_eq "v2|-base=v2.0.1" "$(cat "$GORELEASE_LOG")"
}

test_incompatible_changes_fail_from_v1() {
	setup_apicompat
	tag alpha/v1.0.0
	SHIM_INCOMPATIBLE=1 assert_exit 1 "$SCRIPTS_DIR/apicompat.sh" alpha
	assert_contains "$OUT" "FAIL  alpha  incompatible API changes since v1.0.0"
	assert_contains "$OUT" "Hello: removed"
}

test_incompatible_changes_are_reported_but_allowed_on_v0() {
	setup_apicompat
	tag alpha/v0.3.0
	SHIM_INCOMPATIBLE=1 assert_exit 0 "$SCRIPTS_DIR/apicompat.sh" alpha
	assert_contains "$OUT" "Hello: removed"
	assert_contains "$OUT" "PASS  alpha"
}

test_report_goes_to_the_job_summary_in_ci() {
	setup_apicompat
	summary="$TEST_TMP/summary.md"
	GITHUB_ACTIONS=true GITHUB_STEP_SUMMARY="$summary" assert_exit 0 "$SCRIPTS_DIR/apicompat.sh" alpha
	assert_contains "$(cat "$summary")" "Suggested version: v1.1.0"
}

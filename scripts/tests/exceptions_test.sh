#!/usr/bin/env bash
# Tests for the .module-exceptions parser in scripts/lib.sh.
# shellcheck source=helpers.sh
. "$(dirname "${BASH_SOURCE[0]}")/helpers.sh"

setup_exceptions() {
	REPO_ROOT=$(tmpdir)
	export REPO_ROOT
	# shellcheck source=../lib.sh
	. "$SCRIPTS_DIR/lib.sh"
	printf '%s\n' "$@" >"$REPO_ROOT/.module-exceptions"
}

test_valid_file_parses_and_values_are_returned() {
	setup_exceptions \
		"# module kind value justification" \
		"" \
		"retry     coverage   70    generated code is fuzzed only" \
		"fsnotify  platforms  linux/amd64,linux/arm64  relies on inotify"
	parse_exceptions
	assert_eq "70" "$(exception_value retry coverage)"
	assert_eq "linux/amd64,linux/arm64" "$(exception_value fsnotify platforms)"
	assert_eq "" "$(exception_value retry platforms)"
	assert_eq "generated code is fuzzed only" "$(exception_justification retry coverage)"
}

test_coverage_threshold_defaults_to_80() {
	setup_exceptions "retry coverage 55 reason"
	assert_eq "55" "$(coverage_threshold retry)"
	assert_eq "80" "$(coverage_threshold other)"
}

test_missing_file_is_valid_and_empty() {
	setup_exceptions ""
	rm "$REPO_ROOT/.module-exceptions"
	parse_exceptions
	assert_eq "80" "$(coverage_threshold retry)"
}

test_missing_justification_is_rejected() {
	setup_exceptions "# header" "retry coverage 70"
	assert_exit 2 parse_exceptions
	assert_contains "$OUT" ".module-exceptions:2:"
	assert_contains "$OUT" "justification"
	assert_contains "$OUT" "expected: <module> <coverage|platforms> <value> <justification>"
}

test_unknown_kind_is_rejected() {
	setup_exceptions "retry speed 10 because"
	assert_exit 2 parse_exceptions
	assert_contains "$OUT" "unknown kind 'speed'"
}

test_coverage_outside_0_79_is_rejected() {
	setup_exceptions "retry coverage 80 too high"
	assert_exit 2 parse_exceptions
	assert_contains "$OUT" "integer 0-79"
	setup_exceptions "retry coverage abc not a number"
	assert_exit 2 parse_exceptions
	assert_contains "$OUT" "integer 0-79"
}

test_unsupported_platform_is_rejected() {
	setup_exceptions "retry platforms linux/amd64,plan9/386 exotic"
	assert_exit 2 parse_exceptions
	assert_contains "$OUT" "invalid platform 'plan9/386'"
}

test_duplicate_module_kind_is_rejected() {
	setup_exceptions "retry coverage 70 first" "retry coverage 60 second"
	assert_exit 2 parse_exceptions
	assert_contains "$OUT" ".module-exceptions:2:"
	assert_contains "$OUT" "duplicate entry for retry coverage"
}

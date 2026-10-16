#!/usr/bin/env bash
# Tests for module discovery and target resolution in scripts/lib.sh.
# shellcheck source=helpers.sh
. "$(dirname "${BASH_SOURCE[0]}")/helpers.sh"

setup_lib() {
	REPO_ROOT=$(tmpdir)
	export REPO_ROOT
	# shellcheck source=../lib.sh
	. "$SCRIPTS_DIR/lib.sh"
}

add_mod() {
	mkdir -p "$REPO_ROOT/$1"
	: >"$REPO_ROOT/$1/go.mod"
}

test_discover_lists_top_level_and_major_subdirectories_sorted() {
	setup_lib
	add_mod beta
	add_mod alpha
	add_mod alpha/v2
	add_mod gamma/v10
	assert_eq "$(printf 'alpha\nalpha/v2\nbeta\ngamma/v10')" "$(discover_modules)"
}

test_discover_ignores_hidden_nested_and_invalid_major_directories() {
	setup_lib
	add_mod alpha
	add_mod alpha/v1
	add_mod alpha/v02
	add_mod alpha/vx
	add_mod alpha/testdata/x
	add_mod alpha/sub
	add_mod .hidden
	mkdir -p "$REPO_ROOT/notamodule"
	assert_eq "alpha" "$(discover_modules)"
}

test_discover_with_no_modules_prints_nothing() {
	setup_lib
	assert_eq "" "$(discover_modules)"
}

test_resolve_all_returns_every_module() {
	setup_lib
	add_mod beta
	add_mod alpha
	resolve_targets --all >/dev/null
	assert_eq "alpha beta" "$TARGETS"
}

test_resolve_all_without_modules_says_so() {
	setup_lib
	out=$(resolve_targets --all)
	assert_contains "$out" "no modules found"
}

test_resolve_explicit_list_returns_exactly_those_modules() {
	setup_lib
	add_mod alpha
	add_mod alpha/v2
	add_mod beta
	resolve_targets alpha/v2 beta/
	assert_eq "alpha/v2 beta" "$TARGETS"
}

test_resolve_without_arguments_has_nothing_to_do() {
	setup_lib
	add_mod alpha
	out=$(resolve_targets)
	assert_contains "$out" "no modules to check"
	resolve_targets >/dev/null
	assert_eq "" "$TARGETS"
}

test_resolve_unknown_module_exits_2_and_lists_discovered_modules() {
	setup_lib
	add_mod alpha
	add_mod beta
	assert_exit 2 resolve_targets nope
	assert_contains "$OUT" "unknown module 'nope'"
	assert_contains "$OUT" "alpha beta"
}

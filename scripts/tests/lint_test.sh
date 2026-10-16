#!/usr/bin/env bash
# Tests for scripts/lint.sh, using a golangci-lint shim selected via
# $GOLANGCI_LINT that records its working directory, GOWORK and arguments.
# shellcheck disable=SC2016 # shim bodies and expected output are literal text
# shellcheck source=helpers.sh
. "$(dirname "${BASH_SOURCE[0]}")/helpers.sh"

setup_lint() {
	REPO=$(make_repo)
	write_versions "$REPO"
	: >"$REPO/.golangci.yml"
	make_module "$REPO" alpha github.com/paccolamano/go/alpha
	make_module "$REPO" beta github.com/paccolamano/go/beta
	REPO_REAL=$(cd "$REPO" && pwd -P)
	LINT_LOG="$TEST_TMP/lint.log"
	: >"$LINT_LOG"
	export LINT_LOG
	make_shim "$TEST_TMP/shims" golangci-lint '
if [ "$*" = "version --short" ]; then echo "${SHIM_VERSION:-2.14.0}"; exit 0; fi
printf "%s|%s|%s\n" "$(pwd -P)" "${GOWORK:-}" "$*" >>"$LINT_LOG"
case "$(pwd -P)" in */"${SHIM_FAIL:-none}") echo "x.go:1:1: lint finding"; exit 1 ;; esac'
	GOLANGCI_LINT="$TEST_TMP/shims/golangci-lint"
	export GOLANGCI_LINT
	cd "$REPO"
}

test_wrong_version_exits_2_with_install_command() {
	setup_lint
	SHIM_VERSION=2.1.0 assert_exit 2 "$SCRIPTS_DIR/lint.sh" --all
	assert_contains "$OUT" "expected 2.14.0"
	assert_contains "$OUT" "found 2.1.0"
	assert_contains "$OUT" 'curl -sSfL https://golangci-lint.run/install.sh | sh -s -- -b "$(go env GOPATH)/bin" v2.14.0'
}

test_missing_binary_exits_2() {
	setup_lint
	GOLANGCI_LINT="$TEST_TMP/shims/does-not-exist" assert_exit 2 "$SCRIPTS_DIR/lint.sh" --all
	assert_contains "$OUT" "golangci-lint not found"
}

test_runs_from_each_module_root_with_workspace_disabled() {
	setup_lint
	assert_exit 0 "$SCRIPTS_DIR/lint.sh" --all
	expected="$REPO_REAL/alpha|off|run --config $REPO_REAL/.golangci.yml ./...
$REPO_REAL/beta|off|run --config $REPO_REAL/.golangci.yml ./..."
	assert_eq "$expected" "$(cat "$LINT_LOG")"
	assert_contains "$OUT" "PASS  alpha"
	assert_contains "$OUT" "PASS  beta"
}

test_single_module_is_linted_alone() {
	setup_lint
	assert_exit 0 "$SCRIPTS_DIR/lint.sh" beta
	assert_eq "1" "$(wc -l <"$LINT_LOG" | tr -d ' ')"
	assert_contains "$(cat "$LINT_LOG")" "/beta|"
}

test_failure_keeps_going_and_is_summarized() {
	setup_lint
	SHIM_FAIL=alpha assert_exit 1 "$SCRIPTS_DIR/lint.sh" --all
	assert_contains "$OUT" "x.go:1:1: lint finding"
	assert_contains "$OUT" "FAIL  alpha"
	assert_contains "$OUT" "PASS  beta"
}

test_no_arguments_checks_nothing() {
	setup_lint
	assert_exit 0 "$SCRIPTS_DIR/lint.sh"
	assert_contains "$OUT" "no modules to check"
	assert_eq "" "$(cat "$LINT_LOG")"
}

#!/usr/bin/env bash
# Tests for scripts/test.sh, using the real Go toolchain on fixture modules.
# shellcheck source=helpers.sh
. "$(dirname "${BASH_SOURCE[0]}")/helpers.sh"

setup_gotest() {
	REPO=$(make_repo)
	write_versions "$REPO"
	make_module "$REPO" alpha github.com/paccolamano/go/alpha
	make_module "$REPO" beta github.com/paccolamano/go/beta
	cd "$REPO"
}

# add_untested <root> adds an exported function with three uncovered
# statements, dropping the module to 25% coverage.
add_untested() {
	cat >"$REPO/$1/extra.go" <<'EOF'
package alpha

// Sum adds two fixed numbers.
func Sum() int {
	a := 1
	b := 2
	return a + b
}
EOF
}

test_fully_covered_modules_pass() {
	setup_gotest
	assert_exit 0 "$SCRIPTS_DIR/test.sh" --all
	assert_contains "$OUT" "PASS  alpha  coverage 100.0%"
	assert_contains "$OUT" "PASS  beta  coverage 100.0%"
}

test_coverage_below_threshold_fails_without_stopping() {
	setup_gotest
	add_untested alpha
	assert_exit 1 "$SCRIPTS_DIR/test.sh" --all
	assert_contains "$OUT" "FAIL  alpha  coverage 25.0% < 80% threshold"
	assert_contains "$OUT" "PASS  beta"
}

test_declared_exception_lowers_the_threshold() {
	setup_gotest
	add_untested alpha
	printf 'alpha coverage 20 fixture with intentionally untested code\n' >"$REPO/.module-exceptions"
	assert_exit 0 "$SCRIPTS_DIR/test.sh" alpha
	assert_contains "$OUT" "PASS  alpha  coverage 25.0%"
	assert_contains "$OUT" "threshold 20"
	assert_contains "$OUT" "fixture with intentionally untested code"
}

test_module_without_statements_passes() {
	setup_gotest
	mkdir -p "$REPO/empty"
	printf 'module github.com/paccolamano/go/empty\n\ngo 1.26.0\n' >"$REPO/empty/go.mod"
	printf '// Package empty has no statements.\npackage empty\n' >"$REPO/empty/doc.go"
	assert_exit 0 "$SCRIPTS_DIR/test.sh" empty
	assert_contains "$OUT" "PASS  empty  no statements"
}

test_failing_tests_fail_the_module() {
	setup_gotest
	printf 'package alpha\n\nimport "testing"\n\nfunc TestBroken(t *testing.T) { t.Fatal("broken") }\n' >"$REPO/alpha/broken_test.go"
	assert_exit 1 "$SCRIPTS_DIR/test.sh" --all
	assert_contains "$OUT" "FAIL  alpha  tests failed"
	assert_contains "$OUT" "PASS  beta"
}

test_working_tree_stays_clean() {
	setup_gotest
	commit_all "$REPO" "feat(alpha): add fixtures"
	assert_exit 0 "$SCRIPTS_DIR/test.sh" --all
	assert_eq "" "$(git -C "$REPO" status --porcelain)"
}

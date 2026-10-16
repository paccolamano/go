#!/usr/bin/env bash
# Tests for scripts/work-sync.sh, using the real Go toolchain.
# shellcheck source=helpers.sh
. "$(dirname "${BASH_SOURCE[0]}")/helpers.sh"

setup_work() {
	REPO=$(make_repo)
	write_versions "$REPO"
	unset GOWORK
	cd "$REPO"
}

# work_lines prints go.work without blank lines.
work_lines() {
	grep -v '^[[:space:]]*$' "$REPO/go.work"
}

test_zero_modules_writes_only_the_go_line() {
	setup_work
	assert_exit 0 "$SCRIPTS_DIR/work-sync.sh"
	out=$(work_lines)
	assert_eq "go 1.26.0" "$out"
}

test_modules_are_listed_sorted_without_toolchain() {
	setup_work
	make_module "$REPO" beta github.com/paccolamano/go/beta
	make_module "$REPO" alpha github.com/paccolamano/go/alpha
	make_module "$REPO" alpha/v2 github.com/paccolamano/go/alpha/v2
	assert_exit 0 "$SCRIPTS_DIR/work-sync.sh"
	out=$(work_lines)
	assert_eq "$(printf 'go 1.26.0\nuse (\n\t./alpha\n\t./alpha/v2\n\t./beta\n)')" "$out"
	assert_not_contains "$out" "toolchain"
}

test_removed_module_is_dropped() {
	setup_work
	make_module "$REPO" alpha github.com/paccolamano/go/alpha
	make_module "$REPO" beta github.com/paccolamano/go/beta
	"$SCRIPTS_DIR/work-sync.sh" >/dev/null
	rm -r "$REPO/beta"
	assert_exit 0 "$SCRIPTS_DIR/work-sync.sh"
	assert_not_contains "$(cat "$REPO/go.work")" "beta"
	assert_contains "$(cat "$REPO/go.work")" "./alpha"
}

test_existing_checksum_file_is_kept() {
	setup_work
	printf 'example.com/x v1.0.0 h1:abc=\n' >"$REPO/go.work.sum"
	assert_exit 0 "$SCRIPTS_DIR/work-sync.sh"
	assert_eq "example.com/x v1.0.0 h1:abc=" "$(cat "$REPO/go.work.sum")"
}

test_module_requiring_newer_go_fails() {
	setup_work
	make_module "$REPO" gamma github.com/paccolamano/go/gamma 1.27.0
	assert_exit 1 "$SCRIPTS_DIR/work-sync.sh"
	assert_contains "$OUT" "above the oldest supported Go 1.26"
}

#!/usr/bin/env bash
# Tests for scripts/cross-build.sh (FR-031), using a go shim on PATH that logs
# GOOS/GOARCH/CGO_ENABLED and the subcommand.
# shellcheck disable=SC2016 # shim bodies and expected output are literal text
# shellcheck source=helpers.sh
. "$(dirname "${BASH_SOURCE[0]}")/helpers.sh"

setup_cross() {
	REPO=$(make_repo)
	write_versions "$REPO"
	make_module "$REPO" alpha github.com/paccolamano/go/alpha
	make_module "$REPO" beta github.com/paccolamano/go/beta
	GO_LOG="$TEST_TMP/go.log"
	: >"$GO_LOG"
	export GO_LOG
	make_shim "$TEST_TMP/shims" go '
printf "%s|%s/%s|%s|%s\n" "${PWD##*/}" "$GOOS" "$GOARCH" "$CGO_ENABLED" "$1" >>"$GO_LOG"
if [ "$GOOS/$GOARCH" = "${SHIM_FAIL_PLATFORM:-none}" ] && [ "$1" = build ]; then
	echo "x.go:1:1: undefined: syscall.Mmap"
	exit 1
fi'
	PATH="$TEST_TMP/shims:$PATH"
	cd "$REPO"
}

test_builds_and_vets_all_six_platforms_without_cgo() {
	setup_cross
	assert_exit 0 "$SCRIPTS_DIR/cross-build.sh" alpha
	expected=""
	for p in linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64 windows/arm64; do
		expected="${expected}alpha|$p|0|build"$'\n'"alpha|$p|0|vet"$'\n'
	done
	assert_eq "${expected%$'\n'}" "$(cat "$GO_LOG")"
}

test_platform_exception_limits_the_platforms() {
	setup_cross
	printf 'alpha platforms linux/amd64,linux/arm64 relies on inotify\n' >"$REPO/.module-exceptions"
	assert_exit 0 "$SCRIPTS_DIR/cross-build.sh" alpha
	assert_eq "4" "$(wc -l <"$GO_LOG" | tr -d ' ')"
	assert_not_contains "$(cat "$GO_LOG")" "windows"
}

test_platform_failure_is_named_and_other_modules_still_run() {
	setup_cross
	SHIM_FAIL_PLATFORM=windows/arm64 assert_exit 1 "$SCRIPTS_DIR/cross-build.sh" --all
	assert_contains "$OUT" "FAIL  alpha  windows/arm64"
	assert_contains "$OUT" "FAIL  beta  windows/arm64"
	assert_contains "$OUT" "undefined: syscall.Mmap"
}

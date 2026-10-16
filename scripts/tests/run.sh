#!/usr/bin/env bash
# Runs the tooling self-tests: every test_* function of scripts/tests/*_test.sh,
# each in its own subshell with a private temporary directory.
#
# Usage: scripts/tests/run.sh [name]   (name = file stem, e.g. "lib" for lib_test.sh)
set -euo pipefail

TESTS_DIR=$(cd "$(dirname "$0")" && pwd)

# Hermetic environment: ignore the user's git config, never download Go
# toolchains, and do not inherit CI variables that change script behavior.
export GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_NOSYSTEM=1 GOTOOLCHAIN=local GOFLAGS=
unset GITHUB_ACTIONS GITHUB_STEP_SUMMARY GITHUB_OUTPUT REPO_ROOT

passed=0
failed=0

for file in "$TESTS_DIR"/${1:-*}_test.sh; do
	[ -e "$file" ] || continue
	tests=$(
		# shellcheck source=/dev/null
		. "$file"
		declare -F | awk '{print $3}' | grep '^test_' || true
	)
	for t in $tests; do
		tmp=$(mktemp -d "${TMPDIR:-/tmp}/scripts-test.XXXXXX")
		mkdir "$tmp/work"
		set +e
		(
			set -eu
			TEST_TMP="$tmp/work"
			export TEST_TMP
			cd "$TEST_TMP"
			# shellcheck source=/dev/null
			. "$file"
			"$t"
		) >"$tmp/log" 2>&1
		status=$?
		set -e
		if [ "$status" -eq 0 ]; then
			printf 'PASS %s::%s\n' "${file##*/}" "$t"
			passed=$((passed + 1))
		else
			printf 'FAIL %s::%s\n' "${file##*/}" "$t"
			sed 's/^/    /' "$tmp/log"
			failed=$((failed + 1))
		fi
		rm -rf "$tmp"
	done
done

printf '\n%d passed, %d failed\n' "$passed" "$failed"
[ "$failed" -eq 0 ]

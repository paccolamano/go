#!/usr/bin/env bash
# Assertions and fixture builders for the tooling self-tests.
#
# Sourced by every scripts/tests/*_test.sh. scripts/tests/run.sh runs each test
# function in its own subshell with "set -eu" and a private $TEST_TMP that is
# removed afterwards, so fixtures never need explicit cleanup.

TESTS_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
# shellcheck disable=SC2034 # used by the test files that source this one
SCRIPTS_DIR=$(cd "$TESTS_DIR/.." && pwd)

# OUT holds the combined stdout and stderr of the last assert_exit call.
OUT=""

fail() {
	printf 'ASSERTION FAILED: %s\n' "$*" >&2
	exit 1
}

assert_eq() {
	[ "$1" = "$2" ] || fail "${3:-values differ}"$'\n'"  expected: $1"$'\n'"  actual:   $2"
}

assert_contains() {
	case "$1" in
	*"$2"*) ;;
	*) fail "${3:-missing text}: '$2' not found in:"$'\n'"$1" ;;
	esac
}

assert_not_contains() {
	case "$1" in
	*"$2"*) fail "${3:-unexpected text}: '$2' found in:"$'\n'"$1" ;;
	esac
}

# assert_exit <status> <cmd...> runs the command, stores its output in OUT and
# fails unless it exits with <status>.
assert_exit() {
	local want=$1 got
	shift
	set +e
	OUT=$("$@" 2>&1)
	got=$?
	set -e
	[ "$got" = "$want" ] || fail "exit status: expected $want, got $got for: $*"$'\n'"$OUT"
}

# tmpdir creates a directory under $TEST_TMP and prints its path.
tmpdir() {
	mktemp -d "$TEST_TMP/d.XXXXXX"
}

# make_repo creates a git repository on branch main with one commit and
# prints its path.
make_repo() {
	local d
	d=$(tmpdir)
	git -c init.defaultBranch=main init -q "$d"
	git -C "$d" config user.name "Test"
	git -C "$d" config user.email "test@example.com"
	git -C "$d" config commit.gpgsign false
	git -C "$d" config tag.gpgsign false
	printf '# fixture\n' >"$d/README.md"
	git -C "$d" add -A
	git -C "$d" commit -q -m "chore: initial commit"
	printf '%s\n' "$d"
}

# commit_all <repo> <message> stages every change and commits it.
commit_all() {
	git -C "$1" add -A
	git -C "$1" commit -q --allow-empty -m "$2"
}

# make_module <repo> <root> <module-path> [go-version] writes a compliant,
# fully covered module: go.mod, README.md, CHANGELOG.md, doc.go and a test.
make_module() {
	local repo=$1 root=$2 path=$3 gov=${4:-1.26.0} pkg
	pkg=${path##*/}
	case "$pkg" in
	v[0-9]*) pkg=${path%/*} && pkg=${pkg##*/} ;;
	esac
	pkg=$(printf '%s' "$pkg" | tr '.-' '__')
	mkdir -p "$repo/$root"
	printf 'module %s\n\ngo %s\n' "$path" "$gov" >"$repo/$root/go.mod"
	printf '# %s\n' "$root" >"$repo/$root/README.md"
	printf '# Changelog\n' >"$repo/$root/CHANGELOG.md"
	cat >"$repo/$root/doc.go" <<EOF
// Package $pkg is a test fixture.
package $pkg

// Hello returns a greeting.
func Hello() string { return "hello" }
EOF
	cat >"$repo/$root/hello_test.go" <<EOF
package $pkg

import "testing"

func TestHello(t *testing.T) {
	if Hello() != "hello" {
		t.Fatal("unexpected greeting")
	}
}
EOF
}

# make_shim <dir> <name> <body> writes an executable bash script <dir>/<name>.
make_shim() {
	mkdir -p "$1"
	printf '#!/usr/bin/env bash\n%s\n' "$3" >"$1/$2"
	chmod +x "$1/$2"
}

# write_versions <dir> writes a versions.env with the supported Go versions.
write_versions() {
	printf 'GO_VERSION_OLDEST=1.26\nGO_VERSION_NEWEST=1.27\nGOLANGCI_LINT_VERSION=v2.14.0\n' >"$1/versions.env"
}

sha256_file() {
	if command -v sha256sum >/dev/null 2>&1; then
		sha256sum "$1" | awk '{print $1}'
	else
		shasum -a 256 "$1" | awk '{print $1}'
	fi
}

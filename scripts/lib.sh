#!/usr/bin/env bash
# Shared helpers for the repository tooling scripts. Source it, do not run it.
#
# Portability: Bash 3.2 and POSIX utilities only (macOS defaults), see
# specs/001-init-repo-tooling/research.md R-02.
#
# Exit codes used by every script: 0 pass, 1 a check failed, 2 usage or
# environment error.

log() {
	printf '%s\n' "$*"
}

warn() {
	printf 'warning: %s\n' "$*" >&2
}

# die <code> <message...> prints an error and exits with <code>.
die() {
	local code=$1
	shift
	printf 'error: %s\n' "$*" >&2
	exit "$code"
}

# repo_root prints the repository root ($REPO_ROOT when set, for tests).
repo_root() {
	if [ -n "${REPO_ROOT:-}" ]; then
		printf '%s\n' "$REPO_ROOT"
		return 0
	fi
	git rev-parse --show-toplevel 2>/dev/null || die 2 "not inside a git repository"
}

# load_versions sources versions.env from the repository root.
load_versions() {
	local file="${REPO_ROOT:?}/versions.env"
	[ -f "$file" ] || die 2 "missing $file"
	# shellcheck source=../versions.env
	. "$file"
}

# is_major_dir <name> succeeds for v2, v3, ... (major-version subdirectories).
is_major_dir() {
	local n=${1#v}
	case "$1" in v*) ;; *) return 1 ;; esac
	case "$n" in '' | *[!0-9]* | 0*) return 1 ;; esac
	[ "$n" -ge 2 ]
}

# discover_modules prints every module root, sorted: each non-hidden top-level
# directory with a go.mod, plus each <dir>/vN (N >= 2) with a go.mod.
discover_modules() {
	(
		cd "${REPO_ROOT:?}" || exit 2
		for d in */; do
			d=${d%/}
			[ -d "$d" ] || continue
			[ ! -f "$d/go.mod" ] || printf '%s\n' "$d"
			for v in "$d"/v*/; do
				v=${v%/}
				[ -d "$v" ] || continue
				is_major_dir "${v##*/}" || continue
				[ ! -f "$v/go.mod" ] || printf '%s\n' "$v"
			done
		done
	) | LC_ALL=C sort
}

# resolve_targets "$@" sets TARGETS (space-separated module roots) from the
# script arguments:
#   --all       every discovered module
#   <root>...   exactly these modules (unknown roots exit 2)
#   (nothing)   no module: prints "no modules to check", TARGETS is empty
resolve_targets() {
	local all m
	TARGETS=""
	if [ $# -eq 0 ]; then
		log "no modules to check"
		return 0
	fi
	all=$(discover_modules)
	if [ "$1" = "--all" ]; then
		[ $# -eq 1 ] || die 2 "--all takes no other arguments"
		if [ -z "$all" ]; then
			log "no modules found"
			return 0
		fi
		TARGETS=$(printf '%s\n' "$all" | tr '\n' ' ')
		TARGETS=${TARGETS% }
		return 0
	fi
	for m in "$@"; do
		m=${m%/}
		if ! printf '%s\n' "$all" | grep -qxF -- "$m"; then
			die 2 "unknown module '$m'; discovered modules: $(printf '%s\n' "${all:-(none)}" | tr '\n' ' ')"
		fi
		TARGETS="${TARGETS:+$TARGETS }$m"
	done
}

# module_for_path <path> <roots> prints the module root, among the
# newline-separated <roots>, that contains the repository-relative <path>.
module_for_path() {
	local path=$1 roots=$2 top rest sub
	top=${path%%/*}
	[ "$top" != "$path" ] || return 0
	rest=${path#*/}
	sub=${rest%%/*}
	if [ "$sub" != "$rest" ] && printf '%s\n' "$roots" | grep -qxF -- "$top/$sub"; then
		printf '%s\n' "$top/$sub"
	elif printf '%s\n' "$roots" | grep -qxF -- "$top"; then
		printf '%s\n' "$top"
	fi
}

# exceptions_file prints the path of .module-exceptions.
exceptions_file() {
	printf '%s/.module-exceptions\n' "${REPO_ROOT:?}"
}

# parse_exceptions validates .module-exceptions and exits 2 on the first
# malformed file (every bad line is reported). A missing file is valid.
parse_exceptions() {
	local file
	file=$(exceptions_file)
	[ -f "$file" ] || return 0
	awk -v file=".module-exceptions" -v q="'" '
		function err(msg) {
			printf "%s:%d: %s (expected: <module> <coverage|platforms> <value> <justification>)\n", file, NR, msg > "/dev/stderr"
			bad = 1
		}
		/^[[:space:]]*(#|$)/ { next }
		{
			if (NF < 4) { err("missing justification"); next }
			kind = $2; value = $3
			if (kind == "coverage") {
				if (value !~ /^[0-9]+$/ || value + 0 > 79) err("coverage value must be an integer 0-79, got " q value q)
			} else if (kind == "platforms") {
				n = split(value, p, ",")
				for (i = 1; i <= n; i++)
					if (p[i] !~ /^(linux|darwin|windows)\/(amd64|arm64)$/) err("invalid platform " q p[i] q)
			} else {
				err("unknown kind " q kind q)
			}
			key = $1 " " kind
			if (key in seen) err("duplicate entry for " key)
			seen[key] = 1
		}
		END { exit bad }
	' "$file" || exit 2
}

# exception_field <module> <kind> <value|justification> prints one field of
# the matching .module-exceptions line, or nothing.
exception_field() {
	local file
	file=$(exceptions_file)
	[ -f "$file" ] || return 0
	awk -v m="$1" -v k="$2" -v f="$3" '
		/^[[:space:]]*(#|$)/ { next }
		$1 == m && $2 == k {
			if (f == "value") { print $3; exit }
			line = $0
			sub(/^[[:space:]]*[^[:space:]]+[[:space:]]+[^[:space:]]+[[:space:]]+[^[:space:]]+[[:space:]]+/, "", line)
			print line
			exit
		}
	' "$file"
}

exception_value() {
	exception_field "$1" "$2" value
}

exception_justification() {
	exception_field "$1" "$2" justification
}

# coverage_threshold <module> prints the module's minimum statement coverage.
coverage_threshold() {
	local v
	v=$(exception_value "$1" coverage)
	printf '%s\n' "${v:-80}"
}

# sha256_of <file> prints the file's SHA-256 digest.
sha256_of() {
	if command -v sha256sum >/dev/null 2>&1; then
		sha256sum "$1" | awk '{print $1}'
	else
		shasum -a 256 "$1" | awk '{print $1}'
	fi
}

SUMMARY_LINES=""
SUMMARY_FAILED=0

# summary_add <PASS|FAIL> <root> [reason] records one module outcome.
summary_add() {
	local line="$1  $2"
	[ -z "${3:-}" ] || line="$line  $3"
	SUMMARY_LINES="${SUMMARY_LINES}${line}"$'\n'
	[ "$1" != FAIL ] || SUMMARY_FAILED=1
}

# summary_print <title> prints the summary block (also to the GitHub job
# summary in CI) and returns 1 if any module failed.
summary_print() {
	printf '\n==> summary\n%s' "$SUMMARY_LINES"
	if [ "${GITHUB_ACTIONS:-}" = true ] && [ -n "${GITHUB_STEP_SUMMARY:-}" ]; then
		# shellcheck disable=SC2016 # literal Markdown code fence, nothing to expand
		printf '### %s\n\n```text\n%s```\n\n' "$1" "$SUMMARY_LINES" >>"$GITHUB_STEP_SUMMARY"
	fi
	[ "$SUMMARY_FAILED" -eq 0 ]
}

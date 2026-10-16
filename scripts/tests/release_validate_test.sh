#!/usr/bin/env bash
# Tests for scripts/release-validate.sh (FR-034, FR-035).
# shellcheck source=helpers.sh
. "$(dirname "${BASH_SOURCE[0]}")/helpers.sh"

setup_release() {
	REPO=$(make_repo)
	write_versions "$REPO"
	make_module "$REPO" alpha github.com/paccolamano/go/alpha
	make_module "$REPO" gamma/v2 github.com/paccolamano/go/gamma/v2
	cat >"$REPO/alpha/CHANGELOG.md" <<'EOF'
# Changelog

## [Unreleased]

## [0.2.0-rc.1] - 2026-10-16

### Added
- Release candidate.

## [0.1.0] - 2026-10-16

### Added
- First release.

## [0.0.9]

## [0.0.1]

- Initial.
EOF
	printf '# Changelog\n\n## [2.0.0]\n\n- Major release.\n' >"$REPO/gamma/v2/CHANGELOG.md"
	commit_all "$REPO" "chore: add fixtures"
	RELEASE_MAIN_REF=main
	MODULE_PREFIX=github.com/paccolamano/go
	NOTES_FILE="$TEST_TMP/notes.md"
	export RELEASE_MAIN_REF MODULE_PREFIX NOTES_FILE
	cd "$REPO"
}

tag() {
	git -C "$REPO" tag "$1"
}

validate() {
	"$SCRIPTS_DIR/release-validate.sh" "$1"
}

test_valid_tag_prints_outputs_and_writes_notes() {
	setup_release
	tag alpha/v0.1.0
	assert_exit 0 validate alpha/v0.1.0
	assert_contains "$OUT" "dir=alpha"
	assert_contains "$OUT" "root=alpha"
	assert_contains "$OUT" "version=0.1.0"
	assert_contains "$OUT" "prerelease=false"
	assert_contains "$OUT" "asset=alpha-v0.1.0.cdx.json"
	assert_eq "$(printf '### Added\n- First release.')" "$(cat "$NOTES_FILE")"
}

test_prerelease_version_is_flagged() {
	setup_release
	tag alpha/v0.2.0-rc.1
	assert_exit 0 validate alpha/v0.2.0-rc.1
	assert_contains "$OUT" "prerelease=true"
	assert_eq "$(printf '### Added\n- Release candidate.')" "$(cat "$NOTES_FILE")"
}

test_major_subdirectory_and_undated_heading() {
	setup_release
	tag gamma/v2.0.0
	assert_exit 0 validate gamma/v2.0.0
	assert_contains "$OUT" "root=gamma/v2"
	assert_contains "$OUT" "asset=gamma-v2.0.0.cdx.json"
	assert_eq "- Major release." "$(cat "$NOTES_FILE")"
}

test_tags_outside_the_grammar_are_rejected() {
	setup_release
	for t in v1.0.0 alpha/1.0.0 alpha/v01.0.0 alpha/v1.0.0+meta Alpha/v1.0.0; do
		assert_exit 1 validate "$t"
		assert_contains "$OUT" "invalid tag" "tag $t"
	done
}

test_directory_without_module_is_rejected() {
	setup_release
	assert_exit 1 validate nope/v1.0.0
	assert_contains "$OUT" "no module in directory 'nope'"
}

test_major_version_mismatch_is_rejected() {
	setup_release
	tag alpha/v2.0.0
	assert_exit 1 validate alpha/v2.0.0
	assert_contains "$OUT" "expected module path github.com/paccolamano/go/alpha/v2"
	assert_contains "$OUT" "found github.com/paccolamano/go/alpha"
}

test_commit_not_on_main_is_rejected() {
	setup_release
	git -C "$REPO" checkout -q -b side
	printf 'side\n' >>"$REPO/alpha/doc.go"
	commit_all "$REPO" "feat(alpha): side change"
	tag alpha/v0.1.0
	git -C "$REPO" checkout -q main
	assert_exit 1 validate alpha/v0.1.0
	assert_contains "$OUT" "is not on main"
}

test_missing_changelog_section_is_rejected() {
	setup_release
	tag alpha/v0.3.0
	assert_exit 1 validate alpha/v0.3.0
	assert_contains "$OUT" "no non-empty '## [0.3.0]' section in alpha/CHANGELOG.md"
}

test_empty_changelog_section_is_rejected() {
	setup_release
	tag alpha/v0.0.9
	assert_exit 1 validate alpha/v0.0.9
	assert_contains "$OUT" "no non-empty '## [0.0.9]' section"
}

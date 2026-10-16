# Contract: Release workflow

Covers FR-034 to FR-041. The workflow file is `.github/workflows/release.yml`.

## Trigger

`push` of a tag matching the filter `*/v*`. Anything that passes the filter but not the strict
tag grammar fails in `validate` and creates no release.

Tag grammar:

```text
^([a-z0-9][a-z0-9._-]*)/v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z.-]+)?$
```

Concurrency: `release-<tag>`, `cancel-in-progress: false`.

## Jobs

```text
validate ──► gates ──► publish
    └──────► sbom ───┘
```

| Job | Permissions | Third-party binaries | Responsibility |
|-----|-------------|----------------------|----------------|
| `validate` | `contents: read` | none | Runs `scripts/release-validate.sh "$TAG"`, uploads the `release-notes` artifact, outputs `root`, `version`, `prerelease`, `exists` |
| `gates` | `contents: read` | golangci-lint | Runs `scripts/lint.sh <root>` and `scripts/test.sh <root>` on `GO_VERSION_NEWEST` |
| `sbom` | `contents: read` | Trivy (hash-pinned) | Runs `trivy fs --format cyclonedx <root>` and uploads the `release-sbom` artifact |
| `publish` | `contents: write` | none, and no checkout | Downloads both artifacts and runs `gh release create` |

When `validate` outputs `exists=true`, the three later jobs are skipped, and the run succeeds
after logging "release already exists, nothing to do" (FR-039).

## Validation (in order, first failure stops the run)

| # | Check | Failure message names |
|---|-------|-----------------------|
| 1 | Tag matches the grammar | the tag and the expected format |
| 2 | `<dir>` (or `<dir>/vN` when N ≥ 2) contains `go.mod` | the directory |
| 3 | Module path equals `github.com/paccolamano/go/<dir>`, plus `/vN` when N ≥ 2 | expected and actual path |
| 4 | `git merge-base --is-ancestor $GITHUB_SHA origin/main` | the commit and `main` |
| 5 | `<root>/CHANGELOG.md` has a non-empty `## [X.Y.Z]` section | the file and the version |
| 6 | Local gates pass (`gates` job) | the failing gate output |

## Publication

```text
gh release create "$TAG" \
  --repo "$GITHUB_REPOSITORY" \
  --verify-tag \
  --title "$TAG" \
  --notes-file release-notes.md \
  [--prerelease] \
  "<dir>-v<version>.cdx.json"
```

- `--verify-tag` aborts if the tag does not exist; the workflow never creates, moves or deletes
  tags (FR-040).
- `--prerelease` is passed when the version has a `-` suffix (FR-037).
- The SBOM is the only asset (FR-038). No binaries are built.

## Recovery

A tag that fails validation stays in place, because published tags are immutable (constitution,
Principle II). The maintainer fixes the problem on `main` and pushes the **next** version. If
the broken version was already resolvable by consumers, the maintainer adds a `retract` directive
in that next version.

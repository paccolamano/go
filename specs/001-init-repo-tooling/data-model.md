# Data Model: Repository Tooling & CI/CD Bootstrap

**Feature**: [spec.md](spec.md) | **Research**: [research.md](research.md)

This feature stores no runtime data. Its "data" is a set of repository conventions and versioned
configuration files read by the scripts and workflows. Exact file grammars live in
[contracts/config-files.md](contracts/config-files.md).

## Module

A Go module hosted in the repository, the unit of linting, testing, scanning and releasing.

| Field | Source | Rule |
|-------|--------|------|
| `dir` | file system | Top-level directory name, without leading `.`, matching `^[a-z0-9][a-z0-9._-]*$` |
| `root` | file system | `<dir>` or `<dir>/vN` (N ≥ 2), whichever holds the `go.mod` |
| `path` | `go list -m` in `root` | `github.com/paccolamano/go/<dir>`, plus `/vN` when major N ≥ 2 |
| `major` | `path` suffix | 0 or 1 when there is no suffix; otherwise N from `/vN` |
| `goVersion` | `go` directive of `go.mod` | `major.minor` ≤ `GO_VERSION_OLDEST` (FR-030) |
| `hasReadme`, `hasChangelog` | file system | both true (FR-029) |
| `replaces` | `go.mod` | must be empty (FR-028) |

**Discovery** (FR-002): for each non-hidden top-level directory `d`, `d` is a module root if
`d/go.mod` exists, and each `d/vN/go.mod` (N ≥ 2) is an additional module root. Nested `go.mod`
files anywhere else (for example under `testdata/`) are ignored. Results are sorted for
deterministic output.

## Release tag

`<dir>/vMAJOR.MINOR.PATCH[-PRERELEASE]`, which identifies one immutable module version.

| Field | Rule |
|-------|------|
| `dir` | Must name an existing module directory |
| `version` | SemVer without build metadata (`+` is rejected) |
| `prerelease` | true when a `-` suffix is present, then published as a GitHub pre-release |
| `moduleRoot` | resolved as in [research.md R-12](research.md#r-12-release-workflow) |

Validation order (first failure wins, reported with its reason):

1. format
2. module exists
3. major matches the path suffix
4. commit reachable from `main`
5. changelog section exists and is not empty
6. local gates pass

## Changelog entry

The section of `<root>/CHANGELOG.md` (Keep a Changelog) for one version.

- Heading: `## [X.Y.Z]` or `## [X.Y.Z] - YYYY-MM-DD`, with the version written without `v`.
- Body: every line up to the next `## ` heading or the end of the file, with blank lines trimmed.
- Used as the release notes (FR-036). An empty body counts as missing.

## Module exception

One line of `.module-exceptions`: an approved deviation for one module.

| Field | Rule |
|-------|------|
| `module` | Module root as discovered (`retry`, `retry/v2`) |
| `kind` | `coverage` or `platforms` |
| `value` | `coverage`: integer 0–79. `platforms`: comma-separated `GOOS/GOARCH` list of the platforms the module **does** support |
| `justification` | Free text, required and non-empty |

Rules:

- At most one line per (`module`, `kind`).
- Lines naming a module that does not exist make the `structure` status fail (stale exception).
- A `platforms` exception must also be stated in the module README (checked in review).

## Repository-level scope

One line of `.github/commit-scopes`: a Conventional Commit scope allowed for changes that do not
belong to a module. Lowercase, `^[a-z0-9][a-z0-9._-]*$`, and it must not collide with a module
directory. Initial set: `ci`, `deps`, `docs`, `repo`, `specs`.

## Changelog waiver

The GitHub label **`skip-changelog`** on a pull request. While present, the `changelog` status
passes and prints "waived by label". Adding or removing the label re-runs `pr-meta.yml`.

## Gate (status check)

| Field | Rule |
|-------|------|
| `name` | Stable job name listed in [contracts/ci-checks.md](contracts/ci-checks.md) |
| `workflow` | `pr.yml`, `pr-meta.yml` or `release.yml` |
| `scope` | `touched`, `all`, `repository` or `commits` |
| `outcome` | `success` or `failure`; never left pending or skipped on a PR (FR-024) |

**State transitions** on a pull request:

```text
queued → in_progress → success
                     → failure
in_progress → cancelled   (only when superseded by a newer run, FR-027; the newer run reports)
```

## Tool version pin

One `KEY=value` line of `versions.env`, the single source of truth for toolchain and tool
versions (FR-009, FR-015, FR-042). Keys are listed in
[contracts/config-files.md](contracts/config-files.md#versionsenv).

## SBOM

A CycloneDX JSON document per module.

- PR runs: `sbom/<root with "/" replaced by "_">.cdx.json` inside the `sbom` workflow artifact.
- Releases: attached as `<dir>-v<version>.cdx.json`.

## Workspace file

`go.work` at the repository root:

- a `go <GO_VERSION_OLDEST>.0` line;
- no `toolchain` line;
- a `use` entry for each module root, sorted.

`go.work.sum` sits next to it once it exists. Both are committed, and no gate reads them
(FR-012).

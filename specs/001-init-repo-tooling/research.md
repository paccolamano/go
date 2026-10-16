# Research: Repository Tooling & CI/CD Bootstrap

**Feature**: [spec.md](spec.md) | **Plan**: [plan.md](plan.md) | **Date**: 2026-10-16

All versions below were checked on 2026-10-16 against the upstream release feeds. Unless stated
otherwise, the pinned version is the newest stable release published at least 14 days before
that date (cool-down policy, see R-03).

## R-01 Supported Go versions

- **Decision**: oldest supported = **1.26**, newest = **1.27** (current patches 1.26.9 and 1.27.2).
  CI resolves the latest patch with `1.26.x` / `1.27.x`. `GOTOOLCHAIN=local` is set in every
  workflow so that a job never silently switches to another toolchain.
- **Rationale**: `go.dev/dl/?mode=json` lists exactly two stable lines, 1.27.2 and 1.26.9. This
  matches the constitution's "two most recent major Go releases". The module `go` directive is
  compared on `major.minor` only (`go 1.26.3` is allowed, `go 1.27.0` is not, while 1.26 is the
  oldest).
- **Alternatives considered**: resolving the versions dynamically from `go.dev` at run time.
  Rejected because a new Go release would change the CI results with no commit in the
  repository. The periodic constitution review already covers updating the versions by hand.

## R-02 Implementation language for repository tooling

- **Decision**: put the logic in **portable Bash scripts** under `scripts/`, called by both the
  Makefile and the workflows. The Makefile stays a thin wrapper. The scripts must run on
  **Bash 3.2** (the macOS default) and use only POSIX utilities. That means no `mapfile`, no
  associative arrays, no `${x,,}`, no `grep -P`, no `sed -i` and no `sort -V`; versions are
  compared in `awk`. The Makefile must run on **GNU Make 3.81** (the macOS default), so no
  `.ONESHELL`, `!=` or `$(file)`.
- **Rationale**:
  - Calling the same scripts locally and in CI gives identical verdicts (SC-004).
  - Bash needs no extra toolchain.
  - The repository root must not become a Go module (Principle I).
- **Alternatives considered**:
  - A Go tooling module. Rejected: either it is a top-level module, and then it must have a
    README and CHANGELOG, be released and be versioned like a library, or it is hidden outside
    the module layout, which bends Principle I. Recorded in Complexity Tracking.
  - Inline workflow YAML only. Rejected: it cannot be run locally and cannot be unit-tested.
  - `jq` for JSON. Rejected for local scripts because it is not installed by default on macOS.
    `go list -m -f` and `awk` over `go.mod` are enough.

## R-03 Supply-chain policy for CI components

- **Decision**:
  1. Every GitHub Action is pinned to a **full commit SHA**, with the tag in a trailing comment.
  2. Every downloaded binary (Trivy, Gitleaks, Bearer) is pinned by **version and SHA-256** in
     `versions.env`. `scripts/install-tool.sh` downloads it and verifies the hash before running
     it. The checksum file shipped with the same release is not trusted.
  3. The Semgrep container image is pinned by **digest**.
  4. Versions follow a **14-day cool-down**.
  5. No third-party binary runs in a job that holds a write token.
- **Rationale**: on 2026-03-19, 76 of the 77 tags of `aquasecurity/trivy-action` and every tag of
  `aquasecurity/setup-trivy` were force-pushed to credential-stealing code, and a malicious
  Trivy binary (v0.69.4) was published (CVE-2026-33634). Pinning by tag or trusting a same-release
  checksum would not have prevented it. A pinned hash and a cool-down would have. This also
  satisfies FR-042.
- **Alternatives considered**: `aquasecurity/trivy-action` or `setup-trivy` pinned by SHA.
  Rejected: neither is needed, since a single binary is easy to install, and they are the
  compromised components. `gitleaks/gitleaks-action`: rejected because it adds a license-key
  path for organizations and posts PR comments (it needs extra permissions); the CLI is enough.

## R-04 Pinned components

| Component | Version | Pin | Notes |
|-----------|---------|-----|-------|
| `actions/checkout` | v7.0.1 | `3d3c42e5aac5ba805825da76410c181273ba90b1` | |
| `actions/setup-go` | v7.0.0 | `b7ad1dad31e06c5925ef5d2fc7ad053ef454303e` | |
| `actions/upload-artifact` | v7.0.1 | `043fb46d1a93c77aae656e7c1c64a875d1fc6a0a` | |
| `actions/download-artifact` | v8.0.1 | `3e5f45b2cfb9172054b4087a40e8e0b5a5461e7c` | check that it reads v7 uploads during implementation |
| `golangci/golangci-lint-action` | v9.3.0 | `ba0d7d2ec06a0ea1cb5fa41b2e4a3ab91d21278a` | used with `install-only: true` |
| golangci-lint | v2.14.0 | version in `versions.env` | released 2026-09-24 |
| gorelease | `v0.0.0-20260824195058-e88cd73687aa` | pseudo-version in `versions.env` | `golang.org/x/exp/cmd/gorelease`, last change 2026-08-24 |
| Trivy | v0.75.0 | version + SHA-256 | v0.76.0 is newer than the cool-down allows |
| Gitleaks | v8.30.1 | version + SHA-256 | |
| Semgrep | 1.178.0 | image digest | `semgrep/semgrep` |
| Bearer | v2.1.1 | version + SHA-256 | v2.1.2 is newer than the cool-down allows |

The SHA-256 values and the Semgrep digest are computed during implementation, by downloading
each artifact once and recording its hash.

## R-05 Linter configuration (golangci-lint v2)

- **Decision**: one root `.golangci.yml` (`version: "2"`), passed explicitly with `--config` from
  each module directory.
  - Formatters: `gofmt`, `goimports` (`local-prefixes: github.com/paccolamano/go`).
  - Linters: the `standard` set (`errcheck`, `govet`, `ineffassign`, `staticcheck`, `unused`),
    plus the following, each mapped to the constitution:

    | Linter | Principle IV rule enforced |
    |--------|----------------------------|
    | `revive` (`exported`, `package-comments`) | every exported identifier and package has a doc comment |
    | `errorlint`, `errname`, `wrapcheck` (exported packages only) | errors are wrapped with `%w`; sentinel errors and error types are usable with `errors.Is`/`As` |
    | `forbidigo` (`fmt.Print*`, `print`/`println`, `os.Exit`, `log.*`, `panic` in non-test code; `exclude-godoc-examples: true`) | no stdout/stderr writes, no exit, no global logger |
    | `gochecknoinits`, `gochecknoglobals` (sentinel `Err*` variables are allowed by the linter) | no `init()` side effects, no global mutable state |
    | `noctx`, `contextcheck` | functions that do I/O take a `context.Context` |
    | `nolintlint` (`require-explanation`, `require-specific`) | every `//nolint` names the linter and states the reason |
    | `usetesting`, `thelper`, `misspell` | test hygiene and spelling |

  - Test files are excluded from `forbidigo`, `gochecknoglobals` and `wrapcheck`.
  - The `comments` exclusion preset is **not** enabled, because it would hide missing doc
    comments.
- **Rationale**: Principle IV requires one tool with `gofmt`, `goimports` and `govet`. The extra
  linters turn Principle IV rules into checks a machine can verify.
- **Alternatives considered**: the `standard` set alone. Rejected: doc comments, `os.Exit` and
  `init()` would be left to review. `gosec`: left out to avoid duplicate findings, because
  Semgrep `p/golang` and Bearer already cover security.

## R-06 Local test gate and coverage

- **Decision**: run
  `go test -race -shuffle=on -covermode=atomic -coverprofile=<tmp>/cover.out ./...` from the
  module directory with `GOWORK=off`. The total comes from `go tool cover -func`
  (`total:` line). A module with no statements passes. A module below its threshold (80% by
  default, or its entry in `.module-exceptions`) fails, and the output shows the measured value,
  the threshold and the justification.
- **Rationale**:
  - Since Go 1.22, packages without tests are counted at 0%, so untested packages lower the total.
  - The profile is written outside the module, so the working tree stays clean.
  - `-race` needs cgo and a C toolchain; this is a documented prerequisite.
- **Alternatives considered**: per-package thresholds. Rejected: the spec assumes a per-module
  total.

## R-07 Pull request status layout

- **Decision**: two workflows.

  `pr.yml` runs on `opened`, `synchronize` and `reopened`. Its jobs, each with a stable name,
  are:

  | Status | Spec reference |
  |--------|----------------|
  | `lint` | |
  | `test` | aggregator over the `test (go 1.26)` / `test (go 1.27)` matrix |
  | `tidy` | |
  | `api-compat` | |
  | `vulnerabilities` | |
  | `sbom` | |
  | `secrets` | |
  | `semgrep` | |
  | `bearer` | |
  | `structure` | FR-028–030 |
  | `cross-build` | FR-031 |
  | `scripts (ubuntu-latest)`, `scripts (macos-latest)` | tooling self-tests |

  `pr-meta.yml` runs on `opened`, `synchronize`, `reopened`, `edited`, `labeled` and `unlabeled`,
  with the jobs `commits` (PR title) and `changelog`. Editing the title or adding the waiver label
  re-evaluates only these two cheap statuses.

  The workflows have no path filters; a job that has nothing to check succeeds with an explicit
  message. A `setup` job outputs only the Go-version matrix. Every other job computes its own
  module list and has no `needs:`, because a job skipped after a failed dependency can count as
  passing for branch protection. The `test` aggregator (`if: always()`) is the only required
  status with a dependency. Each workflow has `concurrency: <workflow>-<PR number>` with
  `cancel-in-progress: true`.
- **Rationale**:
  - Path filters or job-level `if` can leave required statuses pending or skipped (FR-024).
  - Matrix job names change when the Go versions change, so branch protection requires the
    stable `test` aggregator instead.
  - Splitting the metadata checks avoids re-running every scan on a label change.
- **Alternatives considered**:
  - One workflow for everything. Rejected: every label or title edit would re-run every scan.
  - A single "all gates" aggregator as the only required check. Rejected: the constitution says
    each gate is a required status check.
  - One status per compliance rule (FR-028, FR-029 and FR-030 separately). Rejected: the spec's
    acceptance scenario US2-8 defines a single "structural status".

## R-08 Touched-module detection

- **Decision**: `git diff --name-only <base.sha>...<head.sha>`, with three dots (from the merge
  base). Each file maps to `<dir>/vN` when that is a module, otherwise to `<dir>` when that is a
  module. All modules count as touched when any of these change: `.golangci.yml`,
  `versions.env`, `Makefile`, `scripts/**`, `.github/workflows/**`, `.github/commit-scopes`,
  `.module-exceptions`, `.trivyignore`, `.gitleaksignore`, `.semgrepignore`, `bearer.yml`,
  `bearer.ignore`. `go.work` is not on the list, because no gate uses it (FR-012).
- **Rationale**: the three-dot diff ignores commits that landed on `main` after the branch point.
- **Alternatives considered**: `dorny/paths-filter`. Rejected: it adds a third-party action, and
  it cannot map files to a dynamic list of modules.

## R-09 API compatibility (gorelease)

- **Decision**: `scripts/apicompat.sh` finds the base itself. It takes the highest
  `<dir>/vX.Y.Z` tag of the same major version as the module path (`fetch-depth: 0`).
  - With no tag, it runs `gorelease -base=none` and reports only.
  - With a tag, it runs `gorelease -base=vX.Y.Z` with no `-version`, captures the report in
    `$GITHUB_STEP_SUMMARY`, and **fails only when the base major is at least 1 and the report
    contains an incompatible-changes section**.
- **Rationale**:
  - Without `-version`, gorelease's exit status is not documented as reflecting incompatible
    changes. Its docs only guarantee a non-zero exit for a *proposed* version of v1 or higher.
    Parsing the report states the rule explicitly.
  - Computing the base from git tags avoids depending on how gorelease guesses a base for nested
    module paths.
- **Alternatives considered**:
  - `apidiff`. Rejected: the constitution names gorelease.
  - Letting gorelease guess the base. Rejected: hard to verify for `<dir>/vN` layouts.

## R-10 Security scanners

- **Trivy**: `trivy fs --scanners vuln --severity HIGH,CRITICAL --exit-code 1
  --ignorefile .trivyignore .` runs once at the root. It discovers every `go.mod` and reports
  per target. If the vulnerability database cannot be downloaded, Trivy exits non-zero
  (fail closed, FR-026). No database cache for now; add one only if rate limits show up.
- **SBOM**: `trivy fs --format cyclonedx --output sbom/<dir>[_vN].cdx.json <module dir>` for each
  module, uploaded as the `sbom` artifact (90-day retention). The same command produces the
  release asset.
  - *Alternative*: Syft. Rejected because it is one more tool; Trivy is already mandated.
- **Gitleaks**: `gitleaks git --log-opts="<base.sha>..<head.sha>" --redact --exit-code 1`. The
  root `.gitleaksignore` is picked up automatically.
- **Semgrep**: `semgrep scan --metrics=off --error`, using the rulesets in the `SEMGREP_RULES`
  env var of `pr.yml`: `p/golang p/trailofbits p/github-actions`.
  - `p/github-actions` guards the workflows themselves, for example against script injection
    from the PR title.
  - Registry rules download without login (checked: `semgrep.dev/c/p/golang` returns HTTP 200
    with 42 rules).
  - Registry rules are not version-pinned, so an upstream rule change can surface new findings.
    Accepted, and recorded as a known risk.
  - The root `.semgrepignore` replaces Semgrep's default ignore list, so it must re-list the
    defaults the repository needs.
- **Bearer**: `bearer scan . --config-file bearer.yml`, with `report: security` and
  `fail-on-severity: critical,high,medium,low,warning`. Its security report includes the
  privacy (sensitive-data) rules, which covers "no security or privacy findings". Bearer exits 1
  with "0 rules found" when the repository has no file in a language it supports, so the job
  skips the scan with an explicit message in that case (checked with the pinned binary: it finds
  CWE-532 in a sample Go file and errors on a Go-free tree). Suppressions
  live in `bearer.ignore`, managed with `bearer ignore add`. `specs/` and `scripts/tests/` are
  skipped.
- **Licenses** (tools only, never linked into or distributed with any module):

  | Tool | License |
  |------|---------|
  | Trivy | Apache-2.0 |
  | Gitleaks | MIT |
  | golangci-lint | GPL-3.0 |
  | Semgrep CLI | LGPL-2.1 (registry rules under the Semgrep Rules License, internal use allowed) |
  | Bearer | Elastic License 2.0 (internal CI use allowed) |

  Principle V's license rule concerns module dependencies, which this feature does not add.

## R-11 Compliance checks (FR-028 to FR-033)

- **structure** (all modules):
  - `replace` directives: `awk` match of `^[[:space:]]*replace([[:space:]]|\()` in `go.mod`.
  - `README.md` and `CHANGELOG.md` must exist.
  - The `go` directive, read from `go.mod` with `awk` (it keeps working on a `go.mod` with
    broken `replace` targets, unlike `go list -m`), must be at most `GO_VERSION_OLDEST` on
    `major.minor`.
- **cross-build** (touched modules): for each GOOS × GOARCH in {linux, darwin, windows} ×
  {amd64, arm64}, minus restrictions from `.module-exceptions`, run `CGO_ENABLED=0 go build ./...`
  and `go vet ./...`. vet type-checks test files for that platform.
- **commits** (constitution v1.2.0: squash merge only, so the PR title is the only message that
  reaches `main`):
  - Input: `$PR_TITLE`, passed through `env:` and never interpolated into the script (injection
    guard). Branch commits are not checked.
  - Format: `^(feat|fix|docs|style|refactor|perf|test|build|ci|chore|revert)(\([a-z0-9][a-z0-9._-]*\))?!?: .+`.
    GitHub's default `Revert "…"` title is also accepted.
  - The scope must be a module directory (modules at base ∪ head) or a line of
    `.github/commit-scopes`.
  - When every file in `git diff --name-only <base>...<head>` belongs to one module, the title
    must use that module's directory as scope.
  - The ruleset allows only squash merges (`allowed_merge_methods: ["squash"]`) and adds
    `required_linear_history`. The repository setting `squash_merge_commit_title=PR_TITLE` makes
    the checked title the actual commit message, also for single-commit pull requests.
- **changelog**:
  - Triggered by changes to non-`_test.go` `*.go` files outside `testdata/`, and to `go.mod`.
  - Requires `<dir>/CHANGELOG.md` to change in the same diff.
  - Waived when the PR carries the `skip-changelog` label. Only users with triage or write access
    can apply labels.

## R-12 Release workflow

- **Decision**: `release.yml` runs on `push: tags: ['*/v*']`. Its jobs are:

  | Job | Permissions | What it does |
  |-----|-------------|--------------|
  | `validate` | `contents: read` | Parses the tag, resolves the module directory, checks the module path suffix, ancestry from `origin/main` and the CHANGELOG section, and writes the notes artifact. Outputs `exists=true` when a release already exists. |
  | `gates` | `contents: read` | Runs `scripts/lint.sh` and `scripts/test.sh` for the module on the newest Go. |
  | `sbom` | `contents: read` | Runs Trivy and uploads the artifact. |
  | `publish` | `contents: write` | No checkout and no third-party binary. Runs `gh release create "$TAG" --verify-tag --title "$TAG" --notes-file notes.md [--prerelease] sbom.cdx.json`. |

  - The tag must match
    `^([a-z0-9][a-z0-9._-]*)/v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z.-]+)?$`.
  - For major N ≥ 2, the module directory is `<dir>/vN` if it contains `go.mod`, otherwise
    `<dir>`. The module path must equal `github.com/paccolamano/go/<dir>`, plus `/vN` when N ≥ 2.
  - The CHANGELOG section is the block under `## [X.Y.Z]` (date optional), up to the next `## `
    heading; it must not be empty.
- **Rationale**:
  - The tag push is the constitution's release procedure.
  - `--verify-tag` guarantees that the workflow never creates a tag (FR-040).
  - Splitting the jobs keeps the write token away from third-party code (R-03).
  - The `exists` short-circuit makes re-runs idempotent (FR-039).
- **Alternatives considered**:
  - GoReleaser. Rejected: built for binaries, and its monorepo support is a paid feature.
  - `softprops/action-gh-release`. Rejected: `gh` is preinstalled and first-party.
- **Testing note**: tags are immutable under the constitution, so the release scenarios are
  validated on a **fork or scratch repository**, never on `paccolamano/go` itself (see
  [quickstart.md](quickstart.md)).

## R-13 Workspace file

- **Decision**: `scripts/work-sync.sh` regenerates `go.work` from scratch, then
  `go work edit -go=<GO_VERSION_OLDEST>.0 -toolchain=none`, then `go work use <modules…>`. With no
  modules, the file contains only the `go` line. `go.work.sum` is committed when the go command
  creates it.
- **Rationale**: regenerating instead of patching drops modules that were removed (FR-011).
  Pinning the `go` line to the oldest supported version keeps the workspace valid for every
  supported toolchain. The go command rejects a module that requires a newer Go than the
  workspace, which surfaces FR-030 violations early.

## Known risks

| Risk | Mitigation |
|------|------------|
| Semgrep registry rules change upstream and add new findings with no repository change | Suppress through `.semgrepignore` or `nosemgrep` with a justification; vendor the rules later if it happens often |
| Trivy DB download is rate-limited | Trivy defaults to `mirror.gcr.io`; add `actions/cache` for `~/.cache/trivy` if it happens |
| macOS-only shell incompatibilities | `scripts (macos-latest)` runs the tooling self-tests on Bash 3.2 and BSD userland |
| Pinned SHAs and hashes go stale | Out of scope (no Dependabot); updated by hand at the constitution's periodic review |

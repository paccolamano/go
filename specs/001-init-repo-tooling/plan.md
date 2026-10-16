# Implementation Plan: Repository Tooling & CI/CD Bootstrap

**Branch**: `001-init-repo-tooling` | **Date**: 2026-10-16 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `specs/001-init-repo-tooling/spec.md`

**Note**: This template is filled in by the `/speckit-plan` command; its definition describes the execution workflow.

## Summary

The repository gets the shared tooling that every future Go module depends on:

- a **Makefile** running the constitution's local gates (lint, test) per module, plus workspace
  sync;
- a committed **`go.work`**;
- a **pull request workflow pair** enforcing the eight constitutional PR gates and six more
  automated compliance checks;
- a **release workflow** that turns a pushed `<dir>/vX.Y.Z` tag into a GitHub release, with
  notes from the module changelog and an SBOM attached.

All logic lives in portable Bash scripts shared by Make and CI, so local and CI verdicts are
identical. Every external component is pinned to an immutable reference with a 14-day
cool-down. The cool-down was chosen in response to the March 2026 Trivy supply-chain compromise.

## Technical Context

**Language/Version**: Bash ≥ 3.2 with POSIX utilities (scripts); GNU Make ≥ 3.81; GitHub Actions
YAML. Target Go toolchains: **1.26** (oldest) and **1.27** (newest).

**Primary Dependencies**: all pinned per [research.md R-04](research.md#r-04-pinned-components).

| Group | Components |
|-------|------------|
| Mandated by the constitution | golangci-lint v2.14.0, gorelease (x/exp pseudo-version), Trivy 0.75.0, Gitleaks 8.30.1, Semgrep 1.178.0, Bearer 2.1.1 |
| First-party GitHub | `actions/checkout`, `setup-go`, `upload-artifact`, `download-artifact`, plus the preinstalled `gh` and `shellcheck` |
| Other | `golangci/golangci-lint-action` (install only) |

No Go dependency is added to any module, because no module exists yet.

**Storage**: N/A. State is versioned files: `versions.env`, `.module-exceptions`,
`.github/commit-scopes`, and the scanner ignore files.

**Testing**: the tooling scripts get a plain-Bash self-test suite (`scripts/tests/`, with
fixture git repositories in `mktemp -d`), plus `shellcheck`. It runs on Ubuntu and macOS in CI.
End-to-end behavior is validated with throwaway modules per [quickstart.md](quickstart.md).

**Target Platform**: GitHub-hosted `ubuntu-latest` (all gates) and `macos-latest` (script
self-tests). Contributors use Linux or macOS.

**Project Type**: repository infrastructure (build scripts and CI/CD) for a Go multi-module
monorepo.

**Performance Goals**:

- all statuses for a single-module PR within 15 minutes (SC-006);
- release visible within 10 minutes of the tag push (SC-007);
- local `make check` dominated by `go test` time.

**Constraints**:

- no repository secrets, so fork PRs are fully checked;
- least-privilege tokens, with write access only in the release `publish` job;
- `GOWORK=off` and `GOTOOLCHAIN=local` in every gate;
- every required status conclusive on every PR;
- Bash 3.2 and Make 3.81 compatibility;
- the repository root must not be a Go module.

**Scale/Scope**: 0 modules today, designed for tens of modules. Module discovery is automatic,
so adding a module requires no tooling change (SC-002).

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

| Rule | Assessment | Status |
|------|------------|--------|
| **I. Independent modules** | No module is created. Discovery treats each top-level `go.mod` (and `<dir>/vN`) as an isolated module. Every gate runs with `GOWORK=off` from the module root. `go.work` is committed, as allowed, but no gate reads it. The root has no `go.mod`. | ✅ |
| **II. SemVer & stable API** | `api-compat` runs gorelease in the PR workflow and fails on incompatible changes at v1 or later. Releases only accept `<dir>/vX.Y.Z`, check the `/vN` suffix, and never move or delete tags. The `changelog` status enforces CHANGELOG updates. | ✅ |
| **III. Test-first** | Not applicable to Go code (no Go code in this feature). The tooling scripts are developed test-first against a Bash self-test suite (red, then green). The Go-specific clauses (`testing` package, race, coverage) are *enforced* for future modules by `test.sh`. Bash tests not using the Go `testing` package are recorded below. | ✅ (justified) |
| **IV. Idiomatic Go** | `.golangci.yml` enables `gofmt`, `goimports` and `govet`, plus linters that encode doc-comment, error-wrapping, no-exit/no-stdout, no-`init` and `nolint` rules ([R-05](research.md#r-05-linter-configuration-golangci-lint-v2)). | ✅ |
| **V. Simplicity & dependencies** | No module dependency is added. The CI tools are those the constitution mandates, plus first-party GitHub actions and one golangci-lint installer action. Each is justified with license and maintenance status in research R-03, R-04 and R-10. Bearer is under ELv2 and used as a CI tool only, never linked. There is no speculative abstraction: one script per check. | ✅ |
| Go toolchain support | `versions.env` declares 1.26 and 1.27. `structure` blocks a `go` directive above 1.26. | ✅ |
| Platforms | `cross-build` covers linux, darwin and windows on amd64 and arm64, with declared exceptions. | ✅ |
| Repository layout | All tooling is at the root and applies uniformly. Module overrides are allowed only through `.module-exceptions`, each with a justification. | ✅ |
| Dependency hygiene | `tidy` runs `go mod tidy -diff` for all modules. | ✅ |
| Vulnerability scanning | Trivy only, CI only. No govulncheck anywhere. | ✅ |
| Secrets | Gitleaks on the PR commits. No secrets are used by the workflows. | ✅ |
| Local gates | `make check` = `golangci-lint run ./...` + `go test -race -shuffle=on ./...` with coverage ≥ 80%, and nothing else. | ✅ |
| PR gates 1–8 | All mapped to stable statuses ([contracts/ci-checks.md](contracts/ci-checks.md)). Required checks are configured via the ruleset in `.github/ruleset-main.json` (an admin step). | ✅ |
| Finding suppression | Only through `.trivyignore`, `.gitleaksignore`, `.semgrepignore`/`nosemgrep` and `bearer.ignore`, each entry with a justification. | ✅ |
| Merging (v1.2.0) | `.github/ruleset-main.json` allows only squash merges and requires a linear history on `main`; `CONTRIBUTING.md` sets the squash commit title to the PR title. | ✅ |
| Commits (v1.2.0) | The `commits` status checks the PR title (the squash commit message): Conventional Commits with module scope. Branch commits are not checked. | ✅ |
| Release procedure | Update the CHANGELOG, choose the bump from the `api-compat` report, push the tag. The workflow validates and publishes. | ✅ |

**Result (pre-research)**: PASS. One deviation is justified in Complexity Tracking.

**Result (post-design re-check)**: PASS. The design artifacts (data model, four contracts,
quickstart) introduce no new dependency, module or rule exception beyond the table above. Every
FR is mapped to a script or status in the contracts.

## Project Structure

### Documentation (this feature)

```text
specs/001-init-repo-tooling/
├── plan.md              # This file
├── research.md          # Phase 0: decisions, pinned versions, risks
├── data-model.md        # Phase 1: entities and validation rules
├── quickstart.md        # Phase 1: end-to-end validation guide
├── contracts/
│   ├── makefile.md      # local commands and script interface
│   ├── ci-checks.md     # PR statuses (required checks)
│   ├── config-files.md  # versions.env, exceptions, scopes, scanner files
│   └── release.md       # release workflow
├── checklists/
│   └── requirements.md  # spec quality checklist
└── tasks.md             # Phase 2 (/speckit-tasks, not created here)
```

### Source Code (repository root)

```text
Makefile                   # thin wrapper: help, lint, test, check, work, scripts-test
versions.env               # single source of truth for Go and tool versions and hashes
go.work                    # committed workspace (go 1.26.0, no modules yet)
.golangci.yml              # shared linter config (v2)
.module-exceptions         # per-module coverage and platform exceptions, with justifications
.trivyignore               # Trivy suppressions
.gitleaksignore            # Gitleaks suppressions
.semgrepignore             # Semgrep ignore list
bearer.yml                 # Bearer config
.shellcheckrc              # shellcheck: follow sourced scripts (lib.sh, helpers.sh)
.gitignore                 # bin/, sbom/, *.cdx.json, cover.out
CONTRIBUTING.md            # local gates, PR statuses, release procedure, branch protection
README.md                  # add a short "Development" pointer to CONTRIBUTING.md

scripts/
├── lib.sh                 # logging, summary, versions.env loading, module discovery, exceptions parser
├── modules.sh             # list | touched <base> <head> | json-go-versions
├── lint.sh
├── test.sh
├── work-sync.sh
├── tidy-check.sh
├── apicompat.sh
├── structure-check.sh
├── cross-build.sh
├── commits-check.sh
├── changelog-check.sh
├── release-validate.sh
├── install-tool.sh
└── tests/
    ├── run.sh             # discovers and runs *_test.sh, prints a summary, exit 0/1
    ├── helpers.sh         # assert_eq, assert_exit, fixture repo and module builders
    ├── modules_test.sh
    ├── exceptions_test.sh
    ├── structure_test.sh
    ├── commits_test.sh
    ├── changelog_test.sh
    └── release_validate_test.sh

.github/
├── commit-scopes          # ci, deps, docs, repo, specs
├── ruleset-main.json      # branch ruleset: required statuses, squash-only, linear history
└── workflows/
    ├── pr.yml             # setup, lint, test (matrix + aggregator), tidy, api-compat, vulnerabilities,
    │                      # sbom, secrets, semgrep, bearer, structure, cross-build, scripts (matrix)
    ├── pr-meta.yml        # commits (PR title), changelog
    └── release.yml        # validate, gates, sbom, publish
```

**Structure Decision**: tooling only, at the repository root. Logic sits in `scripts/` and is
reused by the Makefile and the workflows. No Go module is created, and the root is not a module.
Future modules are added as top-level directories and are picked up automatically.

## Complexity Tracking

| Violation | Why Needed | Simpler Alternative Rejected Because |
|-----------|------------|--------------------------------------|
| Tooling tests are written in Bash, not with Go's `testing` package (Principle III: "Tests MUST use the standard `testing` package") | The tooling is Bash, so it must be tested in Bash. Writing it in Go would need a Go module for repository tooling. | A Go tooling module would have to be either (a) a top-level module, which needs a README, a CHANGELOG, tagged releases and API stability as if it were a public library, or (b) hidden outside the module layout, which bends Principle I. Both cost more than a Bash suite plus shellcheck, and neither is needed for scripts this size. |

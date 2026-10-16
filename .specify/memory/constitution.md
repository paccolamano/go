<!--
Sync Impact Report
==================
Version change: 1.0.0 → 1.1.0
Bump rationale: MINOR. Quality gates are split into local and pull request gates, and a security
scanning gate set (Trivy, SBOM, Gitleaks, Semgrep, Bearer) is added. govulncheck is replaced by
Trivy with the same intent (no known vulnerable dependencies); no principle is removed or
redefined.

Modified principles:
  - II. Stable Public API & Semantic Versioning: gorelease check moved from a local release step
    to the pull request workflow.
  - IV. Idiomatic, High-Quality Go: gofmt/goimports and go vet are enforced through the shared
    golangci-lint configuration instead of as separate tools.

Modified sections:
  - Technology & Compatibility Constraints: govulncheck removed; Trivy declared the single
    vulnerability scanner (CI only); new "Vulnerability scanning" rule; Gitleaks enforces the
    secrets rule.
  - Development Workflow & Quality Gates: "Quality gates" replaced by "Local gates"
    (golangci-lint + go test only) and "Pull request gates" (required status checks, including
    security scans); new "Finding suppression" rule; release procedure uses the CI gorelease
    report.
  - Governance / Compliance review: references local and pull request gates.

Added sections: none (new rules live inside existing sections)
Removed sections: none

Dependent templates (read the constitution at runtime; not modified by this command):
  ✅ .specify/templates/plan-template.md — "Constitution Check" derives gates from this file.
  ✅ .specify/templates/tasks-template.md — no change needed.
  ✅ .specify/templates/spec-template.md, checklist-template.md — no conflicts.

Deferred TODOs: none
-->

# paccolamano/go Constitution

## Core Principles

### I. Independent, Self-Contained Modules

Every library or utility in this repository is a standalone Go module that consumers install on
its own, without pulling in the rest of the repository.

- Each module MUST live in its own top-level directory containing its own `go.mod`, with module
  path `github.com/paccolamano/go/<dir>` (plus a `/vN` suffix for N ≥ 2).
- Each module MUST build, vet and test in isolation (`GOWORK=off`, from the module directory).
  A `go.work` file MAY exist for local development but MUST NOT be required by any gate.
- Committed `go.mod` files MUST NOT contain `replace` directives.
- A module that depends on another module of this repository MUST reference a published tagged
  version, exactly like any external dependency. Dependency cycles between modules are forbidden.
- Each module MUST have a single, clearly stated purpose. Grab-bag modules or packages named
  `utils`, `common`, `helpers`, `misc` or similar are forbidden.
- Each module MUST ship a `README.md` (purpose, `go get` command, minimal usage example, minimum
  Go version) and a `CHANGELOG.md`.

**Rationale**: consumers only pay for what they import, and each module can evolve, be versioned
and be retired without coordinating with the others.

### II. Stable Public API & Semantic Versioning

The exported API of every module is a contract with every downstream project that imports it.

- Each module MUST be versioned independently following Semantic Versioning, using Go's nested
  module tag format `<dir>/vX.Y.Z` (e.g. `retry/v1.2.0`).
- Once a module reaches `v1.0.0`, any backward-incompatible change to its exported API or
  documented behavior MUST ship as a new MAJOR version with the `/vN` module path suffix.
- Exported surface MUST be kept minimal: anything consumers do not need to call MUST be
  unexported or placed under `internal/`.
- Removals MUST be preceded by a `// Deprecated:` doc comment released in at least one MINOR
  version before the MAJOR version that removes the identifier.
- API compatibility of every touched module MUST be checked against its latest release tag with
  `gorelease` in the pull request workflow, and the released version MUST match the change
  level it reports.
- Published tags MUST NOT be moved or deleted. A broken release is fixed by publishing a new
  version and adding a `retract` directive to `go.mod`.
- Every user-visible change MUST be recorded in the module's `CHANGELOG.md`
  (Keep a Changelog format) in the same change that introduces it.

**Rationale**: these libraries are shared by many projects; an unannounced break propagates to
all of them, and the Go module proxy makes published versions immutable.

### III. Test-First & Comprehensive Testing (NON-NEGOTIABLE)

No behavior exists until a test proves it.

- Tests MUST be written before the implementation and MUST be observed failing first
  (Red → Green → Refactor). In Spec Kit task lists, test tasks precede the implementation tasks
  they cover.
- Every bug fix MUST include a regression test that fails without the fix.
- Every exported function, method and type MUST be exercised by tests in the same change that
  introduces or modifies it. Statement coverage per module MUST be ≥ 80%; lower coverage for a
  specific package MUST be justified in the plan's Complexity Tracking table.
- `go test -race -shuffle=on ./...` MUST pass for every module.
- Tests MUST be deterministic and hermetic: no real network access, no dependence on wall-clock
  timing or `time.Sleep` for synchronization, no shared state between tests. Use `t.TempDir`,
  `t.Cleanup`, `t.Setenv` and injected clocks or fakes.
- Tests that need external resources MUST be isolated behind a build tag or `testing.Short()`
  and MUST NOT run in the default `go test` invocation.
- Table-driven tests with named subtests (`t.Run`) are the default style.
- Each primary exported API MUST have at least one runnable `Example` function, which also
  serves as compile-checked documentation.
- Code that parses or decodes external input MUST have fuzz tests (`Fuzz*`); code declared
  performance-sensitive MUST have benchmarks (`Benchmark*`).
- Tests MUST use the standard `testing` package. Any third-party test dependency MUST be
  justified per Principle V.

**Rationale**: shared libraries are refactored for years by people (and agents) without the
original context; a fast, trustworthy test suite is what makes long-term change safe.

### IV. Idiomatic, High-Quality Go

Code MUST read as idiomatic Go and follow Effective Go and the Go Code Review Comments.

- Code MUST pass `golangci-lint` with zero findings, using the shared repository configuration.
  That configuration MUST enable the `gofmt` and `goimports` formatters and the `govet` linter,
  so formatting, vetting and static analysis are all enforced by this single tool. Inline
  `//nolint` directives MUST name the linter and state the reason.
- Every exported identifier MUST have a doc comment, and every package MUST have a package doc
  comment.
- Errors MUST NOT be silently discarded. Errors MUST be wrapped with context using `%w`; error
  conditions that callers need to branch on MUST be exposed as sentinel errors or error types
  usable with `errors.Is`/`errors.As`.
- Library code MUST NOT `panic` for recoverable conditions and MUST NOT call `os.Exit`,
  `log.Fatal` or equivalents.
- Library code MUST NOT write to stdout/stderr or use a global logger. If logging is needed, the
  caller injects a `*slog.Logger`.
- Functions that perform I/O or may block MUST accept a `context.Context` as their first
  parameter and honor cancellation.
- Packages MUST NOT hold global mutable state or perform side effects in `init()`. Configuration
  is passed explicitly (parameters, config structs or functional options).
- Every goroutine started by a library MUST have a defined owner and shutdown path; goroutine
  leaks are defects.

**Rationale**: consistent, idiomatic code lowers the cost of reading and changing every module,
and libraries must never impose process-level behavior on their consumers.

### V. Simplicity & Minimal Dependencies

The simplest design that satisfies the current, specified need wins.

- The standard library is the default. Each new third-party dependency (including test-only
  ones) MUST be justified in the feature plan, covering why the stdlib is insufficient, its
  maintenance status, its license and its transitive dependency footprint.
- Dependencies MUST use licenses compatible with this repository's MIT license.
- No speculative abstractions (YAGNI): interfaces, generics, options and extension points are
  introduced only when a concrete current use case requires them.
- Interfaces SHOULD be small and defined by the consumer; functions SHOULD accept interfaces
  and return concrete types.
- cgo is forbidden unless justified in the plan's Complexity Tracking table, because it breaks
  cross-compilation and plain `go get` installability.

**Rationale**: every dependency and abstraction is a long-term maintenance and security cost
inherited by every project that imports the module.

## Technology & Compatibility Constraints

- **Go toolchain support**: modules MUST support the two most recent major Go releases, matching
  the upstream Go support policy. The `go` directive in each `go.mod` MUST NOT exceed the older of
  the two. Raising a module's minimum Go version is a MINOR change and MUST be noted in its
  `CHANGELOG.md`.
- **Platforms**: modules MUST compile for `linux`, `darwin` and `windows` on `amd64` and `arm64`,
  unless the module's README explicitly declares a platform restriction. OS-specific code MUST
  use build constraints.
- **Repository layout**: shared tooling configuration (linter config, CI workflows, scripts)
  lives at the repository root and applies uniformly to every module. Module-specific overrides
  MUST be justified.
- **Dependency hygiene**: `go mod tidy -diff` MUST report no changes in any module.
- **Vulnerability scanning**: Trivy is the single vulnerability scanner for this repository and
  runs only in CI (see "Pull request gates"). `govulncheck` MUST NOT be added as a gate, locally
  or in CI, so that a single scanner is the source of truth for known vulnerabilities.
- **Secrets**: credentials, tokens and private endpoints MUST NOT appear in code, tests,
  fixtures or examples. Gitleaks enforces this rule in CI.

## Development Workflow & Quality Gates

- **Local gates**: before a change is pushed for review, every module it touches MUST pass, in
  isolation (`GOWORK=off`), these two checks and no others:
  1. `golangci-lint run ./...` reports zero findings (Principle IV);
  2. `go test -race -shuffle=on ./...` passes with statement coverage ≥ 80%, or the shortfall
     is justified per Principle III.
- **Pull request gates**: a GitHub Actions workflow MUST run on every pull request targeting
  `main`. A pull request MUST NOT be merged until all of the following pass, and they MUST be
  configured as required status checks through branch protection:
  1. the local gates for every touched module, with tests run on every supported Go version;
  2. `go mod tidy -diff` reports no changes;
  3. `gorelease` reports no incompatible API change for touched modules at `v1` or later
     (Principle II);
  4. **Trivy** reports no HIGH or CRITICAL vulnerabilities in any module's dependencies;
  5. an **SBOM** (CycloneDX or SPDX) is generated for every module and kept as a workflow
     artifact;
  6. **Gitleaks** detects no secrets in the commits of the pull request;
  7. **Semgrep** reports no findings from the configured rulesets;
  8. **Bearer** reports no security or privacy findings.
- **Finding suppression**: a security finding MAY be suppressed only through the tool's own
  ignore mechanism (`.trivyignore`, `.gitleaksignore`, `.semgrepignore` or `nosemgrep`,
  Bearer's ignore file), scoped as narrowly as the tool allows and accompanied by a written
  justification.
- **Change scope**: a change SHOULD touch a single module. Coordinated changes across modules
  MUST be released in dependency order, each module tagging its own version.
- **Commits**: commit messages MUST follow Conventional Commits with the module directory as
  scope (e.g. `feat(retry): add jittered backoff`, `fix(cache)!: ...` for breaking changes).
- **Release procedure**: update `CHANGELOG.md`, choose the version bump consistent with the
  `gorelease` report from the pull request workflow, then push the `<dir>/vX.Y.Z` tag.
- **Spec Kit integration**: every `plan.md` "Constitution Check" MUST evaluate Principles I–V
  and the constraints above. Any deviation MUST be recorded in its Complexity Tracking table
  with the reason and the simpler alternative that was rejected.

## Governance

- This constitution supersedes all other development practices and conventions in this
  repository. Agent runtime guidance files (e.g. `CLAUDE.md`) MUST NOT contradict it.
- **Amendments** are made by editing this file in a dedicated change with commit message
  `docs: amend constitution to vX.Y.Z (<summary>)`. Each amendment MUST state its rationale and
  include a migration note when existing modules become non-compliant.
- **Versioning policy** for this document follows Semantic Versioning:
  - MAJOR: a principle or governance rule is removed or redefined in a backward-incompatible way;
  - MINOR: a principle or section is added, or guidance is materially expanded;
  - PATCH: clarifications, wording and typo fixes with no semantic change.
- **Compliance review**: every feature plan passes the Constitution Check before Phase 0 and
  again after Phase 1 design; every change passes the local gates before it is pushed and the
  pull request gates before it is merged into `main`. Unjustified complexity is grounds for
  rejecting a change.
- **Periodic review**: the constitution MUST be reviewed at each new major Go release (about
  every six months) to update the toolchain support policy and tooling requirements.

**Version**: 1.1.0 | **Ratified**: 2026-10-16 | **Last Amended**: 2026-10-16

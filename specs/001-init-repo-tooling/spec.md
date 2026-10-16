# Feature Specification: Repository Tooling & CI/CD Bootstrap

**Feature Branch**: `001-init-repo-tooling` (suggested; not yet created)

**Created**: 2026-10-16

**Status**: Draft

**Input**: User description: "Inizializza il progetto: aggiungi il makefile per poter eseguire i lint e i test; crea un go.work; crea i file workflows di github. Il workflow di pr dovrà eseguire tutti i controlli necessari mentre il workflow di release dovrà creare una release su github"

## Context

The repository is meant to host many independent Go library modules (Constitution, Principle I),
but it does not contain any module or any tooling yet. This feature provides the shared,
repository-level tooling that every future module relies on:

- a single local entry point (Makefile) that runs the constitution's **local gates** (lint and
  test);
- a workspace file (`go.work`) for comfortable local development across modules;
- an automated **pull request workflow** that enforces every constitutional **pull request gate**
  plus the other constitution rules that can be checked automatically;
- an automated **release workflow** that publishes a GitHub release for a module version.

The tools named in this spec (linter, scanners, Makefile, workspace file, GitHub workflows) are
either explicitly requested deliverables or mandated by the constitution (v1.2.0); they are
constraints, not design choices made here.

**Actors**:

- **Contributor**: writes code in one or more modules and opens pull requests.
- **Maintainer**: reviews and merges pull requests, publishes module releases.
- **Consumer**: imports a module and reads its releases and release notes.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Run the local gates with one command (Priority: P1)

A contributor who has just changed a module wants to know, before pushing, whether the change
passes the local gates required by the constitution (lint with zero findings; race-enabled,
shuffled tests with at least 80% statement coverage). They run one command from the repository
root and get a clear pass/fail verdict for every module, or for only the module they are working
on.

**Why this priority**: the local gates are the first line of defense and the foundation the pull
request workflow reuses; without them no other part of the feature can be trusted or verified.

**Independent Test**: add a throwaway module with one passing test, run the lint and test
commands from the repository root, and verify the verdicts; then introduce a lint violation, a
failing test and a coverage drop, and verify that each is reported and makes the command fail.

**Acceptance Scenarios**:

1. **Given** a repository with two compliant modules, **When** the contributor runs the combined
   local-gates command, **Then** lint and tests run for both modules in isolation (workspace
   file ignored) and the command succeeds.
2. **Given** a module with a lint finding, **When** the contributor runs the lint command,
   **Then** the finding is shown with file and line, the module is named in a final summary, and
   the command exits with a failure status.
3. **Given** a module whose statement coverage is 70%, **When** the contributor runs the test
   command, **Then** the command fails and reports the module name, the measured coverage and
   the required threshold.
4. **Given** several modules, **When** the contributor restricts a command to one module,
   **Then** only that module is checked.
5. **Given** two modules where the first one fails, **When** the contributor runs a command on
   all modules, **Then** the second module is still checked and the summary lists every failing
   module.

---

### User Story 2 - Every pull request is gated automatically (Priority: P2)

A contributor opens a pull request targeting `main`. Without any manual action, every
constitutional pull request gate runs, together with the other constitution rules that can be
checked automatically (module structure, platform support, the pull request title, changelog
updates),
and each reports its own status on the pull request. The maintainer can rely on those statuses,
configured as required checks, to know whether the pull request may be merged.

**Why this priority**: it is the enforcement point of the constitution; it depends on the local
gates (Story 1) and is a prerequisite for safe releases (Story 3).

**Independent Test**: on a test branch, open pull requests that (a) change only documentation,
(b) change a compliant throwaway module, (c) introduce a known vulnerable dependency, (d) commit
a fake secret, (e) use a non-conventional pull request title, (f) change module code without a
changelog update; verify that every status is conclusive and that only the expected ones fail.

**Acceptance Scenarios**:

1. **Given** a pull request that changes one module, **When** it is opened or updated, **Then**
   the local gates run for that module on every supported Go version, and all other pull request
   gates (dependency tidiness, API compatibility, vulnerability scan, SBOM, secret scan, static
   analysis, security/privacy scan) run and each reports its own named status.
2. **Given** a pull request that changes only documentation, **When** it is opened, **Then**
   every required status still reports a conclusive result (none remains pending) and the module
   gates succeed without checking any module.
3. **Given** a pull request that changes the shared linter configuration or the CI tooling,
   **When** it is opened, **Then** the local gates run for every module.
4. **Given** a pull request that adds a dependency with a known HIGH or CRITICAL vulnerability,
   **When** the checks run, **Then** the vulnerability gate fails and names the dependency and
   the advisory.
5. **Given** a pull request that introduces an incompatible API change in a module already at
   `v1.x`, **When** the checks run, **Then** the API compatibility gate fails; for a module at
   `v0.x` or without any release, the report is shown but the gate does not fail.
6. **Given** any pull request, **When** the checks complete, **Then** an SBOM for every module is
   available as a downloadable result of that run, and the API compatibility report (including
   the suggested next version of each touched module) is visible in the run summary.
7. **Given** a new commit pushed to a pull request whose checks are still running, **When** the
   new run starts, **Then** the outdated run is cancelled.
8. **Given** a module whose module definition contains a `replace` directive, or that lacks a
   `README.md` or `CHANGELOG.md`, or that declares a minimum Go version newer than the oldest
   supported one, **When** the checks run, **Then** the structural status fails and names the
   module and the violated rule.
9. **Given** a touched module that does not compile for `windows/arm64` and declares no platform
   restriction, **When** the checks run, **Then** the cross-platform status fails and names the
   platform.
10. **Given** a pull request titled `fixed stuff`, **When** the checks run, **Then** the commit
    message status fails and quotes the title; branch commits such as `wip` are not checked,
    because the pull request is squash-merged with its title as the commit message.
11. **Given** a pull request that changes the source of module `retry` without changing
    `retry/CHANGELOG.md`, **When** the checks run, **Then** the changelog status fails; **When** a
    maintainer then applies the changelog waiver, **Then** the status is re-evaluated and passes.

---

### User Story 3 - Publish a module release by pushing a tag (Priority: P3)

After merging changes and updating a module's changelog, the maintainer pushes a tag
`<dir>/vX.Y.Z`. A GitHub release for that module version is created automatically, with release
notes taken from the module's changelog, so consumers can see what changed.

**Why this priority**: releases are less frequent than pull requests and only make sense once
modules exist and are gated, but they are the way value reaches consumers.

**Independent Test**: on a test copy of the repository, push a valid tag for a throwaway module
with a matching changelog entry and verify that the release appears with the right notes; then
push invalid tags (bad format, missing changelog entry, non-existent module, commit not on
`main`) and verify that no release is created and the failure reason is clear.

**Acceptance Scenarios**:

1. **Given** module `retry` with a changelog section for `1.2.0` on `main`, **When** the
   maintainer pushes tag `retry/v1.2.0`, **Then** a release titled `retry/v1.2.0` is created
   with that changelog section as notes and the module's SBOM attached.
2. **Given** tag `retry/v1.3.0-rc.1`, **When** it is pushed, **Then** the release is marked as a
   pre-release.
3. **Given** a tag whose version has no section in the module's changelog, **When** it is
   pushed, **Then** no release is created and the run fails stating the missing entry.
4. **Given** a tag `cache/v2.0.0` for a module whose path lacks the `/v2` suffix, **When** it is
   pushed, **Then** no release is created and the run fails stating the major-version mismatch.
5. **Given** a tag pointing to a commit that is not on `main`, **When** it is pushed, **Then** no
   release is created.
6. **Given** a release that already exists for a tag, **When** the release run is re-executed,
   **Then** no duplicate release is created and the existing release is left unchanged.

---

### User Story 4 - Develop across modules with a workspace (Priority: P4)

A contributor working on several modules at once opens the repository in their editor and gets
code navigation, completion and builds across all modules, thanks to a workspace file at the
repository root. When a module is added or removed, one command brings the workspace file back in
sync.

**Why this priority**: it improves day-to-day comfort but no gate depends on it (Principle I).

**Independent Test**: add two throwaway modules, run the workspace sync command, and verify the
workspace file lists exactly those modules and that the editor resolves symbols across them;
then verify that the local gates give the same verdict with and without the workspace file.

**Acceptance Scenarios**:

1. **Given** a newly added module, **When** the contributor runs the workspace sync command,
   **Then** the workspace file lists every module present and no module that no longer exists.
2. **Given** a workspace file that is out of date or missing, **When** any local gate or pull
   request gate runs, **Then** its verdict is unaffected.

---

### Edge Cases

- **No modules yet** (the current state): every local command and every pull request gate
  completes successfully with an explicit "no modules found" message; the workspace file is valid
  with no modules listed.
- **New module added in a pull request**: it is treated as touched; the API compatibility gate
  has no previous release to compare with and reports without failing.
- **Module at major version 2 or later**: the API compatibility gate compares against the latest
  release of the same major version; the release tag prefix is the module directory without the
  `/vN` suffix.
- **Linter missing or at the wrong version locally**: the lint command fails immediately with a
  message stating the expected version and how to install it, instead of producing misleading
  results.
- **Module with a justified coverage exception**: the test command applies the lower threshold
  declared for that module and shows the justification in its output.
- **Pull request from a fork**: every gate runs without repository secrets and reports a
  conclusive result.
- **Scanner or advisory database unavailable**: the affected gate fails (fail closed); it never
  reports success without having scanned.
- **Suppressed finding**: a finding listed in the tool's own ignore file at the repository root
  does not fail the gate; suppressions anywhere else have no effect.
- **Tag not matching `<dir>/vX.Y.Z`** (e.g. `v1.0.0`, `retry/1.0.0`) or pointing to a directory
  without a module: no release is created and the run states why.
- **Branch commits** (including `wip`, `fixup!` or merges of `main` into the branch) are never
  checked: only the pull request title reaches `main`, as the squash commit message.
- **Repository-level pull requests** (CI, docs, constitution amendments) that touch no module may
  omit the scope or use a declared repository-level scope.
- **Pull request touching several modules** (a coordinated change, discouraged but allowed): its
  title may use any of the touched modules or a repository-level scope; the changelog check still
  applies to each touched module.
- **Pull request changing only tests or documentation of a module**: no changelog change is
  required.
- **Pull request title edited after opening**: the commit message status is re-evaluated.

## Requirements *(mandatory)*

### Functional Requirements

**Local gates (Makefile)**

- **FR-001**: The repository MUST provide, at its root, a single command entry point offering:
  a lint command, a test command, a combined command running both (the local gates), a workspace
  sync command, and a help command listing all commands with a one-line description.
- **FR-002**: Every command MUST discover modules automatically (each top-level directory, or its
  `/vN` subdirectory, containing a module definition); adding or removing a module MUST NOT
  require editing any tooling file.
- **FR-003**: Every command MUST accept an optional restriction to a single module.
- **FR-004**: Every command MUST check each module in isolation, from the module's own directory,
  with the workspace file disabled.
- **FR-005**: The lint command MUST use one shared, repository-level linter configuration that
  enables the formatting checks (`gofmt`, `goimports`) and vet analysis required by Principle IV,
  and MUST fail on any finding.
- **FR-006**: The test command MUST run tests with the race detector and randomized test order,
  MUST measure statement coverage per module, and MUST fail if a module is below 80%.
- **FR-007**: A module MAY declare a lower coverage threshold only in a single versioned
  exceptions file at the repository root, together with a written justification; the test command
  MUST apply and display it.
- **FR-008**: When checking several modules, a command MUST continue after a module fails, MUST
  end with a summary of every module and its outcome, and MUST exit with a failure status if any
  module failed.
- **FR-009**: The linter version MUST be declared in exactly one place and be the same for local
  commands and for the pull request workflow; a missing or mismatched local linter MUST produce a
  clear error with installation instructions.

**Workspace (go.work)**

- **FR-010**: The repository MUST contain, committed under version control, a workspace file at
  its root (together with its checksum file, once one exists) listing every module, whose Go
  version does not exceed the oldest supported Go version.
- **FR-011**: The workspace sync command MUST add every discovered module to the workspace file
  and remove entries for modules that no longer exist.
- **FR-012**: No local gate, pull request gate or release step MAY depend on the workspace file.

**Pull request workflow**

- **FR-013**: A workflow MUST run automatically whenever a pull request targeting `main` is
  opened, updated with new commits, reopened, edited (title change), or has its changelog waiver
  (FR-033) added or removed.
- **FR-014**: The workflow MUST determine the touched modules (modules containing changed files);
  when shared tooling changes (linter configuration, workflows, Makefile, exceptions file,
  allowed commit scopes, scanner configuration), every module MUST be considered touched.
- **FR-015**: The workflow MUST run the local gates (FR-005, FR-006) for every touched module on
  each supported Go version (the two most recent major Go releases, declared in one place).
- **FR-016**: The workflow MUST verify that dependency tidying reports no changes in any module.
- **FR-017**: The workflow MUST check API compatibility of every touched module against its latest
  release of the same major version; it MUST fail on incompatible changes for modules at `v1` or
  later, MUST NOT fail for modules at `v0` or without a release, and MUST publish each report,
  including the suggested next version, in the run summary.
- **FR-018**: The workflow MUST scan the dependencies of every module for known vulnerabilities
  and fail on any HIGH or CRITICAL finding, naming the dependency and the advisory.
- **FR-019**: The workflow MUST generate an SBOM (CycloneDX or SPDX) for every module and retain
  it as a downloadable result of the run.
- **FR-020**: The workflow MUST scan the commits of the pull request for secrets and fail on any
  finding.
- **FR-021**: The workflow MUST run static analysis with rulesets declared in versioned
  configuration in the repository and fail on any finding.
- **FR-022**: The workflow MUST run a security and privacy scan and fail on any finding.
- **FR-023**: The workflow MUST honor finding suppressions only through each tool's own ignore
  mechanism, configured at the repository root.
- **FR-024**: Each gate MUST be reported as a separate status with a stable name, and every status
  MUST reach a conclusive result (success or failure) on every pull request, including pull
  requests that touch no module, so that all of them can be configured as required checks.
- **FR-025**: The workflow MUST NOT require repository secrets, so pull requests from forks are
  fully checked.
- **FR-026**: Any gate whose tool or data source fails MUST fail rather than pass (fail closed).
- **FR-027**: A newer run for the same pull request MUST cancel any older run still in progress.
**Pull request workflow — additional constitution compliance checks**

Beyond the constitutional pull request gates 1–8, the workflow MUST enforce the following rules of
the constitution that can be checked automatically. They are reported through named statuses
under the same rules as FR-024 to FR-026: one structural status (FR-028 to FR-030) and one
status each for FR-031, FR-032 and FR-033.

- **FR-028**: No module definition of any module MAY contain `replace` directives (Principle I).
- **FR-029**: Every module MUST contain a `README.md` and a `CHANGELOG.md` (Principle I).
- **FR-030**: The minimum Go version declared by every module MUST NOT exceed the oldest supported
  Go version (Technology & Compatibility Constraints).
- **FR-031**: Every touched module MUST compile for `linux`, `darwin` and `windows` on `amd64` and
  `arm64`, except for platforms excluded by a restriction declared for that module in the
  exceptions file (FR-007), which MUST match the restriction stated in the module's README.
- **FR-032**: The pull request title, which becomes the squash commit message on `main`, MUST
  follow Conventional Commits (including `!` for breaking changes). A scope, when present, MUST be
  an existing module directory or one of the repository-level scopes declared in versioned
  configuration; a pull request whose changes are confined to a single module MUST use that
  module's directory as scope. Branch commits MUST NOT be checked. The failure MUST quote the
  title and state the reason.
- **FR-033**: When a pull request changes a module's non-test source files or its module
  definition, that module's `CHANGELOG.md` MUST also be changed in the same pull request. A
  maintainer MUST be able to waive this check for a specific pull request through an explicit
  marker visible on the pull request; changes limited to tests, README or other documentation
  MUST NOT require a changelog change.

**Release workflow**

- **FR-034**: A workflow MUST run automatically when a tag matching `<dir>/vX.Y.Z` (optionally
  with a pre-release suffix, e.g. `-rc.1`) is pushed.
- **FR-035**: Before creating a release, the workflow MUST verify that: the tag format is valid;
  the directory contains a module; the module path's major-version suffix matches the tag's major
  version; the tagged commit is reachable from `main`; the module's changelog contains a section
  for the version; and the local gates pass for that module at the tagged commit. If any check
  fails, no release MUST be created and the run MUST state the reason.
- **FR-036**: The workflow MUST create a GitHub release named after the tag, whose notes are the
  module's changelog section for that version.
- **FR-037**: Versions with a pre-release suffix MUST be published as pre-releases.
- **FR-038**: The workflow MUST attach the module's SBOM to the release.
- **FR-039**: Re-running the workflow for a tag that already has a release MUST NOT create a
  duplicate or modify the existing release.
- **FR-040**: The workflow MUST NOT create, move or delete tags.

**Workflow security**

- **FR-041**: Each workflow MUST run with the least permissions it needs: read-only for the pull
  request workflow; write access limited to creating releases for the release workflow.
- **FR-042**: Every third-party automation component used by the workflows MUST be pinned to an
  immutable reference.

**Merge policy**

- **FR-043**: The repository MUST provide a versioned branch protection definition for `main`
  that requires every status of FR-024, allows only squash merges and requires a linear history
  (constitution v1.2.0, "Merging").

### Key Entities

- **Module**: a top-level directory containing a module definition; has a path, a minimum Go
  version, a README and a changelog; unit of linting, testing, scanning and releasing.
- **Release tag**: `<dir>/vX.Y.Z[-prerelease]`; identifies one immutable version of one module.
- **Changelog entry**: the section of a module's changelog for one version; source of the release
  notes.
- **Gate**: a named automated check with a conclusive pass/fail outcome; either local (lint, test)
  or pull request (constitutional gates 1–8 and the compliance checks FR-028 to FR-033).
- **Module exception**: module name, kind (lowered coverage threshold or platform restriction),
  value and written justification; all exceptions are kept in one versioned file.
- **Repository-level scope**: a Conventional Commit scope allowed for changes that do not belong
  to a module (e.g. CI, docs), declared in versioned configuration.
- **Changelog waiver**: an explicit, visible marker on a pull request, applied by a maintainer,
  that disables the changelog check for that pull request only.
- **Workspace file**: list of all modules used for local development only.
- **SBOM**: inventory of a module's dependencies, produced per module on every pull request and
  attached to every release.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: A new contributor can run all local gates for all modules with a single command,
  following only the README, within 10 minutes of cloning the repository.
- **SC-002**: Adding a new module requires zero edits to tooling files for it to be covered by the
  local gates, the pull request gates and the release workflow.
- **SC-003**: 100% of pull requests targeting `main` receive a conclusive result on every required
  status; none stays pending indefinitely.
- **SC-004**: For the same commit and Go version, the lint and test verdicts obtained locally match
  those of the pull request workflow in 100% of cases.
- **SC-005**: Each of the 8 constitutional pull request gates and each of the 6 additional
  compliance checks (FR-028 to FR-033) is enforced by at least one named, automated status (100%
  coverage of both lists).
- **SC-006**: A typical pull request touching one module receives all its statuses within 15
  minutes.
- **SC-007**: After updating the changelog, the maintainer publishes a release with exactly one
  action (pushing a tag), and the release is visible within 10 minutes.
- **SC-008**: In a test campaign with at least one invalid tag of each kind listed in FR-035, 100%
  of invalid tags produce no release and a failure message naming the reason.

## Assumptions

- The repository is public, hosted on GitHub under a personal account, with GitHub Actions
  enabled; every scanner required by the constitution can run there without paid licenses or
  repository secrets.
- Configuring branch protection (marking the statuses as required) is a manual administrative
  step outside this feature; the feature documents the exact status names to require.
- No module exists yet; the feature is verified with temporary throwaway modules on a test branch
  that is not merged.
- The two supported Go versions are declared in one place and updated by hand at each new major
  Go release, in line with the constitution's periodic review.
- Contributors run local commands on Linux or macOS with `make` and Go installed; native Windows
  without `make` is not supported for local commands (CI still covers Windows where required).
- The coverage threshold applies to each module's total statement coverage.
- Security scanners run only in CI, as required by the constitution; they are not exposed as local
  commands.
- Modules are libraries: releases carry release notes and the SBOM, no compiled binaries.
- Changelogs follow Keep a Changelog, with one heading per released version.
- The changelog waiver is a pull request marker (e.g. a label) that only users with write access
  to the repository can apply.
- Pull requests are squash-merged (constitution v1.2.0), and the repository setting makes the
  squash commit take the pull request title, so the title is the only message to check.
- The workspace file is committed so that a fresh clone is ready for cross-module development;
  the Go documentation's general advice against committing it is outweighed here because no gate
  uses it (FR-012).
- Out of scope: automated dependency updates, automated changelog generation, automatic version
  bumping or tagging, branch protection setup, documentation site.

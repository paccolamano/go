---

description: "Task list for Repository Tooling & CI/CD Bootstrap"
---

# Tasks: Repository Tooling & CI/CD Bootstrap

**Input**: Design documents from `specs/001-init-repo-tooling/`

**Prerequisites**: [plan.md](plan.md), [spec.md](spec.md), [research.md](research.md), [data-model.md](data-model.md), [contracts/](contracts/), [quickstart.md](quickstart.md)

**Tests**: INCLUDED. Constitution Principle III (NON-NEGOTIABLE) requires test tasks to precede
the implementation they cover. Each `*_test.sh` task MUST be written first and MUST be seen
failing (`scripts/tests/run.sh <name>`) before its implementation task starts. Approach and
deviation: [plan.md, Complexity Tracking](plan.md#complexity-tracking).

**Organization**: tasks are grouped by user story, so each story can be implemented and
validated on its own.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: can run in parallel (different files, no dependency on an incomplete task)
- **[Story]**: the user story the task belongs to (US1–US4)

## Conventions that apply to every task

- **Shell portability** ([research.md R-02](research.md#r-02-implementation-language-for-repository-tooling)):
  - every script starts with `#!/usr/bin/env bash` and `set -euo pipefail`;
  - it must run on **Bash 3.2**, so no `mapfile`/`readarray`, no associative arrays, no
    `${var,,}`/`${var^^}`, no `|&`;
  - it uses POSIX utilities only, so no `grep -P`, no `sed -i`, no `sort -V`, no `find -printf`,
    no `readlink -f`;
  - versions are compared in `awk`; SHA-256 uses `sha256sum`, falling back to `shasum -a 256`.
- **Makefile**: must run on **GNU Make 3.81**, so no `.ONESHELL`, `!=` or `$(file …)`.
- **Script interface** ([contracts/makefile.md](contracts/makefile.md#script-interface-shared-by-make-and-ci)):
  - `--all` discovers every module, explicit `<root>…` checks exactly those modules, and no
    arguments prints `no modules to check` and exits 0;
  - exit codes are `0` pass, `1` check failed, `2` usage or environment error;
  - module loops keep going after a failure and end with a `==> summary` block of
    `PASS|FAIL  <root>  [reason]` lines;
  - when `GITHUB_ACTIONS=true`, the summary is also appended to `$GITHUB_STEP_SUMMARY`.
- **Go commands** always run from the module root with `GOWORK=off`. Nothing is written inside a
  module directory; temporary files go to `mktemp -d`, removed by `trap … EXIT`.
- **Tests** are plain Bash:
  - each `scripts/tests/<name>_test.sh` defines `test_*` functions, and `scripts/tests/run.sh`
    runs them;
  - fixtures are built in `mktemp -d` with the helpers from T005;
  - tests never touch the real repository, the network or real scanners. External tools are
    replaced by shim executables on a temporary `PATH`, except `go` and `git`, which are real
    where noted.
- **Pinned components**: every `uses:` in a workflow is a full 40-char SHA followed by
  `# vX.Y.Z`, with values taken from [research.md R-04](research.md#r-04-pinned-components).

---

## Phase 1: Setup (shared infrastructure)

**Purpose**: create the repository-level configuration files that every later phase reads.

- [X] T001 Create the directories `scripts/tests/` and `.github/workflows/`. Create the root `.gitignore` with exactly these entries, each preceded by a one-line comment: `/bin/`, `/sbom/`, `*.cdx.json`, `cover.out`.
- [X] T002 Create `versions.env`, the single source of truth for versions.
  - **Format** (contract, verbatim): "`KEY=value` lines, `#` comments, no quotes and no spaces around `=`". Start with a header comment saying the file is read by the Makefile (`include`), by the scripts (`. ./versions.env`) and by the workflows (`$GITHUB_ENV`).
  - **Fixed values**: `GO_VERSION_OLDEST=1.26`, `GO_VERSION_NEWEST=1.27`, `GOLANGCI_LINT_VERSION=v2.14.0`, `GORELEASE_VERSION=v0.0.0-20260824195058-e88cd73687aa`, `TRIVY_VERSION=0.75.0`, `GITLEAKS_VERSION=8.30.1`, `BEARER_VERSION=2.1.1`.
  - **Hashes**: compute `TRIVY_SHA256`, `GITLEAKS_SHA256` and `BEARER_SHA256` by downloading each tarball once and running `sha256sum` on it:
    - `https://github.com/aquasecurity/trivy/releases/download/v0.75.0/trivy_0.75.0_Linux-64bit.tar.gz`
    - `https://github.com/gitleaks/gitleaks/releases/download/v8.30.1/gitleaks_8.30.1_linux_x64.tar.gz`
    - `https://github.com/Bearer/bearer/releases/download/v2.1.1/bearer_2.1.1_linux_amd64.tar.gz`

    Cross-check each hash against that release's checksums file, and stop if they differ.
  - **Semgrep image**: set `SEMGREP_IMAGE=semgrep/semgrep:1.178.0@sha256:<digest>`, resolving the digest with `docker buildx imagetools inspect semgrep/semgrep:1.178.0` (or the Docker Hub registry API).
- [X] T003 [P] Create `.github/commit-scopes`. Add a header comment with the rule (verbatim from the data model): "Lowercase, `^[a-z0-9][a-z0-9._-]*$`, and it must not collide with a module directory". Then list one scope per line: `ci`, `deps`, `docs`, `repo`, `specs`.
- [X] T004 [P] Create `.module-exceptions` with comment lines only. Document the columns `module kind value justification`, the kinds `coverage` ("integer 0–79") and `platforms` ("comma-separated `GOOS/GOARCH` list of the platforms the module **does** support"), and the rule that a justification is required. Include the two commented-out examples from [contracts/config-files.md](contracts/config-files.md#module-exceptions).

---

## Phase 2: Foundational (blocking prerequisites)

**Purpose**: the test harness, the shared library and the tool installer that every story uses.

**⚠️ CRITICAL**: no user story work can begin until this phase is complete.

- [X] T005 Create `scripts/tests/helpers.sh`, sourced by every test file. It provides:
  - **assertions**: `assert_eq <expected> <actual> [msg]`, `assert_contains <haystack> <needle> [msg]`, `assert_not_contains`, and `assert_exit <code> <cmd…>`. `assert_exit` captures the command's stdout and stderr into `$OUT` and fails with a diff-like message.
  - **fixture builders**:
    - `make_repo` creates a temporary git repo with a local `user.name`/`user.email` and an initial commit on branch `main`, and echoes its path.
    - `make_module <repo> <root> <module-path> [go-version=1.26.0]` writes `go.mod`, `README.md` and `CHANGELOG.md` (`# Changelog`), plus a `doc.go` with a package doc comment and one exported, documented function `Hello() string`, and a `hello_test.go` that fully covers it.
    - `make_shim <dir> <name> <script-body>` writes an executable shim.
  - **cleanup**: every temporary directory is registered and removed by an `EXIT` trap.
- [X] T006 Create `scripts/tests/run.sh`.
  - It sources `helpers.sh`, then for each `scripts/tests/*_test.sh` (or only `<name>_test.sh` when a name argument is given) runs every `test_*` function in its own subshell, listing them with `declare -F | awk '{print $3}' | grep '^test_'`.
  - It prints `PASS|FAIL <file>::<test>` and a final count, and exits 1 if any test failed.
  - It works with zero test files.
- [X] T007 [P] Write the failing tests for module discovery and target resolution in `scripts/tests/lib_test.sh`:
  - `discover_modules` lists every non-hidden top-level directory that has a `go.mod`, plus each `<dir>/vN/go.mod` with N ≥ 2;
  - it ignores nested `go.mod` files elsewhere (for example `alpha/testdata/x/go.mod`) and hidden directories;
  - its output is sorted;
  - `resolve_targets --all` returns all modules, `resolve_targets alpha` returns `alpha`, and no arguments prints `no modules to check` and signals "nothing to do" with exit 0;
  - an unknown module exits 2 and lists the discovered modules.
- [X] T008 [P] Write the failing tests for the `.module-exceptions` parser in `scripts/tests/exceptions_test.sh`:
  - comment and blank lines are ignored;
  - `exception_value <module> coverage` returns the declared value, or empty when there is no entry;
  - `coverage_threshold <module>` returns the declared value or `80`;
  - `exception_justification` returns the text after the third column;
  - malformed lines exit 2 with the file, the line number and the expected format. Cases to cover:
    - a missing justification;
    - an unknown kind;
    - a coverage value outside "integer 0–79" (for example `80` or `abc`);
    - a platform outside the six `{linux,darwin,windows}/{amd64,arm64}`;
    - a duplicate (`module`, `kind`), which breaks "At most one line per (`module`, `kind`)".
- [X] T009 Implement `scripts/lib.sh` so that T007 and T008 pass. Functions:
  - `log`, `warn`, `die <code> <msg>`, `repo_root` (via `git rev-parse --show-toplevel`, with an override in `REPO_ROOT` for tests);
  - `load_versions`, which sources `versions.env`;
  - `discover_modules` and `resolve_targets "$@"`, which sets the global `TARGETS` (space-separated) and handles `--all`, an explicit list and the empty case;
  - `parse_exceptions` (validates the whole file once), `exception_value`, `coverage_threshold` and `exception_justification`;
  - `summary_add <PASS|FAIL> <root> [reason]` and `summary_print`, which prints `==> summary` and appends to `$GITHUB_STEP_SUMMARY` when `GITHUB_ACTIONS=true`; it returns 1 if any line is FAIL.

  Follow every shell portability rule listed above.
- [X] T010 [P] Write the failing tests for the pinned-binary installer in `scripts/tests/install_tool_test.sh`. Build a fixture tarball containing a fake `gitleaks` binary, set `INSTALL_TOOL_BASE_URL=file://<dir>`, and write a temporary `versions.env` with its hash. Check that:
  - a matching hash extracts only the binary into `<dest>` with mode +x;
  - a mismatched hash exits non-zero, prints the expected and actual hashes, and leaves nothing in `<dest>`;
  - an unknown tool name exits 2.
- [X] T011 Implement `scripts/install-tool.sh <trivy|gitleaks|bearer> <dest-dir>` so that T010 passes.
  - The base URL is `${INSTALL_TOOL_BASE_URL:-https://github.com}`, with these asset paths:
    - Trivy: `aquasecurity/trivy/releases/download/v${TRIVY_VERSION}/trivy_${TRIVY_VERSION}_Linux-64bit.tar.gz`
    - Gitleaks: `gitleaks/gitleaks/releases/download/v${GITLEAKS_VERSION}/gitleaks_${GITLEAKS_VERSION}_linux_x64.tar.gz`
    - Bearer: `Bearer/bearer/releases/download/v${BEARER_VERSION}/bearer_${BEARER_VERSION}_linux_amd64.tar.gz`
  - Download with `curl -fsSL` into `mktemp -d`, verify against `<TOOL>_SHA256` from `versions.env`, and never trust the release checksum file. Then extract only the named binary.
- [X] T012 Create the `Makefile` skeleton:
  - `SHELL := bash`, `include versions.env`, `.DEFAULT_GOAL := help`, and `.PHONY` for every target;
  - a `help` target that prints every target annotated with a trailing `## description`, extracted with `awk` from `$(MAKEFILE_LIST)` (Make 3.81 compatible);
  - a `scripts-test` target that runs `scripts/tests/run.sh` and then `shellcheck scripts/*.sh scripts/tests/*.sh`. When `shellcheck` is missing, it exits 2 with the hint "install shellcheck (needed only for tooling development)".

**Checkpoint**: `make scripts-test` passes, with lib, exceptions and install_tool green.

---

## Phase 3: User Story 1 - Run the local gates with one command (Priority: P1) 🎯 MVP

**Goal**: `make lint`, `make test` and `make check` run the constitution's local gates on every
module, or on one module with `MODULE=`, each module in isolation, with a summary and the right
exit code.

**Independent Test**: [quickstart.md §1](quickstart.md#1-local-gates-user-story-1). With two
throwaway modules, `make check` passes, and each failure scenario in the table produces the
documented output and exit code.

### Tests for User Story 1 ⚠️ (write first, see them fail)

- [X] T013 [P] [US1] Write `scripts/tests/lint_test.sh`, using a shim `golangci-lint` on a temporary `PATH` that logs its working directory, `GOWORK` and arguments, and answers `version --short`. Check that:
  - when the shim reports a version other than `GOLANGCI_LINT_VERSION` (without the leading `v`), the script exits 2 and the output contains the expected version, the found version, and `curl -sSfL https://golangci-lint.run/install.sh | sh -s -- -b "$(go env GOPATH)/bin" v2.14.0`;
  - when the binary is missing, the script exits 2;
  - with the correct version, the shim runs once per module from the module root with `GOWORK=off` and the arguments `run --config <repo>/.golangci.yml ./...`;
  - when the shim fails for `alpha`, `beta` is still linted, the summary contains `FAIL  alpha`, and the exit code is 1;
  - with no arguments, the output is `no modules to check` and the exit code is 0.
- [X] T014 [P] [US1] Write `scripts/tests/gotest_test.sh`, which uses the real `go` toolchain on fixture modules from `make_module`. Check that:
  - a fully covered module passes;
  - adding an untested exported function that drops coverage below 80% prints `FAIL  <root>  coverage NN.N% < 80% threshold` and exits 1;
  - with `<root> coverage 50 <reason>` in a temporary `.module-exceptions`, the module passes and the output shows `threshold 50` and the reason;
  - a module whose only file is a `doc.go` with no statements passes, with `no statements`;
  - after a run, `git status --porcelain` inside the fixture is empty (no `cover.out` in the module);
  - a failing first module does not stop the second one.

### Implementation for User Story 1

- [X] T015 [P] [US1] Create `.golangci.yml` (golangci-lint v2, `version: "2"`), following [research.md R-05](research.md#r-05-linter-configuration-golangci-lint-v2).
  - **Formatters**: `gofmt` and `goimports`, with `local-prefixes: [github.com/paccolamano/go]`.
  - **Linters**: `default: standard`, plus `revive` (rules `exported` and `package-comments`), `errorlint`, `errname`, `wrapcheck`, `forbidigo`, `gochecknoinits`, `gochecknoglobals`, `noctx`, `contextcheck`, `nolintlint`, `usetesting`, `thelper` and `misspell`.
  - **`forbidigo`**: forbid `^fmt\.Print`, `^print(ln)?$`, `^os\.Exit$`, `^log\.` and `^panic$`, with `exclude-godoc-examples: true`.
  - **`nolintlint`**: `require-explanation: true` and `require-specific: true`.
  - **Exclusions**: exclude `forbidigo`, `gochecknoglobals` and `wrapcheck` for `_test\.go`. Do **not** enable the `comments` exclusion preset.
  - Add a header comment mapping each linter to the Principle IV rule it enforces.
  - Validate with `golangci-lint config verify`.
- [X] T016 [US1] Implement `scripts/lint.sh` so that T013 passes.
  - Use `resolve_targets`, then the version check: compare `golangci-lint version --short` with `${GOLANGCI_LINT_VERSION#v}`, and exit 2 with the install command on a mismatch or when the binary is missing.
  - Per module, run `(cd <root> && GOWORK=off golangci-lint run --config "$REPO_ROOT/.golangci.yml" ./...)`, then `summary_add` and `summary_print`.
- [X] T017 [US1] Implement `scripts/test.sh` so that T014 passes.
  - Per module, run `GOWORK=off go test -race -shuffle=on -covermode=atomic -coverprofile="$tmp/<root>.out" ./...` from the module root.
  - Compute the total with `go tool cover -func` (`total:` line). A profile with no lines after `mode:` passes as `no statements`.
  - Compare the total with `coverage_threshold`, using `awk` for the floating-point comparison, and print the threshold and the justification when an exception applies.
  - Exit 2 with the hint "install gcc/clang (Xcode CLT on macOS)" when `go env CC` cannot be found.
- [X] T018 [US1] Add the `Makefile` targets `lint` (`## Run golangci-lint on every module, or MODULE=<root>`), `test` (`## Run race+shuffle tests with the coverage gate`) and `check` (`## Run the local gates: lint, then test`). Each calls its script with `$(if $(MODULE),$(MODULE),--all)`. `check` runs exactly `lint` and then `test`, and nothing else.
- [X] T019 [US1] Validate [quickstart.md §1](quickstart.md#1-local-gates-user-story-1) end to end on a local scratch branch `scratch/tooling-check` with the throwaway modules `alpha` and `beta`. Run every row of the failure table, then delete the throwaway modules (never commit them).

**Checkpoint**: US1 is fully functional. `make check` is the MVP local gate.

---

## Phase 4: User Story 2 - Every pull request is gated automatically (Priority: P2)

**Goal**: `pr.yml` and `pr-meta.yml` report every status from
[contracts/ci-checks.md](contracts/ci-checks.md) on every pull request targeting `main`, each with
a conclusive result.

**Independent Test**: [quickstart.md §3](quickstart.md#3-pull-request-checks-user-story-2). On a
draft PR from a scratch branch, each commit row produces exactly the expected statuses.

**Depends on**: US1 (the `lint` and `test` jobs call `scripts/lint.sh` and `scripts/test.sh`).

### Tests for User Story 2 ⚠️ (write first, see them fail)

- [X] T020 [P] [US2] Write `scripts/tests/modules_test.sh` (real `git`, fixture repo with modules `alpha`, `alpha/v2` and `beta`). Check `modules.sh touched <base> <head>`:
  - a change in `alpha/x.go` yields `alpha`;
  - a change in `alpha/v2/x.go` yields `alpha/v2` only;
  - a change in the root `README.md` only yields empty output;
  - a change to each shared tooling path (`.golangci.yml`, `versions.env`, `Makefile`, `scripts/lib.sh`, `.github/workflows/pr.yml`, `.github/commit-scopes`, `.module-exceptions`, `.trivyignore`, `.gitleaksignore`, `.semgrepignore`, `bearer.yml`, `bearer.ignore`) yields all modules;
  - a change to `go.work` does **not** yield all modules;
  - a commit added to `main` after the branch point is not counted (three-dot diff);
  - a module deleted in the head is not listed.

  Also check that `modules.sh list` equals `discover_modules`, and that `modules.sh json-go-versions` prints `["1.26","1.27"]` from `versions.env`.
- [X] T021 [P] [US2] Write `scripts/tests/structure_test.sh` (real `go`). Each case fails with `<root>: <rule>` and exit 1:
  - `replace` in single-line form and in block form;
  - missing `README.md`;
  - missing `CHANGELOG.md`;
  - a `go 1.27.0` directive while `GO_VERSION_OLDEST=1.26` (a module with `go 1.26.3` passes);
  - a top-level module directory whose name does not match `^[a-z0-9][a-z0-9._-]*$`;
  - a `.module-exceptions` line naming a module that does not exist ("Lines naming a module that does not exist make the `structure` status fail");
  - a malformed exception line, which exits 2.

  A compliant set of modules passes, and so does zero modules.
- [X] T022 [P] [US2] Write `scripts/tests/commits_test.sh` (real `git`; fixture modules `alpha` and `beta`; `.github/commit-scopes` containing `ci`). These pass:
  - `feat(alpha): add x` touching `alpha/`;
  - merge commits, which are ignored;
  - `Revert "feat(alpha): add x"`;
  - `ci: tweak` touching only root files;
  - `feat(alpha)!: break` and `feat!: break` on root files;
  - a commit touching both `alpha/` and `beta/` with scope `alpha`.

  These fail, and the output names the offending commit or `PR title`:
  - `fixed stuff`;
  - `fixup! feat(alpha): add x`;
  - `feat(unknown): x`;
  - a commit confined to `alpha/` with scope `ci` or with no scope;
  - `PR_TITLE='bad title'`.

  A `.github/commit-scopes` entry equal to a module directory exits 2.
- [X] T023 [P] [US2] Write `scripts/tests/changelog_test.sh` (real `git`).
  - A change to `alpha/x.go` without `alpha/CHANGELOG.md` fails and names `alpha`.
  - The same change plus `alpha/CHANGELOG.md` passes.
  - Changes limited to `alpha/x_test.go`, `alpha/testdata/f` or `alpha/README.md` pass.
  - A change to `alpha/go.mod` alone requires a changelog change.
  - With `CHANGELOG_WAIVED=true`, the failing case passes and prints `waived by label`.
  - With two modules, only the one missing its changelog is reported.
- [X] T024 [P] [US2] Write `scripts/tests/apicompat_test.sh` (real `git`; a shim `gorelease` that logs its arguments and prints a canned report chosen by an env var). Check that:
  - with no tags, the shim is called with `-base=none` and the module passes;
  - with tags `alpha/v1.2.0`, `alpha/v1.10.0` and `alpha/v1.11.0-rc.1`, the base is `v1.10.0` (semver order, prereleases excluded);
  - a module with path suffix `/v2` uses only `alpha/v2.*` tags;
  - a report containing a line `## incompatible changes` fails when the base major is ≥ 1, and passes but still prints the report when the base is `v0.x`;
  - when `GITHUB_ACTIONS=true`, the report is appended to `$GITHUB_STEP_SUMMARY`.
- [X] T025 [P] [US2] Write `scripts/tests/cross_build_test.sh`, with a shim `go` that logs `GOOS/GOARCH/CGO_ENABLED` and the subcommand, and fails for a configurable platform. Check that:
  - `go build ./...` and `go vet ./...` run for all six `{linux,darwin,windows}×{amd64,arm64}` with `CGO_ENABLED=0`;
  - a `platforms linux/amd64,linux/arm64` exception limits the run to those two;
  - a failure on `windows/arm64` yields `FAIL  <root>  windows/arm64` and exit 1.
- [X] T026 [P] [US2] Write `scripts/tests/tidy_test.sh`, with a shim `go` where `mod tidy -diff` prints a diff and exits 1 for one module. Check that the module fails, its name and the diff are shown, and the other modules are still checked.

### Implementation for User Story 2

- [X] T027 [US2] Implement `scripts/modules.sh` with three subcommands, so that T020 passes:
  - `list`;
  - `touched <base> <head>`, using `git diff --name-only <base>...<head>`, mapping each file to `<dir>/vN` when that is a module root and otherwise to `<dir>`, and expanding to all modules when a shared tooling path from [research.md R-08](research.md#r-08-touched-module-detection) changes;
  - `json-go-versions`.
- [X] T028 [P] [US2] Implement `scripts/tidy-check.sh` so that T026 passes: per module, run `GOWORK=off go mod tidy -diff`.
- [X] T029 [P] [US2] Implement `scripts/apicompat.sh` so that T024 passes.
  - Find the module major from its path suffix.
  - Pick the highest non-prerelease tag `<dir>/v<major>.*` (for major 0 or 1, consider `v0.*` and `v1.*`), using an `awk` semver comparator.
  - Run `GOWORK=off gorelease -base=<vX.Y.Z|none>` from the module root and capture the report.
  - Fail only if the base major is ≥ 1 and the report matches `^## incompatible changes`.
  - When `GITHUB_ACTIONS=true`, append each report and its `Suggested version` to `$GITHUB_STEP_SUMMARY`.
- [X] T030 [P] [US2] Implement `scripts/structure-check.sh` so that T021 passes.
  - Check the directory name against `^[a-z0-9][a-z0-9._-]*$`.
  - Find `replace` directives with `awk '/^[[:space:]]*replace([[:space:]]|\()/'` on `go.mod`.
  - Check that `README.md` and `CHANGELOG.md` exist.
  - Read the `go` directive with `GOWORK=off go list -m -f '{{.GoVersion}}'` and compare its `major.minor` with `GO_VERSION_OLDEST` in `awk`.
  - Validate `.module-exceptions`: run `parse_exceptions`, and fail on any line naming a module that does not exist.
- [X] T031 [P] [US2] Implement `scripts/cross-build.sh` so that T025 passes. Per module and per platform (all six, or the `platforms` exception), run `CGO_ENABLED=0 GOOS=… GOARCH=… GOWORK=off go build ./...` and then `go vet ./...` from the module root. The FAIL reason names the platform(s) that failed.
- [X] T032 [P] [US2] Implement `scripts/commits-check.sh <base> <head>` so that T022 passes.
  - Read the commits with `git log --no-merges --format='%H%x09%s' <base>..<head>`, and `PR_TITLE` from env only.
  - Commit subjects must match the regex `^(feat|fix|docs|style|refactor|perf|test|build|ci|chore|revert)(\([a-z0-9][a-z0-9._-]*\))?!?: .+` or `^Revert ".+"$`. Reject `^(fixup|squash)! `.
  - A scope must be a module directory (modules at `<base>` ∪ `<head>`) or a line of `.github/commit-scopes`.
  - A commit whose files, from `git diff-tree --no-commit-id --name-only -r`, all belong to one module must use that module's top-level directory as scope.
  - Validate `.github/commit-scopes`: a bad format, or a collision with a module directory, exits 2.
- [X] T033 [P] [US2] Implement `scripts/changelog-check.sh <base> <head>` so that T023 passes.
  - For each module, a change to a non-`_test.go` `*.go` file outside `testdata/`, or to `go.mod`, in `git diff --name-only <base>...<head>` requires `<root>/CHANGELOG.md` in the same diff.
  - `CHANGELOG_WAIVED=true` turns every failure into a PASS with `waived by label`.
- [X] T034 [P] [US2] Create the scanner configuration files, each starting with a comment that explains the "each entry needs a written justification" rule (FR-023):
  - `.trivyignore`, with comments only;
  - `.gitleaksignore`, with comments only;
  - `.semgrepignore`, which replaces Semgrep's defaults, so it lists `.git/`, `vendor/`, `specs/`, `scripts/tests/` and `**/testdata/`;
  - `bearer.yml`, with `report: security`, `fail-on-severity: critical,high,medium,low,warning` and `skip-path: [specs, scripts/tests]` (check the key names against the `bearer scan --help` output of v2.1.1).
- [X] T035 [US2] Create the first part of `.github/workflows/pr.yml`.
  - **Header**: name `pr`; `on.pull_request` with `branches: [main]` and `types: [opened, synchronize, reopened]`; top-level `permissions: {}`; `concurrency: {group: pr-${{ github.event.pull_request.number }}, cancel-in-progress: true}`; workflow `env: {GOWORK: "off", GOTOOLCHAIN: local}`.
  - **Shared step pattern**: checkout, then `grep -v '^#' versions.env | grep . >> "$GITHUB_ENV"`, then `actions/setup-go` with `go-version: ${{ env.GO_VERSION_NEWEST }}.x` and `cache: false` (no `go.sum` exists yet).
  - **Rules for every job in this task**: `permissions: {contents: read}`, **no `needs:`** except where stated, and the touched list computed in-job with `scripts/modules.sh touched "$BASE_SHA" "$HEAD_SHA"`. `BASE_SHA` and `HEAD_SHA` come from `github.event.pull_request.base.sha` and `head.sha` via `env:`. Checkout uses `fetch-depth: 0`.
  - **Jobs**:
    - `setup` (outputs only `go-versions` from `scripts/modules.sh json-go-versions`);
    - `lint` (`golangci/golangci-lint-action` with `install-only: true` and `version: ${{ env.GOLANGCI_LINT_VERSION }}`, then `scripts/lint.sh $TOUCHED`);
    - `test-go`, named `test (go ${{ matrix.go }})`, with `needs: setup`, `strategy.fail-fast: false` and `matrix.go: ${{ fromJSON(needs.setup.outputs.go-versions) }}`; it installs Go `${{ matrix.go }}.x` and runs `scripts/test.sh $TOUCHED`;
    - `test`, the aggregator, with `needs: test-go` and `if: always()`; its single step fails unless `needs.test-go.result == 'success'`;
    - `tidy` (`scripts/tidy-check.sh --all`);
    - `api-compat` (`go install golang.org/x/exp/cmd/gorelease@${GORELEASE_VERSION}`, then `scripts/apicompat.sh $TOUCHED`);
    - `structure` (`scripts/structure-check.sh --all`);
    - `cross-build` (`scripts/cross-build.sh $TOUCHED`).
  - Mark each intentional word-split of `$TOUCHED` with `# shellcheck disable=SC2086`.
- [X] T036 [US2] Add the security and tooling jobs to `.github/workflows/pr.yml`. Each job has `permissions: {contents: read}` and no `needs:`.
  - `vulnerabilities`: `scripts/install-tool.sh trivy "$RUNNER_TEMP/bin"`, then `trivy fs --scanners vuln --severity HIGH,CRITICAL --exit-code 1 --ignorefile .trivyignore .`.
  - `sbom`: install Trivy. For each module from `scripts/modules.sh list`, run `trivy fs --format cyclonedx --output "sbom/$(echo <root> | tr / _).cdx.json" <root>`. Then `actions/upload-artifact` with `name: sbom`, `path: sbom/`, `retention-days: 90` and `if-no-files-found: ignore`.
  - `secrets`: checkout with `fetch-depth: 0`, install Gitleaks, then `gitleaks git --log-opts="$BASE_SHA..$HEAD_SHA" --redact --exit-code 1 .`.
  - `semgrep`: `docker run --rm -v "$PWD:/src" -w /src "$SEMGREP_IMAGE" semgrep scan --metrics=off --error` with env `SEMGREP_RULES: "p/golang p/trailofbits p/github-actions"`, passed into the container with `-e SEMGREP_RULES`.
  - `bearer`: install Bearer, then `bearer scan . --config-file bearer.yml`.
  - `scripts`, named `scripts (${{ matrix.os }})`, with `matrix.os: [ubuntu-latest, macos-latest]` and `fail-fast: false`. It runs `make scripts-test` on Ubuntu (shellcheck is preinstalled) and `scripts/tests/run.sh` on macOS.
- [X] T037 [US2] Create `.github/workflows/pr-meta.yml`.
  - **Header**: name `pr-meta`; `on.pull_request` with `branches: [main]` and `types: [opened, synchronize, reopened, edited, labeled, unlabeled]`; `permissions: {}`; concurrency group `pr-meta-${{ github.event.pull_request.number }}` with `cancel-in-progress: true`.
  - **Jobs**: `commits` and `changelog`, each with `permissions: {contents: read}` and checkout with `fetch-depth: 0`.
    - `commits` runs `scripts/commits-check.sh "$BASE_SHA" "$HEAD_SHA"` with `env: PR_TITLE: ${{ github.event.pull_request.title }}`. Never interpolate the title inside `run:`.
    - `changelog` runs `scripts/changelog-check.sh "$BASE_SHA" "$HEAD_SHA"` with `env: CHANGELOG_WAIVED: ${{ contains(github.event.pull_request.labels.*.name, 'skip-changelog') }}`.
- [X] T038 [P] [US2] Create `.github/ruleset-main.json`, a GitHub repository ruleset:
  - `name: main-protection`, `target: branch`, `enforcement: active`, and `conditions.ref_name.include: ["refs/heads/main"]`;
  - rules `pull_request` (`required_approving_review_count: 0`) and `required_status_checks` with `strict_required_status_checks_policy: true`;
  - one `required_status_checks` entry per context: `lint`, `test`, `tidy`, `api-compat`, `vulnerabilities`, `sbom`, `secrets`, `semgrep`, `bearer`, `structure`, `cross-build`, `commits`, `changelog`, `scripts (ubuntu-latest)`, `scripts (macos-latest)`;
  - `integration_id: 15368` (GitHub Actions) on each entry.

  Validate the file with `jq .`.
- [ ] T039 [US2] Validate [quickstart.md §3](quickstart.md#3-pull-request-checks-user-story-2): push a scratch branch to `paccolamano/go` and open a **draft** PR (an outward action, so confirm with the maintainer first). Walk through every commit row, then close the PR and delete the branch.

**Checkpoint**: every PR to `main` reports all the contracted statuses, each with a conclusive
result.

---

## Phase 5: User Story 3 - Publish a module release by pushing a tag (Priority: P3)

**Goal**: pushing `<dir>/vX.Y.Z` creates a GitHub release with changelog notes and an SBOM.
Invalid tags create nothing and say why.

**Independent Test**: [quickstart.md §4](quickstart.md#4-release-user-story-3--on-a-fork-or-scratch-repository-only),
on a fork. Every tag row produces the documented outcome.

**Depends on**: US1 (the `gates` job reuses `scripts/lint.sh` and `scripts/test.sh`) and
Foundational (`scripts/install-tool.sh`). It does not depend on US2.

### Tests for User Story 3 ⚠️ (write first, see them fail)

- [X] T040 [P] [US3] Write `scripts/tests/release_validate_test.sh` (real `git` and `go`; fixture repo with modules `alpha` (`github.com/paccolamano/go/alpha`) and `gamma/v2` (`github.com/paccolamano/go/gamma/v2`); `RELEASE_MAIN_REF=main`; `MODULE_PREFIX=github.com/paccolamano/go`; `NOTES_FILE` in a temp dir).
  - **Grammar**: tags must match `^([a-z0-9][a-z0-9._-]*)/v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z.-]+)?$`. These fail: `v1.0.0`, `alpha/1.0.0`, `alpha/v01.0.0` and `alpha/v1.0.0+meta`.
  - **Module resolution**: `nope/v1.0.0` fails ("module exists"). `gamma/v2.0.0` resolves `root=gamma/v2`. `alpha/v2.0.0` fails with the expected and actual module path.
  - **Ancestry**: a tag on a commit that only exists on a side branch fails with "not on main".
  - **Changelog**:
    - these headings are found: `## [0.1.0]` and `## [0.1.0] - 2026-10-16`;
    - the body runs up to the next `## ` heading, with blank lines trimmed;
    - an empty body counts as missing, and so does a missing section;
    - `alpha/v0.2.0-rc.1` with `## [0.2.0-rc.1]` prints `prerelease=true`.
  - **Output**: on success, stdout has `dir=`, `root=`, `version=`, `prerelease=` and `asset=<dir>-v<version>.cdx.json` lines, and `NOTES_FILE` contains exactly the section body.

### Implementation for User Story 3

- [X] T041 [US3] Implement `scripts/release-validate.sh <tag>` so that T040 passes. Run the checks in the order of [contracts/release.md](contracts/release.md#validation-in-order-first-failure-stops-the-run), and stop at the first failure.
  - Commit: `git rev-parse "<tag>^{commit}"`. Ancestry: `git merge-base --is-ancestor <commit> "${RELEASE_MAIN_REF:-origin/main}"`.
  - Module root: for major N ≥ 2, prefer `<dir>/vN` when it has a `go.mod`, otherwise `<dir>`.
  - Expected path: `${MODULE_PREFIX:-github.com/paccolamano/go}/<dir>`, plus `/vN` when N ≥ 2.
  - Changelog: extract the section with `awk`, escaping the dots in the version.
  - Every failure message names its reason, as listed in the contract table.
- [X] T042 [US3] Create `.github/workflows/release.yml`.
  - **Header**: `on.push.tags: ['*/v*']`; `permissions: {}`; `concurrency: {group: release-${{ github.ref_name }}, cancel-in-progress: false}`; `env: {GOWORK: "off", GOTOOLCHAIN: local}`.
  - **`validate`** (`contents: read`; checkout with `fetch-depth: 0`):
    1. run `scripts/release-validate.sh "$TAG"` with `TAG: ${{ github.ref_name }}` and `NOTES_FILE=release-notes.md`, appending its stdout to `$GITHUB_OUTPUT`;
    2. set `exists=true|false` from `gh release view "$TAG" --repo "$GITHUB_REPOSITORY"` (with `GH_TOKEN: ${{ github.token }}`);
    3. upload the `release-notes` artifact.
  - **`gates`** (`contents: read`; `needs: validate`; `if: needs.validate.outputs.exists != 'true'`): load `versions.env`, set up Go `${GO_VERSION_NEWEST}.x`, install golangci-lint with `golangci/golangci-lint-action` (`install-only: true`), then run `scripts/lint.sh <root>` and `scripts/test.sh <root>`.
  - **`sbom`** (`contents: read`; same `needs`/`if`): `scripts/install-tool.sh trivy`, then `trivy fs --format cyclonedx --output "<asset>" <root>`, then upload the `release-sbom` artifact.
  - **`publish`** (`contents: write`; `needs: [validate, gates, sbom]`; same `if`; **no checkout and no third-party binary**): download both artifacts with `actions/download-artifact`, then run `gh release create "$TAG" --repo "$GITHUB_REPOSITORY" --verify-tag --title "$TAG" --notes-file release-notes.md "<asset>"`, adding `--prerelease` when `prerelease == 'true'`.
  - When `exists == 'true'`, `validate` logs `release already exists, nothing to do` in the job summary.
- [ ] T043 [US3] Validate [quickstart.md §4](quickstart.md#4-release-user-story-3--on-a-fork-or-scratch-repository-only) on a **fork or scratch repository only**. This is an outward action, so confirm with the maintainer. Never push test tags to `paccolamano/go`. Run every tag row, including the re-run idempotency row, and confirm that `actions/download-artifact` v8.0.1 reads the artifacts uploaded by `actions/upload-artifact` v7.0.1. If it does not, align the two majors in all workflows and in [research.md R-04](research.md#r-04-pinned-components).

**Checkpoint**: tag-driven releases work, and invalid tags never publish.

---

## Phase 6: User Story 4 - Develop across modules with a workspace (Priority: P4)

**Goal**: a committed `go.work` that lists every module and is regenerated by `make work`. No
gate uses it.

**Independent Test**: [quickstart.md §2](quickstart.md#2-workspace-user-story-4). `make work`
reflects added and removed modules, and the gate verdicts are unchanged with a broken `go.work`.

**Depends on**: Foundational only.

### Tests for User Story 4 ⚠️ (write first, see them fail)

- [X] T044 [P] [US4] Write `scripts/tests/work_sync_test.sh` (real `go`; fixture repo; `GO_VERSION_OLDEST=1.26` in a temporary `versions.env`). Check the workspace rules (verbatim from the data model): "a `go <GO_VERSION_OLDEST>.0` line; no `toolchain` line; a `use` entry for each module root, sorted".
  - With zero modules, `go.work` contains only `go 1.26.0`.
  - With `beta` and `alpha`, it contains `use (./alpha ./beta)` in sorted order.
  - After deleting `beta` and re-running, `./beta` is gone.
  - An existing `go.work.sum` is left in place.

### Implementation for User Story 4

- [X] T045 [US4] Implement `scripts/work-sync.sh` so that T044 passes: `rm -f go.work`, then `go work init`, then `go work edit -go="${GO_VERSION_OLDEST}.0" -toolchain=none`, then `go work use <sorted roots…>` when any module exists, all run at the repository root.
- [X] T046 [US4] Add the `Makefile` target `work` (`## Regenerate go.work from the discovered modules`), which calls `scripts/work-sync.sh`.
- [X] T047 [US4] Run `make work` at the repository root, check that the resulting `go.work` contains only `go 1.26.0` (no modules exist yet), and add it to version control. Do not add `go.work` to `.gitignore`.

**Checkpoint**: all four user stories work independently.

---

## Phase 7: Polish & cross-cutting concerns

- [X] T048 [P] Write `CONTRIBUTING.md` for contributors and maintainers. Sections:
  - **Prerequisites**: the [quickstart table](quickstart.md#prerequisites).
  - **Local gates**: `make help`, `check`, `lint`, `test`, `MODULE=`, and coverage exceptions via `.module-exceptions`.
  - **Workspace**: `make work`, and the rule that no gate uses it.
  - **Pull request statuses**: the table from [contracts/ci-checks.md](contracts/ci-checks.md), the Conventional Commits rules with module scope and `.github/commit-scopes`, the `skip-changelog` waiver, and the suppression files with the justification rule.
  - **Release procedure**: update `CHANGELOG.md`, read the `api-compat` summary, push `<dir>/vX.Y.Z`, and recover with the next version or a `retract`, as described in [contracts/release.md](contracts/release.md).
  - **Branch protection**: `gh api -X POST repos/paccolamano/go/rulesets --input .github/ruleset-main.json`.
  - **Updating pinned versions**: edit `versions.env` and the workflow SHAs together, recompute the hashes, and respect the 14-day cool-down.
- [X] T049 [P] Add a short "Development" section to `README.md`, linking to `CONTRIBUTING.md` and showing `make check`.
- [X] T050 Review `.github/workflows/*.yml` for security, against [research.md R-03](research.md#r-03-supply-chain-policy-for-ci-components) and FR-041/FR-042:
  - every `uses:` is pinned to a 40-char SHA with a `# vX.Y.Z` comment;
  - `contents: write` appears only in `release.yml` → `publish`;
  - no `${{ github.event.* }}` expression appears inside a `run:` block;
  - no workflow references `secrets.` other than `github.token`.

  Fix any finding, then run `make scripts-test` and `shellcheck` until both are clean.
- [ ] T051 Create the `skip-changelog` label in `paccolamano/go` with `gh label create skip-changelog --description "Waive the changelog check for this PR (maintainers only)" --color BFD4F2`. This is an outward action, so confirm with the maintainer first.
- [ ] T052 Once the first real PR containing this feature has run both PR workflows, apply branch protection with `gh api -X POST repos/paccolamano/go/rulesets --input .github/ruleset-main.json`. This is an outward admin action, so confirm with the maintainer first. Check that every context from T038 appears as required.
- [ ] T053 Run the full [quickstart.md](quickstart.md) "Done when" list. Then update the [spec quality checklist notes](checklists/requirements.md) with the validation date and any deviation found.

---

## Phase 8: Constitution v1.2.0 follow-up (squash merge only)

**Purpose**: align the feature with the amended "Merging" and "Commits" rules. Only the pull
request title reaches `main`, as the squash commit message.

- [X] T054 Rewrite `scripts/tests/commits_test.sh` for the title-only rule and see it fail against the old script. Cover: branch commits (`wip`, `fixup!`) are ignored; module, repository and no-scope titles; `!`; `Revert "…"`; a non-conventional title; an unknown scope; a PR confined to one module that must use its scope; a missing `PR_TITLE`, which exits 2; a scope colliding with a module, which exits 2.
- [X] T055 Rewrite `scripts/commits-check.sh` so that T054 passes. Check only `PR_TITLE`, and compute the confinement rule over `git diff --name-only <base>...<head>`.
- [X] T056 Add `required_linear_history` and `allowed_merge_methods: ["squash"]` to `.github/ruleset-main.json`, and align `.github/workflows/pr-meta.yml`, `CONTRIBUTING.md`, spec FR-032 and the new FR-043, research R-11, the contracts, the quickstart and the plan.
- [ ] T057 Apply the repository merge settings (an outward admin action, so confirm with the maintainer first): `gh api -X PATCH repos/paccolamano/go -F allow_squash_merge=true -F allow_merge_commit=false -F allow_rebase_merge=false -f squash_merge_commit_title=PR_TITLE -f squash_merge_commit_message=PR_BODY`.

---

## Dependencies & Execution Order

### Phase dependencies

- **Setup (Phase 1)**: no dependencies.
- **Foundational (Phase 2)**: depends on Setup and blocks every user story.
- **US1 (Phase 3)**: depends on Foundational.
- **US2 (Phase 4)**: depends on US1 (it reuses `lint.sh` and `test.sh`).
- **US3 (Phase 5)**: depends on US1 and Foundational, and is independent of US2.
- **US4 (Phase 6)**: depends on Foundational only, so it can run in parallel with US1–US3.
- **Polish (Phase 7)**: depends on every story. T052 also needs a real PR run of US2.

### Story completion graph

```text
Setup → Foundational ─┬─► US1 ─┬─► US2 ─┐
                      │        └─► US3 ─┼─► Polish
                      └─► US4 ──────────┘
```

### Within each story

- Test tasks first; each must fail for the right reason before its implementation starts.
- Scripts come before the workflows that call them (T027–T033 before T035–T037, T041 before
  T042).
- The `Makefile` is edited in T012, T018 and T046, and `pr.yml` in T035 and T036; tasks that
  touch the same file run in sequence.
- Each story ends with its quickstart validation task.

## Parallel opportunities

- **Setup**: T003 and T004 can run in parallel after T001.
- **Foundational**: T007, T008 and T010 (test files) can run in parallel after T005 and T006.
- **US1**: T013, T014 and T015 can run in parallel.
- **US2**: T020–T026 (seven test files), then T028–T034 (separate scripts and config files),
  and T038, all in parallel.
- **US3 and US4**: T040 and T044 can be written in parallel with US2 work.

### Parallel example: User Story 2

```bash
# 1) Write all failing tests together:
Task: "T020 modules_test.sh"      Task: "T021 structure_test.sh"   Task: "T022 commits_test.sh"
Task: "T023 changelog_test.sh"    Task: "T024 apicompat_test.sh"   Task: "T025 cross_build_test.sh"
Task: "T026 tidy_test.sh"
# 2) Then T027 (modules.sh), and in parallel the independent scripts and configs:
Task: "T028 tidy-check.sh"   Task: "T029 apicompat.sh"   Task: "T030 structure-check.sh"
Task: "T031 cross-build.sh"  Task: "T032 commits-check.sh"  Task: "T033 changelog-check.sh"
Task: "T034 scanner configs" Task: "T038 ruleset-main.json"
# 3) Then the workflow files in sequence: T035 → T036 → T037
```

## Implementation Strategy

### MVP first (User Story 1 only)

1. Phase 1, then Phase 2. Check that `make scripts-test` is green.
2. Phase 3 (US1). **Stop and validate** with quickstart §1. Contributors can already run the
   constitution's local gates.

### Incremental delivery

1. **US1**: local gates (MVP).
2. **US4**: the workspace. Small, independent, and can land together with US1.
3. **US2**: PR enforcement. After it, every change to `main` is gated; then apply branch
   protection (T052).
4. **US3**: tag-driven releases, validated on a fork.
5. **Polish**: documentation, the label, the security review and the full quickstart.

## Notes

- Never commit throwaway modules. Never push test tags to `paccolamano/go` (tags are immutable).
- The commits of this feature must follow its own rules, for example `ci: add pr workflow`,
  `build: add Makefile local gates`, `docs: add CONTRIBUTING`, using scopes from
  `.github/commit-scopes`.
- Tasks marked as outward actions (T039, T043, T051, T052) need explicit maintainer confirmation
  before running.

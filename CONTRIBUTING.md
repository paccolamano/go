# Contributing

This repository hosts independent Go library modules, one per top-level directory, under the
rules of the [constitution](.specify/memory/constitution.md). The shared tooling (Makefile,
`scripts/`, GitHub workflows) applies to every module and finds modules automatically: adding a
module never requires a tooling change.

## Prerequisites

| Tool | Version | Needed for |
|------|---------|------------|
| Go | `GO_VERSION_OLDEST` or newer (see [`versions.env`](versions.env)) | everything |
| GNU Make | 3.81 or newer | local commands |
| Bash | 3.2 or newer | local commands |
| C toolchain (gcc or clang; Xcode CLT on macOS) | any | `go test -race` |
| golangci-lint | exactly `GOLANGCI_LINT_VERSION` | `make lint` |
| shellcheck | any recent | `make scripts-test` (tooling changes only) |

If golangci-lint is missing or at another version, `make lint` stops and prints the install
command for the pinned version.

## Local gates

Run these before pushing. They are the constitution's local gates, and CI runs the same scripts.

```bash
make help                 # list the targets
make check                # lint, then test, on every module
make lint MODULE=retry    # one module only (use retry/v2 for a major-version subdirectory)
make test MODULE=retry
```

- **`make lint`**: runs golangci-lint with the shared [`.golangci.yml`](.golangci.yml) from each
  module root. `//nolint` directives must name the linter and give a reason.
- **`make test`**: runs `go test -race -shuffle=on`, and fails a module below 80% statement
  coverage.
  - A lower threshold needs a line in [`.module-exceptions`](.module-exceptions) with a written
    justification, and the plan of the change must justify it too.
  - The same file declares platform restrictions, which must also be stated in the module README.
- Every command runs each module in isolation with `GOWORK=off`, keeps going after a failure, and
  ends with a `PASS`/`FAIL` summary.

## Workspace

[`go.work`](go.work) lists every module, for editor navigation and cross-module development.
Run `make work` after adding or removing a module and commit the result. No gate reads
`go.work`, so a module that only builds through the workspace still fails CI.

## Pull request checks

Every pull request to `main` runs the statuses below, and all of them are required. A status
with nothing to check passes and says why.

| Status | What it enforces |
|--------|------------------|
| `lint` | local gate 1 on touched modules |
| `test` | local gate 2 on touched modules, on every supported Go version (summarizes the `test (go …)` jobs) |
| `tidy` | `go mod tidy -diff` is clean in every module |
| `api-compat` | gorelease finds no incompatible change in touched modules at v1 or later; the job summary shows each report and the suggested next version |
| `vulnerabilities` | Trivy finds no HIGH or CRITICAL vulnerability in any module's dependencies |
| `sbom` | one CycloneDX SBOM per module, downloadable from the run's `sbom` artifact |
| `secrets` | Gitleaks finds no secret in the pull request's commits |
| `semgrep` | Semgrep (`p/golang`, `p/trailofbits`, `p/github-actions`) reports no finding |
| `bearer` | Bearer reports no security or privacy finding |
| `structure` | each module has a valid directory name, no `replace`, a README and a CHANGELOG, and a `go` directive no newer than the oldest supported Go; `.module-exceptions` is valid |
| `cross-build` | touched modules build and vet for linux, darwin and windows on amd64 and arm64 |
| `commits` | the PR title, which becomes the squash commit message, follows Conventional Commits |
| `changelog` | changed module code comes with a CHANGELOG entry |
| `scripts (ubuntu-latest)`, `scripts (macos-latest)` | tooling self-tests (Bash 3.2 on macOS) and shellcheck |

A module is *touched* when the pull request changes a file inside it. A change to shared tooling
(linter config, `versions.env`, `Makefile`, `scripts/`, workflows, exceptions, commit scopes,
scanner config) touches every module.

### Merging and commit messages

Pull requests are merged into `main` only with **squash merge**, so `main` has a linear history
with one commit per pull request. The squash commit takes the **pull request title**, which is
the message that matters. Commits on your branch are not checked; keep them few.

The PR title follows `<type>(<scope>)!: <description>`, where:

- `type` is one of `feat`, `fix`, `docs`, `style`, `refactor`, `perf`, `test`, `build`, `ci`,
  `chore` or `revert`;
- `!` marks a breaking change.

Scope rules:

- A pull request that changes a single module **must** use that module's directory as scope:
  `feat(retry): add jittered backoff`, `fix(retry)!: …`.
- Changes outside modules use no scope or one of [`.github/commit-scopes`](.github/commit-scopes):
  `ci: …`, `docs: …`.

GitHub's default `Revert "…"` title is also accepted.

### Changelog

A pull request that changes a module's non-test `.go` files or its `go.mod` must also update that
module's `CHANGELOG.md` ([Keep a Changelog](https://keepachangelog.com)). Tests, `testdata/` and
documentation are exempt. For a change with no user-visible effect, a maintainer can add the
**`skip-changelog`** label; the check re-runs on its own.

### Suppressing a security finding

Use only the tool's own mechanism, as narrowly as possible, and write the justification next to
the entry:

- [`.trivyignore`](.trivyignore)
- [`.gitleaksignore`](.gitleaksignore) (rotate real secrets, never ignore them)
- [`.semgrepignore`](.semgrepignore), or an inline `nosemgrep: <rule-id>` comment
- `bearer.ignore`, through `bearer ignore add <fingerprint> --comment "<justification>"`

## Releasing a module

1. Merge the changes to `main`, with the module's `CHANGELOG.md` gaining a
   `## [X.Y.Z] - YYYY-MM-DD` section.
2. Pick the version from the `api-compat` report of the last pull request: a major bump for
   incompatible changes at v1 or later, otherwise minor or patch.
3. Tag the `main` commit and push the tag: `git tag retry/v1.2.0 && git push origin retry/v1.2.0`.
   For major versions 2 and later, the module path must end in `/vN`.

The [release workflow](.github/workflows/release.yml) then runs these steps:

1. It checks the tag, the module path, that the commit is on `main`, and the changelog section.
2. It re-runs the local gates.
3. It publishes a GitHub release with the changelog section as notes and the module SBOM
   attached. Versions such as `v1.3.0-rc.1` become pre-releases.

If a check fails, no release is created. Published tags are never moved or deleted: fix the
problem and release the next version. If the broken version was already published, add a
`retract` directive in that next version.

## Branch protection (administrators)

[`.github/ruleset-main.json`](.github/ruleset-main.json) protects `main`. It requires every
status above, allows only squash merges, requires a linear history, and blocks force pushes and
deletion. Apply it once, after both pull request workflows have run on a pull request:

```bash
gh api -X POST repos/paccolamano/go/rulesets --input .github/ruleset-main.json
```

Then make the repository settings match. Allow squash merge only, and make the squash commit
take the PR title and body:

```bash
gh api -X PATCH repos/paccolamano/go \
  -F allow_squash_merge=true -F allow_merge_commit=false -F allow_rebase_merge=false \
  -f squash_merge_commit_title=PR_TITLE -f squash_merge_commit_message=PR_BODY
```

## Updating tool versions

All toolchain and tool versions live in [`versions.env`](versions.env). When updating:

1. Pick the newest stable release that is at least 14 days old.
2. For Trivy, Gitleaks and Bearer, download the linux/amd64 tarball and record its SHA-256 (it
   must match the release's checksum file).
3. For Semgrep, record the image digest.
4. Update the commit SHAs (and `# vX.Y.Z` comments) of every action in `.github/workflows/` in the
   same change.

## Changing the tooling

Tooling scripts are portable Bash: Bash 3.2, POSIX utilities and no GNU-only flags. They are
tested first in `scripts/tests/`, where every `*_test.sh` holds `test_*` functions that run in
isolated temporary repositories. Run `make scripts-test` (tests plus shellcheck) before pushing.

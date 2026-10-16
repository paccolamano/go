# Contract: Pull request statuses (required checks)

Covers FR-013 to FR-033 and FR-041 to FR-042. Each **Status** name below is a stable job name.
All of them except the matrix legs must be configured as required status checks on `main`
(see [quickstart.md](../quickstart.md#configure-branch-protection)).

Every job runs on every pull request, with no path filters and no job-level `if` that could skip
it. A job with nothing to check succeeds and states why (FR-024).

## Common workflow settings

| Setting | `pr.yml` | `pr-meta.yml` |
|---------|----------|---------------|
| Triggers | `pull_request` → `main`: `opened`, `synchronize`, `reopened` | `pull_request` → `main`: those three plus `edited`, `labeled`, `unlabeled` |
| Permissions | top-level `{}`; each job `contents: read` | same |
| Concurrency | `<workflow>-<PR number>`, `cancel-in-progress: true` (FR-027) | same |
| Env | `GOWORK=off`, `GOTOOLCHAIN=local` | same |

Neither workflow uses repository secrets, so fork PRs are fully checked (FR-025). Untrusted
event fields (PR title, branch names) reach scripts only through `env:`, never through `${{ }}`
inside `run:`.

## `pr.yml`

`setup` is an internal job and is not required. It outputs **only** the Go-version matrix (from
`versions.env`), which the `test (go …)` legs need at expression time. Every other job computes
its own module list with `scripts/modules.sh` and has **no** `needs:`. A job skipped because a
dependency failed can count as passing for branch protection, so no required status may depend
on another job, except the `test` aggregator, which runs with `if: always()` and fails unless
its legs succeeded.

| Status | Scope | Go | Checks | Spec | Constitution PR gate |
|--------|-------|----|--------|------|----------------------|
| `lint` | touched | newest | `scripts/lint.sh` | FR-015 | 1 |
| `test (go <ver>)` | touched | each supported | `scripts/test.sh` (matrix leg, **not** required) | FR-015 | 1 |
| `test` | — | — | Aggregator: succeeds only if every `test (go …)` leg succeeded | FR-015, FR-024 | 1 |
| `tidy` | all | newest | `scripts/tidy-check.sh --all` | FR-016 | 2 |
| `api-compat` | touched | newest | `scripts/apicompat.sh`; report in the job summary | FR-017 | 3 |
| `vulnerabilities` | all | — | Trivy, HIGH/CRITICAL, `.trivyignore` | FR-018 | 4 |
| `sbom` | all | — | Trivy CycloneDX per module; uploads the `sbom` artifact | FR-019 | 5 |
| `secrets` | PR commits | — | Gitleaks over `base.sha..head.sha`, `.gitleaksignore` | FR-020 | 6 |
| `semgrep` | repository | — | Semgrep `SEMGREP_RULES`, `--error`, `.semgrepignore` | FR-021 | 7 |
| `bearer` | repository | — | Bearer `bearer.yml`, `bearer.ignore`; passes with "nothing to scan" when no file is in a language Bearer supports | FR-022 | 8 |
| `structure` | all | newest | `scripts/structure-check.sh --all` | FR-028, FR-029, FR-030 | — |
| `cross-build` | touched | newest | `scripts/cross-build.sh` | FR-031 | — |
| `scripts (ubuntu-latest)` | repository | newest | `make scripts-test` | tooling self-test | — |
| `scripts (macos-latest)` | repository | newest | `make scripts-test` | tooling self-test | — |

`actions/checkout` uses `fetch-depth: 0` in the jobs that need history or tags: the jobs scoped
to touched modules (`lint`, `test (go …)`, `api-compat`, `cross-build`) and `secrets`.

## `pr-meta.yml`

| Status | Inputs | Checks | Spec |
|--------|--------|--------|------|
| `commits` | `base.sha`, `head.sha`, env `PR_TITLE` | `scripts/commits-check.sh`: the PR title (the squash commit message); branch commits are not checked | FR-032 |
| `changelog` | `base.sha`, `head.sha`, env `CHANGELOG_WAIVED` = PR has the `skip-changelog` label | `scripts/changelog-check.sh` | FR-033 |

## Failure semantics

- A missing tool, a failed download, a hash mismatch or an unavailable advisory database makes
  the job fail (FR-026).
- Suppressions are honored only through the root ignore files listed in
  [config-files.md](config-files.md) (FR-023).
- Each failing job prints the offending module, file, commit or advisory in its log, and a
  summary in `$GITHUB_STEP_SUMMARY`.

## Third-party components

All components are pinned per [research.md R-03 and R-04](../research.md#r-04-pinned-components).
No component other than those listed there may be added without updating `research.md` (or a
later plan) and `versions.env`.

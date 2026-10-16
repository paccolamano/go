# Quickstart: validating the repository tooling

**Feature**: [spec.md](spec.md) | **Contracts**: [contracts/](contracts/)

This guide shows the feature working end to end. The repository has no module yet, so every
scenario uses **throwaway modules** on a scratch branch that is never merged. Release scenarios
run on a **fork or scratch repository** only, because tags are immutable on `paccolamano/go`
(constitution, Principle II).

## Prerequisites

| Tool | Version | Needed for |
|------|---------|------------|
| Go | `GO_VERSION_OLDEST` or newer (see `versions.env`) | everything |
| GNU Make | ≥ 3.81 | local commands |
| Bash | ≥ 3.2 | local commands |
| C toolchain (gcc/clang; Xcode CLT on macOS) | any | `-race` |
| golangci-lint | exactly `GOLANGCI_LINT_VERSION` | `make lint` |
| shellcheck | any recent | `make scripts-test` only |
| `gh` CLI, authenticated | any recent | PR, release and branch-protection steps |

## 1. Local gates (User Story 1)

```bash
make help                      # lists help, lint, test, check, work, scripts-test
make check                     # no modules yet → "no modules found", exit 0
```

Create two throwaway modules on a scratch branch:

```bash
git switch -c scratch/tooling-check
for m in alpha beta; do
  mkdir -p "$m" && (cd "$m" && go mod init "github.com/paccolamano/go/$m" && go mod edit -go=1.26.0)
  printf '# %s\n' "$m" > "$m/README.md"; printf '# Changelog\n' > "$m/CHANGELOG.md"
done
# add to each one: a package doc comment, one exported func with a doc comment, and a test
# that covers it
make check                     # expected: PASS alpha, PASS beta, exit 0
make test MODULE=alpha         # expected: only alpha is tested
```

Failure scenarios. Apply each, check the result, then revert it:

| Change | Command | Expected result |
|--------|---------|-----------------|
| Unformatted file in `alpha` | `make lint` | `alpha` fails with `file:line` and `(gofmt)`; `beta` is still checked; summary lists `FAIL  alpha`; the script exits 1 (`make` itself exits 2) |
| Untested exported func dropping `beta` to 25% | `make test` | `FAIL  beta  coverage 25.0% < 80% threshold` |
| Add `beta  coverage  20  <reason>` to `.module-exceptions` | `make test` | `beta` passes; the threshold of 20 and the justification are printed |
| Rename the golangci-lint binary away from `PATH` | `make lint` | exit 2, expected version and install command printed |
| `GOWORK=on` with a broken `go.work` | `make check` | same verdict as before (the workspace is ignored) |

## 2. Workspace (User Story 4)

```bash
make work && cat go.work       # go 1.26.0, plus use ./alpha and ./beta
rm -r beta && make work        # ./beta disappears from go.work
```

Open the repository in an editor: symbols from `alpha` resolve in other modules.

## 3. Pull request checks (User Story 2)

Push the scratch branch and open a **draft** PR against `main` in the real repository. PR
checks are safe because they create nothing permanent. Then push one commit per row:

| Commit | Expected statuses |
|--------|-------------------|
| `docs: tweak README` (root README only) | all statuses green; module jobs print "no modules to check" |
| `feat(alpha): add Foo` with `alpha/CHANGELOG.md` updated | all green; `test (go 1.26)` and `test (go 1.27)` both ran for `alpha` only |
| `ci: touch .golangci.yml` | `lint`, `test` and `cross-build` run for **all** modules |
| `wip` (any branch commit), PR title unchanged | `commits` stays green: branch commits are not checked |
| `feat(alpha): change Foo` without a changelog change | `changelog` fails; add the `skip-changelog` label → `changelog` re-runs and passes |
| Add `replace example.com/x => ../x` to `alpha/go.mod` | `structure` fails: `alpha: replace directive` |
| Add a `syscall.Mmap` call without build tags | `cross-build` fails for `windows/amd64` and `windows/arm64` |
| Require a module with a known HIGH CVE in `beta` | `vulnerabilities` fails, naming the module and the advisory |
| Commit a fake AWS key in a test fixture | `secrets` fails with a redacted finding |

Also check that:

- the run's artifacts contain `sbom` with one `.cdx.json` per module;
- the `api-compat` summary shows a gorelease report per touched module;
- pushing a new commit while checks are running cancels the older run;
- editing the PR title to `bad title` makes `commits` fail and quote the title;
- the merge button offers only "Squash and merge" once branch protection is applied.

Close the PR and delete the scratch branch afterwards. Nothing from it is merged.

## 4. Release (User Story 3) — on a fork or scratch repository only

```bash
gh repo fork paccolamano/go --clone=false   # or use a scratch repository
# On the fork's main: add module alpha with CHANGELOG section "## [0.1.0] - 2026-10-16"
git tag alpha/v0.1.0 && git push fork alpha/v0.1.0
```

| Tag pushed on the fork | Expected result |
|------------------------|-----------------|
| `alpha/v0.1.0` | release `alpha/v0.1.0`, notes = changelog section, asset `alpha-v0.1.0.cdx.json` |
| `alpha/v0.2.0-rc.1` (with changelog section) | published as a pre-release |
| `alpha/v0.3.0` (no changelog section) | no release; `validate` names the missing section |
| `alpha/v2.0.0` (module path without `/v2`) | no release; major-version mismatch reported |
| `alpha/v0.4.0` on a commit that is only on a side branch | no release; "not on main" |
| `v1.0.0` or `alpha/1.0.0` | no release (filter or grammar) |
| re-run the `alpha/v0.1.0` workflow | succeeds with "release already exists"; release unchanged |

## Configure branch protection

This is a one-time step for an administrator of `paccolamano/go`, run after the workflows have
run at least once on a PR. It creates a ruleset that requires every stable status from
[contracts/ci-checks.md](contracts/ci-checks.md):

Required contexts: `lint`, `test`, `tidy`, `api-compat`, `vulnerabilities`, `sbom`, `secrets`,
`semgrep`, `bearer`, `structure`, `cross-build`, `commits`, `changelog`,
`scripts (ubuntu-latest)`, `scripts (macos-latest)`.

```bash
gh api -X POST repos/paccolamano/go/rulesets --input .github/ruleset-main.json
```

`.github/ruleset-main.json` targets `refs/heads/main` with the rules `deletion`,
`non_fast_forward`, `required_linear_history`, `pull_request` (`allowed_merge_methods:
["squash"]`) and `required_status_checks` (`strict: true`, one context per name above).
`CONTRIBUTING.md` documents it, together with the repository settings that allow only squash
merges and make the squash commit take the PR title.

## Done when

- Every row of the tables above behaves as expected.
- Branch protection lists every status from the contract.
- The scratch branch, the draft PR and the fork tags have been cleaned up.

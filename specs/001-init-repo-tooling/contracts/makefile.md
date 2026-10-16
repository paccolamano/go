# Contract: Local commands (Makefile and scripts)

Covers FR-001 to FR-012. The Makefile is a thin wrapper around `scripts/`. CI calls the same
scripts directly, which keeps local and CI verdicts identical (SC-004).

## Make targets

| Target | Runs | Description |
|--------|------|-------------|
| `make help` (default) | — | Lists the targets with a one-line description each |
| `make lint` | `scripts/lint.sh` | golangci-lint with the root `.golangci.yml`, per module |
| `make test` | `scripts/test.sh` | `go test -race -shuffle=on` with the coverage threshold, per module |
| `make check` | `lint`, then `test` | The constitution's local gates, and nothing else |
| `make work` | `scripts/work-sync.sh` | Regenerates `go.work` (and `go.work.sum`) from the discovered modules |
| `make scripts-test` | `scripts/tests/run.sh` + `shellcheck` | Self-tests of the tooling scripts (tooling contributors only) |

**Variables**

- `MODULE=<root>` restricts `lint`, `test` or `check` to one module, for example
  `make test MODULE=retry` or `make lint MODULE=retry/v2` (FR-003).
- When `MODULE` is unset, the targets run on every discovered module (`--all`).

**Compatibility**: GNU Make ≥ 3.81 and Bash ≥ 3.2 (macOS defaults). `versions.env` is
`include`d.

## Script interface (shared by Make and CI)

```text
scripts/<name>.sh --all            # discover every module
scripts/<name>.sh <root> [<root>…] # check exactly these modules
scripts/<name>.sh                  # no modules: print "no modules to check" and exit 0
```

The empty form is what CI uses when a pull request touches no module (FR-024).

| Script | Extra inputs | Behavior |
|--------|-------------|----------|
| `modules.sh` | `list` \| `touched <base> <head>` \| `json-go-versions` | Prints module roots (one per line, sorted), or the JSON Go-version matrix |
| `lint.sh` | — | Per module, from its root, `GOWORK=off golangci-lint run --config <repo>/.golangci.yml ./...` |
| `test.sh` | — | Per module: race, shuffle and coverage; compares the total with the threshold from `.module-exceptions` or 80 |
| `work-sync.sh` | — | Rewrites `go.work` (see data model) |
| `tidy-check.sh` | — | Per module, `go mod tidy -diff` |
| `apicompat.sh` | — | Per module, gorelease against the latest same-major tag ([research.md R-09](../research.md#r-09-api-compatibility-gorelease)) |
| `structure-check.sh` | — | FR-028 to FR-030, plus stale or invalid `.module-exceptions` lines |
| `cross-build.sh` | — | FR-031 over the six platforms, minus declared restrictions |
| `commits-check.sh` | `<base> <head>`, env `PR_TITLE` (required) | FR-032: checks the PR title only |
| `changelog-check.sh` | `<base> <head>`, env `CHANGELOG_WAIVED=true\|false` | FR-033 |
| `release-validate.sh` | `<tag>`, env `NOTES_FILE` | FR-034 and FR-035; prints `root=`, `version=`, `prerelease=` lines and writes the notes |
| `install-tool.sh` | `<trivy\|gitleaks\|bearer> <dest dir>` | Downloads the version pinned in `versions.env`, verifies its SHA-256, and fails on mismatch |

## Output and exit status

- Every module-loop script keeps going after a module fails (FR-008). It ends with a summary
  block, then exits `0` only if every module passed:

  ```text
  ==> summary
  PASS  retry
  FAIL  cache        coverage 71.4% < 80% threshold
  ```

- Findings are printed with `file:line` (linter, vet, compiler output passed through unchanged).
- In CI (`GITHUB_ACTIONS=true`), scripts also append the summary to `$GITHUB_STEP_SUMMARY`.
- Exit codes: `0` success, `1` a check failed, `2` usage or environment error.

## Environment errors (exit 2)

| Condition | Message must include |
|-----------|----------------------|
| golangci-lint missing, or a version different from `GOLANGCI_LINT_VERSION` | expected version, found version, and the install command from golangci-lint's docs pinned to the expected version (FR-009) |
| `go` missing | minimum required version (`GO_VERSION_OLDEST`) |
| C toolchain missing (needed by `-race`) | the hint "install gcc/clang (Xcode CLT on macOS)" |
| Unknown module passed as an argument | the list of discovered modules |
| Malformed `.module-exceptions` line | file, line number, and the expected format |

## Invariants

- Every go command runs from the module root with `GOWORK=off` (FR-004, FR-012).
- No script writes inside a module directory. Coverage profiles and temporary files go to
  `mktemp -d` and are removed on exit.
- `make check` runs exactly the two local gates. Security scanners are CI-only, as the
  constitution requires.

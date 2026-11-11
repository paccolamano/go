# Quickstart: Validating slogx 0.2.0

This guide proves that the feature works end to end. The expected behavior is defined in:

- [contracts/public-api.md](contracts/public-api.md): the API, the configuration errors, the
  `gorelease` report and the CHANGELOG entry;
- [contracts/record-semantics.md](contracts/record-semantics.md): the level decision, the fanout
  and context values;
- [contracts/readme-recipes.md](contracts/readme-recipes.md): the three recipes and what each
  must do.

This guide only says how to check it. The run patterns below use the test name prefixes of
[research R-10](research.md#r-10-testing-approach-principle-iii-sc-001-to-sc-008); `tasks.md`
fixes the exact names.

## Prerequisites

- Go 1.26 or newer, and a C toolchain for `-race`.
- golangci-lint and gorelease at the versions pinned in `versions.env`.
- A clone with the tag `slogx/v0.1.0` (`git fetch --tags`).
- Checks run from `slogx/` with `GOWORK=off`, unless stated otherwise.

## 1. New behavior, before the code (red)

With the new tests in place and the library still as in `slogx/v0.1.0`:

```bash
GOWORK=off go vet ./...
```

**Expected**: the test build fails on `slogx.WithDefaultLevel`, `slogx.Fanout` and
`slogx.WithContextValue`, which do not exist yet. Once each identifier exists as a stub that
does nothing, its tests fail on behavior, not on compilation.

`TestNew_SameOutputAsV010` is the exception: it passes before and after the change, because it
guards behavior that must not change.

## 2. Local gates (constitution)

```bash
make check MODULE=slogx     # from the repository root: lint with 0 findings, then -race -shuffle=on with coverage ≥ 80%
```

**Expected**: `PASS` for `slogx` from both `lint` and `test`. Coverage stays well above 80% (the
spike measured 98.1%). The only `//nolint` in library code is `wrapcheck` in `forward.go`. No
`.module-exceptions` entry is added.

## 3. Tidy, changelog and API compatibility

```bash
GOWORK=off go mod tidy -diff                          # prints nothing
git diff --exit-code slogx/go.mod                     # from the repository root: go.mod unchanged
scripts/changelog-check.sh origin/main HEAD           # from the repository root
GORELEASE=$(go env GOPATH)/bin/gorelease scripts/apicompat.sh slogx   # from the repository root
```

**Expected**:

- `go.mod` keeps `go 1.26.0` and no `require`;
- the changelog check passes, because `slogx/CHANGELOG.md` changes with the sources;
- `gorelease` prints exactly the report of
  [contracts/public-api.md](contracts/public-api.md#expected-gorelease-report-against-slogxv010):
  11 removals and 3 additions, suggesting `v0.2.0`. At 0.x, the script reports `PASS` despite
  the incompatible changes;
- every identifier of that report appears in the `[Unreleased]` section of the CHANGELOG
  (SC-004).

## 4. Scenario checks per user story

| Story | Command | What must hold |
|-------|---------|----------------|
| US1: the default level | `GOWORK=off go test -race -run 'TestDefaultLevel_' -v .` | Over an all-levels output, Info passes and Debug does not. A context level wins below and above. An output whose own threshold is Info writes Debug under a default Debug. A `*slog.LevelVar` set at run time applies to the next record. Without a default level, the next handler decides. A nil level and a nil `*slog.LevelVar` are invalid. In SC-002, every record after the change follows the new level, and `-race` reports nothing. |
| US2: the fanout | `GOWORK=off go test -race -run 'TestFanout_\|TestMultiHandler_' -v .` | Both outputs receive the same records, an output with default options included. Output thresholds are ignored. Without a default level, with no output or a nil output, `New` reports invalid configuration, also after `WithAttrs` and `WithGroup` on the fanout. With groups, context attributes and redaction, every output gets the same line. A nested `New` adds the trace IDs to its output only, with no key twice. With three outputs and the second failing, the others receive the record and the error is returned unchanged; with two failing, both errors are found with `errors.Is`. The edge cases of the spec hold. The SC-001 matrix gives the same records on both outputs in all 12 combinations. Under `slog.NewMultiHandler`, an output at Info drops a request's Debug record. |
| US3: context values | `GOWORK=off go test -race -run 'TestContextValue_' -v .` | The rows of the context values table of [record-semantics.md](contracts/record-semantics.md#context-values-fr-012-to-fr-015) hold. Every record of a request served through a middleware that stores the identifier carries `request_id` (SC-003). |
| US4: unchanged output | `GOWORK=off go test -race -run 'TestNew_SameOutputAsV010' -v .` | The 195 sections are byte-identical to `testdata/v0.1.0.golden`. |
| US4: migrated tests | `GOWORK=off go test -race -run 'TestContext\|TestRedact\|TestLevel\|TestNil\|TestPanic\|TestSecret' -v .` | Every former standalone test passes with its expected output unchanged, built with `New`. |
| US4: any combination | `GOWORK=off go test -race -run 'TestNew_' -v .` | Every combination of the options is accepted. No error mentions "does not apply" or "required". |
| Conformance and allocations | `GOWORK=off go test -race -run 'Conformance\|TestAlloc_' -v . ./slogxtest` | `slogtest` passes for `Fanout` alone, `New` over `Fanout`, `New` and the recorder. A disabled record allocates 0 times with a default level, two context values and a fanout of two outputs. |
| Examples | `GOWORK=off go test -run 'Example' -v .` | `ExampleWithDefaultLevel`, `ExampleFanout` and `ExampleWithContextValue` pass, with standard library outputs only. No example uses a removed identifier. |

## 5. The golden file

`testdata/v0.1.0.golden` comes from `slogx/v0.1.0`, never from the current code. To check it, or
to produce it again after adding a case to `golden_test.go`:

```bash
# from the repository root
git worktree add /tmp/slogx-v010 slogx/v0.1.0
cp slogx/golden_test.go /tmp/slogx-v010/slogx/
(cd /tmp/slogx-v010/slogx && GOWORK=off go test -run TestNew_SameOutputAsV010 -golden.write .)
diff /tmp/slogx-v010/slogx/testdata/v0.1.0.golden slogx/testdata/v0.1.0.golden
git worktree remove --force /tmp/slogx-v010
```

**Expected**: `golden_test.go` compiles against `slogx/v0.1.0` unchanged, and `diff` prints
nothing. After a case is added, copy the new file from the worktree into `slogx/testdata/`
before removing the worktree.

## 6. Benchmarks (SC-006, SC-007)

```bash
GOWORK=off go test -run '^$' -bench . -benchmem -count 5 .
```

**Expected**: every `BenchmarkDisabled` sub-benchmark reports `0 B/op` and `0 allocs/op`. In
`BenchmarkEnabled`, the handler with redaction and a context value takes at most twice the time
of `bare-same-attrs` (the spike measured 1.48 times). Figures are in
[research R-11](research.md#r-11-cost-fr-021-sc-006-sc-007).

## 7. README recipes in a separate project (SC-009)

Before the release, in a scratch directory outside the repository:

```bash
mkdir recipes && cd recipes
go mod init example.com/recipes
go mod edit -require=github.com/paccolamano/go/slogx@v0.1.0 -replace=github.com/paccolamano/go/slogx=<path to the working tree>/slogx
go get github.com/lmittmann/tint@latest golang.org/x/term@latest \
  go.opentelemetry.io/contrib/bridges/otelslog@latest go.opentelemetry.io/otel/trace@latest \
  go.opentelemetry.io/otel/sdk@latest go.opentelemetry.io/otel/sdk/log@latest \
  go.opentelemetry.io/otel/exporters/stdout/stdoutlog@latest github.com/paccolamano/go/middleware@latest
```

Write one `main` package per recipe, with the code copied from the README. For recipe 2 and 3,
build `provider` with `sdklog.NewLoggerProvider` and a `stdoutlog` exporter writing to stderr, so
that the OpenTelemetry records can be read. For recipe 2, log inside a span from
`sdktrace.NewTracerProvider()`, in a handler wrapped by the `requestid` middleware and called
through `ServeHTTP`. Then run:

```bash
go run ./color | cat -v                         # no escape sequences
script -qc "go run ./color" /dev/null | cat -v  # escape sequences
NO_COLOR=1 script -qc "go run ./color" /dev/null | cat -v   # no escape sequences
go run ./otel 2>otel.json                       # stdout lines; OpenTelemetry records in otel.json
go run ./thresholds 2>thresholds.json
```

**Expected**: the behavior listed under each recipe in
[contracts/readme-recipes.md](contracts/readme-recipes.md). Record the versions used in the PR
description. The `replace` stays in the scratch project: no committed `go.mod` gets one.

## 8. Documentation review (FR-011, FR-023, FR-026)

```bash
# from the repository root: no match in the code, the tests, the examples or the README
grep -rniE 'sampl|flush' --include='*.go' slogx slogx/README.md
grep -rnE '\b(New)?(Context|Level|Redact|Sampling)Handler\b|\bWithSampl' --include='*.go' slogx slogx/README.md
go doc ./slogx                 # lists Fanout, WithContextValue, WithDefaultLevel; no removed identifier
go doc ./slogx Fanout          # states that the outputs' levels are ignored, and when to use slog.NewMultiHandler
```

**Expected**:

- the `grep` prints nothing; only `slogx/CHANGELOG.md` names the removed identifiers;
- `slogx/doc.go` and `slogx/README.md` have the sections of
  [research R-12](research.md#r-12-documentation-and-changelog-fr-023-fr-025-fr-026), and the
  statements of [record-semantics.md](contracts/record-semantics.md#documentation-statements-fr-011-fr-023);
- the README has the three recipes of [readme-recipes.md](contracts/readme-recipes.md);
- `slogx/CHANGELOG.md`, `[Unreleased]`, matches
  [public-api.md](contracts/public-api.md#changelog-entry).

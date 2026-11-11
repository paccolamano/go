# Implementation Plan: A Simpler slogx, With a Threshold of Its Own (slogx 0.2.0)

**Branch**: `013-simplify-slogx` (suggested; work currently on `main`) | **Date**: 2026-11-10 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `specs/013-simplify-slogx/spec.md`

**Note**: This template is filled in by the `/speckit-plan` command; its definition describes the execution workflow.

## Summary

This feature changes the existing module `github.com/paccolamano/go/slogx`, released as
`slogx/v0.1.0`, and ships as `slogx/v0.2.0`, a breaking minor release at 0.x. It keeps the
in-process capabilities (context attributes, redaction, the per-request level) and simplifies
the rest:

- **One handler**: `New` and `Handler` remain. The standalone `ContextHandler` and
  `RedactHandler` become unexported types, unchanged inside. `LevelHandler` merges into
  `Handler.Enabled`. The applicability and required-option checks go: each option checks its
  own arguments (R-03, R-08).
- **Sampling removed**: `SamplingHandler`, `WithSampling`, `WithSampleErrors`, `Handler.Flush`
  and the sampler (R-08).
- **`WithDefaultLevel(slog.Leveler)`**: `Handler.Enabled` decides with the context level, else
  the default level read at each call, else the next handler. A nil `Leveler` or a nil
  `*slog.LevelVar` is invalid (R-04).
- **`Fanout(outputs ...slog.Handler) slog.Handler`**: it passes a clone of every record to every
  output, never calling `Enabled`. It returns one error unchanged and several joined. It records
  why it is invalid, since it cannot return an error. `New` recognizes it as `next`, also after
  `WithAttrs` and `WithGroup`, and then requires a default level (R-05). Context attributes and
  redaction compose over it with no change (R-06).
- **`WithContextValue[T](key, get)`**: a context value joins the extraction functions in one
  ordered list of sources. It is skipped when its key is taken, and converted with
  `slog.AnyValue`. A panic gives the existing placeholder (R-07).
- **Unchanged output**: a golden file produced by `slogx/v0.1.0` guards 195 output sections. The
  tests of the former standalone handlers keep their expected outputs, built with `New` (R-09).
- **Documentation**: `doc.go` and the README are rewritten without sampling or standalone
  handlers. The README gains three recipes. The CHANGELOG records the removals, additions,
  changes and the migration (R-12, R-13).

The design was implemented in a scratch clone of the repository, outside the working tree:

- `golangci-lint` reported 0 issues, and the migrated and new tests passed under `-race` at
  98.1% coverage;
- `gorelease` reported exactly 11 removals and 3 additions, and suggested `v0.2.0`;
- the golden output of `slogx/v0.1.0` matched byte for byte;
- disabled records allocated nothing, and the enabled pipeline took 1.48 times the bare
  handler (limit 2);
- the three recipes ran as described against tint v1.2.1, x/term v0.48.0, otelslog v0.21.0 and
  OpenTelemetry v1.47.0;
- the reproduction of problem 2: under `slog.NewMultiHandler`, an output built with nil options
  dropped a request's Debug record, which `Fanout` delivered.

## Technical Context

**Language/Version**: Go. The `go` directive stays `go 1.26.0`. CI also tests on 1.27.
`slog.NewMultiHandler` (Go 1.26) appears only in tests and in the README. Nothing else newer than
what the module already uses is needed.

**Primary Dependencies**: the standard library only, tests and examples included (FR-022).
`go.mod` does not change. The README recipes name `github.com/lmittmann/tint`,
`golang.org/x/term`, `go.opentelemetry.io/contrib/bridges/otelslog`,
`go.opentelemetry.io/otel/trace` and `github.com/paccolamano/go/middleware/requestid`, as
documentation only ([R-13](research.md#r-13-readme-recipes-fr-024-sc-009), [R-14](research.md#r-14-lint-and-dependencies-fr-022)).

**Storage**: N/A. State lives in contexts and in immutable handlers. The sampler's table goes.

**Testing**: the standard `testing` package, with table-driven tests and named subtests.

- `slogtest.Run` for `Fanout`, `New` over `Fanout`, `New` and the recorder.
- `slogxtest.Recorder` as an output that enables every level, in place of the OpenTelemetry
  bridge.
- A golden file produced by `slogx/v0.1.0` (SC-005).
- `sync.WaitGroup` barriers and `-race` for SC-002, with no sleep.
- `httptest.NewRecorder` with `ServeHTTP`, without a listener, for SC-003.
- `AllocsPerRun`, benchmarks, `FuzzRedact`, and examples for every primary API.

Details: [research R-09](research.md#r-09-same-output-as-010-fr-020-sc-005-us4) and
[R-10](research.md#r-10-testing-approach-principle-iii-sc-001-to-sc-008).

**Target Platform**: any platform Go supports. Pure Go, with no build constraints.

**Project Type**: library. One existing Go module, `slogx/`, with the packages `slogx`
(changed), `slogx/slogxtest` and `slogx/internal/attrs` (both unchanged).

**Performance Goals** (SC-006, SC-007, [R-11](research.md#r-11-cost-fr-021-sc-006-sc-007)):

- Disabled records: 0 allocations, also with a default level, two context values and a fanout of
  two outputs. The spike measured 0 B in every configuration, at 9 to 12 ns.
- An enabled record with 5 attributes and 2 context attributes, through `New` with redaction:
  at most twice the time of the bare JSON handler writing the same 7 attributes. The spike
  measured 1.48 times with a context value and 1.47 times with an extraction function.

**Constraints**:

- No new state: `Handler` gains a `slog.Leveler`, and the fanout holds its outputs and a reason
  string. Both are immutable.
- No goroutine, no `init`, no global mutable state, no panic, no output of its own.
- The only `//nolint` in library code stays `wrapcheck` on the `handle` forwarder. The
  `contextcheck` one goes with sampling.
- Output unchanged for configurations without the new options (FR-020).

**Scale/Scope**:

- Library code shrinks from 1,177 to about 780 lines, the package documentation aside:
  - `fanout.go`: new, about 80 lines;
  - `level.go` and `sampling.go`: removed;
  - `handler.go`, `options.go`, `contexthandler.go`, `redact.go`: changed.
- The exported surface of `slogx` loses 11 identifiers and their methods, and gains 3
  ([contracts/public-api.md](contracts/public-api.md)).
- Tests: the sampling tests go; the others are migrated; new tests cover the three additions.
- Documentation: `doc.go`, the README with the three recipes, and the CHANGELOG.

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

| Rule | Assessment | Status |
|------|------------|--------|
| **I. Independent modules** | Only `slogx/` changes. No dependency, no `replace`. The README and the CHANGELOG are updated. The module builds and tests with `GOWORK=off`. Its purpose narrows to in-process `slog` handling. The recipe check uses a scratch module with a local `replace` that is never committed. | ✅ |
| **II. SemVer & stable API** | The tag is `slogx/v0.2.0`. At 0.x, a MINOR release may break, and `gorelease` suggests `v0.2.0` (R-01). The deprecation rule covers removals in a MAJOR version, so none applies at 0.x, as plan 012 noted. The surface shrinks to what the spec needs: 3 additions, each mapped to FRs, and 11 removals. The CHANGELOG lists exactly the `gorelease` report, marked **Breaking**, with a migration table (SC-004, FR-026). | ✅ |
| **III. Test-first** | `tasks.md` will order every test before its code, and step 1 of the quickstart observes the red phase. Every exported identifier added or changed is exercised: the default level, the fanout, context values, `New`, `Handler`. Tests are table-driven and hermetic: in-memory outputs, no network listener, no sleep. Examples cover the three additions (FR-025). The benchmarks are kept for the code declared performance-sensitive, and `FuzzRedact` stays. The module parses no external input. Coverage: 98.1% in the spike. | ✅ |
| **IV. Idiomatic Go** | 0 lint issues in the spike, with one fewer `//nolint`. Every new identifier has a doc comment, and `doc.go` is rewritten. Every configuration error wraps `ErrInvalidConfig`. The fanout returns one error unchanged and combines several with `errors.Join`, usable with `errors.Is`. No panic: a nil output or a nil `Leveler` is reported by `New`, and a fanout used on its own skips nil outputs. No `init`, no global, no goroutine. No I/O of its own, so no new `context.Context` parameter. | ✅ |
| **V. Simplicity & dependencies** | No dependency. The fanout, the default level and context values each map to a requirement. The generic `WithContextValue` has a current use, `requestid.FromContext`. The module loses a handler type per capability, the option bit set and the sampler. `Fanout` returns `slog.Handler`, not a concrete type, as the request specifies: this departs from a SHOULD of the principle and is recorded in Complexity Tracking. | ✅ (one recorded SHOULD deviation) |
| Go toolchain support | `go 1.26.0` is unchanged. `slog.NewMultiHandler` exists from Go 1.26. | ✅ |
| Platforms | Pure Go, with no OS-specific code. The recipe's `term.IsTerminal` is README code, not module code. | ✅ |
| Repository layout | No tooling change, no lint override, no `.module-exceptions` entry. | ✅ |
| Dependency hygiene | `go.mod` is unchanged, so `go mod tidy -diff` stays clean. | ✅ |
| Vulnerability scanning / Secrets | No dependency to scan. Test values stay obviously fake (`s3cr3t`, `hunter2`). The recipes contain no credential. | ✅ |
| Local and PR gates | `make check MODULE=slogx`. `scripts/changelog-check.sh` passes because the CHANGELOG changes with the sources. `scripts/apicompat.sh` prints the incompatible changes and passes at 0.x. | ✅ |
| Commits / Release | The PR title is `feat(slogx)!: simplify the API, with a default level, a fanout and context values`, with the `slogx` scope and `!` for the breaking change. The release follows in `chore(slogx): prepare release 0.2.0`, which tags `slogx/v0.2.0`. | ✅ |

**Result (pre-research)**: PASS, with one SHOULD deviation of Principle V recorded below.

**Result (post-design re-check)**: PASS.

- The data model adds two internal pieces of state, the default level and the fanout, both
  immutable. It adds no goroutine and no global.
- The three contracts map every addition and removal to an FR. The `gorelease` report of the
  spike matches the contract.
- No `NEEDS CLARIFICATION` remains: the spec settled every decision, and the research filled in
  the details.
- The spec needs no change: no decision of the research contradicts it. Research records the
  choices the spec left open:
  - a key of only whitespace is invalid for a context value, as for `WithRedactKeys` (R-07);
  - a context value whose key is taken is not computed (R-07);
  - a panic in a context value gives the existing `!CONTEXTFUNC` placeholder (R-07);
  - a nil `Leveler` in any call is invalid, even if a later call gives a valid one (R-04);
  - unlike `slog.MultiHandler`, which wraps even one error in `errors.Join`, the fanout returns
    a single error unchanged, as FR-009 requires (R-05).

## Project Structure

### Documentation (this feature)

```text
specs/013-simplify-slogx/
├── spec.md                    # feature specification
├── plan.md                    # this file
├── research.md                # Phase 0: decisions R-01 … R-14, with spike evidence
├── data-model.md              # Phase 1: config, Handler and its level decision, sources, fanout
├── quickstart.md              # Phase 1: validation guide
├── contracts/
│   ├── public-api.md          # API at 0.2.0, removals, configuration errors, gorelease report, CHANGELOG entry
│   ├── record-semantics.md    # level decision, fanout, context values, composition, unchanged output
│   └── readme-recipes.md      # the three README recipes, as verified, and what each must do
├── checklists/
│   └── requirements.md        # spec quality checklist
└── tasks.md                   # Phase 2 (/speckit-tasks, not created here)
```

### Source Code (repository root)

```text
slogx/
├── go.mod                     # unchanged: go 1.26.0, no require
├── CHANGELOG.md               # [Unreleased]: Removed (Breaking), Added, Changed, Migration from 0.1.0
├── README.md                  # rewritten: usage, context attributes, redaction, levels, outputs, three recipes, slogxtest
├── doc.go                     # rewritten: no sampling, no standalone handlers; levels and outputs sections
├── options.go                 # Option, ErrInvalidConfig, WithDefaultLevel, WithContextFunc, WithContextValue,
│                              # WithRedactKeys; per-option validation; optionSet and sampling options removed
├── handler.go                 # Handler (default level + level decision), New (fanout recognition); Flush removed
├── fanout.go                  # NEW: Fanout, fanout
├── contexthandler.go          # contextHandler (unexported), source, context values
├── redact.go                  # redactHandler (unexported), otherwise unchanged
├── context.go                 # With, WithLevel: doc comments name Handler
├── secret.go, forward.go      # unchanged
├── level.go                   # REMOVED (merged into handler.go)
├── sampling.go                # REMOVED
├── internal/attrs/            # unchanged
│
├── options_internal_test.go   # rewritten: per-option validation, accumulation, last default level wins
├── handler_test.go            # New: invalid configuration, any combination accepted, derived handlers; manual-chain test removed
├── level_test.go              # migrated to New; + default level tests, SC-002 concurrency
├── fanout_test.go             # NEW: US2, edge cases, errors, SC-001 matrix, conformance, standard fan-out behavior
├── redact_internal_test.go    # NEW: a sealed redactHandler returns itself (moved from redact_test.go)
├── contextvalue_test.go       # NEW: US3, SC-003
├── golden_test.go             # NEW: TestNew_SameOutputAsV010 (SC-005), -golden.write flag
├── testdata/v0.1.0.golden     # NEW: produced on slogx/v0.1.0
├── contexthandler_test.go     # migrated: New instead of NewContextHandler; applicability cases removed
├── redact_test.go             # migrated: New instead of NewRedactHandler; removed-behavior checks dropped
├── context_test.go, panic_test.go, fuzz_test.go   # migrated to New
├── secret_test.go, conformance_test.go, alloc_test.go, helpers_test.go   # unchanged helpers, new callers
├── bench_test.go              # sampling removed; + context value, Fanout, slog.NewMultiHandler
├── example_test.go            # + ExampleWithDefaultLevel, ExampleFanout, ExampleWithContextValue;
│                              # ExampleWithContextFunc, ExampleWithRedactKeys replace the standalone ones; sampling example removed
├── sampling_test.go           # REMOVED
├── sampling_internal_test.go  # REMOVED
│
└── slogxtest/                 # unchanged
```

**Structure Decision**: the existing module `slogx/` is changed in place:

- the new fanout gets its own file, as each capability had one;
- the level decision moves into `handler.go`, where `Handler` lives;
- context values join the extraction functions in `contexthandler.go`, which owns the sources;
- the options stay together in `options.go`.

Tests follow the same split, plus the golden test and its `testdata/`. Nothing outside `slogx/`
and this spec directory changes. `go.work` already lists `./slogx`.

## Complexity Tracking

| Deviation | Why Needed | Simpler Alternative Rejected Because |
|-----------|------------|-------------------------------------|
| `Fanout` returns the `slog.Handler` interface, not a concrete type (Principle V: functions SHOULD return concrete types) | The request fixes the signature `Fanout(outputs ...slog.Handler) slog.Handler`. The fanout has no method beyond `slog.Handler`, and `New` recognizes its unexported type with a type assertion. | An exported `FanoutHandler` type adds an identifier and four methods that no caller needs outside the `slog.Handler` interface, against the minimal-surface rule of Principle II. A named slice type is not the requested signature, and the caller could modify it after `New` validated it ([research R-02](research.md#r-02-api-shape-one-handler-three-additions)). |

# Implementation Plan: Composable Structured Logging Handlers (slogx)

**Branch**: `006-slogx-handlers` (suggested; work currently on `main`) | **Date**: 2026-10-19 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `specs/006-slogx-handlers/spec.md`

**Note**: This template is filled in by the `/speckit-plan` command; its definition describes the execution workflow.

## Summary

This feature adds a new independent module, `github.com/paccolamano/go/slogx`. It provides
`slog.Handler` middleware and no logger type:

- **Context attributes**: `With(ctx, attrs...)` attaches attributes to a context, and
  `ContextHandler` writes them at the top level of every record. Extraction functions
  (`WithContextFunc`) read values that other packages stored in the context. The record's
  attributes win on duplicate keys.
- **Redaction**: `RedactHandler` replaces the values of sensitive keys (`WithRedactKeys`) with
  `[REDACTED]`, case-insensitively and at every depth. This covers logger attributes and the
  groups that `LogValuer` values resolve to. `Secret[T]` renders as `[REDACTED]` everywhere.
- **Per-request level**: `WithLevel(ctx, level)`, applied by `LevelHandler`.
- **Sampling**: `SamplingHandler` keeps the first N records of each (level, message) per
  interval, then one every M, and emits one summary per group with drops. Counts are exact and
  memory is bounded.
- **Composition**: `New(next, opts...)` builds the only correct order, and `Flush` emits pending
  summaries.
- **Test recorder**: `slogxtest.Recorder` captures normalized records for assertions.

Design decisions validated by a spike ([research.md](research.md)):

- **Context attributes under open groups** (R-04): the handler keeps a "root" view of the next
  handler, without groups. When groups are open, the record is sent to that root with the groups
  rebuilt as nested attributes. `slogtest` passes on both paths.
- **Redaction** (R-07): it checks the key before resolving the value, makes a non-allocating
  first pass, and "seals" a `WithGroup` whose name is a sensitive key.
- **Secret** (R-08): it stores a pointer, so that `fmt` cannot print the value through an
  unexported struct field.
- **Sampling** (R-10): a mutex-guarded map in a tumbling window, with lazy summaries and no
  goroutine. Exact under 50 concurrent goroutines with `-race`.
- **Costs** (R-14):
  - 0 allocations for disabled records, everywhere;
  - the full pipeline takes 1.6× the time of the bare JSON handler writing the same attributes.

The module uses the standard library only. Tests use `testing`, `testing/slogtest`,
`testing/synctest`, `AllocsPerRun`, benchmarks with `b.Loop`, and `FuzzRedact`.

## Technical Context

**Language/Version**: Go. The `go` directive is `go 1.26.0`, the oldest version the
constitution supports. CI also tests on 1.27. The features used are `testing/synctest` and
`sync.WaitGroup.Go` (1.25), `testing.B.Loop` (1.24), `testing/slogtest.Run` (1.22), `log/slog`,
`slices`, `cmp` and `clear` (1.21).

**Primary Dependencies**: the standard library only (`cmp`, `context`, `errors`, `fmt`, `io`,
`log/slog`, `slices`, `strings`, `sync`, `time`), and no third-party module, test-only ones
included. There is no dependency on other modules of this repository either
([research.md R-17](research.md#r-17-dependencies)).

**Storage**: none. State lives in contexts, in immutable handlers and in the sampler's bounded
in-memory table.

**Testing**: the standard `testing` package.

- External test packages `slogx_test` and `slogxtest_test`, and internal tests for the sampler
  bound and `internal/attrs`.
- `slogtest.Run` for each handler, with the slow path forced (R-04).
- `testing/synctest` for intervals, `-race` for concurrency, `AllocsPerRun` for the disabled
  path.
- `FuzzRedact`, benchmarks, and an `Example` for each primary API.

Details: [research.md R-16](research.md#r-16-testing-approach-principle-iii).

**Target Platform**: any platform Go supports. The code is pure Go with no build constraints,
and builds for linux, darwin and windows on amd64 and arm64.

**Project Type**: library. It is one Go module (`slogx/go.mod`) with the packages `slogx`,
`slogx/slogxtest` and `slogx/internal/attrs`.

**Performance Goals** (SC-003, SC-008, research R-14):

- Disabled records: 0 allocations for every handler and for `New`. The spike measured 0.
- Enabled record with 5 attributes and 2 context attributes, no open group: the full pipeline
  takes at most 2× the time of the bare JSON handler writing the same 7 attributes. The spike
  measured 1.6× (1,348 ns against 830 ns).
- Standalone overheads in the spike: redaction +112 ns, sampling +119 ns, both with 0
  allocations.

**Constraints**:

- No package-level mutable state. The only package-level variable is `ErrInvalidConfig`, and
  the limits are `const`: 1,024 tracked groups and a depth of 64.
- No `init`, no `panic`, no output, no logging of its own, and no goroutine.
- `recover` is used only around extraction functions (R-06). `LogValue` panics are recovered by
  `slog.Value.Resolve`.

**Scale/Scope**: about 900 lines of library code in about 11 files. The API has 60 exported
identifiers, counting functions, types, methods, options and the variable: 47 in `slogx` and 13
in `slogxtest` ([contracts/public-api.md](contracts/public-api.md)).

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

| Rule | Assessment | Status |
|------|------------|--------|
| **I. Independent modules** | A single new top-level module `slogx/`, with module path `github.com/paccolamano/go/slogx`, its own `go.mod`, a README.md and a CHANGELOG.md. It has no `replace` and no dependency on other repository modules, and builds and tests with `GOWORK=off`. Its single purpose is composable `slog` handlers. The name describes the content. The `middleware` module will adopt `slogxtest` only after `slogx/v0.1.0` is tagged, in a separate change. | ✅ |
| **II. SemVer & stable API** | Tags are `slogx/vX.Y.Z`, starting at v0.1.0. The exported surface is the minimum the spec asks for: every identifier in [public-api.md](contracts/public-api.md) maps to an FR. Every constructor takes `...Option`, so options can be added compatibly (R-02). Handler internals, limits and placeholders are unexported constants. `slogxtest.Record` may gain fields. Every change goes into the CHANGELOG. | ✅ |
| **III. Test-first** | `tasks.md` will order test tasks before implementation tasks. The suite uses the standard `testing` package with table-driven subtests. It is deterministic: `synctest` gives virtual time, with no sleeps on real time, no network and no shared state. Benchmarks cover the code declared performance-sensitive (SC-003, SC-008). `FuzzRedact` backs SC-004. The module parses no external input, so fuzzing is not mandatory. There are `Example`s for every primary API. Coverage target: ≥ 80%. | ✅ |
| **IV. Idiomatic Go** | The repository's golangci-lint config applies unchanged. The spike showed two findings, each confined to one justified place (R-15): `//nolint:wrapcheck` on the single `handle` forwarder, and `//nolint:contextcheck` on the background context of the summaries. Every exported identifier and both packages are documented. Configuration errors wrap the sentinel `ErrInvalidConfig`. There is no `panic` or `init`, and no global mutable state. The module writes nothing on its own: it only passes records to the injected next handler. It starts no goroutine (FR-033). `Flush` performs I/O, so it takes a `context.Context`. | ✅ (two justified `nolint`) |
| **V. Simplicity & dependencies** | No third-party dependencies. Every abstraction maps to a requirement: one `Option` type for all constructors, one handler type per capability, `Handler` for `New` and `Flush`, and `internal/attrs` shared by two packages. There are no configurable limits, no interfaces of its own and no cgo. | ✅ |
| Go toolchain support | `go 1.26.0` is not above `GO_VERSION_OLDEST=1.26`, and every feature used exists in 1.26 (Technical Context). `go vet`'s `stdversion` check passed in the spike. | ✅ |
| Platforms | The code is pure Go with no OS-specific calls. There is no `.module-exceptions` entry. | ✅ |
| Repository layout | No tooling change. `make work` adds `./slogx` to `go.work`, and the shared lint config is used without overrides. | ✅ |
| Dependency hygiene | With no `require` list, `go mod tidy -diff` is clean. | ✅ |
| Vulnerability scanning / Secrets | Trivy finds no dependencies to scan. Tests and examples use obviously fake values (`hunter2`, `s3cr3t`), and no real credentials. Gitleaks and Bearer must not flag them: if a fixture trips a rule, it is renamed rather than suppressed. The module's purpose, keeping secrets out of logs, helps the Bearer privacy scan of consumer projects. | ✅ |
| Local and PR gates | `make check MODULE=slogx` runs lint, then race and shuffle tests with the coverage gate. The tooling discovers the new module on its own. | ✅ |
| Commits / Release | The PR title is `feat(slogx): add composable slog handlers module`. The release is `slogx/v0.1.0`, with a CHANGELOG section. | ✅ |

**Result (pre-research)**: PASS, with no deviation needing a Complexity Tracking entry.

**Result (post-design re-check)**: PASS.

- The data model and the two contracts add no dependency, no global state, no goroutine and no
  module exception.
- Every exported identifier in [contracts/public-api.md](contracts/public-api.md) maps to a
  functional requirement (traceability table).
- The only lint suppressions are the two of R-15.
- One refinement to the spec came out of the research, and the spec and its checklist were
  updated (checklist, Iteration 3): SC-008 now compares against the output handler writing the
  same attributes.

## Project Structure

### Documentation (this feature)

```text
specs/006-slogx-handlers/
├── spec.md                  # feature specification
├── plan.md                  # this file
├── research.md              # Phase 0: decisions R-01 … R-18, with spike measurements
├── data-model.md            # Phase 1: context values, handler state, sampler state machine, recorder
├── quickstart.md            # Phase 1: validation guide
├── contracts/
│   ├── public-api.md        # complete exported Go API, with FR traceability
│   └── record-semantics.md  # placement, precedence, redaction, level, sampling, summaries, placeholders
├── checklists/
│   └── requirements.md      # spec quality checklist
└── tasks.md                 # Phase 2 (/speckit-tasks, not created here)
```

### Source Code (repository root)

```text
go.work                          # + ./slogx (make work)

slogx/
├── go.mod                       # module github.com/paccolamano/go/slogx; go 1.26.0; no require
├── README.md                    # purpose, go get, composed example, min Go 1.26, recorder link
├── CHANGELOG.md                 # Keep a Changelog; [Unreleased] → [0.1.0]
├── doc.go                       # package doc: context-only methods + sloglint, composition order,
│                                # redaction coverage and limits, sampling and summaries, placeholders
├── options.go                   # Option, WithContextFunc, WithRedactKeys, WithSampling,
│                                # WithSampleErrors, ErrInvalidConfig, validation, applicability
├── context.go                   # With, WithLevel, context keys, merge
├── forward.go                   # handle(): the single nolint:wrapcheck forwarder
├── level.go                     # LevelHandler
├── contexthandler.go            # ContextHandler: root/next, topKeys, ops, fast and slow paths
├── redact.go                    # RedactHandler: match, two-pass Handle, sealing
├── secret.go                    # Secret[T]
├── sampling.go                  # SamplingHandler, sampler, summaries, Flush
├── handler.go                   # Handler, New
├── internal/
│   └── attrs/
│       ├── attrs.go             # Flatten (inline empty-key groups), MaxDepth, DepthPlaceholder
│       └── attrs_internal_test.go
│
├── context_test.go              # With/WithLevel merge, nil ctx (US1-4, US1-5, FR-010)
├── contexthandler_test.go       # placement, precedence, extraction (US1)
├── level_test.go                # US3
├── redact_test.go               # US2-1 … US2-5, US2-7, sealing, LogValuer groups
├── secret_test.go               # US2-6: verbs, JSON, text handler, unexported fields
├── sampling_test.go             # US6 with synctest; Flush; errors option
├── sampling_internal_test.go    # 1,024-group bound (US6-4, SC-006)
├── handler_test.go              # US5: composed scenario, manual equivalence, option errors
├── panic_test.go                # FR-005: LogValue panic, extraction panic, depth, nil ctx
├── conformance_test.go          # slogtest.Run for every handler, slow path forced (FR-003)
├── alloc_test.go                # AllocsPerRun == 0 on disabled records (FR-004)
├── bench_test.go                # BenchmarkDisabled, BenchmarkEnabled (SC-003, SC-008)
├── fuzz_test.go                 # FuzzRedact (SC-004)
├── example_test.go              # Example* for New, With, WithLevel, the handlers, Secret
│
└── slogxtest/
    ├── recorder.go              # package doc, Recorder, Option, WithLevel, normalization
    ├── record.go                # Record, Record.Attr
    ├── recorder_test.go         # US4, concurrency, slogtest conformance
    └── example_test.go          # ExampleRecorder
```

**Structure Decision**: the feature is one new top-level module, `slogx/`, laid out as above.

- **Package `slogx`** holds every handler, so that `New` can compose them, and they share one
  option type.
- **`slogx/slogxtest`** keeps test helpers out of the production API. `internal/attrs` holds the
  two helpers both packages need.
- **Files** are split by capability. Each has its own test file, plus cross-cutting files for
  conformance, allocations, panics, benchmarks and fuzzing.
- **Nothing else changes** in the repository except `go.work`, which `make work` regenerates.
  Migrating the `middleware` capture handlers to `slogxtest.Recorder` is a separate, later
  change in the `middleware` module, after `slogx/v0.1.0` is tagged.

## Complexity Tracking

There are no constitution violations to justify. The constitution allows the two `nolint`
directives of research R-15 (`wrapcheck` on the single forwarder, `contextcheck` on the
summaries' background context), because each names its linter and states its reason.

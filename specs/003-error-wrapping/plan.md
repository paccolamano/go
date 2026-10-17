# Implementation Plan: Error Wrapping and Error Detail Module

**Branch**: `003-error-wrapping` (suggested; work currently on `main`) | **Date**: 2026-10-16 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `specs/003-error-wrapping/spec.md`

**Note**: This template is filled in by the `/speckit-plan` command; its definition describes the execution workflow.

## Summary

This feature adds a new independent module, `github.com/paccolamano/go/errors`: a single package
`errors` that is a **drop-in replacement** for the standard library's `errors` package.

- **Standard surface**: `Is`, `As`, `AsType` and `Unwrap` forward to the standard functions, and
  `ErrUnsupported` is the standard value itself.
- **Constructors**: `New`, `Errorf`, `Wrap`, `Wrapf`, `WithStack` and `Join`. Each records the
  location of its caller, plus the call stack once per chain, at its origin.
- **Sentinels**: errors created during package initialization, which is how sentinels are
  declared, record nothing.
- **Caller skipping**: `type Skip int` mirrors every constructor, with one numbering everywhere.
- **Custom types**: they embed `BaseError` and are built with the generic `Bind`.
- **Detail**: `Describe(err) Detail` returns the detail of any error as data, with stable JSON
  names. `Detail.String()` and `%+v` render a readable, injection-safe text.
- **slog**: `Attr` and `DetailValue` provide an opt-in slog attribute that is resolved lazily.

Design decisions validated by a spike ([research.md](research.md)):

- **Messages** are composed at construction, exactly as `fmt.Errorf` composes them. `Errorf`
  delegates to `fmt.Errorf`, so `go vet` accepts `%w` in `Errorf` and rejects it in `Wrapf`
  (R-03, R-04).
- **Two unexported node types**: a 48-byte `*wrapError` (zero or one cause) and a `*joinError`
  (several causes). A plain wrap stores one PC; an origin stores up to 32 PCs. Names are
  resolved only when a detail is produced (R-03, R-05, R-06).
- **Package initialization** is detected from the bottom of the captured stack: `runtime.main`
  is present and the first non-runtime frame is not `main.main`. The check costs about 56 ns
  (R-07).
- **Locations** use `<import path>/<file>:<line>`, with no build directory (R-09).
- **slog's text handlers** format values with `%+v`. Every library error therefore implements
  `slog.LogValuer` and resolves to a plain view, so that ordinary log lines stay identical to
  those of standard errors (R-12, R-14).

Everything is built on the standard library only. Tests use `testing`, table-driven subtests,
fuzzing (`FuzzDetailText`), benchmarks with `b.Loop`, and an opt-in vet check behind the build
tag `vetcheck`.

## Technical Context

**Language/Version**: Go. The `go` directive is `go 1.26.0`, the oldest version the
constitution supports. CI also tests on 1.27. The features used are `errors.AsType` (1.26),
`testing.B.Loop` (1.24), `reflect.TypeFor` (1.22), `log/slog` and `max` (1.21), and
`fmt.FormatString` (1.20).

**Primary Dependencies**: the standard library only (`errors`, `fmt`, `io`, `log/slog`, `path`,
`reflect`, `runtime`, `strconv`, `strings`), and no third-party module, test-only ones included
([research.md R-19](research.md#r-19-dependencies)).

**Storage**: none. Errors are immutable in-memory values.

**Testing**: the standard `testing` package.

- External test package `errors_test`, with internal tests for the unexported helpers.
- Line expectations captured with `runtime.Caller` helpers.
- Package-level sentinels declared in the test files, to test package initialization.
- Built-in slog handlers writing into buffers.
- A fuzz test for the text layout.
- Benchmarks against `fmt.Errorf`.
- `go vet` on `testdata/vetmisuse`, behind `-tags vetcheck`.

Details: [research.md R-18](research.md#r-18-testing-approach-principle-iii).

**Target Platform**: any platform Go supports. The code is pure Go with no build constraints,
and builds for linux, darwin and windows on amd64 and arm64.

**Project Type**: library. It is one Go module with one package (`errors/go.mod`), and no
subpackages or `internal/`.

**Performance Goals** (SC-004, as refined by research R-16):

- A wrap of an error that already carries a call stack costs at most 3× the time and 2× the
  memory of `fmt.Errorf("ctx: %w")`. The spike measured about 1.9× and about 1.4×.
- An origin created with `New` at a call depth of 10 takes under 1.5 µs. The spike measured
  about 1.0 µs.
- The detail of a pathological tree takes under 100 ms (SC-005).

**Constraints**:

- No package-level mutable state. The only package-level variable is the `ErrUnsupported`
  alias, and the limits are `const`.
- No `init`, no `panic`, no output and no logging.
- `recover` is used only to survive panicking foreign `Error` and `Unwrap` methods during
  `Describe`.
- Bounded work: 32 stack frames, 64 levels of depth and 256 levels per detail.

**Scale/Scope**: about 1,000 lines of library code in about 11 files. The API has 34 exported
identifiers, counting functions, types, methods and the variable
([contracts/public-api.md](contracts/public-api.md)).

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

| Rule | Assessment | Status |
|------|------------|--------|
| **I. Independent modules** | A single new top-level module `errors/`, with module path `github.com/paccolamano/go/errors`, its own `go.mod`, a README.md and a CHANGELOG.md. It has no `replace` and no dependency on other repository modules, and it builds and tests with `GOWORK=off`. Its single purpose is error creation, wrapping and detail. The name describes the content and is not a grab-bag name. | ✅ |
| **II. SemVer & stable API** | Tags are `errors/vX.Y.Z`, starting at v0.1.0. The exported surface is the minimum the spec asks for: every identifier in [public-api.md](contracts/public-api.md) maps to an FR. The node types, the call-stack representation, the limits and the slog key are unexported. Extension happens through new fields of `Level`, `Location`, `Stack` and `Detail` (each with `omitempty`), new `Skip` methods, or new methods of `Custom`. All of these are compatible: outside packages cannot implement `Custom`, because it has an unexported method. The JSON field names are part of the contract. Every change goes into the CHANGELOG. | ✅ |
| **III. Test-first** | `tasks.md` will order test tasks before implementation tasks. The suite uses the standard `testing` package with table-driven subtests. It is deterministic: no time, no network and no shared state, since the package-level sentinels in the test files are immutable fixtures. `FuzzDetailText` covers external input (messages) that ends up in the text layout. The benchmarks cover the code declared performance-sensitive (SC-004). There are `Example`s for every primary API. Coverage target: ≥ 80%. The vet test spawns the go toolchain, so it runs only with `-tags vetcheck` and stays out of the default `go test`. | ✅ |
| **IV. Idiomatic Go** | The repository's golangci-lint config applies unchanged. The spike showed exactly one finding, `errorlint` on the type switch over `Unwrap`, confined to the `causes` helper with `//nolint:errorlint // <reason>` (R-17). Every exported identifier and the package are documented (`doc.go`). Conditions that callers branch on are types usable with `errors.As`/`AsType` (`BaseError`-based types) or sentinels. There is no `panic`, no `init` and no global mutable state. There is no I/O, so no `context.Context` is needed. No goroutines are started. The library never logs: `Attr` and `DetailValue` only build values for the caller's logger. | ✅ (one justified `nolint`) |
| **V. Simplicity & dependencies** | No third-party dependencies. Every abstraction maps to a requirement: `Skip` (FR-017), `BaseError`, `Custom` and `Bind` (FR-026 to FR-029), the description types (FR-024), `Attr` and `DetailValue` (FR-030). The `LogValuer` masking is the minimum needed to keep FR-023 and FR-030 true together (R-14). There are no options, no configurable limits, no global toggles and no cgo. | ✅ |
| Go toolchain support | `go 1.26.0` is not above `GO_VERSION_OLDEST=1.26`, and every feature used exists in 1.26 (Technical Context). | ✅ |
| Platforms | The code is pure Go. `runtime.Callers` and `runtime.CallersFrames` behave the same on all six targets, which the `cross-build` status covers. There is no `.module-exceptions` entry. | ✅ |
| Repository layout | No tooling change. Running `make work` adds `./errors` to `go.work`, and the shared lint config is used without overrides. The `testdata/vetmisuse` fixture is ignored by the gates' `./...` patterns (verified). | ✅ |
| Dependency hygiene | With an empty `require` list, `go mod tidy -diff` is clean. | ✅ |
| Vulnerability scanning / Secrets | Trivy finds no dependencies to scan. Tests and examples contain no credentials, and their example hosts use `example.com`. Locations never include build-machine paths (FR-018), which also helps the Bearer privacy scan. | ✅ |
| Local and PR gates | `make check MODULE=errors` runs lint, then race and shuffle tests with the coverage gate. The tooling discovers the new module on its own. | ✅ |
| Commits / Release | The PR title is `feat(errors): add error wrapping and error detail module`. The release is `errors/v0.1.0`, with a CHANGELOG section. | ✅ |

**Result (pre-research)**: PASS, with no deviation needing a Complexity Tracking entry.

**Result (post-design re-check)**: PASS.

- The data model and the three contracts add no dependency, no global state and no module
  exception.
- Every exported identifier in [contracts/public-api.md](contracts/public-api.md) maps to a
  functional requirement.
- The only lint suppression is the `errorlint` one of R-17.
- Three refinements to the spec came out of the research, and the spec and its checklist were
  updated accordingly (checklist, Iteration 3):
  - SC-004: realistic cost targets;
  - FR-023: the exact verb contract;
  - FR-024: no per-level full message, which would grow quadratically.

## Project Structure

### Documentation (this feature)

```text
specs/003-error-wrapping/
├── spec.md               # feature specification
├── analysis.md           # analysis of the reference library (sorintlab/errors)
├── plan.md               # this file
├── research.md           # Phase 0: decisions R-01 … R-20, with spike measurements
├── data-model.md         # Phase 1: nodes, invariants, description values, slog values
├── quickstart.md         # Phase 1: validation guide
├── contracts/
│   ├── public-api.md     # complete exported Go API
│   ├── detail-format.md  # derivation rules, text grammar, JSON, formatting verbs
│   └── log-attributes.md # slog attribute shapes and plain-value masking
├── checklists/
│   └── requirements.md   # spec quality checklist
└── tasks.md              # Phase 2 (/speckit-tasks, not created here)
```

### Source Code (repository root)

```text
go.work                        # + ./errors (make work)

errors/
├── go.mod                     # module github.com/paccolamano/go/errors; go 1.26.0; no require
├── README.md                  # purpose, go get, example, min Go 1.26, migration, %+v and slog notes
├── CHANGELOG.md               # Keep a Changelog; [Unreleased] → [0.1.0]
├── doc.go                     # package doc: location model, sentinels, detail format, limits,
│                              # custom types, logging, AsType vs As, using next to stdlib
├── std.go                     # Is, As, AsType, Unwrap, ErrUnsupported (drop-in forwarders)
├── errors.go                  # wrapError, joinError; New, Errorf, Wrap, Wrapf, WithStack, Join
├── skip.go                    # Skip and its methods
├── trace.go                   # capture, callStack, isTraced, causes (the one errorlint nolint),
│                              # package-initialization check
├── location.go                # PC → Location: CallersFrames, %2e unescaping, import path + file
├── custom.go                  # BaseError, Custom, Bind
├── detail.go                  # Detail, Level, Location, Stack; Describe (walk, own text, merge,
│                              # hidden types, limits, recover)
├── text.go                    # Detail.String(): text layout and quoting
├── format.go                  # shared fmt.Formatter implementation
├── slog.go                    # Attr, DetailValue, detailValuer, plain view, LogValue methods
│
├── std_test.go                # drop-in parity (US1-8, FR-001)
├── errors_test.go             # constructors, messages, causes, nil handling (US1)
├── skip_test.go               # skip numbering (FR-017)
├── trace_test.go              # origin rule, package-level sentinels (US5, FR-008, FR-016)
├── location_internal_test.go  # import path + file derivation, %2e, build-dir independence
├── custom_test.go             # BaseError/Bind, overrides, AsType (US3)
├── detail_test.go             # Describe, own text, merge, types, branches, limits, panics (US2)
├── text_test.go               # text layout, quoting, hostile messages
├── format_test.go             # verb parity (FR-023)
├── slog_test.go               # Attr, DetailValue, laziness, plain-value masking (US4)
├── fuzz_test.go               # FuzzDetailText
├── bench_test.go              # SC-004 benchmarks
├── example_test.go            # Example* functions
├── vet_test.go                # //go:build vetcheck: TestVetReportsWrapfMisuse
└── testdata/
    └── vetmisuse/vetmisuse.go # deliberate Wrapf %w misuse and valid Errorf calls
```

**Structure Decision**: the feature is one new top-level module, `errors/`, laid out as above.

- It contains a single package, imported as `github.com/paccolamano/go/errors`, so that
  replacing the standard import is the only change consumers make.
- Code is split by concern: creation, skip, trace, location, custom types, detail, text,
  formatting and slog. Each file has its own test file.
- Nothing else in the repository changes except `go.work`, which `make work` regenerates.

## Complexity Tracking

There are no constitution violations to justify. The constitution allows the single
`//nolint:errorlint` directive (research R-17), because it names its linter and gives its reason.

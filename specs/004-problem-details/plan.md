# Implementation Plan: RFC 9457 Problem Details Module

**Branch**: `004-problem-details` (suggested; work currently on `errors-release-0.1.0`) | **Date**: 2026-10-18 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `specs/004-problem-details/spec.md`

**Note**: This template is filled in by the `/speckit-plan` command; its definition describes the execution workflow.

## Summary

This feature adds a new independent module, `github.com/paccolamano/go/problem`: a single package
`problem` that produces and reads RFC 9457 problem details in JSON.

- **Declarations**:
  - `NewType(uri, title, status)` declares a problem type.
  - `NewKey[T](name)` declares an extension member and the Go type of its value.
  - Both check the RFC's rules when they are declared, and the generic `Must` turns them into
    package-level values.
- **Occurrences**:
  - `Type.New(opts...)` and `Status(code, opts...)` build an immutable `*problem.Error`.
  - The options are `WithDetail`, `WithTitle`, `WithInstance`, `WithCause` and `WithExtension`.
  - `*Error` is a Go `error`: it can be wrapped and combined, and `Type.Match` finds it anywhere
    in a tree.
- **Writing**: `Write(w, r, err)` answers with any error.
  - The first `*Error` in the tree is written as is.
  - Any other error is answered with the generic 500 `about:blank` problem.
  - A problem that cannot be written is replaced by that same generic problem, and the reason
    is returned.
- **Reading**:
  - `Read(resp)` recognizes `application/problem+json`, reads at most 1 MiB, applies the RFC's
    rules for consumers and resolves relative URIs.
  - `Parse(data)` does the same for a document alone.
  - `Key.Lookup` reads extension values with their Go type.

Design decisions validated by a spike ([research.md](research.md)):

- **Static and per-request values**: values fixed in the source code are checked when declared,
  so that `Must` fails at program start. Values computed for each request are checked when the
  occurrence is built, and reported by `Write` (R-03).
- **Extension values** are marshaled when the occurrence is built. This keeps it immutable, and
  makes writing deterministic and cheap (R-05).
- **Reading** decodes into a `map[string]json.RawMessage`. Struct decoding in `encoding/json`
  is case-insensitive (`"TYPE"` fills `type`), and `encoding/json/v2` is still experimental in
  Go 1.27 (R-05, R-09).
- **Names**: the occurrence type is `problem.Error`, because `errname` requires the `Error`
  suffix and `problem.Problem` stutters (R-02).
- **Measurements**: writing takes about 3–4 µs, and parsing a 1 MiB document about 5 ms
  (SC-005 allows 100 ms) (R-13).

Everything is built on the standard library only. Tests use `testing`, `httptest`, table-driven
subtests, an RFC 9457 Appendix A conformance helper, a leak corpus, fuzzing (`FuzzParse`) and
benchmarks with `b.Loop`.

## Technical Context

**Language/Version**: Go. The `go` directive is `go 1.26.0`, the oldest version the
constitution supports. CI also tests on 1.27. The features used are `testing.T.Context` (1.24),
`testing.B.Loop` (1.24), `maps.Keys` and `slices.Sorted` (1.23), and generics (1.18). Nothing
requires a version above 1.26.

**Primary Dependencies**: the standard library only (`bytes`, `encoding/json`, `errors`, `fmt`,
`io`, `maps`, `math`, `mime`, `net/http`, `net/url`, `slices`, `strconv`, `strings`). There is no
third-party module, test-only ones included, and no other module of this repository
([research.md R-17](research.md#r-17-dependencies)).

**Storage**: none. Declarations and occurrences are immutable in-memory values.

**Testing**: the standard `testing` package.

- External test package `problem_test`, plus internal tests for URI and name validation.
- `httptest.NewRecorder` for writing. Requests are built with
  `httptest.NewRequestWithContext(t.Context(), …)`, as `noctx` requires.
- `http.Response` values built in memory for reading, with counting and failing readers. There
  is no network access.
- A schema helper for RFC 9457 Appendix A, a leak corpus, concurrency under `-race`, and
  `FuzzParse`.
- Benchmarks for `Write` and for `Parse` of 1 MiB.

Details: [research.md R-16](research.md#r-16-testing-approach-principle-iii).

**Target Platform**: any platform Go supports. The code is pure Go with no build constraints,
and builds for linux, darwin and windows on amd64 and arm64.

**Project Type**: library. It is one Go module with one package (`problem/go.mod`), and no
subpackages or `internal/`.

**Performance Goals**: none beyond SC-005, because the module runs on error paths.

- Reading any input of up to 1 MiB takes under 100 ms. The spike measured about 5 ms.
- `BenchmarkWrite` is recorded for reference. The spike measured about 3–4 µs.

**Constraints**:

- **No global mutable state**: the only package-level variables are the three `Err*`
  sentinels. Limits are `const`.
- **No side effects**: no `init`, and no output or logging.
- **One panic site**: `Must`, for static declarations (R-03).
- **Bounded reading**: at most 1 MiB is read, plus one byte, and JSON nesting is limited to
  10,000 by `encoding/json`.
- **No partial responses**: nothing is written to the response until the whole document is
  encoded.

**Scale/Scope**: about 600 lines of library code in 8 files. The API has 31 exported
identifiers, counting the constant, the variables, types, functions and methods
([contracts/public-api.md](contracts/public-api.md)).

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

| Rule | Assessment | Status |
|------|------------|--------|
| **I. Independent modules** | A single new top-level module `problem/`, with module path `github.com/paccolamano/go/problem`, its own `go.mod`, a README.md and a CHANGELOG.md. It has no `replace` and no dependency on `errors` or `middleware`, and it builds and tests with `GOWORK=off`. Its single purpose is RFC 9457 problem details, produced and read. The name describes the content and is not a grab-bag name. | ✅ |
| **II. SemVer & stable API** | Tags are `problem/vX.Y.Z`, starting at v0.1.0. The exported surface is the minimum the spec asks for: every identifier in [public-api.md](contracts/public-api.md) maps to an FR. `Type` has no accessors, and `Key` has only `Lookup`, because no requirement needs more. Fields are unexported, and the limits are unexported constants. Extension happens through new options, accessors and sentinels, all of which are compatible. The document layout ([document.md](contracts/document.md)) and the `Error()` format are documented behavior. Every change goes into the CHANGELOG. | ✅ |
| **III. Test-first** | `tasks.md` will order test tasks before implementation tasks. The suite uses the standard `testing` package with table-driven subtests. It is deterministic: no time, no network and no shared state, since the package-level declarations in the test files are immutable fixtures. `FuzzParse` covers the external input (problem documents read from other APIs). The benchmarks back SC-005. There are `Example`s for every exported capability (FR-003). Coverage target: ≥ 80%. | ✅ |
| **IV. Idiomatic Go** | The repository's golangci-lint config applies unchanged. The spike's only library finding was a De Morgan rewrite. There are two justified directives: `//nolint:forbidigo` on the panic in `Must` (R-03) and `//nolint:errorlint` on the type switch of the tree walk in `Match` (R-08). Every exported identifier and the package are documented (`doc.go`). Conditions that callers branch on are sentinels (`ErrInvalid`, `ErrNotProblem`, `ErrMalformed`) and the `*Error` type. There is no `panic` for recoverable conditions: a refused declaration returns an error, and `Must` is the opt-in for static values. There is no `init`, no global mutable state, no goroutine and no logging. `Write` and `Read` take no `context.Context`, a deviation recorded in Complexity Tracking (R-11). | ✅ (2 justified `nolint`, 1 tracked deviation) |
| **V. Simplicity & dependencies** | No third-party dependencies, and no JSON Schema library in tests. Every abstraction maps to a requirement: `Key[T]` (FR-006, FR-024 typed reading), `Must` (FR-008 checks at declaration), `Option` (FR-009), `Match` (FR-021). There are no configurable limits, no global toggles, no content negotiation, no `json.Marshaler` or `slog.LogValuer`, and no cgo. | ✅ |
| Go toolchain support | `go 1.26.0` is not above `GO_VERSION_OLDEST=1.26`, and every feature used exists in 1.26 (Technical Context). `encoding/json/v2` is deliberately not used (R-09). | ✅ |
| Platforms | The code is pure Go, with only `net/http`, `net/url`, `mime` and `encoding/json` behavior, which is identical on all six targets. The `cross-build` status covers them. There is no `.module-exceptions` entry. | ✅ |
| Repository layout | No tooling change. Running `make work` adds `./problem` to `go.work`, and the shared lint config is used without overrides. | ✅ |
| Dependency hygiene | With an empty `require` list, `go mod tidy -diff` is clean. | ✅ |
| Vulnerability scanning / Secrets | Trivy finds no dependencies to scan. The leak corpus uses obviously fake, low-entropy values (`password authentication failed for user "admin"`, `password=hunter2`) that Gitleaks's rules do not flag. If one is flagged anyway, it gets a narrow `.gitleaksignore` entry with a justification. Example hosts use `example.com` and `example.net`. The library never sends error messages to clients (FR-020), which also helps the Bearer privacy scan. | ✅ |
| Local and PR gates | `make check MODULE=problem` runs lint, then race and shuffle tests with the coverage gate. The tooling discovers the new module on its own. | ✅ |
| Commits / Release | The PR title is `feat(problem): add RFC 9457 problem details module`. The release is `problem/v0.1.0`, with a CHANGELOG section. | ✅ |

**Result (pre-research)**: PASS. One deviation (no `context.Context` on `Write` and `Read`) is
justified in Complexity Tracking.

**Result (post-design re-check)**: PASS.

- The data model and the two contracts add no dependency, no global state and no module
  exception.
- Every exported identifier in [contracts/public-api.md](contracts/public-api.md) maps to a
  functional requirement.
- The lint suppressions are the two of R-14.
- Four refinements to the spec came out of the research ([R-15](research.md#r-15-refinements-to-the-spec)),
  and the spec and its checklist were updated accordingly (checklist, Iteration 3):
  - FR-006: extension names are checked when declared, and standard names are reserved in any
    capitalization;
  - FR-007 and FR-016: an invalid instance is reported by `Write`;
  - FR-008: `about:blank` cannot be declared;
  - FR-023: an invalid `instance` read is treated as absent.

## Project Structure

### Documentation (this feature)

```text
specs/004-problem-details/
├── spec.md               # feature specification
├── plan.md               # this file
├── research.md           # Phase 0: decisions R-01 … R-18, with spike measurements
├── data-model.md         # Phase 1: declarations, occurrences, validation, selection
├── quickstart.md         # Phase 1: validation guide
├── contracts/
│   ├── public-api.md     # complete exported Go API
│   └── document.md       # wire format: response, document produced, reading rules
├── checklists/
│   └── requirements.md   # spec quality checklist
└── tasks.md              # Phase 2 (/speckit-tasks, not created here)
```

### Source Code (repository root)

```text
go.work                        # + ./problem (make work)

problem/
├── go.mod                     # module github.com/paccolamano/go/problem; go 1.26.0; no require
├── README.md                  # purpose, go get, producing and reading examples, min Go 1.26,
│                              # middleware refusal snippet
├── CHANGELOG.md               # Keep a Changelog; [Unreleased] → [0.1.0]
├── doc.go                     # package doc: declarations and Must, type URIs, writing, what
│                              # never reaches the client, reading and limits, middleware
├── problem.go                 # MediaType, sentinels; Error, accessors, Error(), Unwrap;
│                              # Status; generic server error
├── type.go                    # Type, NewType, Type.New, Type.Match (the errorlint nolint),
│                              # Must (the forbidigo nolint)
├── extension.go               # Key, NewKey, Key.Lookup, WithExtension, name rule
├── option.go                  # Option, WithDetail, WithTitle, WithInstance, WithCause
├── uri.go                     # URI reference validation, absolute check, resolution
├── write.go                   # Write, document encoding
├── read.go                    # Read, Parse, media type, size limit, member rules
│
├── problem_test.go            # occurrences, options order, message, cause (US2-3, US2-6, FR-009)
├── type_test.go               # NewType, Must, Match (US3-1, US3-2, US2-5)
├── extension_test.go          # NewKey, WithExtension, Lookup (US3-3, FR-004)
├── write_test.go              # US1, leak corpus and selection (US2), concurrency, edge cases
├── read_test.go               # US4, edge cases of reading
├── uri_internal_test.go       # URI reference and name validation tables (R-04, R-05)
├── helpers_test.go            # Appendix A schema helper, counting and failing readers and
│                              # writers, shared declarations
├── fuzz_test.go               # FuzzParse
├── bench_test.go              # BenchmarkWrite, BenchmarkParse
└── example_test.go            # Example* functions
```

**Structure Decision**: the feature is one new top-level module, `problem/`, laid out as above.

- It contains a single package, imported as `github.com/paccolamano/go/problem`, which serves
  both producers and consumers of problem details.
- Code is split by concern: the occurrence, types, extensions, options, URIs, writing and
  reading. Each concern has its own test file.
- Nothing else in the repository changes except `go.work`, which `make work` regenerates.

## Complexity Tracking

| Violation | Why Needed | Simpler Alternative Rejected Because |
|-----------|------------|-------------------------------------|
| `Write(w, r, err)` and `Read(resp)` perform I/O without a `context.Context` parameter (Principle IV) | Both work inside an HTTP exchange whose context already governs cancellation. `Write` writes to the `ResponseWriter` of `r`, whose context is `r.Context()`, as `http.Error` and `http.ServeContent` do. `Read` reads a client response body, and `net/http` aborts a blocked body read when the context of `resp.Request` is canceled (R-11). | Adding `ctx` as the first parameter duplicates the request's context. Honoring it in `Read` would mean closing a body the caller owns, from a `context.AfterFunc` callback. The result is a wider API with no extra cancellation power. |

The two `//nolint` directives are allowed by the constitution, because each names its linter and
gives its reason. They are not deviations:

- `forbidigo` on the panic in `Must`, which is for static declarations only (R-03);
- `errorlint` on the tree walk in `Match` (R-08).

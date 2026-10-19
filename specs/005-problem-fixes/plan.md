# Implementation Plan: Problem Module Fixes and Improvements

**Branch**: `005-problem-fixes` (suggested; work currently on `main`) | **Date**: 2026-10-19 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `specs/005-problem-fixes/spec.md`

**Note**: This template is filled in by the `/speckit-plan` command; its definition describes the execution workflow.

## Summary

This feature changes the existing module `github.com/paccolamano/go/problem` (released as
`problem/v0.1.0`), and ships as `problem/v0.2.0`. It fixes two bugs, adds two capabilities and
corrects the documentation.

- **B1, headers**: before setting `Content-Type` and the status, `Write` deletes
  `Content-Length`, `Content-Encoding`, `Content-Range`, `ETag`, `Last-Modified`,
  `Content-Disposition` and `Trailer`. This is the list of the `recovery` middleware.
  - Every other header is kept.
  - The removal runs on the single path that every response takes, including the generic
    server error and HEAD.
  - In a spike, a test through `httptest.NewServer` reproduced both reported failures
    (`unexpected EOF`, `gzip: invalid header`), and passed after the fix (R-02).
- **B2, nil problem**: `Key.Lookup(nil)` returns the zero value and false, instead of panicking
  (R-03).
- **I1, derive**: `(*Error).With(opts...)` copies the problem. It deep-copies the extension map
  and values, keeps the cause and any recorded failure, and applies the options to the copy
  only. A nil receiver gives nil (R-04).
- **I2, validation errors**:
  - `Item{Detail, Pointer}` is one validation error.
  - `WithErrors(items...)` writes the `errors` member through the same extension slot as
    `WithExtension`, so the last option applied wins.
  - `(*Error).Errors()` reads the member back with exact rules: exact names, string values, and
    all or nothing (R-05, R-06).
  - `Pointer(tokens...)` builds an RFC 6901 pointer in URI fragment form, with a byte-wise
    encoder defined by RFC 3986. It is fuzzed for ASCII output, URI validity and a round trip
    (R-07, R-08).
- **I3, I4, documentation**:
  - The README, `doc.go` and every example stop discarding `Write`'s error.
  - The README explains `ErrInvalid` against the other errors that `Write` can return.
  - The middleware snippet keeps `rej.Err` as the cause, in 5 lines.
  - A new README section, "Translating domain errors", also exists as a whole-file example
    (R-10).

All of it uses the standard library only, and adds no `//nolint` directive. The spike passed
`golangci-lint` with 0 issues, ran under `-race` with 99.6% coverage, and fuzzed the pointer
builder for 45 seconds without failure.

## Technical Context

**Language/Version**: Go. The `go` directive stays `go 1.26.0`. CI also tests on 1.27. The new
code uses nothing newer than Go 1.23 (`slices`, `maps`). The examples use `errors.AsType`, which
Go 1.26 added (it is in `api/go1.26.txt`), as the request's own example does.

**Primary Dependencies**: the standard library only. The library code uses `encoding/json`,
`slices` and `strings`, which it already imports. The tests use `net/http/httptest`, `net/url`
and `log/slog`. There is no third-party module and no other module of this repository
([R-14](research.md#r-14-dependencies)).

**Storage**: none.

**Testing**: the standard `testing` package, table-driven tests with named subtests.

- `httptest.NewServer` on loopback for the header regression tests, which a recorder cannot
  reproduce. `httptest.NewRecorder` for everything else.
- `-race`, including a 100-goroutine derivation test.
- `FuzzPointer` (new) and `FuzzParse` (extended to `Errors()`).
- Runnable examples, three of them whole-file examples.

Details: [research.md R-08, R-09, R-13](research.md#r-13-testing-approach-principle-iii).

**Target Platform**: any platform Go supports. Pure Go, with no build constraints.

**Project Type**: library. One existing Go module with one package (`problem/`).

**Performance Goals**: none new. `BenchmarkWrite` is unchanged by the header removal (38
allocations, about 4.7–5.0 µs per operation, R-02). `Pointer` and `With` are not declared
performance-sensitive, so they have no benchmarks.

**Constraints**:

- **No global mutable state**: the header list is a function, not a variable
  (`gochecknoglobals`). `errorsMember` is a `const`.
- **No new panic site**, no I/O and no logging in library code.
- **Immutability**: `*Error` stays immutable. `With` only reads the receiver.
- **Compatibility**: the API changes are additions only (R-01).

**Scale/Scope**: about 120 lines of library code:

- 2 new files, `validation.go` and `pointer.go`;
- small edits to `problem.go`, `extension.go`, `write.go` and `doc.go`;
- 5 new exported identifiers ([contracts/public-api.md](contracts/public-api.md)).

The tests and examples are about 500 lines.

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

| Rule | Assessment | Status |
|------|------------|--------|
| **I. Independent modules** | Only `problem/` changes. There is no dependency on `middleware`: the header list is copied, and a comment names its origin (R-02). There is no `replace`. README and CHANGELOG are updated. The module builds and tests with `GOWORK=off`. | ✅ |
| **II. SemVer & stable API** | All 5 new identifiers are additions: `With`, `Item`, `WithErrors`, `Errors` and `Pointer`, each mapped to the FR groups of [public-api.md](contracts/public-api.md). The behavior change of `Write` is a fix, recorded under "Fixed" with an explicit behavior-change note. The release is `problem/v0.2.0` (MINOR at 0.x), confirmed by `gorelease` in CI. The CHANGELOG is drafted in R-01. Nothing is removed, so no deprecation is needed. | ✅ |
| **III. Test-first** | Each fix has a regression test that fails without it. The spike observed them failing first (R-13), and `tasks.md` will order tests before code. The header tests run through a loopback `httptest.NewServer`, which is hermetic and deterministic: nothing leaves the machine, and the result is passed through a channel (R-09). There is a race test of concurrent `With`. `FuzzPointer` is added, and `FuzzParse` covers `Errors()`, which decodes external input (R-08). Each new export has a runnable example. Coverage target ≥ 80%; the spike measured 99.6%. | ✅ |
| **IV. Idiomatic Go** | The spike reported 0 lint issues with the shared config, and adds no `//nolint` (R-11). Every new identifier has a doc comment. No panic: `Lookup(nil)` stops panicking. No `init`, no globals, no goroutines, no logging in library code. The documentation stops discarding errors, which this principle requires. `With`, `Pointer` and `Errors` do no I/O, so no `context.Context` is needed. `Write` keeps the context deviation tracked in 004 (see Complexity Tracking). | ✅ |
| **V. Simplicity & dependencies** | No dependency. Every addition maps to a requirement. These were rejected as unnecessary: a configurable header list, a pointer parser, pointer validation, a mapper API, and integer tokens (R-02, R-05, R-07). `WithErrors` reuses `WithExtension` instead of a second storage path. | ✅ |
| Go toolchain support | `go 1.26.0` is unchanged, and every API used exists in 1.26. | ✅ |
| Platforms | Pure Go. The behavior of `net/http` and `encoding/json` is identical on the six targets. | ✅ |
| Repository layout | No tooling change and no lint override. | ✅ |
| Dependency hygiene | `go.mod` keeps no `require`, so `go mod tidy -diff` stays clean. | ✅ |
| Vulnerability scanning / Secrets | No dependency to scan. The translation example uses the fake, low-entropy text `pq: duplicate key value violates unique constraint "users_email_key"`, which Gitleaks does not flag. Hosts are `example.com` and `example.net`. | ✅ |
| Local and PR gates | `make check MODULE=problem`. | ✅ |
| Commits / Release | The PR title is `fix(problem): remove representation headers and add derivation and validation errors`, or the maintainer's equivalent with the `problem` scope. The PR description lists the follow-ups that the spec puts out of scope. The release is `problem/v0.2.0`. | ✅ |

**Result (pre-research)**: PASS. No new deviation.

**Result (post-design re-check)**: PASS.

- The data model and the contracts add no state, no dependency and no exception.
- Every new exported identifier maps to a functional requirement.
- The research refined the spec in three places, and the spec was updated
  ([R-12](research.md#r-12-refinements-to-the-spec)):
  - FR-019 and SC-007 cover `doc.go`;
  - FR-003 requires the package documentation to state the header rule;
  - an edge case fixes the exact-name and `null` rules of `Errors`.

## Project Structure

### Documentation (this feature)

```text
specs/005-problem-fixes/
├── spec.md               # feature specification (refined by research R-12)
├── plan.md               # this file
├── research.md           # Phase 0: decisions R-01 … R-14, with spike evidence
├── data-model.md         # Phase 1: With copy semantics, Item, Errors rules, pointers, headers
├── quickstart.md         # Phase 1: validation guide
├── contracts/
│   ├── public-api.md     # API additions and changed doc comments (v0.1.0 → v0.2.0)
│   └── document.md       # wire format: removed headers, the errors member, pointer encoding
├── checklists/
│   └── requirements.md   # spec quality checklist
└── tasks.md              # Phase 2 (/speckit-tasks, not created here)
```

### Source Code (repository root)

```text
problem/
├── CHANGELOG.md                   # [Unreleased]: Added (With, Item/WithErrors/Errors, Pointer),
│                                  # Fixed (headers, with the behavior-change note; Lookup(nil))
├── README.md                      # Write errors handled and explained; new sections "Adding
│                                  # members when writing", "Validation errors", "Translating
│                                  # domain errors"; middleware snippet with WithCause (R-10)
├── doc.go                         # header rule; With; validation items; no discarded errors
├── problem.go                     # + (*Error).With
├── extension.go                   # Key.Lookup nil check; NewKey doc: "errors" owned by WithErrors
├── write.go                       # representationHeaders(); removal before Content-Type; doc
├── validation.go                  # NEW: Item, errorsMember, WithErrors, (*Error).Errors
├── pointer.go                     # NEW: Pointer, fragment byte classes
│
├── write_test.go                  # TestWrite_RealServer, TestWrite_RepresentationHeaders,
│                                  # TestWrite_HeadersKept (adapted)
├── extension_test.go              # TestLookup_NilProblem
├── with_test.go                   # NEW: TestWith_*
├── validation_test.go             # NEW: TestWithErrors_*, TestErrors_*, TestPointer_*
├── fuzz_test.go                   # + FuzzPointer; FuzzParse calls Errors() and round-trips items
├── example_test.go                # + ExamplePointer, ExampleError_Errors; no discarded errors
├── example_with_test.go           # NEW whole-file example: ExampleError_With (request_id)
├── example_validation_test.go     # NEW whole-file example: ExampleWithErrors (422, two items)
└── example_translate_test.go      # NEW whole-file example: Example_translateDomainErrors
```

**Structure Decision**: the existing single-package module `problem/` is extended in place,
following its one-file-per-concern layout:

- validation items go in `validation.go`, and JSON Pointers in `pointer.go`;
- `With` goes in `problem.go`, next to `apply` and `fail`, which it relies on;
- each new source file has a matching test file;
- each example that needs package-level declarations gets its own file, so that the
  documentation renders it as a whole-file example.

Nothing outside `problem/` changes.

## Complexity Tracking

No new violation. The one deviation of the module, `Write(w, r, err)` and `Read(resp)` doing
I/O without a `context.Context` parameter, is recorded and justified in
[004 plan.md](../004-problem-details/plan.md#complexity-tracking). This feature does not change
it, and adds no other function that performs I/O.

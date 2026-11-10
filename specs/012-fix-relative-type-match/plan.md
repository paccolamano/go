# Implementation Plan: Relative Problem Types Match Problems Read From a Response (problem)

**Branch**: `012-fix-relative-type-match` (suggested; work currently on `main`) | **Date**: 2026-11-10 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `specs/012-fix-relative-type-match/spec.md`

**Note**: This template is filled in by the `/speckit-plan` command; its definition describes the execution workflow.

## Summary

This feature changes the existing module `github.com/paccolamano/go/problem`, released as
`problem/v0.2.0`, and ships as `problem/v0.2.1`. It fixes one defect: `Type.Match` never matches
a problem read with `Read` when the type is declared with a relative URI, because `Read` resolves
the problem's type against the request URL and `Match` compares the declaration as written.

- **The URL**: `Error` gains an unexported field, `base *url.URL`. `Read` sets it to a copy of
  the request URL that it resolves the members against. Every other source leaves it nil, and
  `With` carries it over by copying the struct (R-02).
- **The comparison**: `match` calls a new unexported method, `(*Error).hasType`. When the problem
  has a base and the declared URI begins with "/", the declaration is resolved against the base
  with `resolve`, the function `Read` uses, then compared exactly. Otherwise, the URIs are compared
  as written, as in 0.2.0 (R-03).
- **Unchanged**: the exported API, the documents `Write` produces, `Error.Type`, `NewType`, the
  walk of the error tree, and the cost of every path but the new one (R-04, R-05).
- **Documentation**:
  - the `Type.Match` and `Read` doc comments, the package documentation and the README state the
    new rule;
  - the advice to declare another API's absolute URI is removed;
  - a runnable example shows a shared declaration;
  - the changelog records a behavior change under "Fixed" (R-07).
- **Tests**:
  - the table of FR-007, in memory;
  - a loopback server matrix of 12 declarations by 6 request URLs, a redirect among them;
  - error trees and derived problems;
  - an allocation test for SC-003, a write-again test for FR-009;
  - a fuzz target for FR-015 (R-06).

The design was implemented in a scratch copy of the module, outside the working tree. With it,
`golangci-lint` reported 0 issues and the existing suite passed under `-race` at 99.3% coverage.
The FR-007 table and the loopback case failed before the change and passed after it. The 72
combinations of the matrix matched, 3.2 million fuzz executions found no counterexample, and
`gorelease` reported no API change, suggesting `v0.2.1`.

## Technical Context

**Language/Version**: Go. The `go` directive stays `go 1.26.0`. CI also tests on 1.27. Nothing
newer than the module already uses is needed.

**Primary Dependencies**: unchanged. `net/url` and `strings` are already imported by the module.
`go.mod` and `go.sum` do not change ([R-08](research.md#r-08-dependencies)).

**Storage**: N/A.

**Testing**: the standard `testing` package, with table-driven tests and named subtests.

- `httptest.NewServer` on the loopback interface, as `write_test.go` already uses it, for
  responses that `http.Client` reads, with a redirect between two servers.
- In-memory responses through the existing `response()` helper, whose request URL is
  `https://api.example.com/v1/messages`.
- `testing.AllocsPerRun` for SC-003, a `Fuzz*` target for FR-015, and a runnable `Example`.

Details: [research.md R-06](research.md#r-06-testing-approach-fr-014-fr-015-sc-001-to-sc-005-principle-iii).

**Target Platform**: any platform Go supports. Pure Go, with no build constraints.

**Project Type**: library. One existing Go module, `problem/`, changed in its only package.

**Performance Goals**: unchanged paths allocate nothing (SC-003): problems built locally, read
with `Parse` or without a request, absolute declarations and trees. The new path costs 4
allocations and about 560 ns per problem compared, on error paths only (R-05).

**Constraints**:

- **No new exported API** (FR-010), and `gorelease` reports no change (SC-005).
- **Immutability**: `Error` stays immutable and safe for concurrent use. `base` is a private copy
  that nothing modifies.
- **No new state** beyond the field, and no goroutine, global or logging.

**Scale/Scope**:

- Library code, about 20 lines:
  - `problem.go`: the field and its comment;
  - `read.go`: two lines changed, one sentence of documentation;
  - `type.go`: `hasType`, one changed line in `match`, the `Match` doc comment;
  - `doc.go`: one paragraph.
- Tests: about 250 lines.
- Documentation: the README, the CHANGELOG and one example.

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

| Rule | Assessment | Status |
|------|------------|--------|
| **I. Independent modules** | Only `problem/` changes. No new dependency and no `replace`. The README and the CHANGELOG are updated. The module builds and tests with `GOWORK=off`. | ✅ |
| **II. SemVer & stable API** | No exported identifier is added, removed or changed. `gorelease` against `problem/v0.2.0` reports no change and suggests `v0.2.1` (spike, R-01), so the release is a PATCH. The CHANGELOG marks the new matches as a **Behavior change** under "Fixed". At 0.x, nothing needs deprecating. | ✅ |
| **III. Test-first** | `TestMatch_ReadResponse` and `TestMatch_ReadFromServer` reproduce the defect, and fail without the fix (observed in the spike, FR-014). `tasks.md` will order every test before its code. The loopback servers run in the test process, with no real network and no sleep. `Type.Match` and `Read` are exercised by the new tests, and `ExampleType_Match_shared` covers the shared declaration (FR-012). `FuzzRead` covers the new resolution of external input (FR-015). `Match` is not declared performance-sensitive, so no benchmark is required. An asserting allocation test guards SC-003. Coverage: 99.6% at 0.2.0, above 99% after (R-06). | ✅ |
| **IV. Idiomatic Go** | 0 lint issues in the spike, and no new `//nolint`. The existing one on `match` keeps its reason. `hasType` and the field have doc comments, and the `Match`, `Read` and package docs state the new rule. No new error, panic, `init`, global or goroutine. No I/O is added, so no context parameter is needed. | ✅ |
| **V. Simplicity & dependencies** | No dependency. One field and one method, the smallest change the fix needs. These were rejected as heavier, or as giving the wrong result (R-02, R-03, R-05): a new exported API, keeping the caller's URL, keeping the document's type as written, resolving against the problem's own origin, a parsed URI cached in `Type`, and a per-problem cache. | ✅ |
| Go toolchain support | `go 1.26.0` is unchanged, and nothing newer is used. | ✅ |
| Platforms | Pure Go, with no OS-specific code. | ✅ |
| Repository layout | No tooling change, no lint override and no `.module-exceptions` entry. | ✅ |
| Dependency hygiene | `go.mod` and `go.sum` are unchanged, so `go mod tidy -diff` stays clean. | ✅ |
| Vulnerability scanning / Secrets | No dependency to scan. Test values are example hosts and loopback servers, with no credentials. | ✅ |
| Local and PR gates | `make check MODULE=problem`. `scripts/changelog-check.sh` passes, because the CHANGELOG changes with the sources. | ✅ |
| Commits / Release | The PR title is `fix(problem): match relative types on problems read from a response`, with the `problem` scope and no `!`. The release follows in `chore(problem): prepare release 0.2.1`, which tags `problem/v0.2.1` (R-01). | ✅ |

**Result (pre-research)**: PASS. No deviation.

**Result (post-design re-check)**: PASS.

- The data model adds one internal field, never written or exported (FR-009, FR-010). The
  contract changes one documented behavior, mapped to FR-001 to FR-006.
- The research confirmed the spec's assumption of a PATCH release with `gorelease`.
- The spec needs no change: no decision of the research contradicts it. R-03 records one
  consequence that the spec does not state: a relative declaration with dot segments, such as
  `/probs/./x`, matches a read problem whose type resolves to the same URI, but not one built
  locally. This follows from FR-001, which requires RFC 3986 §5 resolution.

## Project Structure

### Documentation (this feature)

```text
specs/012-fix-relative-type-match/
├── spec.md               # feature specification
├── plan.md               # this file
├── research.md           # Phase 0: decisions R-01 … R-08, with reproduction and spike evidence
├── data-model.md         # Phase 1: the base field, its sources, the comparison and its results
├── quickstart.md         # Phase 1: validation guide
├── contracts/
│   └── public-api.md     # behavior and doc comment changes (v0.2.0 → v0.2.1); API unchanged
├── checklists/
│   └── requirements.md   # spec quality checklist
└── tasks.md              # Phase 2 (/speckit-tasks, not created here)
```

### Source Code (repository root)

```text
problem/
├── CHANGELOG.md            # [Unreleased]: Fixed, Behavior change (Type.Match and relative declarations)
├── README.md               # "Reading problem details": a shared declaration may be relative
├── doc.go                  # "Reading problem details": the rule of FR-001 to FR-003
├── problem.go              # Error gains base *url.URL (unexported)
├── read.go                 # Read keeps a copy of the request URL in base; doc sentence
├── type.go                 # match calls (*Error).hasType; Match doc comment rewritten
├── type_test.go            # + TestMatch_ReadResponse, TestMatch_ReadFromServer, TestMatch_ReadTree, TestMatch_Allocs
├── with_test.go            # TestWith_ReadProblem: the derived problem matches as the original does
├── read_test.go            # + TestRead_WriteAgain
├── fuzz_test.go            # + FuzzRead
└── example_test.go         # + ExampleType_Match_shared
```

**Structure Decision**: the existing module `problem/` is changed in place, in the files that own
each concern:

- the problem's fields stay in `problem.go`;
- reading stays in `read.go`;
- the type test stays in `type.go`, and its tests in `type_test.go`;
- the fuzz target joins `FuzzParse` in `fuzz_test.go`, and the example joins `ExampleType_Match`
  in `example_test.go`.

No new file is needed. Nothing outside `problem/` and this spec directory changes.

## Complexity Tracking

No violation. The module has no recorded deviation, and this feature adds none.

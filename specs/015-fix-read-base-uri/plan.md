# Implementation Plan: Read Resolves Against the URL the Request Sent (problem)

**Branch**: `015-fix-read-base-uri` (suggested; work currently on `main`) | **Date**: 2026-11-11 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `specs/015-fix-read-base-uri/spec.md`

**Note**: This template is filled in by the `/speckit-plan` command; its definition describes the execution workflow.

## Summary

This feature changes the existing module `github.com/paccolamano/go/problem`, released as
`problem/v0.2.1`, and ships as `problem/v0.2.2`. It fixes one defect: `Read` copies the userinfo
of the request URL into every relative `type` and `instance` it resolves, and the fragment into a
member that is just `#`. It also keeps both in the URL it stores for the type test.

- **The fix**: `Read` already resolves against a copy of the request URL. It now clears the copy's
  `User`, `Fragment` and `RawFragment` before it resolves and stores it. This is three lines in
  `read.go` (R-02). Nothing else in the code changes:
  - the type test resolves against the same stored copy, so both sides of its comparison stay on
    one base;
  - `With` carries the copy over;
  - every other field of the URL, the query included, is kept.
- **Unchanged**:
  - the exported API, `Parse` and the comparison rule of spec 012;
  - every result for a request URL without userinfo and without a fragment (R-03);
  - the cost: no allocation is added (R-05).
- **Documentation**:
  - the `Read` doc comment, the package documentation and the README state the URL the request
    sent;
  - `Type.Match` and `ExampleRead` drop "request" from "the request URL";
  - the changelog records the fix in a new "Security" section, marked as a behavior change
    (R-01, R-09).
- **Tests**:
  - the FR-006 table in memory;
  - a loopback server read through `http.Client`, with a user name and a password, a user name
    only, and a relative redirect;
  - the table of spec 012 with credentials added;
  - derived problems and error trees;
  - `FuzzRead`, with new seeds and checks (R-06).

The design was implemented in a scratch copy of the module, outside the working tree:

- **Before the change**:
  - the five relative rows of FR-006 and all three loopback cases failed;
  - the reproduction also showed something the spec does not state: with credentials in the
    request URL, `Type.Match` misses rows 2 and 9 of the table of spec 012.
- **After the change**:
  - every probe passed;
  - the suite passed under `-race` at 99.6% coverage;
  - `golangci-lint` reported 0 issues;
  - `go doc -all` showed the same exported API;
  - 60 seconds of fuzzing ran 4.86 million executions without a failure.

## Technical Context

**Language/Version**: Go. The `go` directive stays `go 1.26.0`. CI also tests on 1.27. Nothing
newer than the module already uses is needed.

**Primary Dependencies**: unchanged. The fix assigns fields of a `url.URL` value that `Read`
already holds, so no import changes. `go.mod` and `go.sum` do not change
([R-10](research.md#r-10-dependencies)).

**Storage**: N/A.

**Testing**: the standard `testing` package, with table-driven tests and named subtests.

- `httptest.NewServer` on the loopback interface, read by its `http.Client`, with a relative
  redirect, as `TestMatch_ReadFromServer` already does.
- In-memory responses whose request is built with `http.NewRequestWithContext`, as a client
  builds it. `httptest.NewRequest`, used by the existing helpers, moves a fragment into the query
  (spike).
- Request URLs built as `url.URL` values with `url.UserPassword` and `url.User`, so no URL literal
  with credentials appears in the sources (R-08).
- `FuzzRead`, extended with seeds and checks.

Details: [research.md R-06](research.md#r-06-testing-approach-fr-014-fr-015-sc-001-to-sc-005-principle-iii).

**Target Platform**: any platform Go supports. Pure Go, with no build constraints.

**Project Type**: library. One existing Go module, `problem/`, changed in its only package.

**Performance Goals**: no regression. `Read` made the copy of the request URL in 0.2.1 already,
and clearing three of its fields allocates nothing. `TestMatch_Allocs` still passes (R-05).

**Constraints**:

- **No exported API change** (FR-011), and `gorelease` reports no change (SC-005).
- **The caller's request is not modified** (FR-005): the fields are cleared on `Read`'s copy, and
  the caller's `*url.Userinfo` is left as it is.
- **Immutability**: `Error` stays immutable and safe for concurrent use. `base` is still a
  private copy that nothing modifies.
- **No secret-like literals**: the placeholder credentials must pass Gitleaks, Semgrep and Bearer
  in the pull request (R-08).

**Scale/Scope**:

- Library code: 3 lines in `read.go`, and doc comments in `read.go`, `problem.go` and `type.go`.
- Documentation: `doc.go`, the README, `ExampleRead`'s comment and the CHANGELOG.
- Tests: about 250 lines.

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

| Rule | Assessment | Status |
|------|------------|--------|
| **I. Independent modules** | Only `problem/` changes. No new dependency and no `replace`. The README and the CHANGELOG are updated. The module builds and tests with `GOWORK=off`. | ✅ |
| **II. SemVer & stable API** | No exported identifier is added, removed or changed: `go doc -all` is identical to `problem/v0.2.1` (spike). `gorelease` is expected to suggest `v0.2.2`, so the release is a PATCH. The CHANGELOG records the fix under "Security", marked **Behavior change**, in the same change. At 0.x, nothing needs deprecating. | ✅ |
| **III. Test-first** | The new and extended tests reproduce the defect, and fail without the fix (observed in the spike for each of them; FR-014). `tasks.md` will order every test before the change to `read.go`. The loopback servers run in the test process, with no real network and no sleep. `Read` is exercised by the new tests, and `ExampleRead` remains its example. `FuzzRead` covers the changed handling of external input (FR-015). Nothing new is declared performance-sensitive, so no benchmark is required. Coverage: 99.6% before and after (spike). | ✅ |
| **IV. Idiomatic Go** | 0 lint issues in the spike, and no `//nolint`. The `Read` and package docs state the new rule. No new error, panic, `init`, global or goroutine. No I/O is added, so no context parameter is needed. | ✅ |
| **V. Simplicity & dependencies** | No dependency, no new field, no new function. Clearing two components of the copy that `Read` already makes is the smallest change the fix needs. These were rejected as heavier, or as giving the wrong result (R-02): cleaning the resolved strings, cleaning inside `resolve`, rebuilding the URL from a list of fields, `url.URL.Redacted`, a round trip through a string, and a new helper. | ✅ |
| Go toolchain support | `go 1.26.0` is unchanged, and nothing newer is used. | ✅ |
| Platforms | Pure Go, with no OS-specific code. | ✅ |
| Repository layout | No tooling change, no lint override and no `.module-exceptions` entry. | ✅ |
| Dependency hygiene | `go.mod` and `go.sum` are unchanged, so `go mod tidy -diff` stays clean. | ✅ |
| Vulnerability scanning | No dependency to scan. | ✅ |
| Secrets | The tests and the spec use obvious placeholders (`svc`, `s3cret`, `api_key=XYZ`). A local Gitleaks scan found no leak in the spec or in a test with URL literals. The Go sources build the userinfo with `url.UserPassword`, so they contain no such literal (R-08). | ✅ |
| Local and PR gates | `make check MODULE=problem`. `scripts/changelog-check.sh` passes, because the CHANGELOG changes with the sources. | ✅ |
| Commits / Release | The PR title is `fix(problem): resolve read problems against the URL the request sent`, with the `problem` scope and no `!`. The release follows in `chore(problem): prepare release 0.2.2`, which tags `problem/v0.2.2` (R-01). | ✅ |

**Result (pre-research)**: PASS. No deviation.

**Result (post-design re-check)**: PASS.

- The data model changes the value of one internal field, never written or exported (FR-003,
  FR-011). The contract changes the documented base of `Read`, mapped to FR-001 to FR-006.
- The research confirmed the spec's assumption of a PATCH release, with `go doc -all` standing in
  for `gorelease` locally.
- **The spec needs one wording fix**, which this plan does not apply:
  - The opening paragraph of User Story 2 says that, for request URLs with credentials, "the type
    test gives the same results" as 0.2.1. The reproduction shows otherwise. On 0.2.1, with
    credentials in the request URL, rows 2 and 9 of the table of spec 012 report no match. FR-008
    requires a match, and the fix gives one.
  - FR-008, the acceptance scenarios and the success criteria are right, and only that narrative
    sentence is not. Proposed wording: "…hosts, ports, paths and queries are those of 0.2.1, and
    the type test gives the results it gives for the same URL without credentials."
  - The changelog draft of R-01 states the change to `Type.Match` in addition to what FR-013
    lists.
- The research records consequences that the spec does not state, all following from FR-001
  (R-03):
  - an empty userinfo, as in `http://@host`, is removed;
  - the opaque part of a non-hierarchical URL is kept, and only its fragment is removed;
  - the member `//`, which Go resolves like `#`, also loses the request's fragment.
- **Fuzzing note**: `FuzzRead` stalls after about 9 seconds with the default minimization time,
  and did so on `problem/v0.2.1` too. The quickstart adds `-fuzzminimizetime 1s` to the 60-second
  run (R-07).

## Project Structure

### Documentation (this feature)

```text
specs/015-fix-read-base-uri/
├── spec.md               # feature specification
├── plan.md               # this file
├── research.md           # Phase 0: decisions R-01 … R-10, with reproduction and spike evidence
├── data-model.md         # Phase 1: the request URL, the URL the request sent, the results
├── quickstart.md         # Phase 1: validation guide
├── contracts/
│   └── public-api.md     # behavior and doc comment changes (v0.2.1 → v0.2.2); API unchanged
├── checklists/
│   └── requirements.md   # spec quality checklist
└── tasks.md              # Phase 2 (/speckit-tasks, not created here)
```

### Source Code (repository root)

```text
problem/
├── CHANGELOG.md            # [Unreleased]: Security, Behavior change (R-01)
├── README.md               # "Reading problem details": the URL the request sent
├── doc.go                  # "Reading problem details": the URL the request sent
├── read.go                 # Read clears User, Fragment and RawFragment of its copy; doc comment
├── problem.go              # comment of base: no userinfo, no fragment
├── type.go                 # Match doc comment: "the URL", not "the request URL"
├── example_test.go         # ExampleRead: one comment
├── helpers_test.go         # a helper that reads a document from a client-built request URL
├── read_test.go            # + TestRead_RequestURL, TestRead_Credentials
├── type_test.go            # TestMatch_ReadResponse with and without credentials; TestMatch_ReadTree with credentials
├── with_test.go            # TestWith_ReadProblem with credentials
└── fuzz_test.go            # FuzzRead: seeds with userinfo, query and fragment; checks of FR-015
```

**Structure Decision**: the existing module `problem/` is changed in place, in the files that own
each concern:

- reading stays in `read.go`, and its tests in `read_test.go`;
- the type test's tests stay in `type_test.go`, derived problems in `with_test.go`, and the fuzz
  target in `fuzz_test.go`;
- the shared test helper joins `readProblem` in `helpers_test.go`.

No new file is needed. Nothing outside `problem/` and this spec directory changes.

## Complexity Tracking

No violation. The module has no recorded deviation, and this feature adds none.

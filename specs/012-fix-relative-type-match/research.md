# Research: Relative Problem Types Match Problems Read From a Response (problem)

**Feature**: [spec.md](spec.md) | **Plan**: [plan.md](plan.md) | **Date**: 2026-11-10

The Technical Context had no open questions. The language, toolchain, module and dependencies are
those of the existing `problem` module (specs 004 and 005), and the request settles the approach:
resolve a relative declaration against the URL the problem was read from. This research records
the design decisions, and the evidence behind them:

- **Reproduction**: a probe test in a scratch copy of `problem/v0.2.0` applied the table of FR-007.
  Rows 1 and 2 failed ("Match = false, want true (type
  "https://api.example.com/probs/out-of-credit")"), and the other eight rows passed. A loopback
  server writing `/probs/out-of-credit`, read by `http.Client` from `/api/v1/users/me`, gave the
  type `http://127.0.0.1:<port>/probs/out-of-credit` and no match.
- **Spike**: this design was implemented on 2026-11-10 in a scratch copy of the module, outside
  the working tree. With it:
  - the ten rows of FR-007 and the loopback case passed;
  - 12 relative declarations, each written by a loopback server and read from 6 request URLs
    (three paths on one server, a second server on another port, a redirect between them, and a
    URL with parameters, a query and a fragment), matched in 72 of 72 combinations. No
    declaration matched the problem of another;
  - `testing.AllocsPerRun` measured 0 allocations under `-race` for problems built locally, read
    with `Parse`, absolute declarations and error trees;
  - a fuzz target for the property of FR-015 ran 3.2 million executions in 45 seconds without a
    failure;
  - the existing suite passed under `-race -shuffle=on`, at 99.3% statement coverage. The only
    statement it does not reach is the new resolving branch, which the new tests cover;
  - `golangci-lint` with the repository configuration reported 0 issues;
  - `gorelease -base=v0.2.0`, at the version pinned in `versions.env`, reported no API change and
    suggested `v0.2.1`.
- **Not in the spike**: the documentation, the runnable example, the write-again test of FR-009,
  and the final form of the tests listed in [R-06](#r-06-testing-approach-fr-014-fr-015-sc-001-to-sc-005-principle-iii).
  The spike measured allocations with logs, not with an asserting test.

## R-01 Release level and API compatibility

- **Decision**: release `problem/v0.2.1`, a PATCH.
  - The exported API does not change (FR-010). `gorelease` against `problem/v0.2.0` reports no
    change, and suggests `v0.2.1` (spike).
  - `Type.Match` now reports matches it did not report. The changelog marks this as a behavior
    change, as 0.2.0 did for the headers that `Write` removes.
  - Code that relied on the old result would need a relative declaration never to match a
    response written from that same declaration. The spec judges this unlikely (Assumptions).
- **Changelog draft** (`problem/CHANGELOG.md`, section `[Unreleased]`):

  ```markdown
  ### Fixed

  - `Type.Match` matches a problem read with `Read` when the type is declared with a relative URI,
    such as a declaration that a service shares with its client package. `Match` resolves the
    relative URI against the request URL that `Read` resolved the problem's type against, then
    compares the two. **Behavior change**: `Match` used to compare the relative declaration with
    the resolved type, and reported no match, even for a response written from the same
    declaration. It now reports a match when both name the same path on the server that answered.
    Absolute declarations, and problems built locally or read with `Parse`, are compared as
    written, as before. `Error.Type` still returns the resolved URI.
  ```

- **Pull request**: title `fix(problem): match relative types on problems read from a response`.
  No `!`: nothing breaks. The release follows in its own pull request,
  `chore(problem): prepare release 0.2.1`, which moves the entry to `## [0.2.1] - <date>`, as
  #20 and #22 did for `auth`.
- **Rationale**: Principle II. The released version matches the level that `gorelease` reports,
  and the changelog records the user-visible change in the same change.
- **Alternatives rejected**: a MINOR release (0.3.0). Nothing is added to the API, and the
  constitution ties the version to the `gorelease` report.

## R-02 Where the problem keeps the URL it was read from (FR-001, FR-006, FR-009)

- **Decision**: `Error` gains one unexported field, `base *url.URL`. `Read` sets it to a copy of
  `*resp.Request.URL`, and resolves `typ` and `instance` against that copy, as it does today
  against the original. Every other way of building a problem leaves it nil.

  ```go
  // in Read, replacing the two resolve calls
  base := *resp.Request.URL
  e.base = &base
  e.typ = resolve(e.base, e.typ)
  e.instance = resolve(e.base, e.instance)
  ```

  - **Copy, not the caller's pointer**: `resp.Request.URL` belongs to the caller, who may change
    it after `Read`, for example to send the request again. `Error` is documented as immutable and
    safe for concurrent use. A shallow copy is enough: the only pointer in `url.URL` is `User`, of
    type `*url.Userinfo`, which has no method that modifies it.
  - **Derived problems**: `(*Error).With` copies the struct, so the copy keeps `base` (FR-006)
    with no new code. No option changes the type.
  - **Never written**: `Write` builds the document from the named members, and none of the
    accessors reads `base`. The document, `Error()` and every accessor are unchanged (FR-009).
- **Rationale**: the URL that resolved the problem's type is exactly the base FR-001 requires.
  Keeping it next to the members is the smallest change that gives `Match` that base.
- **Alternatives rejected**:
  - **Keep `resp.Request.URL` itself**: shares mutable state with the caller, so a later change
    to the request would change the result of `Match` and race with it.
  - **Keep the base as a string**: `Match` would parse it on every resolving comparison. That
    costs one more allocation, and adds a second parse that must agree with the first.
  - **Keep the document's type as written, and compare the declaration with it as well**: this
    fails row 2 of FR-007, where the document carries the absolute form of the declared path. It
    also disagrees with resolution, which removes dot segments, so `/probs/./x` written and
    `/probs/x` declared would not match. Not what FR-001 specifies.
  - **Resolve the declaration against the origin of the problem's own type**: then
    `/probs/out-of-credit` would match `https://other.example.com/probs/out-of-credit`, which
    row 3 of FR-007 forbids.
  - **A new exported function or option, such as a `Match` variant taking a base URL**: FR-010
    forbids it, and every client would need to change its code to benefit from the fix.

## R-03 The comparison rule (FR-001 to FR-004, FR-015)

- **Decision**: `match` compares through a new unexported method of `*Error`:

  ```go
  func (e *Error) hasType(uri string) bool {
      if e.base != nil && strings.HasPrefix(uri, "/") {
          return e.typ == resolve(e.base, uri)
      }
      return e.typ == uri
  }
  ```

  - **Relative test**: `NewType` accepts only absolute URIs and references that begin with "/"
    (`isTypeURI`). For a declared URI, "relative" and "begins with `/`" are therefore the same
    test. A network-path reference such as `//cdn.example.com/x` begins with "/", and resolves
    with the base's scheme (spec edge case).
  - **Same function as `Read`**: the declaration goes through `resolve`, the function that
    resolved the document's type, with the same base. When the document's type equals the
    declaration, both sides are the same computation, so they are equal. This holds whatever the
    base, and whatever the URI contains: query, fragment, percent-encodings or dot segments. The
    property of FR-015 follows from it.
  - **Exact comparison**: the result is compared with `==`, with no other normalization of case,
    ports or percent-encodings (FR-004). Resolution removes dot segments (RFC 3986 §5.2.4) on both
    sides, as FR-001 requires. A consequence: a relative declaration `/probs/./x` matches a read
    problem written as `/probs/x`, but not a problem built locally from `/probs/x`, which is
    compared as written. Declaring a type with dot segments has no practical use.
  - **`about:blank`**: resolving a reference that begins with "/" always gives a URI with a path
    that begins with "/", so it never equals `about:blank`. A document without a type still
    matches no declaration (spec edge case).
- **Rationale**: one rule, written once. The guard order keeps every case of spec 004 on the old
  code path (FR-002, FR-003).
- **Alternatives rejected**:
  - **Resolve every declaration, absolute ones included**: `resolve` returns an absolute URI
    unchanged, but parses it first, which allocates (SC-003).
  - **Parse the declared URI once, in `NewType`, and keep the result in `Type`**: adds a pointer
    to `Type`. Two declarations of the same URI would then compare unequal with `==`, a visible
    change to a comparable exported type. It saves one parse out of four allocations.

## R-04 Error trees and derived problems (FR-005, FR-006)

- **Decision**: the walk of `match` does not change. Each `*Error` it visits is compared with its
  own `base`, so problems read from several services in one tree are each resolved against their
  own URL. The first match in the order of the walk wins, as before.
- **Evidence (spike)**: two problems of type `/probs/out-of-credit`, read from
  `https://a.example.com/v1` and `https://b.example.com/v1`, joined after a problem of another
  type: `Match` returned the first one. The second one, set as the cause of a problem of another
  type, was found through the cause. A problem derived with `With` matched in each of the 72
  combinations of the matrix.

## R-05 Cost of the comparison (SC-003)

- **Decision**: no cache, and no benchmark.
  - Unchanged paths allocate nothing: problems built locally, read with `Parse` or without a
    request, absolute declarations and error trees all measured 0 allocations (spike, under
    `-race`).
  - The new path, a relative declaration against a problem read from a response, costs 4
    allocations and about 560 ns per problem compared (spike, `go1.27.1`, linux/amd64): one parse,
    the resolution and the string. `Match` runs on error paths, after an HTTP round trip.
  - An asserting test with `testing.AllocsPerRun` guards SC-003 (R-06). `Match` is not declared
    performance-sensitive, so Principle III requires no benchmark for it.
- **Alternatives rejected**: caching the resolved declaration on the `Error`. It needs
  synchronization, because `Error` is safe for concurrent use, and one entry per declaration
  tested. Not justified by an error-path cost.

## R-06 Testing approach (FR-014, FR-015, SC-001 to SC-005; Principle III)

Every test uses the standard `testing` package, with table-driven tests and named subtests.
Responses are built in memory, or served by `httptest.NewServer` on the loopback interface in the
test process, as `write_test.go` already does (spec Assumptions). No real network, no sleep.

| Test (file) | Covers | Red without the fix |
|-------------|--------|---------------------|
| `TestMatch_ReadResponse` (`type_test.go`) | FR-007: the ten rows, with `response()`, whose request URL is `https://api.example.com/v1/messages`; US1 scenarios 1–3; US2 scenarios 1–3 | rows 1 and 2 (observed) |
| `TestMatch_ReadFromServer` (`type_test.go`) | FR-014, SC-002, US1 scenario 5: at least 10 relative declarations, query, fragment, percent-encoding, dot segments and network path among them, written by a loopback server through `Write`, read by its `http.Client` from 6 request URLs: paths, a second server on another port, a redirect from one to the other. Each matches its own declaration, and no other one. The problem derived with `With` also matches. Redirect edge case: an absolute declaration naming the first server does not match. | every combination (observed for one) |
| `TestMatch_ReadTree` (`type_test.go`) | US1 scenario 4 and FR-005: the problem of US1 wrapped, joined, set as the cause of another problem, and after a problem of another type; two services in one tree, first match wins | every subtest |
| `TestMatch_Allocs` (`type_test.go`) | SC-003: 0 allocations, asserted with `testing.AllocsPerRun`, for a relative declaration against problems built locally, read with `Parse` and read without a request; for an absolute declaration against a problem read from a response; and for a tree | passes before and after: it guards the old behavior |
| `TestWith_ReadProblem` (`with_test.go`, extended) | US2 scenario 4, FR-006: the derived problem matches the relative and the absolute declaration exactly when the original does | the relative case |
| `TestRead_WriteAgain` (`read_test.go`) | US2 scenario 5, FR-009: a problem read from a response, written with `Write`, gives the document of 0.2.0, with the resolved type and instance and nothing else | passes before and after |
| `FuzzRead` (`fuzz_test.go`) | FR-015, SC-004: a document and a request URL; when `Read` accepts the document and its `"type"` as written can be declared, the declaration matches. Seeds: the documents of every row of FR-007 with the request URL of FR-007, plus a relative request URL, `about:blank`, `mailto:a@b` and an empty URL | the seeds of rows 1 and 2 |
| `ExampleType_Match_shared` (`example_test.go`) | FR-012, US3 scenario 2: a service writes a problem of the declaration `/probs/out-of-credit` to a recorder, and the client reads the result with the request URL set. Prints the resolved type and the match. | the match line |

- **Kept unchanged**: `TestNewType_Rules` already covers US2 scenario 6 (FR-008).
  `TestRead_Resolve` covers the resolved type and instance (FR-009). `TestMatch_Tree` covers
  problems built locally. `FuzzParse` keeps its properties.
- **Red first**: in `tasks.md`, the tests of US1 precede the change to `type.go` and `read.go`,
  and are run once to observe the failures in the right-hand column (FR-014).
- **Coverage**: the module was at 99.6% at `problem/v0.2.0`. The new branch is reached by every
  test of US1, so coverage stays above 99% (SC-005).

## R-07 Documentation (FR-011 to FR-013)

The exact wording is set at implementation time, but each text must state the rule of FR-001 to
FR-003. The doc comments are drafted in [contracts/public-api.md](contracts/public-api.md).

| Where | Change |
|-------|--------|
| `type.go`, `Type.Match` | Replace "a problem read with Read has its type URI resolved first, so a type declared to match another API's problems must use that API's absolute URI" with the rule. Add: declare an absolute URI to match one host only. |
| `read.go`, `Read` | After the resolution paragraph: `Type.Match` resolves a relative declared type against the same URL. |
| `doc.go`, "Reading problem details" | After "Read also resolves relative …": the rule, framed for declarations shared by a service and its clients. |
| `README.md`, "Reading problem details" | After the paragraph under the example: `OutOfCredit` can be the declaration the service uses, relative or absolute, and why it matches. |
| `CHANGELOG.md` | The entry drafted in R-01. |
| `example_test.go` | `ExampleType_Match_shared` (R-06). `ExampleRead` is unchanged: its absolute declaration still matches (FR-007 row 9). |

`doc.go` keeps "Prefer an absolute URI; a relative one must begin with "/"". RFC 9457 §3.1.1
still recommends absolute URIs, and the sentence is about declaring, not matching.

## R-08 Dependencies

- **Decision**: none added. `problem.go` imports `net/url`, which `uri.go` already uses, and
  `type.go` imports `strings`. `go.mod` and `go.sum` do not change.
- **Rationale**: Principle V. The standard library resolves URIs (RFC 3986 §5) and the module
  already relies on it in `Read`.

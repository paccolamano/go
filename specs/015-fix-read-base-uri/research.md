# Research: Read Resolves Against the URL the Request Sent (problem)

**Feature**: [spec.md](spec.md) | **Plan**: [plan.md](plan.md) | **Date**: 2026-11-11

The Technical Context had no open questions. The language, toolchain, module and dependencies are
those of the existing `problem` module (specs 004, 005 and 012), and the request settles the
approach: remove the userinfo and the fragment from the URL that `Read` resolves against and keeps.
This research records the design decisions, and the evidence behind them.

## Evidence

- **Reproduction**: a probe test in a scratch copy of `problem/v0.2.1`, outside the working tree.
  - **FR-006 in memory**: with the request URL
    `http://svc:s3cret@api.example.com/v1/accounts/42?api_key=XYZ#frag`, the five relative rows
    failed. Each value held `svc:s3cret@`, and the member `#` gave `…?api_key=XYZ#frag`. The three
    other rows passed: the network-path reference, the absolute URI and the missing type.
  - **Loopback, through `http.Client`**: three request URLs were read. Each gave a `Type()`, an
    `Instance()`, an `Error()` and a document written again with `Write` that contained the
    credentials:
    - a user name and a password, a query and a fragment;
    - a user name only, `http://svc@…`;
    - a relative redirect, from `/old#frag` to `/v2/accounts/42?page=2`.
  - **The type test breaks too**: the spec does not say so. The table of FR-007 of spec 012 was
    read from its request URL with credentials and a fragment added, `https://svc:s3cret@api.example.com/v1/messages#frag`.
    Rows 1 and 4 gave types holding the credentials. Rows 2 and 9 reported no match where the
    table requires one:
    - row 2: a relative declaration against a document with the absolute type;
    - row 9: an absolute declaration against a document with a relative type.

    The declaration and the type no longer share the userinfo, so they differ.
  - **What the request sent**: after a redirect whose `Location` is `/v2/accounts/42?page=2#loc`:
    - the server received the request-target `/v2/accounts/42?page=2` and an
      `Authorization: Basic` header;
    - `resp.Request.URL` still held
      `http://svc:s3cret@127.0.0.1:<port>/v2/accounts/42?page=2#loc`.
- **Spike**: the design of [R-02](#r-02-where-the-url-is-cleaned-fr-001-to-fr-005) was applied to
  the scratch copy. Three lines in `read.go` changed. With it:
  - every probe above passed;
  - the existing suite passed under `-race -shuffle=on`, at 99.6% statement coverage, as at
    `problem/v0.2.1`;
  - `golangci-lint` v2.14.0, with the repository configuration, reported 0 issues;
  - `go doc -all` printed the same exported API as `problem/v0.2.1`;
  - `FuzzRead`, extended with the checks of [R-06](#r-06-testing-approach-fr-014-fr-015-sc-001-to-sc-005-principle-iii):
    - the new seeds with userinfo failed on `problem/v0.2.1` (10 seeds) and passed with the fix;
    - 60 seconds of fuzzing ran 4.86 million executions without a failure (see
      [R-07](#r-07-fuzzing-throughput-sc-004)).
- **Not in the spike**:
  - `gorelease`, which is not installed locally. The `go doc` comparison stands in for it, and the
    pull request workflow runs it.
  - Bearer and Semgrep (see [R-08](#r-08-placeholder-credentials-and-the-pull-request-scanners)).
  - The documentation, and the final form of the tests.

## R-01 Release level and changelog

- **Decision**: release `problem/v0.2.2`, a PATCH.
  - The exported API does not change (FR-011). `go doc -all` is identical to `problem/v0.2.1`
    (spike). `gorelease` is expected to report no change and suggest `v0.2.2`, as it suggested
    `v0.2.1` for spec 012.
  - The type and instance URIs change only for request URLs with userinfo or a fragment
    ([R-03](#r-03-what-changes-and-what-does-not-fr-006-to-fr-010)). The changelog marks the change
    as a behavior change.
- **Changelog draft** (`problem/CHANGELOG.md`, section `[Unreleased]`, a new "Security" section):

  ```markdown
  ### Security

  - `Read` resolves relative `type` and `instance` members against the URL that the request sent:
    the request URL without its userinfo and its fragment, with its query. **Behavior change**:
    when the request URL held a user name or a password, `Read` copied them into the type and
    instance URIs of the problem it read. From there they reached the problem's message, so the
    logs, and the response of any service that writes the problem again. These URIs no longer
    contain them. For such requests, `Type.Match` now also matches a relative declaration with a
    problem whose type is the absolute URI on the server that answered, and an absolute
    declaration with a problem whose type is relative, as it does for other requests. An
    `instance` or a `type` that is just `#` no longer takes the fragment of the request URL.
    Request URLs without userinfo and without a fragment give the same results as before.
  ```

  The entry names no URL with credentials, so the secret scans have nothing to match in it.
- **Beyond FR-013**: the entry also states the change to `Type.Match`, observed in the reproduction.
  FR-013 lists what the entry must say, and does not forbid more.
- **Pull request**: title `fix(problem): resolve read problems against the URL the request sent`.
  It has no `!`, because nothing breaks. The release follows in its own pull request,
  `chore(problem): prepare release 0.2.2`. That pull request moves the entry to
  `## [0.2.2] - <date>`, as #26 did for 0.2.1.
- **Rationale**: Principle II. The released version matches the level that `gorelease` reports,
  and the changelog records the user-visible change in the same change.

## R-02 Where the URL is cleaned (FR-001 to FR-005)

- **Decision**: `Read` clears the userinfo and the fragment of its copy of the request URL, before
  it resolves the members and before it keeps the copy in `Error.base`:

  ```go
  if resp.Request != nil && resp.Request.URL != nil {
      // The URL the request sent (RFC 9110 §4.2.4 and §7.1): a copy of the
      // request URL without its userinfo and its fragment. A copy, because the
      // caller may change its request after Read returns.
      base := *resp.Request.URL
      base.User = nil
      base.Fragment, base.RawFragment = "", ""
      e.base = &base
      e.typ = resolve(e.base, e.typ)
      e.instance = resolve(e.base, e.instance)
  }
  ```

  - **One URL for everything**: the members are resolved against the cleaned copy, and the type
    test resolves relative declarations against the same copy, through `e.base` (FR-003). Both
    sides of the comparison still share one base, as spec 012 requires.
  - **The caller's URL is untouched** (FR-005): the assignments change the copy's fields. The
    caller's `*url.Userinfo` is not modified: the copy stops pointing to it.
  - **Derived problems** (FR-010): `(*Error).With` copies the struct, so a derived problem keeps
    the cleaned `base`, with no new code.
  - **Every other field is kept**: scheme, opaque part, host, path, raw path, query and the flags
    of `url.URL`. A request URL without userinfo and without a fragment therefore gives a copy
    equal to the one of 0.2.1, field for field (FR-007).
- **Rationale**: removing the two components from the base is exactly the base of FR-001. Clearing
  fields of the copy that `Read` already makes adds no allocation and no new code path.
- **Alternatives rejected**:
  - **Remove the userinfo from the resolved strings instead of the base**: `e.base` would keep the
    credentials, which FR-003 forbids. The type test would resolve relative declarations with the
    credentials, so `/probs/out-of-credit` would never match the cleaned type, breaking spec 012.
  - **Clean the base inside `resolve`**: the type test calls `resolve` on every relative
    comparison, so the URL would be copied each time. `e.base` would still hold the credentials.
  - **Build a new `url.URL` from a list of the fields to keep**: equivalent today. But a field
    that a later Go release adds to `url.URL` would be dropped without notice, and FR-007 would
    then depend on keeping the list complete. Clearing the two components keeps everything else
    by construction.
  - **`url.URL.Redacted`**: it returns a string, and replaces only the password, with `xxxxx`. The
    user name stays, and FR-001 removes it (RFC 9110 §4.2.4).
  - **A round trip through `String` and `url.Parse`**: costs allocations, and adds an error path
    for a URL that is already parsed.
  - **An unexported helper in `uri.go`**: one call site, three lines, and the public tests cover
    it through `Read`.
  - **Remove the query as well**: RFC 3986 §5.2.2 gives a fragment-only reference the query of
    the base (spec Assumptions).

## R-03 What changes and what does not (FR-006 to FR-010)

`url.URL.ResolveReference` takes from the base:

- **The userinfo**: for every reference without a scheme, a host and a userinfo of its own.
  Examples are `/probs/x`, `msgs/1`, `?page=2`, `#msg-1` and `#`. The fix removes it from all of
  them.
- **The fragment**: only for a reference with an empty path, no query and no fragment. `Parse`
  refuses the empty string, so two members qualify:
  - `#`;
  - `//`, a network-path reference with an empty authority, which Go resolves like `#`.

  The fix changes their result only.
- **The type test**: declarations begin with "/" (`NewType`), so they have a path, and only the
  declaration `//` alone takes the base's fragment. Such a declaration has no practical use. For
  every other declaration, the fragment's removal changes no result of the type test. Removing
  the userinfo changes a result only for request URLs with userinfo: rows 2 and 9 of the table of
  spec 012 now match, as FR-008 requires. They did not match on 0.2.1 (reproduction).

Consequences, observed in the spike:

| Request URL | Member | `problem/v0.2.1` | With the fix |
|-------------|--------|------------------|--------------|
| `http://@api.example.com/v1/x`, an empty userinfo | type `/probs/x` | `http://@api.example.com/probs/x` | `http://api.example.com/probs/x` |
| `mailto:a@b`, with no fragment | type `/probs/x` | `mailto:///probs/x` | unchanged |
| `http:svc@x#frag`, an opaque URL | instance `#m` | `http:svc@x#m` | unchanged |
| `http:svc@x#frag`, an opaque URL | instance `#` | `http:svc@x#frag` | `http:svc@x` |
| `http://svc:s3cret@api.example.com/v1/x?q=1#frag` | instance `#` | `http://svc:s3cret@api.example.com/v1/x?q=1#frag` | `http://api.example.com/v1/x?q=1` |

- **Empty userinfo**: Go parses `http://@host` with a userinfo that is present and empty. RFC 3986
  counts it as a userinfo component, so FR-001 removes it, and FR-007 does not apply to it.
- **Opaque URLs**: in `http:svc@x`, `svc@x` is the opaque part, not a userinfo (`User` is nil), so
  it is kept, as in 0.2.1. Only its fragment is removed (FR-001). The spec's edge case, "resolution
  gives the result of 0.2.1", holds for such a URL without a fragment.
- **`#` with no trailing `#`**: Go's `url.URL` does not tell an empty fragment from none. The
  member `#` therefore resolves to the URL the request sent with no `#`, as the table of FR-006
  states. This is also what 0.2.1 gave for a request URL without a fragment. RFC 3986 §5.2.2
  strictly gives an empty fragment, `…?api_key=XYZ#`, and both forms name the same resource.
- **Parse**: unchanged. It never resolves (FR-009, US2 scenario 5).

## R-04 Redirects (FR-004)

- **Decision**: no code beyond R-02. `Read` already uses `resp.Request`, the last request of any
  redirect chain (R-10 of spec 004).
- **Evidence**: Go's `http.Client` builds each next request URL with
  `req.URL.Parse(location)`, which is `ResolveReference`. A relative `Location` keeps the userinfo
  of the previous URL. It takes the `Location`'s own fragment, and drops the previous one. In the
  reproduction:
  - `/old#frag` redirected to `/v2/accounts/42?page=2#loc` left `resp.Request.URL` with the
    userinfo and `#loc`;
  - redirected to `/v2/accounts/42?page=2`, it left the userinfo and no fragment.
- **Consequence for the tests**: the redirect case uses a `Location` with a fragment. The last
  request URL then holds both the userinfo and a fragment, and the test shows that both are
  removed.

## R-05 Cost

- **Decision**: no benchmark.
  - `Read` made the copy in 0.2.1 already. Clearing three fields of it allocates nothing.
  - The type test is unchanged, and `TestMatch_Allocs` still passes (spike).
  - Neither `Read` nor `Match` is declared performance-sensitive (Principle III).

## R-06 Testing approach (FR-014, FR-015, SC-001 to SC-005; Principle III)

Every test uses the standard `testing` package, with table-driven tests and named subtests.
Responses are built in memory, or served by `httptest.NewServer` on the loopback interface in the
test process, as specs 005 and 012 do (spec Assumptions). No real network, no sleep.

**Building the request URL**: the existing helpers `newRequest` and `readProblem` build the request
with `httptest.NewRequestWithContext`, which parses the target as a server reads a request-target.
In the spike, it kept the userinfo, but moved `#frag` into the query
(`RawQuery = "api_key=XYZ#frag"`, `Fragment = ""`). The new tests therefore build the request with
`http.NewRequestWithContext`, as a client does. It keeps the userinfo and the fragment in
`req.URL` (spike). The request URL itself is built with `url.UserPassword` or `url.User` in a
`url.URL` value ([R-08](#r-08-placeholder-credentials-and-the-pull-request-scanners)).

| Test (file) | Covers | Red without the fix |
|-------------|--------|---------------------|
| `TestRead_RequestURL` (`read_test.go`), new | FR-006: the eight rows, in memory, with the request URL of the table. FR-001, FR-002, FR-009. The request URL still holds its userinfo and fragment after `Read` (FR-005). Edge cases: a user name only, and `mailto:a@b`, with the result of 0.2.1. US1 scenarios 1, 2, 4 and 6; US2 scenarios 3 and 6. | the five relative rows (observed) |
| `TestRead_Credentials` (`read_test.go`), new | FR-014, SC-002, US1 scenarios 1 to 7. A loopback server answers with each document of the FR-006 table. `http.Client` reads it from three request URLs: a user name and a password with a query and a fragment; a user name only; and a relative redirect whose `Location` has a fragment. For each, `Type()`, `Instance()`, `Error()` (no title and no detail) and the document that `Write` produces contain neither the user name, nor the password, nor the request's fragment. The markers are strings that appear nowhere else in the URLs. `sharedCredit` matches, and the caller's request URL still holds its userinfo (FR-005). | every request URL (observed) |
| `TestMatch_ReadResponse` (`type_test.go`), extended | FR-008, US2 scenario 2. The rows of the FR-007 table of spec 012, read from `apiTarget` and from `apiTarget` with credentials and a fragment added. | with credentials: rows 1 and 4 (type), rows 2 and 9 (match), observed |
| `TestMatch_ReadTree` (`type_test.go`), extended | Edge case "problem from several services in one error": one of the two services read with credentials. Each problem matches against its own URL. | the subtest with credentials |
| `TestWith_ReadProblem` (`with_test.go`), extended | FR-010, US2 scenario 4. A problem read with credentials. The derived problem matches the relative and the absolute declaration exactly when the original does, and its `Type()` and `Instance()` hold no credentials. | the absolute declaration, and the credentials check |
| `FuzzRead` (`fuzz_test.go`), extended | FR-015, SC-003, SC-004. It keeps the property of spec 012. Seeds: each instance member of the FR-006 table, and `//`, read from three request URLs: with a user name, a password, a query and a fragment; with a user name only; and with neither. New checks: (a) `Read` leaves the request URL unchanged; (b) for a request URL without userinfo and without a fragment, `Type()` and `Instance()` equal `ResolveReference` against that URL, the computation of 0.2.1; (c) a relative member without a host and a userinfo of its own resolves to a URI with no userinfo, whose fragment is the member's own. | the seeds with a userinfo: 10 (observed) |

- **Kept unchanged**:
  - `TestRead_Resolve` covers request URLs without credentials, a response without a request, and
    `Parse` (FR-007, FR-009, US2 scenario 5).
  - `TestMatch_ReadFromServer` covers the spec 012 matrix. One of its targets, `/x;p?q#frag`,
    carries a fragment and still matches: none of its declarations takes the base's fragment
    (R-03).
  - `TestRead_WriteAgain` and `TestMatch_Allocs`.
- **Red first**: in `tasks.md`, the tests precede the change to `read.go`. They are run once to see
  the failures of the right-hand column (FR-014).
- **Coverage**: the fix adds no branch. Coverage stays at 99.6% (spike), above 80% (SC-005).

## R-07 Fuzzing throughput (SC-004)

- **Observation**: with the default `-fuzzminimizetime` of 60 s, a 60-second run of `FuzzRead`
  stops executing after about 9 seconds, and still passes.
  - It happens on the unmodified `problem/v0.2.1` as well: 226,973 executions, then 0 per second,
    with 16 workers and with 4.
  - `FuzzParse` does not stall: 974,308 executions in 15 seconds.
  - With `-fuzzminimizetime=1s`, `FuzzRead` sustains about 75,000 executions per second: 4.86
    million in 60 seconds, with the checks of R-06.

  The time goes to minimizing new inputs. The cause inside the fuzzing engine was not
  investigated.
- **Decision**: the quickstart runs the 60-second fuzz with `-fuzzminimizetime=1s`, so that the 60
  seconds of SC-004 are spent fuzzing. The command of the request, without the flag, still passes,
  but fuzzes for about 9 seconds.
- **Rationale**: the stall comes before this fix, and changes neither its code nor its tests. The
  flag only changes how long the engine spends shrinking an interesting input.

## R-08 Placeholder credentials and the pull request scanners

The tests and this spec contain placeholder credentials (`svc`, `s3cret`). Three pull request gates
scan for secrets or for security findings:

| Gate | What it scans | Status |
|------|---------------|--------|
| Gitleaks (6) | every commit of the pull request, `specs/` included | A local `gitleaks dir` run found no leak in `spec.md` and in a probe test with `http://svc:s3cret@…` literals. CI uses 8.30.1 (`versions.env`). The local build does not report its version. |
| Semgrep (7) | the repository except `specs/`, with `p/golang`, `p/trailofbits` and `p/github-actions` | not run locally |
| Bearer (8) | the repository except `specs/`, tests included, at every severity | not installed locally |

- **Decision**: in the Go sources, the request URL is a `url.URL` value whose userinfo is built
  with `url.UserPassword("svc", "s3cret")` or `url.User("svc")`. No `scheme://user:password@host`
  literal appears in the code, and no identifier is named `password`, `secret` or `token`. The
  expected strings contain no credentials anyway.
- **If a scanner still reports a placeholder**: suppress that one finding with the tool's own
  ignore mechanism, scoped to its fingerprint. The justification is "test placeholder, not a
  credential" (constitution, "Finding suppression").
- **Rationale**: constitution, "Secrets", and spec Assumptions. The values are obvious
  placeholders. Keeping them out of URL literals avoids depending on each scanner's patterns.

## R-09 Documentation (FR-012, FR-013)

The exact wording is set at implementation time. Each text must state the rule of FR-001 and
FR-002. The doc comments are drafted in [contracts/public-api.md](contracts/public-api.md).

| Where | Change |
|-------|--------|
| `read.go`, `Read` | "resolved against the URL of resp.Request" becomes the URL that the request sent: the URL of `resp.Request` without its userinfo and its fragment, with its query. |
| `read.go`, the comment in `Read` | The copy is the URL the request sent. |
| `problem.go`, the comment of `base` | It holds no userinfo and no fragment. |
| `type.go`, `Type.Match` | "the request URL that Read resolved the problem's members against" becomes "the URL that Read resolved …". A reader takes "the request URL" to include the credentials. |
| `doc.go`, "Reading problem details" | "against the URL of the request" becomes the URL that the request sent. "the same request URL" becomes "the same URL". |
| `README.md`, "Reading problem details" | "relative URIs are resolved against the request URL" becomes the URL the request sent, without its credentials and its fragment. "the same request URL as `Read`" becomes "the same URL as `Read`". |
| `example_test.go`, `ExampleRead` | The comment "resolved against the request URL", shown in the godoc example, becomes "resolved against the URL the request sent". |
| `CHANGELOG.md` | The entry drafted in R-01. |

No example is added, and no example output changes. `ExampleRead` uses a request URL without
credentials. `Read` already has its runnable example (Principle III).

## R-10 Dependencies

- **Decision**: none added. The fix assigns fields of a `url.URL` value that `Read` already holds,
  so `read.go` needs no new import. `go.mod` and `go.sum` do not change.
- **Rationale**: Principle V.

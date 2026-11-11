# Feature Specification: Read Resolves Against the URL the Request Sent (problem)

**Feature Branch**: `015-fix-read-base-uri` (suggested; not yet created)

**Created**: 2026-11-11

**Status**: Draft

**Input**: User description: "problem: Read must not put the userinfo or the fragment of the
request URL into the URIs of the problem it reads (fix for problem v0.2.1, released as v0.2.2)".
The full request is reproduced verbatim in [Appendix: original request](#appendix-original-request).

## Context

The `problem` module (spec `004-problem-details`, last released as `problem/v0.2.1` after spec
`012-fix-relative-type-match`) answers failed HTTP requests with RFC 9457 problem details, and
reads the problem details that other APIs answer with.

Reading a response resolves a relative `type` or `instance` member against the URL of the request
that produced the response (FR-025 of spec 004). The module uses that URL exactly as the caller's
HTTP client holds it. A client configured with credentials in the URL, such as
`http://svc:s3cret@users.internal:8080/v1/accounts/42`, keeps them in that URL: it sends them in
an `Authorization` header, and the request it sends carries no credentials in its URI. The module
then copies them into every relative reference it resolves. A problem of type
`/probs/out-of-credit` read from that request has the type
`http://svc:s3cret@users.internal:8080/probs/out-of-credit`. The credentials reach:

- the type URI and the instance URI of the problem;
- the problem's message, which is the type URI when the problem has no title and no detail, and
  therefore the logs;
- the response of any service that writes the read problem again;
- the URL that the module keeps with the problem for the type test (spec 012).

The fragment of the request URL is kept in that stored URL too, and a member that is just `#`
resolves to the request URL with its fragment. A relative redirect keeps the credentials of the
previous URL, so the last request of a redirect chain carries them as well.

Reproduced on `problem/v0.2.1` with a test server and a real HTTP client: a request to
`http://svc:s3cret@127.0.0.1:<port>/old#frag`, redirected to `/v2/accounts/42?page=2`, gives the
type `http://svc:s3cret@127.0.0.1:<port>/probs/out-of-credit` and, for the instance `#msg-1`,
`http://svc:s3cret@127.0.0.1:<port>/v2/accounts/42?page=2#msg-1`. A request URL with a user name
and no password, `http://svc@…`, puts `svc@` in both.

**What the RFCs say**:

- RFC 9457 §3.1.1 and §3.1.5: a relative `type` or `instance` "is resolved relative to the
  document's base URI, as per [URI], Section 5".
- RFC 3986 §5.1.3: the base URI is the URI used to retrieve the representation; after a redirect,
  the last URI used. RFC 3986 §5.1: a base URI is stripped of any fragment before use.
- RFC 9110 §7.1: the target URI excludes the fragment. RFC 9110 §4.2.4: a sender MUST NOT generate
  the userinfo subcomponent in a target URI. RFC 3986 §3.2.1 and §7.5 deprecate passwords in
  userinfo and ask applications not to render them in clear text.
- RFC 3986 §5.2.2: a reference with an empty path and no query, such as the fragment-only `#msg-1`,
  takes the query of the base.

The URI that retrieved the response is therefore the request URL without its userinfo and without
its fragment, with its query. Resolving against it makes the reading operation conform to the
RFCs. The fix removes the userinfo and the fragment from the URL used to resolve, and from the URL
kept with the problem. The type test resolves relative declarations against that same URL, so both
sides of its comparison keep sharing one base.

| Requirement wording | Name in the module |
|---------------------|--------------------|
| reading a response | `Read` |
| reading a document alone | `Parse` |
| the type test | `Type.Match` |
| deriving a problem | `(*Error).With` |
| writing a problem | `Write` |
| the type URI, the instance URI, the message of a problem | `(*Error).Type`, `(*Error).Instance`, `(*Error).Error` |
| the request URL | the URL of `resp.Request`, the last request of any redirect chain (R-10 of spec 004) |
| the URL kept with the problem | the URL that the type test resolves relative declarations against (FR-001 of spec 012) |

**Changes to earlier specs**:

- FR-001 of this spec amends FR-025 of spec 004: a relative `type` or `instance` is resolved
  against the request URL without its userinfo and its fragment, instead of the request URL as the
  client holds it.
- FR-001 of spec 012 is unchanged. The URL it refers to, "the URL that the reading operation used
  to resolve that problem's members", is now the URL of FR-001 of this spec.

**Actors**:

- **Client developer**: reads the problems of another API, logs them, branches on their type and,
  in a gateway, may write them again. Their HTTP client may carry credentials in the request URL.
- **Maintainer**: evolves the module and publishes its releases.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Credentials in the request URL never reach a problem read from the response (Priority: P1)

A client authenticates to another API with credentials in the request URL, such as
`http://svc:s3cret@api.example.com/v1/accounts/42`. The API answers with a problem whose type is
`/probs/out-of-credit`. The client reads it and logs it. Neither the log, nor the type URI, nor the
instance URI, nor a response that writes the problem again, contains the credentials.

**Why this priority**: this is the defect. Credentials end up in logs and, through a gateway, in
responses to third parties, without any error or warning.

**Independent Test**: send a request with credentials, a query and a fragment in its URL to a test
server that answers with a problem, read the response, and search the type URI, the instance URI,
the message and a rewritten document for the user name, the password and the fragment.

**Acceptance Scenarios**:

1. **Given** a request to `http://svc:s3cret@api.example.com/v1/accounts/42?api_key=XYZ#frag`,
   **When** the response carries a problem of type `/probs/out-of-credit` and is read, **Then**
   the type URI is `http://api.example.com/probs/out-of-credit`.
2. **Given** the same request, **When** the problem's instance is `#msg-1`, **Then** the instance
   URI is `http://api.example.com/v1/accounts/42?api_key=XYZ#msg-1`: no credentials, no fragment
   of the request, and the query of the request.
3. **Given** the problem of scenario 1 with no title and no detail, **When** its message is
   produced, **Then** it contains neither `svc` nor `s3cret`.
4. **Given** a request URL with a user name and no password, `http://svc@api.example.com/v1`,
   **When** a problem with a relative type is read, **Then** the type URI does not contain `svc@`.
5. **Given** a request with credentials that the server redirects to a relative location, **When**
   the final response carries a problem and is read, **Then** neither its type URI nor its
   instance URI contains the credentials.
6. **Given** the request of scenario 1, **When** the problem's instance is `#`, **Then** the
   instance URI is `http://api.example.com/v1/accounts/42?api_key=XYZ`, without the request's
   fragment `#frag`.
7. **Given** the problem of scenario 1, **When** it is written again, **Then** the written document
   contains neither the credentials nor the fragment of the request.

---

### User Story 2 - Everything else resolves and matches as before (Priority: P1)

A developer upgrades the module. For request URLs without credentials and without a fragment,
every problem reads exactly as before. For request URLs with them, only the credentials and the
fragment disappear: hosts, ports, paths and queries are those of 0.2.1, and the type test gives
the same results.

**Why this priority**: the type test is how code branches on problems, and the type URI is how
code identifies them. A change outside the defect would silently change the branches of released
code.

**Independent Test**: read the same documents from the same request URLs with and without
credentials and a fragment, and compare the type URIs, the instance URIs and the type test results
with `problem/v0.2.1`.

**Acceptance Scenarios**:

1. **Given** a request URL without credentials and without a fragment, **When** any document is
   read, **Then** the type URI, the instance URI and the type test results equal those of 0.2.1.
2. **Given** the declared type `/probs/out-of-credit` and the request of User Story 1, scenario 1,
   **When** the problem of type `/probs/out-of-credit` is read, **Then** the type test reports a
   match and returns the problem.
3. **Given** the request of User Story 1, scenario 1, **When** a document is read, **Then** each
   relative member resolves as in the table of FR-006.
4. **Given** a problem read from a response with credentials in the request URL, **When** a problem
   is derived from it, **Then** the type test matches the derived problem exactly when it matches
   the original, and the derived problem contains no credentials.
5. **Given** a document read alone, **When** its members are read, **Then** they are kept as
   written, as in 0.2.1.
6. **Given** the caller's request, **When** its response is read, **Then** the caller's request URL
   still holds its credentials and its fragment: the reading operation does not modify it.

---

### User Story 3 - Developers learn which URL relative members resolve against (Priority: P2)

A developer reads the documentation of the reading operation, the package documentation or the
README. They learn that relative members are resolved against the URL that the request sent: the
request URL without its credentials and its fragment, with its query. The changelog tells
upgraders that problems read from such requests no longer carry the credentials.

**Why this priority**: the behavior is correct without the documentation, but the documentation
says "the URL of the request", which a reader takes to include the credentials.

**Independent Test**: read the documentation and the changelog.

**Acceptance Scenarios**:

1. **Given** the documentation of the reading operation, the package documentation and the README,
   **When** a developer reads how relative members are resolved, **Then** it states the URL of
   FR-001 and FR-002.
2. **Given** the changelog, **When** a developer upgrades, **Then** an entry in a "Security"
   section, marked as a behavior change, says which type and instance URIs change.

---

### Edge Cases

- **Credentials without a password**: a user name alone is removed, as a user name with a password
  is (RFC 9110 §4.2.4 forbids any userinfo in a target URI).
- **Fragment-only member**: `#msg-1` takes the path and the query of the request URL, and its own
  fragment, never the request's fragment. A member that is just `#` takes the path and the query
  of the request URL, and no fragment.
- **Redirects**: the URL is the one of the last request of the redirect chain, without its
  credentials and its fragment, even when the client copied the credentials from an earlier URL.
- **Absolute members and network-path references**: `https://other.example.com/probs/x` is kept as
  written, and `//cdn.example.com/probs/x` takes only the scheme of the request URL, as in 0.2.1.
  Credentials written by the server inside an absolute member are part of the document, not of the
  request, and are kept as written, as reading a document alone keeps them.
- **Response without a request**: nothing is resolved, as in 0.2.1.
- **Request URL that is not hierarchical**, such as `mailto:a@b`: resolution gives the result of
  0.2.1; such a URL has no userinfo component.
- **Problem from several services in one error**: each problem keeps its own URL, without
  credentials, and the type test uses it, as in spec 012.

## Requirements *(mandatory)*

### Functional Requirements

#### The URL relative members resolve against

- **FR-001**: When a problem is read from a response, its relative `type` and `instance` members
  MUST be resolved against the request URL without its userinfo, user name and password alike,
  and without its fragment: the target URI that the request sent (RFC 9110 §7.1 and §4.2.4,
  RFC 3986 §5.1 and §5.1.3). This amends FR-025 of spec 004.
- **FR-002**: The other components of the request URL (scheme, host, port, path and query) MUST be
  kept. A member with an empty path and no query, such as `#msg-1`, MUST take the query of the
  request URL, as RFC 3986 §5.2.2 requires.
- **FR-003**: The URL kept with the problem for the type test MUST be the URL of FR-001. No part
  of a problem read from a response, exported or internal, MUST hold the userinfo or the fragment
  of the request URL.
- **FR-004**: FR-001 to FR-003 MUST apply to the last request of a redirect chain, whatever URL
  the client derived it from.
- **FR-005**: Reading a response MUST NOT modify the caller's request or its URL.
- **FR-006**: For a request to `http://svc:s3cret@api.example.com/v1/accounts/42?api_key=XYZ#frag`,
  the members of the document MUST resolve as follows:

  | Member in the document | `problem/v0.2.1` | This spec |
  |------------------------|------------------|-----------|
  | type `/probs/out-of-credit` | `http://svc:s3cret@api.example.com/probs/out-of-credit` | `http://api.example.com/probs/out-of-credit` |
  | instance `#msg-1` | `http://svc:s3cret@api.example.com/v1/accounts/42?api_key=XYZ#msg-1` | `http://api.example.com/v1/accounts/42?api_key=XYZ#msg-1` |
  | instance `#` | `http://svc:s3cret@api.example.com/v1/accounts/42?api_key=XYZ#frag` | `http://api.example.com/v1/accounts/42?api_key=XYZ` |
  | instance `?page=2` | `http://svc:s3cret@api.example.com/v1/accounts/42?page=2` | `http://api.example.com/v1/accounts/42?page=2` |
  | instance `msgs/1` | `http://svc:s3cret@api.example.com/v1/accounts/msgs/1` | `http://api.example.com/v1/accounts/msgs/1` |
  | type `//cdn.example.com/probs/x` | `http://cdn.example.com/probs/x` | unchanged |
  | type `https://other.example.com/probs/x` | as written | unchanged |
  | no type | `about:blank` | unchanged |

#### Behavior kept

- **FR-007**: For a request URL without userinfo and without a fragment, the type URI, the instance
  URI and the type test results of every problem read from the response MUST equal those of
  `problem/v0.2.1`.
- **FR-008**: The type test MUST give the results of the table of FR-007 of spec 012 both for the
  request URL of that table and for the same URL with credentials and a fragment added.
- **FR-009**: Reading a document alone MUST keep its members as written, and absolute members and
  network-path references MUST resolve as in `problem/v0.2.1`.
- **FR-010**: A problem derived from a problem read from a response MUST keep the URL of FR-001
  (FR-006 of spec 012).
- **FR-011**: The fix MUST NOT change the exported API of the module.

#### Documentation and release

- **FR-012**: The documentation of the reading operation, the package documentation and the README
  MUST state that relative members are resolved against the URL that the request sent: the request
  URL without its userinfo and its fragment, with its query.
- **FR-013**: The module's changelog MUST record the fix under the unreleased version, in a
  "Security" section, marked as a behavior change: the type URI, the instance URI and the message
  of a problem read from a response no longer contain the userinfo of the request URL, and a
  member that is just `#` no longer takes the fragment of the request URL.
- **FR-014**: Regression tests MUST reproduce User Story 1 with an HTTP client and a test server:
  a request URL with a user name and a password, a query and a fragment; a request URL with a user
  name only; a relative redirect; and a member that is just `#`. They MUST fail without the fix
  (Constitution Principle III).
- **FR-015**: The fuzz test of reading a response MUST keep checking the property of FR-015 of
  spec 012, MUST include in its seed corpus a request URL with userinfo, a query and a fragment,
  and MUST also check that a member that is a relative reference without an authority never
  resolves to a URI with userinfo or with the request's fragment.

### Key Entities

- **Request URL**: the URL of the last request of the exchange, as the caller's HTTP client holds
  it. It may hold credentials and a fragment that the request never sent.
- **URL the request sent**: the request URL without its userinfo and its fragment. It is the base
  URI of the response's document, and the URL kept with a problem read from it. It is internal:
  never written and never exported.
- **Problem read from a response**: its members, with the type and the instance resolved against
  the URL the request sent.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: 100% of the rows of the FR-006 table, and of the acceptance scenarios and edge cases
  of this spec, give the stated result in automated tests.
- **SC-002**: For request URLs with a user name and a password, with a user name only, with a
  fragment, and reached through a relative redirect, combined with every member of the FR-006
  table, 0 type URIs, instance URIs, messages and rewritten documents contain the user name, the
  password or the request's fragment.
- **SC-003**: For request URLs without userinfo and without a fragment, 100% of the type URIs,
  instance URIs and type test results of the fuzz seed corpus and of the FR-007 table of spec 012
  equal those of `problem/v0.2.1`.
- **SC-004**: The fuzz properties of FR-015 hold for 100% of the seed corpus, and 60 seconds of
  fuzzing find no counterexample.
- **SC-005**: Against `problem/v0.2.1`, the API compatibility check reports no change. Statement
  coverage of the module stays at least 80%.

## Assumptions

- **Release**: the fix ships as a patch release, `problem/v0.2.2`, in a release change separate
  from the fix, as for the previous releases. The API compatibility check reports no change, and
  the constitution requires the released version to match the level it reports (Principle II).
  The type and instance URIs change only for request URLs with credentials or a fragment, so the
  changelog marks the fix as a behavior change. Code is unlikely to rely on the old values: it
  would have to expect the client's own credentials inside a URI written by another service.
- **Changelog section**: the module's changelog has not used "Security" before. Keep a Changelog
  defines it for fixed vulnerabilities, and leaking credentials into logs and responses is one.
- **The query stays in the URL**: RFC 3986 §5.2.2 gives the request's query to fragment-only
  members, and RFC 9110 §17.9 states that URIs are meant to be shared, not secured. A fragment-only
  member is also one that RFC 9457 discourages, since it recommends absolute URIs or a full path.
- **Only the request URL is changed**: members that the server wrote as absolute URIs or
  network-path references are the server's content. They are kept as written, as when a document
  is read alone, and RFC 9457 §3.1.1 makes the type URI the problem type's identifier.
- **One URL for both sides of the type test**: the URL kept with the problem is the URL used to
  resolve its members, so a document whose type equals a relative declaration always matches it
  (spec 012).
- **Tests**: responses are served by a test server running in the test process on the loopback
  interface, as specs 005 and 012 allow. They stay hermetic and deterministic (Principle III). The
  credentials in tests and examples are obvious placeholders, which the secret scan of the pull
  request accepts.

## Appendix: original request

The request, as submitted to `/speckit-specify`:

````text
problem: Read must not put the userinfo or the fragment of the request URL into the URIs of the problem it reads (fix for problem v0.2.1, released as v0.2.2).

Context. `problem.Read` (problem/read.go) resolves relative "type" and "instance" members against a copy of `resp.Request.URL`, stored in the unexported `Error.base`, with `resolve` (problem/uri.go), which calls `url.URL.ResolveReference`. `ResolveReference` copies the base's userinfo into every relative reference it resolves. Go's `http.Client` keeps the userinfo in `req.URL` (it only turns it into an `Authorization: Basic` header; the request-target sent on the wire never contains it), so a request to `http://svc:s3cret@users.internal:8080/v1/accounts/42` with a problem whose type is `/probs/out-of-credit` gives `Type()` = `http://svc:s3cret@users.internal:8080/probs/out-of-credit`. The credentials then reach `Type()`, `Instance()`, `Error()` (which returns the type URI when title and detail are empty, so logs), and the response of any service that writes the read problem with `Write`. `Error.base` also keeps them in memory, and so does the fragment of the request URL.

What the RFCs say (verified on the RFC texts):
- RFC 9457 §3.1.1 and §3.1.5: relative "type" and "instance" are "resolved relative to the document's base URI, as per [URI], Section 5".
- RFC 3986 §5.1.3: the base URI is the URI used to retrieve the representation; after redirects, the last one. RFC 3986 §5.1: a base URI must be stripped of any fragment before use.
- RFC 9110 §7.1: the target URI excludes the fragment. RFC 9110 §4.2.4: a sender MUST NOT generate userinfo in a target URI. So the URI that actually retrieved the response never has userinfo nor fragment: removing them makes Read conform to the RFCs, it is not a deviation.
- RFC 3986 §5.2.2: a reference with an empty path and no query (a fragment-only reference such as "#msg-1") takes the base's query. The query is part of the target URI, so it stays in the base.

Decision (taken by the maintainer):
- Read resolves against the request URL without its userinfo and without its fragment. The copy stored for later use (`Error.base`) is the same sanitized URL, because `Type.Match` (`hasType` in problem/type.go) resolves relative declared types against `e.base`, and both sides of the comparison must share one base.
- The query of the request URL is kept, as RFC 3986 §5.2.2 requires: a fragment-only "instance" or "type" still inherits it. Every other reference resolves as before.
- The fix applies to the last request of a redirect chain as well (a relative redirect keeps the userinfo of the previous URL in Go).

Expected results, for a request to `http://svc:s3cret@HOST/v1/accounts/42?api_key=XYZ#frag`:
- type "/probs/out-of-credit" → `http://HOST/probs/out-of-credit`;
- instance "#msg-1" → `http://HOST/v1/accounts/42?api_key=XYZ#msg-1`;
- `Error()` contains no "s3cret" and no "svc@";
- a type declared as "/probs/out-of-credit" still matches the problem through `Type.Match`;
- `Parse` is unchanged (it never resolves).

Constraints:
- No public API change. Behavior change only for requests whose URL carries userinfo or a fragment.
- Update the godoc of `Read` (and the "Reading problem details" section of doc.go and of the README if they describe the base) to say that the base is the request URL without userinfo and fragment, i.e. the URL that the request sent.
- `problem/CHANGELOG.md`: an entry under `## [Unreleased]` in a "Security" section, marked **Behavior change**; release as `problem/v0.2.2` in a separate release PR, as for the previous releases.
- Tests: Read through a real `http.Client` against an httptest server with userinfo, query and fragment in the URL; redirect case; `Type.Match` with a relative declaration; FuzzRead keeps passing, with a seed target carrying userinfo, query and fragment, and a fuzz assertion that no resolved URI contains the userinfo of the request URL.
- Gates: `make lint MODULE=problem`, `make test MODULE=problem`, and `go test -run='^$' -fuzz=FuzzRead -fuzztime=60s` in problem/.
- One PR, title `fix(problem): ...` in Conventional Commits.
````

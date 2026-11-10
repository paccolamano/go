# Feature Specification: Relative Problem Types Match Problems Read From a Response (problem)

**Feature Branch**: `012-fix-relative-type-match` (suggested; not yet created)

**Created**: 2026-11-10

**Status**: Draft

**Input**: User description: "problem: make Type.Match work for relative type URIs on problems
read with Read". The full request is reproduced verbatim in
[Appendix: original request](#appendix-original-request).

## Context

The `problem` module (spec `004-problem-details`, last released as `problem/v0.2.0` after spec
`005-problem-fixes`) answers failed HTTP requests with RFC 9457 problem details, and reads the
problem details that other APIs answer with.

A developer declares each problem type once, with a type URI of their choice. The module accepts
an absolute URI or a reference that begins with "/" (FR-008 of spec 004): RFC 9457 §3.1.1
recommends absolute URIs and allows relative ones that include the full path. Writing a problem
copies the declared URI into the document as written; the module adds no host. Reading a
response resolves a relative type against the URL of the request (FR-025 of spec 004), because
RFC 9457 §3.1.1 makes the URI after resolution the problem type's primary identifier.

The type test compares the declared URI with the problem's type URI (FR-021 of spec 004). For a
problem read from a response, only the problem's side has been resolved. A relative declaration
therefore never matches a problem read from a response, even when the response was written from
the same declaration. The test reports no match, and no error. The same declaration matches the
same problem when it is built locally or read from a document alone. Services commonly share
their problem declarations with their client package, so a client that reads the service's
responses cannot recognize the service's problems. A first user of the module worked around the
defect by reading responses by hand, which loses the resolution and the size bound of the reading
operation.

Reproduced with a test server: a problem of the type `/problems/user-not-registered`, written by
the server and read by a client from a response to `http://127.0.0.1:<port>/api/v1/users/me`,
has the type `http://127.0.0.1:<port>/problems/user-not-registered`. The type test with the same
declaration reports no match.

The fix resolves both sides of the comparison against the same URL. A relative declaration is
resolved against the URL that the problem was read from, then compared with the problem's type.
A relative declaration then means "this path, on the server that answered", which is what the
server meant when it wrote it.

| Requirement wording | Name in the module |
|---------------------|--------------------|
| declaring a type | `NewType` |
| the type test | `Type.Match` |
| reading a response | `Read` |
| reading a document alone | `Parse` |
| building a problem locally | `Type.New`, `Status` |
| writing a problem | `Write` |
| deriving a problem | `(*Error).With` |
| the type URI of a problem | `(*Error).Type` |
| the URL a problem was read from | the URL of `resp.Request`, the last request of any redirect chain (R-10 of spec 004) |

**Changes to spec 004**:

- FR-001 of this spec amends FR-021 of spec 004. Spec 004 compares the declared type URI with the
  problem's type URI after resolution. This spec resolves both against the URL the problem was
  read from.
- FR-008 (declaring a type) and FR-025 (resolving when reading) of spec 004 are unchanged.

**Actors**:

- **Service developer**: declares the problem types of a service and writes its problems.
- **Client developer**: reads the problems of a service, often with the declarations that the
  service shares in its client package. It can be the same person as the service developer.
- **Maintainer**: evolves the module and publishes its releases.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - A client recognizes the problems of a relative type it shares with the service (Priority: P1)

A service declares "out of credit" as `/probs/out-of-credit`, and ships the declaration in its
client package. A client calls `https://api.example.com/v1/messages`, gets a 403 that carries
the problem, reads it, and tests it against the shared declaration to read the balance and react.
The test reports a match.

**Why this priority**: this is the defect. The type test fails without any error, so a client
takes the wrong branch, and the workaround bypasses the size bound of the reading operation.

**Independent Test**: write a problem of a relative declared type in a response, read it back as
a client does, with the request URL set, and apply the type test with the same declaration.

**Acceptance Scenarios**:

1. **Given** the declared type `/probs/out-of-credit`, **When** a response to a request for
   `https://api.example.com/v1/messages` carries a document of type `/probs/out-of-credit` and is
   read, **Then** the type test with that declaration reports a match and returns the problem.
   The problem's type is `https://api.example.com/probs/out-of-credit`, and its extensions are
   readable.
2. **Given** the same declaration and request, **When** the document's type is the absolute
   `https://api.example.com/probs/out-of-credit`, **Then** the type test reports a match.
3. **Given** the same declaration and request, **When** the document's type is `/probs/other` or
   `https://other.example.com/probs/out-of-credit`, **Then** the type test reports no match.
4. **Given** the problem read in scenario 1, wrapped with context, joined with other errors, or
   set as the internal cause of another problem, **When** the type test runs on the outer error,
   **Then** it finds the problem, looking past problems of other types (FR-021 of spec 004).
5. **Given** a server on `http://127.0.0.1:<port>` that writes a problem from a relative
   declaration, **When** a client sends a request to any path of that server and reads the
   response, **Then** the type test with the same declaration reports a match.

---

### User Story 2 - What matched still matches, and nothing else starts matching (Priority: P1)

A developer upgrades the module. Server-side code that tests problems built locally, clients that
declare the absolute URIs of other APIs, and code that reads documents alone behave exactly as
before. Only the case of User Story 1 changes.

**Why this priority**: the type test is how code branches on problems. A change outside the
defect would silently change the branches of released code.

**Independent Test**: apply the table of FR-007 to problems built locally, read from a document
alone, read from a response without a request, and derived. Compare the results with
`problem/v0.2.0`.

**Acceptance Scenarios**:

1. **Given** the declared type `https://api.example.com/probs/out-of-credit`, **When** a response
   to a request for `https://api.example.com/v1/messages` carries a document of type
   `/probs/out-of-credit` and is read, **Then** the type test reports a match, as in 0.2.0.
2. **Given** the same absolute declaration, **When** a document of type `/probs/out-of-credit`
   is read alone, **Then** the type test reports no match, as in 0.2.0.
3. **Given** the declared type `/probs/out-of-credit`, **When** a problem is built locally from
   it, or a document of type `/probs/out-of-credit` is read alone or from a response without a
   request, **Then** the type test reports a match. When a document of type
   `https://api.example.com/probs/out-of-credit` is read alone, it reports no match. Both results
   are those of 0.2.0.
4. **Given** a problem read from a response, **When** a problem is derived from it, **Then** the
   type test matches the derived problem exactly when it matches the original, for relative and
   absolute declarations (FR-006 of spec 005).
5. **Given** a problem read from a response, **When** its type URI is asked for, **Then** it is
   the resolved URI, as in 0.2.0. **When** it is written again, **Then** the document is the one
   that 0.2.0 writes.
6. **Given** the type URIs `/probs/out-of-credit` and `out-of-credit`, **When** each is
   declared, **Then** the first is accepted and the second is refused, as in 0.2.0.

---

### User Story 3 - Developers learn the rule from the documentation (Priority: P2)

A developer reads the documentation of the type test or of the reading operation, the package
documentation or the README. They learn that a declaration shared with a service can be
relative: it matches a problem read from a response when it names the same path on the server
that answered. Absolute declarations are compared as written. The advice that a type must be
absolute to match another API's problems is gone. The changelog tells upgraders that the type
test now reports matches that it did not report before.

**Why this priority**: the behavior is correct without the documentation, but today's
documentation states the opposite rule.

**Independent Test**: read the documentation, and run the runnable example.

**Acceptance Scenarios**:

1. **Given** the documentation of the type test, **When** a developer reads it, **Then** it
   states the rule of FR-001 to FR-003, and no longer requires an absolute URI.
2. **Given** the README example that reads a problem from a response, **When** its declaration is
   one shared with the service, **Then** the example is correct as written. A runnable example
   shows the same flow, with a response whose request URL is set.
3. **Given** the changelog, **When** a developer upgrades, **Then** an entry under "Fixed",
   marked as a behavior change, explains which type tests now report a match.

---

### Edge Cases

- **Redirects**: the URL is the one of the last request of the redirect chain, as for the
  resolution of the reading operation. A relative declaration matches the type written by the
  server that answered last. An absolute declaration naming the host of the first request does
  not match a relative type written by the host of the last one, as in 0.2.0.
- **Problems from several services in one error**: each problem is compared with the declaration
  resolved against its own URL. The first match in the order of the walk wins.
- **Network-path reference**: a declared type such as `//cdn.example.com/probs/out-of-credit`
  begins with "/", so it can be declared. It resolves with the scheme of the URL the problem was
  read from, as RFC 3986 §5 says.
- **Query, fragment or percent-encoding in a declared type**: the declared type resolves exactly
  as the reading operation resolves the document's type, so a document whose type equals the
  declaration always matches.
- **Document without a type**: its type is `about:blank`. No declaration resolves to
  `about:blank`, so no declared type matches it, as in 0.2.0.
- **Absent problem, absent error or undeclared type**: the type test reports no match, as in
  0.2.0.

## Requirements *(mandatory)*

### Functional Requirements

#### The type test

- **FR-001**: When the type test examines a problem read from a response, and the declared type
  URI is relative, it MUST resolve the declared URI against the URL that the reading operation
  used to resolve that problem's members (FR-025 of spec 004), with the same resolution rules
  (RFC 3986 §5), and compare the result with the problem's type URI. This amends FR-021 of
  spec 004: both sides of the comparison are resolved against the same URL.
- **FR-002**: An absolute declared type URI MUST be compared as written, as in spec 004.
- **FR-003**: For a problem that has no URL, because it was built locally, read from a document
  alone or read from a response without a request, the declared type URI MUST be compared as
  written, as in spec 004.
- **FR-004**: The comparison MUST stay an exact comparison of the two URIs after resolution, with
  no other normalization (letter case, default ports, percent-encoding), as in spec 004.
- **FR-005**: In an error tree, each problem MUST be compared using its own URL, when it has one.
  The order of the walk, and looking past problems of other types, MUST NOT change (FR-021 of
  spec 004).
- **FR-006**: A problem derived from a problem read from a response MUST keep the URL of the
  original, so that the type test matches it exactly when it matches the original (FR-006 of
  spec 005).
- **FR-007**: For a problem read from a response to a request for
  `https://api.example.com/v1/messages`, or from the sources given in the table, the type test
  MUST give these results:

  | Declared type | Problem | Problem's type | Match |
  |---------------|---------|----------------|-------|
  | `/probs/out-of-credit` | read from the response, document type `/probs/out-of-credit` | `https://api.example.com/probs/out-of-credit` | yes (was no) |
  | `/probs/out-of-credit` | read from the response, document type `https://api.example.com/probs/out-of-credit` | as written | yes (was no) |
  | `/probs/out-of-credit` | read from the response, document type `https://other.example.com/probs/out-of-credit` | as written | no |
  | `/probs/out-of-credit` | read from the response, document type `/probs/other` | `https://api.example.com/probs/other` | no |
  | `/probs/out-of-credit` | read from a response without a request, document type `/probs/out-of-credit` | as written | yes |
  | `/probs/out-of-credit` | read from a document alone, type `/probs/out-of-credit` | as written | yes |
  | `/probs/out-of-credit` | read from a document alone, type `https://api.example.com/probs/out-of-credit` | as written | no |
  | `/probs/out-of-credit` | built locally from the declaration | `/probs/out-of-credit` | yes |
  | `https://api.example.com/probs/out-of-credit` | read from the response, document type `/probs/out-of-credit` | `https://api.example.com/probs/out-of-credit` | yes |
  | `https://api.example.com/probs/out-of-credit` | read from a document alone, type `/probs/out-of-credit` | as written | no |

  Only the first two rows change from `problem/v0.2.0`.

#### Behavior kept

- **FR-008**: Declaring a type MUST keep accepting relative URIs that begin with "/", and
  refusing the other relative URIs (FR-008 of spec 004).
- **FR-009**: The type URI of a problem read from a response MUST stay the resolved URI (FR-025 of
  spec 004). Writing any problem MUST produce the same document as `problem/v0.2.0`.
- **FR-010**: The fix MUST NOT change the exported API of the module.

#### Documentation and release

- **FR-011**: The documentation of the type test, the documentation of the reading operation, the
  package documentation and the README MUST state the rule of FR-001 to FR-003. The statement that
  a type declared to match another API's problems must use that API's absolute URI MUST be
  replaced by it.
- **FR-012**: A runnable example MUST show a declaration shared by a service and its client
  matching a problem read from a response whose request URL is set.
- **FR-013**: The module's changelog MUST record the fix under the unreleased version, in
  "Fixed", marked as a behavior change: the type test now reports a match for a relative
  declaration and a problem read from a response, where it reported none.
- **FR-014**: Regression tests MUST reproduce User Story 1 by reading responses whose request URL
  is set, as an HTTP client sets it. They MUST fail without the fix (Constitution Principle III).
- **FR-015**: A fuzz test MUST check, for documents read from a response, that a type declared
  with the document's own type URI, as written, matches the problem whenever that URI can be
  declared.

### Key Entities

- **Problem type declaration**: a type URI (absolute, or a reference that begins with "/"), a
  title and a status code. It does not change.
- **Problem read from a response**: its members, with the type and the instance resolved, and the
  URL they were resolved against. The URL is internal: it is never written and never exported.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: 100% of the rows of the FR-007 table, and of the acceptance scenarios and edge cases
  of this spec, give the stated result in automated tests.
- **SC-002**: At least 10 relative declarations, including ones with a query, a fragment,
  percent-encoding and a network-path reference, are each written by a server and read by a
  client from responses to at least 5 request URLs: different paths, a different port, and a
  redirect. The type test with the same declaration reports a match in 100% of the combinations.
- **SC-003**: For problems built locally, problems read from a document alone, and absolute
  declarations, the type test allocates no memory, as with `problem/v0.2.0`.
- **SC-004**: The fuzz property of FR-015 holds for 100% of the seed corpus, which includes the
  documents of every row of the FR-007 table.
- **SC-005**: Against `problem/v0.2.0`, the API compatibility check reports no change. Statement
  coverage of the module stays at least 80%.

## Assumptions

- **Release**: the fix ships as a patch release, `problem/v0.2.1`. The API compatibility check
  reports no change, and the constitution requires the released version to match the level it
  reports (Principle II). The type test now reports matches it did not report, so the changelog
  marks the fix as a behavior change, as 0.2.0 did for the write fix. Code is unlikely to rely
  on the old result: it would have to depend on a relative declaration never matching a response
  written from that same declaration.
- **Same URL as the reading operation**: the declaration is resolved against the URL that the
  reading operation used for the problem's members: the last request of the redirect chain
  (R-10 of spec 004). Using one URL for both sides guarantees that a document whose type equals
  the declaration always matches.
- **Meaning of a relative declaration**: "this path, on the server that answered". The module
  does not check which server answered. A relative declaration matches a problem of that path
  from any server the client reads, as it already does for documents read alone. Developers who
  need to bind a type to one host declare it with an absolute URI, which is compared as written.
- **Relative declarations stay accepted**: RFC 9457 allows them, and they let a service that does
  not know its public host, such as one installed on several domains, serve the documentation of
  its types on each of them. Refusing them would also break existing declarations.
- **Tests**: responses whose request URL is set are built in memory, or served by a test server
  running in the test process on the loopback interface, as spec 005 allows. Both stay hermetic
  and deterministic (Principle III).

## Appendix: original request

The request, as submitted to `/speckit-specify`:

````text
problem: make Type.Match work for relative type URIs on problems read with Read. Today NewType accepts relative type URIs that begin with "/" (RFC 9457 §3.1.1 allows them, spec 004 FR-008), Write writes them verbatim, and Read resolves the document's type against the URL of the request (spec 004 FR-025). Type.Match then compares the declared relative URI with the resolved one as strings, so a declaration shared by server and client (e.g. "/problems/user-not-registered") never matches a problem read with Read, silently returning false, while it matches the same problem built locally or read with Parse. Fix: when a problem was read with Read, Match resolves a relative declared type URI against the same URL that Read used for the document, then compares it with the resolved type. This amends FR-021 of spec 004 (type equality): both sides of the comparison are resolved against the same base. Unchanged: absolute declared types; problems built locally, read with Parse or with no request URL (compared as written, as today); Type() still returns the resolved URI; NewType keeps accepting relative URIs. A problem derived with With keeps the URL of the problem it derives from. With redirects the base is the last request of the chain, as Read already does. No new exported API. Release as a patch (problem v0.2.1); CHANGELOG under Fixed with a Behavior change note (Match now returns true where it returned false). Update the docs that say "a type declared to match another API's problems must use that API's absolute URI" (doc.go, Read, Type.Match, README).
````

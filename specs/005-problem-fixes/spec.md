# Feature Specification: Problem Module Fixes and Improvements

**Feature Branch**: `005-problem-fixes` (suggested; not yet created)

**Created**: 2026-10-19

**Status**: Draft

**Input**: User description: "Task: fix bugs and improve the problem module". The full request
(items B1, B2, I1 to I4 and "Not in this task") is reproduced verbatim in
[Appendix: original request](#appendix-original-request).

## Context

The `problem` module (spec `004-problem-details`, released as `problem/v0.1.0`) answers failed
HTTP requests with RFC 9457 problem details. Two defects were found after the release, and its
first users need three things that every API ends up writing by hand. This feature fixes the
defects, adds the missing capabilities and corrects the documentation, in the same module.

| Item | Kind | Summary | Requirements |
|------|------|---------|--------------|
| B1 | Bug | Headers that describe a previous representation corrupt the problem response | FR-001 to FR-003 |
| B2 | Bug | Reading an extension of an absent problem crashes the program | FR-004 |
| I1 | Improvement | Derive a problem with more members, for example a request ID added by the HTTP layer | FR-005 to FR-010 |
| I2 | Improvement | Report validation errors as RFC 9457 §3 suggests, with correct JSON Pointers (RFC 6901) | FR-011 to FR-018 |
| I3 | Documentation | Never discard the error that the write operation returns | FR-019 to FR-021 |
| I4 | Documentation | Translate domain errors into problems, without leaking internals | FR-022 to FR-024 |
| — | Release | Examples, changelog and regression tests for all of the above | FR-025 to FR-027 |

The request names the new operations, and for a library the exported names are the contract
that users see. The requirements below describe behavior. This table maps them to the names of
the request, whose signatures are in the appendix:

| Requirement wording | Name given by the request |
|---------------------|---------------------------|
| deriving a problem | `(*Error).With` |
| validation item | `Item`, with members `detail` and `pointer` |
| setting the validation items | `WithErrors` |
| building a pointer | `Pointer` |
| reading the validation items | `(*Error).Errors` |
| the write operation | `Write` (existing) |
| reading an extension | `Key.Lookup` (existing) |
| the invalid-problem error | `ErrInvalid` (existing) |

**Changes to spec 004**: FR-001 and FR-002 of this spec replace FR-014 of spec 004, which kept
every header except the media type. Spec 004 accepted `errors` as an extension name. That stays
true, but the member now belongs to the validation items (FR-017).

**Actors** (as in spec 004):

- **Developer**: builds an HTTP API and answers failed requests with problem details.
- **Client**: the program that receives a problem document and acts on it.
- **Maintainer**: evolves the library and publishes its releases.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - The client can read a problem whatever the handler set before failing (Priority: P1)

A handler starts preparing a successful response, then fails before sending the body, and answers
with a problem. Before failing, it may have set headers that describe the body it meant to send:

- the length of a file;
- `Content-Encoding: gzip`, for a precompressed asset;
- an `ETag`;
- a `Content-Disposition`, for a download.

Today these headers stay on the problem response and contradict the problem document. The client
gets a truncated body, or tries to decompress plain JSON, and cannot read the problem at all.
Other headers apply to any response and must stay: the `Retry-After` that a rate limiter sets
before it refuses a request, `Content-Language`, `Cache-Control`, `Vary`, and security headers.

**Why this priority**: the defect is in released behavior. When it occurs, the client cannot read
the problem at all, and delivering the problem is the module's purpose.

**Independent Test**: serve handlers through an in-process HTTP server and read the responses
with a standard HTTP client. A recorded response is not enough: the length mismatch and the
failed decompression only happen between a real server and a real client.

**Acceptance Scenarios**:

1. **Given** a handler that sets `Content-Length: 5` and then answers with a 404 problem,
   **When** a client requests it through a real server, **Then** the client receives status 404
   and the complete document `{"type":"about:blank","title":"Not Found","status":404}`, without a
   read error, and the write operation reports no error.
2. **Given** a handler that sets `Content-Encoding: gzip` and then answers with a 404 problem,
   **When** a client requests it through a real server, **Then** the response has no
   `Content-Encoding`, and the client reads the document without a decoding error.
3. **Given** each of `Content-Length`, `Content-Encoding`, `Content-Range`, `ETag`,
   `Last-Modified`, `Content-Disposition` and `Trailer`, set before a problem is written,
   **When** the problem is written, **Then** the response does not contain that header.
4. **Given** the headers `Retry-After: 120`, `Content-Language: it`, `Cache-Control: no-store`,
   `Vary: Accept-Language`, `X-Content-Type-Options: nosniff` and `Content-Type: text/plain`, set
   before a 503 problem is written, **When** the response is sent, **Then** the first five headers
   keep their values, and the media type is `application/problem+json`.
5. **Given** representation headers set before writing a problem that is replaced by the generic
   server error (a problem built with an invalid instance, or an error that is not a problem),
   **When** the response is sent, **Then** the representation headers are removed in that case too.
6. **Given** representation headers set before a problem answers a HEAD request, **When** the
   response is sent, **Then** they are removed, and the response has no body.

---

### User Story 2 - Reading an extension never crashes on an absent problem (Priority: P1)

A developer tests whether an error is an "out of credit" problem, then reads its `balance`
extension. When the error is not that problem, the test yields no problem. A developer who
ignores the test's result, or reads both in one expression, passes that absent problem to the
extension reader. Today the program crashes. It must answer that there is no value.

**Why this priority**: the defect crashes the program on a common, short form of the code, and the
reading example in the README invites that form.

**Independent Test**: read an extension of an absent problem, directly and through the result of a
failed type test, and check the result.

**Acceptance Scenarios**:

1. **Given** an absent problem, **When** its `balance` extension is read, **Then** the result is
   "no value" (the zero value, and not found), and the program does not crash.
2. **Given** a plain error that contains no "out of credit" problem, **When** the developer tests it
   against "out of credit", ignores the result of the test and reads `balance` from the problem
   returned, **Then** the result is "no value".
3. **Given** an absent problem, **When** its validation items are read (User Story 4), **Then** the
   result is "not found", and the program does not crash.

---

### User Story 3 - Add per-request members to a problem built elsewhere (Priority: P2)

The service layer returns an "out of credit" problem, with a detail and a `balance`. It knows
nothing about the HTTP request. The HTTP layer knows the request ID and the trace ID, and wants
them in the document, so that the client can quote them to support. Problems are immutable.
Today the HTTP layer cannot add a member without rebuilding the whole problem, which loses what
the service set. The developer derives a new problem from the one received, with the extra
members. The original does not change.

**Why this priority**: per-request identifiers in error responses are common practice, and the
request names this the main improvement. The module works without it, so it follows the bug
fixes.

**Independent Test**: derive problems from occurrences, from status-only problems, from problems
read from responses and from invalid problems. Write the originals and the derived problems, and
compare the documents and the error inspection results. Repeat from many concurrent tasks with
race detection enabled.

**Acceptance Scenarios**:

1. **Given** an "out of credit" occurrence with a detail, an instance and `balance` = 30, **When**
   the HTTP layer derives a problem with `request_id` = `"req-42"` and writes it, **Then** the
   document has every member of the original plus `request_id`. **When** the original is written
   afterwards, **Then** its document has no `request_id`.
2. **Given** an occurrence with an internal cause, **When** a problem is derived from it, **Then**
   the standard error inspection finds the same cause, by identity and by type, in the derived
   problem, and the derived problem is still an "out of credit" problem for the type test.
3. **Given** one problem shared by many concurrent requests, each deriving a problem with its own
   request ID, **When** the responses are compared, **Then** each one carries only its own ID, the
   original is unchanged, and race detection reports no data race.
4. **Given** a derivation that sets `balance` = 10 on a problem whose `balance` is 30, **When**
   both are read, **Then** the derived problem has 10 and the original still has 30.
5. **Given** an occurrence whose instance was invalid when it was built, **When** a problem is
   derived from it with valid members and written, **Then** the generic server error is sent, and
   the write operation reports the original reason.
6. **Given** a valid occurrence, **When** a problem is derived from it with an invalid member (an
   instance with a space, or an extension value that has no JSON representation), **Then**
   writing the derived problem sends the generic server error and reports why, and the original
   is still written normally.
7. **Given** an absent problem, **When** a problem is derived from it, **Then** the result is an
   absent problem, and the program does not crash.
8. **Given** a problem read from another API's response, **When** a problem is derived from it,
   **Then** the derived problem keeps the resolved type URI and every extension.
9. **Given** a problem, **When** a problem is derived from it with no options, **Then** both
   problems produce byte-identical documents.

---

### User Story 4 - Report validation errors with correct JSON Pointers (Priority: P2)

An API validates a request and finds several errors: the age is not a positive integer, and the
favorite color is not one of the allowed values. RFC 9457 §3 recommends answering with one
problem whose `errors` extension lists the errors. Each error has a `detail` and a `pointer`,
which is a JSON Pointer (RFC 6901) to the offending member. Pointers are easy to get wrong when
member names contain `/`, `~`, spaces or non-ASCII characters. The developer builds each pointer
from the member names and array indexes, and the library escapes them. Clients, including this
library's reading operation, get the items back.

**Why this priority**: every API that validates input needs this, and the RFC defines the shape.
Without this story, each developer declares the same extension by hand and escapes pointers on
their own.

**Independent Test**: build validation problems with items and pointers. Write them, read them back
and compare. Check the built pointers against the RFC 6901 §6 examples, and fuzz the pointer
builder.

**Acceptance Scenarios**:

1. **Given** the validation problem type of RFC 9457 §3 (type
   `https://example.net/validation-error`, title "Your request is not valid.", status 422) and two
   items:
   - "must be a positive integer", pointing to the member `age`;
   - "must be 'green', 'red' or 'blue'", pointing to the member `color` of the member `profile`;

   **When** an occurrence with these items is written, **Then** the document contains
   `"errors":[{"detail":"must be a positive integer","pointer":"#/age"},{"detail":"must be 'green', 'red' or 'blue'","pointer":"#/profile/color"}]`,
   with the items in the given order.
2. **Given** the tokens `items`, `0`, `qty`, **When** a pointer is built, **Then** it is
   `#/items/0/qty`. **Given** no tokens, **Then** it is `#`.
3. **Given** the reference tokens of the RFC 6901 §6 examples, **When** pointers are built,
   **Then** they are exactly the pointers the RFC lists:

   | Tokens | Pointer |
   |--------|---------|
   | (none) | `#` |
   | `foo` | `#/foo` |
   | `foo`, `0` | `#/foo/0` |
   | (one empty token) | `#/` |
   | `a/b` | `#/a~1b` |
   | `c%d` | `#/c%25d` |
   | `e^f` | `#/e%5Ef` |
   | `g\|h` | `#/g%7Ch` |
   | `i\j` | `#/i%5Cj` |
   | `k"l` | `#/k%22l` |
   | (a space) | `#/%20` |
   | `m~n` | `#/m~0n` |

4. **Given** the token `città`, **When** a pointer is built, **Then** it is `#/citt%C3%A0`.
5. **Given** an item without a pointer, for example an error about the request as a whole,
   **When** it is written, **Then** the item has only its `detail` member.
6. **Given** a validation problem written by the library, **When** the response is read back,
   **Then** its items equal the originals, in the same order.
7. **Given** a problem without an `errors` member, or whose `errors` member is a list of strings
   (as another API might send), **When** its validation items are read, **Then** the result is
   "not found", with no error and no crash.
8. **Given** a document from another API whose items have members other than `detail` and
   `pointer`, for example `code`, **When** its validation items are read, **Then** each item has
   its detail and pointer, and the other members stay available through the problem's raw
   extensions.
9. **Given** an occurrence built with both the validation items and an extension that the
   developer declared with the name `errors`, **When** it is written, **Then** the `errors` member
   comes from whichever was applied last. Both orders are tested.
10. **Given** an occurrence built with the validation items option and no items, **When** it is
    written, **Then** the document contains `"errors":[]`.

---

### User Story 5 - The documentation never discards the write error (Priority: P3)

Developers copy the README and the runnable examples. Today every one of them discards the error
that the write operation returns. That error is the only sign that a problem was replaced by the
generic server error because it was built incorrectly. For example, an instance containing a
space silently becomes a 500. Copying the examples hides such bugs, and breaks the constitution's
rule that errors are never discarded silently.

**Why this priority**: the documentation is wrong, but no code misbehaves. It still matters, because
the examples are copied into every project that adopts the module.

**Independent Test**: review the README and the runnable examples, and run the examples.

**Acceptance Scenarios**:

1. **Given** the README, the package documentation and the runnable examples, **When** they are
   searched, **Then** none of them discards the error of the write operation. Each one handles
   it, by logging it.
2. **Given** the README section on answering requests, **When** a developer reads it, **Then** it
   explains that an error wrapping the invalid-problem error means the problem was built
   incorrectly and replaced by the generic server error, which is a bug to fix, and that any other
   error usually means that the client went away while the response was sent.
3. **Given** the README section on the `middleware` module, **When** a developer reads it, **Then**
   the refusal function passes the refusal's error as the internal cause of the problem and
   handles the write error. It still takes at most 5 lines (spec 004, SC-007).

---

### User Story 6 - Translate domain errors into problems (Priority: P3)

Services return domain errors, such as:

- a "not found" sentinel error;
- an "insufficient funds" error type, with the balance and the price;
- errors from a database, whose messages name tables and constraints.

The library deliberately does not map arbitrary errors to problems (spec 004, "Out of scope"), but
every developer writes this translation. The documentation shows the recommended pattern as code
that the developer writes.

**Why this priority**: documentation only. It prevents the most common leak, writing an error's
message into `detail`.

**Independent Test**: run the runnable example of the pattern, and review the README section.

**Acceptance Scenarios**:

1. **Given** the README, **When** a developer reads the section "Translating domain errors",
   **Then** it shows a function that:
   - recognizes domain errors by identity (sentinel errors) and by type;
   - for each one, returns an occurrence of the matching problem type, with the detail and the
     extensions built from the domain error's fields, and the domain error as the internal cause;
   - returns every other error unchanged, which the write operation answers with the generic
     server error.
2. **Given** that section, **When** a developer reads it, **Then** it states the rule that a detail
   never contains an error's message, because the message can reveal internals such as a database
   constraint name, and it shows a wrong and a right example.
3. **Given** the runnable example of the pattern, **When** the tests run, **Then** it compiles and
   its output is checked:
   - an "insufficient funds" error becomes a 403 problem whose detail and extension come from the
     error's fields;
   - an unknown error becomes the generic server error;
   - no part of the domain errors' messages, such as the constraint name, appears in any
     response.

### Edge Cases

- **Header with several values**: a representation header set with several values (for example
  two `Trailer` lines) is removed with all its values.
- **Correct `Content-Length`**: a `Content-Length` equal to the length of the problem document is
  removed too. The server computes the length of the body it sends.
- **Response already started**: once the status has been sent, headers can no longer be removed.
  The write operation's documentation already says to call it before writing anything else.
- **Derivation and identity**: a derived problem is a distinct occurrence. Comparing it by
  identity with the original gives "not equal". The type test, and inspection of the original's
  cause, give the same results as on the original (FR-006).
- **Derivation that replaces the cause**: an internal cause given to the derivation replaces the
  original's cause in the derived problem. The last option wins, as for every member.
- **Two failures**: when the original has a recorded failure and the derivation adds another,
  writing the derived problem reports the original's failure, which is the first one recorded.
- **Absent options**: absent (nil) options passed to the derivation are ignored, as when a problem
  is built.
- **Pointer tokens with escape sequences**: the token `~1` becomes `~01`, and decodes back to
  `~1`, not to `/`. The token `/` becomes `~1`.
- **Pointer tokens with URI delimiters**: `#` and `%` are percent-encoded. `?`, `:` and `@` are
  allowed in a URI fragment and stay as they are.
- **Pointer tokens that are not valid UTF-8**: each byte is percent-encoded, and decoding the
  pointer gives back the same bytes.
- **Pointer to an array element**: an index is a token like any other (`"0"`). The builder does
  not interpret tokens, so `-` (the element after the last, RFC 6901 §4) passes through
  unchanged.
- **Item with an empty detail**: it is written with `"detail":""`. The detail member is always
  present, as in RFC 9457 §3.
- **Pointers written by hand**: a pointer that the developer writes in an item without the builder
  is written exactly as given. The library does not check it.
- **Validation items with mistyped members**: when one item of a document read has a detail that
  is not a string, the whole `errors` member does not decode as validation items, and reading them
  gives "not found", as reading any extension does when the value does not decode.
- **Exact names in validation items**: item members are matched by their exact names, as the
  standard members are. `Detail` or `POINTER` are other members, and are ignored. An `errors`
  member that is null, or that contains a null element, gives "not found".

## Requirements *(mandatory)*

### Functional Requirements

#### Writing responses (B1)

- **FR-001**: Before writing a problem, the write operation MUST remove from the response the
  headers that describe a previous representation, with all their values: `Content-Length`,
  `Content-Encoding`, `Content-Range`, `ETag`, `Last-Modified`, `Content-Disposition` and
  `Trailer`. This is the list that the repository's `recovery` middleware removes. The removal
  applies to every response the write operation sends: occurrences, status-only problems, the
  generic server error that replaces an invalid problem or answers an error that is not a problem,
  and responses to HEAD requests.
- **FR-002**: Every other header set before writing MUST be kept with its values, in particular
  `Retry-After`, `Content-Language`, `Cache-Control`, `Vary` and security headers. The media type
  is still always `application/problem+json`.
- **FR-003**: The documentation of the write operation, and the package documentation, MUST list
  the removed headers, and state that every other header is kept.

#### Reading extensions (B2)

- **FR-004**: Reading an extension of an absent problem MUST return the zero value and "not found",
  without crashing. The same holds for reading validation items (FR-016).

#### Deriving problems (I1)

- **FR-005**: Developers MUST be able to derive a new problem from an existing one, applying, in
  order, any of the options used to build a problem. For the same member, the last option wins.
  The original problem MUST NOT change.
- **FR-006**: The derived problem MUST carry every member of the original (type, title, status,
  detail, instance and extensions) and its internal cause, except those that an option replaces.
  Unless an option replaces the cause, the standard error inspection (by identity, by type and by
  unwrapping) MUST find the original's cause in the derived problem, as it does in the original,
  and the type test MUST match the derived problem exactly when it matches the original.
- **FR-007**: The derived problem MUST own its extension values: no change made through one problem
  can affect the other. Deriving problems from the same problem in many concurrent tasks MUST be
  safe, and free of data races.
- **FR-008**: A failure recorded while the original was built MUST carry over to the derived
  problem: writing it sends the generic server error and reports the original's reason. A failure
  caused by an option of the derivation MUST be recorded on the derived problem only.
- **FR-009**: Deriving from an absent problem MUST give an absent problem, without crashing.
- **FR-010**: Deriving MUST work on every problem, including one read from another API. It keeps
  the resolved type URI and every extension.

#### Validation errors (I2)

- **FR-011**: Developers MUST be able to set the `errors` extension of a problem to a list of
  validation items, with one option. An item has a detail, which is always written, and a pointer,
  which is written only when it is not empty. Items are written in the given order, with `detail`
  before `pointer`. With no items, the member is an empty list, never null.
- **FR-012**: Developers MUST be able to build a JSON Pointer in URI fragment form from a sequence
  of reference tokens. The builder escapes `~` as `~0` and `/` as `~1` in each token (RFC 6901
  §3). It then percent-encodes every character that a URI fragment does not allow (RFC 3986),
  including `%` and every byte of a non-ASCII character in UTF-8 (RFC 6901 §6). With no tokens,
  the pointer is `#`.
- **FR-013**: For any sequence of tokens, including tokens that are not valid UTF-8, the built
  pointer MUST be a valid URI reference made only of ASCII characters. Decoding it as RFC 6901 §6
  describes MUST give back exactly the same tokens.
- **FR-014**: For the reference tokens of the RFC 6901 §6 examples, the builder MUST produce
  exactly the pointers that the RFC lists (User Story 4, scenario 3).
- **FR-015**: The library MUST write the pointers of items exactly as the developer gives them. It
  does not check or change a pointer written without the builder.
- **FR-016**: Developers MUST be able to read the validation items of a problem, including one
  read from another API. The result MUST be "not found" when the problem is absent, has no
  `errors` member, or has an `errors` member that does not decode as a list of items. Members of an
  item other than `detail` and `pointer` are ignored. Reading MUST never crash.
- **FR-017**: The `errors` extension member MUST be reserved for validation items. Declaring an
  extension named `errors` stays accepted, and the documentation MUST state that the validation
  items option owns this member. When both set it on the same problem, the last one applied wins,
  as for every other member.
- **FR-018**: Validation items written by the library and read back MUST equal the originals, in
  the same order.

#### Documentation (I3, I4)

- **FR-019**: No example in the README, in the package documentation or among the runnable
  examples may discard the error of the write operation. Each one MUST handle it, by logging it.
- **FR-020**: The README MUST explain the error of the write operation:
  - an error that wraps the invalid-problem error means that the problem was built incorrectly
    and was replaced by the generic server error, which is a bug to fix, for example an instance
    containing a space;
  - any other error usually means that the client went away while the response was sent.
- **FR-021**: In the README section on the `middleware` module, the refusal function MUST pass the
  refusal's error as the internal cause of the problem, and handle the write error, in at most 5
  lines.
- **FR-022**: The README MUST have a section "Translating domain errors" that shows, as code the
  developer writes, a function that:
  - recognizes domain errors by identity and by type;
  - returns an occurrence of the matching problem type, with the detail and extensions built from
    the domain error's fields, and the domain error as the internal cause;
  - returns every other error unchanged, which the write operation answers with the generic server
    error.
- **FR-023**: The section MUST state the rule that a detail never contains an error's message,
  because the message can reveal internals such as database constraint names. It MUST show a wrong
  and a right example.
- **FR-024**: The translation pattern MUST also exist as a runnable example whose output is checked
  by the tests.

#### Examples, changelog and tests

- **FR-025**: Each new capability MUST have a runnable example, including:
  - deriving a problem that adds a `request_id` extension when the problem is written;
  - a validation function that returns a 422 problem with two items.
- **FR-026**: The module's changelog MUST record, under "Unreleased":
  - under "Fixed": the removal of representation headers, stating that these headers were kept
    before and are removed now, and the crash when reading an extension of an absent problem;
  - under "Added": deriving problems, validation items, the pointer builder and reading validation
    items.
- **FR-027**: Each fix MUST have a regression test that fails without the fix:
  - for the header fix, tests through a real in-process server and client for `Content-Length`
    and `Content-Encoding`, in addition to recorded responses;
  - a table test showing that each header of FR-001 is removed, and that `Retry-After` and
    `Content-Language` are kept. The existing test of kept headers is adapted to this behavior.

  The pointer builder MUST have a fuzz test of FR-013. Concurrent derivation MUST be tested with
  race detection enabled.

### Key Entities

- **Representation header**: a header that describes one specific body: its length, encoding,
  range, validators (`ETag`, `Last-Modified`), disposition or trailers. It is meaningless for the
  problem document that replaces that body.
- **Derived problem**: a new problem built from an existing one plus options. It owns its values,
  and it keeps the original's type and internal cause unless an option replaces them.
- **Validation item**: one error of a validation problem. It has a detail, which is text for the
  client, and an optional pointer to the offending member of the request.
- **JSON Pointer (URI fragment form)**: a reference to a member of a JSON document, written as `#`
  followed by escaped reference tokens, for example `#/items/0/qty`.
- **Reference token**: one step of a JSON Pointer: a member name or an array index, as text.
- **Domain error**: an error that the developer's own code defines, such as a sentinel or a typed
  error. The developer translates it into a problem.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: For each of the 7 representation headers, set alone and all together before a
  problem is written, a standard HTTP client receives the complete problem document through a real
  server, with 0 read or decoding errors in 100% of the cases.
- **SC-002**: 100% of the headers outside that list that were set before writing reach the client
  with their values.
- **SC-003**: Reading an extension or the validation items of an absent problem, and deriving from
  an absent problem, cause 0 crashes.
- **SC-004**: Adding a per-request member to a problem received from another layer takes 1 call.
  Across at least 100 concurrent derivations from one problem, each response carries only its own
  member, the original's document is byte-identical before and after, and race detection reports
  0 data races.
- **SC-005**: A fuzzing session of at least 10 minutes on the pointer builder finds 0 inputs whose
  pointer is not a valid URI reference, or does not decode back to the original tokens. 100% of
  the RFC 6901 §6 examples match.
- **SC-006**: 100% of the validation problems written by the library and read back give the same
  items, in the same order.
- **SC-007**: The README, the package documentation and the runnable examples contain 0
  discarded write errors.
- **SC-008**: In the example of translating domain errors, 0 bytes of the domain errors' messages,
  including the constraint name, appear in any response.
- **SC-009**: 100% of the acceptance scenarios and edge cases of this spec are covered by automated
  tests. Statement coverage of the module is at least 80%. The API compatibility check against
  `problem/v0.1.0` reports only additions.

## Assumptions

- **Release**: the changes ship in the next minor release of the module (0.2.0). It adds
  capabilities and changes the behavior of the write operation, which is allowed in a minor
  release while the module is at 0.x. The plan confirms the level with the API compatibility
  check.
- **In-process test server**: a test HTTP server running inside the test process, on the loopback
  interface, does not count as network access in the sense of the constitution (Principle III).
  It reaches nothing outside the machine and stays hermetic and deterministic. The request asks
  for it, because a recorded response cannot reproduce the failures of User Story 1.
- **Fixed header list**: the list of removed headers is fixed and matches the `recovery`
  middleware. No option changes it, because no current use needs one.
- **Item shape**: the detail of an item is always written, even when empty. An item's pointer is
  free text, and the pointer builder is the recommended way to produce it.
- **Reading validation items**: the whole `errors` member decodes as a list of items or gives
  "not found". This matches how reading any extension behaves (spec 004).
- **Out of scope**: the following are not part of this feature. The pull request description
  lists them as possible follow-ups:
  - a mapper or `From` operation in the library;
  - making a problem type match `about:blank` problems by status code;
  - reading `application/json` responses that contain problem details.

## Appendix: original request

```text
Task: fix bugs and improve the problem module

Bugs
B1. Leftover representation headers corrupt the problem response

Write keeps every header already set on w except Content-Type (research.md, step 3 of Write; FR-014). Headers that describe the previous representation then contradict the problem document. Reproduced against a real httptest.Server:

go
// Content-Length set before the error: the client gets status 404, an empty body and
// "unexpected EOF"; Write returns "http: wrote more than the declared Content-Length".
w.Header().Set("Content-Length", "5")
err := problem.Write(w, r, problem.Status(http.StatusNotFound))

// Content-Encoding left by a handler serving precompressed files: the JSON is sent in
// clear text with "Content-Encoding: gzip", and the client fails with "gzip: invalid header".
w.Header().Set("Content-Encoding", "gzip")
_ = problem.Write(w, r, problem.Status(http.StatusNotFound))

Fix: before writing, delete Content-Length, Content-Encoding, Content-Range, ETag, Last-Modified, Content-Disposition and Trailer (the list the recovery middleware already removes, see middleware/recovery/recovery.go). Keep every other header, in particular Retry-After, Content-Language, Cache-Control, Vary and security headers.

Regression tests with httptest.NewServer, not only a recorder, for Content-Length and Content-Encoding; a table test proving each listed header is removed and that Retry-After and Content-Language survive. Adapt TestWrite_HeadersKept.
CHANGELOG under Fixed, stating the behavior change; update the Write doc comment.
B2. Key.Lookup(nil) panics

Key[T].Lookup dereferences its *Error argument, so Balance.Lookup(p) with a nil p (for example after ignoring the ok of Type.Match) panics with a nil pointer dereference. It must return the zero value and false.

Improvements
I1. Derive a problem with more members: (*Error).With

*Error is immutable and cannot be copied, so a problem built in the service layer cannot receive per-request members (request id, trace id) when the HTTP layer writes it. Add:

go
// With returns a copy of e with the options applied in order. e is not modified.
func (e *Error) With(opts ...Option) *Error
The copy owns its extension map (deep copy of the raw values); concurrent With calls on the same *Error are race-free (test with -race).
A failure already recorded on e is carried over; a new failure from opts is recorded on the copy only. With on a nil *Error returns nil.
The cause is kept, so errors.Is, errors.As and Type.Match behave the same on the copy.
Example: add a request_id extension at write time.
go
  var RequestID = problem.Must(problem.NewKey[string]("request_id"))

  func writeProblem(w http.ResponseWriter, r *http.Request, err error) {
  	if p, ok := errors.AsType[*problem.Error](err); ok {
  		if id, ok := requestIDFrom(r.Context()); ok {
  			err = p.With(problem.WithExtension(RequestID, id))
  		}
  	}
  	if werr := problem.Write(w, r, err); werr != nil {
  		slog.ErrorContext(r.Context(), "write problem", "err", werr)
  	}
  }
I2. Validation errors: Item, WithErrors and Pointer

RFC 9457 §3 shows several errors of the same type reported in an errors extension of {detail, pointer} objects, where pointer is a JSON Pointer (RFC 6901) to the offending member. Every API that validates input needs this, and JSON Pointers are easy to get wrong.

go
// Item is one error of a validation problem.
type Item struct {
	Detail  string `json:"detail"`
	Pointer string `json:"pointer,omitempty"`
}

// WithErrors sets the "errors" extension member to items.
func WithErrors(items ...Item) Option

// Pointer returns the JSON Pointer, in URI fragment form, to the member reached through
// the given reference tokens: Pointer("items", "0", "qty") is "#/items/0/qty".
func Pointer(tokens ...string) string

// Errors returns the "errors" extension member of e, decoded.
func (e *Error) Errors() ([]Item, bool)
Pointer escapes ~ as ~0 and / as ~1 (RFC 6901 §3), then percent-encodes what a URI fragment does not allow (RFC 6901 §6); Pointer() with no tokens is "#". Fuzz it: the result must always be a valid URI reference, and decoding it must give the tokens back.
errors is a reserved extension name from now on: NewKey keeps accepting it, but document that WithErrors owns it, and decide (and test) which wins when both are used: the last option applied, as for every other member.
Example: a validation function returning a 422 problem with two items.
I3. Documentation: never discard Write's error

Every README example uses _ = problem.Write(...), but that error is how Write reports a problem it replaced with a generic 500 (it wraps ErrInvalid): an instance containing a space silently becomes a 500. The constitution forbids discarding errors silently.

Handle the error in the README and in the examples: log it, and explain that ErrInvalid means a bug in how the problem was built, while other errors usually mean the client went away.
In "Using it with the middleware module", keep the internal cause for logs: problem.Status(rej.Status, problem.WithCause(rej.Err)).
I4. Documentation: translating domain errors

Mapping arbitrary errors stays out of scope for the library (spec, "Out of scope"), but every user has to do it, so add a README section "Translating domain errors" with the recommended pattern as user code:

a function that checks errors.Is and errors.AsType against domain errors and returns SomeType.New(problem.WithDetail(...), problem.WithCause(err)), and otherwise returns err unchanged, which Write answers with the generic 500;
typed data for WithDetail and extensions comes from the domain error's fields;
the rule that WithDetail must never contain err.Error(), which would leak internals such as database constraint names, with a wrong-and-right example.

Add it also as an Example so it is compiled and tested.

Not in this task

Do not implement these; list them in the PR description as possible follow-ups:

a mapper or From API in the library;
making Type match about:blank problems by status code;
reading application/json responses that contain problem details.
```

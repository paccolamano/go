# Feature Specification: RFC 9457 Problem Details Module

**Feature Branch**: `004-problem-details` (suggested; not yet created)

**Created**: 2026-10-18

**Status**: Draft

**Input**: User description: "Implementiamo un nuovo modulo chiamato problem e che implementi l'rfc9457."

## Context

When an HTTP API fails a request, the status code tells the client the class of failure. It does
not say which problem occurred, why, or what the client can do about it. Each API therefore
invents its own error body, and every client has to learn the shape of each API. RFC 9457
("Problem Details for HTTP APIs", July 2023, which obsoletes RFC 7807) standardizes that body. It
is a small document with five standard members (`type`, `title`, `status`, `detail`,
`instance`), plus extension members defined by each problem type, served with its own media type.

This feature delivers a new, independent module in this repository (Constitution, Principle I),
named `problem`. It provides:

- **problem types**: the type URI, title and status code that RFC 9457 requires every problem
  type to document, declared once and checked when declared;
- **responses**: an occurrence of a problem written as an HTTP response that follows the RFC,
  including the generic `about:blank` type;
- **error integration**: problems travel through the program as errors, and any error can be
  answered with a problem document that reveals nothing the developer did not put in it;
- **reading**: problem documents returned by other APIs are read with the RFC's rules for
  consumers.

The Go language, the module layout and the standard library baseline are given by the request
and by the constitution (v1.2.0). They are not design choices made here. Member names, media
types, HTTP status codes and header names appear in this spec because they are the observable
contract of RFC 9457. How the module is built is left to `/speckit-plan`.

**Actors**:

- **Developer**: builds an HTTP API and answers failed requests with problem details, or calls
  other APIs and reads the problem details they return.
- **Client**: the program that receives a problem document and acts on it: it retries, shows a
  message or branches on the problem type.
- **Maintainer**: evolves the library and publishes its releases.

### RFC 9457 coverage

Section numbers refer to RFC 9457.

| RFC 9457 provision | Decision | Requirements |
|--------------------|----------|--------------|
| Standard members `type`, `title`, `status`, `detail`, `instance` (§3.1) | Implement | FR-004, FR-012 |
| An absent `type` means `about:blank` (§3.1.1) | Implement, when writing and when reading | FR-005, FR-023 |
| Absolute type URIs are recommended; relative ones include the full path (§3.1.1) | Enforce when a type is declared | FR-008 |
| Generators MUST send the same status code in the response (§3.1.2) | Guarantee | FR-011, FR-012 |
| The `title` does not change between occurrences, except for localization (§3.1.3) | The title comes from the type; it can be replaced for localization | FR-009 |
| The `about:blank` title SHOULD be the status phrase, and MAY be localized (§4.2.1) | Default the title, allow a replacement | FR-005 |
| Extension members (§3.2) and their name syntax: a letter first, then letters, digits or `_`, three characters or more | Implement, and enforce the syntax when producing | FR-004, FR-006 |
| Consumers MUST ignore a member whose value has the wrong type (§3.1) | Implement | FR-023 |
| Consumers MUST ignore extensions they do not recognize (§3.2) | Implement: extensions are kept and accessible, never an error | FR-024 |
| The type URI is the problem type's primary identifier (§3.1.1) | Implement | FR-021 |
| Relative `type` and `instance` are resolved against the base URI (§3.1.1, §3.1.5) | Implement when reading a response | FR-025 |
| Type definitions MUST document their type URI, title and status code (§4) | Required when a type is declared | FR-008 |
| Type definitions MAY specify the `Retry-After` header (§4) | Headers set by the developer are kept | FR-014 |
| Several problems: send the most relevant one; problems of one type go in an extension list (§3) | Supported through extension values; the choice stays with the developer | FR-004 |
| Problem details are not a debugging tool; avoid exposing implementation details (§4, §5) | Guarantee for every error that is not the problem being written | FR-019, FR-020 |
| JSON format, media type `application/problem+json` (§3, §6) | Implement | FR-011 |
| Embedding in other formats (Appendix C) | Out of scope: the RFC makes no recommendation | — |
| The registry of problem types (§4.2) | Only `about:blank` is used; registering types is out of scope | — |

### Clarifications

#### Session 2026-10-18

- Q: Should the module also read problem details returned by other APIs, or only produce them?
  → A: Produce and read. Reading follows the RFC's rules for consumers (FR-022 to FR-028; User
  Story 4).

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Answer a failed request with a standard problem document (Priority: P1)

A developer's handler cannot serve a request, and it answers with a problem document in one call.
It sends either a status code alone, when the status says it all, or an occurrence of a problem
type that the API documents, with details specific to this request. Any RFC 9457 client
understands the response, and generic HTTP software (proxies, caches, monitors) sees the right
status code.

**Why this priority**: this is the purpose of the RFC, and every other story builds on the
document this story produces.

**Independent Test**: serve requests in memory through handlers that answer with a status-only
problem and with an occurrence of a declared type. Check the status codes and headers, and check
the documents against the RFC's JSON schema and the expected members.

**Acceptance Scenarios**:

1. **Given** a handler that answers with status 404 alone, **When** a GET request is served,
   **Then** the response has status 404 and media type `application/problem+json`, and the
   document is `{"type":"about:blank","title":"Not Found","status":404}`, with its members in the
   documented order.
2. **Given** the problem type "out of credit" (type `https://example.com/probs/out-of-credit`,
   title "You do not have enough credit.", status 403), **When** a handler answers with an
   occurrence that adds:
   - the detail "Your current balance is 30, but that costs 50.";
   - the instance `/account/12345/msgs/abc`;
   - the extension `balance` = 30;
   - the extension `accounts` = `["/account/12345", "/account/67890"]`;

   **Then** the response has status 403, and the document contains the five standard members and
   both extensions, with `balance` as a number and `accounts` as a list of strings.
3. **Given** a status-only problem for 404 whose title is replaced with "Risorsa non trovata",
   **When** it is written, **Then** the document carries that title.
4. **Given** a handler that sets `Retry-After: 120`, `Content-Language: it` and
   `Content-Type: text/plain` before answering with a 503 problem, **When** the response is sent,
   **Then** it keeps the first two headers, and its media type is `application/problem+json`.
5. **Given** a validation problem with an `errors` extension that lists objects, each with a
   `detail` and a `pointer` member, **When** it is written, **Then** the document carries the
   list with the same nesting, order and values.
6. **Given** a HEAD request answered with a problem, **When** the response is sent, **Then** it
   has the problem's status and headers, and no body.
7. **Given** the same problem written twice, or written from many concurrent requests, **When**
   the documents are compared, **Then** they are byte-identical.

---

### User Story 2 - Return problems through error handling without leaking internals (Priority: P1)

The code that detects a problem is rarely the handler. A service or repository layer finds that
the account is out of credit, and the error travels up through several layers, each adding
context. The developer wants to return the problem as an error from any layer, and the handler to
answer with whatever error it received. Errors that are not problems must never reach the client:
their messages can contain query text, file paths or credentials.

**Why this priority**: in idiomatic Go code, handlers receive errors, not response documents.
Without this story, each handler maps errors by hand. The easy mistake, writing the error's
message into `detail`, is exactly the exposure that RFC 9457 §5 warns against.

**Independent Test**: have a stub service return problems, wrapped problems, combined errors and
plain errors. Let a handler answer each error with one call, then inspect the responses and the
errors.

**Acceptance Scenarios**:

1. **Given** an occurrence of "out of credit" that two layers wrap with the context
   "charge message" and then "send message", **When** the handler answers with the resulting
   error, **Then** the response is that occurrence, and neither "charge message" nor
   "send message" appears in the response.
2. **Given** a plain error whose message is `pq: password authentication failed for user "admin"`,
   **When** the handler answers with it, **Then** the response is the generic server error:
   status 500, type `about:blank`, title "Internal Server Error", and no other member. No part of
   the error's message appears in the response.
3. **Given** an occurrence of a "service unavailable" type that carries an internal cause (the
   database error that triggered it), **When** the handler answers with it, **Then** the response
   is the occurrence alone, and the standard error inspection still finds the cause, for example
   to log it.
4. **Given** an error that combines a plain error and an occurrence of "out of credit", **When**
   the handler answers with it, **Then** the response is that occurrence: the first problem found
   in the order that the standard error inspection uses.
5. **Given** the errors of the previous scenarios, **When** the developer asks whether each one is
   an "out of credit" problem, **Then** the answer is yes exactly for the errors whose tree
   contains an occurrence of that type, wrapped or not.
6. **Given** an occurrence of "out of credit", **When** its message is read, for example by a
   log, **Then** it contains the title and the detail.

---

### User Story 3 - Declare a catalog of problem types and catch mistakes early (Priority: P2)

An API's documentation lists the problem types the API can return. The developer declares each
one once, typically as a package-level value next to its documentation, and creates occurrences
of it wherever the problem is detected. Some mistakes would produce non-compliant documents:

- a type that is not a usable URI;
- a status that is not an error;
- a missing title;
- an extension name that collides with a standard member or breaks the RFC's syntax.

These mistakes are reported when the type or the extension name is declared. They are never
discovered by a client in production.

**Why this priority**: RFC 9457 §4 requires every problem type to document its type URI, title
and status code, and §3.1.3 asks that the title stay the same across occurrences. A catalog keeps
both consistent. It builds on Story 1.

**Independent Test**: declare valid and invalid problem types and extension names, and write
occurrences with valid and invalid instances. Check the reported errors, the contents of the
occurrences and the responses. Use one declaration from many concurrent tasks.

**Acceptance Scenarios**:

1. **Given** the declared "out of credit" type, **When** two occurrences are created with
   different details and balances, **Then** both carry the type's URI, title and status, and the
   declaration is unchanged.
2. **Given** a declaration whose type URI is `out-of-credit` (relative, without the full path),
   malformed or `about:blank`, whose title is empty, or whose status is 200, 302 or 600, **When**
   it is declared, **Then** it is refused with an error that names the invalid member. The type URIs
   `https://example.com/probs/out-of-credit`, `/probs/out-of-credit`,
   `tag:example.com,2026:out-of-credit` and `urn:example:out-of-credit` are accepted.
3. **Given** the extension names `type`, `title`, `status`, `detail`, `instance` and `Status`,
   **When** each is declared, **Then** it is refused. **When** `id` (too short), `user-id`
   (hyphen), `2fa` (digit first) or `_meta` (underscore first) is declared, **Then** it is
   refused. **When** `balance`, `retry_after` or `errors` is declared, **Then** it is accepted.
4. **Given** an occurrence whose instance is not a valid URI reference, for example `/a b`,
   **When** it is written, **Then** the generic server error is sent, and the developer is told
   why.
5. **Given** one declared type used by many concurrent tasks to create and write occurrences,
   **When** the responses are compared, **Then** every response is correct, and no occurrence
   affects another.

---

### User Story 4 - Read problem details returned by other APIs (Priority: P2)

A developer's service calls another API and needs to understand its failures: branch on the
problem type, read the documented extensions (a balance, a retry hint) and log the detail. The
other API may be any RFC 9457 implementation, possibly a sloppy one. A proxy in between may even
answer with an HTML page instead.

**Why this priority**: RFC 9457 sets requirements for consumers too (§3.1, §3.2). A client that
crashes, or rejects a whole document because of one mistyped member, defeats the purpose of the
format. This story reuses the problem representation of Stories 1 to 3.

**Independent Test**: feed recorded responses, with no network access, to the reading operation:
correct, sloppy, malformed and oversized documents, and responses that are not problem documents.
Check the results.

**Acceptance Scenarios**:

1. **Given** a 403 response carrying the "out of credit" document of Story 1, scenario 2,
   **When** it is read, **Then** its type, title, status, detail, instance and both extensions are
   available, with `balance` as a number and `accounts` as a list of strings, and the problem
   matches the declared "out of credit" type.
2. **Given** a document whose `status` is the string `"403"` and whose `title` is the number 42,
   **When** it is read, **Then** both members are treated as absent, the other members are read
   normally, and no error is reported.
3. **Given** a document without `type`, **When** it is read, **Then** its type is `about:blank`.
4. **Given** a response to a request for `https://api.example.com/v1/messages`, whose document has
   the type `/probs/out-of-credit` and the instance `/account/12345/msgs/abc`, **When** it is
   read, **Then** the type is `https://api.example.com/probs/out-of-credit` and the instance is
   `https://api.example.com/account/12345/msgs/abc`.
5. **Given** a document with extensions that the developer does not know, **When** it is read,
   **Then** reading succeeds, and every extension is available with its value.
6. **Given** a 502 response with media type `text/html`, **When** it is read, **Then** it is
   reported as not being a problem document, and its body is left unread for the developer.
7. **Given** a body larger than the documented maximum, a body that is not valid JSON, or valid
   JSON that is not an object, **When** it is read, **Then** an error is reported, and no more
   than the documented maximum is read.
8. **Given** a response with status 503 whose document says `"status": 500`, **When** it is read,
   **Then** both values are available, and the library does not choose between them.
9. **Given** any problem written by the library (Story 1), **When** the response is read back,
   **Then** the result equals the original: same members, same extension values.

### Edge Cases

- **Invalid status in a status-only problem**: a status that is missing or outside 400–599 (for
  example 0 or 200) makes the library send the generic server error instead, and the developer is
  told (FR-016).
- **Status without a phrase**: an `about:blank` problem whose status has no recommended phrase
  (for example 499), and whose title the developer did not set, has no `title` member.
- **Value that cannot be represented**: an extension value that has no JSON representation (for
  example an infinite number) makes the library send the generic server error instead, and the
  developer is told. A partial document is never sent (FR-016).
- **Response already started**: once a status has been sent, the library cannot change it. The
  package documentation states this.
- **Write failures**: a client that disconnects while the document is sent makes the write
  operation report the failure. It never crashes the program.
- **Empty values**: an empty `detail` or `instance` is omitted. An extension whose value is an
  empty string, zero, false, null, an empty list or an empty object is written with that value.
- **Special characters**: non-ASCII text (accents, emoji), quotes and markup such as `<script>`
  in titles, details and extension values are preserved exactly and survive a round trip.
- **No error**: answering with no error (nil) sends the generic server error, so that the
  request never appears successful, and the developer is told.
- **One type URI, two declarations**: the type URI is the identifier (§3.1.1), so two
  declarations with the same URI are the same problem type for every type test.
- **Mistyped standard member when reading**: a standard member whose value has the wrong type is
  ignored. It does not become an extension either.
- **Capitalization when reading**: member names are case-sensitive, so `TYPE` or `Status` in a
  document read are extensions, not standard members.
- **Status member when reading**: a `status` that is not a whole number between 100 and 599 is
  ignored.
- **Duplicate member names when reading**: the document is read deterministically, and the
  documentation states which occurrence is kept.
- **Deeply nested extension values when reading**: they are bounded by the documented maximum
  size, and never crash the program.
- **Media type spelling when reading**: `Application/Problem+JSON; charset=utf-8` is recognized as
  a problem document.
- **Empty document when reading**: `{}` is a valid problem of type `about:blank` with no other
  member.
- **Concurrent use**: declared types and occurrences are used, written and read from many
  concurrent tasks at once, with identical results and no interference (FR-002).

## Requirements *(mandatory)*

### Functional Requirements

#### Module and shared conventions

- **FR-001**: The library MUST be delivered as one independent module in this repository, in the
  directory `problem`, whose package is also named `problem`. It MUST NOT depend on third-party
  modules or on other modules of this repository.
- **FR-002**: Declared problem types and occurrences MUST be immutable once created, and safe to
  use, write and read from concurrent tasks. The library MUST NOT keep a process-wide registry of
  problem types or any global configuration. It MUST NOT write to any output or log on its own.
- **FR-003**: The package documentation MUST explain:
  - how to declare problem types, and what RFC 9457 expects of their type URIs and documentation;
  - how to answer a request with a problem, with a status alone and with any error;
  - which information never reaches the client (FR-020);
  - how to read problem details returned by other APIs;
  - how to answer the refusals of the repository's `middleware` module with problem details.

  Every exported capability MUST have a runnable example.

#### Problems

- **FR-004**: A problem MUST carry the five standard members and any number of extension members.
  An extension value MUST be able to be any JSON value: a string, a number, a boolean, null, a
  list or an object, nested to any depth. This lets a developer report several problems of one
  type as a list in a single extension, as RFC 9457 §3 suggests.
- **FR-005**: When the developer does not give a type, the problem's type MUST be `about:blank`.
  The title of an `about:blank` problem MUST default to the recommended phrase of its status code
  ("Not Found" for 404). When the status code has no recommended phrase, the title is absent. The
  developer MUST be able to replace the title, for example to localize it.
- **FR-006**: Developers MUST declare an extension name before an occurrence can use it. The
  library MUST refuse a name that is one of the standard member names, in any capitalization, or
  that does not follow the syntax of RFC 9457 §3.2: a letter first, then only letters, digits or
  `_`, for three characters or more. The refusal MUST be reported to the developer when the name
  is declared. Capitalization is ignored because some clients match member names without regard
  to case.
- **FR-007**: The library MUST refuse an instance that is not a valid URI reference. Because an
  instance is computed for each request, the refusal happens when the occurrence is written: the
  library sends the generic server error instead and reports the reason (FR-016).

#### Problem types

- **FR-008**: Developers MUST be able to declare a problem type with its three mandatory elements
  (RFC 9457 §4):
  - a type URI, which is either an absolute URI or a reference with the full path (beginning with
    `/`), and is not `about:blank`, which status-only problems use (FR-010);
  - a title, which is not empty;
  - a status code between 400 and 599.

  A declaration that breaks any of these rules MUST be refused when it is made, with an error
  that names the invalid element.
- **FR-009**: Developers MUST be able to create occurrences of a declared type, adding a detail,
  an instance, extensions and an internal cause (FR-018). An occurrence carries the type URI,
  title and status of its type. The title can be replaced for localization only, as RFC 9457
  §3.1.3 allows, and the documentation MUST say so. Creating occurrences MUST NOT change the
  declaration.
- **FR-010**: Developers MUST be able to answer a request with a status code alone, in one
  operation. This produces an `about:blank` problem (FR-005).

#### Writing responses

- **FR-011**: Developers MUST be able to write a problem as the HTTP response in one operation.
  The response status code MUST be the problem's status. The media type MUST be exactly
  `application/problem+json`. The body is the problem document.
- **FR-012**: The document MUST always contain `type` and `status`, and `title` when the problem
  has one. It MUST contain `detail` and `instance` when they are not empty, and every extension.
  The `status` member MUST always equal the status code of the response.
- **FR-013**: Writing MUST be deterministic: the members appear in a documented order, and the
  same problem always produces a byte-identical document.
- **FR-014**: Headers that the developer set before writing (for example `Retry-After` or
  `Content-Language`) MUST be kept. The only exception is the media type, which is always the
  problem media type (FR-011).
- **FR-015**: The response to a HEAD request MUST carry the problem's status and headers, and no
  body.
- **FR-016**: The whole document MUST be produced before anything is sent. If the problem cannot
  be represented (an extension value without a JSON representation, or an instance that is not a
  valid URI reference), or its status is missing or outside 400–599, the library MUST send the
  generic server error (FR-019) instead and report the reason to the developer. A failure to send the response MUST be reported to the developer. No
  input may crash the program.

#### Error integration

- **FR-017**: A problem MUST be usable as an error: it can be returned, wrapped and combined with
  the language's standard error handling, and the standard type inspection finds it through any
  number of levels. Its message MUST contain its title and, when present, its detail.
- **FR-018**: An occurrence MAY carry an internal cause. The standard error inspection (by
  identity, by type and by unwrapping) MUST find the cause, so that it can be logged. The cause
  MUST NOT be written in the response.
- **FR-019**: Developers MUST be able to answer a request with any error, in one operation:
  - when the error's tree contains a problem, the library writes the first problem found, in the
    order of the standard type inspection;
  - otherwise, and when there is no error (nil), the library writes the generic server error:
    status 500, type `about:blank`, title "Internal Server Error", and no other member.
- **FR-020**: No text from an error other than the problem being written may appear in the
  response. This covers errors that are not problems, the context added by wrapping levels, and
  internal causes.
- **FR-021**: Developers MUST be able to test whether an error, or any error in its tree, is a
  problem of a given declared type. Two problems are of the same type when their type URIs are
  equal. For a problem read from a response, the URI after resolution (FR-025) is used.

#### Reading problem details

- **FR-022**: Developers MUST be able to tell whether an HTTP response is a problem document from
  its media type, `application/problem+json`, ignoring case and parameters, without reading its
  body.
- **FR-023**: Developers MUST be able to read a problem from a response or from a document. A
  standard member whose value has the wrong type MUST be treated as absent (RFC 9457 §3.1), and it
  does not become an extension. A `status` is valid only as a whole number between 100 and 599. A
  missing or invalid `type` means `about:blank`. An `instance` that is not a valid URI reference
  is treated as absent.
- **FR-024**: Every extension of a document read MUST be kept and accessible with its value.
  Unknown extensions MUST NOT cause a failure.
- **FR-025**: When a problem is read from a response, a relative `type` or `instance` MUST be
  resolved against the URL of the request that produced the response. When it is read from a
  document alone, both are kept as written.
- **FR-026**: When a problem is read from a response, both the `status` member (or its absence)
  and the actual status code of the response MUST be available. The library MUST NOT reconcile
  the two.
- **FR-027**: Reading MUST be bounded by a documented maximum size, and MUST NOT read beyond it. A
  body that is too large, not valid JSON or not a JSON object MUST be reported as an error.
  Reading MUST always complete, and no input may crash the program.
- **FR-028**: A problem written by the library and read back MUST equal the original: the same
  standard members and the same extension values.

### Key Entities

- **Problem type**: a kind of problem that an API documents. It has a type URI, which is its
  identifier, a title and a status code. It is declared once and is immutable.
- **Problem (occurrence)**: one instance of a problem type in a response. It has the type's URI,
  title and status, plus an optional detail, an optional instance, extensions, and an optional
  internal cause that never leaves the program.
- **Standard member**: one of `type`, `title`, `status`, `detail` and `instance`, with the
  meaning and value type that RFC 9457 §3.1 defines.
- **Extension member**: an additional member defined by a problem type, whose value is any JSON
  value.
- **Extension name**: the name of an extension member, declared once and checked when declared,
  before any occurrence uses it.
- **Problem document**: the serialized form of a problem, sent with the problem media type.
- **Generic server error**: the `about:blank` problem with status 500 and title "Internal Server
  Error", sent for any error that is not a problem and whenever a problem cannot be written.
- **Internal cause**: the error that led to a problem. It is available to the program's error
  handling and logs, and never sent to the client.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: 100% of the documents that the library writes are valid against the JSON schema of
  RFC 9457 Appendix A, and every extension name in them follows the syntax of §3.2. In 100% of
  the responses, the `status` member equals the response status code.
- **SC-002**: Answering a failed request takes one call in the handler, whether with a status code
  alone, an occurrence of a declared type, or any error.
- **SC-003**: Across a test set of errors whose messages contain credentials, file paths, SQL text
  and stack traces, zero bytes of those messages appear in any response. The same holds for the
  context of wrapping levels and for internal causes.
- **SC-004**: 100% of the problems written by the library are equal to the original when read
  back, including every extension value.
- **SC-005**: Reading any input up to the documented maximum size completes within 100
  milliseconds on a typical developer machine, without failure. A fuzzing session of at least 10
  minutes on the reading operation reports no crash and no hang.
- **SC-006**: The same problem produces a byte-identical document across runs, processes and
  concurrent writes.
- **SC-007**: A refusal function for the repository's `middleware` module that answers every
  refusal with problem details takes at most 5 lines of code, as the documentation shows.
- **SC-008**: Adopting the library adds zero third-party dependencies to a consumer project, and
  no other module of this repository.
- **SC-009**: 100% of the acceptance scenarios and edge cases in this spec are covered by
  automated tests, and statement coverage of the module is at least 80%.

## Assumptions

- **Module and versioning**: one module, `github.com/paccolamano/go/problem`, released with
  `problem/vX.Y.Z` tags. Releases stay at 0.x until the API is considered stable, then 1.0.0.
- **Go version**: the minimum Go version is the oldest release the constitution supports (1.26 at
  the time of writing).
- **Other modules of this repository**: the module depends neither on `errors` nor on
  `middleware`, and neither depends on it. Errors of the `errors` module work through the
  standard error inspection like any other error. A `middleware` refusal function answers with
  problem details in a few lines of the developer's code (SC-007), which the README shows.
- **Error statuses only**: problem details describe errors (RFC 9457, Abstract), so problem types
  and status-only problems accept only 4xx and 5xx status codes. Reading accepts any valid status
  code.
- **Extension names**: the name syntax of §3.2 is enforced only for the extensions that the
  developer adds. Names used in RFC 7807 examples, such as `invalid-params`, are therefore
  refused. Documents read from other APIs keep every name as received.
- **Content negotiation**: problem documents are sent with their media type whatever the request's
  `Accept` header says, as RFC 9457 §3 and HTTP allow.
- **Localization**: the developer supplies localized titles and details. The library neither
  translates text nor negotiates the language.
- **Problem type documentation**: RFC 9457 §4 recommends that a type URI resolve to human-readable
  documentation. Writing and serving that documentation is the developer's responsibility.
- **Defaults chosen in the plan**: the maximum size of a document read, the member order, the
  format of a problem's error message and the names of the operations are fixed and documented
  in `/speckit-plan` (FR-003).
- **Out of scope**:
  - registering problem types with IANA;
  - embedding problem details in other formats (RFC 9457 Appendix C);
  - mapping errors that are not problems to status codes other than 500;
  - converting panics into problems (the `recovery` middleware does that);
  - retrying requests when another API answers with a problem.
- **Tests**: tests are hermetic and deterministic (Principle III): responses are served and read
  in memory, with no network access. The reading operation has fuzz tests, because it decodes
  external input.

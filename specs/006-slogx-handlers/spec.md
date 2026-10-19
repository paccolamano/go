# Feature Specification: Composable Structured Logging Handlers (slogx)

**Feature Branch**: `006-slogx-handlers` (suggested; not yet created)

**Created**: 2026-10-19

**Status**: Draft

**Input**: User description: "Implementiamo un package slogx che esponga degli handler compatibili
con la libreria slog di golang." The full request is reproduced verbatim in
[Appendix: original request](#appendix-original-request).

## Context

The standard library's structured logger leaves three production problems to each project:

- **Missing correlation attributes**: request identifier, user and tenant. In this repository,
  `accesslog` and `recovery` both read the request identifier from the context by hand. In
  application code a record such as "order created", logged from a service, carries no request
  identifier unless an enriched logger is passed everywhere, which in practice nobody does
  consistently.
- **Sensitive data in the output**: tokens, passwords, the `Authorization` header.
- **Repeated records**: the same error ten thousand times a minute while a database is down.

This feature delivers a new, independent module in this repository (Constitution, Principle I),
named `slogx`. It introduces **no logger type of its own**. It offers only handlers that wrap
another handler, so developers keep using the standard library's logger, and the existing
middleware does not change. It provides:

- **context attributes**: attributes attached to a context, or extracted from it, are added to
  every record logged with that context;
- **redaction**: values of sensitive keys, and values wrapped in a secret type, never reach the
  output;
- **per-request level**: a minimum level attached to a context, so that debug records can be
  enabled for one request in production;
- **sampling**: repeated records are thinned out during bursts and summarized;
- **composition**: one constructor that combines the handlers in the only correct order;
- **a test recorder**: a handler that keeps records for inspection in tests.

The Go language, the standard library's structured logger (`log/slog`) and its handler
conformance suite (`testing/slogtest`) are given by the request and by the constitution (v1.2.0).
They are not design choices made here. They appear in this spec because they are the observable
contract of a handler library. How the module is built is left to `/speckit-plan`.

**Actors**:

- **Developer**: configures logging at startup, writes code that logs, and tests what is logged.
- **Operator**: runs services, reads and queries their logs, and enables extra detail when
  diagnosing a problem.
- **Maintainer**: evolves the module and publishes its releases.

### Clarifications

#### Session 2026-10-19

- Q: Should sampling ship in the first release (0.1.0)? → A: Yes. User Story 6 and FR-027 to
  FR-033 are part of this feature.
- Q: When a top-level attribute of the record, or one added to the logger, has the same key as a
  context attribute, which one is written? → A: The record's attribute wins and the context
  attribute is skipped, so every key appears once at the top level (FR-015; User Story 1,
  scenario 6).

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Correlate every record with its request (Priority: P1)

A developer configures logging once, at startup. From then on, every record logged with a
context carries that context's correlation attributes, such as the request identifier, the user
and the tenant. These attributes were either attached to the context by application code, or
stored there by another package and read back by an extraction function. No logger has to be
passed through the call stack.

**Why this priority**: this is the core of the module and the most common gap in production logs.
It also removes the hand-written request identifier code from `accesslog` and `recovery`.

**Independent Test**: wrap an output handler with the context handler and an extraction function
that reads a value stored in the context by another package. Log from functions that receive only
the context, through plain and grouped loggers, and inspect the records.

**Acceptance Scenarios**:

1. **Given** a context with the attribute `tenant=t1` attached, **When** a service logs "order
   created" with that context, **Then** the record contains `tenant=t1` at its top level.
2. **Given** an extraction function that reads the request identifier stored by another package,
   **When** a record is logged with a context whose identifier is "abc", **Then** the record
   contains `request_id=abc`. **When** the context has no identifier, **Then** the record has no
   `request_id`.
3. **Given** a logger derived with a group named "db", **When** it logs a record with attribute
   `rows=3` and a context that carries `tenant` and a request identifier, **Then** `tenant` and
   `request_id` are at the top level of the record and `rows` is inside `db`.
4. **Given** a context to which a handler attached `user=u1`, and a derived context to which a
   service attached `tenant=t1`, **When** a record is logged with the derived context, **Then**
   it carries both attributes. **When** it is logged with the original context, **Then** it
   carries only `user`.
5. **Given** a context to which `tenant` was attached twice, first `t1` and then `t2`, **When** a
   record is logged with it, **Then** the record contains `tenant` once, with value `t2`.
6. **Given** a record that already carries `request_id=abc` explicitly, as `accesslog` does,
   **When** it is logged with a context that provides `request_id=xyz`, **Then** the record
   contains `request_id` once, with value `abc` (FR-015). The same holds when `request_id=abc`
   was added to the logger rather than to the record.
7. **Given** a record logged with a method that takes no context, **When** it is handled,
   **Then** it carries no context attributes. The package documentation states this limitation.
8. **Given** a logger whose level is disabled, **When** a record is logged with a context that
   carries attributes, **Then** no memory is allocated (FR-004).

---

### User Story 2 - Keep secrets out of the logs (Priority: P1)

An operator must be able to rely on tokens, passwords, cookies and authorization headers never
reaching the log output, whichever path they take into a record: an attribute of the record, an
attribute added to the logger, an attribute of the context, or a field of a value that renders as
a group.

**Why this priority**: a leaked credential in a log is a security incident. Logs are copied to
many systems and kept for a long time, so the damage cannot be undone.

**Independent Test**: configure the sensitive keys "authorization", "password", "token" and
"cookie". Send a known secret string through every path listed above, then search the output for
it. Separately, wrap a value in the secret type and send it through a plain output handler, the
formatting verbs and JSON encoding.

**Acceptance Scenarios**:

1. **Given** the sensitive keys above, **When** a record carries `password=hunter2`, **Then** the
   output shows `password=[REDACTED]`.
2. **Given** an access log record whose `header` group contains `Authorization` and `Accept`,
   **When** it is handled, **Then** `header.Authorization` is `[REDACTED]` and `header.Accept` is
   unchanged. Keys are matched without regard to case.
3. **Given** a value whose log representation is a group containing the field `token`, **When**
   it is logged, **Then** `token` is `[REDACTED]` and its other fields are unchanged.
4. **Given** a logger derived with the attribute `token=…`, **When** it logs any record, **Then**
   `token` is `[REDACTED]` in every record.
5. **Given** the composed handler (User Story 5) and a context that provides `token=…`, **When**
   a record is logged with it, **Then** `token` is `[REDACTED]`.
6. **Given** a value wrapped in the secret type, **When** it is logged through a standard output
   handler with no handler of this module, printed with any formatting verb, or encoded as JSON
   as a field of a struct, **Then** each output shows `[REDACTED]`. **When** the developer calls
   its explicit accessor, **Then** the original value is returned.
7. **Given** the sensitive key "password", **When** a record carries `password_hint=…`, **Then**
   it is not redacted: a key matches only as a whole.
8. **Given** a value whose log representation raises a panic, **When** it is logged, **Then** the
   record is still written, with a placeholder in place of that value (FR-005).

---

### User Story 3 - Enable debug records for a single request in production (Priority: P2)

An operator diagnosing a problem in production enables debug records for one request only, for
example one carrying a signed header or one made by an internal user. All other traffic keeps
logging at the normal level.

**Why this priority**: it gives full detail exactly where it is needed, without the cost and noise
of lowering the level for the whole service. It is less central than correlation and redaction.

**Independent Test**: wrap an output handler whose threshold is Info with the level handler. Log
debug records with a context that carries the Debug level and with one that carries no level.

**Acceptance Scenarios**:

1. **Given** an output handler whose threshold is Info and a context with level Debug attached,
   **When** a debug record is logged with it, **Then** the record is written.
2. **Given** the same output handler and a context with no level, **When** a debug record is
   logged with it, **Then** nothing is written and no memory is allocated.
3. **Given** a context with level Debug, and a derived context with level Warn attached for a
   sub-operation, **When** info records are logged, **Then** those logged with the derived context
   are suppressed and those logged with the original context are written.
4. **Given** a context with level Debug, **When** a debug record is logged with a method that
   takes no context, **Then** nothing is written.

---

### User Story 4 - Verify what the code logs, in tests (Priority: P2)

A developer, including this repository's maintainers, checks in tests which records the code
emits: their message, level and attributes, including those added to the logger and those placed
in groups.

**Why this priority**: `middleware/accesslog` and `middleware/recovery` each define a nearly
identical capture handler for their tests. Both ignore attributes added to the logger and groups,
so they cannot verify those. One public recorder replaces them and serves every user of the
module.

**Independent Test**: log through a logger built on the recorder, with plain, derived and grouped
loggers, from one and from many concurrent tasks. Inspect the captured records.

**Acceptance Scenarios**:

1. **Given** a recorder, **When** code logs "panic recovered" with `request_id=abc`, **Then** the
   last captured record has message "panic recovered", and looking up `request_id` returns
   "abc".
2. **Given** a logger derived from the recorder with `component=db` and then the group "query",
   **When** it logs "slow" with `ms=12`, **Then** the captured record has `component` at its top
   level, and looking up the path `query`, `ms` returns 12.
3. **Given** a recorder whose minimum level is Warn, **When** code logs at Info, **Then** the
   logger reports the level as disabled and nothing is captured.
4. **Given** a recorder, **When** 100 concurrent tasks each log one record, **Then** exactly 100
   records are captured.
5. **Given** an empty recorder, **When** the last record is requested, **Then** its absence is
   reported, without a failure.
6. **Given** a captured record, **When** an attribute that does not exist is looked up, **Then**
   its absence is reported.

---

### User Story 5 - Get the correct composition with one call (Priority: P2)

A developer builds the whole logging pipeline over an output handler with one constructor and a
few options, and gets the handlers in the correct order without having to know why the order
matters.

**Why this priority**: the order is easy to get wrong, and the mistakes are silent. If the level
handler is not outermost, the output handler discards debug records before they arrive. If
redaction runs before context attributes are added, a token stored in the context escapes. The
individual handlers remain available for developers who need finer control.

**Independent Test**: build the composed handler over a JSON output handler at Info, with an
extraction function, sensitive keys and sampling. Log with contexts that carry a level, sensitive
attributes and correlation attributes. Compare the output with the documented manual composition.

**Acceptance Scenarios**:

1. **Given** the composed handler over an output handler at Info, **When** a debug record is
   logged with a context that carries level Debug, `request_id=abc` and `token=…`, **Then** the
   record is written, with `request_id=abc` and `token=[REDACTED]`.
2. **Given** the same records, **When** they are logged through the composed handler and through
   the documented manual composition, **Then** the outputs are identical.
3. **Given** the composed handler with no options, **When** records are logged, **Then** context
   attributes and the per-request level work, values wrapped in the secret type are redacted, and
   no record is sampled.
4. **Given** an invalid option, such as a sampling interval of zero or an empty sensitive key,
   **When** the composed handler is built, **Then** construction reports an error.

---

### User Story 6 - Survive log storms (Priority: P3)

When a dependency fails, an operator still gets readable logs. Each repeated record appears a few
times, together with a count of how many identical records were dropped, instead of thousands of
copies that hide everything else and drive up storage costs.

**Why this priority**: it is the most complex capability and the least often needed. It only
pays off during incidents. It ships in the first release nonetheless (see Clarifications).

**Independent Test**: send bursts of identical and distinct records through the sampler, from
one and from many concurrent tasks, across interval boundaries. Count the written records and
read the summaries.

**Acceptance Scenarios**:

1. **Given** sampling that keeps the first 10 records, then one every 100, per 1-second interval,
   **When** "db query failed" is logged 10,000 times at Warn within one interval, **Then** 109
   records are written. After the interval ends, one summary record reports 9,891 dropped records,
   with the message and the level.
2. **Given** the default configuration, **When** records at Error level are logged in a burst,
   **Then** they are all written. **Given** the explicit option to sample errors, **Then** they
   are sampled like the others.
3. **Given** an interval with no more than 10 records for a message, **When** it ends, **Then**
   all of them were written and no summary is emitted.
4. **Given** 1,000,000 distinct messages within one interval, **When** they are logged, **Then**
   the number of tracked messages never exceeds the documented maximum, and records of messages
   that cannot be tracked are written.
5. **Given** 50 concurrent tasks logging the same message, **When** the interval ends, **Then**
   the written records plus the dropped count in the summary equal the number of records logged.
6. **Given** a burst followed by silence, **When** the application flushes the sampler at
   shutdown, **Then** the pending summary is emitted.
7. **Given** a burst logged with request contexts that carry correlation attributes, **When** the
   summary is emitted, **Then** it carries no attribute of any individual request.

### Edge Cases

- **Context attributes and groups**: an attribute attached to the context stays at the top level
  even when the logger has open groups. An attribute with the same key inside a group, for
  example `db.request_id`, is at a different path, so the duplicate key rule (FR-015) does not
  apply to it.
- **Extraction function that returns nothing**: nothing is added. Attributes with an empty key
  are ignored, as the standard library requires.
- **Extraction function that panics**: the record is still written, and a placeholder attribute
  makes the failure visible (FR-005).
- **Value that resolves to another value**: a value whose log representation is itself a value
  with a log representation is resolved fully before redaction is checked.
- **Opaque values**: a struct logged as a plain value is encoded by the output handler, and
  redaction by key cannot see its fields. The documentation states this, and the secret type is
  the protection for such fields (FR-021).
- **Group whose name is a sensitive key**: a group named, for example, "password", whether it is
  an attribute or a group opened on the logger, is redacted as a whole.
- **Empty group after redaction**: a redacted group becomes the marker, so it is never dropped as
  an empty group.
- **Secret type zero value**: a secret that wraps a zero value is still shown as `[REDACTED]`.
- **Per-request level above the threshold**: a level higher than the output handler's threshold
  suppresses, for that request, records the output handler would otherwise write (FR-024).
- **Disabled records**: no handler allocates memory for a disabled record, with or without
  context attributes or a per-request level (FR-004).
- **Errors from the output handler**: an error returned by the wrapped handler is returned
  unchanged to the logger.
- **Sampling, many distinct messages**: each distinct message is a new group. Once the tracking
  bound is reached, records of untracked groups pass and are never dropped for lack of space
  (FR-031).
- **Sampling, interval with no drops**: no summary is emitted.
- **Sampling, summary records**: summaries are never sampled or dropped themselves.
- **Recorder, values resolved later**: a captured value whose log representation would change
  after capture keeps the value it had when it was captured.

## Requirements *(mandatory)*

### Functional Requirements

#### Module and shared conventions

- **FR-001**: The module MUST be delivered as one independent module in this repository, in the
  directory `slogx`. Its package is named `slogx`, and its test-support package is
  `slogx/slogxtest`. The module MUST NOT introduce a logger type: every capability is a handler
  of the standard library's structured logger, or a function that stores values in a context.
  Developers keep using the standard logger and its methods.
- **FR-002**: Every handler of the module MUST wrap another handler, called the next handler, and
  MUST itself be a standard handler. Handlers can then be composed with each other, and with
  third-party handlers, in any order the developer chooses.
- **FR-003**: Every handler of the module MUST pass the standard library's handler conformance
  suite when it wraps a conforming handler: each standalone handler, the composed handler and the
  recorder. The sampler passes it under a configuration that drops no record during the suite.
- **FR-004**: When the logger asks a handler of the module whether a level is enabled and the
  answer is no, the handler MUST NOT allocate memory. This holds also when the context carries
  attributes or a per-request level, and it is verified by benchmarks.
- **FR-005**: Handlers of the module MUST NOT panic, or let a panic escape, while processing a
  record, whatever the input.
  - A value whose log representation panics is replaced by a documented placeholder, and the
    record is still written.
  - An extraction function (FR-012) that panics contributes a documented placeholder attribute
    instead of its attributes, and the record is still written.
  - An error returned by the next handler is returned unchanged.
- **FR-006**: Every handler of the module MUST be safe for concurrent use, and so are the
  handlers derived from it by adding attributes or opening groups. The module MUST NOT hold
  process-wide state, use a global logger or write to any output on its own.
- **FR-007**: Invalid configuration MUST be reported to the caller as an error when the handler
  is built, never as a panic or a silent fallback. This includes a missing next handler, a
  missing extraction function, an empty sensitive key and invalid sampling parameters.
- **FR-008**: The module MUST NOT depend on any module outside the standard library: neither
  third-party modules nor the other modules of this repository. This includes its tests and
  examples.
- **FR-009**: The package documentation MUST explain:
  - that context attributes and the per-request level apply only to records logged with methods
    that take a context, and that a linter rule requiring those methods prevents the mistake;
  - the order in which the handlers must be composed, and why;
  - what redaction covers and what it does not (FR-021);
  - the sampling rules and the content of summary records.

  Every exported capability MUST have a runnable example.

#### Context attributes

- **FR-010**: Developers MUST be able to attach attributes to a context. Attaching attributes to
  a context that already carries some produces a derived context that carries both. The original
  context is unchanged.
- **FR-011**: The context handler MUST add, to every record logged with a context, the attributes
  attached to that context.
- **FR-012**: Developers MUST be able to configure one or more extraction functions. Each one
  computes attributes from the context of every record. Values stored in the context by other
  packages, such as the request identifier of `middleware/requestid`, can then appear in records
  without the module depending on those packages.
- **FR-013**: Context attributes, whether attached or extracted, MUST be placed at the top level
  of the record, whatever groups the logger has opened. Attributes of the record and attributes
  added to the logger keep their groups.
- **FR-014**: Among context attributes, each key MUST appear once. Attached attributes take
  precedence over extracted ones. Among attached attributes, the most recently attached value
  wins. Among extraction functions, the first one configured wins.
- **FR-015**: When an attribute at the top level of the record, either from the record itself or
  added to the logger, has the same key as a context attribute, the record's attribute MUST be
  kept and the context attribute skipped, so that the key appears once at the top level. The
  comparison is exact and applies only at the top level. Duplicate keys among the record's own
  attributes are left as they are.
- **FR-016**: A record logged with a context that carries no attributes, and for which no
  extraction function returns any, MUST be passed to the next handler unchanged.

#### Redaction

- **FR-017**: Developers MUST be able to configure a set of sensitive keys. An attribute whose
  key equals a sensitive key, compared without regard to case, MUST be written with the same key
  and the fixed marker `[REDACTED]` as its value. Keys match only as a whole, never as a
  substring.
- **FR-018**: Matching MUST apply at every depth: inside groups of the record, inside groups the
  logger has opened, and inside groups produced by values' log representations.
- **FR-019**: Redaction MUST cover attributes from every source: attributes of the record,
  attributes added to the logger, and context attributes when the handlers are composed as in
  FR-034. A value's log representation MUST be resolved before the check, so that a value that
  renders as a group has its fields checked.
- **FR-020**: An attribute or a group opened on the logger whose key matches a sensitive key MUST
  be redacted as a whole, including all its content.
- **FR-021**: Redaction MUST NOT change the message and MUST NOT inspect the content of values.
  Values that the output handler encodes on its own, such as plain structs, are passed through
  unchanged, and the documentation says so.
- **FR-022**: Attributes that do not match MUST keep their key, value, order and group.
- **FR-023**: Developers MUST be able to wrap a value of any type in a secret type. It MUST show
  as `[REDACTED]` wherever it ends up:
  - as a log value, through any handler, including standard output handlers with no handler of
    this module;
  - in formatted output, with every formatting verb;
  - in JSON and text encoding, including when it is a field of an encoded struct.

  The wrapped value MUST be available only through an explicit accessor.

#### Per-request level

- **FR-024**: Developers MUST be able to attach a minimum level to a context. For records logged
  with that context, the level handler MUST decide whether a level is enabled by comparing it
  with that level alone, both below and above the next handler's own threshold. For records whose
  context carries no level, it MUST defer to the next handler.
- **FR-025**: A record enabled by the per-request level MUST reach the output even when the
  output handler's own threshold is higher. No handler of the module filters a record by level
  again once it has been enabled.
- **FR-026**: When levels are attached more than once along a chain of derived contexts, the most
  recently attached level MUST apply.

#### Sampling

- **FR-027**: The sampler MUST group records by level and message. Attributes, including those
  added to the logger, do not distinguish records. All the handlers derived from one sampler share
  its counts.
- **FR-028**: In each interval, the first N records of a group MUST be written, then one record
  every M. A group that receives T records in an interval, with T greater than N, therefore has
  N + ⌊(T − N) / M⌋ records written. N, M and the interval length are configured by the
  developer.
- **FR-029**: Records at Error level and above MUST NOT be sampled unless the developer enables
  it explicitly.
- **FR-030**: For each group that had records dropped in an interval, exactly one summary record
  MUST be emitted after the interval ends.
  - The summary has the group's level, and it carries the group's message, the number of dropped
    records and the period it covers.
  - It carries no attribute of any individual request.
  - It is emitted no later than when the sampler handles its next record, or when the developer
    flushes the sampler, for example at shutdown.
  - Summary records are never sampled or dropped.
- **FR-031**: The number of groups tracked in an interval MUST be bounded by a documented maximum,
  so that memory does not grow with the number of distinct messages. When the bound is reached,
  records of groups that are not tracked MUST be written, never dropped for lack of space.
- **FR-032**: Under concurrent logging, counts MUST be exact. For every group and interval, the
  records written plus the records reported as dropped equal the records received.
- **FR-033**: The sampler MUST NOT start background work that outlives the developer's control.
  Any such work has an owner and a documented way to stop it.

#### Composition

- **FR-034**: A single constructor MUST build the full pipeline over an output handler, from
  options:
  - the per-request level and context attributes are always active;
  - extraction functions are added when configured;
  - redaction by key is active when sensitive keys are configured;
  - sampling is active when configured.
- **FR-035**: The composed handler MUST guarantee:
  - the per-request level is effective even when the output handler's threshold is higher
    (FR-025);
  - context attributes, attached and extracted, are subject to redaction;
  - summary records carry no attribute of any individual request (FR-030).
- **FR-036**: Each capability MUST also be available as a standalone handler. The documentation
  MUST give the manual composition that is equivalent to the composed handler, and the two MUST
  produce identical output for the same records.

#### Test recorder

- **FR-037**: The test-support package MUST provide a recorder: a handler that keeps every record
  it handles, in order, for inspection. It is safe for concurrent use.
- **FR-038**: Captured records MUST reflect attributes added to the logger and groups opened on
  it, as an output handler would write them. Values' log representations are resolved when the
  record is captured.
- **FR-039**: Developers MUST be able to retrieve all captured records in order, the number of
  records and the last record. For a captured record, they MUST be able to read its time, level
  and message, and to look up an attribute by key at the top level or by a path of group names.
  A missing last record and a missing attribute are reported as absent, without a failure.
- **FR-040**: The recorder's minimum level MUST be configurable. By default every level is
  enabled.
- **FR-041**: The recorder MUST cover every use of the capture handlers in
  `middleware/accesslog` and `middleware/recovery`: records in order, top-level attributes and a
  minimum level.

### Key Entities

- **Next handler**: the handler a handler of the module wraps and passes records to. The last one
  in a pipeline is the output handler, which writes records.
- **Context attributes**: attributes attached to a context, or computed from it by an extraction
  function, and added to every record logged with that context.
- **Extraction function**: a developer-supplied function that computes attributes from a context.
- **Sensitive key**: an attribute key whose values must never be written. It is matched without
  regard to case, at any depth.
- **Secret**: a value wrapped so that it always shows as `[REDACTED]`, and is available only
  through an explicit accessor.
- **Per-request level**: a minimum level attached to a context, which overrides the threshold for
  the records logged with that context.
- **Sampling group**: the records that share a level and a message.
- **Interval**: the period over which the sampler counts the records of each group.
- **Summary record**: a record that reports how many records of a group were dropped in an
  interval.
- **Recorder** and **captured record**: the test handler and the records it keeps.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: In a test service where the request identifier is stored in the context by a
  middleware, 100% of the records logged with the request's context, from any function, carry the
  request identifier. No logger is passed through the call stack, and no call site contains code
  to add it.
- **SC-002**: 100% of the module's handlers, including the composed handler and the recorder, pass
  the standard library's handler conformance suite.
- **SC-003**: Every handler of the module, and the composed handler, allocates 0 bytes for a
  disabled record, as measured by the module's benchmarks.
- **SC-004**: A known secret string sent through every path covered by FR-017 to FR-023 appears 0
  times in the output. The paths are: record attribute, attribute added to the logger, attached
  and extracted context attribute, nested group, group opened on the logger, group produced by a
  value's log representation, keys in mixed case, and the secret type through log values,
  formatting verbs and JSON encoding.
- **SC-005**: With sampling that keeps the first 10 records, then one every 100, 10,000 identical
  records in one interval produce exactly 109 written records and one summary that reports 9,891
  dropped records. The totals are identical when the records come from 50 concurrent tasks.
- **SC-006**: Logging 1,000,000 distinct messages within one interval never makes the sampler
  track more groups than its documented maximum.
- **SC-007**: In 100% of tested cases, a value whose log representation panics and an extraction
  function that panics produce a placeholder, and the record is still written. The program never
  crashes.
- **SC-008**: For an enabled record with 5 attributes and 2 context attributes, logged through a
  logger with no open groups, the composed handler with redaction and sampling takes at most twice
  the time of the output handler alone writing the same 7 attributes, as measured by the module's
  benchmarks.
- **SC-009**: The recorder covers 100% of the uses of the two capture handlers in the middleware
  module. Once the middleware tests are migrated, neither test suite defines a capture handler of
  its own.
- **SC-010**: Adopting the module adds zero dependencies beyond the standard library to a consumer
  project.
- **SC-011**: 100% of the acceptance scenarios and edge cases in this spec are covered by
  automated tests, and statement coverage of the module is at least 80%.

## Assumptions

- **Module and versioning**: one module, `github.com/paccolamano/go/slogx`, released with
  `slogx/vX.Y.Z` tags. The first release is 0.1.0, and releases stay at 0.x until the API is
  considered stable.
- **Go version**: the minimum Go version is the oldest release the constitution supports (1.26 at
  the time of writing).
- **Existing middleware**: `accesslog` and `recovery` keep their behavior, including the
  `request_id` attribute they add themselves. Replacing their capture handlers with the recorder
  is a change to the `middleware` module. It is made after `slogx` is first tagged, because a
  module may depend only on published versions of another (Constitution, Principle I, and the
  change scope rule).
- **Redaction marker**: the marker is the fixed string `[REDACTED]`. Matching is on whole keys,
  without regard to case. Path patterns and substrings are not supported.
- **Per-request level**: the level attached to a context overrides the threshold in both
  directions, so it can also silence a request. This is the simplest rule to explain, and an
  operator who attaches a level states the level they want.
- **Defaults chosen in the plan**: the maximum number of tracked sampling groups, the placeholder
  values of FR-005, the message and attribute names of summary records, and the syntax of
  attribute paths in the recorder are fixed and documented in `/speckit-plan` (FR-009).
- **Out of scope**:
  - fan-out to several destinations;
  - log file rotation and shipping logs to external services;
  - a bridge to OpenTelemetry;
  - output formats of its own, including a colored handler for local development;
  - a global level that can be changed at runtime.
- **Tests**: tests are hermetic and deterministic (Principle III). Sampling intervals are verified
  without real waiting. Benchmarks back FR-004, SC-003 and SC-008.

## Appendix: original request

The request, as submitted to `/speckit-specify`:

```text
Implementiamo un package slogx che esponga degli handler compatibili con la libreria slog di golang.

Il problema di partenza

Nel repository, sia accesslog sia recovery fanno questo a mano:

go
if id, ok := requestid.FromContext(ctx); ok {
	attrs = append(attrs, slog.String("request_id", id))
}

Nel codice applicativo succede lo stesso, ma peggio. Un logger.InfoContext(ctx, "ordine creato") scritto in un service non contiene il request_id, a meno di passare ovunque un logger già arricchito. E in pratica nessuno lo fa con costanza.

I problemi tipici dei log in produzione sono tre, e slog da solo non li risolve:

Mancano gli attributi di correlazione: request_id, utente, tenant.
Finiscono nei log dati che non dovrebbero esserci: token, password, header Authorization.
I log ripetuti sono troppi: lo stesso errore diecimila volte al minuto quando il database è giù.
Il principio: niente logger nuovo

La libreria non dovrebbe introdurre un tipo di logger proprio. Dovrebbe offrire solo slog.Handler componibili, cioè middleware per handler. L’utente continua a usare *slog.Logger e la libreria standard, e i tuoi middleware non cambiano. Il nome che suggerisco è slogx, più chiaro di logx e senza confusione con il pacchetto log.

Cosa dovrebbe fare
1. Attributi dal contesto (il cuore)
go
ctx = slogx.With(ctx, slog.String("tenant", t.ID))  // aggiunge attributi al contesto
logger.InfoContext(ctx, "ordine creato")            // il record contiene "tenant"

L’handler legge gli attributi dal contesto a ogni record. Accetta anche funzioni di estrazione, così l’integrazione con requestid avviene senza dipendenze tra i moduli:

go
h := slogx.NewContextHandler(json, slogx.WithContextFunc(func(ctx context.Context) []slog.Attr {
	if id, ok := requestid.FromContext(ctx); ok {
		return []slog.Attr{slog.String("request_id", id)}
	}
	return nil
}))

Funziona solo con i metodi *Context: logger.Info senza contesto non ha nulla da leggere. Va documentato, e il linter sloglint ha una regola che impone l’uso del contesto.

2. Redazione dei dati sensibili
Per chiave, senza distinzione tra maiuscole e minuscole e anche dentro i gruppi. Questo copre il gruppo header di accesslog (header.authorization).
Per tipo, con un slogx.Secret[T] che si rende sempre come [REDACTED]. È la protezione più affidabile, perché vale ovunque il valore finisca.

La trappola: un valore che implementa LogValuer va risolto prima del controllo, altrimenti una struct che si rende come gruppo nasconde i campi sensibili. Lo stesso vale per gli attributi aggiunti con logger.With(...), che non passano da Handle come i normali attributi del record.

3. Livello dinamico per singola richiesta
go
ctx = slogx.WithLevel(ctx, slog.LevelDebug)

Handler.Enabled riceve il contesto, quindi si possono abilitare i log di debug per una sola richiesta in produzione, ad esempio attivandoli con un header firmato o solo per utenti interni. Il resto del traffico resta a livello Info. Per il livello globale modificabile a runtime, slog.LevelVar della libreria standard basta già.

4. Campionamento dei log ripetuti

Per ogni coppia (livello, messaggio), i primi N record per intervallo passano, poi uno ogni M. Alla fine dell’intervallo esce un record riassuntivo: “scartati 9.950 record simili”. Gli errori di default non verrebbero campionati, oppure solo con un’opzione esplicita. Le trappole qui sono la memoria, che va limitata anche con molti messaggi diversi, e la concorrenza.

5. Un handler per i test

In middleware/accesslog/capture_test.go e middleware/recovery/capture_test.go hai già due handler di cattura quasi identici. Un slogx/slogxtest.Recorder pubblico li sostituirebbe, e servirebbe anche agli utenti per controllare cosa viene loggato:

go
rec := slogxtest.NewRecorder()
logger := slog.New(rec)
// ...
r := rec.Last()
if r.Message != "panic recovered" || r.Attr("request_id").String() != "abc" { ... }
6. Composizione nell’ordine giusto

L’ordine degli handler conta, ed è facile sbagliarlo:

il livello per richiesta deve stare all’esterno, altrimenti l’Enabled dell’handler JSON scarta il debug prima ancora di arrivare;
la redazione deve stare dopo l’aggiunta degli attributi dal contesto, altrimenti un token messo nel contesto sfugge.

Per questo offrirei un costruttore che le compone già correttamente:

go
logger := slog.New(slogx.New(slog.NewJSONHandler(os.Stdout, nil),
	slogx.WithContextFunc(requestIDAttrs),
	slogx.WithRedactKeys("authorization", "password", "token", "cookie"),
	slogx.WithSampling(10, 100, time.Second),
))

Chi vuole il controllo fine usa i singoli handler.

Requisiti di correttezza

Sono il motivo per cui ha senso farne una libreria invece di copiare codice da un progetto all’altro:

Conformità a testing/slogtest: ogni handler deve superare slogtest.TestHandler, che verifica la semantica di WithAttrs, WithGroup, gruppi vuoti e attributi senza chiave.
Gruppi: quando l’utente fa logger.WithGroup("db"), gli attributi del contesto vanno al livello principale e non dentro db. Il request_id deve stare sempre nello stesso posto, altrimenti le query sui log diventano un incubo.
Chiavi duplicate: se accesslog aggiunge già request_id e lo aggiunge anche l’handler, il JSON avrebbe la chiave due volte. Proporrei che l’attributo esplicito del record vinca e quello dal contesto venga saltato.
Costo zero quando il log è disabilitato: niente allocazioni se Enabled restituisce false, verificato da un benchmark come quello di errors (BenchmarkAttrDisabled).
Mai un panic, coerentemente con errors.Describe. Un LogValue che va in panic produce un valore segnaposto, non un crash.
Cosa lascerei fuori
Fanout verso più destinazioni: dalla Go 1.26 c’è slog.NewMultiHandler nella libreria standard.
Rotazione dei file e invio a servizi esterni: è un compito dell’infrastruttura. Il servizio scrive su stdout.
Bridge OpenTelemetry: richiede dipendenze esterne, ed esiste già otelslog.
Un formato di output proprio: JSON e Text della libreria standard bastano. Un handler colorato per lo sviluppo locale sarebbe carino, ma lo rimanderei.
Decisioni da prendere
Campionamento: lo includiamo nella v0.1 o lo rimandiamo? È la parte più complessa e meno usata delle sei.
Integrazione con errors: l’handler potrebbe trasformare automaticamente ogni valore error in errors.DetailValue, così basta "err", err senza errors.Attr. È comodo, ma rende slogx dipendente da errors e cambia il comportamento documentato (“errors logged as plain values appear exactly like standard errors”).
Chiavi duplicate: “vince il record” come proposto sopra, oppure lasciamo i duplicati, come fa la libreria standard?
Redazione basata sul contenuto dei valori, ad esempio con espressioni regolari che riconoscono i JWT: la vuoi? È costosa e dà falsi positivi. Io la escluderei e mi affiderei a chiavi e tipi.
```

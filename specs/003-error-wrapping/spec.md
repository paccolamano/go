# Feature Specification: Error Wrapping and Error Detail Module

**Feature Branch**: `003-error-wrapping` (suggested; not yet created)

**Created**: 2026-10-16

**Status**: Draft

**Input**: User description: "Vorrei implementare una libreria golang per migliorare la gestione degli errori. il punto di riferimento è questa libreria https://github.com/sorintlab/errors. L'integrazione con zerolog direi che è superflua ma il wrapping degli errori e il dettaglio dell'errore sono funzionlità da implementare. Vorrei che iniziassi una fase di analisi del progetto per capire quali sono i pro e i contro della libreria per poter creare una versione migliore."

## Context

The language's standard error wrapping keeps the chain of causes, but it records no location.
When an error reaches a log, the reader sees a message. They do not see where the error was
created, which path it took on the way up, or who called the code that failed. The reference
library `github.com/sorintlab/errors` addresses this by recording locations and printing an error
detail. Its analysis ([analysis.md](analysis.md)) found real strengths and also defects that make
it hard to trust and to read:

- a formatted constructor that silently drops causes;
- formatting verbs that are ignored;
- details that repeat every message at every level and leak build paths;
- meaningless locations for sentinel errors;
- a crash on pathological inputs;
- no tests.

This feature delivers a new, independent module in this repository (Constitution, Principle I),
named `errors`. It is a drop-in replacement for the standard library's errors package. It keeps
the reference's strengths and fixes its weaknesses. It provides:

- **wrapping**: creating errors and adding context to them, recording where each step happened,
  with messages and inspection identical to the standard library's;
- **error detail**: a readable and a structured rendering of an error tree, with each level's own
  text, its location and the origin's call stack;
- **logging**: an opt-in helper that turns an error into an attribute of the standard library's
  structured logger.

The Go language, the module layout and the standard library baseline are given by the request
and by the constitution (v1.2.0). They are not design choices made here. Formatting verbs (`%v`,
`%+v`, `%q`, the error-wrapping directive `%w`) and JSON appear in this spec because they are the
observable contract of an error library. How the module is built is left to `/speckit-plan`.

**Actors**:

- **Developer**: writes code that creates, wraps, inspects and logs errors, and defines custom
  error types.
- **Operator**: reads error details in logs and test output to diagnose failures.
- **Maintainer**: evolves the library and publishes its releases.

### Reference analysis summary

| Finding in the reference | Decision | Requirements |
|--------------------------|----------|--------------|
| Familiar vocabulary (create, wrap, annotate, combine) | Keep | FR-006 to FR-012 |
| Works with the standard inspection, also through standard wrapping levels | Keep | FR-002 |
| Wrapping no error yields no error | Keep | FR-013 |
| A location at every wrap point | Keep, cheaper | FR-015 |
| Error trees shown with indexes and indentation | Keep, fixed for multi-line messages | FR-022 |
| Embeddable building block for custom error types | Keep, with consistent locations | FR-026 to FR-029 |
| Formatted constructor ignores the error-wrapping directive and drops the cause | Fix | FR-007, FR-010 |
| Formatting verbs ignored; the detailed verb shows nothing more | Fix | FR-023 |
| Detail repeats cumulative messages, duplicates levels, shows internal type names and absolute build paths | Fix | FR-018 to FR-020 |
| Full stack captured at every level but never shown; about 4 times the standard cost per wrap | Fix | FR-016, FR-021, SC-004 |
| Sentinel errors carry the location of package initialization | Fix | FR-008 |
| Caller-skip numbering off by one between APIs | Fix | FR-017, FR-028 |
| Detail available only as text lines | Fix | FR-024, FR-030 |
| Detail of a self-referencing error crashes the process | Fix | FR-025 |
| Only part of the standard errors capabilities offered | Fix: full drop-in replacement | FR-001 |
| No tests, no examples, undocumented exports | Fix | FR-005, SC-009 |

### Clarifications

#### Session 2026-10-16

- Q: Should the module be a drop-in replacement for the standard library's errors package, or a
  companion package with a distinct name? → A: Drop-in replacement. The directory and the
  package are both named `errors`, and the module offers every capability of the standard
  package (FR-001; User Story 1, scenario 8).
- Q: How should the library integrate with the standard library's structured logger? → A: Through
  an opt-in helper that turns an error into a log attribute with its message and structured
  detail. Errors logged without the helper are unchanged (FR-030; User Story 4, scenarios 2 and
  3).

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Add context to errors as they propagate (Priority: P1)

A developer adds context to an error at each layer it passes through, with one call per layer.
Callers can still recognize the underlying cause. What the developer knows about the standard
library's wrapping applies unchanged: same messages, same inspection, and nothing breaks when the
library is mixed with standard wrapping.

**Why this priority**: every other capability (locations, detail, custom types) is built on
wrapping. A wrapping that changes messages or loses causes would make the library unsafe to adopt.

**Independent Test**: build a three-level chain over a standard sentinel error with the library,
and the equivalent chain with the standard wrapping. Then compare the messages and the results of
identity and type inspection. Also wrap a nil error, and use formats that reference causes.

**Acceptance Scenarios**:

1. **Given** a cause whose message is "file does not exist", **When** it is wrapped with
   "open config", then "load tenant 42", then "handle request", **Then** the message is
   "handle request: load tenant 42: open config: file does not exist". This is identical to the
   equivalent standard chain, and identity inspection finds the original cause.
2. **Given** a function result that is no error, **When** any wrapping, annotating or combining
   operation is applied to it, **Then** the result is no error.
3. **Given** an error created from a format that references two errors with the error-wrapping
   directive, **When** it is inspected, **Then** both errors are found. Its message equals the one
   the standard library produces for the same format.
4. **Given** a chain with a level wrapped by the standard library in the middle, **When** it is
   inspected by identity and by type, **Then** the errors above and below that level are found.
5. **Given** a formatted wrapping context that uses the error-wrapping directive, **When** the
   code is checked with the toolchain's standard static checks, **Then** the misuse is reported.
6. **Given** an error annotated with a location only, **When** its message is read, **Then** it is
   unchanged.
7. **Given** three errors, one of them nil, **When** they are combined, **Then** the message lists
   the two others one per line, and both are found by inspection. **Given** only nil errors,
   **Then** the result is no error.
8. **Given** a package written against the standard library's errors package, **When** only its
   import is switched to the library, **Then** it compiles unchanged, and every message and every
   inspection result stays the same.

---

### User Story 2 - Diagnose an error from its detail (Priority: P1)

An operator reading a log, or a developer reading a test failure, needs a single detail rendering
to show three things: where the error originated, the call path that led to the origin, and every
place the error passed through on the way up. Each piece of context appears once.

**Why this priority**: this is why anyone would adopt the library instead of the standard
wrapping. It is also where the reference fails most visibly (analysis W3 and W4).

**Independent Test**: build chains across functions in different packages: linear chains, chains
with combined errors, chains with standard-library levels and chains with multi-line messages.
Then check the rendered detail and the structured form.

**Acceptance Scenarios**:

1. **Given** the chain of Story 1, scenario 1, built in three functions, **When** its detail is
   rendered, **Then** it shows four levels from the outermost to the root cause:
   - each level shows only its own text: "handle request", "load tenant 42", "open config" and
     "file does not exist";
   - the first three levels each show a function, a file and a line;
   - the call stack appears once, at the "open config" level, which is the origin.
2. **Given** that chain, **When** it is formatted with the detailed verb, **Then** the output
   equals the explicit rendering. **When** it is formatted with `%s`, `%v` or `%q`, **Then** the
   output equals what a standard library error with the same message produces.
3. **Given** an error that combines two chains, **When** its detail is rendered, **Then** each
   chain is a labeled branch, indented under the combining level. A two-line message in one branch
   stays indented within that branch.
4. **Given** a chain with a standard-library wrapping level "query" in the middle, **When** its
   detail is rendered, **Then** that level shows only "query" and no location, and the levels
   below it keep their locations.
5. **Given** a chain that contains an error of a type defined by a third party, **When** its
   detail is rendered, **Then** that level shows its type. The library's own types and the
   standard library's generic types are not shown.
6. **Given** the same program built in two different directories, **When** the same error is
   rendered, **Then** the two details are identical.
7. **Given** any error, **When** its structured form is serialized to JSON, **Then** the document
   contains the same levels, texts, locations and call stack as the text detail, under the
   documented field names.
8. **Given** an origin whose call stack is deeper than the documented maximum, **When** its detail
   is rendered, **Then** the stack is cut at the maximum and marked as truncated.
9. **Given** an error created in one concurrent task and wrapped in another, **When** its detail
   is rendered, **Then** it shows the origin's call stack from the first task and the wrap
   locations from the second.

---

### User Story 3 - Define custom error types that callers can branch on (Priority: P2)

A developer defines a domain error type, for example a connection error that carries the remote
address, so that callers can branch on it by type. It carries a message, a cause, a location and
a detail like any other library error, and the developer writes none of that support by hand.

**Why this priority**: typed errors are the idiomatic way to expose conditions that callers
branch on (Constitution, Principle IV). Without help, each type re-implements the same support
and loses the location.

**Independent Test**: define a custom type that embeds the building block and has a constructor.
Create errors through the constructor, with and without a cause. Then inspect them by type,
render their detail and vary the skip level.

**Acceptance Scenarios**:

1. **Given** a custom type that embeds the building block and a constructor that wraps a cause,
   **When** code calls the constructor, **Then** type-based inspection of the outermost error
   finds the custom error and its fields, and identity inspection still finds the cause.
2. **Given** that error, **When** its detail is rendered, **Then** the level shows the developer's
   type, and its location is the line that called the constructor, not the constructor's body.
3. **Given** the constructor configured with skip level 0, **Then** the location is inside the
   constructor. **Given** skip level 2, **Then** it is the caller's caller. This is the same
   numbering as every other operation.
4. **Given** a custom error without a cause, **Then** its message is its own message. **Given**
   one with a cause, **Then** its message is "message: cause message".

---

### User Story 4 - Feed error details to logs and tools (Priority: P2)

An operator wants error details in structured log records. The log platform can then index the
origin's location and the levels of the error, instead of receiving one multi-line string.

**Why this priority**: production logs are structured, and the reference only produces text
lines that consumers have to re-assemble (analysis W8). It builds on the detail of Story 2.

**Independent Test**: serialize the structured form of several error trees and check that the
fields are complete and stable. Log errors with and without the helper (FR-030) through a logger
whose handler captures records, at enabled and disabled levels, and inspect the records.

**Acceptance Scenarios**:

1. **Given** an error tree, **When** its structured form is serialized to JSON and decoded back,
   **Then** the decoded data holds the same levels, texts, types, locations and call stacks.
2. **Given** a structured logger with a capturing handler, **When** an error is logged with the
   helper, **Then** the record carries one attribute, under the default key, with the error
   message and the structured detail as nested attributes. **When** the same error is logged as a
   plain value without the helper, **Then** the record carries only its message, as it would for
   a standard library error.
3. **Given** a logger whose level disables the record, **When** an error is logged with the
   helper, **Then** the detail of the error is not computed.

---

### User Story 5 - Keep sentinel errors clean and frequent errors cheap (Priority: P3)

A developer declares package-level sentinel errors and uses errors in frequently executed code,
for example expected "not found" conditions. Neither carries information nobody reads or costs
much more than standard errors.

**Why this priority**: an optimization and a polish of the detail. In the reference, sentinels
carry the location of package initialization and every wrap costs about 4 times the standard
wrapping (analysis W5 and W6).

**Independent Test**: declare sentinels at package level, then render them alone and as the root
of a chain. Run the module benchmarks against the standard library's equivalents.

**Acceptance Scenarios**:

1. **Given** a sentinel declared at package level with the library's message constructor, **When**
   it is rendered alone or as the root of a chain, **Then** it shows only its message, with no
   location and no call stack.
2. **Given** that sentinel wrapped twice, **When** it is inspected by identity, **Then** it is
   found.
3. **Given** an error that already carries a call stack, **When** it is wrapped, **Then** no
   second call stack is recorded, and the cost stays within SC-004.

### Edge Cases

- **Empty context**: wrapping with an empty context, or annotating with a location only, leaves
  the message equal to the cause's. In the detail:
  - when the cause has no location of its own, the two are shown as one level: the cause's text
    with the annotation's location;
  - otherwise the level shows its location and no text.
- **Text that cannot be isolated**: a level produced by other code whose message does not end with
  its cause's message shows its full message (FR-020). A level from other code that adds no text
  shows no text.
- **Shared causes**: when the same error is a cause in two branches of a tree, it is rendered in
  both branches.
- **Pathological trees**: self-referencing errors, chains of 10,000 levels and errors that combine
  1,000 causes produce a bounded detail with the cut marked. They never fail, hang or crash the
  program (FR-025).
- **Package initialization**: errors created while packages are being initialized carry no
  location and no call stack. This includes errors built through helper functions called from
  package-level declarations. The two documented exceptions of FR-008 (concurrent tasks started
  during initialization, very deep initialization call stacks) keep a location.
- **Combining a single error**: combining one non-nil error behaves as the standard library does.
- **No error**: formatting no error with the detailed verb prints the standard formatting's marker
  for no value. The explicit rendering of no error is empty (FR-025).
- **Concurrent use**: the same error is inspected, formatted and rendered at the same time from
  many concurrent tasks, with identical results and no interference (FR-003).
- **Errors with their own stack traces**: errors from other libraries that carry their own stack
  traces are shown as ordinary levels, with their text and type. Their stacks are not interpreted.
- **Mixing with the standard package**: errors created by the standard errors package and by the
  library are interchangeable for inspection in both directions. The library's "unsupported
  operation" sentinel matches errors that report the standard one (FR-001).
- **Logging no error**: the logging helper applied to no error adds nothing to the record
  (FR-030).

## Requirements *(mandatory)*

### Functional Requirements

#### Module and shared conventions

- **FR-001**: The library MUST be delivered as one independent module in this repository, in the
  directory `errors`, and its package is also named `errors`. It MUST be a drop-in replacement for
  the standard library's errors package: it offers every capability of that package, so that code
  which switches its import needs no other change. The capabilities are inspection by identity,
  inspection by type (in both forms), unwrapping, combining and the "unsupported operation"
  sentinel.
  - Inspection and unwrapping MUST behave exactly as in the standard package.
  - The "unsupported operation" sentinel MUST be the standard package's own value, so that it
    matches errors from code that uses the standard package.
  - Creating and combining errors MUST produce the same messages and inspection results as the
    standard package. In addition, they record locations (FR-015, FR-016).
- **FR-002**: Every error the library produces MUST work with the standard error inspection of the
  language (by identity, by type and by unwrapping) through any number of levels. This includes
  levels produced in between by the standard library or by third-party code.
- **FR-003**: Errors MUST be immutable once created, and safe to inspect, format and render
  concurrently. The library MUST NOT hold process-wide state or configuration. It MUST NOT write
  to any output or log on its own.
- **FR-004**: The library MUST NOT depend on any third-party module.
- **FR-005**: The package documentation MUST explain:
  - the location model (FR-015 to FR-017);
  - the detail format (FR-019 to FR-024);
  - how to declare sentinel errors and custom error types;
  - when to wrap, annotate or create an error;
  - how to log errors with the helper (FR-030).

  Every exported capability MUST have a runnable example.

#### Creating errors

- **FR-006**: Developers MUST be able to create an error from a message. The error records its
  location (FR-015) and its call stack (FR-016).
- **FR-007**: Developers MUST be able to create an error from a format specification. The
  formatting MUST behave exactly like the language's standard formatted error creation, including
  the error-wrapping directive: every error referenced with that directive, one or several,
  becomes a cause of the new error and stays inspectable (FR-002).
- **FR-008**: An error created while the program's packages are being initialized MUST carry no
  location and no call stack. This is how package-level sentinel errors are declared. Such an
  error MUST behave like a standard library error with the same message: it is matched by
  identity, and the detail shows only its message. Two documented exceptions keep a location,
  because initialization cannot be recognized there:
  - errors created in a concurrent task started during initialization;
  - errors created at the bottom of a call stack deeper than the maximum depth of FR-016.

  In both cases the error carries more information, never less.

#### Wrapping

- **FR-009**: Developers MUST be able to wrap an error with a context message. The resulting
  message MUST be the context, a colon, a space and the cause's message. This MUST be identical to
  the standard wrapping of the same cause with the same context. When the context is empty, the
  message is the cause's message, unchanged.
- **FR-010**: Developers MUST be able to wrap an error with a formatted context. The context
  follows the standard formatting rules. The error-wrapping directive is not supported in a
  context, and its use MUST be reported by the toolchain's standard static checks.
- **FR-011**: Developers MUST be able to annotate an error with its location (FR-015) without
  changing its message.
- **FR-012**: Developers MUST be able to combine several errors into one, with the standard
  library's semantics:
  - nil errors are discarded;
  - combining only nil errors yields no error;
  - the message lists the combined errors' messages, one per line;
  - every combined error stays inspectable.

  The combination records its location.
- **FR-013**: Every wrapping, annotating and combining operation applied to no error (nil) MUST
  return no error, so that it can be applied unconditionally to a function's result.
- **FR-014**: Wrapping MUST NOT alter the cause: inspection finds the original error value.

#### Locations

- **FR-015**: Every error the library creates, wraps, annotates or combines MUST record the
  location of the code that asked for it: the function, the source file and the line. This MUST
  hold also when that code was inlined by the compiler.
- **FR-016**: When no error in the tree being wrapped already carries a call stack, the new error
  MUST also record the call stack that led to it, up to a documented maximum depth. A deeper stack
  is truncated, and the detail MUST say so. Otherwise only the location is recorded. As a result,
  each chain carries its call stack once, at its origin.
- **FR-017**: Code that builds errors on behalf of its caller, such as helper functions and
  constructors of custom error types, MUST be able to attribute the location and the call stack
  to a caller further up, by stating how many levels to skip. The numbering MUST be the same for
  every operation that accepts it: 0 designates the code that calls the library.
- **FR-018**: A location MUST identify its source file by the package import path and the file
  name, not by the directory where the program was built. The same program built in two different
  directories therefore renders identical details.

#### Error detail

- **FR-019**: The library MUST render the detail of any error, including errors it did not create.
  The detail lists the levels of the error from the outermost one to the root cause. For each
  level it shows:
  - the text the level adds to the message (FR-020);
  - its location, when the level has one;
  - its type, unless it is one of the library's own types or one of the standard library's
    plain-message and generic-wrapping types. Every other type is shown, including specific
    standard library types such as a path error or a system error number, because they tell the
    reader what kind of failure occurred.
- **FR-020**: The text of each level MUST appear once in the detail: it is the part of the message
  that the level adds, without the messages of its causes. When a level was produced by other code
  and its own part cannot be isolated, its full message is shown.
- **FR-021**: The call stack of an origin (FR-016) MUST be shown once, at the level that carries
  it, with one location per frame, from the most recent call to the oldest.
- **FR-022**: When a level has several causes, the detail MUST show them as branches of a tree.
  Each branch is labeled with its position and indented under its parent. Multi-line messages
  MUST stay indented within their entry, so that every line of the detail can be attributed to
  its level unambiguously.
- **FR-023**: The detail MUST be available in two ways: through an explicit operation that returns
  it as text, and through the language's standard formatting with the detailed verb (`%+v`).
  - The string verbs (`%v`, `%s`, `%q`, `%x`, `%X`), with any flags, width and precision, MUST
    produce exactly what they produce for a standard library error with the same message.
  - Every other verb formats the message string with the same directive. A standard error's
    output for those verbs depends on its own internal representation, so it cannot be
    reproduced.
- **FR-024**: The library MUST provide the information of FR-019 to FR-022 as structured data:
  - for each level: its own text, its type when shown, its location (function, file and line) and
    its position in the tree;
  - for each call stack: its ordered locations and whether it was truncated.

  The full message is not repeated for each level. It is the error's own message, and repeating
  it would make the size grow quadratically with depth, the same defect as the reference's text
  detail (SC-002). The structured form MUST serialize to JSON with stable, documented field
  names, so that log pipelines and tools can consume it without parsing text.
- **FR-025**: Rendering the detail, as text or as structured data, MUST always complete and MUST
  NOT fail or crash the program, whatever the input. The number of levels followed in depth, and
  in total, is bounded by documented maximums, and every cut MUST be marked in the output. The
  explicit rendering of no error (nil) is empty.

#### Custom error types

- **FR-026**: Developers MUST be able to define their own error types that carry a message, an
  optional cause, a location and a call stack (per FR-016). They do so by embedding a building
  block of the library, without writing message composition, unwrapping, formatting or detail
  support themselves.
- **FR-027**: Type-based inspection MUST find a custom error anywhere in a tree (FR-002). The
  detail MUST show the developer's type, not the building block's.
- **FR-028**: By default, the location of a custom error MUST be the caller of the developer's
  constructor function, which is the most common pattern. The developer MUST be able to choose
  another level with the numbering of FR-017, in which the default is level 1.
- **FR-029**: A custom error with a cause MUST compose its message as wrapping does (FR-009).
  Without a cause, its message is its own message.

#### Logging integration

- **FR-030**: The library MUST offer an opt-in helper that turns an error into one attribute of the
  standard library's structured logger.
  - The attribute carries the error message and the structured detail (FR-024) as nested
    attributes.
  - It uses a documented default key, which the developer can replace.
  - The detail MUST be computed only when the record is actually emitted, and not, for example,
    when its level is disabled.
  - Applied to no error, the helper adds nothing to the record.
  - An error logged without the helper MUST appear exactly as a standard library error with the
    same message would: the library's errors never change log records on their own.

### Key Entities

- **Error tree**: an error and the causes it wraps, recursively. It is usually a chain with one
  cause per level. Combining errors creates branches.
- **Level**: one error of the tree. It has its own text, its full message, its type, a location
  (optional) and its causes.
- **Location**: the function, the source file (package import path and file name) and the line of
  the code that created, wrapped, annotated or combined an error.
- **Origin**: the first level of a chain that the library recorded. It carries the call stack.
- **Call stack**: the ordered locations that led to an origin, from the most recent call to the
  oldest. It may be truncated.
- **Sentinel error**: a package-level error value that callers match by identity. It carries no
  location.
- **Custom error type**: a developer-defined type that embeds the library's building block and
  adds its own fields.
- **Detail**: the rendering of an error tree, either as text for people or as structured data for
  tools.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: For 100% of chains built with the library's wrapping operations, the message is
  identical to the equivalent chain built with the language's standard wrapping. Likewise, 100% of
  the identity and type inspections that succeed on one chain succeed on the other. Switching an
  existing package's import from the standard errors package to the library requires zero other
  code changes.
- **SC-002**: In the detail of a chain of N levels, each level's own text appears exactly once and
  the call stack appears once, so the size of the detail grows linearly with N.
- **SC-003**: In the detail of a 5-level chain spanning 3 packages, 100% of the levels that have a
  location, and 100% of the frames of the origin's call stack, show a function, a file and a line.
- **SC-004**: Wrapping an error that already carries a call stack costs at most 3 times the time
  and twice the memory of the standard wrapping of the same error, as measured by the module
  benchmarks. Recording the caller's location alone costs about as much as the whole standard
  wrapping. Creating an error with its call stack, at a call depth of 10, takes under 1.5
  microseconds on a typical developer machine.
- **SC-005**: Rendering the detail of any error completes within 100 milliseconds on a typical
  developer machine, without failure, and its output stays within the documented maximums. This
  includes self-referencing errors, chains of 10,000 levels and errors combining 1,000 causes.
- **SC-006**: The detail of the same error is byte-identical across builds of the same program in
  two different directories.
- **SC-007**: A custom error type with message, cause, location and detail support needs one
  embedded field and one constructor function, with zero hand-written message, unwrapping or
  formatting methods.
- **SC-008**: Adopting the library adds zero third-party dependencies to a consumer project.
- **SC-009**: 100% of the acceptance scenarios and edge cases in this spec are covered by
  automated tests, and statement coverage of the module is at least 80%. Two scenarios depend on
  the toolchain, which the default test run must not invoke:
  - the static check of User Story 1, scenario 5, is covered by an opt-in automated test;
  - the build-directory independence of User Story 2, scenario 6, is covered by an automated
    test of the location rules, and confirmed end to end by a manual check.

## Assumptions

- **Module and versioning**: one module, `github.com/paccolamano/go/errors`, released with
  `errors/vX.Y.Z` tags. Releases stay at 0.x until the API is considered stable, then 1.0.0.
- **Go version**: the minimum Go version is the oldest release the constitution supports (1.26 at
  the time of writing). The generic form of type-based inspection exists since that release.
- **Tracking the standard package**: when a later Go release adds a capability to the standard
  errors package, the library mirrors it in a later minor release, once the module's minimum Go
  version includes that capability. This keeps FR-001 true over time.
- **Original implementation**: the library is written from scratch. The reference (Apache-2.0)
  serves only as a functional benchmark, and no source is copied from it.
- **Defaults chosen in the plan**: the maximum call stack depth, the maximum detail depth and
  size, the exact text layout and the JSON field names are fixed and documented in
  `/speckit-plan` (FR-005).
- **Location format**: locations name the package import path and the file name (FR-018), so
  details are reproducible and do not disclose build-machine directories. Absolute paths that
  editors can open directly are not produced.
- **Out of scope**:
  - error codes and categories;
  - mapping errors to HTTP or process exit statuses;
  - separating user-facing from internal messages;
  - translating messages;
  - converting panics into errors.
- **Tests**: tests are hermetic and deterministic (Principle III). Benchmarks back the cost
  targets of SC-004.

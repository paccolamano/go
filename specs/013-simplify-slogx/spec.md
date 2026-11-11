# Feature Specification: A Simpler slogx, With a Threshold of Its Own (slogx 0.2.0)

**Feature Branch**: `013-simplify-slogx` (suggested; not yet created)

**Created**: 2026-11-10

**Status**: Draft

**Input**: User description: "slogx v0.2.0: simplify the API of the slogx module, keeping its
in-process capabilities". The full request, with the decisions already taken, is reproduced
verbatim in [Appendix: original request](#appendix-original-request).

## Context

The `slogx` module (spec `006-slogx-handlers`, released as `slogx/v0.1.0`) adds three things to
the standard library's structured logger, without a logger type of its own: attributes taken from
the context of each record, redaction of sensitive data, and a minimum level attached to the
context of one request. It also samples repeated records. One constructor composes these
capabilities in the only correct order, and each of them is also exported as a standalone handler.

Its first user, a service that writes its logs to stdout and exports them to OpenTelemetry, found
five problems:

1. **No threshold of its own.** Without a level in the context, slogx asks the next handler
   whether a level is enabled. When the next handler is the standard fan-out over stdout and the
   OpenTelemetry bridge, the bridge enables every level, so no threshold applies at all. The
   service put its own level handler above slogx. That handler is then the outermost one, so the
   per-request level of slogx no longer works.
2. **A second, silent level check.** The standard fan-out asks every output again whether it
   handles a record's level. An output built with default options has the threshold Info, so it
   silently drops the records that slogx enabled below Info: the debug records of one request, or
   every debug record when the service runs with a debug level. The OpenTelemetry output receives
   them. This was reproduced: a debug record enabled for one request reached OpenTelemetry and not
   stdout.
3. **Sampling mixes requests.** The sampler groups records by level and message and ignores their
   attributes. An access log writes "http request" at Info for every request, so above 10 requests
   per second it keeps 1 request in 100, and the records of a request ID reported by a user can be
   gone. Errors are never sampled by default, so the case that motivated sampling, the same error
   thousands of times while a database is down, is not covered. Sampling also requires flushing at
   shutdown, so the service keeps a registry of every handler it creates.
4. **Standalone handlers nobody composes.** No user composes the standalone handlers: the
   documentation itself says that the constructor composes them in the only correct order. Because
   one option type serves five constructors, building a handler can fail with "option does not
   apply" or "required option missing".
5. **The same extraction function everywhere.** Nearly every extraction function reads one value
   from the context and returns it as one attribute, as in the request identifier example that
   motivated the module.

This feature keeps what must happen inside the process (context attributes, redaction and the
per-request level) and simplifies the rest. One handler remains. It gains a default level of its
own, a fanout that passes the same records to several outputs without a second level check, and
a one-line way to log a context value. Sampling and the standalone handlers are removed.

| Requirement wording | Name in the module or in the request |
|---------------------|--------------------------------------|
| the handler, and building it | `Handler`, `New` |
| attaching attributes to a context | `With` |
| attaching a level to a context (the per-request level) | `WithLevel` |
| the default level | `WithDefaultLevel` |
| a level variable | `*slog.LevelVar` |
| an extraction function | `WithContextFunc` |
| a context value | `WithContextValue` |
| sensitive keys | `WithRedactKeys` |
| the secret type | `Secret`, `NewSecret` |
| the fanout | `Fanout` |
| the standard fan-out | `slog.NewMultiHandler` |
| invalid configuration | an error wrapping `ErrInvalidConfig` |
| the recorder | `slogxtest.Recorder` |

**Migration from `slogx/v0.1.0`** (the changelog entry of FR-026):

| 0.1.0 | 0.2.0 |
|-------|-------|
| `NewContextHandler(next, opts...)` | `New(next, opts...)` |
| `NewLevelHandler(next)` | `New(next)` |
| `NewRedactHandler(next, WithRedactKeys(keys...))` | `New(next, WithRedactKeys(keys...))` |
| `NewSamplingHandler`, `WithSampling`, `WithSampleErrors`, `Handler.Flush` | removed: delete the options and the calls to `Flush` |
| a level handler of one's own above `New` | `WithDefaultLevel` |
| an extraction function that returns one attribute | `WithContextValue` |
| the standard fan-out under `New`, meant to give every output the same records | `Fanout`, with `WithDefaultLevel` |

**Changes to spec 006**:

- FR-001 of this spec amends FR-024 of spec 006. With a default level, a record whose context
  carries no level is compared with the default level, and the next handler is not asked.
  Without one, the handler still defers to the next handler.
- FR-004 extends FR-025 of spec 006 to the default level, and FR-006 extends it to every output
  of the fanout.
- FR-009 amends the last point of FR-005 of spec 006 for the fanout: the errors of several
  outputs are combined.
- FR-016 removes FR-036 of spec 006 and amends FR-002 and FR-003 of spec 006: the module exports
  one handler and the fanout.
- FR-017 amends FR-007 of spec 006: the "option does not apply" and "required option missing"
  errors no longer exist. FR-005, FR-007 and FR-014 of this spec add new invalid configurations.
- FR-018 removes FR-027 to FR-033, SC-005 and SC-006 of spec 006, and the sampling parts of
  FR-034, FR-035 and SC-008 of spec 006.
- Spec 006 left out a fan-out because the standard library provides one. That reason does not
  hold when every output must receive the same records, because the standard fan-out checks the
  level again (problem 2). FR-006 to FR-011 add the fanout for that case only.

**Actors**:

- **Developer**: configures logging at startup, writes code that logs, and tests what is logged.
- **Operator**: runs services, reads their logs, and enables debug records for one request when
  diagnosing a problem.
- **Maintainer**: evolves the module and publishes its releases.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - slogx decides the level, whatever the next handler (Priority: P1)

A developer sets the level of a service once, on the slogx handler: Info in production, Debug
when the service starts with a debug flag. The level applies whatever the next handler is,
including one that enables every level. The operator can still enable debug records for one
request, or silence one.

**Why this priority**: today, with the most common production setup, stdout plus OpenTelemetry,
no threshold applies, and the workaround disables the per-request level, one of the module's
three capabilities.

**Independent Test**: build the handler with a default level over a next handler that enables
every level, then log records at every level, with and without a level in the context.

**Acceptance Scenarios**:

1. **Given** the default level Info, over a next handler that enables every level, **When**
   records at Debug and at Info are logged with a context that carries no level, **Then** only
   the Info record is written.
2. **Given** the default level Info, **When** a Debug record is logged with a context that
   carries the level Debug, **Then** it is written. **When** an Info record is logged with a
   context that carries the level Warn, **Then** it is not written.
3. **Given** the default level Debug, over an output whose own threshold is Info, **When** a Debug
   record is logged, **Then** the output writes it: the output's threshold is not consulted.
4. **Given** a default level held in a level variable set to Info, **When** the variable is set
   to Debug while the service runs, **Then** the next Debug record is written, without building
   the handler again.
5. **Given** no default level, **When** records are logged, **Then** the next handler decides,
   as in 0.1.0.
6. **Given** an absent default level value, **When** the handler is built, **Then** it reports
   invalid configuration.

---

### User Story 2 - The same records on every output (Priority: P1)

A developer writes the logs of a service to stdout and to OpenTelemetry. Both outputs receive
exactly the same records: the same levels, the same context attributes, the same redaction. A
level enabled by slogx is never dropped by an output on its own.

**Why this priority**: with the standard fan-out, an output built with default options silently
drops the records that slogx enabled below Info. The missing records are discovered when they are
needed.

**Independent Test**: build the handler with a default level over a fanout of two recording
outputs, one of them built with default options, then compare what each output received.

**Acceptance Scenarios**:

1. **Given** a fanout of stdout and OpenTelemetry under the default level Info, **When** an Info
   record and a Debug record are logged with a context that carries no level, **Then** both
   outputs receive the Info record and neither receives the Debug record.
2. **Given** the same setup, with stdout built with default options, **When** a Debug record is
   logged with a context that carries the level Debug, **Then** both outputs receive it.
3. **Given** a fanout whose stdout output was built with the threshold Error, **When** an Info
   record is enabled, **Then** stdout receives it too: the levels of the outputs are ignored, as
   the documentation states.
4. **Given** a fanout, **When** the handler is built over it without a default level, **Then**
   building it reports invalid configuration.
5. **Given** sensitive keys and a context that carries attributes, **When** a record is logged
   through a logger with an open group, **Then** every output receives the same record: the
   sensitive values redacted, and the context attributes at the top level.
6. **Given** a fanout whose stdout output is itself a handler that adds the trace and span IDs,
   **When** a record is logged, **Then** only stdout has those attributes, and no context
   attribute appears twice in it.
7. **Given** a fanout of three outputs where the second fails, **When** a record is logged,
   **Then** the first and the third receive it, and the handler returns the failure.

---

### User Story 3 - A context value in one line (Priority: P2)

A developer logs the request identifier, stored in the context by the request ID middleware, with
one option that names the key and the function that reads the value. They write no extraction
function.

**Why this priority**: this is the example that motivated the module. Today it takes a function
of six lines for each value, and every service writes the same one.

**Independent Test**: configure a context value whose function returns a known value, then
inspect the records logged with and without that value in the context.

**Acceptance Scenarios**:

1. **Given** a context value with the key `request_id`, **When** a record is logged with a
   context that carries the identifier `abc`, **Then** the record has `request_id=abc` at the top
   level.
2. **Given** the same configuration, **When** the context does not carry the identifier,
   **Then** the record has no `request_id` attribute.
3. **Given** a record that has its own `request_id` attribute, or a context with `request_id`
   attached, **When** it is logged, **Then** the record's or the attached value is written, and
   the context value is not (FR-014 and FR-015 of spec 006).
4. **Given** a context value whose value is a secret, or whose key is a sensitive key, **When** a
   record is logged, **Then** the value is written as `[REDACTED]`.
5. **Given** a context value whose function panics, **When** a record is logged, **Then** the
   record is written with the documented placeholder, and the program does not crash.
6. **Given** a context value with an empty key or without a function, **When** the handler is
   built, **Then** it reports invalid configuration.
7. **Given** a context value, **When** a disabled record is logged, **Then** the function is not
   called and no memory is allocated.

---

### User Story 4 - A smaller API, and the upgrade from 0.1.0 (Priority: P2)

A developer upgrades from 0.1.0. Code that uses only the constructor, without sampling, compiles
and logs exactly the same output. Code that uses a removed identifier fails to build, and the
changelog says what replaces it. Any combination of options is accepted.

**Why this priority**: the module is used in production. The release removes identifiers, so the
upgrade path must be explicit, and the output must not change where it was not meant to change.

**Independent Test**: compare, record by record, the output of 0.1.0 and of this release for the
same configurations without sampling. Check that every removed identifier appears in the
changelog with its replacement.

**Acceptance Scenarios**:

1. **Given** a configuration without sampling and without the new options, **When** the same
   records are logged with 0.1.0 and with this release, **Then** the outputs are byte-identical.
2. **Given** code that uses a removed identifier, **When** it is built, **Then** the build fails
   on that identifier, and the changelog maps it to its replacement or to its removal.
3. **Given** any combination of the options of this release, **When** the handler is built,
   **Then** no option is refused as "does not apply" and none as "required".
4. **Given** each former standalone use (context, level, redaction), **When** it is replaced by
   the constructor with the matching option, **Then** records logged with a context that carries
   no attributes and no level produce the same output.

---

### User Story 5 - Recipes for the common setups (Priority: P3)

A developer opens the README to set up logging for a new service: colored output while
developing, stdout plus OpenTelemetry in production, or different thresholds per output. Each
case has a recipe that they can copy.

**Why this priority**: the module works without the recipes. But both traps of the Context,
the missing threshold and the second level check, appear exactly when these setups are written by
hand.

**Independent Test**: read the README, and build each recipe in a separate project against the
libraries it names.

**Acceptance Scenarios**:

1. **Given** the colored-output recipe, **When** it runs in a terminal, **Then** the output is
   colored. **When** `NO_COLOR` is set or the output is not a terminal, **Then** it is not.
2. **Given** the stdout plus OpenTelemetry recipe, **When** it runs, **Then** it uses the fanout
   and the default level, and only stdout carries the trace and span IDs as attributes.
3. **Given** the recipe for different thresholds per output, **When** a developer reads it,
   **Then** it uses the standard fan-out, and it explains that each output applies its own level
   and that the per-request level does not go below it.
4. **Given** the README and the package documentation, **When** a developer reads them, **Then**
   neither mentions sampling or standalone handlers.

---

### Edge Cases

- **Default level given twice**: the last one applies.
- **Per-request level above the default level**: the per-request level applies, and silences the
  request's records below it.
- **Fanout with one output**: it behaves like that output, except that its level is not asked.
- **Fanout with no output, or with an absent output**: building the handler over it reports
  invalid configuration.
- **The same output given twice**: it receives each record twice, as given.
- **A fanout inside a fanout**: every output receives each record once per occurrence.
- **A fanout used on its own, outside the handler**: it reports every level as enabled, and passes
  every record to every output.
- **A fanout wrapped by another handler before the handler is built**: the handler cannot see it,
  so the default level is not required. The documentation says to give the fanout as the next
  handler directly.
- **A level variable changed while records are being logged**: each record follows the value
  read when its level is checked, and there is no data race.
- **A context value whose function returns a zero value as present**: the attribute is written
  with that zero value.
- **A context value and an extraction function that return the same key**: the one configured
  first wins (FR-014 of spec 006).

## Requirements *(mandatory)*

### Functional Requirements

#### The default level

- **FR-001**: Developers MUST be able to give the handler a default level. For a record whose
  context carries no level, the handler MUST decide whether the record's level is enabled by
  comparing it with the default level alone, without asking the next handler. This amends FR-024
  of spec 006. Without a default level, the handler MUST keep deferring to the next handler.
- **FR-002**: The per-request level MUST take precedence over the default level, both below and
  above it (FR-024 and FR-026 of spec 006).
- **FR-003**: The default level MUST be read at every decision, so that a level variable changed
  at run time applies from the next decision. This MUST be safe under concurrent logging.
- **FR-004**: A record enabled by the default level MUST reach the output even when the output's
  own threshold is higher (FR-025 of spec 006).
- **FR-005**: An absent default level value MUST be reported as invalid configuration. When a
  default level is given more than once, the last one MUST apply.

#### The fanout

- **FR-006**: Developers MUST be able to combine several outputs into one next handler, the
  fanout. The fanout MUST pass every record it receives to every output, in the order given,
  without asking any output whether it handles the record's level. Each output MUST receive its
  own copy of the record.
- **FR-007**: The handler built with a fanout as its next handler MUST require a default level:
  without one, building it MUST report invalid configuration. This holds also for a fanout to
  which attributes or groups were added. A fanout with no output, or with an absent output, MUST
  be reported as invalid configuration when the handler is built.
- **FR-008**: Attributes added to the logger and groups opened on it MUST reach every output.
  Context attributes and redaction MUST be applied once, before the fanout, so that every output
  receives the same attributes, with the context attributes at the top level (FR-013 of
  spec 006).
- **FR-009**: An output that fails MUST NOT prevent the following outputs from receiving the
  record. The handler MUST return the error unchanged when one output fails, and the errors
  combined when several do. This amends the last point of FR-005 of spec 006 for the fanout.
- **FR-010**: Used on its own, outside the handler, the fanout MUST report every level as
  enabled.
- **FR-011**: The documentation MUST state that the fanout ignores the levels of its outputs, and
  that the standard fan-out is the tool for deliberately different thresholds per output, where
  each output applies its own level and the per-request level does not go below it.

#### Context values

- **FR-012**: Developers MUST be able to configure a context value: an attribute key, and a
  function that reads a value from the context and reports whether the context carries it. For
  every enabled record whose context carries the value, the handler MUST add one attribute with
  that key and value. Otherwise it MUST add nothing.
- **FR-013**: A context value MUST follow the rules of extraction functions (FR-012 to FR-016 and
  FR-005 of spec 006). It is placed at the top level. The record's own attributes and the attached
  attributes win over it. Among extraction functions and context values, the one configured first
  wins. It is computed only for enabled records. A function that panics contributes the
  documented placeholder. The attribute is subject to redaction.
- **FR-014**: A context value with an empty key, or without a function, MUST be reported as
  invalid configuration.
- **FR-015**: The value MUST be logged as the standard logger logs any attribute value: a value
  with a log representation of its own is resolved, and a secret is written as `[REDACTED]`.

#### A smaller API

- **FR-016**: The module MUST export one handler, built by one constructor over a next handler,
  and the fanout. The standalone context, level, redaction and sampling handlers and their
  constructors MUST be removed. This removes FR-036 of spec 006 and amends FR-002 and FR-003 of
  spec 006.
- **FR-017**: Every option of the module MUST apply to the handler. The invalid configurations
  "option does not apply" and "required option missing" MUST no longer exist.
- **FR-018**: Sampling MUST be removed: its options, the flush operation and the summary records.
  This removes FR-027 to FR-033, SC-005 and SC-006 of spec 006, and the sampling parts of FR-034,
  FR-035 and SC-008 of spec 006.
- **FR-019**: The handler MUST apply, from the outside in: the level, the context attributes, the
  redaction when sensitive keys are configured, then the next handler. This is FR-035 of
  spec 006 without sampling.

#### Behavior kept

- **FR-020**: For a configuration that uses neither sampling nor the options added by this spec,
  the handler MUST produce the same output as `slogx/v0.1.0` for the same records.
- **FR-021**: The handler, the fanout and the recorder MUST pass the standard library's handler
  conformance suite (FR-003 of spec 006). A disabled record MUST allocate no memory, also with a
  default level, context values and a fanout configured (FR-004 of spec 006). No handler of the
  module may panic (FR-005 of spec 006), and all of them MUST be safe for concurrent use (FR-006
  of spec 006).
- **FR-022**: The module MUST keep depending on the standard library alone, its tests and
  examples included (FR-008 of spec 006).

#### Documentation and release

- **FR-023**: The package documentation and the README MUST describe the handler, the default
  level, the fanout and context values. They MUST NOT mention sampling or standalone handlers.
- **FR-024**: The README MUST contain three recipes:
  - colored output with `github.com/lmittmann/tint`, without colors when `NO_COLOR` is set or the
    output is not a terminal, as detected with `golang.org/x/term`;
  - stdout plus OpenTelemetry with the fanout and the default level, with the trace and span IDs
    as attributes only on stdout, through a handler nested on that output;
  - different thresholds per output with the standard fan-out (FR-011).

  The recipes are documentation: they add no dependency to the module (FR-022).
- **FR-025**: Runnable examples MUST show the default level, the fanout and a context value,
  using only outputs from the standard library.
- **FR-026**: The module's changelog MUST record, under the unreleased version:
  - the removed identifiers, as a breaking change;
  - the added identifiers;
  - the changed behavior (FR-017);
  - how to migrate each removed use from 0.1.0, as in the migration table of the Context.

### Key Entities

- **Handler**: the pipeline built over a next handler: level, context attributes, redaction.
- **Default level**: the threshold of the records whose context carries no level. It is a fixed
  level or a level variable.
- **Fanout**: a next handler made of several outputs, which receive every record without a level
  check of their own.
- **Output**: a handler that writes records somewhere, such as stdout or the OpenTelemetry
  bridge.
- **Context value**: an attribute key with the function that reads its value from the context.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: The test matrix combines the default levels Debug, Info, Warn and Error with no
  per-request level, a per-request level below the default and one above it. It uses a fanout of
  two outputs, one of them built with default options. In 100% of the combinations, both outputs
  receive exactly the same records: those whose level is at least the per-request level, or the
  default level when the context carries none.
- **SC-002**: A level variable used as the default level is changed while 50 concurrent tasks
  log. Every record whose level is checked after the change follows the new level, and race
  detection reports 0 data races.
- **SC-003**: In a test service where the request ID middleware stores the identifier, 100% of
  the records logged with the request's context carry it. The developer writes one option and no
  extraction function.
- **SC-004**: The module exports one handler and the fanout, instead of five handlers. Against
  `slogx/v0.1.0`, the API compatibility check reports exactly the removals and additions listed in
  the changelog.
- **SC-005**: For 100% of the tested configurations without sampling and without the new options,
  the output is byte-identical to that of `slogx/v0.1.0` for the same records.
- **SC-006**: A disabled record allocates 0 bytes, with a default level, two context values and a
  fanout of two outputs configured, as measured by the module's benchmarks.
- **SC-007**: For an enabled record with 5 attributes and 2 context attributes, logged through a
  logger with no open groups, the handler with redaction takes at most twice the time of the
  output alone writing the same 7 attributes.
- **SC-008**: 100% of the acceptance scenarios and edge cases of this spec are covered by
  automated tests. Statement coverage of the module is at least 80%. The module still adds zero
  dependencies beyond the standard library to a consumer project.
- **SC-009**: Each README recipe, built in a separate project against the current releases of
  the libraries it names, behaves as its text describes.

## Assumptions

- **Release**: the changes ship as `slogx/v0.2.0`. The module is at 0.x, where a minor release may
  break compatibility; the constitution requires a new major version only from v1.0.0
  (Principle II). The changelog marks the removals as breaking.
- **No user composes the standalone handlers**: no other module of this repository imports slogx,
  and its first external user calls only the constructor. Every former standalone use is the
  constructor with the matching option (User Story 4).
- **Sampling leaves the module with no replacement in it**: its problems (Context, point 3)
  outweigh its use, and slogx keeps only what must happen inside the process.
- **The fanout is meant as the next handler of the handler**: the handler recognizes it there
  (FR-007). A fanout hidden inside another handler is not recognized, and the documentation says
  so.
- **Errors of the outputs**: one failure is returned as is, and several are combined, as the
  standard fan-out does.
- **Names**: "default" names the level that applies when the context carries none. The
  per-request level is also a minimum level, so a name such as "minimum level" would not tell the
  two apart.
- **Recipes are verified outside the module**: the module cannot import the libraries the recipes
  name (FR-022). The recipes are built in a separate project before the release, against the
  versions current at that time.
- **Tests**: outputs are in-memory handlers and recorders, so the tests stay hermetic and
  deterministic (Principle III).

## Appendix: original request

The request, as submitted to `/speckit-specify`:

````text
slogx v0.2.0: simplify the API of the slogx module, keeping its in-process capabilities. Decisions already taken:
1. One public handler. Keep only New and Handler; remove the standalone ContextHandler, LevelHandler, RedactHandler and SamplingHandler with their constructors, and the option applicability and required-option checks (every option applies to New). This reverses FR-036 of spec 006: no user composes the standalone handlers, and the documentation itself says New composes them in the only correct order. Every former standalone use is New with the matching option.
2. Remove sampling: SamplingHandler, WithSampling, WithSampleErrors and Handler.Flush. Reasons: it groups records by level and message and ignores attributes, so records of different requests share a group (an access log that writes "http request" at Info for every request keeps 1 request in 100 above 10 requests per second, and the records of a reported request ID can be gone); errors are never sampled by default, so the motivating case (the same error thousands of times while a database is down) is not covered; it imposes Flush at shutdown and keeping every handler; volume can be handled downstream (OpenTelemetry collector logdedup and probabilistic sampling, journald and Loki limits). slogx keeps only what must happen inside the process: context attributes, redaction, per-request level.
3. WithDefaultLevel(slog.Leveler) Option: the minimum level of the records logged with a context that carries no level (WithLevel). When given, the next handler is not asked whether a level is enabled, as already happens for the context level. A nil Leveler is an invalid configuration. A *slog.LevelVar changes it at run time. Reason: with slog.NewMultiHandler(stdout, otelslog handler) as the next handler, the OpenTelemetry bridge enables every level, so slogx applied no threshold; users had to put their own level handler above slogx, which disables the per-request level.
4. WithContextValue[T any](key string, get func(context.Context) (T, bool)) Option: adds the attribute key, with the value get returns, to every record whose context carries it (get returns true). Example: WithContextValue("request_id", requestid.FromContext). It replaces the common hand-written extraction function; WithContextFunc stays for functions that return several attributes.
5. Fanout(outputs ...slog.Handler) slog.Handler: passes every record to every output, without asking the outputs whether they handle its level; the level is decided once, by New. New over a Fanout requires WithDefaultLevel, otherwise it reports an invalid configuration. The documentation states that the level of the outputs is ignored, and that slog.NewMultiHandler is the tool for deliberately different thresholds per output (each output applies its own level, and the per-request level does not go below it). Reason, verified: slog.NewMultiHandler asks every output again whether it is enabled, so an output built with nil options (Info) silently drops records that slogx enabled (a per-request debug level, or a default level of Debug), while the OpenTelemetry output receives them.
6. README recipes: colored output with github.com/lmittmann/tint (no colors when NO_COLOR is set or the output is not a terminal, detected with golang.org/x/term); stdout plus OpenTelemetry with Fanout, with the trace and span IDs as attributes only on stdout through a New nested on that output; different thresholds per output with slog.NewMultiHandler. The recipes are documentation: the module gains no dependency.
Unchanged: With, WithLevel, WithContextFunc, WithRedactKeys, Secret, slogxtest.Recorder, the composition order (level, then context attributes, then redaction, then the next handler), conformance with testing/slogtest, no panics, no allocation when a record is disabled.
Release: slogx/v0.2.0, a breaking minor release while the module is at 0.x; the CHANGELOG lists what is removed, added and changed, with how to migrate from 0.1.0.
````

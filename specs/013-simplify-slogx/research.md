# Research: A Simpler slogx, With a Threshold of Its Own (slogx 0.2.0)

**Feature**: [spec.md](spec.md) | **Plan**: [plan.md](plan.md) | **Date**: 2026-11-10

Each section records a decision, its rationale and the alternatives rejected.

The design was validated by a spike, kept outside the working tree: a clone of this repository in
a scratch directory, where `slogx/` was changed as described below and committed locally. In the
spike:

- the existing suite, migrated as in [R-09](#r-09-same-output-as-010-fr-020-sc-005-us4), passed
  under `-race -shuffle=on`, with new tests for every user story;
- `golangci-lint` with the repository configuration reported 0 issues;
- statement coverage was 98.1% for the module (97.9% for `slogx`);
- `gorelease` against `slogx/v0.1.0` reported exactly the identifiers of [R-01](#r-01-release-level-and-api-compatibility);
- 195 output sections were byte-identical to those of `slogx/v0.1.0`;
- the three README recipes were built and run in a separate project against the current releases
  of the libraries they name ([R-13](#r-13-readme-recipes-fr-024-sc-009)).

Measurements come from Go 1.27.1 on linux/amd64 and an Intel i7-11800H, averaged over 5 runs.

## R-01 Release level and API compatibility

- **Decision**:
  - The release is `slogx/v0.2.0`, a breaking MINOR release while the module is at 0.x.
  - `gorelease -base=v0.1.0`, run on the spike, reports:

    ```text
    ## incompatible changes
    (*Handler).Flush: removed
    ContextHandler: removed
    LevelHandler: removed
    NewContextHandler: removed
    NewLevelHandler: removed
    NewRedactHandler: removed
    NewSamplingHandler: removed
    RedactHandler: removed
    SamplingHandler: removed
    WithSampleErrors: removed
    WithSampling: removed
    ## compatible changes
    Fanout: added
    WithContextValue: added
    WithDefaultLevel: added

    # summary
    Suggested version: v0.2.0 (with tag slogx/v0.2.0)
    ```

  - The CHANGELOG lists exactly these 11 removals and 3 additions (SC-004), the removals marked
    **Breaking** as in `auth/v0.2.0`.
  - The feature PR is titled `feat(slogx)!: simplify the API, with a default level, a fanout and
    context values`. The release follows in `chore(slogx): prepare release 0.2.0`, which tags
    `slogx/v0.2.0`.
- **Rationale**:
  - Principle II requires a new MAJOR version only from v1.0.0. At 0.x, `scripts/apicompat.sh`
    prints incompatible changes without failing, and the suggested version matches the request.
  - The deprecation rule of Principle II applies to "the MAJOR version that removes the
    identifier". No MAJOR version is involved at 0.x, as plan 012 also noted ("At 0.x, nothing
    needs deprecating").
  - The `!` of Conventional Commits marks the breaking change in the squash commit.
- **Alternatives rejected**:
  - A `v0.1.1` that only adds `// Deprecated:` comments, before `v0.2.0` removes the
    identifiers: the constitution does not require it at 0.x. The only external user calls
    only `New`, and no module of this repository imports slogx (spec, Assumptions).
  - `v1.0.0`: the request fixes `v0.2.0`, and declaring the API stable is a separate decision.

## R-02 API shape: one handler, three additions

- **Decision**: after the change, package `slogx` exports:
  - `Handler` with `Enabled`, `Handle`, `WithAttrs`, `WithGroup`, and `New`, unchanged in
    signature;
  - `Fanout(outputs ...slog.Handler) slog.Handler`, which returns the unexported `*fanout`;
  - `WithDefaultLevel(slog.Leveler) Option` and
    `WithContextValue[T any](key string, get func(context.Context) (T, bool)) Option`;
  - unchanged: `With`, `WithLevel`, `WithContextFunc`, `WithRedactKeys`, `Option`,
    `ErrInvalidConfig`, `Secret`, `NewSecret` and the methods of `Secret`.

  `slogxtest` does not change. The full surface is in
  [contracts/public-api.md](contracts/public-api.md).
- **Rationale**:
  - The names and signatures are those of the request.
  - `Fanout` returns the interface, as the request states. `New` recognizes the unexported type
    with a type assertion (FR-007). An exported type would add an identifier and four methods
    that nobody calls outside the `slog.Handler` interface. Principle V says functions SHOULD
    return concrete types. This one does not, and the
    [Complexity Tracking](plan.md#complexity-tracking) table records why.
  - `WithContextValue` is generic so that an accessor such as `requestid.FromContext`, of type
    `func(context.Context) (string, bool)`, is passed as it is, with `T` inferred. This is the
    current use case that justifies the type parameter (Principle V).
- **Alternatives rejected**:
  - An exported `FanoutHandler` type: more surface, and nothing to call on it.
  - A named slice type, `slogx.Fanout{stdout, otel}`: it is not the requested signature, and the
    caller could modify the slice after `New` validated it.
  - An option `WithOutputs(...)` on `New`: two ways to give the next handler, and `next` would
    become optional.
  - `WithContextValue(key string, get func(context.Context) (any, bool))`: every accessor would
    need a wrapping closure, which is the code the option exists to remove.

## R-03 Options and configuration errors (FR-005, FR-007, FR-014, FR-017)

- **Decision**:
  - `Option` stays `func(*config)`. Each option checks its own arguments when `New` applies it,
    and records the first problem in the config. `New` then returns it. The applicability bit
    set (`optionSet`, `accepted`, `required`) is removed with the standalone constructors.
  - The error format is kept: `slogx: invalid configuration: New: <subject>: <reason>`, where
    the subject is the option, `next`, `options[i]` or `Fanout`. Every error wraps
    `ErrInvalidConfig`.
  - The checks, in the order `New` makes them:
    1. `next` is nil;
    2. an option is nil (`options[i]`);
    3. the first invalid option, in the order given;
    4. `next` is a fanout that is invalid (no output, a nil output, also in a nested fanout);
    5. `next` is a fanout and no default level is given.

    The messages are listed in [contracts/public-api.md](contracts/public-api.md#configuration-errors).
- **Rationale**:
  - With one constructor, every option applies, so "does not apply" and "required" cannot
    happen (FR-017). Checking each option where it is defined keeps its rules next to its
    documentation, and removes the second pass over the options.
  - Keeping `New:` in the message keeps the format of 0.1.0 for the errors that remain.
- **Alternatives rejected**:
  - A validation pass after all options, as in 0.1.0: it needs flags such as `noKeys` to
    remember what each call received. Recording the error in the option needs none.
  - Dropping `New:` from the messages: the format would change for no gain.

## R-04 The default level (FR-001 to FR-005)

- **Decision**:
  - `Handler` keeps the `slog.Leveler` given with `WithDefaultLevel`, nil without it.
    `Handler.Enabled` decides in this order:
    1. the context carries a level (`WithLevel`): enabled when `level >= ctx level`;
    2. a default level is given: enabled when `level >= default.Level()`;
    3. otherwise the next handler decides, as in 0.1.0.
  - `Level()` is called at every decision. A `*slog.LevelVar` reads an atomic value, so a change
    applies from the next decision, without a data race (FR-003).
  - A nil `Leveler`, or a nil `*slog.LevelVar`, is invalid configuration, also when a later
    `WithDefaultLevel` gives a valid one. Among valid ones, the last applies (FR-005).
  - The spike's matrix (SC-001) and its concurrency test (SC-002) passed under `-race`.
- **Rationale**:
  - The order is FR-001 and FR-002: the per-request level wins in both directions, and the next
    handler is asked only when neither level is set.
  - `slog.Logger` asks only the outermost handler, and the handlers under it never check the
    level again. So a record enabled by the default level reaches the output, whatever its own
    threshold (FR-004), as the per-request level already did (FR-025 of spec 006).
  - A nil `*slog.LevelVar` is the likely mistake (`var level *slog.LevelVar`). It would panic in
    `Enabled` at the first record. Checking it costs one type assertion at build time. A nil in
    any call is reported, like an empty key in any `WithRedactKeys` call, so that a later call
    does not hide a bug.
- **Alternatives rejected**:
  - `recover` around `Level()` in every `Enabled`: a cost on the hottest path, for a programmer
    error that `slog.HandlerOptions.Level` does not guard against either.
  - A reflection check for a nil pointer in any `Leveler`: `reflect` for a rare mistake, when the
    common case is `*slog.LevelVar`.
  - Calling `Level()` once in `New` to detect a panic: it still misses a `Leveler` that panics
    later, and it calls user code at build time.
  - Reading the level once at build time: a `*slog.LevelVar` could no longer change it (FR-003).

## R-05 The fanout (FR-006, FR-007, FR-009, FR-010)

- **Decision**:
  - `Fanout` copies its outputs into an unexported `*fanout`. It cannot return an error, so it
    records why the fanout is invalid, as a reason that `New` reports: "no output",
    `outputs[i]: must not be nil`, or the reason of an invalid nested fanout, prefixed with its
    index.
  - `Enabled` returns true for every level (FR-010).
  - `Handle` passes `r.Clone()` to each output in order, without calling `Enabled` (FR-006). It
    skips a nil output, so that an invalid fanout used on its own does not panic. It collects the
    errors in order: none gives nil, one is returned unchanged, several are combined with
    `errors.Join` (FR-009). Each output receives its record through the `handle` forwarder, so
    no new `//nolint` is needed.
  - `WithAttrs` and `WithGroup` return a new `*fanout` whose outputs have the attributes or the
    group, and which keeps the invalid reason. A fanout with attributes or groups is therefore
    still recognized (FR-007).
  - `New` recognizes a fanout only when it is `next` itself. A fanout inside another handler is
    not seen, and the documentation says to give the fanout as `next` directly.
  - The same output given twice receives each record twice, and a nested fanout is not
    flattened: each output receives a record once per occurrence (Edge Cases).
- **Rationale**:
  - The spike reproduced problem 2 of the spec. With a default level Info and a per-request level
    Debug, `slog.NewMultiHandler` over a JSON handler built with nil options and an all-levels
    recorder delivered the Debug record to the recorder only. `Fanout` delivered it to both.
  - `Record.Clone` does not allocate: it clips the record's attribute slice, so that an output
    that adds attributes copies it instead of writing into the slice of the next output.
  - `slog.MultiHandler` returns `errors.Join` even for a single error, which wraps it. FR-009
    requires the single error unchanged, as FR-005 of spec 006 does for one next handler. The
    spec's assumption that several errors are combined "as the standard fan-out does" refers to
    the combining.
  - A panic in an output is not recovered, as for the next handler in 0.1.0 and in
    `slog.MultiHandler`: the fanout itself never panics (FR-021).
- **Alternatives rejected**:
  - Validating nested fanouts by walking them in `New`: the same result, with more code than a
    reason computed when each fanout is built.
  - Flattening nested fanouts at construction: no observable difference, and more code.
  - Panicking in `Fanout` on a nil output: forbidden by FR-021 and Principle IV.
  - Recovering panics of the outputs: a policy that `slog.MultiHandler` and 0.1.0 do not have,
    and that would hide bugs in the outputs.

## R-06 Context attributes and redaction over a fanout (FR-008, US2-5, US2-6)

- **Decision**: no change to the context handler or to the redaction handler. The fanout is the
  next handler like any other.
- **Rationale**:
  - The context handler keeps two views of the next handler (R-04 of spec 006): the root, before
    the first group, and the next with every group. The fanout applies `WithAttrs` and
    `WithGroup` to each output, so both views exist for every output, and the slow path sends the
    rebuilt record to the root fanout.
  - Redaction runs before the fanout, so every output receives the same redacted record.
  - In the spike, a record logged through `logger.With(...).WithGroup("db").With(...)` with
    context attributes and sensitive keys reached two outputs as the same JSON line, apart from
    the trace IDs of the nested handler below.
  - A `New` nested on one output, which adds the trace and span IDs with `WithContextFunc`,
    writes no key twice (US2-6):
    - when the outer handler added context attributes, they are top-level attributes of the
      record that the nested one receives, so the record wins over the nested context
      attributes (FR-015 of spec 006);
    - when it added none, the nested handler receives the record with the groups open, and its
      own slow path places its attributes at the top level.
  - The fanout never calls `Enabled`, so the default level and the threshold of a nested `New`
    are never consulted. The documentation says so.
- **Alternatives rejected**: none. The existing design already composes.

## R-07 Context values (FR-012 to FR-015)

- **Decision**:
  - The context handler holds one ordered list of sources. Each source is either an extraction
    function (`WithContextFunc`) or a context value (`WithContextValue`): a key, and a function
    that returns a `slog.Value` and whether the context carries it. The generic option builds
    that function: it calls `get`, and converts the value with `slog.AnyValue`.
  - For an enabled record, in configuration order, a context value:
    1. is skipped without calling `get` when its key is already taken: by the record, by a
       top-level logger attribute, by an attached attribute, or by an earlier source;
    2. otherwise, adds `key=value` at the top level when `get` returns true, and nothing
       otherwise. A zero value returned as present is written.
  - A `get` that panics adds the placeholder of extraction functions,
    `!CONTEXTFUNC="panic: <value>"`, so that one placeholder is documented.
  - The value is logged as any attribute value is (FR-015). A `LogValuer` is resolved by the
    redaction or by the output, so a `Secret` is written `[REDACTED]`, and a sensitive key is
    redacted like any other context attribute.
  - `New` reports invalid configuration for an empty key, a key of only whitespace, and a nil
    `get` (FR-014).
  - The spike checked US3-1 to US3-7. In particular, `get` was called for the 2 enabled records
    whose key was free, and not for the disabled record or for those whose key was taken.
- **Rationale**:
  - One list keeps one precedence rule for both kinds of source: the first configured wins
    (FR-013).
  - Skipping a taken key avoids the call and the conversion when the result would be dropped
    anyway. An extraction function cannot be skipped this way, because its keys are known only
    after the call.
  - A key of only whitespace is rejected as `WithRedactKeys` rejects it: an attribute named
    `" "` is a mistake.
  - Converting a non-pointer `T` to `any` for `slog.AnyValue` costs one allocation for some
    types, such as a string. A hand-written extraction function also allocates its slice.
- **Alternatives rejected**:
  - Turning a context value into an extraction function that returns a one-element slice: one
    more allocation per record, and the function would run even when its key is taken.
  - A placeholder under the value's own key, such as `request_id="!PANIC: ..."`: a second
    placeholder to document. FR-013 asks for "the documented placeholder".
  - Accepting only a key of length 0 as empty: inconsistent with `WithRedactKeys`.

## R-08 Internal structure and what is removed (FR-016, FR-018, FR-019)

- **Decision**:
  - `Handler` holds the default level and the head of the chain. Its `Enabled` is the former
    `LevelHandler.Enabled` with the default level added (R-04). `level.go` is removed.
  - `ContextHandler` and `RedactHandler` become the unexported `contextHandler` and
    `redactHandler`. Their code is unchanged, except that `contextHandler` holds the sources of
    R-07 instead of a list of functions.
  - `sampling.go` is removed, with its tests, `Handler.Flush` and the shared sampler. `Handler`
    no longer holds a sampler.
  - `New` builds, from the inside out: the next handler, then `redactHandler` when sensitive keys
    are given, then `contextHandler`, then `Handler`. The order seen by a record is the level,
    the context attributes, the redaction, then the next handler (FR-019).
  - In the spike, the library shrank from 1,177 to about 780 lines of code, the package
    documentation aside.
- **Rationale**:
  - Keeping the context and redaction handlers as they are keeps the code that produced the
    output of 0.1.0, which FR-020 requires to stay the same.
  - Merging the level into `Handler` removes one layer, with no behavior attached to it.
- **Alternatives rejected**:
  - One struct that does everything: it rewrites tested code for no change in behavior.
  - Keeping the standalone types unexported but with their constructors: dead code.

## R-09 Same output as 0.1.0 (FR-020, SC-005, US4)

- **Decision**:
  - A new test, `TestNew_SameOutputAsV010` in `golden_test.go`, logs a matrix through `New` and
    compares the result with `testdata/v0.1.0.golden`, byte for byte.
  - The matrix has 5 configurations, 3 outputs and 13 cases, so 195 sections and 380 lines:
    - configurations: no option; an extraction function; sensitive keys; both; three extraction
      functions, one of them panicking, with two `WithRedactKeys` calls;
    - outputs: JSON at Info with default options, JSON at Warn so that the next handler decides,
      and text at Debug;
    - cases: plain, without a context, a nil context, every level, a per-request Debug level, a
      per-request Error level, groups, logger attributes, duplicate keys, a sealed group,
      `LogValuer` and `Secret` values, nested and inline groups, and an error value.
  - Times are removed with `ReplaceAttr`. A panicking `LogValue` is left out, because slog writes
    a stack trace with absolute file paths, which differ between checkouts. `panic_test.go`
    already covers it.
  - The file uses only identifiers that exist, with the same signatures, in both versions, and
    declares its own helpers. The golden file is produced on a checkout of `slogx/v0.1.0` with
    the flag `-golden.write`, as the [quickstart](quickstart.md) shows. It is never regenerated
    from the current code.
  - US4-4: the tests of the former standalone handlers keep their expected outputs byte for
    byte. Only the constructor changes: `NewContextHandler(next, opts...)`,
    `NewLevelHandler(next)` and `NewRedactHandler(next, opts...)` become `New(next, opts...)`. In
    the spike, every such test passed after the mechanical replacement.
  - Tests of removed behavior are deleted: option applicability and required options. The check
    that a sealed redaction handler returns itself, an internal property of a type that is no
    longer exported, moves to an internal test.
- **Rationale**:
  - The spike generated the golden file from `slogx/v0.1.0` and compared it with the spike: the
    two were identical.
  - A golden file is reviewable in the diff, needs no tooling, and keeps the test hermetic.
- **Alternatives rejected**:
  - Importing `slogx/v0.1.0` in a test: a module cannot require an older version of itself.
  - A differential program with `replace` directives, run in CI: committed `go.mod` files must
    not contain `replace` (Principle I), and it adds tooling.
  - Regenerating the golden file from the current code: it would compare the code with itself.

## R-10 Testing approach (Principle III, SC-001 to SC-008)

- **Decision**:
  - New tests, by story, each written before its code:

    | Story | Tests |
    |-------|-------|
    | US1 | default level over an all-levels output; per-request level below and above; an output whose own threshold is Info under a default Debug; a `*slog.LevelVar` changed at run time; no default level; nil `Leveler` and nil `*slog.LevelVar`; the SC-001 matrix; SC-002 |
    | US2 | the fanout under a default level; an output with default options; output thresholds ignored; no default level; open groups with redaction and context attributes; a nested `New` with trace IDs; a failing output, one and several; the edge cases; conformance |
    | US3 | present and absent value; record, logger and attached keys win; first configured wins; `Secret` and sensitive key; panic; empty key and nil function; not called and no allocation when disabled; SC-003 |
    | US4 | the golden test (R-09); the migrated tests; any combination of options accepted |

  - SC-001: default levels Debug, Info, Warn, Error × no per-request level, one 4 below and one
    4 above, × records at levels −8 to 12. The outputs are a JSON handler built with nil options
    and a `slogxtest.Recorder`, which enables every level like the OpenTelemetry bridge.
  - SC-002: 50 goroutines log Debug records at the default level Info, then the variable is set
    to Debug while one goroutine logs, then 50 goroutines log again. `sync.WaitGroup` separates
    the phases, with no sleep. The second phase must deliver every record, and `-race` reports
    nothing.
  - SC-003: a middleware written in the test stores an identifier in the request's context, as
    `requestid` does. The handler is called through `ServeHTTP` with `httptest.NewRecorder`,
    without a network listener. Every record of the request carries `request_id`. The real
    `requestid` middleware is exercised by the recipe check (R-13).
  - The behavior of the standard fan-out that FR-011 documents is pinned by a test: under
    `slog.NewMultiHandler`, an output at Info drops a request's Debug record, which an
    all-levels output receives.
  - Conformance (`slogtest.Run`): `Fanout` on its own, `New` over a `Fanout`, and the existing
    runs migrated to `New`.
  - Allocations: `New` over a two-output `Fanout`, with a default level, two context values and
    sensitive keys, allocates nothing for a disabled record (SC-006).
  - `FuzzRedact` is kept, built with `New`. The benchmarks lose sampling and gain a context
    value, a fanout of two outputs, and the standard fan-out for comparison.
  - Examples (FR-025): `ExampleWithDefaultLevel`, `ExampleFanout` and `ExampleWithContextValue`
    are added, with standard library outputs only. `ExampleWith`, `ExampleWithLevel`,
    `ExampleNew` and `ExampleSecret` are kept, built with `New`. `ExampleNewContextHandler` and
    `ExampleNewRedactHandler` become `ExampleWithContextFunc` and `ExampleWithRedactKeys`.
    `ExampleNewSamplingHandler` is removed.
  - Sampling tests are removed: `sampling_test.go` and `sampling_internal_test.go`.
- **Rationale**: every acceptance scenario and edge case maps to a test (SC-008). The tests stay
  deterministic and hermetic: in-memory outputs, no sleep, no network, no shared state.
- **Alternatives rejected**:
  - `testing/synctest` for SC-002: there is no time involved, and barriers are enough.
  - An `httptest.Server` for SC-003: a listener is not needed to call a handler.

## R-11 Cost (FR-021, SC-006, SC-007)

- **Decision**: the benchmarks keep the access log record of 0.1.0: 5 attributes, and a context
  with an attached `tenant` and a stored request identifier. The spike measured:

  | Benchmark | Disabled | Enabled |
  |-----------|----------|---------|
  | bare JSON handler, 5 attributes | — | 696 ns, 0 allocs |
  | bare JSON handler, the same 7 attributes | — | 861 ns, 80 B, 1 alloc |
  | `New`, extraction function, sensitive keys | 12 ns, 0 allocs | 1,269 ns, 176 B, 3 allocs |
  | the same, with an open group | 12 ns, 0 allocs | 1,343 ns, 304 B, 3 allocs |
  | `New`, context value, sensitive keys, default level | 9 ns, 0 allocs | 1,271 ns, 144 B, 3 allocs |
  | `New` over `Fanout` of 2 outputs, 2 context values, default level | 9 ns, 0 allocs | 2,197 ns, 320 B, 5 allocs |
  | `New` over `slog.NewMultiHandler` of 2 outputs, 1 context value | 16 ns, 0 allocs | 1,911 ns, 144 B, 3 allocs |

- **Rationale**:
  - SC-006: every disabled record allocates 0 bytes, also with a fanout and two context values.
  - SC-007: the handler with redaction and a context value takes 1.48 times the bare handler
    writing the same 7 attributes, within the limit of 2. With an extraction function instead,
    it takes 1.47 times. In 0.1.0, the composed pipeline, sampling included, took 1.6 times
    (R-14 of spec 006).
  - A fanout costs about the sum of its outputs, because each encodes the record.
  - With a default level, a disabled record costs one context lookup and one atomic load, less
    than asking the next handler.
- **Alternatives rejected**: avoiding the conversion of common `T` types to `any` with a type
  switch. The limit is already met, and it adds code for each type.

## R-12 Documentation and changelog (FR-023, FR-025, FR-026)

- **Decision**:
  - `doc.go` is rewritten, with these sections:
    - the overview: one handler, `New`, no logger type, then the composed example of the README;
    - "Context attributes": `With`, `WithContextFunc`, `WithContextValue`, placement and
      precedence, and the methods that take a context;
    - "Redaction and secrets": unchanged in substance;
    - "Levels": the per-request level, the default level, and the order of R-04;
    - "Several outputs": `Fanout`, which ignores the levels of its outputs and requires
      `WithDefaultLevel`, given as the next handler directly; and `slog.NewMultiHandler` for
      deliberately different thresholds, where each output applies its own level and the
      per-request level does not go below it (FR-011);
    - "Composition": the order level, context attributes, redaction, next handler, and why;
    - "Robustness": the placeholders, the depth bound, errors returned unchanged or combined,
      and no allocation for a disabled record.
  - The README is rewritten: purpose, `go get`, minimum Go version, the usage example, then
    sections for context attributes, redaction and secrets, levels, outputs, the three recipes of
    [R-13](#r-13-readme-recipes-fr-024-sc-009), and testing with `slogxtest`.
  - Neither mentions sampling or standalone handlers (FR-023, US5-4). The CHANGELOG is the only
    file of the module that names them, because FR-026 requires it.
  - The CHANGELOG entry under `[Unreleased]`, drafted in
    [contracts/public-api.md](contracts/public-api.md#changelog-entry), has the sections
    "Removed", with each item marked **Breaking**, "Added" and "Changed", followed by the
    migration table of the spec's Context.
- **Rationale**: FR-023 and FR-026 fix the content. The doc sections follow those of 0.1.0, minus
  sampling, plus levels and outputs.
- **Alternatives rejected**: a separate `MIGRATION.md`. The constitution requires every
  user-visible change in the CHANGELOG, and FR-026 places the migration there.

## R-13 README recipes (FR-024, SC-009)

- **Decision**: the README has three recipes. In the spike, each one was built and run in a
  separate module that required the spike's slogx through a local `replace`, never committed,
  and these versions, current on 2026-11-10:

  | Library | Version |
  |---------|---------|
  | `github.com/lmittmann/tint` | v1.2.1 |
  | `golang.org/x/term` | v0.48.0 |
  | `go.opentelemetry.io/contrib/bridges/otelslog` | v0.21.0 |
  | `go.opentelemetry.io/otel/trace`, `go.opentelemetry.io/otel/sdk`, `go.opentelemetry.io/otel/sdk/log` | v1.47.0 |
  | `go.opentelemetry.io/otel/exporters/stdout/stdoutlog` (check only, not in the recipe) | v0.23.0 |
  | `github.com/paccolamano/go/middleware` (`requestid`) | v0.1.0 |

  1. **Colored output**: `New(tint.NewHandler(os.Stdout, &tint.Options{NoColor: noColor}),
     WithDefaultLevel(slog.LevelDebug))`, where `noColor` is true when `NO_COLOR` is set and not empty, or
     when `term.IsTerminal(int(os.Stdout.Fd()))` is false. Checked: colors in a terminal (run
     through `script`), none when piped, none with `NO_COLOR=1`. The Debug record was written,
     although tint's own threshold is Info.
  2. **Stdout plus OpenTelemetry**:
     - `stdout` is `New(slog.NewJSONHandler(os.Stdout, nil), WithContextFunc(traceAttrs))`,
       where `traceAttrs` returns `trace_id` and `span_id` from
       `trace.SpanContextFromContext(ctx)` when it is valid;
     - the handler is `New(Fanout(stdout, otelslog.NewHandler(name,
       otelslog.WithLoggerProvider(provider))), WithDefaultLevel(level),
       WithContextValue("request_id", requestid.FromContext), WithRedactKeys(...))`, with
       `level` a `*slog.LevelVar`.

     Checked inside a span and behind the `requestid` middleware: stdout had `trace_id` and
     `span_id` as attributes. The OpenTelemetry records carried the same IDs in their own trace
     fields, and not as attributes. Both outputs had `request_id` and a redacted `token`. A Debug
     record was dropped at Info, and the request's Debug record reached both outputs.
  3. **Different thresholds per output**: `New(slog.NewMultiHandler(slog.NewJSONHandler(os.Stdout,
     &slog.HandlerOptions{Level: slog.LevelInfo}), otelHandler), WithDefaultLevel(slog.LevelDebug))`.
     The text explains that the default level is the lowest level any output receives, that each
     output then applies its own, and that a request's level does not go below an output's own.
     Checked: stdout received Info only, OpenTelemetry received Debug and Info, a request at
     Debug still reached OpenTelemetry only, and a request at Warn silenced an Info record on
     both.
- **Rationale**:
  - The recipes are the setups of the spec's Context, and each one avoids one of its traps: the
    missing threshold, or the second level check.
  - The third recipe keeps OpenTelemetry, because that is the setup users have. Without
    `WithDefaultLevel`, the bridge would enable every level and no threshold would apply: the
    first problem of the spec.
  - `WithContextFunc` stays the right tool for the trace IDs, because it returns two attributes
    (the request, point 4). `requestid.FromContext` already has the signature that
    `WithContextValue` takes.
  - The recipes are documentation. The module's `go.mod` does not change (FR-022).
- **Alternatives rejected**:
  - A third recipe without `WithDefaultLevel`: with the OpenTelemetry bridge, no threshold would
    apply.
  - A minimum-severity processor of the OpenTelemetry SDK to give the bridge a level of its own:
    one more library to explain, for a threshold that `WithDefaultLevel` already gives.
  - Recipe programs committed in the repository, for example under `examples/`: they would be a
    module with third-party dependencies to maintain and scan, for code that runs once before
    the release (spec, Assumptions).

## R-14 Lint and dependencies (FR-022)

- **Decision**:
  - No dependency is added. `go.mod` stays `go 1.26.0` with no `require`, so
    `go mod tidy -diff` stays clean. `slog.NewMultiHandler`, new in Go 1.26, appears only in
    tests and in the README.
  - The only `//nolint` left is `wrapcheck` on the `handle` forwarder. The `contextcheck` one of
    the sampling summaries goes with sampling.
  - The `-golden.write` flag of `golden_test.go` is a package variable of a test file, which the
    shared configuration exempts from `gochecknoglobals`.
- **Rationale**: the spike reported 0 issues with these rules.
- **Alternatives rejected**: none.

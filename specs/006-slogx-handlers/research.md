# Research: Composable Structured Logging Handlers (slogx)

**Feature**: [spec.md](spec.md) | **Plan**: [plan.md](plan.md) | **Date**: 2026-10-19

Each section records a decision, its rationale and the alternatives rejected. Standard library
APIs were checked against the local Go 1.27 toolchain and are all available in Go 1.26, the
module's minimum version (`go vet`'s `stdversion` check passes with `go 1.26.0`).

The risky decisions were validated by a throwaway spike, kept outside this repository: a minimal
module with this module's path, built, tested with `-race` and linted with the repository's
golangci-lint configuration. The measurements below come from that spike, run on Go 1.27.0,
linux/amd64 and an Intel i7-11800H, averaged over 5 runs.

## R-01 Module layout, versioning and Go version

- **Decision**:
  - One module, `github.com/paccolamano/go/slogx`, in the top-level directory `slogx/`, with
    two packages: `slogx` (the handlers) and `slogx/slogxtest` (the recorder). Helpers shared by
    both live in `slogx/internal/attrs`.
  - The `go` directive is `go 1.26.0`. CI tests on 1.26 and 1.27.
  - Releases start at `slogx/v0.1.0` and stay at 0.x until the API is declared stable.
  - `go.work` is regenerated with `make work`.
- **Rationale**:
  - FR-001 names the directory, the package and the test-support package.
  - The recorder lives in its own package, so that `slogx`'s production API stays free of test
    helpers. This mirrors `net/http/httptest` and `testing/slogtest`.
  - `internal/attrs` holds the attribute-tree helpers that both packages need: inlining of
    empty-key groups, the depth bound and its placeholder (R-12). It has two consumers, so it
    is not a speculative layer.
- **Alternatives rejected**:
  - A `slogx/handlers/...` subpackage per handler: the handlers are small, share the option type
    and are composed by `New`, so splitting them only adds import paths.
  - Putting the recorder in `slogx`: test helpers would become part of the production API.

## R-02 API shape

- **Decision**:
  - No logger type (FR-001). The module exports handlers, two context functions (`With`,
    `WithLevel`), the secret type and options.
  - Every handler is an exported concrete type with an exported constructor:
    `NewLevelHandler`, `NewContextHandler`, `NewRedactHandler`, `NewSamplingHandler` and the
    composed `New`. Each constructor has the signature
    `(next slog.Handler, opts ...Option) (*T, error)`.
  - One `Option` type serves every constructor. A constructor that receives an option that does
    not apply to it returns an error wrapping `ErrInvalidConfig`. An option it requires but did
    not receive also returns that error: `NewRedactHandler` needs `WithRedactKeys`, and
    `NewSamplingHandler` needs `WithSampling`.
  - Names follow the request: `With`, `WithLevel`, `WithContextFunc`, `WithRedactKeys`,
    `WithSampling`, `Secret[T]`, `NewRecorder`, `Last`. Additions: `WithSampleErrors` (FR-029),
    `NewSecret` and `Reveal` (FR-023), and `Flush` (FR-030).
  - The full surface is in [contracts/public-api.md](contracts/public-api.md).
- **Rationale**:
  - Concrete return types follow Principle V. They also make `Flush` reachable on the handlers
    that sample, which the `slog.Handler` interface could not expose.
  - Constructors return errors, as the `middleware` constructors do. FR-007 forbids panics and
    silent fallbacks. As a consequence, the request's one-line example,
    `slog.New(slogx.New(...))`, becomes two lines: `h, err := slogx.New(...)`, then
    `slog.New(h)`.
  - A single option type keeps the request's call shapes:
    `NewContextHandler(next, WithContextFunc(fn))` and `New(next, WithContextFunc(fn), ...)`.
    Inapplicable options fail at construction, so a mistake shows up the first time the program
    starts, and never silently.
  - Every constructor takes `...Option`, `NewLevelHandler` included. A later option can then be
    added without changing a signature, which `gorelease` would report as incompatible.
- **Alternatives rejected**:
  - One option type per handler (`ContextOption`, `RedactOption`, ...), plus an interface for
    `New`: compile-time checks, but four more exported types, and options that would have to
    implement several interfaces.
  - Positional parameters on the standalone constructors (`NewRedactHandler(next, keys...)`):
    no option could ever be added without breaking the signature.
  - Constructors that panic on invalid configuration, like `slog.New(nil)`: forbidden by FR-007
    and Principle IV.
  - Returning `slog.Handler`: it hides `Flush`, and goes against Principle V.

## R-03 Context storage

- **Decision**:
  - `With(ctx, attrs...)` stores an unexported `*ctxAttrs{attrs []slog.Attr}` under an unexported
    key. The slice is built once, at `With` time, as the merge of the parent's attributes and the
    new ones:
    - empty attributes are dropped, and groups with an empty key are inlined;
    - when a key repeats, the most recent value is kept, in the position of the new attribute.

    The stored slice is never modified afterwards.
  - `WithLevel(ctx, level)` stores the `slog.Level` under another unexported key.
  - A nil `ctx` is treated as `context.Background()`, because `context.WithValue` panics on a nil
    parent (FR-005). `With` with no non-empty attribute returns `ctx` unchanged.
- **Rationale**:
  - Merging at `With` time moves the work off the logging path. `Handle` reads one context value
    and iterates a ready, deduplicated slice (FR-014, latest attached value wins).
  - Contexts are immutable, so the parent never sees the child's attributes (US1-4, FR-010).
  - A pointer value avoids boxing a slice header at each lookup. `ctx.Value` and a type
    assertion do not allocate, which keeps FR-004 true.
- **Alternatives rejected**:
  - A linked list of `With` calls, walked at `Handle`: no cost at `With`, but every record pays
    for the walk and the deduplication.
  - Accepting `...any` like `slog.Logger.With`: it boxes every argument and brings back
    `!BADKEY` handling. Typed `slog.Attr` is what `LogAttrs` users already write.

## R-04 Context attributes at the top level

- **Decision**: `ContextHandler` keeps two views of the next handler:
  - `root`: the next handler with only the attributes added before the first group;
  - `next`: `root` with every later `WithGroup` and `WithAttrs` applied, as usual.

  It also records the operations applied after the first group (`ops`), and the keys of the
  top-level logger attributes (`topKeys`). `Handle` then takes one of three paths:
  - **No context attributes**: the record goes to `next` unchanged (FR-016). This path does no
    work beyond one context lookup.
  - **Fast path, no open group**: a new record carries the context attributes, then the record's
    own attributes. It goes to `next`.
  - **Slow path, open groups**: a new record carries the context attributes, then the open
    groups. The groups are rebuilt from `ops` as nested group attributes, which wrap the logger
    attributes and the record's attributes. The new record goes to `root`. Example: after
    `WithGroup("G").WithAttrs(a).WithGroup("H")`, the record's attributes `c` are sent as
    `G{a, H{c}}`.
- **Rationale**:
  - FR-013 requires the context attributes outside any open group. A wrapped handler cannot add
    an attribute above a group that its inner handler has already opened. The only correct
    route is to the handler as it was before the groups, with the groups rebuilt in the record.
  - Rebuilt groups are equivalent to `WithGroup` for the standard handlers. The spike ran
    `slogtest` with an extraction function that always returns an attribute, which forces the
    slow path on every case. All cases passed, including empty groups, nested empty groups and
    inlined groups.
  - The slow path loses the inner handler's pre-formatting of grouped logger attributes. It
    applies only to records that have context attributes and an open group. Measured: 1,364 ns,
    against 1,348 ns for the fast path (R-14).
  - Context attributes come right after the top-level logger attributes in both paths, so the
    output order does not depend on the path.
- **Alternatives rejected**:
  - Rebuilding the handler chain per record (`root.WithAttrs(ctx).WithGroup(...)`): it
    allocates a handler, and pre-formats attributes, for every record.
  - Adding the context attributes to the record inside the group: this violates FR-013, which
    is the request's "the request_id must always be in the same place".

## R-05 Duplicate keys (FR-014, FR-015)

- **Decision**:
  - A context attribute is skipped when its key equals the key of:
    - a top-level logger attribute (`topKeys`);
    - on the fast path, a top-level attribute of the record.
  - On the slow path, the record's attributes are inside a group, so they never collide with a
    top-level key.
  - Among context attributes, attached ones are added first. An extracted attribute is added
    only if no attached attribute, and no earlier extraction function, produced its key.
  - Comparison is exact (case-sensitive), as JSON keys are. Duplicates among the record's own
    attributes are left alone.
- **Rationale**:
  - These are the precedence rules of FR-014 and FR-015 (clarification of 2026-10-19).
  - Checking is linear in the number of attributes, with no allocation, because the record's
    attributes are scanned with `Record.Attrs`. Context and record attributes are few, so a map
    would cost more than it saves.
- **Alternatives rejected**:
  - Keeping duplicates, as the standard library does: rejected by the clarification.
  - Removing duplicates among the record's own attributes: out of scope, and it would change
    the behavior of records logged without this module.

## R-06 Extraction functions

- **Decision**:
  - `WithContextFunc(fn func(context.Context) []slog.Attr)` can be given several times. Each
    function is called once per handled record, in configuration order, and only on `Handle`,
    never on `Enabled`.
  - Groups with an empty key in the result are inlined, and attributes with an empty key are
    dropped.
  - Each call is guarded by `recover`. When the function panics, the record is still handled,
    with the attribute `!CONTEXTFUNC` set to `"panic: <value>"` in place of its attributes
    (FR-005).
- **Rationale**:
  - Calling only on `Handle` keeps FR-004: a disabled record never calls user code.
  - The `!` prefix follows slog's own marker for malformed attributes (`!BADKEY`). It is
    unlikely to collide with real keys, and easy to search for.
  - `fmt.Sprintf("%v")` already recovers from panicking `Error` and `String` methods, so
    formatting the panic value cannot panic.
- **Alternatives rejected**:
  - Dropping the failing function's attributes silently: it hides a bug in the program.
  - Letting the panic propagate: FR-005 forbids it.

## R-07 Redaction algorithm

- **Decision**:
  - **Matching**: the keys are kept as given. A key matches when `strings.EqualFold` reports it
    equal to a configured key: case-insensitive, on the whole key, never as a substring
    (FR-017).
  - **Order of checks**: the key is checked before the value is resolved. A matching attribute
    becomes `slog.String(key, "[REDACTED]")`, and its value is never resolved: a `LogValue`
    method of a sensitive value never runs. Otherwise the value is resolved, and when it is a
    group, its children are checked recursively (FR-018, FR-019).
  - **Handle**: a first pass, which does not allocate, checks whether any attribute needs work:
    a matching key, a `LogValuer` value, or a group that contains one. When none does, the
    record is passed through unchanged. Otherwise a new record is built with the processed
    attributes. Resolved values are passed on, so the output handler does not call `LogValue`
    a second time.
  - **WithAttrs**: logger attributes are processed the same way before they reach the next
    handler's `WithAttrs`.
  - **WithGroup with a matching name** "seals" the handler. The parent level gets the attribute
    `name=[REDACTED]`, through the next handler's `WithAttrs`. From then on, the sealed handler
    drops every attribute: those added later to the logger, and those of each record. The
    message, level and time are still written (FR-020).
  - Values are never inspected beyond attribute keys and groups (FR-021).
- **Rationale**:
  - `EqualFold` does not allocate, unlike lowercasing each key. With 4 keys and 5 attributes,
    the measured cost is +112 ns per record (R-14).
  - The two-pass `Handle` costs nothing extra in the common case of flat attributes with no
    match. The spike measured 0 allocations on that path.
  - Sealing makes `logger.WithGroup("password")` produce `"password":"[REDACTED]"`, the same
    shape as a matching group attribute. Its content could not be redacted in place, because the
    next handler has already opened the group. Spike output:
    `{"msg":"sealed",...,"TOKEN":"[REDACTED]"}`.
- **Alternatives rejected**:
  - A map of lowercased keys: the attribute key would have to be lowercased, which allocates,
    to look it up.
  - Path patterns such as `header.authorization`: the spec matches keys at every depth, so a
    plain key already covers the accesslog header group (US2-2).
  - Wrapping `ReplaceAttr` of the standard handlers: it exists only on the built-in handlers,
    it does not see values hidden inside `LogValuer` groups, and it is called for each output
    attribute, not on `WithAttrs`.

## R-08 Secret type (FR-023)

- **Decision**:
  - `type Secret[T any] struct{ p *T }`, built with `NewSecret(v)` and read with `Reveal()`. The
    zero value reveals the zero `T`.
  - Value-receiver methods, so that both `Secret[T]` and `*Secret[T]` are covered:
    - `LogValue()` returns `slog.StringValue("[REDACTED]")`;
    - `String()` returns `"[REDACTED]"`;
    - `Format(fmt.State, rune)` writes `"[REDACTED]"` for every verb, ignoring flags and width;
    - `MarshalText()` returns `"[REDACTED]"`. `encoding/json` uses it, so the value encodes as
      the string `"[REDACTED]"`, including when it is a struct field.
- **Rationale**:
  - **Pointer field**: `fmt` cannot call methods on unexported struct fields. With a plain
    `T` field, `fmt.Sprintf("%v", struct{ s Secret[string] }{...})` would print the raw value,
    and so would slog's `TextHandler`, which formats structs with `%+v`. With a pointer field,
    `fmt` prints only an address. The spike checked `%v %+v %#v %s %q %x %d %10v`, JSON, and the
    text handler, on direct values and on exported and unexported fields. `hunter2` (and its hex
    encoding) appeared in none of them.
  - `MarshalText` alone covers JSON, XML and most text encoders. An extra `MarshalJSON` would be
    redundant.
- **Limits** (documented):
  - Reflection-based deep printers that read unexported fields and follow pointers can still
    reach the value.
  - `encoding/gob` refuses the type, because it has no exported field.
- **Alternatives rejected**:
  - A `T` field: it leaks through unexported fields, as shown above.
  - A closure field: same protection, but it costs more and makes the type harder to compare.

## R-09 Per-request level (FR-024 to FR-026)

- **Decision**:
  - `LevelHandler.Enabled` reads the context level. When one is present, it returns
    `level >= ctxLevel`, without asking the next handler. Otherwise it asks the next handler.
  - `Handle`, `WithAttrs` and `WithGroup` forward. A nil context is handled like one without a
    level.
  - In `New`, `LevelHandler` is outermost (R-11).
- **Rationale**:
  - `slog.Logger` asks only the outermost handler's `Enabled`, and the standard output handlers
    do not check the level again in `Handle`. A record enabled by the context level therefore
    reaches the output (FR-025). The spike confirmed it: a debug record went through a JSON
    handler at Info.
  - The override works in both directions (spec, Assumptions), so `WithLevel(ctx, Warn)`
    silences info records for that request.
  - Reading the context level does not allocate. `WithLevel` boxes the level once, when the
    context is created.
- **Alternatives rejected**:
  - Lowering only, `min(ctxLevel, next threshold)`: the next handler's threshold is not
    observable through `slog.Handler`. Only `Enabled(level)` is, so a lowering-only rule cannot
    be computed.

## R-10 Sampling algorithm (FR-027 to FR-033)

- **Decision**:
  - **State**: one sampler per `NewSamplingHandler` or `New`, shared by every derived handler. It
    holds a `sync.Mutex`, the start of the current interval and a
    `map[groupKey]groupCount`, where `groupKey{level, msg}` and `groupCount{n, dropped int64}`.
  - **Decision per record**: under the lock:
    1. If the current interval has ended, the sampler collects the summaries of the groups with
       drops, clears the map and starts a new interval at the current time.
    2. A record at Error level or above passes, unless `WithSampleErrors` was given.
    3. Otherwise the group's `n` is incremented. The record passes when `n <= first` or
       `(n - first) % thereafter == 0`, and `dropped` is incremented otherwise.
    4. A group not yet in the map, while the map holds 1,024 groups, passes untracked.

    Summaries are emitted after the lock is released.
  - **Interval**: a tumbling window measured with `time.Now()`, which is monotonic. It starts at
    the first record and restarts when a record arrives after it has ended. Tests use
    `testing/synctest` for virtual time (R-16).
  - **Summary records**: one per group with drops. See
    [contracts/record-semantics.md](contracts/record-semantics.md#sampling-summary):
    - the time is the emission time, and the level is the group's level;
    - the message is `records dropped by sampling`;
    - the attributes are one group, `sampling`, with `msg`, `dropped`, `start` and `end`;
    - summaries are sorted by message, then level.

    They are sent to the sampler's own next handler as it was at construction, with
    `context.Background()`, so they carry neither the logger attributes of the record that
    triggered them nor any request attribute.
  - **Flush(ctx)**: closes the current interval early (`end` is the flush time), emits its
    summaries with `ctx`, and starts a new interval. Errors from the next handler are joined
    with `errors.Join` and returned.
  - **No goroutine**: summaries are emitted by the next record handled after the interval ends,
    or by `Flush` (FR-030, FR-033).
  - **Validation**: `first >= 1`, `thereafter >= 1` and `interval > 0`. Anything else returns
    `ErrInvalidConfig`.
- **Rationale**:
  - **Exact counts** (FR-032): a mutex makes the check and the update atomic. The spike ran 50
    goroutines × 200 records under `-race`, plus 5 Error records, with `first=10` and
    `thereafter=100`. It counted exactly 109 + 5 written records and one summary of 9,891
    (SC-005).
  - **Cost**: the lock is not a bottleneck, because the output handler serializes writes with
    its own mutex anyway. Measured: 788 ns against 669 ns for the bare handler, and 236 ns per
    record in parallel when records are dropped, with 0 allocations.
  - **Bound** (FR-031): 1,024 groups. A group costs about 100 bytes plus its message, so the
    bound caps the table at around 100 KB plus the retained messages. Messages are usually
    constants, so they are retained anyway. Passing untracked groups keeps every record of a
    new message: none is dropped for lack of space.
  - **First record**: `first >= 1` guarantees that the first occurrence of a message is always
    written.
  - **Background context**: it is what keeps request attributes off the summaries (FR-030,
    US6-7). The spike verified that a summary triggered inside a request carries no
    `request_id`.
- **Alternatives rejected**:
  - Atomic counters with an atomic reset time, as zap does: lock-free, but counts are lost at
    interval boundaries. This violates FR-032.
  - A ticker goroutine that emits summaries on time: it needs `Close` and lifecycle management
    (FR-033), and it brings real time into tests.
  - Hashing messages into a fixed counter array: collisions would mix the counts of unrelated
    messages, and a summary needs the message text.
  - Using the summary's original message as its message: it makes summaries look like ordinary
    occurrences in dashboards that count by message. With a fixed message, every summary can
    be found with one query.

## R-11 Composition order and the composed handler

- **Decision**: `New(next, opts...)` builds, from the outside in:

  ```text
  LevelHandler → SamplingHandler (if WithSampling) → ContextHandler → RedactHandler (if WithRedactKeys) → next
  ```

  It returns a `*Handler` that delegates to that chain and keeps a pointer to the sampler, for
  `Flush`. `Flush` without sampling returns nil. `WithAttrs` and `WithGroup` return a `*Handler`
  that shares the sampler.
- **Rationale**:
  - **Level outermost**: `Enabled` must not be answered by the output handler first (FR-035).
  - **Sampling before context and redaction**: a dropped record costs no extraction and no
    redaction. Summaries enter below the sampler with a background context, so they carry no
    request attribute (FR-030).
  - **Context before redaction**: attached and extracted context attributes go through
    redaction (FR-035; US2-5, US5-1).
  - **Same chain**: the documented manual composition is exactly this chain, built with the
    standalone constructors, so the outputs are identical (FR-036).
- **Alternatives rejected**:
  - Sampling innermost: every dropped record would first pay for extraction and redaction, and
    summaries would bypass redaction while carrying nothing redactable anyway.

## R-12 Panic safety and placeholders (FR-005)

- **Decision**:
  - **Values**: every resolution uses `slog.Value.Resolve`. When a `LogValue` method panics,
    `Resolve` recovers and returns slog's own placeholder: an error value whose message begins
    with `LogValue panicked`. The module documents this placeholder and does not invent
    another.
  - **Extraction functions**: they are recovered as in R-06, giving `!CONTEXTFUNC`.
  - **Depth**: redaction and the recorder follow groups at most 64 levels deep. A deeper group
    value is replaced by the string `!DEPTH`. A `LogValuer` that returns a group containing
    itself would otherwise recurse until the stack overflows, which is a fatal error that
    `recover` cannot catch.
  - **Nil context**: every context read guards against nil.
  - **Errors from the next handler** are returned unchanged.
- **Rationale**:
  - Reusing `Resolve` keeps the placeholder identical whether the redaction handler or the output
    handler resolves the value. Without redaction keys, only the output handler resolves.
  - The spike logged a panicking `LogValuer` and a self-referencing one through the redaction
    handler. Both records were written, with the placeholder and with `!DEPTH`.
  - The standard JSON and text handlers already recover from panics while encoding a value
    (`!PANIC:`), so nothing that reaches them can crash the program.
- **Alternatives rejected**:
  - A placeholder of the module's own, without slog's stack excerpt: the placeholder would then
    depend on which stage resolved the value. Values left unresolved by this module would still
    get slog's.

## R-13 Test recorder (FR-037 to FR-041)

- **Decision**:
  - `slogxtest.NewRecorder(opts ...Option) *Recorder`. Its only option is
    `WithLevel(slog.Leveler)`. By default every level is enabled, and so is a nil `Leveler`.
  - **Derived recorders**: `WithAttrs` and `WithGroup` return recorders that share the store
    and record the operations. `WithAttrs` resolves values immediately, as the standard
    handlers do.
  - **Handle** normalizes the record into a tree, as an output handler would write it:
    - logger attributes and open groups are folded around the record's attributes, as in the
      slow path of R-04;
    - every value is resolved;
    - empty attributes are dropped, groups with an empty key are inlined and empty groups are
      dropped;
    - depth is bounded as in R-12.

    The result is appended under a mutex.
  - **Captured records**: `Record{Time, Level, Message, Attrs}` is a plain value.
    `Record.Attr(path ...string) (slog.Value, bool)` follows the path through groups. When a
    key repeats at one level, the last occurrence wins, as it would in a JSON decoder.
  - **Recorder methods**: `Records()` returns a copy, `Len()` the count, and `Last()` returns
    `(Record, bool)`.
- **Rationale**:
  - The recorder covers every use of the two middleware capture handlers (FR-041): the list of
    records, the message, the level, top-level and grouped attributes (`header`), the count,
    and a minimum level (accesslog's `minLevel`). Unlike those handlers, it honors `WithAttrs`
    and `WithGroup` (US4-2).
  - **Normalized tree**: tests assert on what an operator would see in the output, not on how
    slog happened to split attributes between the logger and the record.
  - **Variadic path**: keys that contain dots stay unambiguous, and no syntax has to be
    documented.
  - **`(value, ok)`**: Go's lookup idiom, which reports absence (FR-039). The request's
    `r.Attr("request_id").String()` becomes
    `v, ok := r.Attr("request_id"); !ok || v.String() != "abc"`.
  - The spike recorder passed `slogtest`, and 100 concurrent writes under `-race`.
- **Migration**: the `middleware` module switches to the recorder in a separate change, after
  `slogx/v0.1.0` is tagged (spec, Assumptions; Constitution, Principle I).
- **Alternatives rejected**:
  - Storing the raw `slog.Record`, as the current capture handlers do: logger attributes and
    groups would be lost.
  - `Attr(key) slog.Value`, returning a zero value when absent: a zero `slog.Value` prints
    `<nil>`, so absence could not be told apart from a nil value.

## R-14 Performance

Spike measurements. Every handler wraps `slog.NewJSONHandler(io.Discard, nil)`. Records are
logged with `LogAttrs` and 5 attributes. Where a context is used, it carries one attached
attribute, and one extraction function supplies `request_id`.

| Configuration | ns/op | B/op | allocs/op |
|---------------|------:|-----:|----------:|
| Bare JSON handler, 5 attributes | 669 | 0 | 0 |
| Bare JSON handler, the same 7 attributes the pipeline writes | 830 | 80 | 1 |
| `RedactHandler`, 4 keys, no match | 781 | 0 | 0 |
| `SamplingHandler`, records passing | 788 | 0 | 0 |
| `ContextHandler` with extraction function | 1,073 | 176 | 3 |
| `New`: context + 4 redaction keys + sampling | 1,348 | 176 | 3 |
| `New`, logger with an open group (slow path) | 1,364 | 304 | 3 |
| Disabled record, every handler, context with attributes and level | — | 0 | 0 |

- **Decision**: SC-008 compares the composed handler with the output handler alone **writing
  the same attributes**: 1,348 ns against 830 ns, or 1.6×. The spec and its checklist are updated
  (Iteration 3).
- **Rationale**: the composed pipeline writes 2 more attributes than the record carries.
  Comparing it with a 5-attribute baseline (2.0×) measures the cost of the extra output, not of
  the handlers.
- **Allocations on the enabled path**: there are 3, and they are not removable by the module:
  - the record's overflow slice beyond 5 attributes, which the bare 7-attribute record also
    pays;
  - the slice returned by the user's extraction function;
  - the record rebuilt by `ContextHandler`.
- **Disabled path**: 0 allocations for every handler and for `New` (FR-004, SC-003), measured
  with `testing.AllocsPerRun` on contexts that carry attributes and a level.

## R-15 Lint

- **Decision**: the repository configuration applies unchanged. The spike had two findings, both
  justified and confined to one place each:
  - `wrapcheck` reports every `return next.Handle(ctx, r)`, 10 places in the spike. All
    handlers call one package-local forwarder,
    `handle(h slog.Handler, ctx, r) error`, which carries
    `//nolint:wrapcheck // handlers return the next handler's error unchanged (FR-005)`.
  - `contextcheck` reports the background context used for summaries in
    `SamplingHandler.Handle`. The line carries
    `//nolint:contextcheck // summaries must not carry request attributes (FR-030)`.
- **Rationale**:
  - Wrapping the next handler's error would change the text of an error that slog discards
    anyway, and would make the handlers less transparent. A single forwarder keeps the exception
    in one reviewed place, as `errors` does with `errorlint` (its research R-17).
  - The background context is required by FR-030 (R-10).
- **Alternatives rejected**:
  - Adding `ignore-interface-regexps` for `slog.Handler` to the shared `wrapcheck` settings: a
    repository-wide exemption for one module's need.
  - A context stored in the sampler at construction, to avoid the `contextcheck` finding: it
    hides the intent from the linter instead of stating it.

## R-16 Testing approach (Principle III)

- **Decision**:
  - The standard `testing` package only. External test packages are `slogx_test` and
    `slogxtest_test`. Internal tests (`*_internal_test.go`) cover the sampler bound and the
    `internal/attrs` helpers.
  - **Conformance**: `slogtest.Run` for each handler over a JSON handler:
    - `ContextHandler` twice, once with an extraction function that always returns an
      attribute, to force the slow path (R-04);
    - `RedactHandler` with keys absent from the suite;
    - `SamplingHandler` and `New` with `first` large enough to drop nothing;
    - the recorder, through its captured records.
  - **Time**: `testing/synctest.Test` with `time.Sleep` gives virtual time for sampling
    intervals, with no real waiting and no injected clock (Principle III).
  - **Concurrency**: goroutines with `sync.WaitGroup.Go`, under `-race -shuffle=on`.
  - **Allocations**: `testing.AllocsPerRun` asserts 0 allocations for disabled records. The
    benchmarks `BenchmarkDisabled` and `BenchmarkEnabled` use `b.Loop` and back SC-003 and
    SC-008.
  - **Fuzzing**: `FuzzRedact` generates keys in any case, and nesting, and checks that a
    sentinel value under a matching key never reaches the output, and that nothing panics. The
    module parses no external input, so fuzzing is not mandatory. It is the strongest check of
    SC-004.
  - **Examples**: one runnable `Example` for each primary API (FR-009).
  - Coverage target: ≥ 80% per module.
- **Rationale**: these are the constitution's tools. `synctest` (Go 1.25) and `WaitGroup.Go`
  (Go 1.25) are both available in 1.26.

## R-17 Dependencies

- **Decision**: the standard library only, test-only dependencies included. Imports: `cmp`,
  `context`, `errors`, `fmt`, `io`, `log/slog`, `slices`, `strings`, `sync`, `time`, and in
  tests `testing/slogtest` and `testing/synctest`.
- **Rationale**: FR-008, SC-010 and Principle V. The examples that integrate a request
  identifier use a context key local to the example, not `middleware/requestid`. Importing it
  would add a dependency on another module of this repository.

## R-18 Documentation

- **Decision**: `doc.go` explains:
  - that context attributes and the per-request level need the `*Context` and `LogAttrs` logging
    methods, and that golangci-lint's `sloglint` with `context: all` enforces them;
  - the composition order of R-11, and why;
  - what redaction covers and what it does not: values encoded by the output handler, such as
    plain structs, are not inspected, and the secret type is the protection for them;
  - the sampling rules, the summary record and `Flush` at shutdown;
  - the placeholders of R-06 and R-12.

  The README has the purpose, `go get`, a composed example, the minimum Go version and a link to
  the recorder.
- **Rationale**: FR-009 and constitution Principle I.

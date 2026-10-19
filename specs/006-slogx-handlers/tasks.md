---

description: "Task list for the slogx composable slog handlers module"
---

# Tasks: Composable Structured Logging Handlers (slogx)

**Input**: Design documents from `specs/006-slogx-handlers/`

**Prerequisites**: [plan.md](plan.md), [spec.md](spec.md), [research.md](research.md),
[data-model.md](data-model.md), [contracts/](contracts/), [quickstart.md](quickstart.md)

**Tests**: REQUIRED. Constitution Principle III is non-negotiable:

- Tests are written first and observed failing (a compilation failure counts as red).
- Coverage must be ≥ 80%, with `-race -shuffle=on`.
- Performance-sensitive paths (FR-004, SC-003, SC-008) need benchmarks.
- Every primary API needs an `Example`.

Every test task below precedes the implementation it covers.

**Organization**: tasks are grouped by user story (spec.md US1–US6). Every path is relative to the
repository root. All Go code lives in the module `slogx/`, with the packages `slogx`,
`slogx/slogxtest` and `slogx/internal/attrs`.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: can run in parallel with other [P] tasks of the same phase: different files, and no
  dependency on an incomplete task.
- **[Story]**: US1–US6 from spec.md. Setup, Foundational and Polish tasks have no story label.

## Conventions for every task

- **Packages**:
  - Behavior tests use the external packages `slogx_test` and `slogxtest_test`.
  - Tests of unexported code use `package slogx` (or `package attrs`), in files named
    `*_internal_test.go`.
- **Test names**: `Test<Feature>_<Scenario>`, chosen so that the `-run` patterns of
  [quickstart.md §3](quickstart.md) select them:
  - `Test<Handler>_Conformance`;
  - `TestWith_*`, `TestContextHandler_*`, `TestRedactHandler_*`, `TestSecret_*`,
    `TestLevelHandler_*`, `TestWithLevel_*`, `TestRecorder_*`, `TestRecord_*`, `TestNew_*`,
    `TestCompose_*`, `TestOptions_*`, `TestSamplingHandler_*`, `TestSampler_*` and
    `TestFlush_*`;
  - `TestAlloc_<Handler>Disabled`;
  - `TestPanic_*`, `TestDepth_*` and `TestNil_*`.

  The fuzz target is `FuzzRedact`. Benchmarks use `b.Loop()`.
- **Test style**:
  - table-driven, with `t.Run`;
  - no wall-clock assertions and no `time.Sleep` on real time; sampling intervals run inside
    `synctest.Test`, where `time.Sleep` advances virtual time;
  - goroutines are joined with `sync.WaitGroup.Go` and `Wait`.
- **Output assertions**:
  - Records are written by `slog.NewJSONHandler` into a `bytes.Buffer`, with
    `ReplaceAttr` dropping `time` wherever outputs are compared byte for byte.
  - **Duplicate keys are checked on the raw JSON line** (`strings.Count(line, "\"request_id\"")`),
    because decoding into a map hides duplicates.
- **Fixtures**: secrets are obviously fake (`hunter2`, `s3cr3t`, `Bearer x`), and nothing looks
  like a real token (no JWT-shaped strings), so Gitleaks and Bearer stay quiet. If a scanner
  flags a fixture, rename the fixture; never suppress the finding.
- **Contexts**: a nil context is passed through a variable (`var nilCtx context.Context`), never
  as a literal `nil`, which `staticcheck` reports (SA1012).
- **Lint**: golangci-lint must report zero findings. The only `//nolint` directives allowed in
  library code are the two of research R-15:
  - `//nolint:wrapcheck // handlers return the next handler's error unchanged (FR-005)`, on the
    single forwarder in `slogx/forward.go`;
  - `//nolint:contextcheck // summaries must not carry request attributes (FR-030)`, on the
    background context in `SamplingHandler.Handle`.

  Test helpers take `testing.TB` and call `tb.Helper()` first (`thelper`). That includes the
  `results` closures passed to `slogtest.Run`.
- **Forwarding**: every handler passes records on through `handle(ctx, h, r)` (T011), never
  through `h.Handle` directly, so that the `wrapcheck` exception stays in one place.
- **Documentation**: every exported identifier has a doc comment. `slogx/doc.go` holds the
  package doc, which is expanded in T043.

---

## Phase 1: Setup (Shared Infrastructure)

**Purpose**: create the module and register it with the repository tooling.

- [X] T001 Create `slogx/go.mod` containing exactly `module github.com/paccolamano/go/slogx` and `go 1.26.0`, with no `require` and no `replace`.
- [X] T002 [P] Create `slogx/CHANGELOG.md` in Keep a Changelog format (the header text of `errors/CHANGELOG.md`, adapted), with an empty `## [Unreleased]` section. The entries are added in T045.
- [X] T003 [P] Create `slogx/README.md` with:
  - the title `# slogx`;
  - one purpose paragraph: "Composable `slog.Handler` middleware: context attributes, redaction of sensitive data, a per-request level and sampling of repeated records. It adds no logger type: keep using `*slog.Logger`.";
  - `go get github.com/paccolamano/go/slogx`;
  - "Minimum Go version: 1.26";
  - placeholder headings "Usage", "Context attributes", "Redaction and secrets", "Per-request level", "Sampling" and "Testing with slogxtest", which are completed in T044.
- [X] T004 Run `make work` from the repository root so that `go.work` lists `./slogx`. Then confirm that `scripts/structure-check.sh slogx` reports PASS (depends on T001–T003).

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: the shared helpers, the option machinery and the forwarder that every story uses.

**⚠️ CRITICAL**: no user story work can begin until this phase is complete.

- [X] T005 Create `slogx/doc.go` with a short package doc comment for `package slogx`: "Package slogx provides composable slog.Handler middleware: attributes from the context, redaction of sensitive data, a per-request level and sampling of repeated records. It adds no logger type: use *slog.Logger with the handlers of this package." It is expanded in T043.
- [X] T006 [P] Write failing internal tests in `slogx/internal/attrs/attrs_internal_test.go` (`package attrs`):
  1. `TestFlatten_Table`. `Flatten(nil, in)`:
     - returns an empty result for nil;
     - drops `slog.Attr{}` and attributes with an empty key and a non-group value;
     - inlines `slog.Group("", a, b)` in place, recursively, so `Group("", a, Group("", b))` becomes `a, b`;
     - keeps the order;
     - keeps groups with a non-empty key unchanged, including empty ones;
     - appends to a non-nil `dst`.
  2. `TestConstants`: `MaxDepth == 64` and `DepthPlaceholder == "!DEPTH"`.
- [X] T007 Implement `slogx/internal/attrs/attrs.go` until T006 passes:
  - the package doc "Package attrs holds attribute-tree helpers shared by slogx and slogxtest.";
  - `const MaxDepth = 64`;
  - `const DepthPlaceholder = "!DEPTH"`;
  - `func Flatten(dst, attrs []slog.Attr) []slog.Attr`.
- [X] T008 [P] Create the shared test helpers (`package slogx_test`) in `slogx/helpers_test.go`, `slogx/conformance_test.go` and `slogx/alloc_test.go`:
  - in `slogx/helpers_test.go`:
    - `must[H any](h H, err error) H`, which panics on error (Go cannot pass a multi-value call next to another argument, so a `testing.TB` parameter is not possible);
    - `newJSON(w io.Writer, level slog.Leveler) slog.Handler`, a JSON handler whose `ReplaceAttr` drops `slog.TimeKey` at the top level;
    - `lines(tb, buf *bytes.Buffer) []string`, the non-empty output lines;
    - `decode(tb, line string) map[string]any`;
    - `type ridKey struct{}`, `withRID(ctx context.Context, id string) context.Context` and `ridAttrs(ctx context.Context) []slog.Attr`, which returns `[]slog.Attr{slog.String("request_id", id)}` when the context carries an id and nil otherwise;
  - in `slogx/conformance_test.go`: `testConformance(t *testing.T, wrap func(next slog.Handler) slog.Handler)`, which runs `slogtest.Run` with a fresh `bytes.Buffer` and a plain `slog.NewJSONHandler(buf, nil)` (time kept) per subtest, and decodes the last line as the result;
  - in `slogx/alloc_test.go`: `assertNoAllocsWhenDisabled(tb testing.TB, h slog.Handler, ctxs ...context.Context)`. For each context, it asserts that `testing.AllocsPerRun(1000, func() { logger.LogAttrs(ctx, slog.LevelDebug, "m", slog.String("a", "b")) })` is 0, where the logger's handler is `h` and `h` disables Debug for that context.
- [X] T009 [P] Write failing internal tests in `slogx/options_internal_test.go` (`package slogx`) for `newConfig(ctor string, accepted, required optionSet, opts []Option) (config, error)`:
  1. `TestOptions_Validation`. Each rule of data-model.md "Configuration" returns an error that wraps `ErrInvalidConfig`:
     - `WithContextFunc`: "function not nil";
     - `WithRedactKeys`: "at least one key per call; no key empty or only whitespace";
     - `WithSampling`: "`first >= 1`, `thereafter >= 1`, `interval > 0`";
     - `WithSampleErrors`: "requires `WithSampling`".

     A nil `Option` in `opts` also returns an error, and never panics.
  2. `TestOptions_Applicability`: an option outside `accepted`, and a missing option in `required`, both return errors that wrap `ErrInvalidConfig`.
  3. `TestOptions_ErrorFormat`: messages have the form `slogx: invalid configuration: <Constructor>: <Option>: <reason>`, the message of `ErrInvalidConfig` followed by the details as in the `middleware` module, for example `slogx: invalid configuration: NewRedactHandler: WithRedactKeys: empty key`.
  4. `TestOptions_Accumulation`:
     - two `WithContextFunc` calls keep both functions in call order;
     - two `WithRedactKeys` calls accumulate the keys;
     - two `WithSampling` calls keep the last;
     - `WithSampleErrors` sets `sampleErrors`.
- [X] T010 Implement `slogx/options.go` until T009 passes:
  - `var ErrInvalidConfig = errors.New("slogx: invalid configuration")`;
  - `type Option func(*config)`;
  - the unexported `optionSet` bits `optContextFunc`, `optRedactKeys`, `optSampling` and `optSampleErrors`;
  - the `config` fields of data-model.md: `fns`, `keys`, `first`, `thereafter`, `interval`, `sampleErrors` and `set`, plus whatever is needed to detect an empty `WithRedactKeys()` call;
  - `WithContextFunc`, `WithRedactKeys`, `WithSampling(first, thereafter int, interval time.Duration)` and `WithSampleErrors`, with the doc comments of [contracts/public-api.md](contracts/public-api.md);
  - `newConfig`, which applies the options (skipping and reporting nil ones), then checks applicability, then validates;
  - `errNilHandler(ctor string) error`, returning `fmt.Errorf("%w: %s: next: must not be nil", ErrInvalidConfig, ctor)`.
- [X] T011 [P] Create `slogx/forward.go` with `func handle(ctx context.Context, h slog.Handler, r slog.Record) error`, returning `h.Handle(ctx, r)` with the `wrapcheck` directive of Conventions.

**Checkpoint**: `GOWORK=off go test -race ./internal/... && GOWORK=off go test -race -run 'Options' .` passes in `slogx/`. The foundation is ready, and user stories can now begin.

---

## Phase 3: User Story 1 - Correlate every record with its request (Priority: P1) 🎯 MVP

**Goal**: `With` and `ContextHandler` add attached and extracted attributes at the top level of
every record logged with a context. The record wins on duplicate keys (FR-010 to FR-016).

**Independent Test**: wrap a JSON handler with `NewContextHandler` and `WithContextFunc(ridAttrs)`.
Log from functions that receive only the context, through plain and grouped loggers, and inspect
the output lines.

### Tests for User Story 1 ⚠️ (write first, see them fail)

- [X] T012 [P] [US1] Write failing tests in `slogx/context_test.go`. They go through `NewContextHandler(newJSON(&buf, nil))`, because `With` has no getter.
  1. `TestWith_DerivedContexts` (US1-4, FR-010): the parent has `user=u1` and the child adds `tenant=t1`. A record logged with the child carries both, and one logged with the parent carries only `user`.
  2. `TestWith_LatestWins` (US1-5, FR-014): `tenant` attached as `t1`, then as `t2`, in one call or in two, appears once, with `t2`. Check the raw line.
  3. `TestWith_Normalization`: `slog.Attr{}` and empty-key non-group attributes are dropped, and `slog.Group("", a, b)` is inlined.
  4. `TestWith_NoAttrs`: `With(ctx)` and `With(ctx, slog.Attr{})` return `ctx` itself (`==`).
  5. `TestNil_ContextWith`: `With(nilCtx, slog.String("k", "v"))` does not panic and returns a usable context that carries `k`.
- [X] T013 [P] [US1] Write failing tests in `slogx/contexthandler_test.go`:
  1. `TestContextHandler_Conformance`: `testConformance` with `NewContextHandler(next)`, and again with `WithContextFunc` returning `slog.String("ctxattr", "x")` for every context, which forces the slow path (research R-04).
  2. `TestContextHandler_TopLevel` (US1-1): `{"msg":"order created","tenant":"t1"}`.
  3. `TestContextHandler_ExtractionFunc` (US1-2): with `ridAttrs`, `withRID(ctx, "abc")` gives `"request_id":"abc"`, and a context without an id gives no `request_id`.
  4. `TestContextHandler_GroupsKeepContextAtTop` (US1-3, FR-013). `logger.WithGroup("db").With("table", "orders").InfoContext(ctx, "q", "rows", 3)` decodes to exactly `{"level":"INFO","msg":"q","tenant":"t1","request_id":"abc","db":{"table":"orders","rows":3}}`. Also check:
     - `WithGroup("a").With("x", 1).WithGroup("b")` logging `"y", 2` gives `a.x`, `a.b.y` and top-level context attributes;
     - `WithGroup("a")` with no attribute gives no `a` key.
  5. `TestContextHandler_RecordWins` (US1-6, FR-015), on raw lines:
     - a record with `request_id=abc`, a context giving `xyz`: one `"request_id"`, value `abc`;
     - `logger.With("request_id", "w")`: one, value `w`;
     - `logger.WithGroup("db").InfoContext(ctx, "m", "request_id", "x")`: top-level `request_id` from the context **and** `db.request_id=x` (edge case "Context attributes and groups");
     - a record with `request_id` twice and no context value: both kept.
  6. `TestContextHandler_Precedence` (FR-014):
     - an attached `k=attached` beats an extracted `k=extracted`;
     - of two extraction functions returning `k`, the first configured wins;
     - an extracted `slog.Group("", slog.String("g", "1"))` is inlined;
     - an extracted attribute with an empty key is dropped.
  7. `TestContextHandler_OrderStable`: on raw lines, with and without an open group, the order is top-level logger attributes, then context attributes, then the record's attributes or the open group.
  8. `TestContextHandler_NoContextMethod` (US1-7): `logger.Info("m")` carries no attached attribute and no `request_id`, while an extraction function that returns a static `service=api` still applies.
  9. `TestContextHandler_Unchanged` (FR-016): with no attached attribute and no extraction function, a fake next handler (a test type that stores the `slog.Record` it receives) gets a record with the same message, level, time and attributes.
  10. `TestContextHandler_Options`: each of these returns an error wrapping `ErrInvalidConfig`:
      - `NewContextHandler(nil)`;
      - `NewContextHandler(next, WithContextFunc(nil))`;
      - `NewContextHandler(next, WithRedactKeys("x"))`, which is inapplicable;
      - `NewContextHandler(next, nil)`.
  11. `TestAlloc_ContextHandlerDisabled` (FR-004): `assertNoAllocsWhenDisabled` with `ridAttrs` configured, and the contexts `With(withRID(ctx, "abc"), slog.String("tenant", "t"))` and `context.Background()`.
- [X] T014 [P] [US1] Write failing tests in `slogx/panic_test.go`:
  1. `TestPanic_ContextFunc` (FR-005): the first extraction function panics with `"boom"` and the second returns `k=v`. The record is written with `"!CONTEXTFUNC":"panic: boom"` and `"k":"v"`.
  2. `TestNil_ContextHandler`: `h.Handle(nilCtx, r)`, called directly, does not panic and writes the record.

### Implementation for User Story 1

- [X] T015 [US1] Implement `slogx/context.go` (`attrsKey`, `ctxAttrs`, `With`, `attrsFrom`) per research R-03, honoring the invariants of data-model.md "Context attributes":
  - "No attribute has an empty key, and no group has an empty key: they are inlined when attached." Use `attrs.Flatten`.
  - "Keys are unique. When a key was attached more than once, only the most recent value is kept (FR-014)."
  - "The slice is never modified after it is stored, so a parent context never sees attributes attached to its children (FR-010)."

  A nil `ctx` is treated as `context.Background()`, and `attrsFrom` is nil-safe.
- [X] T016 [US1] Implement `slogx/contexthandler.go` until T012–T014 pass:
  - **Type**: `ContextHandler` with the fields of data-model.md (`root`, `next`, `topKeys`, `ops`, `fns`), honoring "When `ops` is empty, `root == next`." and "`ops` and `topKeys` are copied on write, so derived handlers never share a backing array they could append to."
  - **Constructor**: `NewContextHandler(next, opts...)` calls `errNilHandler` for a nil `next`, then `newConfig("NewContextHandler", optContextFunc, 0, opts)`.
  - **Handle**: the three paths of the data-model "Handle paths" table. Context attributes are added straight into the new record, with no intermediate slice. Deduplication follows FR-014 and FR-015 (research R-05). Every record is passed on through `handle`.
  - **Extraction**: each function runs inside a `recover`. On panic it contributes `slog.String("!CONTEXTFUNC", fmt.Sprintf("panic: %v", r))` (research R-06).
  - **Other methods**: `Enabled` forwards. `WithAttrs` with no attribute, and `WithGroup("")`, return the receiver.
- [X] T017 [US1] Add `ExampleWith` and `ExampleNewContextHandler` to `slogx/example_test.go`. The latter extracts a request id from a context key local to the example: never `middleware/requestid` (research R-17). Both write JSON without time to `os.Stdout`, with an `// Output:` comment.

**Checkpoint**: `GOWORK=off go test -race -run 'With|Context|Panic|Nil|Alloc' .` passes in `slogx/`. The MVP is deliverable.

---

## Phase 4: User Story 2 - Keep secrets out of the logs (Priority: P1)

**Goal**: `RedactHandler` replaces sensitive keys at every depth and from every source, and
`Secret[T]` renders as `[REDACTED]` everywhere (FR-017 to FR-023).

**Independent Test**: configure "authorization", "password", "token" and "cookie". Send a known
secret through every path, then search the output. Send a `Secret` through plain handlers,
`fmt` and JSON.

### Tests for User Story 2 ⚠️ (write first, see them fail)

- [X] T018 [P] [US2] Write failing tests in `slogx/redact_test.go`:
  1. `TestRedactHandler_Conformance`: `testConformance` with `WithRedactKeys("password")`, a key the suite never uses.
  2. `TestRedactHandler_Keys` (US2-1): `password=hunter2` gives `"password":"[REDACTED]"`.
  3. `TestRedactHandler_CaseInsensitiveInGroups` (US2-2): `slog.Group("header", "Authorization", "Bearer x", "Accept", "*/*")` gives `"header":{"Authorization":"[REDACTED]","Accept":"*/*"}`. `AUTHORIZATION` and `authorization` are redacted too.
  4. `TestRedactHandler_LogValuerGroup` (US2-3, FR-019): a `LogValuer` resolving to `{name, token}` gives `"user":{"name":"bob","token":"[REDACTED]"}`. A `LogValuer` that counts its calls, logged under the key `token`, is called 0 times.
  5. `TestRedactHandler_WithAttrs` (US2-4): after `logger.With("token", "s3cr3t")`, every record shows `"token":"[REDACTED]"` and no line contains `s3cr3t`. The same holds through `logger.With(...).WithGroup("g")`.
  6. `TestRedactHandler_WholeKeyOnly` (US2-7): `password_hint=x` is unchanged.
  7. `TestRedactHandler_GroupKey` (FR-020):
     - `slog.Group("password", "a", 1)` gives `"password":"[REDACTED]"`;
     - `logger.WithGroup("Password").With("x", 1).Info("m", "a", 2)` gives `"Password":"[REDACTED]"`, and neither `x` nor `a` appears;
     - `logger.WithGroup("g").WithGroup("TOKEN").Info("m", "a", 1)` gives `"g":{"TOKEN":"[REDACTED]"}`;
     - `logger.WithGroup("token").Info("m")`, with no attribute, still writes `"token":"[REDACTED]"` (edge case "Empty group after redaction").
  8. `TestRedactHandler_Untouched` (FR-021, FR-022): a record with nested groups and no match produces the same line with and without the handler, byte for byte, time dropped. The message `"token hunter2"` is never altered.
  9. `TestRedactHandler_Options`: each of these returns an error wrapping `ErrInvalidConfig`:
     - `NewRedactHandler(nil, WithRedactKeys("x"))`;
     - `NewRedactHandler(next)`, without the required option;
     - `NewRedactHandler(next, WithRedactKeys())`;
     - `NewRedactHandler(next, WithRedactKeys(" "))`;
     - `NewRedactHandler(next, WithRedactKeys("x"), WithContextFunc(ridAttrs))`, which is inapplicable.
  10. `TestAlloc_RedactHandlerDisabled`: `assertNoAllocsWhenDisabled` with `context.Background()`.
- [X] T019 [P] [US2] Write failing tests in `slogx/secret_test.go`:
  1. `TestSecret_Fmt` (US2-6):
     - `fmt.Sprintf` of `NewSecret("hunter2")` with `%v %+v %#v %s %q %x %X %d %10v` equals exactly `[REDACTED]`;
     - for a struct with an exported and an unexported `Secret[string]` field, every verb's output contains neither `hunter2` nor `68756e74657232` (its hex encoding).
  2. `TestSecret_JSON`: `json.Marshal(struct{ S slogx.Secret[string]; N struct{ s slogx.Secret[string] } }{...})` equals `{"S":"[REDACTED]","N":{}}`.
  3. `TestSecret_Log`:
     - through `slog.NewJSONHandler` and `slog.NewTextHandler`, with no slogx handler, `"s"` shows `[REDACTED]`;
     - a struct with an unexported `Secret` field, logged through the text handler, does not contain `hunter2`.
  4. `TestSecret_Reveal`:
     - `NewSecret("hunter2").Reveal() == "hunter2"`;
     - `slogx.Secret[int]{}.Reveal() == 0`, and the zero value still renders as `[REDACTED]`;
     - `String()`, `MarshalText()` and `LogValue().String()` all give `[REDACTED]`.
- [X] T020 [P] [US2] Add failing tests to `slogx/panic_test.go`:
  1. `TestPanic_LogValue` (US2-8): a `LogValuer` that panics, logged through `RedactHandler`, gives a written record whose value text starts with `LogValue panicked` (research R-12).
  2. `TestDepth_SelfReferencingValuer`: a `LogValuer` that returns a group containing itself, logged through `RedactHandler`, does not crash. The output contains `!DEPTH`, and the nesting stops at the 65th group level (`attrs.MaxDepth`).
- [X] T021 [P] [US2] Write `FuzzRedact` in `slogx/fuzz_test.go`. Seed corpus: `("token", 0, 0)`, `("Authorization", 5, 3)` and `("cookie", 255, 7)`. Inputs `(key string, caseMask uint8, depth uint8)`:
  - flip the case of `key`'s ASCII letters according to `caseMask`;
  - nest the attribute `variant="SENTINEL-VALUE"` in `depth % 8` groups named `g0…g7`;
  - log it through `NewRedactHandler(json, WithRedactKeys(key))`; skip keys that are empty or only whitespace.

  The output must not contain `SENTINEL-VALUE`, and nothing may panic (SC-004).

### Implementation for User Story 2

- [X] T022 [US2] Implement `slogx/redact.go` until T018, T020 and T021 pass, per research R-07 and the data-model "RedactHandler" state transitions:
  - **Fields**: `next`, `keys` and `sealed`.
  - **Constructor**: `NewRedactHandler` calls `newConfig("NewRedactHandler", optRedactKeys, optRedactKeys, opts)`.
  - **Matching**: `match(key)` uses `strings.EqualFold` against each key.
  - **Order of checks**: the key is checked first, and a match gives `slog.String(key, "[REDACTED]")` without resolving the value. Otherwise the value goes through `Value.Resolve()`, and the children of a group are checked recursively. At depth `attrs.MaxDepth` the group value becomes `attrs.DepthPlaceholder`.
  - **Handle**: a first pass with no allocation looks for work (a match, a `KindLogValuer` value, or a group containing either). With no work, the original record is forwarded. Otherwise a new record carries the processed attributes.
  - **WithAttrs**: processes the attributes before forwarding them to `next.WithAttrs`.
  - **WithGroup**: a matching name returns a sealed handler over `next.WithAttrs([]slog.Attr{slog.String(name, "[REDACTED]")})`.
  - **Sealed state**: `WithAttrs` and `WithGroup` return the receiver, and `Handle` forwards `slog.NewRecord(r.Time, r.Level, r.Message, r.PC)` with no attribute.
  - **Enabled** forwards.
- [X] T023 [P] [US2] Implement `slogx/secret.go` until T019 passes, per research R-08:
  - `type Secret[T any] struct{ p *T }`;
  - `NewSecret`, and `Reveal`, which returns the zero `T` for a nil `p`;
  - value-receiver `LogValue`, `String`, `Format` and `MarshalText`, each giving `[REDACTED]`. `Format` ignores the verb, flags and width, and writes through `io.WriteString` with the results explicitly discarded.

  The marker is the unexported `const redacted = "[REDACTED]"`, shared with `redact.go`.
- [X] T024 [US2] Add `ExampleNewRedactHandler` (an `Authorization` header group) and `ExampleSecret` (logging, `%v` and JSON) to `slogx/example_test.go`, with `// Output:` comments.

**Checkpoint**: `GOWORK=off go test -race -run 'Redact|Secret|Panic|Depth|Alloc' .` passes. US1 still passes.

---

## Phase 5: User Story 3 - Enable debug records for a single request in production (Priority: P2)

**Goal**: `WithLevel` and `LevelHandler` override the threshold for the records of one context
(FR-024 to FR-026).

**Independent Test**: wrap a JSON handler at Info with `NewLevelHandler`, then log debug records
with and without a context level.

### Tests for User Story 3 ⚠️ (write first, see them fail)

- [X] T025 [P] [US3] Write failing tests in `slogx/level_test.go`:
  1. `TestLevelHandler_Conformance`: `testConformance` with `NewLevelHandler(next)`.
  2. `TestLevelHandler_DebugForOneRequest` (US3-1, US3-2): over a JSON handler at Info, `DebugContext(WithLevel(ctx, slog.LevelDebug), "shown")` is written and `DebugContext(ctx, "hidden")` is not.
  3. `TestWithLevel_Nested` (US3-3, FR-026): `WithLevel(WithLevel(ctx, Debug), Warn)` suppresses `InfoContext`, while the outer context still writes it.
  4. `TestLevelHandler_NoContextMethod` (US3-4): `logger.Debug("m")` is not written.
  5. `TestLevelHandler_Silence`: `WithLevel(ctx, slog.LevelError)` suppresses `WarnContext` through a handler at Info (spec, Assumptions: both directions).
  6. `TestLevelHandler_DefersWithoutLevel`: over a handler at Warn, `Enabled(ctx, Info)` is false and `Enabled(ctx, Error)` is true when the context carries no level.
  7. `TestNil_Level`: `WithLevel(nilCtx, Debug)` does not panic. `h.Enabled(nilCtx, Info)` defers to the next handler.
  8. `TestLevelHandler_Options`: `NewLevelHandler(nil)` and `NewLevelHandler(next, WithContextFunc(ridAttrs))`, which is inapplicable, both return errors wrapping `ErrInvalidConfig`.
  9. `TestAlloc_LevelHandlerDisabled`: `assertNoAllocsWhenDisabled` with `context.Background()` and with `WithLevel(ctx, slog.LevelWarn)`.

### Implementation for User Story 3

- [X] T026 [US3] Add `levelKey`, `WithLevel(ctx, level)` and `levelFrom(ctx) (slog.Level, bool)` to `slogx/context.go`. A nil `ctx` is treated as `context.Background()`, and `levelFrom` is nil-safe (research R-03).
- [X] T027 [US3] Implement `slogx/level.go` until T025 passes:
  - `LevelHandler{next}`;
  - `NewLevelHandler`, which calls `newConfig("NewLevelHandler", 0, 0, opts)`;
  - `Enabled`: when `levelFrom` reports a level, return `level >= ctxLevel`; otherwise ask `next`;
  - `Handle` forwards through `handle`;
  - `WithAttrs` and `WithGroup` wrap, and `WithGroup("")` returns the receiver.
- [X] T028 [US3] Add `ExampleWithLevel` (a debug record shown only for the marked context) to `slogx/example_test.go`.

**Checkpoint**: `GOWORK=off go test -race -run 'Level|Nil|Alloc' .` passes. US1 and US2 still pass.

---

## Phase 6: User Story 4 - Verify what the code logs, in tests (Priority: P2)

**Goal**: `slogxtest.Recorder` captures normalized records, honoring `WithAttrs` and `WithGroup`,
and covers every use of the middleware capture handlers (FR-037 to FR-041).

**Independent Test**: log through plain, derived and grouped loggers built on a recorder, from one
and from many goroutines, and inspect the captured records.

### Tests for User Story 4 ⚠️ (write first, see them fail)

- [X] T029 [P] [US4] Write failing tests in `slogx/slogxtest/recorder_test.go` (`package slogxtest_test`):
  1. `TestRecorder_Conformance` (FR-003): `slogtest.Run` with a fresh `NewRecorder()` per subtest. The results come from `Last()`, converted to a map: `slog.TimeKey` only when the time is non-zero, then `slog.LevelKey`, `slog.MessageKey`, and groups as nested maps.
  2. `TestRecorder_Last` (US4-1): after `InfoContext(ctx, "panic recovered", "request_id", "abc")`, `Last()` returns `ok == true`, `Message == "panic recovered"` and `Level == slog.LevelInfo`, and `Attr("request_id")` returns `"abc", true`.
  3. `TestRecorder_GroupsAndAttrs` (US4-2, FR-038): `slog.New(rec).With("component", "db").WithGroup("query").Info("slow", "ms", 12)` gives `Attr("component") == "db"` and `Attr("query", "ms")` with `Int64() == 12`.
  4. `TestRecorder_MinLevel` (US4-3, FR-040):
     - with `NewRecorder(WithLevel(slog.LevelWarn))`, `Enabled(ctx, Info)` is false and an Info record is not captured;
     - with `NewRecorder()` and with `NewRecorder(WithLevel(nil))`, `Enabled(ctx, slog.Level(-100))` is true.
  5. `TestRecorder_Concurrent` (US4-4): 100 goroutines each log once, partly through derived recorders, and `Len() == 100` (FR-037).
  6. `TestRecorder_Empty` (US4-5): `Last()` on a new recorder returns `ok == false`, `Records()` has length 0 and `Len() == 0`.
  7. `TestRecord_Attr` (US4-6, FR-039):
     - a missing key returns false;
     - a path through a non-group value returns false;
     - `Attr()` with no path returns false;
     - when a key repeats at one level, the last occurrence wins.
  8. `TestRecorder_Normalization` (data-model.md "Normalization"):
     - empty attributes are dropped, empty-key groups inlined and empty groups dropped;
     - a `LogValuer` is resolved when the record is captured: a valuer whose result changes after logging keeps its captured value (edge case "Recorder, values resolved later");
     - `WithAttrs` values are resolved when `WithAttrs` is called;
     - a self-referencing `LogValuer` gives `!DEPTH` and does not crash.
  9. `TestRecorder_RecordsCopy`: modifying the slice returned by `Records()` does not change what the recorder holds.
  10. `TestRecorder_MiddlewarePatterns` (FR-041, SC-009): replay the assertion patterns of `middleware/accesslog/accesslog_test.go` and `middleware/recovery/recovery_test.go`, without importing `middleware`:
      - `Records()` in order;
      - `Message` and `Level`;
      - `Attr("status")` read with `Int64()`;
      - `Attr("header", "X-Test")` inside a `header` group;
      - `Attr("response_started")` read with `Bool()`;
      - `Len()`;
      - a minimum level that suppresses Info.

### Implementation for User Story 4

- [X] T030 [P] [US4] Implement `slogx/slogxtest/record.go`: `Record{Time time.Time; Level slog.Level; Message string; Attrs []slog.Attr}` and `func (r Record) Attr(path ...string) (slog.Value, bool)`, which walks one key per group level, the last occurrence winning (research R-13).
- [X] T031 [US4] Implement `slogx/slogxtest/recorder.go` until T029 passes:
  - the package doc "Package slogxtest provides a slog.Handler that records what is logged, for tests.";
  - `Recorder` with the data-model fields `s *store` (`mu sync.Mutex`, `records []Record`), `level slog.Leveler` and `ops`;
  - `Option`, `WithLevel` and `NewRecorder(opts ...Option) *Recorder`;
  - `Enabled`: `level == nil || l >= level.Level()`;
  - `WithAttrs` resolves values and appends an op, copy on write; `WithGroup("")` returns the receiver;
  - `Handle` folds the ops around the record's attributes, normalizes them per data-model.md (with `attrs.Flatten` semantics, `attrs.MaxDepth` and `attrs.DepthPlaceholder`), appends under the mutex, and returns nil;
  - `Records` (a copy), `Len` and `Last`.
- [X] T032 [US4] Create `slogx/slogxtest/example_test.go` with `ExampleRecorder`, the request's scenario: it logs "panic recovered" with `request_id`, then prints the `Last()` message and `Attr("request_id")`, with an `// Output:` comment.

**Checkpoint**: `GOWORK=off go test -race ./slogxtest` passes. US1–US3 still pass.

---

## Phase 7: User Story 5 - Get the correct composition with one call (Priority: P2)

**Goal**: `New` builds the level, context and redaction chain in the only correct order, and
matches the documented manual composition (FR-034 to FR-036). Sampling joins the chain in US6.

**Independent Test**: build `New` over a JSON handler at Info, with an extraction function and
sensitive keys. Log with contexts that carry a level, a token and a request id, then compare the
output with the manual composition.

### Tests for User Story 5 ⚠️ (write first, see them fail)

- [X] T033 [P] [US5] Write failing tests in `slogx/handler_test.go`:
  1. `TestNew_Conformance`: `testConformance` with `New(next, WithContextFunc(func(context.Context) []slog.Attr { return []slog.Attr{slog.String("ctxattr", "x")} }), WithRedactKeys("password"))`.
  2. `TestNew_ComposedScenario` (US5-1, US2-5, FR-035). Over `newJSON(&buf, slog.LevelInfo)` with `WithContextFunc(ridAttrs)` and `WithRedactKeys("authorization", "password", "token", "cookie")`, a `DebugContext` record is written with `"request_id":"abc"` and `"token":"[REDACTED]"`. Its context is `WithLevel(With(withRID(ctx, "abc"), slog.String("token", "s3cr3t")), slog.LevelDebug)`. No line contains `s3cr3t`.
  3. `TestCompose_EquivalentToManual` (US5-2, FR-036):
     - the manual chain is `NewRedactHandler` → `NewContextHandler` → `NewLevelHandler`;
     - a table of records goes through `New` with the same options and through the manual chain: plain, grouped, with logger attributes, with a context level, with a duplicate key;
     - the output buffers are byte-identical.
  4. `TestNew_NoOptions` (US5-3):
     - `New(next)` adds attached context attributes and honors `WithLevel`;
     - a `Secret` shows `[REDACTED]`;
     - 1,000 identical records are all written.
  5. `TestNew_InvalidOptions` (US5-4): each of these returns an error wrapping `ErrInvalidConfig`:
     - `New(nil)`;
     - `New(next, WithRedactKeys(""))`;
     - `New(next, WithContextFunc(nil))`;
     - `New(next, nil)`.
  6. `TestNew_DerivedHandlers`: `WithAttrs` and `WithGroup` return a `*slogx.Handler`, and `WithGroup("")` returns the receiver.
  7. `TestAlloc_NewDisabled` (SC-003): `assertNoAllocsWhenDisabled` on `New(next, WithContextFunc(ridAttrs), WithRedactKeys("token"))`, with a context carrying attributes and with `WithLevel(ctx, slog.LevelWarn)`.

### Implementation for User Story 5

- [X] T034 [US5] Implement `slogx/handler.go` until T033 passes:
  - `type Handler struct{ head slog.Handler }` (the `sampler` field arrives in T039);
  - `New(next, opts...)` calls `errNilHandler` for a nil `next`, then `newConfig("New", optContextFunc|optRedactKeys, 0, opts)`. In this phase, `WithSampling` is reported as inapplicable to `New`; T039 lifts that restriction;
  - it builds `LevelHandler → ContextHandler → RedactHandler (only when keys are configured) → next` with the unexported fields directly, so that the chain is identical to the manual one;
  - `Enabled` and `Handle` delegate to `head`, and `WithAttrs` and `WithGroup` return `&Handler{head: head.WithX(...)}`.
- [X] T035 [US5] Add `ExampleNew` to `slogx/example_test.go`. It is the request's composed example without sampling, with a JSON output without time, and shows `request_id` and `[REDACTED]` in an `// Output:` comment.

**Checkpoint**: `GOWORK=off go test -race -run 'New|Compose|Alloc' .` passes. US1–US4 still pass.

---

## Phase 8: User Story 6 - Survive log storms (Priority: P3)

**Goal**: `SamplingHandler` keeps the first N records of each (level, message) per interval, then
one every M. It summarizes drops exactly, with bounded memory and no goroutine, and joins `New`
with `Flush` (FR-027 to FR-033).

**Independent Test**: under `synctest`, send bursts of identical and distinct records, from one
goroutine and from many, across interval boundaries. Count the written records and read the
summaries.

### Tests for User Story 6 ⚠️ (write first, see them fail)

- [X] T036 [P] [US6] Write failing tests in `slogx/sampling_test.go`. Every interval-dependent case runs inside `synctest.Test`, and records are captured with `slogxtest.NewRecorder()` or a JSON buffer.
  1. `TestSamplingHandler_Conformance`: `testConformance` with `WithSampling(1000, 1, time.Hour)`.
  2. `TestSamplingHandler_Burst` (US6-1, SC-005): with `WithSampling(10, 100, time.Second)`, 10,000 `WarnContext("db query failed")` give 109 records. After `time.Sleep(time.Second)` and one more record, exactly one summary follows, with:
     - level `WARN` and message `records dropped by sampling`;
     - `sampling.msg == "db query failed"` and `sampling.dropped == 9891`;
     - `sampling.start` equal to the interval start and `sampling.end` equal to start + 1 s.
  3. `TestSamplingHandler_Concurrent` (US6-5, FR-032): 50 goroutines × 200 records give the same 109 records and `dropped == 9891`.
  4. `TestSamplingHandler_Errors` (US6-2, FR-029): by default, 500 `ErrorContext` records are all written and produce no summary. With `WithSampleErrors()`, they are sampled like the others.
  5. `TestSamplingHandler_NoDrops` (US6-3): 10 records and then the interval end give no summary.
  6. `TestSamplingHandler_GroupKey` (FR-027):
     - one message at Warn and at Info forms two groups;
     - the same message with different attributes, or logged through a logger derived with `With` or `WithGroup`, shares one group's counts.
  7. `TestSamplingHandler_SummaryOrder`: two groups with drops give summaries sorted by message, then level.
  8. `TestFlush_EmitsPending` (US6-6, FR-030):
     - after a burst, `Flush(context.Background())` emits the summary with `sampling.end` equal to the flush time;
     - a second `Flush` emits nothing;
     - `Flush` before any record returns nil and emits nothing.
  9. `TestSamplingHandler_SummaryNoRequestAttrs` (US6-7, FR-030). Under `New(rec, WithContextFunc(ridAttrs), WithSampling(10, 100, time.Second))`, the burst is logged through `logger.WithGroup("g").With("k", "v")` with a context carrying `withRID` and `With(tenant)`. The summary record has:
     - no `request_id` and no `tenant`;
     - no `k`;
     - no `g` group: `sampling` is at the top level.
  10. `TestSamplingHandler_ErrorsJoined`: when the next handler returns `errFake` for every record, the `Handle` call that emits a summary returns an error for which `errors.Is(err, errFake)` holds.
  11. `TestSamplingHandler_Options`: each of these returns an error wrapping `ErrInvalidConfig`:
      - `NewSamplingHandler(next)`, without the required option;
      - `WithSampling(0, 1, time.Second)`, `WithSampling(1, 0, time.Second)` and `WithSampling(1, 1, 0)`;
      - `NewSamplingHandler(next, WithSampleErrors())`;
      - `New(next, WithSampleErrors())`;
      - `NewSamplingHandler(next, WithSampling(1, 1, time.Second), WithRedactKeys("x"))`, which is inapplicable;
      - `NewSamplingHandler(nil, WithSampling(1, 1, time.Second))`.
  12. `TestNew_WithSampling` (US5-1 with sampling, FR-034, FR-036):
      - the composed scenario of T033 case 2, with `WithSampling(10, 100, time.Second)`, writes the same record;
      - `TestCompose_EquivalentToManualWithSampling`, in this file, compares `New` with the chain `NewRedactHandler` → `NewContextHandler` → `NewSamplingHandler` → `NewLevelHandler`, with byte-identical output, summaries included;
      - `Handler.Flush` emits the summaries;
      - `Flush` on a `Handler` without sampling returns nil.
  13. `TestAlloc_SamplingHandlerDisabled`: `assertNoAllocsWhenDisabled` with `context.Background()`.
- [X] T037 [P] [US6] Write failing internal tests in `slogx/sampling_internal_test.go` (`package slogx`), inside `synctest.Test`:
  1. `TestSampler_Bound` (US6-4, SC-006, FR-031):
     - with `first=1` and `thereafter=1000`, call `s.decide` directly (not through `Handle`, to keep the test fast under `-race`) for 1,000,000 distinct messages (`strconv.Itoa(i)`) in one interval; `len(s.groups)` never exceeds 1,024 (checked after the loop, and after every 10,000 calls);
     - then log a new message 50 times: all 50 records reach a counting next handler, because the untracked group passes;
     - finally, log a tracked message 50 times: only the first passes.
  2. `TestSampler_Rule`: for `first=3` and `thereafter=4`, records 1, 2, 3, 7, 11 and 15 of 16 pass. With T records, `first + ⌊(T − first) / thereafter⌋` pass when `T > first`.

### Implementation for User Story 6

- [X] T038 [US6] Implement `slogx/sampling.go` until T036 (cases 1–11, 13) and T037 pass, per research R-10 and the data-model "SamplingHandler and the shared sampler" section:
  - **Types**: `SamplingHandler{next, s *sampler}`. `sampler` has the fields `root`, `first`, `thereafter`, `interval`, `sampleErrors`, `mu`, `start` and `groups`, with `groups` "at most 1,024 entries (FR-031)" (`const maxGroups = 1024`). `groupKey{level, msg}` and `groupCount{n, dropped int64}` are map values, not pointers.
  - **Constructor**: `NewSamplingHandler` calls `newConfig("NewSamplingHandler", optSampling|optSampleErrors, optSampling, opts)`.
  - **Decision**: `decide(level, msg, now)` follows the data-model interval state machine and the per-record rule: "the record passes when `n <= first` or `(n - first) % thereafter == 0`; otherwise `dropped` is incremented". It honors the invariant "for each group and interval, records passed plus `dropped` equals the records received". Records at Error level and above, without `sampleErrors`, pass uncounted. An untracked group beyond the bound passes.
  - **Summaries** follow [contracts/record-semantics.md](contracts/record-semantics.md#sampling-summary):
    - `slog.NewRecord(now, level, "records dropped by sampling", 0)`, with `slog.Group("sampling", slog.String("msg", …), slog.Int64("dropped", …), slog.Time("start", …), slog.Time("end", …))`;
    - sorted by message, then level;
    - collected under the lock and emitted after it is released, to `s.root` with `context.Background()`, with the `contextcheck` directive of Conventions;
    - errors joined with `errors.Join` to the record's own result.
  - **Flush(ctx)**: closes the interval with `end = now`, emits with `ctx` and starts a new interval. Before any record it returns nil.
  - **Other methods**: `WithAttrs` and `WithGroup` share `s`, and `Enabled` forwards. No goroutine is started (FR-033).
- [X] T039 [US6] Extend `slogx/handler.go` until T036 case 12 passes:
  - `New` accepts `optSampling|optSampleErrors`;
  - with `WithSampling`, it inserts the `SamplingHandler` between `LevelHandler` and `ContextHandler` (research R-11);
  - `Handler` gains `sampler *sampler`, shared by `WithAttrs` and `WithGroup`;
  - add `func (h *Handler) Flush(ctx context.Context) error`, which returns nil without a sampler.
- [X] T040 [US6] Add `ExampleNewSamplingHandler` to `slogx/example_test.go`: a burst of 25 identical records with `WithSampling(2, 10, time.Minute)`, then `Flush`. The interval is a minute, so that the burst can never span two intervals and the output stays deterministic. The `// Output:` shows the 4 written records and the summary with `dropped` 21; times are dropped by `ReplaceAttr`, including `sampling.start` and `sampling.end`.

**Checkpoint**: `GOWORK=off go test -race -shuffle=on ./...` passes in `slogx/`, with every story together.

---

## Phase 9: Polish & Cross-Cutting Concerns

**Purpose**: benchmarks, documentation, release notes and the quickstart validation.

- [X] T041 [P] Write `slogx/bench_test.go`, with the setups of research R-14: every handler wraps `slog.NewJSONHandler(io.Discard, nil)`; records use `LogAttrs` with `method`, `path`, `status`, `duration` and `client`; the context carries `tenant` (attached) and `request_id` (via `ridAttrs`).
  - `BenchmarkDisabled/{level,context,redact,sampling,new}` logs at Debug, with `b.ReportAllocs()`.
  - `BenchmarkEnabled/{bare,bare-same-attrs,context,redact,sampling,composed,composed-group}`:
    - `bare-same-attrs` logs the same 7 attributes directly;
    - `redact` and `composed` use the 4 keys `authorization`, `password`, `token` and `cookie`;
    - `sampling` and `composed` use `WithSampling(1<<62, 1, time.Second)`, so every record passes;
    - `composed-group` is `composed` behind `WithGroup("db")`.
- [X] T042 [P] Add `BenchmarkParallelSampling` to `slogx/bench_test.go` (after T041): `b.RunParallel` with `WithSampling(10, 100, time.Second)` logging one message at Warn. It documents the cost under contention (research R-10).
- [X] T043 Expand `slogx/doc.go` into the full package documentation (FR-009, research R-18):
  - context attributes and the per-request level apply only to the `*Context` and `LogAttrs` methods; golangci-lint's `sloglint` with `context: all` enforces them;
  - the composition order `LevelHandler → SamplingHandler → ContextHandler → RedactHandler`, and the reason for each position;
  - what redaction covers, and what it does not: the message, and values encoded by the output handler, such as structs, maps and error text; `Secret` is the protection for those, and the limits of research R-08;
  - the sampling rule, the error exemption, the 1,024-group bound, the summary shape, and `Flush` at shutdown;
  - the placeholders `LogValue panicked…`, `!CONTEXTFUNC` and `!DEPTH`, and the duplicate key rule.
- [X] T044 [P] Complete `slogx/README.md` under the placeholder headings of T003:
  - the composed example from the request, adapted to `h, err := slogx.New(...)`;
  - one short example per capability;
  - a `slogxtest.Recorder` example;
  - the note that the middleware's `request_id` and the extraction function do not duplicate the key.
- [X] T045 [P] Fill `slogx/CHANGELOG.md` `## [Unreleased]` → `### Added` with entries for:
  - `With` and `ContextHandler`;
  - `WithContextFunc`;
  - `RedactHandler`, `WithRedactKeys` and `Secret`;
  - `WithLevel` and `LevelHandler`;
  - `SamplingHandler`, `WithSampling`, `WithSampleErrors` and `Flush`;
  - `New`;
  - `ErrInvalidConfig`;
  - `slogxtest.Recorder`.
- [X] T046 Run `cd slogx && GOWORK=off go mod tidy -diff`, which must print nothing, and the cross-build check of [quickstart.md §2](quickstart.md): `go vet ./...` for linux, darwin and windows × amd64 and arm64. There must be no failures. Confirm that `slogx/go.mod` still has no `require` (SC-010).
- [X] T047 Run `FuzzRedact` for 60 s ([quickstart.md §5](quickstart.md)). Fix any failure, then commit the failing input under `slogx/testdata/fuzz/FuzzRedact/` as a regression seed.
- [X] T048 Run the benchmarks of [quickstart.md §4](quickstart.md) with `-count 5`, and record the medians in the PR description. Check the targets:
  - every `BenchmarkDisabled/*`: `0 B/op` and `0 allocs/op` (SC-003);
  - `BenchmarkEnabled/composed`: ≤ 2× the ns/op of `BenchmarkEnabled/bare-same-attrs` (SC-008).

  Optimize the implementation if a target is missed. Do not change the targets.
- [X] T049 Verify the exported surface of `slogx/` and `slogx/slogxtest/` against [contracts/public-api.md](contracts/public-api.md):
  - `GOWORK=off go doc -all .` and `GOWORK=off go doc -all ./slogxtest` in `slogx/` list exactly the contract's identifiers (47 and 13), each with a doc comment;
  - `GOWORK=off go test -coverprofile=c.out ./... && go tool cover -func=c.out` shows no exported function or method at 0% (Principle III).
- [X] T050 Run the manual end-to-end check of [quickstart.md §6](quickstart.md), in a throwaway module outside the repository, with local `replace` directives for `slogx` and `middleware` that are never committed:
  - `requestid` with `accesslog` (and `WithHeaders("Authorization")`), and a handler that calls `logger.InfoContext(ctx, "order created")`;
  - each record carries `request_id` exactly once;
  - `header.Authorization` is `[REDACTED]`;
  - `Flush` prints one summary after 1,000 identical `WarnContext` calls.
- [X] T051 Run the local gates with `make check MODULE=slogx`. golangci-lint must report zero findings, with only the two `//nolint` directives allowed by Conventions, and `go test -race -shuffle=on` must pass with coverage ≥ 80%. Fix any finding before pushing.

---

## Dependencies & Execution Order

### Phase dependencies

- **Setup (Phase 1)**: no dependencies.
- **Foundational (Phase 2)**: depends on Setup and blocks every user story. US1, US2, US3 and US5
  use `options.go` and `forward.go`, and US1, US2 and US4 use `internal/attrs`.
- **US1 (Phase 3)**: depends on Foundational only.
- **US2 (Phase 4)**: depends on Foundational only. It shares `example_test.go` and
  `panic_test.go` with US1, so those edits run after US1's.
- **US3 (Phase 5)**: depends on Foundational and on US1's `context.go` (T015), which it extends
  in T026.
- **US4 (Phase 6)**: depends on Foundational (`internal/attrs`) only. It can run in parallel with
  US1–US3, because it touches only `slogx/slogxtest/`.
- **US5 (Phase 7)**: depends on US1 (`ContextHandler`), US2 (`RedactHandler`, `Secret`) and US3
  (`LevelHandler`), because `New` composes them.
- **US6 (Phase 8)**: depends on Foundational for `SamplingHandler`. Its `New` integration (T039)
  depends on US5, and its tests use `slogxtest.Recorder` (US4).
- **Polish (Phase 9)**: depends on every story.

The stories therefore run in priority order: US1 → US2 → US3 → US4 → US5 → US6. US4 may start
as soon as Foundational is done. Each story adds a complete, testable increment, and each leaves
the earlier stories' tests green.

### Within each story

- Test tasks are written first and observed failing. A compilation failure counts.
- Implementation follows its tests: `context.go` → `contexthandler.go` (US1), `redact.go` and
  `secret.go` (US2), `context.go` → `level.go` (US3), `record.go` → `recorder.go` (US4),
  `sampling.go` → `handler.go` (US6).
- Example tasks depend on the implementation they show.
- Some files are edited by several tasks, which never run in parallel:
  - `slogx/example_test.go`: T017, T024, T028, T035, T040;
  - `slogx/panic_test.go`: T014, T020;
  - `slogx/context.go`: T015, T026;
  - `slogx/handler.go`: T034, T039;
  - `slogx/bench_test.go`: T041, T042.

### Parallel opportunities

- **Setup**: T002 and T003.
- **Foundational**: T006, T008, T009 and T011 together, then T007 and T010.
- **US1**: T012, T013 and T014 together, then T015 → T016 → T017.
- **US2**: T018, T019, T020 and T021 together, then T022 and T023 in parallel, then T024.
- **US3**: T025, then T026 → T027 → T028.
- **US4**: T029, then T030 → T031 → T032. The whole phase can run in parallel with US1–US3.
- **US5**: T033, then T034 → T035.
- **US6**: T036 and T037 together, then T038 → T039 → T040.
- **Polish**: T041 (then T042), T044 and T045 together, then T043, then T046–T051.

---

## Parallel Example: User Story 2

```bash
# Write all failing tests for US2 together:
Task: "T018 redaction tests in slogx/redact_test.go"
Task: "T019 Secret tests in slogx/secret_test.go"
Task: "T020 LogValue panic and depth tests in slogx/panic_test.go"
Task: "T021 FuzzRedact in slogx/fuzz_test.go"

# Then implement in parallel, then add the examples:
"T022 slogx/redact.go" ‖ "T023 slogx/secret.go"  →  "T024 slogx/example_test.go"
```

## Parallel Example: User Story 1 alongside User Story 4

```bash
# After Phase 2, two independent streams:
Task: "T012–T014 US1 failing tests in slogx/context_test.go, contexthandler_test.go, panic_test.go"
Task: "T029 US4 failing tests in slogx/slogxtest/recorder_test.go"
# then T015 → T016 → T017 (slogx/) and T030 → T031 → T032 (slogx/slogxtest/)
```

---

## Requirement Coverage

| Requirement | Tasks |
|-------------|-------|
| FR-001, FR-002 | T001, T016, T022, T027, T031, T034, T038, T049 |
| FR-003, SC-002 | T008, T013, T018, T025, T029, T033, T036 |
| FR-004, SC-003 | T008, T013, T018, T025, T033, T036, T041, T048 |
| FR-005, SC-007 | T014, T016, T020, T022, T025, T029, T036 (case 10) |
| FR-006 | T029 (case 5), T036 (cases 3, 6), T051 (`-race`) |
| FR-007 | T009, T010, T013, T018, T025, T033, T036 |
| FR-008, SC-010 | T001, T017, T046 |
| FR-009 | T017, T024, T028, T032, T035, T040, T043, T044 |
| FR-010 to FR-016, SC-001 | T012, T013, T015, T016, T050 |
| FR-017 to FR-022, SC-004 | T018, T020, T021, T022, T047 |
| FR-023 | T019, T023 |
| FR-024 to FR-026 | T025, T026, T027 |
| FR-027 to FR-033, SC-005, SC-006 | T036, T037, T038, T039 |
| FR-034 to FR-036 | T033, T034, T036 (case 12), T039 |
| FR-037 to FR-041, SC-009 | T029, T030, T031, T032 |
| SC-008 | T041, T048 |
| SC-011 | every test task, T051 |

---

## Implementation Strategy

### MVP first (User Story 1)

1. Phase 1: Setup (T001–T004).
2. Phase 2: Foundational (T005–T011). This is critical: it blocks everything else.
3. Phase 3: US1 (T012–T017).
4. **Stop and validate**: `GOWORK=off go test -race -run 'With|Context|Panic|Nil|Alloc' .`
   passes. Application code now gets `request_id` and `tenant` in every record logged with a
   context.

### Incremental delivery

1. Setup + Foundational: the module builds and the option machinery is tested.
2. + US1: context attributes (MVP).
3. + US2: redaction and `Secret`, the other P1 story.
4. + US3: the per-request level.
5. + US4: the recorder, possibly developed in parallel with US1–US3.
6. + US5: `New` composes level, context and redaction.
7. + US6: sampling, also inside `New`, and `Flush`.
8. Polish: benchmarks against the targets, documentation, CHANGELOG and gates. Then release
   `slogx/v0.1.0`.

Each increment leaves `make check MODULE=slogx` green.

---

## Notes

- [P] tasks touch different files and depend on no incomplete task.
- Commit after each task or logical group. The PR title is
  `feat(slogx): add composable slog handlers module`.
- Do not change the cost targets in T048 to make them pass: optimize instead. If a target proves
  unreachable, update spec.md and research.md first, as was done for SC-008 during planning.
- Replacing the capture handlers of `middleware/accesslog` and `middleware/recovery` with
  `slogxtest.Recorder` is not part of this task list. It is a change to the `middleware` module,
  made after `slogx/v0.1.0` is tagged (spec, Assumptions).

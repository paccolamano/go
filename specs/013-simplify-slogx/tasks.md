---

description: "Task list for slogx 0.2.0: one handler, a default level, a fanout and context values"
---

# Tasks: A Simpler slogx, With a Threshold of Its Own (slogx 0.2.0)

**Input**: Design documents from `specs/013-simplify-slogx/`

**Prerequisites**: [plan.md](plan.md), [spec.md](spec.md), [research.md](research.md),
[data-model.md](data-model.md), [contracts/](contracts/), [quickstart.md](quickstart.md)

**Tests**: REQUIRED. Constitution Principle III is non-negotiable, and SC-008 asks for every
acceptance scenario and edge case to be covered by automated tests:

- Every test task precedes the implementation it covers, and is observed failing first: T022 for
  US1, T030 for US2, T037 and T038 for US3. The removal of the old API is guarded by tests that
  pass before and after it: the golden file of `slogx/v0.1.0` (T003, T004) and the migrated tests
  (T005–T013).
- Each new exported identifier gets a runnable example (T021, T029, T036).
- The benchmarks and the allocation tests cover the code declared performance-sensitive (T048,
  T049).
- Coverage stays well above 80% with `-race -shuffle=on`. The spike measured 98.1%.

**Organization**: tasks are grouped by user story (spec.md US1–US5). The removal of sampling and
of the standalone handlers is in the Foundational phase, because every story builds on the new
option model and on the single `Handler`: it implements the code part of US4 (FR-016 to
FR-019), and the US4 phase completes the upgrade path. All paths are relative to the repository
root. All code lives in the existing module `slogx/`. Nothing outside `slogx/` changes, apart
from ticking the boxes of this file.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: can run in parallel with other [P] tasks of the same phase: different files, and no
  dependency on an incomplete task.
- **[Story]**: US1–US5 from spec.md. Setup, Foundational and Polish tasks have no story label.

## Conventions for every task

- **Commands** run from `slogx/` with `GOWORK=off`, unless a task says otherwise. `make` targets,
  `git` and `scripts/` run from the repository root.
- **Packages**: behavior tests use the external test package `slogx_test`. Only
  `options_internal_test.go` and the new `redact_internal_test.go` use the internal package
  `slogx`.
- **Existing helpers** (reuse them, do not duplicate them):
  - `slogx/helpers_test.go`: `must(h, err)`, `dropTime`, `newJSON(w, level)` (a JSON handler
    without the time; `nil` level means Info), `lines(tb, buf)`, `onlyLine(tb, buf)`,
    `decode(tb, line)`, `ridKey`, `withRID(ctx, id)`, `ridAttrs(ctx)` (an extraction function
    for `request_id`), `recordWith(attrs...)` (an Info record with message `m`);
  - `slogx/contexthandler_test.go`: `alwaysAttr`, `requestContext()` (attached `tenant=t1`, and
    the identifier `abc` for `ridAttrs`), `recordingHandler{records, err}` (enables every level,
    keeps clones of the records, returns `err`);
  - `slogx/redact_test.go`: `sensitiveKeys` (`authorization`, `password`, `token`, `cookie`),
    `redactLogger(buf)`, `userValue{name, token}` (a `LogValuer`), `countingValuer`;
  - `slogx/context_test.go`: `contextLogger(tb, buf, opts...)`; `slogx/level_test.go`:
    `levelLogger(buf, threshold)`;
  - `slogx/conformance_test.go`: `testConformance(t, wrap)`; `slogx/alloc_test.go`:
    `assertNoAllocsWhenDisabled(tb, h, ctxs...)` (the handler must disable Debug for each
    context);
  - `slogx/example_test.go`: `stdoutJSON()` (JSON on stdout without the time, at Info),
    `requestIDKey`, `requestIDAttrs`;
  - `slogxtest.NewRecorder()`: without `slogxtest.WithLevel` it enables every level, like the
    OpenTelemetry bridge. `Records()`, `Len()`, `Last()`, and `Record.Attr(path...)`;
    `slogxtest.Record` has the fields `Time`, `Level`, `Message`, `Attrs`.
- **Test names** are fixed by the tasks, so that the `-run` patterns of
  [quickstart.md §4](quickstart.md#4-scenario-checks-per-user-story) select them. The
  [coverage map](#coverage-map-sc-008) lists them by scenario.
- **Test style**: table-driven with `t.Run` where there are several cases; no `time.Sleep`, no
  network listener, no shared state between tests; `sync.WaitGroup` for concurrency;
  `httptest.NewRequestWithContext(t.Context(), …)` and `httptest.NewRecorder` for HTTP; helpers
  call `tb.Helper()` first (`thelper`). An identity check of an error uses `err != want` with
  `//nolint:errorlint // identity check (FR-009)`, as the existing tests do for FR-005.
- **Lint**: golangci-lint must report zero findings at every checkpoint. Library code adds **no**
  `//nolint`; the only one left in it is `wrapcheck` in `slogx/forward.go`. Imports group
  `github.com/paccolamano/go` last (`goimports`). Run `gofmt -w` on every Go file you edit:
  adding a field to a struct realigns its comments. The `unused` linter fails on a helper that
  nothing uses yet: add each helper in the task that first uses it.
- **Documentation**: when a contract or a task gives the text of a doc comment, a paragraph or a
  CHANGELOG entry, use it verbatim, wrapped at 80 columns in Go comments and 100 in Markdown.
- **Code blocks** in this file come from the spike, which passed lint and the suite. Use them as
  given.
- **Commits**: do not commit, push or open a pull request unless the maintainer asks for it.

---

## Phase 1: Setup (Shared Infrastructure)

**Purpose**: work on a feature branch from a known state.

- [X] T001 From the repository root, confirm that `git status --short` lists only `go.work.sum` and `specs/013-simplify-slogx/` as untracked, with no change under `slogx/`; that `git tag --list 'slogx/*'` ends with `slogx/v0.1.0`; and that `git diff --quiet slogx/v0.1.0 HEAD -- slogx` succeeds (the module has not changed since its release). If the current branch is `main`, create the feature branch with `git switch -c 013-simplify-slogx` before editing any file. Creating the branch is not a commit.

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: guard the output of 0.1.0, migrate the tests to `New`, then remove sampling, the
standalone handlers and the option applicability checks (FR-016 to FR-020; research R-03, R-08,
R-09).

**⚠️ CRITICAL**: no user story work begins until this phase is complete. T016–T019 change files
that only compile together: do T016, T017, T018 and T019 in order, and run nothing between them.

- [X] T002 Record the baseline. From the repository root, run `make check MODULE=slogx`, which must report `PASS` for lint and test. From `slogx/`, run `GOWORK=off go test -cover ./...` and record the coverage of `slogx`, `slogx/internal/attrs` and `slogx/slogxtest`.
- [X] T003 Create `slogx/golden_test.go`, package `slogx_test` (FR-020, SC-005; US4 scenario 1; research R-09). It must compile, unchanged, against `slogx/v0.1.0`: use only `slogx.New`, `slogx.With`, `slogx.WithLevel`, `slogx.WithContextFunc`, `slogx.WithRedactKeys` and `slogx.NewSecret`, and declare every helper in this file with the prefix `golden` (do not use the helpers of other test files). Imports: `bytes`, `context`, `errors`, `flag`, `fmt`, `log/slog`, `os`, `strings`, `testing` and `github.com/paccolamano/go/slogx`.
  1. **Flag**: `var goldenWrite = flag.Bool("golden.write", false, "write testdata/v0.1.0.golden; run only on a checkout of slogx/v0.1.0")`, with the doc comment `// goldenWrite rewrites testdata/v0.1.0.golden. Run it only on a checkout of slogx/v0.1.0, with this file copied there; never on the current code.`
  2. **Helpers**:
     - `type goldenRIDKey struct{}`;
     - `type goldenUser struct{ name, token string }` with `func (u goldenUser) LogValue() slog.Value` returning `slog.GroupValue(slog.String("name", u.name), slog.String("token", u.token))`;
     - `goldenRID(ctx context.Context) []slog.Attr`: `request_id` from `ctx.Value(goldenRIDKey{}).(string)` when present, else nil;
     - `goldenTenant(context.Context) []slog.Attr`: returns `slog.String("tenant", "from-func")`, `slog.String("request_id", "from-func")`, `slog.Attr{}` and `slog.Group("", slog.Int("inl", 1))`;
     - `goldenPanicFunc(context.Context) []slog.Attr`: `panic("extract")`.
  3. **`goldenOutput() (string, error)`** logs every case through every configuration and output:
     - `keys := []string{"authorization", "password", "token", "cookie"}`;
     - configurations, as `name, opts`: `none` (no option); `context` (`WithContextFunc(goldenRID)`); `redact` (`WithRedactKeys(keys...)`); `both` (`WithContextFunc(goldenRID)`, `WithRedactKeys(keys...)`); `many` (`WithContextFunc(goldenRID)`, `WithContextFunc(goldenTenant)`, `WithContextFunc(goldenPanicFunc)`, `WithRedactKeys("token")`, `WithRedactKeys("Cookie", "secret")`);
     - outputs, as `name, func(*bytes.Buffer) slog.Handler`, each with a `ReplaceAttr` that drops the top-level `slog.TimeKey`: `json-info` (`slog.NewJSONHandler`, no level), `json-warn` (`slog.NewJSONHandler`, `Level: slog.LevelWarn`), `text-debug` (`slog.NewTextHandler`, `Level: slog.LevelDebug`);
     - `var nilCtx context.Context`, and `req := slogx.With(context.WithValue(context.Background(), goldenRIDKey{}, "abc"), slog.String("token", "s3cr3t"), slog.String("tenant", "t1"))`;
     - cases, as `name, func(*slog.Logger)`, in this order:
       1. `plain`: `l.InfoContext(req, "plain", "password", "hunter2", "n", 1)`;
       2. `background`: `l.Info("background", "a", 1)`;
       3. `nil context`: `l.Log(nilCtx, slog.LevelWarn, "nil context")`;
       4. `levels`: for each of Debug, Info, Warn, Error, `l.Log(req, level, "level")`;
       5. `context level debug`: `l.DebugContext(slogx.WithLevel(req, slog.LevelDebug), "debug for one request")`;
       6. `context level error`: with `ctx := slogx.WithLevel(req, slog.LevelError)`, `l.WarnContext(ctx, "silenced")` then `l.ErrorContext(ctx, "kept")`;
       7. `groups`: `l.WithGroup("db").With("table", "orders", "Authorization", "x").WithGroup("q").InfoContext(req, "grouped", "rows", 3)`;
       8. `logger attrs`: `l.With("service", "api", "cookie", "c", "tenant", "logger").InfoContext(req, "logger")`;
       9. `duplicates`: `l.InfoContext(req, "dup", "request_id", "own", "tenant", "own")`;
       10. `sealed group`: `l.WithGroup("Token").InfoContext(req, "sealed", "a", 1)`;
       11. `valuers`: `l.InfoContext(req, "valuers", "user", goldenUser{"bob", "t0k"}, "pin", slogx.NewSecret(1234))`;
       12. `nested`: `l.InfoContext(req, "nested", slog.Group("a", slog.Group("b", slog.String("password", "p"), slog.Group("", slog.Int("inline", 1)), slog.Group("empty"))))`;
       13. `error`: `l.ErrorContext(req, "failed", "err", errors.New("token=abc"))`.
     - For each configuration, then each output: build `var buf bytes.Buffer` and `h, err := slogx.New(output(&buf), opts...)`, returning `err` if any. For each case: `buf.Reset()`, run the case with `slog.New(h)`, then `fmt.Fprintf(&out, "## %s / %s / %s\n%s", config, output, case, buf.String())` into a `strings.Builder`.
     - Do not add a `LogValue` that panics: slog writes a stack trace with absolute paths, which differs between checkouts (`panic_test.go` covers it).
  4. **`TestNew_SameOutputAsV010`**, with the doc comment `// TestNew_SameOutputAsV010 compares the output of configurations without the options added in 0.2.0 with the output of slogx/v0.1.0 (FR-020, SC-005).`: compute `goldenOutput()`. With `-golden.write`, create `testdata` with `os.MkdirAll("testdata", 0o755)`, write `testdata/v0.1.0.golden` with mode `0o644`, and return. Otherwise read the file and compare byte for byte. On a difference, report the first differing line, 1-based, with both versions, or the two line counts.
- [X] T004 Produce `slogx/testdata/v0.1.0.golden` from `slogx/v0.1.0`, never from the working tree. From the repository root:

  ```bash
  git worktree add /tmp/slogx-v010 slogx/v0.1.0
  command cp slogx/golden_test.go /tmp/slogx-v010/slogx/
  (cd /tmp/slogx-v010/slogx && GOWORK=off go test -run TestNew_SameOutputAsV010 -golden.write .)
  mkdir -p slogx/testdata && command cp /tmp/slogx-v010/slogx/testdata/v0.1.0.golden slogx/testdata/
  git worktree remove --force /tmp/slogx-v010
  ```

  The file must have 195 sections (`grep -c '^## ' slogx/testdata/v0.1.0.golden`) and about 380 lines, with no `"time"` and no absolute path. Then, from `slogx/`, `GOWORK=off go test -race -run TestNew_SameOutputAsV010 -v .` passes. From now on it guards every change.
- [X] T005 [P] In `slogx/context_test.go`, make `contextLogger` build `slog.New(must(slogx.New(newJSON(buf, nil), opts...)))`, and change its doc comment to `// contextLogger returns a logger that writes the context attributes as JSON into buf, through New.` Nothing else changes.
- [X] T006 [P] In `slogx/panic_test.go`, replace both `slogx.NewContextHandler(` calls with `slogx.New(`, and rename `TestNil_ContextHandler` to `TestNil_ContextHandle`. Nothing else changes.
- [X] T007 [P] In `slogx/contexthandler_test.go` (US4 scenario 4):
  - replace every `slogx.NewContextHandler(` with `slogx.New(`;
  - rename every `TestContextHandler_X` to `TestContext_X`, and `TestAlloc_ContextHandlerDisabled` to `TestAlloc_ContextDisabled`;
  - in `TestContext_Options`, delete the row `{"inapplicable option", …}`, and change the message `"NewContextHandler = %v, %v; want nil, ErrInvalidConfig"` to `"New = %v, %v; want nil, ErrInvalidConfig"`.

  Every expected output stays byte for byte as it is.
- [X] T008 [P] In `slogx/redact_test.go` (US4 scenario 4):
  - `redactLogger` builds `slog.New(must(slogx.New(newJSON(buf, nil), slogx.WithRedactKeys(sensitiveKeys...))))`, with the doc comment `// redactLogger returns a logger that writes into buf through New, with the sensitive keys.`;
  - replace every other `slogx.NewRedactHandler(` with `slogx.New(`;
  - rename every `TestRedactHandler_X` to `TestRedact_X`, and `TestAlloc_RedactHandlerDisabled` to `TestAlloc_RedactDisabled`;
  - in `TestRedact_Options`, delete the rows `{"missing required option", …}` and `{"inapplicable option", …}`, and change the message to `"New = %v, %v; want nil, ErrInvalidConfig"`;
  - in `TestRedact_Derived`, delete the three lines that build `sealed` and check that it returns itself. T019 moves that check to an internal test.

  Every expected output stays byte for byte as it is.
- [X] T009 [P] In `slogx/level_test.go` (US4 scenario 4):
  - replace every `slogx.NewLevelHandler(` with `slogx.New(`, and `*slogx.LevelHandler` with `*slogx.Handler`;
  - rename every `TestLevelHandler_X` to `TestLevel_X`, and `TestAlloc_LevelHandlerDisabled` to `TestAlloc_LevelDisabled`;
  - in `TestLevel_Options`, delete the `"inapplicable option"` entry of the map, and change the message to `"New = %v, %v; want nil, ErrInvalidConfig"`.
- [X] T010 [P] In `slogx/handler_test.go`, delete `manualChain` and `TestCompose_EquivalentToManual`: the golden test of T003 replaces the comparison with the manual chain. Keep `composedCases`, which `slogx/sampling_test.go` still uses; T018 deletes it with the sampling tests. Change the doc comment of `composedOptions` to `// composedOptions are the options of the composed example.` Keep every other test as it is.
- [X] T011 [P] In `slogx/fuzz_test.go`, replace `slogx.NewRedactHandler(` with `slogx.New(`.
- [X] T012 [P] In `slogx/example_test.go`:
  - `ExampleWith` builds its handler with `slogx.New(stdoutJSON())`;
  - rename `ExampleNewContextHandler` to `ExampleWithContextFunc`, built with `slogx.New(stdoutJSON(), slogx.WithContextFunc(requestIDAttrs))`;
  - rename `ExampleNewRedactHandler` to `ExampleWithRedactKeys`, built with `slogx.New(stdoutJSON(), slogx.WithRedactKeys("authorization", "password", "token", "cookie"))`;
  - `ExampleWithLevel` builds its handler with `slogx.New(stdoutJSON())`.

  Keep every comment and `// Output:` block. Leave `ExampleNewSamplingHandler` for T019.
- [X] T013 With the library still as in `slogx/v0.1.0`, from `slogx/`, run `GOWORK=off go test -race -shuffle=on ./...`, then `make check MODULE=slogx` from the repository root. Both pass: the tests of the former standalone handlers, built with `New`, give the same outputs on the code of 0.1.0 (US4 scenario 4). `git diff --stat -- slogx` lists only test files and `testdata/`.
- [X] T014 Rewrite `slogx/options_internal_test.go`, package `slogx`, for the per-option validation (FR-017; research R-03; data-model §1). Remove `allOptions`, `TestOptions_Applicability`, every sampling row and the `time` import. Keep `fnAttrs`. Then:
  - `TestOptions_Validation`: a table over `name, opts []Option, want string`, calling `newConfig(tt.opts)` and asserting `errors.Is(err, ErrInvalidConfig)` and `strings.HasSuffix(err.Error(), tt.want)`. Rows: `"nil extraction function"` (`WithContextFunc(nil)`, `WithContextFunc: function must not be nil`); `"no key"` (`WithRedactKeys()`, `WithRedactKeys: at least one key is required`); `"empty key"` (`WithRedactKeys("token", "")`, `WithRedactKeys: empty key`); `"whitespace key"` (`WithRedactKeys(" \t")`, `WithRedactKeys: empty key`); `"nil option"` (`WithRedactKeys("token"), nil`, `options[1]: must not be nil`); `"first invalid option reported"` (`WithRedactKeys(""), WithContextFunc(nil)`, `WithRedactKeys: empty key`).
  - `TestOptions_Valid`: `newConfig(nil)` and `newConfig([]Option{WithContextFunc(fnAttrs("a")), WithRedactKeys("token")})` return no error.
  - `TestOptions_ErrorFormat`: `newConfig([]Option{WithRedactKeys("")})` returns exactly `slogx: invalid configuration: New: WithRedactKeys: empty key`; `invalid("next", "must not be nil").Error()` is exactly `slogx: invalid configuration: New: next: must not be nil`, and wraps `ErrInvalidConfig`.
  - `TestOptions_Accumulation`: `newConfig` of `WithContextFunc(fnAttrs("first"))`, `WithRedactKeys("a", "b")`, `WithContextFunc(fnAttrs("second"))`, `WithRedactKeys("c")` keeps `cfg.fns` in call order (keys `first`, `second`) and `cfg.keys` as `a,b,c`.

  Run `GOWORK=off go vet ./...` and observe the failure: `newConfig` and `invalid` do not have these signatures yet.
- [X] T015 Rewrite `slogx/options.go` (FR-016, FR-017; research R-03). The file becomes:

  ```go
  package slogx

  import (
  	"context"
  	"errors"
  	"fmt"
  	"log/slog"
  	"strings"
  )

  // ErrInvalidConfig is wrapped by every error New returns.
  var ErrInvalidConfig = errors.New("slogx: invalid configuration")

  // Option configures New. Every option applies, in any combination.
  type Option func(*config)

  type config struct {
  	fns  []func(context.Context) []slog.Attr
  	keys []string
  	err  error // the first invalid option, which New reports
  }

  // fail records the first invalid option.
  func (c *config) fail(option, reason string) {
  	if c.err == nil {
  		c.err = invalid(option, reason)
  	}
  }

  // WithContextFunc adds a function that computes attributes from the context of
  // every handled record, for example a request identifier stored by another
  // package. It is called only for enabled records. It can be given several
  // times: when two functions return the same key, the first one configured wins,
  // and attributes attached with With win over both.
  func WithContextFunc(fn func(ctx context.Context) []slog.Attr) Option {
  	return func(c *config) {
  		if fn == nil {
  			c.fail("WithContextFunc", "function must not be nil")
  			return
  		}
  		c.fns = append(c.fns, fn)
  	}
  }

  // WithRedactKeys adds sensitive keys. The value of every attribute whose key
  // equals one of them, compared without regard to case and at any depth, is
  // written as [REDACTED]. It can be given several times: the keys accumulate.
  func WithRedactKeys(keys ...string) Option {
  	return func(c *config) {
  		if len(keys) == 0 {
  			c.fail("WithRedactKeys", "at least one key is required")
  			return
  		}
  		for _, k := range keys {
  			if strings.TrimSpace(k) == "" {
  				c.fail("WithRedactKeys", "empty key")
  				return
  			}
  		}
  		c.keys = append(c.keys, keys...)
  	}
  }

  // newConfig applies opts in order, and returns the first invalid option.
  func newConfig(opts []Option) (config, error) {
  	var c config
  	for i, opt := range opts {
  		if opt == nil {
  			return config{}, invalid(fmt.Sprintf("options[%d]", i), "must not be nil")
  		}
  		opt(&c)
  	}
  	if c.err != nil {
  		return config{}, c.err
  	}
  	return c, nil
  }

  // invalid returns an error that wraps ErrInvalidConfig, about subject: an
  // option, or a parameter of New.
  func invalid(subject, reason string) error {
  	return fmt.Errorf("%w: New: %s: %s", ErrInvalidConfig, subject, reason)
  }
  ```

  `optionSet`, `optionNames`, `WithSampling`, `WithSampleErrors`, `errNilHandler`, the method `validate` and the sampling fields are gone.
- [X] T016 Rewrite `slogx/handler.go`, and delete `slogx/level.go` (FR-016, FR-018, FR-019; research R-08; data-model §2). `Handler` holds only `next`; its `Enabled` is the former `LevelHandler.Enabled`; `Flush` and the sampler are gone:

  ```go
  // Handler is the slog.Handler built by New. It passes each record through, in
  // order:
  //
  //  1. the level, decided once for the whole pipeline (see Handler.Enabled);
  //  2. the context attributes, written at the top level of the record;
  //  3. the redaction of the sensitive keys given with WithRedactKeys, so that
  //     the context attributes are redacted too;
  //  4. the next handler given to New.
  type Handler struct {
  	next slog.Handler // the context handler, then the redaction handler, then the next handler given to New
  }

  // New returns a Handler that passes records to next, typically a
  // slog.JSONHandler or a slog.TextHandler. The context attributes and the
  // per-request level are always active, and every option applies.
  //
  // It returns an error wrapping ErrInvalidConfig when next is nil or an option
  // is invalid.
  func New(next slog.Handler, opts ...Option) (*Handler, error) {
  	if next == nil {
  		return nil, invalid("next", "must not be nil")
  	}
  	cfg, err := newConfig(opts)
  	if err != nil {
  		return nil, err
  	}
  	h := next
  	if len(cfg.keys) > 0 {
  		h = &redactHandler{next: h, keys: cfg.keys}
  	}
  	return &Handler{next: &contextHandler{root: h, next: h, fns: cfg.fns}}, nil
  }

  // Enabled reports whether records at level are handled for ctx. A level
  // attached to ctx with WithLevel decides alone; otherwise the next handler
  // decides.
  func (h *Handler) Enabled(ctx context.Context, level slog.Level) bool {
  	if threshold, ok := levelFrom(ctx); ok {
  		return level >= threshold
  	}
  	return h.next.Enabled(ctx, level)
  }
  ```

  Keep `Handle` (`return handle(ctx, h.next, r)`). `WithAttrs` and `WithGroup` keep their early returns for no attribute or an empty name, and return `&Handler{next: h.next.WithAttrs(attrs)}` and `&Handler{next: h.next.WithGroup(name)}`, with the doc comments `// WithAttrs returns a Handler whose pipeline has the attributes.` and `// WithGroup returns a Handler whose pipeline has the group open.`
- [X] T017 Unexport the standalone handlers, and delete `slogx/sampling.go` (FR-016, FR-018):
  1. In `slogx/contexthandler.go`, rename the type `ContextHandler` to `contextHandler` everywhere, and delete `NewContextHandler` and its doc comment. The type's doc comment starts `// contextHandler adds context attributes to every record:`; the rest of the file does not change.
  2. In `slogx/redact.go`, rename `RedactHandler` to `redactHandler` everywhere, and delete `NewRedactHandler` and its doc comment. The type's doc comment starts `// redactHandler replaces the values of sensitive keys`.
  3. In `slogx/context.go`, in the doc comment of `With`, replace `ContextHandler adds them` with `The Handler built by New adds them`; in the doc comment of `WithLevel`, replace `LevelHandler compares` with `The Handler built by New compares`.
  4. Delete `slogx/sampling.go`.
- [X] T018 Remove the tests of the removed code in `slogx/sampling_test.go`, `slogx/sampling_internal_test.go`, `slogx/handler_test.go`, `slogx/example_test.go` and `slogx/bench_test.go`, and keep the check of T008 in `slogx/redact_internal_test.go` (FR-018):
  1. Delete `slogx/sampling_test.go` and `slogx/sampling_internal_test.go`. Then delete `composedCases` and its doc comment from `slogx/handler_test.go`: nothing uses it any more, and `unused` would flag it.
  2. In `slogx/example_test.go`, delete `ExampleNewSamplingHandler` and its comment, and the `time` import, which nothing else uses.
  3. In `slogx/bench_test.go`: `benchHandlers` returns only `"composed"`, `New(next, slogx.WithContextFunc(ridAttrs), slogx.WithRedactKeys(sensitiveKeys...))`, and `"composed-group"`, its `WithGroup("db")`, with the doc comment `// benchHandlers returns the handlers that the benchmarks compare, over next.`; delete `BenchmarkParallelSampling`. In the doc comment of `BenchmarkEnabled`, the requirement becomes `(SC-007 of spec 013)`; in that of `BenchmarkDisabled`, `(FR-021 of spec 013)`.
  4. Create `slogx/redact_internal_test.go`, package `slogx`, with `TestRedact_SealedReturnsItself` and the doc comment `// TestRedact_SealedReturnsItself checks that a group opened under a sensitive key seals the handler: later groups and attributes are dropped without building new handlers.` It builds `h := &redactHandler{next: slog.NewJSONHandler(io.Discard, nil), keys: []string{"token"}}` and `sealed := h.WithGroup("token")`, and reports an error unless `sealed.WithGroup("x") == sealed` and `sealed.WithAttrs([]slog.Attr{slog.Int("a", 1)}) == sealed`.
- [X] T019 From `slogx/`, run `GOWORK=off go vet ./...` and `GOWORK=off go test -race -shuffle=on ./...`: both pass, `TestNew_SameOutputAsV010` included. `GOWORK=off go doc -short .`, which lists the declarations without the package comment, shows no `ContextHandler`, `LevelHandler`, `RedactHandler`, `SamplingHandler`, their constructors, `WithSampling`, `WithSampleErrors` or `Flush`. From the repository root, `make check MODULE=slogx` reports `PASS`. `slogx/doc.go` and `slogx/README.md` still describe 0.1.0 until T043 and T044.

**Checkpoint**: one handler, every option applies, the output of 0.1.0 is unchanged, and every
story can begin.

---

## Phase 3: User Story 1 - slogx decides the level, whatever the next handler (Priority: P1) 🎯 MVP

**Goal**: `WithDefaultLevel` sets the threshold of the records whose context carries no level,
without asking the next handler (FR-001 to FR-005).

**Independent Test**: build the handler with a default level over a next handler that enables
every level, then log records at every level, with and without a level in the context.

### Tests for User Story 1 ⚠️ (write first, see them fail)

- [X] T020 [US1] In `slogx/level_test.go`, add the following tests, after `TestAlloc_LevelDisabled`. Add the imports `sync` and `github.com/paccolamano/go/slogx/slogxtest`. Unless stated, the logger is `slog.New(must(slogx.New(next, opts...)))` and the context `context.Background()`.
  - `TestDefaultLevel_OverAllLevelsOutput` (US1-1): `rec := slogxtest.NewRecorder()`, with `WithDefaultLevel(slog.LevelInfo)`. First assert `rec.Enabled(ctx, slog.LevelDebug)`, so the output enables every level. Log `DebugContext(ctx, "debug")` and `InfoContext(ctx, "info")`: `rec.Len()` is 1 and `rec.Last()` has the message `info`.
  - `TestDefaultLevel_ContextLevelWins` (US1-2; edge "per-request level above the default"): over a recorder, with the default Info, a table over `name, ctxLevel, recordLevel, written`: `"debug request, debug record"` (Debug, Debug, true); `"warn request, info record"` (Warn, Info, false); `"warn request, warn record"` (Warn, Warn, true). Each row logs one record with `logger.Log(slogx.WithLevel(ctx, tt.ctxLevel), tt.recordLevel, "m")` into a fresh recorder.
  - `TestDefaultLevel_OutputThresholdIgnored` (US1-3, FR-004): over `newJSON(&buf, nil)`, whose own threshold is Info, with `WithDefaultLevel(slog.LevelDebug)`: `DebugContext(ctx, "m")` writes exactly `{"level":"DEBUG","msg":"m"}`.
  - `TestDefaultLevel_LevelVar` (US1-4, FR-003): `var level slog.LevelVar` (Info), over a recorder, with `WithDefaultLevel(&level)`. A Debug record is not written; after `level.Set(slog.LevelDebug)`, a Debug record through the same logger is written.
  - `TestDefaultLevel_None` (US1-5): without `WithDefaultLevel`, over `newJSON(&buf, slog.LevelWarn)`, Info is not written and Warn is; over a recorder, Debug is written. The next handler decides, as in 0.1.0.
  - `TestDefaultLevel_Invalid` (US1-6, FR-005): a table over `name, opts`: `"nil"` (`slogx.WithDefaultLevel(nil)`); `"nil level variable"` (`slogx.WithDefaultLevel(nilVar)` with `var nilVar *slog.LevelVar`); `"nil, then valid"` (`slogx.WithDefaultLevel(nil), slogx.WithDefaultLevel(slog.LevelInfo)`). `slogx.New(newJSON(&bytes.Buffer{}, nil), tt.opts...)` returns a nil handler and an error that wraps `slogx.ErrInvalidConfig` and ends with `WithDefaultLevel: must not be nil`.
  - `TestDefaultLevel_LastWins` (edge "default level given twice"): over `newJSON(&buf, nil)`, with `WithDefaultLevel(slog.LevelWarn), WithDefaultLevel(slog.LevelDebug)`: a Debug record is written.
  - `TestDefaultLevel_NilContext`: `h := must(slogx.New(slogxtest.NewRecorder(), slogx.WithDefaultLevel(slog.LevelInfo)))` and `var nilCtx context.Context`: `h.Enabled(nilCtx, slog.LevelDebug)` is false and `h.Enabled(nilCtx, slog.LevelInfo)` is true.
  - `TestDefaultLevel_Derived`: over a recorder, with the default Info, `logger.With("a", 1).WithGroup("g")` writes an Info record and not a Debug record: derived handlers keep the default level.
  - `TestDefaultLevel_Concurrent` (SC-002; edge "a level variable changed while records are being logged"): `var level slog.LevelVar`, a recorder, `WithDefaultLevel(&level)`, `const workers, perWorker = 50, 20`. A local `phase(msg string)` starts `workers` goroutines with `wg.Go`, each logging `perWorker` Debug records, then waits. Run `phase("before")`: `rec.Len()` is 0. Then, in one `sync.WaitGroup`, start a goroutine that calls `level.Set(slog.LevelDebug)` and `workers` goroutines that each log one Debug record, and wait; any count is accepted for these. Record `before := rec.Len()`, run `phase("after")`, and assert `rec.Len()-before == workers*perWorker`. Run under `-race`.
  - `TestDefaultLevel_Conformance`: `testConformance(t, func(next slog.Handler) slog.Handler { return must(slogx.New(next, slogx.WithDefaultLevel(slog.LevelDebug))) })`.
  - `TestAlloc_DefaultLevelDisabled` (FR-021): for a fixed `slog.LevelInfo` and for a `*slog.LevelVar` at Info, `h := must(slogx.New(slogxtest.NewRecorder(), slogx.WithDefaultLevel(lv), slogx.WithContextFunc(ridAttrs), slogx.WithRedactKeys("token")))`, then `assertNoAllocsWhenDisabled(t, h, requestContext(), slogx.WithLevel(requestContext(), slog.LevelWarn))`.
- [X] T021 [P] [US1] In `slogx/example_test.go`, add `ExampleWithDefaultLevel`, after `ExampleWithLevel` (FR-025), with the comment `// The default level applies to the records logged with a context that carries no level, whatever the output enables. A *slog.LevelVar changes it at run time, and a request can still set its own.`:

  ```go
  level := new(slog.LevelVar) // Info
  h, err := slogx.New(stdoutJSON(), slogx.WithDefaultLevel(level))
  if err != nil {
  	panic(err)
  }
  logger := slog.New(h)
  ctx := context.Background()

  logger.DebugContext(ctx, "hidden at Info")
  level.Set(slog.LevelDebug)
  logger.DebugContext(ctx, "written at Debug")
  level.Set(slog.LevelWarn)
  logger.InfoContext(ctx, "hidden at Warn")
  logger.InfoContext(slogx.WithLevel(ctx, slog.LevelInfo), "written for one request")
  // Output:
  // {"level":"DEBUG","msg":"written at Debug"}
  // {"level":"INFO","msg":"written for one request"}
  ```

  `stdoutJSON()` is at Info: the Debug record shows that the output's own level is not asked.
- [X] T022 [US1] In `slogx/options.go`, add the field `level slog.Leveler // nil without WithDefaultLevel` to `config`, and `WithDefaultLevel` after the type `Option`, with the doc comment of [contracts/public-api.md § Added](contracts/public-api.md#added-fr-001-to-fr-015) without its last sentence ("It is required when the next handler is a Fanout.", added by T034):

  ```go
  func WithDefaultLevel(level slog.Leveler) Option {
  	return func(c *config) {
  		if lv, ok := level.(*slog.LevelVar); level == nil || ok && lv == nil {
  			c.fail("WithDefaultLevel", "must not be nil")
  			return
  		}
  		c.level = level
  	}
  }
  ```

  `New` does not use the level yet. Run `GOWORK=off go test -race -run 'TestDefaultLevel_|TestAlloc_DefaultLevel|ExampleWithDefaultLevel' -v .` and observe the expected failures: `OverAllLevelsOutput`, `OutputThresholdIgnored`, `LevelVar`, `LastWins`, `NilContext`, `Derived`, `Concurrent`, `TestAlloc_DefaultLevelDisabled` (its recorder enables Debug) and `ExampleWithDefaultLevel` fail; `ContextLevelWins`, `None`, `Invalid` and `Conformance` pass. Do not continue until these failures are seen.

### Implementation for User Story 1

- [X] T023 [US1] In `slogx/handler.go` (FR-001 to FR-004; research R-04; data-model §2):
  1. Add the field `level slog.Leveler // the default level, nil without WithDefaultLevel` to `Handler`, before `next`.
  2. In `New`, return `&Handler{level: cfg.level, next: …}`.
  3. Replace `Enabled` and its doc comment with:

     ```go
     // Enabled reports whether records at level are handled for ctx:
     //
     //  1. a level attached to ctx with WithLevel decides alone;
     //  2. otherwise, the default level given with WithDefaultLevel decides, read
     //     at this call;
     //  3. otherwise, the next handler decides.
     //
     // No handler after it checks the level again: an enabled record reaches the
     // next handler whatever the next handler's own level.
     func (h *Handler) Enabled(ctx context.Context, level slog.Level) bool {
     	if threshold, ok := levelFrom(ctx); ok {
     		return level >= threshold
     	}
     	if h.level != nil {
     		return level >= h.level.Level()
     	}
     	return h.next.Enabled(ctx, level)
     }
     ```
  4. `WithAttrs` and `WithGroup` copy the level: `&Handler{level: h.level, next: …}`.

  In `slogx/context.go`, in the doc comment of `WithLevel`, replace `ignoring the threshold of the next handler in both directions` with `ignoring the default level and the threshold of the next handler, in both directions`.
- [X] T024 [US1] Run `GOWORK=off go test -race -shuffle=on ./...`: every test of T020 and T021 passes, and every earlier test still passes, `TestNew_SameOutputAsV010` included. From the repository root, `make check MODULE=slogx` reports `PASS`.

**Checkpoint**: the level of a service applies whatever the next handler enables, and the
per-request level still works. This is the MVP.

---

## Phase 4: User Story 2 - The same records on every output (Priority: P1)

**Goal**: `Fanout` passes every record to every output without a second level check, and `New`
requires a default level over it (FR-006 to FR-011).

**Independent Test**: build the handler with a default level over a fanout of two recording
outputs, one of them built with default options, then compare what each output received.

### Tests for User Story 2 ⚠️ (write first, see them fail)

- [X] T025 [US2] Create `slogx/fanout_test.go`, package `slogx_test`, with these helpers (only this file uses them):
  - `type traceKey struct{}`; `withTrace(ctx context.Context, id string) context.Context`, doc `// withTrace stores a trace identifier the way a tracing library would.`; `traceAttrs(ctx context.Context) []slog.Attr`, doc `// traceAttrs returns trace_id and span_id, as the README recipe does with OpenTelemetry.`, returning `slog.String("trace_id", id)` and `slog.String("span_id", "s-"+id)` when the context carries an `id`, and nil otherwise;
  - `appendingHandler{value string; records []slog.Record}`, doc `// appendingHandler adds added=value to every record it receives, without cloning it, and keeps the record.` `Enabled` returns true; `Handle(_ context.Context, r slog.Record) error` calls `r.AddAttrs(slog.String("added", h.value))`, appends `r` to `h.records` and returns nil; `WithAttrs` and `WithGroup` return `h`.

  Then the tests of T026–T028. Imports as needed, among them `fmt`, `strings`, `time` and `github.com/paccolamano/go/slogx/slogxtest`.
- [X] T026 [US2] In `slogx/fanout_test.go`, add the scenario tests. "Default Info" means `slogx.WithDefaultLevel(slog.LevelInfo)`; "stdout" is `newJSON(&stdout, nil)`, an output built with default options; "otel" is `slogxtest.NewRecorder()`, which enables every level.
  - `TestFanout_SameRecords` (US2-1, US2-2): `logger := slog.New(must(slogx.New(slogx.Fanout(stdout, otel), default Info)))`. Log `InfoContext(ctx, "info")`, `DebugContext(ctx, "debug")` and `DebugContext(slogx.WithLevel(ctx, slog.LevelDebug), "request debug")`. Stdout lines are exactly `{"level":"INFO","msg":"info"}` and `{"level":"DEBUG","msg":"request debug"}`; otel has 2 records, with the messages `info` and `request debug` and the levels Info and Debug.
  - `TestFanout_IgnoresOutputLevels` (US2-3): over `slogx.Fanout(newJSON(&buf, slog.LevelError))` with default Info, `logger.Info("m")` writes exactly `{"level":"INFO","msg":"m"}`.
  - `TestFanout_RequiresDefaultLevel` (US2-4, FR-007): with `out := newJSON(&bytes.Buffer{}, nil)`, a table over `name, next`: `"fanout"` (`slogx.Fanout(out)`); `"with attributes"` (`slogx.Fanout(out).WithAttrs([]slog.Attr{slog.Int("a", 1)})`); `"with a group"` (`slogx.Fanout(out).WithGroup("g")`). `slogx.New(tt.next)` returns a nil handler and an error that wraps `slogx.ErrInvalidConfig` and ends with `Fanout: requires WithDefaultLevel`.
  - `TestFanout_GroupsRedactionContext` (US2-5, FR-008): outputs `newJSON(&a, nil)` and `newJSON(&b, slog.LevelError)`; options default Info, `slogx.WithContextFunc(ridAttrs)`, `slogx.WithRedactKeys(sensitiveKeys...)`; `ctx := slogx.With(requestContext(), slog.String("token", "s3cr3t"))`. Log `logger.With("service", "api").WithGroup("db").With("table", "orders").InfoContext(ctx, "grouped", "password", "p")`, then `logger.InfoContext(ctx, "plain")`. Assert `a.String() == b.String()`, and that the lines of `a` are exactly:

    ```text
    {"level":"INFO","msg":"grouped","service":"api","tenant":"t1","token":"[REDACTED]","request_id":"abc","db":{"table":"orders","password":"[REDACTED]"}}
    {"level":"INFO","msg":"plain","tenant":"t1","token":"[REDACTED]","request_id":"abc"}
    ```
  - `TestFanout_NestedNew` (US2-6): `nested := must(slogx.New(newJSON(&so, nil), slogx.WithContextFunc(traceAttrs)))`; outer `logger := slog.New(must(slogx.New(slogx.Fanout(nested, newJSON(&ot, nil)), default Info, slogx.WithContextFunc(ridAttrs), slogx.WithRedactKeys(sensitiveKeys...))))`; `ctx := withTrace(requestContext(), "T")` and `bare := withTrace(context.Background(), "T")`. Log, in order: `logger.InfoContext(ctx, "plain")`; `logger.WithGroup("db").InfoContext(ctx, "grouped", "rows", 3)`; `logger.InfoContext(bare, "no outer attributes")`; `logger.WithGroup("g").InfoContext(bare, "grouped, no outer attributes", "a", 1)`. Both outputs have 4 lines. For each line `i`:
    - `decode(t, so[i])` has `trace_id` `T` and `span_id` `s-T`; `ot[i]` contains neither `"trace_id"` nor `"span_id"`;
    - for each key `trace_id`, `span_id`, `tenant`, `request_id`, `strings.Count(so[i], "\""+key+"\":")` is at most 1;
    - `strings.NewReplacer("\"trace_id\":\"T\",\"span_id\":\"s-T\",", "", ",\"trace_id\":\"T\",\"span_id\":\"s-T\"", "").Replace(so[i]) == ot[i]`.
  - `TestFanout_Errors` (US2-7, FR-009): three cases, each calling `h.Handle(context.Background(), recordWith(slog.String("a", "b")))` directly on `h := must(slogx.New(slogx.Fanout(…), default Info))`, with `recordingHandler` outputs:
    - `"one fails"`: outputs `first`, `second{err: errSecond}`, `third`: the error is `errSecond` itself (identity check), and each of the three outputs has 1 record;
    - `"several fail"`: outputs `{err: e1}`, `ok`, `{err: e3}`: `errors.Is(err, e1)` and `errors.Is(err, e3)`, `err` is neither `e1` nor `e3`, and `ok` has 1 record;
    - `"none fails"`: two outputs: the error is nil.
- [X] T027 [US2] In `slogx/fanout_test.go`, add the edge-case tests (spec Edge Cases; FR-006, FR-010):
  - `TestFanout_InvalidOutputs`: with `out := newJSON(&bytes.Buffer{}, nil)`, a table over `name, next, opts, want`, each `slogx.New(tt.next, tt.opts...)` returning a nil handler and an error that wraps `slogx.ErrInvalidConfig` and ends with `want`. With default Info: `"no output"` (`slogx.Fanout()`, `Fanout: no output`); `"nil output"` (`slogx.Fanout(out, nil)`, `Fanout: outputs[1]: must not be nil`); `"nil first output"` (`slogx.Fanout(nil, out)`, `Fanout: outputs[0]: must not be nil`); `"nested without output"` (`slogx.Fanout(out, slogx.Fanout())`, `Fanout: outputs[1]: no output`); `"nested nil output"` (`slogx.Fanout(slogx.Fanout(nil))`, `Fanout: outputs[0]: outputs[0]: must not be nil`); `"invalid, with a group"` (`slogx.Fanout().WithGroup("g")`, `Fanout: no output`). Without options: `"invalid before missing level"` (`slogx.Fanout()`, `Fanout: no output`).
  - `TestFanout_OneOutput`: `slog.New(must(slogx.New(slogx.Fanout(newJSON(&got, slog.LevelError)), default Info))).Info("m", "a", 1)` writes the same bytes as `slog.New(newJSON(&want, nil)).Info("m", "a", 1)`.
  - `TestFanout_SameOutputTwice`: with one recorder `rec`, `slogx.Fanout(rec, rec)` under default Info: one `Info` record gives `rec.Len() == 2`.
  - `TestFanout_Nested`: `slogx.Fanout(rec, slogx.Fanout(rec, rec))` under default Info: one record gives `rec.Len() == 3`.
  - `TestFanout_OnItsOwn` (FR-010): `f := slogx.Fanout(rec, newJSON(&buf, slog.LevelError))`. `f.Enabled(ctx, l)` is true for `l` in `-100`, Debug, Error and `100`. `slog.New(f).Debug("m")` gives `rec.Len() == 1` and the line `{"level":"DEBUG","msg":"m"}`. Then an invalid fanout used on its own does not panic: `g := slogx.Fanout(nil, rec2).WithGroup("g").WithAttrs([]slog.Attr{slog.Int("a", 1)})`; `g.Handle(ctx, recordWith())` returns nil and `rec2.Len() == 1`.
  - `TestFanout_Hidden`: `inner := must(slogx.New(slogx.Fanout(slogxtest.NewRecorder()), default Info))`; `slogx.New(inner)` returns a handler and no error: a fanout behind another handler is not recognized.
  - `TestFanout_OwnCopy` (FR-006): `r := slog.NewRecord(time.Time{}, slog.LevelInfo, "m", 0)`, then for `i` in 0–7, `r.AddAttrs(slog.Int(fmt.Sprintf("a%d", i), i))`, one call per attribute, which leaves spare capacity in the record's overflow slice. With `a, b := &appendingHandler{value: "a"}, &appendingHandler{value: "b"}`, `slogx.Fanout(a, b).Handle(context.Background(), r)` returns nil. Then `a.records[0]` and `b.records[0]` each have exactly 9 attributes, none with the key `!BUG`, and their last attribute is `added=a` and `added=b`. A comment explains that without a copy per output, the second output writes into the slice of the first, which slog reports with a `!BUG` attribute.
  - `TestFanout_Derived`: with `f := slogx.Fanout(slogxtest.NewRecorder())`, `f.WithGroup("") == f` and `f.WithAttrs(nil) == f`.
- [X] T028 [US2] In `slogx/fanout_test.go`, add:
  - `TestFanout_LevelMatrix` (SC-001): for each default level in Debug, Info, Warn, Error, and each request in `"none"`, `"below"` (`default-4`) and `"above"` (`default+4`), a subtest named `fmt.Sprintf("%v/%s", def, req)`. Outputs: `newJSON(&buf, nil)` and a recorder, under `slogx.New(slogx.Fanout(…), slogx.WithDefaultLevel(def))`. The context carries `slogx.WithLevel(ctx, threshold)` for `below` and `above`, and no level for `none`, where the threshold is the default. Log one record at each level of `-8, -4, 0, 4, 8, 12` with `logger.Log(ctx, l, "m")`. The expected list is the levels `>= threshold`, in order. Assert that the `"level"` values of the JSON lines equal the `String()` of the expected levels, and that the recorder's `Level` values equal the expected levels.
  - `TestFanout_Conformance` (FR-021): subtests `"alone"`, `testConformance(t, func(next slog.Handler) slog.Handler { return slogx.Fanout(next) })`, and `"under New"`, with `must(slogx.New(slogx.Fanout(next), slogx.WithDefaultLevel(slog.LevelDebug), slogx.WithContextFunc(alwaysAttr), slogx.WithRedactKeys("password")))`.
  - `TestMultiHandler_AppliesOutputLevels` (FR-011; spec Context, problem 2), with the doc comment `// TestMultiHandler_AppliesOutputLevels pins the behavior that the documentation recommends slog.NewMultiHandler for, and the reason Fanout exists: each output applies its own level, even to a record that a request enabled.`: `slog.New(must(slogx.New(slog.NewMultiHandler(newJSON(&buf, nil), rec), default Info))).DebugContext(slogx.WithLevel(ctx, slog.LevelDebug), "m")` leaves `buf` empty and gives `rec.Len() == 1`.
  - `TestAlloc_FanoutDisabled` (FR-021): `h := must(slogx.New(slogx.Fanout(newJSON(&bytes.Buffer{}, nil), slogxtest.NewRecorder()), default Info, slogx.WithContextFunc(ridAttrs), slogx.WithRedactKeys("token")))`, then `assertNoAllocsWhenDisabled(t, h, requestContext(), slogx.WithLevel(requestContext(), slog.LevelWarn))`.
- [X] T029 [P] [US2] In `slogx/example_test.go`, add `ExampleFanout`, after `ExampleWithDefaultLevel` (FR-025), with the comment `// Both outputs receive the same records, whatever their own levels: the level is decided once, by the default level. The text output, built for errors only, receives the Info record too.`:

  ```go
  text := slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError, ReplaceAttr: dropTime})
  h, err := slogx.New(slogx.Fanout(stdoutJSON(), text), slogx.WithDefaultLevel(slog.LevelInfo))
  if err != nil {
  	panic(err)
  }
  logger := slog.New(h)

  logger.Debug("cache miss")
  logger.Info("order created", "order", 1001)
  // Output:
  // {"level":"INFO","msg":"order created","order":1001}
  // level=INFO msg="order created" order=1001
  ```
- [X] T030 [US2] Create `slogx/fanout.go` with a stub whose behavior is the standard fan-out: `func Fanout(outputs ...slog.Handler) slog.Handler { return slog.NewMultiHandler(outputs...) }`, with the doc comment of [contracts/public-api.md § Added](contracts/public-api.md#added-fr-001-to-fr-015) for `Fanout`. Run `GOWORK=off go test -race -run 'TestFanout_(SameRecords|IgnoresOutputLevels|RequiresDefaultLevel|OneOutput|LevelMatrix)|TestMultiHandler_|ExampleFanout' -v .`, and only these: other fanout tests use nil outputs, which make the stub panic. Observe that `SameRecords` (stdout misses `request debug`), `IgnoresOutputLevels`, `RequiresDefaultLevel` (no error), `OneOutput`, the `LevelMatrix` subtests where the JSON output must receive records below Info, and `ExampleFanout` fail, and that `TestMultiHandler_AppliesOutputLevels` passes. Do not continue until these failures are seen.

### Implementation for User Story 2

- [X] T031 [US2] Replace the stub in `slogx/fanout.go` (FR-006, FR-009, FR-010; research R-05; data-model §4). Keep the contract's doc comment on `Fanout`:

  ```go
  package slogx

  import (
  	"context"
  	"errors"
  	"fmt"
  	"log/slog"
  	"slices"
  )

  func Fanout(outputs ...slog.Handler) slog.Handler {
  	f := &fanout{outputs: slices.Clone(outputs)}
  	if len(outputs) == 0 {
  		f.invalid = "no output"
  	}
  	for i, o := range outputs {
  		if o == nil {
  			f.invalid = fmt.Sprintf("outputs[%d]: must not be nil", i)
  			break
  		}
  		if inner, ok := o.(*fanout); ok && inner.invalid != "" {
  			f.invalid = fmt.Sprintf("outputs[%d]: %s", i, inner.invalid)
  			break
  		}
  	}
  	return f
  }

  // fanout is the handler that Fanout returns.
  type fanout struct {
  	outputs []slog.Handler
  	invalid string // why New refuses the fanout, or "" when it is valid
  }

  // Enabled reports every level as enabled: the Handler built over the fanout
  // decides the level.
  func (*fanout) Enabled(context.Context, slog.Level) bool { return true }

  // Handle passes a copy of r to every output, in order, without asking them
  // whether they handle its level. It returns the error of a single failing
  // output unchanged, and joins the errors of several.
  func (f *fanout) Handle(ctx context.Context, r slog.Record) error {
  	var errs []error
  	for _, o := range f.outputs {
  		if o == nil { // only in an invalid fanout used on its own
  			continue
  		}
  		if err := handle(ctx, o, r.Clone()); err != nil {
  			errs = append(errs, err)
  		}
  	}
  	if len(errs) == 1 {
  		return errs[0]
  	}
  	return errors.Join(errs...)
  }

  // WithAttrs returns a fanout whose outputs have the attributes.
  func (f *fanout) WithAttrs(attrs []slog.Attr) slog.Handler {
  	if len(attrs) == 0 {
  		return f
  	}
  	return f.each(func(h slog.Handler) slog.Handler { return h.WithAttrs(attrs) })
  }

  // WithGroup returns a fanout whose outputs have the group open.
  func (f *fanout) WithGroup(name string) slog.Handler {
  	if name == "" {
  		return f
  	}
  	return f.each(func(h slog.Handler) slog.Handler { return h.WithGroup(name) })
  }

  // each returns a fanout whose outputs are those of f passed through fn, and
  // which keeps the reason why f is invalid.
  func (f *fanout) each(fn func(slog.Handler) slog.Handler) *fanout {
  	out := make([]slog.Handler, len(f.outputs))
  	for i, o := range f.outputs {
  		if o != nil {
  			out[i] = fn(o)
  		}
  	}
  	return &fanout{outputs: out, invalid: f.invalid}
  }
  ```
- [X] T032 [US2] Make `New` recognize a fanout, in `slogx/handler.go` and `slogx/options.go` (FR-007; research R-03, R-05; data-model §2):
  1. In `slogx/handler.go`, in `New`, after `newConfig`, insert:

     ```go
     if f, ok := next.(*fanout); ok {
     	switch {
     	case f.invalid != "":
     		return nil, invalid("Fanout", f.invalid)
     	case cfg.level == nil:
     		return nil, invalid("Fanout", "requires WithDefaultLevel")
     	}
     }
     ```
  2. Replace the last paragraph of the doc comment of `New` with: `It returns an error wrapping ErrInvalidConfig when next is nil, when an option is invalid, or when next is a Fanout that is invalid or comes without WithDefaultLevel. A Fanout is recognized only as next itself.`
  3. In `slogx/options.go`, append to the doc comment of `WithDefaultLevel` the paragraph `It is required when the next handler is a Fanout.`, as in the contract.
- [X] T033 [US2] Run `GOWORK=off go test -race -shuffle=on ./...`: every test of T025–T029 passes, and every earlier test still passes. From the repository root, `make check MODULE=slogx` reports `PASS`.

**Checkpoint**: stdout and OpenTelemetry receive the same records. User Stories 1 and 2 together
fix both traps of the spec's Context.

---

## Phase 5: User Story 3 - A context value in one line (Priority: P2)

**Goal**: `WithContextValue` adds one attribute read from the context, with the rules of
extraction functions (FR-012 to FR-015).

**Independent Test**: configure a context value whose function returns a known value, then inspect
the records logged with and without that value in the context.

### Tests for User Story 3 ⚠️ (write first, see them fail)

- [X] T034 [US3] In `slogx/helpers_test.go`, after `ridAttrs`, add `ridFrom(ctx context.Context) (string, bool)`, with the doc comment `// ridFrom reads the identifier stored by withRID. It has the signature of requestid.FromContext.` It returns `ctx.Value(ridKey{}).(string)` with the `ok` of the type assertion.
- [X] T035 [US3] Create `slogx/contextvalue_test.go`, package `slogx_test`. Unless stated, the logger is `slog.New(must(slogx.New(newJSON(&buf, nil), opts...)))` and each case uses a fresh buffer. A "counting get" is `get := func(ctx context.Context) (string, bool) { calls++; return ridFrom(ctx) }`.
  - `TestContextValue_PresentAndAbsent` (US3-1, US3-2): with `slogx.WithContextValue("request_id", ridFrom)`, `InfoContext(withRID(ctx, "abc"), "with id")` and `InfoContext(ctx, "without id")` write exactly `{"level":"INFO","msg":"with id","request_id":"abc"}` and `{"level":"INFO","msg":"without id"}`.
  - `TestContextValue_TakenKey` (US3-3, FR-013): a table over `name, opts, log, want string, wantCalls int`, where the last option is `slogx.WithContextValue("request_id", get)` with a counting `get`, and `ctx := withRID(context.Background(), "abc")`:
    - `"record attribute"`: `l.InfoContext(ctx, "m", "request_id", "own")`, `{"level":"INFO","msg":"m","request_id":"own"}`, 0 calls;
    - `"attached attribute"`: `l.InfoContext(slogx.With(ctx, slog.String("request_id", "att")), "m")`, `{"level":"INFO","msg":"m","request_id":"att"}`, 0 calls;
    - `"logger attribute"`: `l.With("request_id", "logger").InfoContext(ctx, "m")`, `{"level":"INFO","msg":"m","request_id":"logger"}`, 0 calls;
    - `"earlier extraction function"`: a first option `slogx.WithContextFunc(func(context.Context) []slog.Attr { return []slog.Attr{slog.String("request_id", "func")} })`, `{"level":"INFO","msg":"m","request_id":"func"}`, 0 calls;
    - `"earlier context value"`: a first option `slogx.WithContextValue("request_id", func(context.Context) (string, bool) { return "first", true })`, `{"level":"INFO","msg":"m","request_id":"first"}`, 0 calls;
    - `"record attribute inside a group"`: `l.WithGroup("g").InfoContext(ctx, "m", "request_id", "own")`, `{"level":"INFO","msg":"m","request_id":"abc","g":{"request_id":"own"}}`, 1 call: a key inside a group does not take the top-level key.
  - `TestContextValue_FirstConfiguredWins` (edge "a context value and an extraction function that return the same key"): with `ctx := withRID(context.Background(), "abc")` and `fn` returning `request_id=func`: options `WithContextValue("request_id", ridFrom), WithContextFunc(fn)` write `"request_id":"abc"`; options `WithContextFunc(fn), WithContextValue("request_id", ridFrom)` write `"request_id":"func"`. Each line has `request_id` once.
  - `TestContextValue_Redaction` (US3-4): options `slogx.WithContextValue("pin", func(context.Context) (slogx.Secret[int], bool) { return slogx.NewSecret(1234), true })`, `slogx.WithContextValue("token", func(context.Context) (string, bool) { return "s3cr3t", true })`. With `slogx.WithRedactKeys("token")` added, the line is exactly `{"level":"INFO","msg":"m","pin":"[REDACTED]","token":"[REDACTED]"}`; without it, `{"level":"INFO","msg":"m","pin":"[REDACTED]","token":"s3cr3t"}`.
  - `TestContextValue_LogValuer` (FR-015): `slogx.WithContextValue("user", func(context.Context) (userValue, bool) { return userValue{name: "bob", token: "t0k"}, true })`. With `slogx.WithRedactKeys(sensitiveKeys...)`, the line is `{"level":"INFO","msg":"m","user":{"name":"bob","token":"[REDACTED]"}}`; without, `{"level":"INFO","msg":"m","user":{"name":"bob","token":"t0k"}}`.
  - `TestContextValue_Panic` (US3-5): options `slogx.WithContextValue("boom", func(context.Context) (int, bool) { panic("bad") })`, `slogx.WithContextValue("request_id", ridFrom)`; `InfoContext(withRID(ctx, "abc"), "m")` writes exactly `{"level":"INFO","msg":"m","!CONTEXTFUNC":"panic: bad","request_id":"abc"}`.
  - `TestContextValue_Invalid` (US3-6, FR-014): a table over `name, opt, want`: `"empty key"` (`slogx.WithContextValue("", ridFrom)`, `WithContextValue: empty key`); `"blank key"` (`slogx.WithContextValue(" \t", ridFrom)`, `WithContextValue: empty key`); `"nil function"` (`slogx.WithContextValue[string]("request_id", nil)`, `WithContextValue: function must not be nil`). `slogx.New(newJSON(&bytes.Buffer{}, nil), tt.opt)` returns a nil handler and an error that wraps `slogx.ErrInvalidConfig` and ends with `want`.
  - `TestContextValue_Disabled` (US3-7, FR-021): with a counting `get`, `h := must(slogx.New(newJSON(&buf, nil), slogx.WithContextValue("request_id", get)))`; `slog.New(h).DebugContext(withRID(ctx, "abc"), "m")` writes nothing and makes 0 calls; then `assertNoAllocsWhenDisabled(t, h, requestContext(), withRID(context.Background(), "abc"))`, and `calls` is still 0.
  - `TestContextValue_ZeroValue` (edge): options `WithContextValue("count", func(context.Context) (int, bool) { return 0, true })` and `WithContextValue("name", func(context.Context) (string, bool) { return "", true })` write exactly `{"level":"INFO","msg":"m","count":0,"name":""}`.
  - `TestContextValue_Group`: with `WithContextValue("request_id", ridFrom)`, `logger.WithGroup("db").InfoContext(withRID(ctx, "abc"), "q", "rows", 3)` writes exactly `{"level":"INFO","msg":"q","request_id":"abc","db":{"rows":3}}`.
  - `TestContextValue_RequestMiddleware` (SC-003): `rec := slogxtest.NewRecorder()`; `logger := slog.New(must(slogx.New(rec, slogx.WithDefaultLevel(slog.LevelInfo), slogx.WithContextValue("request_id", ridFrom))))`, one option for the identifier and no extraction function. A middleware `storeID(next http.Handler) http.Handler` calls `next.ServeHTTP(w, r.WithContext(withRID(r.Context(), r.Header.Get("X-Request-Id"))))`, as the `requestid` middleware stores its identifier. The application handler logs `logger.InfoContext(ctx, "order received")`, `logger.WarnContext(ctx, "stock low", "sku", "a1")` and `logger.WithGroup("db").ErrorContext(ctx, "insert failed")` with `ctx := r.Context()`, then answers 204. For `i` in 0–4, send `httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/orders", nil)` with the header `X-Request-Id: req-<i>` through `storeID(app).ServeHTTP(httptest.NewRecorder(), req)`. `rec.Records()` has 15 records, and record `j` has `Attr("request_id")` equal to `req-<j/3>`.
  - `TestContextValue_Conformance` (FR-021): `testConformance(t, func(next slog.Handler) slog.Handler { return must(slogx.New(next, slogx.WithContextValue("ctxvalue", func(context.Context) (string, bool) { return "x", true }))) })`.
- [X] T036 [P] [US3] In `slogx/example_test.go`, after `requestIDAttrs`, add `requestIDFrom(ctx context.Context) (string, bool)`, with the doc comment `// requestIDFrom reads the identifier that a middleware stored in the context, as requestid.FromContext does.` Then add `ExampleWithContextValue`, after `ExampleWithContextFunc` (FR-025), with the comment `// A context value logs one value that another package stored in the context, such as the identifier of a request ID middleware, with one option.`:

  ```go
  h, err := slogx.New(stdoutJSON(), slogx.WithContextValue("request_id", requestIDFrom))
  if err != nil {
  	panic(err)
  }
  logger := slog.New(h)

  ctx := context.WithValue(context.Background(), requestIDKey{}, "abc")
  logger.InfoContext(ctx, "order created", "order", 1001)
  logger.InfoContext(context.Background(), "no request")
  // Output:
  // {"level":"INFO","msg":"order created","request_id":"abc","order":1001}
  // {"level":"INFO","msg":"no request"}
  ```
- [X] T037 [US3] In `slogx/options.go`, add a stub after `WithContextFunc`: `func WithContextValue[T any](key string, get func(ctx context.Context) (T, bool)) Option { return func(*config) {} }`, with the doc comment of [contracts/public-api.md § Added](contracts/public-api.md#added-fr-001-to-fr-015) for `WithContextValue`. Run `GOWORK=off go test -race -run 'TestContextValue_|ExampleWithContextValue' -v .` and observe that every test fails except `TestContextValue_Disabled`, `TestContextValue_Conformance` and the subtests of `TestContextValue_TakenKey` whose key is taken by the record, an attached attribute, a logger attribute or an extraction function. The subtest `"earlier context value"` fails too, because the stub adds nothing. Do not continue until these failures are seen.
- [X] T038 [US3] In `slogx/options_internal_test.go` (FR-014; data-model §1, §3):
  - add `getString(context.Context) (string, bool)`, returning `"v", true`;
  - add to `TestOptions_Validation` the rows `"context value empty key"` (`WithContextValue("", getString)`, `WithContextValue: empty key`), `"context value blank key"` (`WithContextValue(" \t", getString)`, `WithContextValue: empty key`) and `"context value nil function"` (`WithContextValue[string]("k", nil)`, `WithContextValue: function must not be nil`);
  - change `TestOptions_Accumulation` to `WithContextFunc(fnAttrs("first"))`, `WithRedactKeys("a", "b")`, `WithContextValue("second", getString)`, `WithContextFunc(fnAttrs("third"))`, `WithRedactKeys("c")`, and assert that `cfg.sources` has 3 entries in that order: `sources[0].fn(ctx)[0].Key == "first"`, `sources[1].key == "second"` with `sources[1].fn == nil`, and `sources[2].fn(ctx)[0].Key == "third"`.

  Run `GOWORK=off go vet ./...` and observe the failure: `config` has no field `sources` yet.

### Implementation for User Story 3

- [X] T039 [US3] Implement context values in `slogx/options.go`, `slogx/contexthandler.go` and `slogx/handler.go` (FR-012 to FR-015; research R-07; data-model §3):
  1. In `slogx/options.go`, replace the field `fns` with `sources []source // extraction functions and context values, in configuration order`. `WithContextFunc` appends `source{fn: fn}`. Replace its doc comment with:

     ```go
     // WithContextFunc adds a function that computes attributes from the context of
     // every handled record, such as the trace and span identifiers of a tracing
     // library. It is called only for enabled records. Use WithContextValue for a
     // single value. It can be given several times: when extraction functions and
     // context values produce the same key, the one configured first wins, and
     // attributes attached with With win over all of them.
     ```
  2. Replace the stub of `WithContextValue`, keeping its doc comment:

     ```go
     func WithContextValue[T any](key string, get func(ctx context.Context) (T, bool)) Option {
     	return func(c *config) {
     		switch {
     		case strings.TrimSpace(key) == "":
     			c.fail("WithContextValue", "empty key")
     			return
     		case get == nil:
     			c.fail("WithContextValue", "function must not be nil")
     			return
     		}
     		c.sources = append(c.sources, source{key: key, get: func(ctx context.Context) (slog.Value, bool) {
     			v, ok := get(ctx)
     			if !ok {
     				return slog.Value{}, false
     			}
     			return slog.AnyValue(v), true
     		}})
     	}
     }
     ```
  3. In `slogx/contexthandler.go`: replace the field `fns` with `sources []source`; in `Handle`, `len(h.fns) == 0` becomes `len(h.sources) == 0`; the type's doc comment says `then those of the extraction functions and context values given with WithContextFunc and WithContextValue, in configuration order`. Replace the loop over `h.fns` at the end of `addContext` with:

     ```go
     	free := func(key string) bool {
     		return !skip(key) && !hasKey(attached, key) && !recordHasKey(out, key)
     	}
     	add := func(a slog.Attr) {
     		if free(a.Key) {
     			out.AddAttrs(a)
     			n++
     		}
     	}
     	for _, s := range h.sources {
     		switch {
     		case s.fn != nil:
     			for _, a := range flattenAttrs(extract(ctx, s.fn)) {
     				add(a)
     			}
     		case free(s.key): // a context value whose key is taken is not computed
     			if a, ok := s.value(ctx); ok {
     				add(a)
     			}
     		}
     	}
     	return n
     ```

     Before `extract`, add:

     ```go
     // source computes context attributes: an extraction function (fn), or a
     // context value (key and get).
     type source struct {
     	fn  func(context.Context) []slog.Attr
     	key string
     	get func(context.Context) (slog.Value, bool)
     }

     // value calls get. When get panics, the result is the attribute that reports
     // the panic, as for an extraction function.
     func (s source) value(ctx context.Context) (a slog.Attr, ok bool) {
     	defer func() {
     		if v := recover(); v != nil {
     			a, ok = panicAttr(v), true
     		}
     	}()
     	v, ok := s.get(ctx)
     	return slog.Attr{Key: s.key, Value: v}, ok
     }

     // panicAttr reports a panic of an extraction function or a context value.
     func panicAttr(v any) slog.Attr {
     	return slog.String("!CONTEXTFUNC", fmt.Sprintf("panic: %v", v))
     }
     ```

     In `extract`, the recovered result becomes `attrs = []slog.Attr{panicAttr(v)}`.
  4. In `slogx/handler.go`, `New` builds `&contextHandler{root: h, next: h, sources: cfg.sources}`.
- [X] T040 [US3] Run `GOWORK=off go test -race -shuffle=on ./...`: every test of T035, T036 and T038 passes, and every earlier test still passes, `TestNew_SameOutputAsV010` included. From the repository root, `make check MODULE=slogx` reports `PASS`.

**Checkpoint**: a developer logs the request identifier with one option.

---

## Phase 6: User Story 4 - A smaller API, and the upgrade from 0.1.0 (Priority: P2)

**Goal**: any combination of options is accepted, and the CHANGELOG states what is removed,
added and changed, with the migration (FR-017, FR-020, FR-026; SC-004, SC-005). The code of this
story was removed in the Foundational phase.

**Independent Test**: compare, record by record, the output of 0.1.0 and of this release for the
same configurations without sampling, and check that every removed identifier appears in the
changelog with its replacement.

- [X] T041 [US4] In `slogx/handler_test.go` (US4-3, FR-017):
  - add `TestNew_AnyCombination`: with `get := func(context.Context) (string, bool) { return "v", true }`, a table over `name, next, opts` that `slogx.New` must accept with a non-nil handler and no error: `"every option"` (`WithDefaultLevel(slog.LevelInfo)`, `WithContextFunc(ridAttrs)`, `WithContextValue("k", get)`, `WithRedactKeys("token")`); `"reverse order"` (the same, reversed); `"every option twice"` (each of them twice, with other keys and levels); `"over a fanout"` (the first list, over `slogx.Fanout(newJSON(&bytes.Buffer{}, nil))`); `"no option"`;
  - extend the table of `TestNew_InvalidOptions` with `{"nil default level", next, []slogx.Option{slogx.WithDefaultLevel(nil)}}`, `{"empty context value key", next, []slogx.Option{slogx.WithContextValue("", ridFrom)}}` and `{"fanout without default level", slogx.Fanout(next), nil}`, and assert in every row that the message ends neither with `: does not apply` nor with `: required`.

  These guard behavior that already holds: they pass at once.
- [X] T042 [P] [US4] In `slogx/CHANGELOG.md`, under `## [Unreleased]`, add the entry of [contracts/public-api.md § CHANGELOG entry](contracts/public-api.md#changelog-entry), verbatim: the sections `### Removed`, `### Added`, `### Changed` and `### Migration from 0.1.0` (FR-026). Leave `## [0.1.0]` unchanged.
- [X] T043 [US4] Verify the upgrade path: `GOWORK=off go test -race -run 'TestNew_' -v .` passes, `TestNew_SameOutputAsV010` included; every identifier of the expected report in [contracts/public-api.md](contracts/public-api.md#expected-gorelease-report-against-slogxv010) (11 removed, 3 added) appears in the `[Unreleased]` section of `slogx/CHANGELOG.md`, and the CHANGELOG names no other identifier as removed or added (SC-004; US4 scenario 2). T052 confirms the list with `gorelease`.

**Checkpoint**: the release removes identifiers with a stated replacement, and changes no output
where it was not meant to.

---

## Phase 7: User Story 5 - Recipes for the common setups (Priority: P3)

**Goal**: the package documentation and the README describe the handler, the default level, the
fanout and context values, with three recipes, and never mention sampling or standalone handlers
(FR-011, FR-023, FR-024).

**Independent Test**: read the README, and build each recipe in a separate project against the
libraries it names.

- [X] T044 [P] [US5] Rewrite `slogx/doc.go` (FR-011, FR-023; research R-12; [contracts/record-semantics.md § Documentation statements](contracts/record-semantics.md#documentation-statements-fr-011-fr-023)). Keep plain identifier names, as the current file does, and these sections in this order:
  1. **Overview**: the package provides a `slog.Handler` that adds, to the standard structured logger and without a logger type of its own, attributes taken from the context of each record, redaction of sensitive data, and levels decided once for every output: a default level, and a minimum level attached to the context of one request. Then this code block, indented as in the current file:

     ```go
     h, err := slogx.New(slog.NewJSONHandler(os.Stdout, nil),
     	slogx.WithDefaultLevel(slog.LevelInfo),
     	slogx.WithContextValue("request_id", requestid.FromContext),
     	slogx.WithRedactKeys("authorization", "password", "token", "cookie"),
     )
     if err != nil {
     	return err
     }
     logger := slog.New(h)
     ```
  2. **`# Context attributes`**: the current section, with three changes: the first paragraph says that `WithContextValue` and `WithContextFunc` read values that other packages stored in the context, and that the Handler adds them; a new sentence says that `WithContextValue` adds one attribute from an accessor such as `requestid.FromContext`, and that `WithContextFunc` is for a function that returns several attributes, such as the trace and span identifiers; the precedence sentence ends with "and among context values and extraction functions, the first configured wins". Keep the code example and the paragraph on the methods that take a context and `sloglint`.
  3. **`# Redaction and secrets`**: the current section, with its first sentence starting "With WithRedactKeys, the Handler writes [REDACTED] as the value of every attribute whose key is one of the given keys".
  4. **`# Levels`**, replacing `# Per-request level`: `WithDefaultLevel` sets the level of the records logged with a context that carries no level; the Handler compares them with it alone and does not ask the next handler, so the level holds whatever the outputs enable, the OpenTelemetry bridge for example; a `*slog.LevelVar` changes it at run time; without it, the next handler decides. `WithLevel` attaches a minimum level to a context; those records are compared with it alone, ignoring the default level and the output's own level, in both directions; it can enable debug records for one request in production, or silence one. A record enabled by either level reaches the next handler whatever its own level.
  5. **`# Several outputs`**, new: `Fanout` passes every record to several outputs without asking them whether they handle its level, so they all receive the same records, levels, context attributes and redaction; give it to `New` as the next handler, directly, with `WithDefaultLevel`, which `New` then requires, with the code line `h, err := slogx.New(slogx.Fanout(stdout, otel), slogx.WithDefaultLevel(level))`; a Fanout wrapped in another handler is not recognized; the levels of the outputs are ignored, and a `New` used as one output, to add attributes to that output only, is not asked about levels either; for deliberately different thresholds per output, use `slog.NewMultiHandler`: each output applies its own level, and the per-request level does not go below it.
  6. **`# Composition`**, replacing the current one: the order `level → context attributes → redaction → next handler`, with two reasons: the level comes first because `slog.Logger` asks only the outermost handler; the redaction comes after the context attributes, so that a token stored in the context is redacted too. Then: `New` reports invalid configuration, such as a nil default level or a Fanout without `WithDefaultLevel`, as errors wrapping `ErrInvalidConfig`.
  7. **`# Robustness`**: the current section, with these changes: "No handler panics or lets a panic escape" stays; "An extraction function or a context value that panics is replaced by the attribute !CONTEXTFUNC="panic: <value>""; "The Handler returns the next handler's error unchanged, and a Fanout returns the error of a single failing output unchanged and combines several with errors.Join"; "When a record is disabled, the Handler allocates no memory, and calls no extraction function and no context value".
  8. The closing paragraph on `slogxtest`, unchanged.

  Remove `# Sampling` and every mention of sampling, `Flush` and the standalone handlers.
- [X] T045 [P] [US5] Rewrite `slogx/README.md` (FR-023, FR-024; research R-12, R-13), wrapped at 100 columns, with these sections in order:
  1. `# slogx`: one paragraph: a `slog.Handler` for the standard structured logger, with context attributes, redaction of sensitive data, and a default level and a per-request level decided once for every output; no logger type, keep using `*slog.Logger`; no dependency outside the standard library. Then the `go get github.com/paccolamano/go/slogx` block and `Minimum Go version: 1.26`.
  2. `## Usage`: the code block of T044 step 1, the order block `level → context attributes → redaction → output`, and two bullets: **Level first**, because `slog.Logger` asks only the outermost handler, so the level is decided once for every output; **Redaction after context**, so that a token stored in the context is redacted too. Then: invalid configuration is reported as errors wrapping `ErrInvalidConfig`.
  3. `## Context attributes`: the current section, with the extraction function replaced by `slogx.WithContextValue("request_id", requestid.FromContext)`, a sentence that `WithContextFunc` remains for a function that returns several attributes, and the rest kept: `With`, the example output, "the record wins", the `accesslog` and `recovery` note, and `sloglint`.
  4. `## Redaction and secrets`: unchanged.
  5. `## Levels`: the default level (with a `*slog.LevelVar` example: `level := new(slog.LevelVar)` and `slogx.WithDefaultLevel(level)`, then `level.Set(slog.LevelDebug)` at run time), and the per-request level (the current "Per-request level" example and sentence, extended: the context level wins over the default level too).
  6. `## Several outputs`: `Fanout` versus `slog.NewMultiHandler`, with the statements of [contracts/record-semantics.md § Documentation statements](contracts/record-semantics.md#documentation-statements-fr-011-fr-023).
  7. `## Recipes`: the three recipes of [contracts/readme-recipes.md](contracts/readme-recipes.md), as `### Colored output while developing`, `### Stdout plus OpenTelemetry` and `### Different thresholds per output`. Each one names its libraries, gives the code verbatim, and states every item listed under "The text states". Say once that the recipes use other modules, which slogx itself does not depend on.
  8. `## Testing with slogxtest`: unchanged.

  Remove `## Sampling`, the paragraph on the standalone handlers and the `Flush` comment.
- [X] T046 [US5] Check the recipes in a separate project (SC-009; US5 scenarios 1–3), following [quickstart.md §7](quickstart.md#7-readme-recipes-in-a-separate-project-sc-009): outside the repository, in the session scratchpad, create a module that requires `github.com/paccolamano/go/slogx` with a local `replace` to the working tree's `slogx/`, get the current releases of the libraries the recipes name, and write one `main` package per recipe with the code copied from `slogx/README.md`. Each recipe must behave as listed under "Expected behavior" in [contracts/readme-recipes.md](contracts/readme-recipes.md): colors only in a terminal and without `NO_COLOR`; trace and span IDs as attributes on stdout only, `request_id` and a redacted `token` on both outputs; per-output thresholds with the standard fan-out. Record the library versions for the pull request (T055). If a recipe needs a change, fix `slogx/README.md` and `contracts/readme-recipes.md` together. Nothing of this project goes into the repository.
- [X] T047 [US5] Review the documentation (US5 scenario 4; [quickstart.md §8](quickstart.md#8-documentation-review-fr-011-fr-023-fr-026)). From the repository root:
  - `grep -rniE 'sampl|flush' --include='*.go' slogx slogx/README.md` prints nothing;
  - `grep -rnE '\b(New)?(Context|Level|Redact|Sampling)Handler\b|\bWithSampl' --include='*.go' slogx slogx/README.md` prints nothing;
  - `GOWORK=off go doc -short ./slogx` lists `Fanout`, `WithContextValue` and `WithDefaultLevel` and no removed identifier, `GOWORK=off go doc ./slogx` shows the new package documentation, and `GOWORK=off go doc ./slogx Fanout` states that the outputs' levels are ignored and when to use `slog.NewMultiHandler`.

  Only `slogx/CHANGELOG.md` names the removed identifiers.

**Checkpoint**: every story is complete.

---

## Phase 8: Polish & Cross-Cutting Concerns

**Purpose**: measure the costs, check the whole change against the contracts and the quickstart,
and run the gates.

- [X] T048 [P] In `slogx/bench_test.go` (SC-006, SC-007; research R-11), extend `benchHandlers` with:
  - `"value"`: `slogx.New(next, slogx.WithContextValue("request_id", ridFrom), slogx.WithRedactKeys(sensitiveKeys...), slogx.WithDefaultLevel(slog.LevelInfo))`;
  - `"fanout"`: `slogx.New(slogx.Fanout(next, next), slogx.WithContextValue("request_id", ridFrom), slogx.WithContextValue("trace_id", ridFrom), slogx.WithRedactKeys(sensitiveKeys...), slogx.WithDefaultLevel(slog.LevelInfo))`;
  - `"multi"`: `slogx.New(slog.NewMultiHandler(next, next), slogx.WithContextValue("request_id", ridFrom), slogx.WithRedactKeys(sensitiveKeys...))`.

  The doc comment of `BenchmarkEnabled` says that `value` must take at most twice the time of `bare-same-attrs` (SC-007). Run `GOWORK=off go test -run '^$' -bench . -benchmem -count 5 .`: every `BenchmarkDisabled` sub-benchmark reports `0 B/op` and `0 allocs/op`, and `BenchmarkEnabled/value` averages at most twice `BenchmarkEnabled/bare-same-attrs` (the spike measured 1.48 times). Record the averages for the pull request.
- [X] T049 [P] In `slogx/alloc_test.go`, add `TestAlloc_AllOptionsDisabled` (SC-006), with the doc comment `// TestAlloc_AllOptionsDisabled checks that a disabled record allocates nothing with a default level, two context values, sensitive keys and a fanout of two outputs (SC-006).`: `h := must(slogx.New(slogx.Fanout(newJSON(&bytes.Buffer{}, nil), slogxtest.NewRecorder()), slogx.WithDefaultLevel(slog.LevelInfo), slogx.WithContextValue("request_id", ridFrom), slogx.WithContextValue("trace_id", ridFrom), slogx.WithRedactKeys(sensitiveKeys...)))`, then `assertNoAllocsWhenDisabled(t, h, requestContext(), slogx.WithLevel(requestContext(), slog.LevelWarn))` and `assertNoAllocsWhenDisabled(t, h.WithGroup("g").WithAttrs([]slog.Attr{slog.Int("a", 1)}), requestContext())`. Add the imports `bytes`, `github.com/paccolamano/go/slogx` and `github.com/paccolamano/go/slogx/slogxtest`.
- [X] T050 [P] Check the coverage of the spec (SC-008) for `slogx/`: from `slogx/`, run `GOWORK=off go test -race -shuffle=on -v ./... 2>&1 | grep -E '^(=== RUN|--- (PASS|FAIL))'` and confirm that every test of the [coverage map](#coverage-map-sc-008) runs and passes. Then `GOWORK=off go test -coverprofile=c.out ./... && go tool cover -func=c.out | tail -1`: the total is at least 80%, and every function of `fanout.go`, `handler.go` and `options.go` is above 90%. Delete `c.out`.
- [X] T051 [P] From `slogx/`, check the dependencies and the platforms (FR-022): `GOWORK=off go mod tidy -diff` prints nothing; `git diff --exit-code -- go.mod` passes (still `go 1.26.0`, no `require`); for every `os` in linux, darwin and windows, and every `arch` in amd64 and arm64, `GOWORK=off GOOS=$os GOARCH=$arch go vet ./...` passes.
- [X] T052 [P] Check the API compatibility and the changelog gate (SC-004). `gorelease` and `scripts/changelog-check.sh` need commits, and `gorelease` refuses a working tree with uncommitted changes. In the session scratchpad, not in the repository: clone the repository with `git clone -q --no-hardlinks <repository root> <scratchpad>/repo`, replace its `slogx/` with a copy of the working tree's `slogx/`, and commit there only. If `gorelease` is not installed, install it there with `GOBIN=<scratchpad>/tools go install golang.org/x/exp/cmd/gorelease@<GORELEASE_VERSION of versions.env>`. In the clone, run `GORELEASE=<path to gorelease> scripts/apicompat.sh slogx`: the report equals [contracts/public-api.md § Expected gorelease report](contracts/public-api.md#expected-gorelease-report-against-slogxv010), with `Suggested version: v0.2.0`, and the script reports `PASS` at 0.x. Then run `scripts/changelog-check.sh origin/main HEAD` in the clone: it passes. If the clone cannot be made, skip this task and note that the pull request workflow runs both checks.
- [X] T053 Run the checks of [quickstart.md](quickstart.md) on `slogx/`: §2, §3 (except `changelog-check.sh` and `gorelease`, done in T052), §4 one row at a time, §5 (check the golden file against a fresh `slogx/v0.1.0` worktree: `diff` prints nothing), §6 (done in T048) and §8 (done in T047). Every command passes.
- [X] T054 Run `make check MODULE=slogx` from the repository root: golangci-lint reports zero findings, and `go test -race -shuffle=on` passes with coverage of at least 80%; compare it with the baseline of T002. `grep -n 'nolint' slogx/*.go | grep -v _test.go` prints only the `wrapcheck` line of `slogx/forward.go`.
- [X] T055 Draft the pull request for the changes in `slogx/` and `specs/013-simplify-slogx/`, for the maintainer, and do not open it without their go-ahead:
  - **Title**: `feat(slogx)!: simplify the API, with a default level, a fanout and context values`.
  - **Description**:
    - the two problems: no threshold of its own over a next handler that enables every level, and a second, silent level check in the standard fan-out, with the reproduction of `TestMultiHandler_AppliesOutputLevels`;
    - what changes: `WithDefaultLevel`, `Fanout`, `WithContextValue`; sampling and the standalone handlers removed; every option applies;
    - the breaking changes and the migration, as in the CHANGELOG, and the `gorelease` report of T052;
    - unchanged output: the 195 golden sections of `slogx/v0.1.0`;
    - the coverage of T054, the benchmark averages of T048, and the recipe versions of T046;
    - the release plan: `slogx/v0.2.0`, in a separate `chore(slogx): prepare release 0.2.0` pull request;
    - the attribution line required for pull request descriptions.

---

## Coverage map (SC-008)

| Scenario or criterion | Test |
|-----------------------|------|
| US1-1 | `TestDefaultLevel_OverAllLevelsOutput` |
| US1-2, edge "per-request level above the default" | `TestDefaultLevel_ContextLevelWins` |
| US1-3 | `TestDefaultLevel_OutputThresholdIgnored` |
| US1-4 | `TestDefaultLevel_LevelVar` |
| US1-5 | `TestDefaultLevel_None` |
| US1-6 | `TestDefaultLevel_Invalid` |
| US2-1, US2-2 | `TestFanout_SameRecords` |
| US2-3 | `TestFanout_IgnoresOutputLevels` |
| US2-4 | `TestFanout_RequiresDefaultLevel` |
| US2-5 | `TestFanout_GroupsRedactionContext` |
| US2-6 | `TestFanout_NestedNew` |
| US2-7 | `TestFanout_Errors` |
| US3-1, US3-2 | `TestContextValue_PresentAndAbsent` |
| US3-3 | `TestContextValue_TakenKey` |
| US3-4 | `TestContextValue_Redaction` |
| US3-5 | `TestContextValue_Panic` |
| US3-6 | `TestContextValue_Invalid` |
| US3-7 | `TestContextValue_Disabled` |
| US4-1, SC-005 | `TestNew_SameOutputAsV010` |
| US4-2, SC-004 | `scripts/apicompat.sh` (pull request gate 3) and T043 |
| US4-3 | `TestNew_AnyCombination`, `TestNew_InvalidOptions` |
| US4-4 | the migrated `TestContext_*`, `TestRedact_*`, `TestLevel_*` tests, unchanged outputs (T013) |
| US5-1 to US5-3, SC-009 | the recipe check of T046 |
| US5-4 | the documentation review of T047 |
| Edge "default level given twice" | `TestDefaultLevel_LastWins` |
| Edge "fanout with one output" | `TestFanout_OneOutput` |
| Edge "no output, or an absent output" | `TestFanout_InvalidOutputs` |
| Edge "the same output given twice" | `TestFanout_SameOutputTwice` |
| Edge "a fanout inside a fanout" | `TestFanout_Nested` |
| Edge "a fanout used on its own" | `TestFanout_OnItsOwn` |
| Edge "a fanout wrapped by another handler" | `TestFanout_Hidden` |
| Edge "a level variable changed while logging", SC-002 | `TestDefaultLevel_Concurrent` |
| Edge "a zero value returned as present" | `TestContextValue_ZeroValue` |
| Edge "a context value and an extraction function with the same key" | `TestContextValue_FirstConfiguredWins` |
| SC-001 | `TestFanout_LevelMatrix` |
| SC-003 | `TestContextValue_RequestMiddleware` |
| SC-006 | `TestAlloc_AllOptionsDisabled`, `BenchmarkDisabled` |
| SC-007 | `BenchmarkEnabled` |
| FR-006 (own copy) | `TestFanout_OwnCopy` |
| FR-011 | `TestMultiHandler_AppliesOutputLevels` |
| FR-015 | `TestContextValue_LogValuer` |
| FR-021 (conformance) | `TestNew_Conformance`, `TestFanout_Conformance`, `TestDefaultLevel_Conformance`, `TestContextValue_Conformance`, the migrated conformance tests |

---

## Dependencies & Execution Order

### Phase dependencies

- **Setup (Phase 1)** then **Foundational (Phase 2)**: T001 → T002 → T003 → T004; then the
  migrations T005–T012 in parallel; then T013; then T014 → T015 → T016 → T017 → T018 → T019.
- **US1 (Phase 3)** depends on Phase 2: `WithDefaultLevel` uses the new option model.
- **US2 (Phase 4)** depends on US1: `New` requires `WithDefaultLevel` over a fanout, and every
  fanout test uses it.
- **US3 (Phase 5)** depends on Phase 2 only, in principle; it is done after US2 because
  `TestContextValue_RequestMiddleware` uses `WithDefaultLevel`, and T039 edits `handler.go` after
  T032.
- **US4 (Phase 6)** depends on US1–US3: `TestNew_AnyCombination` uses every option.
- **US5 (Phase 7)** depends on US1–US4: the documentation describes the final API.
- **Polish (Phase 8)** depends on every story.

The recommended order is the phase order. Each checkpoint leaves `make check MODULE=slogx`
green.

### Within each phase

- Test tasks come first, and are observed failing: T022 for US1, T030 for US2, T037 and T038 for
  US3. The removal in Phase 2 is guarded instead by T004 and T013, which pass before and after
  it. US4's T041 guards behavior that already holds.
- **Shared files**: tasks on the same file never run in parallel.

  | File | Edited by |
  |------|-----------|
  | `slogx/golden_test.go`, `slogx/testdata/v0.1.0.golden` | T003, T004 |
  | `slogx/context_test.go` | T005 |
  | `slogx/panic_test.go` | T006 |
  | `slogx/contexthandler_test.go` | T007 |
  | `slogx/redact_test.go` | T008 |
  | `slogx/level_test.go` | T009, T020 |
  | `slogx/handler_test.go` | T010, T018, T041 |
  | `slogx/fuzz_test.go` | T011 |
  | `slogx/example_test.go` | T012, T018, T021, T029, T036 |
  | `slogx/options_internal_test.go` | T014, T038 |
  | `slogx/options.go` | T015, T022, T032, T037, T039 |
  | `slogx/handler.go` | T016, T023, T032, T039 |
  | `slogx/level.go`, `slogx/sampling.go` (deleted) | T016, T017 |
  | `slogx/contexthandler.go` | T017, T039 |
  | `slogx/redact.go` | T017 |
  | `slogx/context.go` | T017, T023 |
  | `slogx/sampling_test.go`, `slogx/sampling_internal_test.go` (deleted) | T018 |
  | `slogx/bench_test.go` | T018, T048 |
  | `slogx/redact_internal_test.go` | T018 |
  | `slogx/fanout_test.go` | T025, T026, T027, T028 |
  | `slogx/fanout.go` | T030, T031 |
  | `slogx/helpers_test.go` | T034 |
  | `slogx/contextvalue_test.go` | T035 |
  | `slogx/CHANGELOG.md` | T042 |
  | `slogx/doc.go` | T044 |
  | `slogx/README.md` | T045 |
  | `slogx/alloc_test.go` | T049 |

### Parallel opportunities

- **Phase 2**: T005–T012 together, eight files. Everything else in sequence.
- **US1**: T020 and T021 together. Then T022, T023, T024.
- **US2**: T025 → T026 → T027 → T028 (same file), with T029 in parallel with them. Then T030,
  T031, T032, T033.
- **US3**: T034, then T035 and T036 together. Then T037, T038, T039, T040.
- **US4**: T041 and T042 together. Then T043.
- **US5**: T044 and T045 together. Then T046 and T047.
- **Polish**: T048 to T052 together. Then T053, T054 and T055.

---

## Parallel Example: Foundational migrations

```bash
Task: "T005 contextLogger through New in slogx/context_test.go"
Task: "T006 New instead of NewContextHandler in slogx/panic_test.go"
Task: "T007 New instead of NewContextHandler in slogx/contexthandler_test.go"
Task: "T008 New instead of NewRedactHandler in slogx/redact_test.go"
Task: "T009 New instead of NewLevelHandler in slogx/level_test.go"
Task: "T010 manual chain removed from slogx/handler_test.go"
Task: "T011 New instead of NewRedactHandler in slogx/fuzz_test.go"
Task: "T012 examples built with New in slogx/example_test.go"
# then T013: the migrated suite passes on the library of 0.1.0
```

## Parallel Example: User Story 2

```bash
# Write the failing tests (T025–T028 share fanout_test.go, so they run in sequence):
Task: "T025–T028 fanout tests in slogx/fanout_test.go"
Task: "T029 ExampleFanout in slogx/example_test.go"

# Observe the failures with the stub (T030), then implement:
Task: "T031 fanout in slogx/fanout.go"
Task: "T032 New recognizes a fanout in slogx/handler.go"
```

## Parallel Example: User Story 5

```bash
Task: "T044 package documentation in slogx/doc.go"
Task: "T045 README with three recipes in slogx/README.md"
# then T046 (recipes in a separate project) and T047 (documentation review)
```

---

## Implementation Strategy

### MVP first (User Story 1)

1. Phase 1 and Phase 2: one handler, the old output guarded.
2. Phase 3: the default level. **Stop and validate**: over an output that enables every level,
   the threshold applies, and a request can still lower or raise it.

This alone fixes the first problem of the spec for a service with one output.

### Incremental delivery

1. Add US2: stdout and OpenTelemetry receive the same records. US1 and US2 together fix both
   traps, and are the smallest releasable change.
2. Add US3: the request identifier in one option.
3. Add US4 and US5: the CHANGELOG, the documentation and the recipes. Release only after
   Phase 8, as `slogx/v0.2.0`, in its own `chore(slogx): prepare release 0.2.0` pull request.

---

## Notes

- [P] tasks touch different files and depend on no incomplete task.
- Each story phase ends with a checkpoint that runs the whole suite and the local gates.
- Never regenerate `testdata/v0.1.0.golden` from the working tree: only from a checkout of
  `slogx/v0.1.0` (T004, quickstart §5).
- The stubs of T030 and T037 exist only to observe the failures; T031 and T039 replace them.

---

description: "Task list for the error wrapping and error detail module"
---

# Tasks: Error Wrapping and Error Detail Module

**Input**: Design documents from `specs/003-error-wrapping/`

**Prerequisites**: [plan.md](plan.md), [spec.md](spec.md), [research.md](research.md),
[data-model.md](data-model.md), [contracts/](contracts/), [quickstart.md](quickstart.md)

**Tests**: REQUIRED. Constitution Principle III is non-negotiable:

- Tests are written first and observed failing (a compilation failure counts as red).
- Coverage must be ≥ 80%, with `-race -shuffle=on`.
- External input that reaches the text layout (messages) needs a fuzz test.
- Performance-sensitive paths (SC-004) need benchmarks.
- Every primary API needs an `Example`.

Every test task below precedes the implementation it covers.

**Organization**: tasks are grouped by user story (spec.md US1–US5). Every path is relative to the
repository root. All Go code lives in the single module `errors/`, with one package `errors` and no
subfolders except `testdata/`.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: can run in parallel with other [P] tasks of the same phase: different files, and no
  dependency on an incomplete task.
- **[Story]**: US1–US5 from spec.md. Setup, Foundational and Polish tasks have no story label.

## Conventions for every task

- **Packages**:
  - Behavior tests use the external package `errors_test`. They import the module as `errors`
    and the standard library as `stderrors "errors"`.
  - Tests of unexported helpers use `package errors`, in files named `*_internal_test.go`.
- **Test names**: `Test<Feature>_<Scenario>`, chosen so that the `-run` patterns of
  [quickstart.md §3](quickstart.md) select them, e.g. `TestWrap_ParityWithStd`,
  `TestDescribe_LinearChain`, `TestSentinel_NoLocation`. The fuzz target is `FuzzDetailText`.
  Benchmarks use `b.Loop()`.
- **Test style**:
  - table-driven, with `t.Run`;
  - no wall-clock assertions and no `time.Sleep`;
  - goroutines are joined with `sync.WaitGroup` or channels;
  - expected source lines are captured on the **same line** as the call under test, with
    `callerLine(t)` (T006), so the tests survive edits.
- **Skip numbering** (research R-08): runtime.Callers index 0 is `runtime.Callers` and index 1 is
  `capture`.
  - `capture(skip, …)` calls `runtime.Callers(skip+2, …)`, so `skip == 0` designates
    `capture`'s caller.
  - Every exported entry point passes `1` (plus the `Skip` level) to its internal constructor,
    and every internal constructor passes `skip+1` to `capture`.
  - The package-level `Bind` passes `2`, because its default is level 1.
- **Lint**: golangci-lint must report zero findings. The only `//nolint` allowed in library code
  is
  `//nolint:errorlint // walking the tree must inspect each level's own Unwrap method, not its chain`,
  on the type switch inside `causes` (research R-17). In tests, identity comparisons of errors
  (`==`) may carry `//nolint:errorlint // identity check (<FR>)`. Test helpers take `testing.TB`
  and call `tb.Helper()` first (`thelper`). Subprocesses use `exec.CommandContext(t.Context(), …)`.
- **Documentation**: every exported identifier has a doc comment. `doc.go` holds the package
  doc, which is expanded in T039.

---

## Phase 1: Setup (Shared Infrastructure)

**Purpose**: create the module and register it with the repository tooling.

- [X] T001 Create `errors/go.mod` containing exactly `module github.com/paccolamano/go/errors` and `go 1.26.0`, with no `require` and no `replace`.
- [X] T002 [P] Create `errors/CHANGELOG.md` in Keep a Changelog format with an empty `## [Unreleased]` section. The entries are added in T041.
- [X] T003 [P] Create `errors/README.md` with:
  - the title `# errors`;
  - one paragraph of purpose ("a drop-in replacement for the standard `errors` package that records where errors are created and wrapped, and renders readable and structured error details");
  - `go get github.com/paccolamano/go/errors`;
  - "Minimum Go version: 1.26";
  - placeholder headings "Usage", "Migrating from the standard errors package", "Error detail" and "Logging", which are completed in T040.
- [X] T004 Run `make work` from the repository root so that `go.work` lists `./errors`. Then confirm that `scripts/structure-check.sh errors` reports PASS (depends on T001–T003).

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: the capture and tree-walking primitives that every story uses.

**⚠️ CRITICAL**: no user story work can begin until this phase is complete.

- [X] T005 Create `errors/doc.go` with a short package doc comment for `package errors` ("Package errors is a drop-in replacement for the standard library's errors package. It records where errors are created and wrapped, and describes error trees as text and as structured data."). It is expanded in T039.
- [X] T006 [P] Create `errors/helpers_test.go` (`package errors_test`) with:
  - `func callerLine(tb testing.TB) int`, which calls `tb.Helper()` and returns the line of its caller through `runtime.Caller(1)`;
  - `func normalizeLines(s string) string`, which replaces every `:<digits>)` with `:N)`, so text layouts can be compared without exact line numbers.
- [X] T007 [P] Write failing internal tests in `errors/trace_internal_test.go` (`package errors`):
  1. `TestCauses_Shapes`. `causes(err) []error` returns:
     - nil for nil and for an error without `Unwrap`;
     - nil when `Unwrap() error` returns nil;
     - `[c]` for `Unwrap() error`;
     - the non-nil entries of `Unwrap() []error`, in order.
  2. `TestCapture_Location`: `capture(0, false)` called from a test helper at a recorded line returns a PC that `runtime.CallersFrames` resolves to that helper and line, and a nil `*callStack`.
  3. `TestCapture_Origin`: `capture(0, true)` returns a non-nil `*callStack`. Its constraints, quoted from data-model.md: "`pcs` 1 to 32 entries, most recent call first. `pcs[0] == pc`." Called from a recursion 40 frames deep, it gives `len(pcs) == 32` and `truncated == true` ("true when the stack was deeper than 32 frames"). From 10 frames deep, `truncated == false`.
  4. `TestIsTraced`. It uses a fake test type that implements the unexported `tracer` interface with a settable `pc`. `isTraced` is:
     - false for nil and for a fake with `pc == 0`;
     - true for a fake with `pc != 0`;
     - true for `fmt.Errorf("q: %w", tracedFake)`;
     - false for `fmt.Errorf("q: %w", stderrors.New("x"))`;
     - true for `stderrors.Join(stderrors.New("a"), tracedFake)`;
     - false when the traced fake lies 70 foreign levels down, because the walk stops after 64 levels.
- [X] T008 Implement `errors/trace.go` until T007 passes:
  - `const maxStackDepth = 32`, `maxDetailDepth = 64` and `maxDetailLevels = 256`;
  - `type callStack struct { pcs []uintptr; truncated bool }`;
  - `type tracer interface { trace() (pc uintptr, origin *callStack) }`;
  - `func capture(skip int, origin bool) (uintptr, *callStack)`:
    - a plain level uses `runtime.Callers(skip+2, buf[:1])`;
    - an origin uses `runtime.Callers(skip+2, buf[:maxStackDepth+1])` into a stack array, sets `truncated` when `maxStackDepth+1` PCs come back, and copies at most 32 PCs into an exact-size slice;
    - package-initialization detection is **not** part of this task (it comes in T037);
  - `func causes(err error) []error`, which carries the single allowed `//nolint:errorlint` (see Conventions);
  - `func isTraced(err error) bool`: walk with `causes`; at the first `tracer` of each branch return `pc != 0`; stop after `maxDetailDepth` levels.

**Checkpoint**: the foundation is ready. User stories can now begin.

---

## Phase 3: User Story 1 - Add context to errors as they propagate (Priority: P1) 🎯 MVP

**Goal**: create, wrap, annotate and combine errors with messages and inspection identical to the
standard library, while recording locations. This includes drop-in parity (FR-001 to FR-014).

**Independent Test**: build a three-level chain over a standard sentinel with the library and the
equivalent chain with `fmt.Errorf`. Then compare the messages and the results of `Is`, `As`,
`AsType` and `Unwrap`, wrap nil, and check the vet behavior on `Wrapf`.

### Tests for User Story 1 ⚠️ (write first, see them fail)

- [X] T009 [P] [US1] Write failing tests in `errors/std_test.go`:
  1. `TestDropIn_Forwarders`: for trees built with `fmt.Errorf`, `stderrors.Join` and the library, `errors.Is`, `errors.As`, `errors.AsType[*fs.PathError]` and `errors.Unwrap` return exactly what the `stderrors` functions return.
  2. `TestDropIn_ErrUnsupported`: `errors.ErrUnsupported == stderrors.ErrUnsupported`. `Is` matches it in both directions, also when it is wrapped by either package.
  3. `TestDropIn_StdlibSurface` (US1 scenario 8): a block written exactly as code against the standard package would be. It uses `New`, `Is`, `As` with a `*fs.PathError` target, `AsType`, `Unwrap`, `Join` and `ErrUnsupported`, with the outcomes documented by the standard library:
     - `Join(nil, nil) == nil`;
     - `Unwrap(New("x")) == nil`;
     - `As` fills the target;
     - `Is(Join(a, b), b)`.
  4. `TestDropIn_MixedPackages` (edge case): errors built with `stderrors` and wrapped by the library, and the reverse, are found by `Is` and `As` of both packages.
- [X] T010 [P] [US1] Write failing tests in `errors/errors_test.go`:
  1. `TestWrap_ParityWithStd` (US1 scenario 1, SC-001). The table holds chains over `fs.ErrNotExist` built with `Wrap`, `Wrapf` and `Errorf`, and the equivalent `fmt.Errorf("%s: %w")` chains. In each case:
     - the messages are equal;
     - `Is(err, fs.ErrNotExist)` holds;
     - the canonical chain's message is exactly `"handle request: load tenant 42: open config: file does not exist"`.
  2. `TestWrap_Nil` (US1 scenario 2, FR-013): `Wrap(nil, "x")`, `Wrapf(nil, "x %d", 1)`, `WithStack(nil)`, `Join()` and `Join(nil, nil)` each return an interface that is `== nil`.
  3. `TestWrap_EmptyContext`: `Wrap(err, "").Error() == err.Error()`.
  4. `TestWithStack_MessageUnchanged` (US1 scenario 6, FR-011).
  5. `TestWrap_CauseIdentity` (FR-014): `errors.Unwrap(Wrap(e, "x")) == e`, with an identity-check nolint.
  6. `TestErrorf_Parity` (FR-007, US1 scenario 3). For each format, the message equals `fmt.Errorf`'s, and every referenced error is found by `Is`. The formats are:
     - no `%w`;
     - one `%w`, for which `errors.Unwrap(Errorf("x: %w", e)) == e`;
     - two `%w`, for which `Unwrap() []error` has length 2, in operand order;
     - `%w` with a nil operand;
     - a repeated operand;
     - `%w` that is not at the end.
  7. `TestWrap_ThroughStdLevel` (US1 scenario 4): in `Wrap(fmt.Errorf("query: %w", Wrap(base, "inner")), "outer")`, `Is` finds `base`, and `As` finds a `*fs.PathError` placed below the standard level.
  8. `TestJoin_Semantics` (US1 scenario 7, FR-012, edge case "Combining a single error"):
     - `Join(a, nil, b)` has message `"a\nb"`, equal to `stderrors.Join(a, nil, b).Error()`, and `Unwrap() []error` returns `[a, b]`;
     - `Join(a)` returns a non-nil error with one cause;
     - `Join()` and `Join(nil)` return nil.
  9. `TestWrapf_Context`: `Wrapf(err, "load tenant %d", 42).Error() == "load tenant 42: " + err.Error()`.
- [X] T011 [P] [US1] Create the vet fixture `errors/testdata/vetmisuse/vetmisuse.go` and its opt-in test `errors/vet_test.go` (FR-010, US1 scenario 5):
  - `errors/testdata/vetmisuse/vetmisuse.go` declares `package vetmisuse`, imports `github.com/paccolamano/go/errors`, and has one exported function returning `[]error` with exactly these calls, one per line:
    - `errors.Errorf("ok: %w", fs.ErrNotExist)`;
    - `errors.Errorf("ok: %w %w", err, fs.ErrNotExist)`;
    - `errors.Wrapf(err, "bad: %w", fs.ErrNotExist)`;
    - `errors.Wrapf(err, "bad arg count %d")`.
  - `errors/vet_test.go` starts with `//go:build vetcheck` and defines `TestVetReportsWrapfMisuse`:
    - it locates `go` with `exec.LookPath`;
    - it runs `go vet ./testdata/vetmisuse` from the module directory with `GOWORK=off`;
    - it expects a non-zero exit;
    - it expects exactly 2 lines matching `vetmisuse.go:\d+:\d+:`: one containing `errors.Wrapf does not support error-wrapping directive %w`, and one containing `reads arg #1, but call has 0 args`.

### Implementation for User Story 1

- [X] T012 [US1] Implement `errors/std.go` until the forwarder parts of T009 pass:
  - `func Is(err, target error) bool`;
  - `func As(err error, target any) bool`, whose doc recommends `AsType` because vet's `errorsas` check covers only the standard `As`;
  - `func AsType[E error](err error) (E, bool)`;
  - `func Unwrap(err error) error`;
  - `var ErrUnsupported = stderrors.ErrUnsupported`, documented as "the standard library's own value".
- [X] T013 [US1] Implement `errors/errors.go` until T009 and T010 pass.
  - **Node types**, as in data-model.md:
    - `type wrapError struct { msg string; cause error; pc uintptr; origin *callStack }` (48 bytes);
    - `type joinError struct { msg string; errs []error; pc uintptr; origin *callStack }`.

    Each implements `Error() string` (returns `msg`), its `Unwrap` (`error`, or `[]error` for `joinError`) and `trace()`.
  - **Constructors**: the exported `New`, `Errorf`, `Wrap`, `Wrapf`, `WithStack` and `Join`, over the internal constructors `newText(skip, text)`, `newErrorf(skip, format, args...)`, `newWrap(skip, err, text)` and `newJoin(skip, errs)`.
  - **Messages**, quoted from the data-model message table:

    | Constructor | Message |
    |-------------|---------|
    | `New(text)` | `text` |
    | `Errorf(format, args...)` | `fmt.Errorf(format, args...).Error()` |
    | `Wrap(err, text)` and `Wrapf` | `text + ": " + err.Error()`, or `err.Error()` when `text == ""` |
    | `WithStack(err)` | `err.Error()` |
    | `Join(errs...)` | the messages of the non-nil errors, joined with `"\n"` |
  - **`Errorf`**: it calls `fmt.Errorf` and takes the causes from the result (one `%w` → `wrapError`, whose cause is the referenced error itself; two or more → `joinError`).
  - **`Wrapf`**: it uses `fmt.Sprintf` for the context, so that vet flags `%w`.
  - **Call stack**: each constructor calls `capture(skip+1, origin)`, where `origin` is `true` for `New` and `!isTraced(cause)` otherwise. For `Errorf` with several causes and for `Join`, `origin` is true only when none of the causes is traced.
  - **Nil**: wrapping, annotating and combining nil returns nil without allocating.
  - **Docs**: doc comments state the message rule and the location rule of each function.
- [X] T014 [US1] Write `ExampleWrap`, `ExampleErrorf`, `ExampleJoin` and `ExampleAsType` in `errors/example_test.go`. Their `// Output:` comments print only messages and inspection results, so they are deterministic (depends on T012 and T013).

**Checkpoint**: `GOWORK=off go test -race -run 'Wrap|WithStack|Errorf|Join|Parity|DropIn' .` passes in `errors/`, and `GOWORK=off go test -tags vetcheck -run TestVetReportsWrapfMisuse .` passes. The MVP is deliverable.

---

## Phase 4: User Story 2 - Diagnose an error from its detail (Priority: P1)

**Goal**: `Describe`, the text layout, the JSON form, locations and the formatting verbs
(FR-015 to FR-025, [contracts/detail-format.md](contracts/detail-format.md)).

**Independent Test**: build chains across functions (linear chains, combined errors, standard
levels, multi-line and hostile messages, pathological trees). Then check `Describe`, `String()`,
JSON and the verb parity.

### Tests for User Story 2 ⚠️

- [X] T015 [P] [US2] Write failing internal tests in `errors/location_internal_test.go` (`package errors`) for `locationOf(f runtime.Frame) Location`. Each row gives the frame's Function and File and the expected `Location`:

  | Function | File | Expected Function | Expected File |
  |----------|------|-------------------|---------------|
  | `example.com/app/api.Handler` | `/home/u/src/app/api/handler.go` | `example.com/app/api.Handler` | `example.com/app/api/handler.go` |
  | `example.com/app/api.Handler` | `/tmp/x/api/handler.go` | identical to the row above | identical to the row above |
  | `example.com/app/api.Handler` | `example.com/app/api/handler.go` (`-trimpath`) | identical to the row above | identical to the row above |
  | `gopkg.in/yaml%2ev3.(*T).M` | `…/yaml.v3/t.go` | `gopkg.in/yaml.v3.(*T).M` | `gopkg.in/yaml.v3/t.go` |
  | `main.main` | `/a/b/main.go` | `main.main` | `main/main.go` |
  | `example.com/app.F.func1`, `example.com/app.G[...]`, `example.com/app.(*S).M` | `/a/app/f.go` | the function name | `example.com/app/f.go` |
  | `runtime.goexit` | `/usr/lib/go/src/runtime/asm_amd64.s` | `runtime.goexit` | `runtime/asm_amd64.s` |
  | any | `""` | the function name | `<import path>/unknown` |

  In every row, Line is copied unchanged, and 0 stays 0.
  - `TestLocation_RealFrame`: a PC captured in the test resolves to `File == "github.com/paccolamano/go/errors/location_internal_test.go"`, with the right line.

  The constraints, quoted from data-model.md: "`File`: `<package import path>/<file base name>` … It never contains the build directory (FR-018)"; "`Line`: ≥ 1, or 0 when the runtime does not know the line".
- [X] T016 [P] [US2] Write failing tests in `errors/detail_test.go`:
  1. `TestDescribe_LinearChain` (US2 scenario 1, SC-003). Three functions in this file (`openConfig`, `loadTenant`, `handleRequest`) build `Wrap(fs.ErrNotExist, "open config")`, then `Wrapf(err, "load tenant %d", 42)`, then `Wrap(err, "handle request")`, each capturing its line with `callerLine`.
     - `Describe` returns exactly 4 levels, with texts `handle request`, `load tenant 42`, `open config` and `file does not exist`.
     - The first three have a `Location` with the right function, `File == "github.com/paccolamano/go/errors_test/detail_test.go"` and the right line.
     - Only `open config` has a `Stack`, with `Frames[0] == *Location`.
     - The last level has no `Location` and `Type == ""`.
  2. `TestDescribe_OwnText` (FR-020): one row per case of the own-text table of the contract, including:
     - a foreign type whose message rewrites its cause's (full message shown);
     - a foreign pass-through (`""`);
     - `Join` (`""`);
     - `Errorf` with two `%w` (full message).
  3. `TestDescribe_Types` (US2 scenario 5):
     - shown: `*fs.PathError`, `syscall.Errno` and a local `*thirdPartyError`;
     - hidden: `stderrors.New`, `fmt.Errorf` with one and with two `%w`, `stderrors.Join`, and library nodes;
     - shown as well: a local type named `errorString`, because hiding is matched by package path.
  4. `TestDescribe_ForeignLevel` (US2 scenario 4): `Wrap(fmt.Errorf("query: %w", Wrap(base, "inner")), "fetch")` gives `fetch` (with location), `query` (no location), `inner` (with location) and `base`.
  5. `TestDescribe_Annotations` (edge case "Empty context"):
     - `WithStack(pathErr)` gives one merged level: text `open a.txt`, type `*fs.PathError`, the `WithStack` line as location, and a stack. It is followed by `permission denied` with type `syscall.Errno`.
     - `Wrap(libErr, "")` gives a level with `Text == ""` and a location, followed by `libErr`'s levels.
  6. `TestDescribe_Branches` (US2 scenario 3, edge case "Shared causes"): `Wrap(Join(a, b), "sync")` gives the levels `sync` and a join level, whose `Text` is `""` and which has 2 `Branches`, each a chain. A cause shared by two branches appears in both.
  7. `TestDescribe_ConcurrentTasks` (US2 scenario 9): an origin created in a goroutine, joined through a channel, and wrapped in the test goroutine. The origin's `Stack` contains the goroutine's function and not the test function. The wrap's `Location` is the test function.
  8. `TestDescribe_StackTruncated` (US2 scenario 8): an origin created 40 frames deep gives `Stack.Truncated == true` and `len(Frames) == 32`.
  9. `TestDescribe_Pathological` (FR-025, SC-005, edge case "Pathological trees"). Each case returns without failure:
     - a comparable value type whose `Unwrap` returns itself: the chain has at most 64 levels, and the last one has `Truncated == true`;
     - a 10,000-level linked foreign type (constant `Error`, `Unwrap` to the next node): at most 64 levels, with `Truncated`;
     - `Join` of 1,000 `stderrors.New` values: at most 256 levels in total, and the join level has `Truncated == true`.
  10. `TestDescribe_PanickingMethods` (research R-10). `Describe` never panics, and:
      - an `Error` method that panics with `"boom"` yields `Text == "%!v(PANIC=Error method: boom)"`;
      - a nil pointer receiver whose `Error` dereferences yields `Text == "<nil>"`;
      - an `Unwrap` that panics keeps the level and follows no causes.
  11. `TestDescribe_Nil` (FR-025): `Describe(nil).Levels` is non-nil with length 0, and `String() == ""`.
  12. `TestDescribe_ForeignStackTrace` (edge case): a type with a `StackTrace() []uintptr` method is an ordinary level, with its text and type and no `Location`.
  13. `TestDescribe_Concurrent` (FR-003): 16 goroutines call `Describe`, `fmt.Sprintf("%+v")` and `Error()` on the same error. The results are identical, and the test passes under `-race`.
  14. `TestDetail_JSON` (US2 scenario 7, FR-024). After `json.Marshal(Describe(chain))`:
      - the only top-level key is `levels`;
      - level keys are a subset of `text`, `type`, `location`, `stack`, `branches` and `truncated`;
      - `location` keys are exactly `function`, `file` and `line`;
      - `stack` keys are a subset of `frames` and `truncated`;
      - unmarshalling into `Detail` gives a value that is `reflect.DeepEqual` to the original;
      - `Describe(nil)` marshals to `{"levels":[]}`.
- [X] T017 [P] [US2] Write failing tests in `errors/text_test.go` that compare `Describe(err).String()`, after `normalizeLines`, with exact expected strings. Name them `TestDetail_Text<Case>`. The cases, following the grammar of [contracts/detail-format.md](contracts/detail-format.md#grammar):
  - the linear chain of T016;
  - branches labeled `    [0]` and `    [1]`, with entries indented by +8;
  - the placeholders `(no message)` and `(2 errors)`;
  - the ` [*fs.PathError]` suffix;
  - multi-line text with `    | ` continuations;
  - the hostile message of the contract example (`"bad input\n    at evil.Fake (x.go:1)"` gives `    |     at evil.Fake (x.go:1)`);
  - a head line starting with a space, written Go-quoted;
  - a line containing a tab or `\x1b`, written Go-quoted;
  - the stack marker `        ... (truncated)`;
  - the cut marker `    ... (more causes not shown: limit reached)`;
  - no trailing newline;
  - the empty string for nil.
- [X] T018 [P] [US2] Write failing tests in `errors/format_test.go`:
  1. `TestFormat_StringVerbsParity` (FR-023, US2 scenario 2, SC-001): for a `Wrap` chain, a `Join` and an `Errorf` with two `%w`, the directives `%v`, `%s`, `%q`, `%x`, `%X`, `%10s`, `%-12v`, `%.3s`, `%#q` and `%+q` produce exactly what `stderrors.New(err.Error())` produces.
  2. `TestFormat_DetailVerb`: `fmt.Sprintf("%+v", err) == errors.Describe(err).String()`.
  3. `TestFormat_OtherVerbs`: `%d` and `%#v` equal `fmt.Sprintf(<same directive>, err.Error())`.
  4. `TestFormat_WrapDirective`: `fmt.Errorf("x: %w", libErr).Error() == "x: " + libErr.Error()`.
  5. `TestFormat_NilError` (edge case "No error"): `fmt.Sprintf("%+v", error(nil)) == "<nil>"`.
- [X] T019 [P] [US2] Write the fuzz target `FuzzDetailText(f *testing.F)` in `errors/fuzz_test.go`, taking three strings `a`, `b` and `c`.
  - It builds `errors.Wrap(errors.Join(errors.New(a), errors.Errorf("%s: %w", b, stderrors.New(c))), a)`, then renders `Describe(err).String()`.
  - It asserts that nothing panics, and that every line matches one of the grammar's line forms for its indentation:
    - a head line that does not start with a space, unless it is quoted;
    - `^ {I+4}(\| |at |stack:$|\.\.\. )`;
    - `^ {I+8}\S`;
    - `^ {I+4}\[\d+\]$`.
  - It asserts that the number of head lines equals the number of `Level` values in the `Detail`.
  - The seed corpus includes the hostile message, a tab, `\n`, `\r`, the empty string, leading spaces and non-ASCII text.

### Implementation for User Story 2

- [X] T020 [US2] Implement `errors/location.go` until T015 passes:
  - `func locationOf(f runtime.Frame) Location`, which:
    - unescapes `%2e` in the function name;
    - derives the import path from the escaped function name, up to the first `.` after its last `/`, then unescapes it;
    - builds `File` as the import path, `/` and `path.Base(f.File)`, or `/unknown` when `f.File == ""`;
  - `func resolve(pc uintptr) *Location`, which returns nil for `pc == 0` and otherwise takes the first frame of `runtime.CallersFrames([]uintptr{pc})`;
  - `func resolveStack(cs *callStack) *Stack`, which takes the frames of `runtime.CallersFrames(cs.pcs)` in order and sets `Truncated` from `cs.truncated`.
- [X] T021 [US2] Implement `errors/detail.go` until T016 passes (depends on T020).
  - The exported `Detail`, `Level`, `Location` and `Stack`, with field names and JSON tags exactly as in [contracts/public-api.md](contracts/public-api.md#error-detail-fr-019-to-fr-025).
  - `func Describe(err error) Detail`, implementing the derivation steps 1–7 of [contracts/detail-format.md](contracts/detail-format.md):
    - the walk with `causes`;
    - the own-text rules;
    - hidden types matched by `reflect` package path and type name, using `reflect.TypeFor[wrapError]().PkgPath()` for this module;
    - the location and stack from `tracer`;
    - annotation merge and skip;
    - the `maxDetailDepth` and `maxDetailLevels` limits, with `Truncated`;
    - `recover` around foreign `Error` and `Unwrap` calls, with the fmt placeholders;
    - an empty non-nil `Levels` slice for nil.
  - The constraints, quoted from data-model.md: "`Branches` … one chain per cause, in `Unwrap` order"; "`Frames` … 1 to 32 entries, most recent call first. `Frames[0]` equals the level's `Location`."
- [X] T022 [US2] Implement `func (d Detail) String() string` in `errors/text.go`, following the grammar and quoting rules of [contracts/detail-format.md](contracts/detail-format.md#text-layout-detailstring-v), until T017 and T019 pass (depends on T021).
- [X] T023 [US2] Implement `errors/format.go` and wire it in, until T018 passes (depends on T022):
  - `func formatError(err error, s fmt.State, verb rune)`: `%+v` writes `Describe(err).String()` with `io.WriteString`; every other verb writes `fmt.Fprintf(s, fmt.FormatString(s, verb), err.Error())`;
  - `Format(s fmt.State, verb rune)` methods on `*wrapError` and `*joinError` in `errors/errors.go`, which call it.
- [X] T024 [US2] Write `ExampleDescribe` in `errors/example_test.go`. It prints, for each level, `Text` and whether `Location != nil`, so its output is deterministic (depends on T021).

**Checkpoint**: `GOWORK=off go test -race -run 'Describe|Detail|Format|Location' .` passes. US1 still passes.

---

## Phase 5: User Story 3 - Define custom error types that callers can branch on (Priority: P2)

**Goal**: `Skip` (FR-017), `BaseError`, `Custom` and `Bind` (FR-026 to FR-029).

**Independent Test**: define `ConnError` (with an embedded `BaseError` and `Addr`) and an
`OverrideError` with its own `Error()`. Check `AsType` through wraps, locations at levels 0, 1
and 2, messages, verbs and the detail type.

### Tests for User Story 3 ⚠️

- [X] T025 [P] [US3] Write failing tests in `errors/skip_test.go`:
  1. `TestSkip_HelperAttribution`: helpers in this file return `errors.Skip(1).X(...)` for each of `New`, `Errorf`, `Wrap`, `Wrapf`, `WithStack` and `Join`. Their caller captures `callerLine` on the same line. `Describe(err).Levels[0].Location` has that line and the caller's function, and for origins `Stack.Frames[0]` equals the `Location`.
  2. `TestSkip_ZeroIsPackageLevel`: `Skip(0).X` records the same function and line rule as `X`.
  3. `TestSkip_Negative`: `Skip(-3)` behaves as `Skip(0)`.
  4. `TestSkip_TwoLevels`: `Skip(2)` called from a nested helper attributes to the caller's caller.
  5. `TestSkip_KeepsSemantics`: `Skip(1).Errorf("x: %w", e)` keeps `Unwrap` identity, and `Skip(1).Wrap(nil, "x") == nil`.

  The rule, quoted from data-model.md: "Level 0 is the function that calls the library. Negative values count as 0. `Skip(0).X(...)` is identical to the package-level `X(...)`."
- [X] T026 [P] [US3] Write failing tests in `errors/custom_test.go`. They declare `type ConnError struct { errors.BaseError; Addr string }`, with `func NewConnError(addr string, cause error) *ConnError { return errors.Bind(&ConnError{Addr: addr}, cause, "connect "+addr) }` and no other methods (SC-007), and `type OverrideError struct { errors.BaseError; Code int }`, with `Error()` returning `fmt.Sprintf("code %d", e.Code)`.
  1. `TestCustom_AsTypeThroughWraps` (US3 scenario 1): `errors.AsType[*ConnError](Wrap(Wrap(ce, "a"), "b"))` finds it with the right `Addr`. `As` works too, and `Is(ce, cause)` holds.
  2. `TestCustom_Location` (US3 scenario 2): `Describe(ce).Levels[0].Location.Line` is the line that called `NewConnError`, and its `Type == "*errors_test.ConnError"`.
  3. `TestCustom_SkipLevels` (US3 scenario 3): constructors using `errors.Skip(0).Bind` attribute inside the constructor; `Skip(2).Bind` attributes to the caller's caller; `Skip(1).Bind` equals `Bind`.
  4. `TestCustom_Message` (US3 scenario 4, FR-029): without a cause, `"connect db"`; with a cause, `"connect db: root"`.
  5. `TestCustom_Override`: `%v`, `%s` and `%q` of an `*OverrideError` use its `Error()`. `%+v` shows `code 7 [*errors_test.OverrideError]` as the first head line.
  6. `TestCustom_DetailVerb`: `fmt.Sprintf("%+v", ce) == errors.Describe(ce).String()`, and it contains `[*errors_test.ConnError]`.
  7. `TestCustom_OriginRule`: `Bind` with an untraced cause records a `Stack`; with a traced cause it does not.
  8. `TestCustom_ZeroBase`: a `ConnError{}` that never went through `Bind` returns `Error() == ""`, has no `Location`, and formats without panicking.

### Implementation for User Story 3

- [X] T027 [US3] Implement `errors/skip.go` until T025 passes:
  - `type Skip int`;
  - an unexported `func (s Skip) n() int { return max(int(s), 0) }`;
  - the methods `New`, `Errorf`, `Wrap`, `Wrapf`, `WithStack` and `Join`, which call the internal constructors of T013 with skip `1 + s.n()` and keep the same nil rules;
  - a doc comment that quotes the numbering rule.
- [X] T028 [US3] Implement `errors/custom.go` until T026 passes (depends on T027):
  - `type BaseError struct { self error; msg string; cause error; pc uintptr; origin *callStack }`;
  - pointer-receiver methods `Error` (returns `msg`), `Unwrap` (returns `cause`), `Format` (calls `formatError(b.self, …)` when `self != nil`, otherwise `formatError(b, …)`), the unexported `trace()` and `base() *BaseError`;
  - `type Custom interface { error; base() *BaseError }`;
  - `func Bind[E Custom](e E, cause error, text string) E`, which calls `bind(2, e, cause, text)` and returns `e`;
  - `func (s Skip) Bind(e Custom, cause error, text string) error`, which calls `bind(1+s.n(), …)` and returns `e`;
  - `bind`, which sets `*e.base()` with `self = e`, a message composed as `Wrap` composes it (just `text` when `cause == nil`), and `capture(skip+1, !isTraced(cause))`;
  - doc comments covering the constraints of research R-13: embed by value, use through a pointer, initialize with `Bind`.
- [X] T029 [US3] Extend `errors/testdata/vetmisuse/vetmisuse.go` with `errors.Skip(1).Wrapf(err, "bad: %w", fs.ErrNotExist)` (invalid) and `errors.Skip(1).Errorf("ok: %w", fs.ErrNotExist)` (valid). Update `errors/vet_test.go` to expect exactly 3 findings; the new one contains `(github.com/paccolamano/go/errors.Skip).Wrapf does not support error-wrapping directive %w` (depends on T027).
- [X] T030 [US3] Write `ExampleBind` (`ConnError` and `AsType`) and `ExampleSkip` (a validation helper that uses `Skip(1).Errorf`) in `errors/example_test.go`, with deterministic outputs: messages, fields and whether `Location != nil` (depends on T028).

**Checkpoint**: `GOWORK=off go test -race -run 'Bind|Custom|Skip' .` passes, and so does the
`vetcheck` test with 3 findings. US1 and US2 still pass.

---

## Phase 6: User Story 4 - Feed error details to logs and tools (Priority: P2)

**Goal**: `Attr` and `DetailValue`, plus the plain-value masking that keeps ordinary log lines
standard (FR-030, [contracts/log-attributes.md](contracts/log-attributes.md)).

**Independent Test**: log through slog's JSON, text and default handlers into buffers, with and
without the helper, at enabled and disabled levels.

### Tests for User Story 4 ⚠️

- [X] T031 [P] [US4] Write failing tests in `errors/slog_test.go`. Every handler uses `HandlerOptions.ReplaceAttr` to drop `time`.
  1. `TestAttr_JSONShape` (US4 scenario 2): `logger.Error("request failed", errors.Attr(chain))` with the JSON handler, decoded with `encoding/json`. `record["error"]` has exactly the keys `message` (`== chain.Error()`) and `detail`, and `detail` equals the JSON of `errors.Describe(chain)`.
  2. `TestAttr_TextHandler`: the output contains `error.message="` and `error.detail="`, with the escaped text layout.
  3. `TestDetailValue_CustomKey`: `slog.Any("err", errors.DetailValue(chain))` produces the same shape under `err`, and `errors.Attr(chain).Key == "error"`.
  4. `TestAttr_Nil` (edge case "Logging no error"): the records for `errors.Attr(nil)` and for `slog.Any("err", errors.DetailValue(nil))` contain neither `error` nor `err`.
  5. `TestAttr_Lazy` (US4 scenario 3). It uses `countingError`, a foreign type whose `Error()` increments a counter, logged directly. The test asserts that:
     - `logger.Info` with `errors.Attr(countingError)` on a handler at level Warn leaves the counter at 0;
     - `logger.Warn` makes it at least 1.
  6. `TestSlog_PlainValues` (FR-030, last rule): for a `Wrap` chain, a `Join`, an `Errorf` with two `%w`, a `*ConnError` and an `*OverrideError`, `logger.Info("m", "err", e)` produces output byte-identical to `stderrors.New(e.Error())`, for the JSON and text handlers. For the default handler, the test sets `log.SetOutput(&buf)` and `log.SetFlags(0)`, restores `log.Writer()` and `log.Flags()` in `t.Cleanup`, calls `slog.Info`, and must not call `t.Parallel`.
  7. `TestSlog_PlainViewInspectable`: a wrapping `slog.Handler` resolves each attribute value. `a.Value.Resolve().Any().(error)` must be found by `stderrors.Is(…, root)` and by `stderrors.As(…, &*ConnError)`.

### Implementation for User Story 4

- [X] T032 [US4] Implement `errors/slog.go` and the `LogValue` methods until T031 passes.
  - `const defaultAttrKey = "error"`.
  - `func Attr(err error) slog.Attr` and `func DetailValue(err error) slog.Value`. Both build a `detailValuer{err}`, and both return an empty group (`slog.GroupValue()`) for nil.
  - `detailValuer.LogValue()` returns `slog.GroupValue(slog.String("message", err.Error()), slog.Any("detail", Describe(err)))`.
  - The unexported `type plain struct{ err error }`, with `Error()` and `Unwrap()` and no `Format`.
  - `LogValue() slog.Value` methods that return `slog.AnyValue(plain{err})`: on `*wrapError` and `*joinError` in `errors/errors.go`, and on `*BaseError` in `errors/custom.go`, where the method uses `self` when it is non-nil.
  - Doc comments describe the laziness, the nil behavior and the masking consequence of research R-14.
- [X] T033 [US4] Write `ExampleAttr` in `errors/example_test.go`. It uses a JSON handler on `os.Stdout` that drops `time`, and logs `errors.Attr(fs.ErrNotExist)`, so the output has no location and is deterministic: `{"level":"ERROR","msg":"open failed","error":{"message":"file does not exist","detail":{"levels":[{"text":"file does not exist"}]}}}` (depends on T032).

**Checkpoint**: `GOWORK=off go test -race -run 'Attr|Slog' .` passes, and every earlier story still passes.

---

## Phase 7: User Story 5 - Keep sentinel errors clean and frequent errors cheap (Priority: P3)

**Goal**: errors created during package initialization record nothing (FR-008), the origin
rule is checked end to end (FR-016), and the cost benchmarks exist (SC-004, SC-005).

**Independent Test**: declare package-level sentinels and describe them, alone and wrapped. Then
run the benchmarks against `fmt.Errorf`.

### Tests for User Story 5 ⚠️

- [X] T034 [P] [US5] Write failing tests in `errors/trace_test.go`. The package-level fixtures are `var errSentinel = errors.New("sentinel")`, `var errFromHelper = newSentinel("helper sentinel")` (where `newSentinel` calls `errors.New`), `var errWrappedAtInit = errors.Wrap(errSentinel, "wrapped at init")`, `var errAnnotatedAtInit = errors.WithStack(errSentinel)` and `var errJoinedAtInit = errors.Join(errSentinel, errFromHelper)`.
  1. `TestSentinel_NoLocation` (US5 scenario 1, FR-008, edge case "Package initialization"): every level of `Describe` of each fixture has a nil `Location` and a nil `Stack`. `errAnnotatedAtInit` renders as the single level `sentinel`, because the annotation is skipped.
  2. `TestSentinel_RootOfChain`: `Describe(Wrap(errSentinel, "get user"))` gives level 0, `get user`, with a `Location` and a `Stack` (the first wrap at run time is the origin), and level 1, `sentinel`, without a `Location`.
  3. `TestSentinel_Identity` (US5 scenario 2): `Is(Wrap(Wrap(errSentinel, "a"), "b"), errSentinel)` holds.
  4. `TestOrigin_NoSecondStack` (US5 scenario 3, FR-016, SC-002). Exactly one level has a `Stack` in each case:
     - `Wrap(Wrap(foreign, "a"), "b")`;
     - `Wrap(fmt.Errorf("q: %w", traced), "c")`.

     For `Join(traced1, traced2)`, the join level has no `Stack`.
  5. `TestInit_RuntimeErrorsKeepLocation`: `errors.New` inside a test has a `Location` and a `Stack`.
- [X] T035 [P] [US5] Add `TestDuringInit` to `errors/trace_internal_test.go` (`package errors`). Declare the package-level `var initPCs = callersForTest()`, where the helper returns `runtime.Callers(1, buf[:])` into a 64-slot buffer, so that it runs during initialization. Then:
  - `duringInit(initPCs)` is true;
  - PCs captured inside the test are false;
  - PCs captured inside a goroutine started by the test are false.
- [X] T036 [P] [US5] Write `errors/bench_test.go`. A `depth(n int, f func() error) error` helper creates every error at a call depth of 10, and package-level `sink` variables hold the results. The benchmarks, all using `b.Loop()` and reported with `-benchmem`:
  - `BenchmarkStdWrap` (`fmt.Errorf("ctx: %w", base)`);
  - `BenchmarkWrap` (the cause already carries a stack);
  - `BenchmarkWrapBehindForeign`;
  - `BenchmarkNew`;
  - `BenchmarkWrapForeign` (`fs.ErrNotExist`);
  - `BenchmarkErrorf` (`"ctx %d: %w"`);
  - `BenchmarkDescribe` (the 5-level chain);
  - `BenchmarkDescribePathological` (the 10,000-level linked type of T016);
  - `BenchmarkAttrDisabled` (`logger.Info` with `errors.Attr(err)` on a handler at Warn).

  The targets are in [quickstart.md §6](quickstart.md).

### Implementation for User Story 5

- [X] T037 [US5] Implement `func duringInit(pcs []uintptr) bool` in `errors/trace.go`, until T034 and T035 pass. It follows the rule of research R-07 verbatim:
  - scan from the outermost PC inward with `runtime.FuncForPC(pc-1)`;
  - skip names with the prefix `runtime.`, and remember whether `runtime.main` was seen;
  - at the first non-runtime frame, return `sawMain && name != "main.main"`;
  - return false when `FuncForPC` returns nil.

  Call it in `capture`'s origin branch, only when the stack was not truncated. When it returns true, `capture` returns `(0, nil)`.
- [X] T038 [US5] Write `Example_sentinel` in `errors/example_test.go`. It declares a package-level `var ErrNotFound = errors.New("not found")` in that file, and prints each level's `Text` and `Location != nil` for `errors.Describe(errors.Wrap(ErrNotFound, "get user"))`, with the output `get user true` / `not found false` (depends on T037).

**Checkpoint**: `GOWORK=off go test -race -run 'Sentinel|Init|Origin' .` passes, and every story passes together.

---

## Phase 8: Polish & Cross-Cutting Concerns

**Purpose**: documentation, release notes and the quickstart validation.

- [X] T039 Expand `errors/doc.go` into the full package documentation (FR-005, research R-20):
  - the location model: one location per level, one call stack per chain at its origin, and no location for errors created during package initialization, with the limitations of R-07;
  - when to use `New`, `Wrap`/`Wrapf`, `WithStack`, `Join` and `Skip`;
  - the detail and its text and JSON forms, with the limits (32 frames, 64 levels of depth, 256 levels);
  - custom types with `BaseError` and `Bind`;
  - logging with `Attr` and `DetailValue`, the key `"error"`, and the plain-value masking and its consequence for handlers that type-assert;
  - `AsType` rather than `As`;
  - using the module next to the standard package through the alias `stderrors`.
- [X] T040 [P] Complete `errors/README.md` under the placeholder headings of T003. Add:
  - a minimal example (`Wrap` across two functions, and `fmt.Printf("%+v\n", err)` with a sample of its output);
  - the migration note ("replace the import; every standard function is available with the same behavior");
  - the `%+v` and JSON notes;
  - an `Attr` example.
- [X] T041 [P] Fill `errors/CHANGELOG.md` `## [Unreleased]` → `### Added` with entries for:
  - drop-in parity (`Is`, `As`, `AsType`, `Unwrap`, `ErrUnsupported`);
  - the constructors with locations and an origin call stack;
  - sentinel handling;
  - `Skip`;
  - `BaseError`/`Bind`;
  - `Describe` with its text and JSON forms;
  - `%+v`;
  - `Attr`/`DetailValue` and plain slog values.
- [X] T042 Run `cd errors && GOWORK=off go mod tidy -diff`, which must print nothing, and the cross-build check of [quickstart.md §2](quickstart.md): `go vet ./...` for linux, darwin and windows × amd64 and arm64. There must be no failures.
- [X] T043 Run [quickstart.md §4](quickstart.md): `GOWORK=off go test -tags vetcheck -run '^TestVetReportsWrapfMisuse$' .` passes, and `go vet ./testdata/vetmisuse` prints exactly 3 findings and exits with status 1.
- [X] T044 Run `FuzzDetailText` for 30 s ([quickstart.md §5](quickstart.md)). Fix any failure, then commit the failing input under `errors/testdata/fuzz/FuzzDetailText/` as a regression seed.
- [X] T045 Run the benchmarks of [quickstart.md §6](quickstart.md) with `-count 5`, and record the medians in the PR description. Check the targets:
  - `BenchmarkWrap` and `BenchmarkWrapBehindForeign`: ≤ 3× the ns/op and ≤ 2× the B/op of `BenchmarkStdWrap`;
  - `BenchmarkNew`: < 1500 ns/op;
  - `BenchmarkDescribePathological`: < 100 ms/op;
  - `BenchmarkAttrDisabled`: ≤ 1 alloc/op.

  Optimize the implementation if a target is missed. Do not change the targets.
- [X] T046 Verify the exported surface against [contracts/public-api.md](contracts/public-api.md). Run `GOWORK=off go doc -all .` in `errors/` and confirm that it lists exactly the identifiers in the contract, each with a doc comment. Then run `GOWORK=off go test -coverprofile=c.out . && go tool cover -func=c.out` and confirm that no exported function or method is at 0% (Principle III).
- [X] T047 Run the manual checks of [quickstart.md §7](quickstart.md):
  - build-directory independence: two directories, identical `%+v` output, no absolute paths;
  - a sentinel in a real `main` program, which has no location.

  Use a throwaway `go.mod` with a local `replace` outside the repository, and never commit it.
- [X] T048 Run the local gates with `make check MODULE=errors`. golangci-lint must report zero findings, with only the `//nolint` directives allowed by Conventions, and `go test -race -shuffle=on` must pass with coverage ≥ 80%. Fix any finding before pushing.

---

## Dependencies & Execution Order

### Phase dependencies

- **Setup (Phase 1)**: no dependencies.
- **Foundational (Phase 2)**: depends on Setup and blocks every user story.
- **US1 (Phase 3)**: depends on Foundational. Every later story builds on its node types and
  constructors (T013).
- **US2 (Phase 4)**: depends on US1 (T013), because `Describe` walks library nodes and
  `Format` is added to them.
- **US3 (Phase 5)**: depends on US1 (internal constructors). Its detail and `%+v` tests
  (T026 cases 2, 5 and 6) also need US2 (T021–T023).
- **US4 (Phase 6)**: depends on US1. Its `detail` attribute and its masking tests need US2
  (T021, T023). The custom-type rows of T031 need US3 (T028).
- **US5 (Phase 7)**: depends on Foundational and US1. Its tests read `Describe` (US2), and
  `BenchmarkAttrDisabled` needs US4.
- **Polish (Phase 8)**: depends on every story.

The stories therefore run in priority order: US1 → US2 → US3 → US4 → US5. Each one adds a
complete, testable increment, and each leaves the earlier stories' tests green.

### Within each story

- Test tasks are written first and observed failing. A compilation failure counts.
- Implementation tasks follow their tests: location → detail → text → format (US2), and
  skip → custom (US3).
- Example tasks depend on the implementation they show.
- `errors/errors.go` is edited by T013, T023 (Format) and T032 (LogValue), and
  `errors/example_test.go` by T014, T024, T030, T033 and T038. Tasks on the same file never run
  in parallel.

### Parallel opportunities

- **Setup**: T002 and T003.
- **Foundational**: T006 and T007, then T008.
- **US1**: the test tasks T009, T010 and T011 together. Then T012 and T013, which touch different
  files, can run in parallel, then T014.
- **US2**: the test tasks T015–T019 together, then the chain T020 → T021 → T022 → T023, with T024
  after T021.
- **US3**: T025 and T026 together, then T027 → T028, with T029 after T027 and T030 after T028.
- **US4**: T031, then T032, then T033.
- **US5**: T034, T035 and T036 together, then T037, then T038.
- **Polish**: T040 and T041 together, and T042–T044 in any order. T045–T048 come last.

---

## Parallel Example: User Story 2

```bash
# Write all failing tests for US2 together:
Task: "T015 location tests in errors/location_internal_test.go"
Task: "T016 Describe tests in errors/detail_test.go"
Task: "T017 text layout tests in errors/text_test.go"
Task: "T018 verb parity tests in errors/format_test.go"
Task: "T019 FuzzDetailText in errors/fuzz_test.go"

# Then implement the chain:
"T020 location.go" → "T021 detail.go" → "T022 text.go" → "T023 format.go"   (T024 after T021)
```

## Parallel Example: User Story 1

```bash
Task: "T009 drop-in parity tests in errors/std_test.go"
Task: "T010 constructor tests in errors/errors_test.go"
Task: "T011 vet fixture and opt-in test in errors/testdata/vetmisuse/ and errors/vet_test.go"
# then T012 (std.go) and T013 (errors.go) in parallel, then T014 (examples)
```

---

## Requirement Coverage

| Requirement | Tasks |
|-------------|-------|
| FR-001 | T009, T012 |
| FR-002 | T009, T010, T013 |
| FR-003 | T013, T016 (case 13) |
| FR-004, SC-008 | T001, T042 |
| FR-005 | T014, T024, T030, T033, T038, T039, T040 |
| FR-006, FR-007, FR-009, FR-011 to FR-014 | T010, T013 |
| FR-008 | T034, T035, T037 |
| FR-010 | T011, T029, T043 |
| FR-015, FR-016 | T007, T008, T016, T034 |
| FR-017 | T025, T027 |
| FR-018, SC-006 | T015, T020, T047 |
| FR-019 to FR-022, FR-024, FR-025 | T016, T017, T019, T021, T022 |
| FR-023 | T018, T023, T026 |
| FR-026 to FR-029, SC-007 | T026, T028 |
| FR-030 | T031, T032 |
| SC-001 | T009, T010, T018 |
| SC-002, SC-003 | T016, T017, T034 |
| SC-004, SC-005 | T016, T036, T045 |
| SC-009 | T046, T048 |

---

## Implementation Strategy

### MVP first (User Story 1)

1. Phase 1: Setup (T001–T004).
2. Phase 2: Foundational (T005–T008). This is critical: it blocks everything else.
3. Phase 3: US1 (T009–T014).
4. **Stop and validate**: run `GOWORK=off go test -race ./...` and the `vetcheck` test in
   `errors/`, then `make check MODULE=errors`. At this point the module is already a drop-in
   replacement that records locations, even before the detail exists.

### Incremental delivery

1. US2 (P1): the detail, which is the reason to adopt the library.
2. US3 (P2): custom types and helpers.
3. US4 (P2): logging.
4. US5 (P3): clean sentinels and the cost benchmarks.
5. Polish: docs, quickstart validation, and the release of `errors/v0.1.0` after the PR is
   squash-merged.

Each step leaves `make check MODULE=errors` green, so the work can pause after any story.

---

## Notes

- Every task keeps to the single module `errors/`. `testdata/vetmisuse` is a fixture, not a
  package of the module's API. The `./...` patterns ignore it.
- Never weaken a test to make it pass. Fix the implementation, or update the spec and contracts
  first if the behavior itself must change.
- Commit after each task or logical group. The PR title is
  `feat(errors): add error wrapping and error detail module`.

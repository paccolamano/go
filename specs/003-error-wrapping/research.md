# Research: Error Wrapping and Error Detail Module

**Feature**: [spec.md](spec.md) | **Plan**: [plan.md](plan.md) | **Date**: 2026-10-16

Each section records a decision, its rationale and the alternatives rejected. Standard library
APIs were checked with `go doc` against the local Go 1.27 toolchain, and are all available in Go
1.26, the module's minimum version.

The risky decisions were validated by a throwaway spike, kept outside this repository: a minimal
module with this module's path, built and linted with the repository's golangci-lint
configuration. The measurements below come from that spike, run on Go 1.27.0, linux/amd64 and an
Intel i7-11800H, with the error created at a call depth of 10 and 3 runs per benchmark. The
behavior of the reference library is in [analysis.md](analysis.md).

## R-01 Module layout, versioning and Go version

- **Decision**:
  - One module, `github.com/paccolamano/go/errors`, in the top-level directory `errors/`, with
    one package named `errors`. There are no subpackages and no `internal/` tree.
  - The `go` directive is `go 1.26.0`. CI tests on 1.26 and 1.27.
  - Releases start at `errors/v0.1.0` and stay at 0.x until the API is declared stable.
  - Inside the module, the standard package is imported as `stderrors`.
  - `go.work` is regenerated with `make work`.
- **Rationale**:
  - The spec requires a drop-in replacement with the same package name (FR-001).
  - The whole library is about 1,000 lines with one purpose, so one package keeps the import path
    identical to the one developers replace.
  - The slog integration needs no package of its own: the core already imports `log/slog`,
    because library errors implement `slog.LogValuer` (R-14).
- **Alternatives rejected**:
  - A separate `errors/errslog` subpackage for `Attr`: the masking of R-14 must live on the error
    types themselves, so the core imports `log/slog` anyway.
  - An `internal/stack` package: there is no second consumer, so it would add layout without
    benefit (Principle V).

## R-02 Drop-in parity with the standard package (FR-001)

- **Decision**:
  - `Is`, `As`, `AsType` and `Unwrap` are one-line forwarders to the standard functions.
  - `ErrUnsupported` is declared as `var ErrUnsupported = stderrors.ErrUnsupported`: the same
    value, not a copy, so `Is` matches it across both packages.
  - `New` and `Join` are the library's own implementations (R-03). Their messages and inspection
    results are identical to the standard ones, and they also record locations.
- **Rationale**:
  - Forwarding guarantees identical behavior and automatically picks up fixes in the standard
    library.
  - `gochecknoglobals` accepts `ErrUnsupported`, because it is an error-typed variable whose name
    starts with `Err` (verified in the spike).
- **Consequences**:
  - `go vet`'s `errorsas` analyzer checks only calls to the standard `errors.As`. A call to this
    package's `As` with a non-pointer target is therefore not reported by vet: it panics at run
    time, as the standard function does. The package documentation recommends `AsType`, which is
    type-safe at compile time.
  - Future additions to the standard package are mirrored in a minor release once the minimum Go
    version includes them (spec, Assumptions).
- **Alternatives rejected**:
  - Re-implementing `Is` and `As`: no benefit, and there is a risk of diverging from the
    standard semantics.
  - Declaring a new `ErrUnsupported` value: it would not match the standard sentinel, which
    violates FR-001.

## R-03 Error representation

- **Decision**: two unexported node types, both immutable after construction.

  | Type | Fields | Used by | Unwrap |
  |------|--------|---------|--------|
  | `*wrapError` | `msg string`, `cause error`, `pc uintptr`, `origin *callStack` (48 bytes) | `New`, `Errorf` with zero or one `%w`, `Wrap`, `Wrapf`, `WithStack` | `Unwrap() error`, which returns `cause` (nil for `New`) |
  | `*joinError` | `msg string`, `errs []error`, `pc uintptr`, `origin *callStack` | `Join`, `Errorf` with two or more `%w` | `Unwrap() []error`, which returns `errs` |

  - **Message**: `msg` is the full message, composed once at construction, as `fmt.Errorf` does.
    - `Wrap`: `text + ": " + cause.Error()`, or `cause.Error()` when `text` is empty.
    - `Errorf`: the string produced by `fmt.Errorf`.
    - `Join`: the messages joined with `"\n"`.

    `Error()` returns `msg`.
  - **Location**: `pc` is the location (FR-015); 0 means "no location".
  - **Call stack**: `origin` points to the call stack and is set only on origins (FR-016).
- **Rationale**:
  - Composing the message at construction reproduces the standard semantics exactly (SC-001):
    the message is frozen when the error is created, even if a cause's message could change
    later. It also makes `Error()` O(1) with no allocation.
  - The call stack sits behind a pointer, so a plain wrap costs a 48-byte node. The spike's
    first layout embedded the stack fields (72 bytes) and allocated 88 B per wrap against 40 B
    for `fmt.Errorf`, over the 2× memory target. A 48-byte node brings it to about 56 B.
  - Two types are needed because a type implements either `Unwrap() error` or
    `Unwrap() []error`, and the standard inspection treats them differently.
- **Alternatives rejected**:
  - Composing the message lazily in `Error()`, as `pkg/errors` and the reference do: it costs
    O(depth) allocations on every call and diverges from `fmt.Errorf` when a cause's message
    changes.
  - Storing each level's own text separately: the detail derives it reliably (R-10), so the extra
    field is not needed.
  - One type per constructor: no behavioral difference, more code.

## R-04 Formatted constructors and the error-wrapping directive (FR-007, FR-010)

- **Decision**:
  - `Errorf` calls `fmt.Errorf(format, args...)` and extracts the result's causes.
    - Exactly one `%w`: a `*wrapError` whose cause is the referenced error itself.
    - Two or more: a `*joinError` with fmt's list.
    - None: a `*wrapError` without a cause.

    The message is fmt's message.
  - `Wrapf` formats its context with `fmt.Sprintf` and then wraps.
- **Rationale**:
  - Delegating to fmt makes every directive behave exactly like the standard library, including
    a nil `%w` operand and repeated operands.
  - Extracting the causes keeps `errors.Unwrap(Errorf("x: %w", e)) == e`, as with `fmt.Errorf`.
    The spike verified it.
  - `go vet`'s printf analyzer infers wrappers automatically:
    - `Errorf` and `Skip(n).Errorf` forward to `fmt.Errorf`, so `%w` is accepted, single or
      multiple.
    - `Wrapf` and `Skip(n).Wrapf` forward to `fmt.Sprintf`, so vet reports
      `does not support error-wrapping directive %w`, together with argument-count mistakes.

    The spike verified all of these cases. This satisfies FR-010 and User Story 1, scenario 5.
- **Alternatives rejected**:
  - Parsing the format to find `%w` ourselves: it duplicates fmt and risks diverging from it.
  - Letting `Wrapf` accept `%w` and adding the referenced error as an extra cause: the result
    would be ambiguous (which cause is the primary one?), and vet could no longer catch the
    common mistake.

## R-05 Capturing and resolving locations (FR-015)

- **Decision**:
  - **Plain level**: `runtime.Callers(skip, buf[:1])`. Only the program counter is stored.
  - **Origin**: `runtime.Callers(skip, buf[:33])` into a stack array. The PCs are copied into a
    slice of exactly the returned length, at most 32. The call stack is truncated when 33 PCs
    were returned (R-15).
  - **Resolution** happens only when a detail is produced, with `runtime.CallersFrames`.
  - Every exported entry point calls one internal constructor, which calls `capture`, so the
    number of frames to skip is a constant per entry point.
- **Rationale**:
  - Resolving function names and lines costs about 50 to 100 ns per frame, so it is paid only
    when someone reads the detail.
  - `runtime.CallersFrames` and the skip count of `runtime.Callers` both work on logical frames,
    including inlined ones. The spike checked this with `go test -gcflags=-m`: `Wrap` is inlined
    into every caller, and an inlined helper that calls `Wrap` is still reported with its own
    name and line.
  - The copy into an exact-size slice avoids the reference's fixed 256-byte array (analysis W5).
- **Measured** (spike):

  | Operation | Time/op | Memory/op | Allocs/op |
  |-----------|---------|-----------|-----------|
  | `runtime.Callers`, 1 PC | 162–177 ns | 0 | 0 |
  | `runtime.Callers`, 33 PCs (about 17 frames deep) | 532–578 ns | 0 | 0 |
  | package-initialization check (R-07) | 55–59 ns | 0 | 0 |

- **Alternatives rejected**:
  - `runtime.Caller(skip)`: it resolves the file and line immediately, on every call.
  - `runtime.FuncForPC` for resolution: its documentation discourages it for PCs returned by
    `runtime.Callers`, and it does not adjust return PCs.
  - Walking frame pointers with `unsafe`: not portable, and forbidden in spirit by
    Principles IV and V.

## R-06 Origin rule (FR-016)

- **Decision**:
  - A node is *traced* when its `pc` is non-zero: either it is an origin, or something below it
    carries a call stack.
  - A new error records the full call stack only when nothing in the tree it wraps is traced.
  - `isTraced(err)` walks down from `err`:
    - it stops at the first library node of each branch and returns that node's state, in O(1);
    - it steps through levels from other code, `fmt.Errorf` wrappers or third-party types, with
      the shared `causes` helper;
    - it stops after 64 levels (R-15).

  A `*joinError` and an `Errorf` with several `%w` are traced when any of their branches is.
- **Rationale**:
  - Each chain gets exactly one call stack, at its origin (SC-002), and every later wrap costs
    only one PC.
  - An error created during package initialization has `pc == 0` (R-07). The first wrap at run
    time therefore becomes the origin and records where the sentinel was actually returned.
  - The spike verified both cases: a wrap of a traced error behind a `fmt.Errorf` level records
    no second stack, and a wrap of a sentinel becomes the origin.
- **Alternatives rejected**:
  - A full stack at every level, as in the reference: 4 to 30 times the cost (analysis W5).
  - A stack only at `New`: errors from other code (for example `os.Open`) would never get one.

## R-07 Errors created during package initialization (FR-008)

- **Decision**: when an origin is captured and the stack was not truncated, the library scans
  the PCs from the outermost frame inward with `runtime.FuncForPC`:
  - it skips every frame whose function name starts with `runtime.`, and notes whether
    `runtime.main` was among them;
  - at the first non-runtime frame, the error is being created during package initialization
    exactly when `runtime.main` was seen and that frame is not `main.main`.

  In that case no location and no call stack are recorded.
- **Rationale**:
  - During initialization, the main goroutine runs
    `runtime.goexit → runtime.main → runtime.doInit… → <pkg>.init → …`. After it, the stack
    starts with `runtime.goexit → runtime.main → main.main`. Other goroutines start from
    `runtime.goexit` followed by their own function.
  - The check depends only on two names that every Go traceback has shown since Go 1.0
    (`runtime.main`, `main.main`) and on the `runtime.` prefix. It does not depend on internal
    names such as `doInit1`.
  - It covers every way a sentinel can be declared. The spike verified direct declarations,
    sentinels built by helper functions, and `Wrap` called in a package-level declaration, both
    in a test binary and in a real `main` program.
  - Cost: about 56 ns, and only on origins (R-05).
- **Limitations** (documented):
  - An error created in a goroutine that was started during initialization keeps its location.
  - So does an error created at the bottom of an initialization stack deeper than 32 frames,
    because the outermost frames are not captured.

  Both are harmless: such an error simply carries a location.
- **Alternatives rejected**:
  - A separate constructor for sentinels: it would not fix the idiomatic
    `var ErrX = errors.New("x")` (analysis W6), and that idiom is exactly what drop-in
    replacement preserves.
  - Matching `<pkg>.init` names: it requires resolving every captured frame, which costs about
    1 µs per origin.
  - A package-level "initialization finished" flag: it is global mutable state, which
    Principle IV forbids, and a library cannot tell when `main` starts anyway.

## R-08 Caller skipping (FR-017)

- **Decision**: `type Skip int`, with the methods `New`, `Errorf`, `Wrap`, `Wrapf`, `WithStack`,
  `Join` and `Bind`.
  - `errors.Skip(n).Wrap(err, "x")` attributes the location and the call stack to the n-th
    caller above the function that makes the call. Level 0 is that function itself, so
    `Skip(0).X` is identical to the package-level `X`.
  - Negative values count as 0.
  - `Skip(n).Bind` uses the same numbering. The package-level `Bind` corresponds to level 1
    (R-13, FR-028).
- **Rationale**:
  - A named integer type needs no constructor, and the conversion reads naturally at the call
    site: `errors.Skip(1).Errorf(...)`.
  - The methods mirror the package functions one to one, so the numbering is the same
    everywhere.
  - vet recognizes the methods as printf wrappers too (R-04).
  - The spike verified `Skip(1).Wrap` from a helper, and `Skip(0)`, `Skip(1)` and the default
    for `Bind`.
- **Alternatives rejected**:
  - Separate functions (`NewSkip`, `WrapSkip`, …): six extra top-level names.
  - A functional option such as `WithSkip(n)`: it allocates, and an option mixed with variadic
    format arguments breaks the printf analyzer.
  - Exporting `Callers` and stack types, as the reference does: a wider API and the off-by-one
    of analysis W7.

## R-09 Rendering locations (FR-018)

- **Decision**: a `Location` holds three values.
  - **`Function`**: the runtime function name, with the linker's `%2e` escapes turned back into
    `.`. For example, `gopkg.in/yaml%2ev3.(*T).M` becomes `gopkg.in/yaml.v3.(*T).M`.
  - **`File`**: the package import path, taken from the escaped function name up to the first
    `.` after its last `/` and then unescaped, followed by `/` and the base name of the source
    file.
  - **`Line`**: the line number.
- **Rationale**:
  - The import path and the base name do not depend on the build directory or on `-trimpath`
    (SC-006). They also never reveal local paths such as `/home/<user>`.
  - The escape handling is needed because the linker escapes dots in the last element of the
    import path. Without it, `gopkg.in/yaml.v3` would yield the package `gopkg.in/yaml`.
  - The spike verified a test function in the external test package, which resolved to
    `github.com/paccolamano/go/errors_test/spike_test.go`, and a real `main` program, which
    resolved to `main/main.go`.
- **Testing**: unit tests feed `runtime.Frame` values with the same function and different
  absolute file paths, including paths built with `-trimpath`, and expect identical locations.
  Real captured frames are checked against the expected import path. Running the go tool from
  tests is not needed.
- **Alternatives rejected**:
  - Absolute paths, as in the reference: they are not reproducible and they disclose local
    paths (analysis W3).
  - Module-relative paths: the module root is not available at run time without build
    information that may be missing.

## R-10 Building the detail (FR-019 to FR-022, FR-025)

- **Decision**: `Describe(err) Detail` walks the tree depth-first and builds `Level` values
  ([data-model.md](data-model.md), [contracts/detail-format.md](contracts/detail-format.md)).
  - **Causes**: a single helper, `causes(err) []error`, returns `Unwrap() error` as one element
    or `Unwrap() []error` without nil entries. It is the only type switch on errors in the
    module (R-17).
  - **Own text**:
    - With one cause: empty when the message equals the cause's message; the prefix when the
      message ends with `": " + cause message`; otherwise the full message.
    - With several causes: empty when the message equals the causes' messages joined with
      `"\n"`; otherwise the full message.

    The same rule applies to library levels and to levels from other code, and it is exact for
    every library constructor.
  - **Annotations**: an annotation is a library level with an empty own text and exactly one
    cause, for example one created by `WithStack` or `Wrap(err, "")`.
    - When the cause has no location, the two render as one level: the cause's text and type,
      with the annotation's location and call stack. This removes the reference's duplicate
      entries (analysis W3).
    - An annotation without a location adds nothing, so it is skipped.
    - Otherwise it renders on its own, with no text.
  - **Types**: `reflect.TypeOf(err).String()`, for example `*fs.PathError`, is shown except for
    the library's own node types and the standard library's generic types:
    - `errors.errorString`;
    - `errors.joinError`;
    - `fmt.wrapError`;
    - `fmt.wrapErrors`.

    They are matched on package path and type name, not on the string, so another package with
    a type called `errorString` is still shown.
  - **Limits**: at most 64 levels along any path, and at most 256 levels in total. A level whose
    causes are not followed because of a limit has `Truncated: true`. This bounds cycles and
    very large trees (SC-005).
  - **Panic safety**: every call to a foreign `Error` or `Unwrap` method runs under `recover`.
    A level whose method panics shows the placeholder that standard formatting would print
    (`<nil>` for a nil pointer receiver, otherwise `%!v(PANIC=Error method: …)`). Its causes are
    not followed.
  - `Describe(nil)` returns a `Detail` with an empty, non-nil `Levels` slice, which renders as
    empty text.
- **Rationale**:
  - The walk depends only on the standard `Unwrap` protocol, so it works on any error (FR-019).
  - Detail rendering usually runs in logging and recovery paths, where crashing or hanging would
    hide the original failure (analysis W9).
- **Alternatives rejected**:
  - Recursion without limits, as in the reference: it crashes on cycles (analysis W9).
  - Detecting cycles by comparing values: many error types are not comparable. The depth limit
    covers every case.
  - Showing every type name: it brings back the noise of analysis W3.

## R-11 Text layout

- **Decision**: the layout in [contracts/detail-format.md](contracts/detail-format.md):
  - **Levels**: one entry per level. The head line holds the level's own text, followed by
    ` [type]` when the type is shown.
  - **Under each head line**, indented by 4 spaces, as applicable:
    - continuation lines, prefixed `| `;
    - `at <function> (<file>:<line>)`;
    - `stack:` followed by the frames, indented by 8;
    - truncation markers.
  - **Branches**: they are labeled `[i]` at +4, with their entries at +8.
  - **Placeholders**: an empty own text renders as `(no message)`, or `(N errors)` for a level
    with several causes.
  - **Quoting**: a text line is written Go-quoted (`strconv.Quote`) when it contains invalid UTF-8
    or a character that `strconv.IsPrint` rejects, or when it starts with `"`. A head line is also
    quoted when it is empty or starts with a space. The `"` rule was added during implementation,
    after `FuzzDetailText` showed that a raw line starting with `"` could not be told apart from a
    quoted one.
  - Lines are separated by `\n`, with no trailing newline.
- **Rationale**:
  - Every line begins with a marker or with the indentation of its entry, so a message cannot
    impersonate a location or another level, even when it contains user input with newlines or
    leading spaces (FR-022, log forging).
  - The structured form keeps the original text unquoted.
- **Alternatives rejected**:
  - The reference's per-entry Go type names and absolute paths (analysis W3).
  - Tree-drawing characters: harder to grep, and less safe in plain-ASCII log pipelines.

## R-12 Formatting verbs (FR-023)

- **Decision**: `*wrapError`, `*joinError` and `BaseError` implement `fmt.Formatter`:
  - `%+v` writes `Describe(err).String()`.
  - Every other verb writes `fmt.Fprintf(s, fmt.FormatString(s, verb), err.Error())`, which
    formats the message string with the same flags, width and precision.
- **Rationale**:
  - For an error operand, `fmt` formats `Error()` as a string with the verbs `v`, `s`, `x`, `X`
    and `q`. Reconstructing the directive with `fmt.FormatString` (Go 1.20) therefore gives
    byte-identical output.
  - The spike compared `%v`, `%s`, `%q`, `%x`, `%X`, `%10s`, `%-12v`, `%.3s` and `%#q` with a
    standard error, and they were identical.
  - Verbs that are not string verbs (`%d`, `%#v`) cannot match a standard error, whose output
    depends on its own Go representation. They format the message string instead, and the
    contract says so. The spec's FR-023 is refined accordingly.
  - fmt formats `%w` inside `fmt.Errorf` through `Format` with verb `v`, so that wrapping is
    unaffected.
- **Interaction with slog**: slog's text handler and default handler format every non-string
  value with `%+v`. Without countermeasures, every library error logged as a plain value would
  print its full detail (verified in the spike). See R-14.
- **Alternatives rejected**:
  - No `Formatter`, so that `%+v` prints only the message: simpler, but it drops the `pkg/errors`
    convention that FR-023 adopts, and with it a fix for analysis W2.
  - Writing `Error()` for every verb, as the reference does: breaks `%q`, widths and the other
    verbs (analysis W2).

## R-13 Custom error types (FR-026 to FR-029)

- **Decision**:

  ```go
  type BaseError struct{ /* self error; msg string; cause error; pc uintptr; origin *callStack */ }

  type Custom interface {
      error
      // contains an unexported method that only *T with an embedded BaseError has
  }

  func Bind[E Custom](e E, cause error, text string) E
  func (s Skip) Bind(e Custom, cause error, text string) error
  ```

  - A developer embeds `errors.BaseError` by value in a struct type and builds values with
    `return errors.Bind(&ConnError{Addr: addr}, cause, "connect "+addr)`.
  - `Bind` initializes the embedded `BaseError`:
    - **message**: composed as `Wrap` would compose it;
    - **cause**: as given;
    - **location and call stack**: at level 1, the caller of the developer's constructor, with
      the origin rule of R-06;
    - **back-reference**: `self = e`.

    It then returns `e`.
  - `BaseError` has pointer-receiver methods: `Error`, `Unwrap`, `Format` and `LogValue`
    (R-14), plus the unexported methods used by the detail. Because of promotion, `*ConnError`
    is a library error.
- **Rationale**:
  - **Why the back-reference**: promoted methods receive only the embedded `BaseError`, never
    the outer value. Without `self`, `%+v` would describe a `BaseError` and lose the developer's
    type (FR-027). Also, a developer who overrides `Error()` would see `%v` print a different
    message than `Error()`. With `self`, `Format` and `LogValue` act on the outer value. The
    spike verified that a type overriding `Error()` formats with its own message under `%v`,
    `%s` and `%q`.
  - The generic `Bind` returns the concrete type, so a constructor can return `*ConnError`. The
    unexported method in `Custom` means only pointers to types that embed `BaseError` satisfy
    the constraint.
  - `AsType[*ConnError]` finds the error through outer wraps (spike).
  - Go methods cannot have type parameters, so `Skip(n).Bind` takes the interface and returns
    `error`. It is needed only for non-default levels.
  - Adding methods to `Custom` later is compatible, because outside packages cannot implement it
    except through embedding (Principle II).
  - `errname` requires error type names to end in `Error`, which `BaseError` does.
- **Constraints** (documented):
  - Embed `BaseError` by value, and use the type through a pointer.
  - A `BaseError` that never went through `Bind` behaves as an error without a location and
    without a message.
  - Passing a nil pointer to `Bind` is a programming error and panics with a nil dereference,
    like any method call on a nil pointer.
- **Alternatives rejected**:
  - A `NewBase(cause, text) BaseError` value constructor: the outer value is invisible, with the
    two defects described above.
  - Requiring a hand-written `Format` method on custom types, as `cockroachdb/errors` does: it
    violates SC-007.
  - The reference's functional options: an inconsistent skip numbering (analysis W7).

## R-14 slog integration (FR-030)

- **Decision**:
  - `Attr(err) slog.Attr` returns `slog.Any("error", detailValuer{err})`.
  - `DetailValue(err) slog.Value` returns the same value without a key, so that a developer can
    choose the key: `slog.Any("err", errors.DetailValue(err))`.
  - `detailValuer.LogValue()` returns a group with two attributes:
    - `message`, the error's message;
    - `detail`, the `Detail` value.
  - For a nil error, both return an empty group, which handlers ignore.
  - **Masking**: `*wrapError`, `*joinError` and `BaseError` implement `slog.LogValuer` by
    returning `slog.AnyValue(plain{err})`. The unexported `plain` type has `Error()` and
    `Unwrap()` but no `Format`.
- **Rationale**:
  - Arguments to a log call are evaluated before the logger checks the level, so `Attr` must be
    cheap. It only wraps the error. slog resolves `LogValuer` values only when a handler
    processes the record. The spike verified that a disabled level never calls `Error()`.
  - With the JSON handler, `detail` serializes as the object of
    [contracts/detail-format.md](contracts/detail-format.md). With the text handler, it prints
    the text layout, because `Detail` implements `fmt.Stringer`.
  - **Why masking is needed**: R-12 makes `%+v` print the detail, and slog's text and default
    handlers format values with `%+v`. Without masking, `logger.Error("x", "err", err)` would
    print the full detail, which violates FR-030's last rule. With masking, the spike showed
    identical output to a standard error for the text, JSON and default handlers, including for
    a custom type that overrides `Error()`. Handlers that inspect attribute values can still use
    `errors.Is` and `errors.As` through the `plain` view.
  - An empty group is dropped by slog's built-in handlers, so a nil error adds nothing (spike).
- **Consequences** (documented):
  - **Handlers**: a custom handler that type-asserts an attribute value directly to a concrete
    error type, for example `v.Any().(*ConnError)`, sees the `plain` view and must use
    `errors.As` instead. Handlers built on `errors.As`, `errors.Is` or `Error()` are unaffected.
  - **Third-party loggers**: loggers that print `%+v` for errors (for example zap's
    `errorVerbose` field) show the detail, following the `pkg/errors` convention. This is
    outside FR-030, which concerns the standard logger.
- **Alternatives rejected**:
  - `LogValue` returning `slog.StringValue(err.Error())`: error-aware handlers would no longer
    see an error at all.
  - Library errors implementing `LogValue` with the full detail, so that it appears
    automatically: this is option C, which the user rejected (spec, Clarifications).
  - `AttrKey(key, err)`: `DetailValue` composes with `slog.Any` and `slog.Group` and needs no
    second attribute constructor.

## R-15 Limits and defaults

| Setting | Value | Notes |
|---------|-------|-------|
| Call stack depth | 32 frames | Same as the reference and `pkg/errors`. The most recent frames are kept, and a truncated stack has `Truncated: true`. |
| Detail depth | 64 levels along any path | Also bounds `isTraced` (R-06). |
| Detail size | 256 levels in total | About 100 KB of text at most. |
| slog key | `"error"` | `DetailValue` lets the developer choose another key. |

These values are unexported constants listed in the package documentation (FR-005). They are not
configurable: no requirement asks for it, and configuration through globals is forbidden
(Principles IV and V).

## R-16 Performance (SC-004)

- **Measured** (spike, before the 48-byte node of R-03):

  | Operation | Time/op | Memory/op | Allocs/op |
  |-----------|---------|-----------|-----------|
  | standard `fmt.Errorf("ctx: %w")` | 151–158 ns | 40 B | 2 |
  | `Wrap` of an error with a call stack | 291–294 ns | 88 B, about 56 B with the 48-byte node | 2 |
  | the same, with a standard-library level in between | 298–305 ns | 96 B | 2 |
  | `New` (origin, about 17 frames) | 974–1014 ns | 208 B | 2 |
  | `Wrap` of a foreign error (origin) | 1141–1256 ns | 232 B | 3 |
  | `Errorf` with a foreign `%w` (origin) | 1290–1305 ns | 272 B | 4 |

- **Decision**: SC-004 is refined to targets that the location capture can physically meet:
  - a wrap of an error that already carries a call stack: at most 3 times the time and 2 times
    the memory of the standard wrapping;
  - an origin created with `New` at a call depth of 10: under 1.5 µs.
- **Rationale**: capturing a single caller PC already costs about 170 ns (R-05), which is more
  than the whole standard wrapping (about 155 ns). The spec's original "twice the time" target
  was measured at 1.9×, which leaves no margin across machines. With the measured values, the
  refined targets have a margin of about 35%.
- **Benchmarks** in the module, all with `b.Loop` and `-benchmem`:
  - `BenchmarkStdWrap`, the baseline;
  - `BenchmarkWrap`, `BenchmarkWrapBehindForeign` and `BenchmarkNew`;
  - `BenchmarkWrapForeign` and `BenchmarkErrorf`;
  - `BenchmarkDescribe`, on a 5-level chain;
  - `BenchmarkAttrDisabled`, which must report 0 allocations beyond the `Attr` value itself.

## R-17 Lint compliance

The repository configuration was run on the spike.

- **`errorlint`** flags type switches on errors. The single `causes` helper (R-10) carries
  `//nolint:errorlint // walking the tree must inspect each level's own Unwrap method, not its
  chain`. Every other inspection uses an assertion to the module's own unexported interface,
  which `errorlint` does not flag.
- **`wrapcheck`**: no findings. The forwarders return the standard results directly.
- **`gochecknoglobals`**: `ErrUnsupported` is allowed. There are no other package-level
  variables: the constants of R-15 are `const`.
- **`errname`**: `BaseError` conforms. The unexported node types end in `Error`.
- **`forbidigo`**: no `panic` and no `fmt.Print*`. `fmt.Fprintf` writes to the `fmt.State`.
- **`revive`**: every exported identifier has a doc comment, and the package doc lives in
  `doc.go`.

## R-18 Testing approach (Principle III)

- **Style**:
  - the standard `testing` package, with table-driven subtests;
  - mostly the external test package `errors_test`, which imports the standard package as
    `stderrors`;
  - internal tests for the unexported helpers: location parsing, own-text isolation and the
    initialization check.
- **Locations**: tests record their expected line with a helper built on `runtime.Caller(1)`,
  placed next to the call under test, so the tests do not break when lines move.
- **Package initialization**: the test files declare package-level sentinels in three ways: a
  direct declaration, a helper function, and `Wrap` in a declaration. The test binary
  initializes them through `runtime.main`, the same path as a real `main` program. The
  quickstart adds a manual check with a real program.
- **Parity**: tables compare each library chain with the equivalent standard chain: messages,
  `Is` and `As` results, `Unwrap` identity, and the formatted output of every string verb with
  flags.
- **Detail**:
  - structured assertions on `Describe`;
  - text assertions in which only line numbers are normalized;
  - JSON round-trip tests;
  - pathological trees: a self-referencing error, 10,000 levels, 1,000 causes, panicking
    `Error` and `Unwrap` methods, nil pointer receivers.
- **Concurrency**: one error is described and formatted from 16 goroutines under `-race`.
- **Fuzz**: `FuzzDetailText` builds trees from fuzzed messages, which are external input that
  ends up in logs. It asserts that the text never panics and that every output line matches the
  grammar of [contracts/detail-format.md](contracts/detail-format.md), so the structure can be
  reconstructed unambiguously.
- **vet**: a fixture package under `testdata/vetmisuse/` holds deliberate misuse:
  - `Wrapf` and `Skip(1).Wrapf` with `%w`;
  - a missing format argument;
  - valid `Errorf` calls with one and two `%w`.

  The `./...` patterns of the lint and test gates ignore `testdata`, while
  `go vet ./testdata/vetmisuse` analyzes it. The spike confirmed that vet reports exactly the
  three misuses. `TestVetReportsWrapfMisuse` runs that command. It lives behind the build tag
  `vetcheck`, so it does not run in the default `go test` invocation (Principle III: it spawns
  the go toolchain). The quickstart runs it explicitly.
- **slog**: built-in handlers write into buffers, and one inspecting handler checks that the
  value can be inspected. Laziness is checked with an error type that counts its `Error()`
  calls.
- **Examples**:
  - `ExampleWrap`;
  - `ExampleErrorf`;
  - `ExampleDescribe`;
  - `ExampleBind`;
  - `ExampleSkip`;
  - `ExampleAttr`;
  - `Example_sentinel`.
- **Benchmarks**: those of R-16.

## R-19 Dependencies

- **Decision**: only the standard library: `errors`, `fmt`, `io`, `log/slog`, `path`, `reflect`,
  `runtime`, `strconv` and `strings`. Tests also use `bytes`, `encoding/json`, `os/exec` (for the
  vet test only), `sync` and `testing`.
- **Consequences**:
  - SC-008 holds.
  - Trivy has nothing to scan, and the SBOM lists only the module.
  - Importing `log/slog` links only the parts of slog that the module references. The JSON and
    text handlers are not linked unless the consumer uses them.

## R-20 Documentation (FR-005, Principle I)

- **`doc.go`**:
  - the location model (one location per level, one call stack per chain, sentinels have no
    location), with its limitations (R-07);
  - when to use `Wrap`, `WithStack` or `New`;
  - the detail format and its limits;
  - custom types with `BaseError` and `Bind`;
  - logging with `Attr` and `DetailValue`, with the masking note of R-14;
  - `AsType` instead of `As` (R-02);
  - using the module next to the standard package, with the alias `stderrors`.
- **`README.md`**: purpose, `go get github.com/paccolamano/go/errors`, a minimal example, the
  minimum Go version (1.26), a migration note ("replace the import") and the `%+v` and slog
  notes.
- **`CHANGELOG.md`**: Keep a Changelog, with an `[Unreleased]` section listing the API.

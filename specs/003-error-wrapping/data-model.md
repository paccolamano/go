# Data Model: Error Wrapping and Error Detail Module

**Feature**: [spec.md](spec.md) | **Plan**: [plan.md](plan.md) | **Date**: 2026-10-16

This module is a library and stores nothing persistently. Its data has three parts:

- the internal representation of the errors it creates;
- the exported values that describe an error tree;
- the slog values derived from them.

Every value is immutable once its constructor returns. The exported signatures are listed in
[contracts/public-api.md](contracts/public-api.md).

## Internal representation (unexported)

### Error nodes

| Node | Fields | Created by | Unwrap |
|------|--------|------------|--------|
| `*wrapError` (48 bytes) | `msg string`, `cause error`, `pc uintptr`, `origin *callStack` | `New`, `Errorf` (zero or one `%w`), `Wrap`, `Wrapf`, `WithStack`, and the `Skip` variants of these | `Unwrap() error`, which returns `cause`; nil for `New` and for `Errorf` without `%w` |
| `*joinError` | `msg string`, `errs []error`, `pc uintptr`, `origin *callStack` | `Join`, `Errorf` (two or more `%w`), and the `Skip` variants | `Unwrap() []error`, which returns `errs`; never empty, never contains nil |
| `BaseError` (embedded by developers) | `self error`, `msg string`, `cause error`, `pc uintptr`, `origin *callStack` | `Bind`, `Skip(n).Bind` | `Unwrap() error`, which returns `cause` |

**Message** (`msg`), computed once when the node is built:

| Constructor | `msg` |
|-------------|-------|
| `New(text)` | `text` |
| `Errorf(format, args...)` | `fmt.Errorf(format, args...).Error()` |
| `Wrap(err, text)`, `Wrapf`, `Bind` with a cause | `text + ": " + err.Error()`; `err.Error()` when `text == ""` |
| `WithStack(err)` | `err.Error()` |
| `Join(errs...)` | the messages of the non-nil errors, joined with `"\n"` |
| `Bind` without a cause | `text` |

**Location and call stack**:

- `pc`: the program counter of the location (FR-015). 0 means "no location", which only happens
  for errors created during package initialization (FR-008, research R-07).
- `origin`: non-nil only on an origin (FR-016). It points to a `callStack`:

  | Field | Type | Rule |
  |-------|------|------|
  | `pcs` | `[]uintptr` | 1 to 32 entries, most recent call first. `pcs[0] == pc`. |
  | `truncated` | `bool` | true when the stack was deeper than 32 frames |

**Traced** (research R-06): a node is traced when `pc != 0`. A new node records `origin` only
when `isTraced` reports false for every cause.

**Back-reference** (`BaseError.self`): the outer custom error that `Bind` received. `Format` and
`LogValue` use it, so that the developer's type and any `Error()` override are honored (research
R-13). It is nil in a zero `BaseError` that never went through `Bind`.

### Invariants

1. `Error()` returns `msg`, unchanged for the life of the node (FR-003, SC-001).
2. A wrapping, annotating or combining operation applied to nil returns nil, and creates no node
   (FR-013).
3. The `cause` and `errs` values are the values that were passed in, so identity inspection finds
   them (FR-014).
4. Every chain built only by this library has at most one `origin` per branch. In particular, a
   linear chain has exactly one, unless it was created entirely during package initialization
   (SC-002).
5. A node is never modified after its constructor returns, which makes concurrent use safe
   (FR-003). `Bind` mutates the developer's value only before returning it.

## Exported description values

`Describe(err)` returns a `Detail`. All of these are plain value types with exported fields and
stable JSON names ([contracts/detail-format.md](contracts/detail-format.md)).

### Detail

| Field | JSON | Type | Rule |
|-------|------|------|------|
| `Levels` | `levels` | `[]Level` | The outermost chain, from the outermost level down to the root cause or to the first level with several causes. It is empty and non-nil for a nil error. |

Methods:

- `String() string`: the text layout. It is empty for a nil error and has no trailing newline.

### Level

| Field | JSON | Type | Rule |
|-------|------|------|------|
| `Text` | `text` | `string` | The level's own text (FR-020). It may be empty. It is never the placeholder: `(no message)` exists only in the text layout. |
| `Type` | `type` (omitted when empty) | `string` | `reflect.TypeOf(err).String()`. It is empty for hidden types: the library's own types and the four generic standard types (research R-10). |
| `Location` | `location` (omitted when nil) | `*Location` | nil when the level has no location |
| `Stack` | `stack` (omitted when nil) | `*Stack` | Non-nil only on origins. When an annotation is merged into its cause, the stack is moved to the merged level. |
| `Branches` | `branches` (omitted when empty) | `[][]Level` | For a level with several causes: one chain per cause, in `Unwrap` order. A branch is itself a chain, with the same structure as `Detail.Levels`. |
| `Truncated` | `truncated` (omitted when false) | `bool` | true when some of this level's causes, or its remaining branches, were not rendered because of a limit (FR-025) |

Construction rules:

- **One cause**: the cause becomes the next element of the same chain (slice). It is not nested.
- **Several causes**: they become `Branches`, and the chain ends at that level.
- **Merging an annotation** (research R-10): when a library level has a hidden type, no own text,
  exactly one cause and a location, and that cause has no location, the two become one `Level`.
  The merged level takes `Text`, `Type` and the causes from the cause, and `Location` and `Stack`
  from the annotation.
- **Skipping an annotation**: a library level with a hidden type, no own text, one cause and no
  location is not shown.
- **Limits**: at most 64 levels along any path from the root, and at most 256 `Level` values in
  total (research R-15).
- **Panicking methods**: a level whose `Error` or `Unwrap` method panics has as `Text` the
  placeholder that standard formatting would print, and no causes.

### Location

| Field | JSON | Type | Rule |
|-------|------|------|------|
| `Function` | `function` | `string` | The fully qualified function name, with `%2e` unescaped, for example `example.com/app/api.(*Server).Handle` |
| `File` | `file` | `string` | `<package import path>/<file base name>`, for example `example.com/app/api/handler.go`. It never contains the build directory (FR-018). |
| `Line` | `line` | `int` | ≥ 1, or 0 when the runtime does not know the line |

### Stack

| Field | JSON | Type | Rule |
|-------|------|------|------|
| `Frames` | `frames` | `[]Location` | 1 to 32 entries, most recent call first (FR-021). `Frames[0]` equals the level's `Location`. |
| `Truncated` | `truncated` (omitted when false) | `bool` | true when the call stack was deeper than 32 frames |

## Skip

| Concept | Representation | Rules |
|---------|----------------|-------|
| Skip level | `type Skip int` | Level 0 is the function that calls the library. Negative values count as 0. `Skip(0).X(...)` is identical to the package-level `X(...)`. For `Bind`, the package-level default is level 1 (FR-017, FR-028). |

## slog values

| Value | Produced by | Shape after `Resolve()` |
|-------|-------------|-------------------------|
| Detail attribute | `Attr(err)` | key `"error"`; a group with `message` (string, `err.Error()`) and `detail` (any, the `Detail`) |
| Detail value | `DetailValue(err)` | the same group, without a key |
| No error | `Attr(nil)`, `DetailValue(nil)` | an empty group, which slog's handlers drop |
| Plain view | `LogValue()` of every library error | `slog.AnyValue(plain{err})`: an `error` whose `Error()` is the message and whose `Unwrap()` returns `err`, with no `Format` method |

Resolution is lazy: `Describe` runs only when a handler processes the record (FR-030). The full
shapes are in [contracts/log-attributes.md](contracts/log-attributes.md).

## State transitions

None. Errors and description values are immutable, and the library keeps no state between calls.

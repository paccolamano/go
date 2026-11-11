# Contract: Public Go API (slogx 0.1.0 → 0.2.0)

**Module**: `github.com/paccolamano/go/slogx` | **Packages**: `slogx`, `slogx/slogxtest` |
**Minimum Go**: 1.26 (unchanged)

This file lists the exported surface of `slogx` at 0.2.0, what is removed, and what the release
must report. `slogxtest` and `internal/attrs` do not change. The exact doc-comment wording is
decided at implementation time; the comments below fix what each one must say. Behavior is
specified in [record-semantics.md](record-semantics.md).

## Package `slogx` at 0.2.0

### Unchanged

```go
var ErrInvalidConfig = errors.New("slogx: invalid configuration")
type Option func(*config)

func With(ctx context.Context, attrs ...slog.Attr) context.Context
func WithLevel(ctx context.Context, level slog.Level) context.Context
func WithContextFunc(fn func(ctx context.Context) []slog.Attr) Option
func WithRedactKeys(keys ...string) Option

type Secret[T any] struct{ /* unexported */ }
func NewSecret[T any](v T) Secret[T]
func (s Secret[T]) Reveal() T
func (Secret[T]) LogValue() slog.Value
func (Secret[T]) String() string
func (Secret[T]) Format(f fmt.State, _ rune)
func (Secret[T]) MarshalText() ([]byte, error)

type Handler struct{ /* unexported */ }
func New(next slog.Handler, opts ...Option) (*Handler, error)
func (h *Handler) Enabled(ctx context.Context, level slog.Level) bool
func (h *Handler) Handle(ctx context.Context, r slog.Record) error
func (h *Handler) WithAttrs(attrs []slog.Attr) slog.Handler // returns *Handler
func (h *Handler) WithGroup(name string) slog.Handler       // returns *Handler
```

The signatures do not change. These doc comments change:

- `ErrInvalidConfig`: "wrapped by every error `New` returns" (there is one constructor).
- `Option`: "configures `New`. Every option applies." The sentence on accepted subsets goes.
- `With`, `WithLevel`: name `Handler` instead of `ContextHandler` and `LevelHandler`.
  `WithLevel` adds that the context level wins over the default level too.
- `WithContextFunc`: precedence is among extraction functions and context values, the first
  configured wins. "Accepted by New and NewContextHandler" goes.
- `WithRedactKeys`: "Accepted by New and NewRedactHandler, which requires it" goes.
- `Handler`: the pipeline is the level, the context attributes, the redaction (with
  `WithRedactKeys`), then `next`. No sampling, no standalone handler.
- `New`: the manual-composition example and "Accepted options" go. It states that a `Fanout`
  given as `next` requires `WithDefaultLevel`.
- `Handler.Enabled`: the order of the level decision ([record-semantics.md](record-semantics.md#level-decision-fr-001-to-fr-005)).

### Added (FR-001 to FR-015)

```go
// WithDefaultLevel sets the minimum level of the records logged with a context
// that carries no level (WithLevel). For those records, the Handler compares the
// level with it alone and does not ask the next handler, so the threshold holds
// whatever the next handler enables. The level is read at every record: pass a
// *slog.LevelVar to change it at run time. When given several times, the last
// one applies. A nil level, or a nil *slog.LevelVar, is invalid.
//
// It is required when the next handler is a Fanout.
func WithDefaultLevel(level slog.Leveler) Option

// WithContextValue adds the attribute key, with the value get returns, to every
// enabled record whose context carries it, that is, for which get returns true:
//
//	slogx.WithContextValue("request_id", requestid.FromContext)
//
// The attribute follows the rules of WithContextFunc: it is placed at the top
// level, the record's attributes and those attached with With win over it, and
// among context values and extraction functions the first configured wins. get
// is not called for disabled records, nor when the key is already taken. The
// value is logged as any attribute value: a Secret is written [REDACTED]. A get
// that panics is replaced by the placeholder of extraction functions. An empty
// key, or a nil get, is invalid. Use WithContextFunc for a function that
// returns several attributes.
func WithContextValue[T any](key string, get func(ctx context.Context) (T, bool)) Option

// Fanout returns a handler that passes every record to every output, in order,
// without asking the outputs whether they handle its level: every output
// receives the same records, whatever its own level. Give it to New as the next
// handler, with WithDefaultLevel, which New then requires: the level is decided
// once, by the Handler. New does not recognize a Fanout wrapped in another
// handler.
//
// Each output receives its own copy of the record. An output that fails does
// not stop the next ones: Handle returns the error of a single failing output
// unchanged, and combines several with errors.Join.
//
// Used on its own, a Fanout reports every level as enabled. For deliberately
// different levels per output, use slog.NewMultiHandler instead: each output
// applies its own level, and the per-request level does not go below it.
func Fanout(outputs ...slog.Handler) slog.Handler
```

`Fanout` returns an unexported `*fanout` ([research R-02](../research.md#r-02-api-shape-one-handler-three-additions)).

### Removed (FR-016, FR-018)

| Identifier | Replacement |
|------------|-------------|
| `ContextHandler`, `NewContextHandler` | `New(next, opts...)` |
| `LevelHandler`, `NewLevelHandler` | `New(next)` |
| `RedactHandler`, `NewRedactHandler` | `New(next, WithRedactKeys(keys...))` |
| `SamplingHandler`, `NewSamplingHandler`, `WithSampling`, `WithSampleErrors`, `(*Handler).Flush` | none: delete the options and the calls to `Flush` |

The methods of the removed types go with them.

## Configuration errors

Every error wraps `ErrInvalidConfig` and has the form
`slogx: invalid configuration: New: <subject>: <reason>`. `New` returns the first that applies,
in this order:

| Condition | Subject and reason |
|-----------|--------------------|
| `next` is nil | `next: must not be nil` |
| option `i` is nil | `options[i]: must not be nil` |
| `WithDefaultLevel(nil)`, or a nil `*slog.LevelVar` | `WithDefaultLevel: must not be nil` |
| `WithContextFunc(nil)` | `WithContextFunc: function must not be nil` |
| `WithContextValue` with an empty or blank key | `WithContextValue: empty key` |
| `WithContextValue` with a nil function | `WithContextValue: function must not be nil` |
| `WithRedactKeys()` | `WithRedactKeys: at least one key is required` |
| `WithRedactKeys` with an empty or blank key | `WithRedactKeys: empty key` |
| `next` is a `Fanout` with no output | `Fanout: no output` |
| `next` is a `Fanout` with a nil output | `Fanout: outputs[i]: must not be nil` |
| `next` is a `Fanout` with an invalid nested `Fanout` | `Fanout: outputs[i]: <reason of the nested one>` |
| `next` is a `Fanout`, and no `WithDefaultLevel` | `Fanout: requires WithDefaultLevel` |

Among invalid options, the first one given is reported. The errors `<Option>: does not apply`
and `<Option>: required` no longer exist (FR-017).

## Expected `gorelease` report against `slogx/v0.1.0`

```text
# github.com/paccolamano/go/slogx
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

This is the spike's report ([research R-01](../research.md#r-01-release-level-and-api-compatibility)).
The CHANGELOG lists the same 11 removals and 3 additions (SC-004).

## CHANGELOG entry

Under `## [Unreleased]` in `slogx/CHANGELOG.md` (FR-026). Wording may be polished; the items may
not change.

```markdown
### Removed

- **Breaking**: the standalone handlers `ContextHandler`, `LevelHandler` and `RedactHandler`, and
  their constructors `NewContextHandler`, `NewLevelHandler` and `NewRedactHandler`. `New` with
  the matching option does what each of them did.
- **Breaking**: sampling: `SamplingHandler`, `NewSamplingHandler`, `WithSampling`,
  `WithSampleErrors` and `Handler.Flush`.

### Added

- `WithDefaultLevel`, the minimum level of the records logged with a context that carries no
  level. With it, the next handler is no longer asked whether a level is enabled, so the level
  applies whatever the outputs enable. A `*slog.LevelVar` changes it at run time.
- `Fanout`, which passes every record to several outputs without asking them whether they handle
  its level, so that every output receives the same records. `New` over a `Fanout` requires
  `WithDefaultLevel`.
- `WithContextValue`, which adds one attribute read from the context, such as
  `WithContextValue("request_id", requestid.FromContext)`.

### Changed

- Every option applies to `New`. The configuration errors "does not apply" and "required" no
  longer exist.
- `New` reports invalid configuration for a nil default level, an empty key or a nil function of
  a context value, and a `Fanout` without `WithDefaultLevel`, without output or with a nil
  output.

### Migration from 0.1.0

| 0.1.0 | 0.2.0 |
|-------|-------|
| `NewContextHandler(next, opts...)` | `New(next, opts...)` |
| `NewLevelHandler(next)` | `New(next)` |
| `NewRedactHandler(next, WithRedactKeys(keys...))` | `New(next, WithRedactKeys(keys...))` |
| `NewSamplingHandler`, `WithSampling`, `WithSampleErrors`, `Handler.Flush` | removed: delete the options and the calls to `Flush` |
| a level handler of one's own above `New` | `WithDefaultLevel` |
| an extraction function that returns one attribute | `WithContextValue` |
| `slog.NewMultiHandler` under `New`, meant to give every output the same records | `Fanout`, with `WithDefaultLevel` |
```

## Traceability

| Identifier or change | Requirements |
|----------------------|--------------|
| `WithDefaultLevel` | FR-001 to FR-005 |
| `Fanout` | FR-006 to FR-011 |
| `WithContextValue` | FR-012 to FR-015 |
| removal of the standalone handlers | FR-016 |
| per-option validation, no applicability errors | FR-017 |
| removal of sampling | FR-018 |
| `Handler`, `New`: order of the pipeline | FR-019 |
| output unchanged for 0.1.0 configurations | FR-020 |
| CHANGELOG entry | FR-026 |

# Contract: Public Go API

**Module**: `github.com/paccolamano/go/slogx` | **Packages**: `slogx`, `slogx/slogxtest` |
**Minimum Go**: 1.26

This file lists the complete exported surface of the module at its first release. Anything not
listed here is unexported or under `internal/`. Later releases may only **add** identifiers,
options or fields of `slogxtest.Record` (Principle II). The exact doc-comment wording is decided
at implementation time. Behavior is specified in [record-semantics.md](record-semantics.md).

## Package `slogx`

### Context functions (FR-010, FR-024)

```go
// With returns a context that carries attrs in addition to the attributes of ctx.
// Empty attributes are dropped, groups with an empty key are inlined, and a key
// attached again replaces its earlier value. A nil ctx is treated as context.Background().
func With(ctx context.Context, attrs ...slog.Attr) context.Context

// WithLevel returns a context that carries the minimum level for the records logged
// with it. It overrides the threshold in both directions. A nil ctx is treated as
// context.Background().
func WithLevel(ctx context.Context, level slog.Level) context.Context
```

Both apply only to records logged with methods that take a context (`InfoContext`, `LogAttrs`,
and so on). `logger.Info` uses `context.Background()`.

### Options and configuration errors (FR-007)

```go
// ErrInvalidConfig is wrapped by every error a constructor returns.
var ErrInvalidConfig = errors.New("slogx: invalid configuration")

// Option configures a handler. Each constructor accepts the subset listed below; any
// other option makes it return an error wrapping ErrInvalidConfig.
type Option func(*config)

func WithContextFunc(fn func(ctx context.Context) []slog.Attr) Option // FR-012; repeatable, order = precedence
func WithRedactKeys(keys ...string) Option                            // FR-017; repeatable, keys accumulate
func WithSampling(first, thereafter int, interval time.Duration) Option // FR-028; last call wins
func WithSampleErrors() Option                                        // FR-029; requires WithSampling
```

| Constructor | Accepted | Required |
|-------------|----------|----------|
| `New` | all | none |
| `NewLevelHandler` | none | none |
| `NewContextHandler` | `WithContextFunc` | none |
| `NewRedactHandler` | `WithRedactKeys` | `WithRedactKeys` |
| `NewSamplingHandler` | `WithSampling`, `WithSampleErrors` | `WithSampling` |

The following also return errors wrapping `ErrInvalidConfig`:

- a nil `next` handler or a nil extraction function;
- `WithRedactKeys()` with no key, or with a key that is empty or only whitespace;
- `first < 1`, `thereafter < 1` or `interval <= 0`.

Error messages have the form `slogx: invalid configuration: <Constructor>: <Option>: <reason>`: the message of `ErrInvalidConfig`, then the details, as in the `middleware` module.

### Composed handler (FR-034 to FR-036)

```go
// Handler is the pipeline built by New:
// LevelHandler → SamplingHandler (with WithSampling) → ContextHandler → RedactHandler (with WithRedactKeys) → next.
type Handler struct{ /* unexported */ }

func New(next slog.Handler, opts ...Option) (*Handler, error)

func (h *Handler) Enabled(ctx context.Context, level slog.Level) bool
func (h *Handler) Handle(ctx context.Context, r slog.Record) error
func (h *Handler) WithAttrs(attrs []slog.Attr) slog.Handler // returns *Handler sharing the sampler
func (h *Handler) WithGroup(name string) slog.Handler       // returns *Handler sharing the sampler

// Flush emits the summaries of the current sampling interval and starts a new one.
// It returns nil when sampling is not configured. ctx is passed to the handlers that
// write the summaries: pass a context without request attributes, for example at shutdown.
func (h *Handler) Flush(ctx context.Context) error
```

### Standalone handlers

Every handler implements `slog.Handler`. `WithAttrs` and `WithGroup` return a value of the same
type. `Handle` returns the next handler's error unchanged.

```go
// LevelHandler applies the level attached with WithLevel (FR-024 to FR-026).
type LevelHandler struct{ /* unexported */ }
func NewLevelHandler(next slog.Handler, opts ...Option) (*LevelHandler, error)
func (h *LevelHandler) Enabled(ctx context.Context, level slog.Level) bool
func (h *LevelHandler) Handle(ctx context.Context, r slog.Record) error
func (h *LevelHandler) WithAttrs(attrs []slog.Attr) slog.Handler
func (h *LevelHandler) WithGroup(name string) slog.Handler

// ContextHandler adds context attributes at the top level of every record (FR-011 to FR-016).
type ContextHandler struct{ /* unexported */ }
func NewContextHandler(next slog.Handler, opts ...Option) (*ContextHandler, error)
func (h *ContextHandler) Enabled(ctx context.Context, level slog.Level) bool
func (h *ContextHandler) Handle(ctx context.Context, r slog.Record) error
func (h *ContextHandler) WithAttrs(attrs []slog.Attr) slog.Handler
func (h *ContextHandler) WithGroup(name string) slog.Handler

// RedactHandler replaces the values of sensitive keys with [REDACTED] (FR-017 to FR-022).
type RedactHandler struct{ /* unexported */ }
func NewRedactHandler(next slog.Handler, opts ...Option) (*RedactHandler, error)
func (h *RedactHandler) Enabled(ctx context.Context, level slog.Level) bool
func (h *RedactHandler) Handle(ctx context.Context, r slog.Record) error
func (h *RedactHandler) WithAttrs(attrs []slog.Attr) slog.Handler
func (h *RedactHandler) WithGroup(name string) slog.Handler

// SamplingHandler samples repeated records and summarizes the dropped ones (FR-027 to FR-033).
type SamplingHandler struct{ /* unexported */ }
func NewSamplingHandler(next slog.Handler, opts ...Option) (*SamplingHandler, error)
func (h *SamplingHandler) Enabled(ctx context.Context, level slog.Level) bool
func (h *SamplingHandler) Handle(ctx context.Context, r slog.Record) error
func (h *SamplingHandler) WithAttrs(attrs []slog.Attr) slog.Handler // shares the sampler
func (h *SamplingHandler) WithGroup(name string) slog.Handler       // shares the sampler
func (h *SamplingHandler) Flush(ctx context.Context) error
```

Manual composition equivalent to `New` (FR-036), with every option given:

```go
r, err := slogx.NewRedactHandler(out, slogx.WithRedactKeys(keys...))
c, err := slogx.NewContextHandler(r, slogx.WithContextFunc(fn))
s, err := slogx.NewSamplingHandler(c, slogx.WithSampling(first, thereafter, interval))
l, err := slogx.NewLevelHandler(s)
```

### Secret (FR-023)

```go
// Secret holds a value that renders as [REDACTED] in logs, formatting and encoding.
type Secret[T any] struct{ /* unexported */ }

func NewSecret[T any](v T) Secret[T]
func (s Secret[T]) Reveal() T                          // the wrapped value; zero T for the zero Secret
func (Secret[T]) LogValue() slog.Value                 // slog.StringValue("[REDACTED]")
func (Secret[T]) String() string                       // "[REDACTED]"
func (Secret[T]) Format(f fmt.State, verb rune)        // writes "[REDACTED]" for every verb
func (Secret[T]) MarshalText() ([]byte, error)         // "[REDACTED]"; used by encoding/json
```

## Package `slogx/slogxtest` (FR-037 to FR-041)

```go
// Recorder is a slog.Handler that keeps every record it handles, for tests.
// It is safe for concurrent use; recorders derived with WithAttrs and WithGroup
// share the records of the recorder they derive from.
type Recorder struct{ /* unexported */ }

func NewRecorder(opts ...Option) *Recorder

func (r *Recorder) Enabled(ctx context.Context, level slog.Level) bool
func (r *Recorder) Handle(ctx context.Context, rec slog.Record) error // always nil
func (r *Recorder) WithAttrs(attrs []slog.Attr) slog.Handler
func (r *Recorder) WithGroup(name string) slog.Handler

func (r *Recorder) Records() []Record     // copy, in handling order
func (r *Recorder) Len() int
func (r *Recorder) Last() (Record, bool)  // false when nothing was recorded

// Option configures a Recorder.
type Option func(*Recorder)

// WithLevel sets the minimum enabled level. Without it, or with nil, every level is enabled.
func WithLevel(level slog.Leveler) Option

// Record is a captured record, with its attributes normalized as an output
// handler would write them: logger attributes and groups included, values
// resolved, empty attributes and groups removed.
type Record struct {
	Time    time.Time
	Level   slog.Level
	Message string
	Attrs   []slog.Attr
}

// Attr returns the value at path, one key per group level, and reports whether it exists.
// When a key repeats at one level, the last occurrence wins.
func (r Record) Attr(path ...string) (slog.Value, bool)
```

## Requirement traceability

| Identifier | Requirements |
|------------|--------------|
| `With` | FR-010, FR-013, FR-014 |
| `WithLevel` | FR-024, FR-026 |
| `Option`, `ErrInvalidConfig` | FR-007 |
| `WithContextFunc` | FR-012, FR-014 |
| `WithRedactKeys` | FR-017 |
| `WithSampling`, `WithSampleErrors` | FR-028, FR-029 |
| `Handler`, `New`, `Flush` | FR-030, FR-034 to FR-036 |
| `LevelHandler`, `NewLevelHandler` | FR-024, FR-025 |
| `ContextHandler`, `NewContextHandler` | FR-011 to FR-016 |
| `RedactHandler`, `NewRedactHandler` | FR-017 to FR-022 |
| `SamplingHandler`, `NewSamplingHandler` | FR-027 to FR-033 |
| `Secret`, `NewSecret` and its methods | FR-023 |
| `slogxtest.Recorder` and its methods, `NewRecorder` | FR-037, FR-038, FR-039, FR-041 |
| `slogxtest.Option`, `slogxtest.WithLevel` | FR-040 |
| `slogxtest.Record`, `Record.Attr` | FR-039 |

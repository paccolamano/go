# Data Model: Composable Structured Logging Handlers (slogx)

**Feature**: [spec.md](spec.md) | **Plan**: [plan.md](plan.md) | **Research**: [research.md](research.md)

The module stores no data. It keeps in-memory values in contexts and in handlers. This document
describes those values, their invariants and the state of the sampler. Exported names are those
of [contracts/public-api.md](contracts/public-api.md). Unexported names are indicative.

## Context values

### Context attributes (`ctxAttrs`, unexported)

Stored by `With(ctx, attrs...)` under an unexported key (research R-03).

| Field | Type | Meaning |
|-------|------|---------|
| `attrs` | `[]slog.Attr` | All attributes attached along the chain of contexts, merged |

Invariants:

- No attribute has an empty key, and no group has an empty key: they are inlined when attached.
- Keys are unique. When a key was attached more than once, only the most recent value is kept
  (FR-014).
- The slice is never modified after it is stored, so a parent context never sees attributes
  attached to its children (FR-010).

### Per-request level (unexported key)

Stored by `WithLevel(ctx, level)` as a `slog.Level`. The innermost `WithLevel` along a context
chain wins, because `ctx.Value` returns the nearest value (FR-026).

## Configuration

### `config` (unexported), filled by `Option` values

| Field | Set by | Validation (error wraps `ErrInvalidConfig`) |
|-------|--------|---------------------------------------------|
| `fns []func(context.Context) []slog.Attr` | `WithContextFunc` (appends) | function not nil |
| `keys []string` | `WithRedactKeys` (appends) | at least one key per call; no key empty or only whitespace |
| `first, thereafter int`, `interval time.Duration` | `WithSampling` (last call wins) | `first >= 1`, `thereafter >= 1`, `interval > 0` |
| `sampleErrors bool` | `WithSampleErrors` | requires `WithSampling` |
| `set` (bit set) | every option | used for the applicability check below |

Applicability (research R-02). An option outside the constructor's set, or a missing required
option, is an error:

| Constructor | Accepted options | Required |
|-------------|------------------|----------|
| `New` | all | none |
| `NewLevelHandler` | none | none |
| `NewContextHandler` | `WithContextFunc` | none |
| `NewRedactHandler` | `WithRedactKeys` | `WithRedactKeys` |
| `NewSamplingHandler` | `WithSampling`, `WithSampleErrors` | `WithSampling` |

A nil `next` handler is an error for every constructor.

## Handlers

Every handler is immutable after construction. `WithAttrs` and `WithGroup` return a new value,
and `WithGroup("")` and `WithAttrs` with no attribute return the receiver, as `slog.Handler`
requires. The handlers derived from one sampler share it.

### `LevelHandler`

| Field | Meaning |
|-------|---------|
| `next` | the next handler |

`Enabled`: when the context carries a level, the result is `level >= ctxLevel`. Otherwise the
next handler decides (FR-024).

### `ContextHandler`

| Field | Meaning |
|-------|---------|
| `root` | the next handler with only the attributes added before the first group |
| `next` | `root` with every later `WithGroup` and `WithAttrs` applied |
| `topKeys []string` | keys of the logger attributes added before the first group (FR-015) |
| `ops []op` | operations after the first group: `op{group string}` or `op{attrs []slog.Attr}` |
| `fns` | the extraction functions, in configuration order |

Invariants:

- When `ops` is empty, `root == next`.
- `ops` and `topKeys` are copied on write, so derived handlers never share a backing array they
  could append to.

`Handle` paths (research R-04):

| Condition | Target | Record sent |
|-----------|--------|-------------|
| no context attribute survives | `next` | the original record |
| `len(ops) == 0` | `next` | context attributes not in `topKeys` or in the record's top level, then the record's attributes |
| `len(ops) > 0` | `root` | context attributes not in `topKeys`, then `ops` folded around the record's attributes |

### `RedactHandler`

| Field | Meaning |
|-------|---------|
| `next` | the next handler |
| `keys []string` | the sensitive keys, compared with `strings.EqualFold` |
| `sealed bool` | true after `WithGroup(name)` with a matching `name` |

State transition:

```text
open --WithGroup(matching name)--> sealed   (next.WithAttrs(name=[REDACTED]) at the parent level)
sealed --WithAttrs/WithGroup--> sealed      (receiver returned, content dropped)
sealed --Handle--> record without attributes sent to next
```

### `SamplingHandler` and the shared `sampler`

`SamplingHandler` has the fields `next` (the next handler, derived as usual) and `s *sampler`,
which is shared.

`sampler` (unexported):

| Field | Meaning |
|-------|---------|
| `root slog.Handler` | the next handler as it was at construction; it receives the summaries |
| `first, thereafter int64`, `interval time.Duration`, `sampleErrors bool` | configuration |
| `mu sync.Mutex` | guards every field below |
| `start time.Time` | start of the current interval; zero before the first record |
| `groups map[groupKey]groupCount` | at most 1,024 entries (FR-031) |

`groupKey{level slog.Level, msg string}` and `groupCount{n, dropped int64}`.

Interval state machine (research R-10). Every transition happens under `mu`:

```text
idle (start zero) --first record--> counting (start = now)
counting --record, now - start < interval--> counting (count or pass untracked)
counting --record, now - start >= interval--> counting (summaries collected, groups cleared, start = now)
counting --Flush--> counting (summaries collected with end = now, groups cleared, start = now)
```

Per-record decision, for a sampled level:

- the group's `n` is incremented;
- the record passes when `n <= first` or `(n - first) % thereafter == 0`;
- otherwise `dropped` is incremented.

Invariant (FR-032): for each group and interval, records passed plus `dropped` equals the
records received.

### `Handler` (composed)

| Field | Meaning |
|-------|---------|
| `head` | the outermost handler of the chain in research R-11 |
| `sampler` | the shared sampler, or nil without `WithSampling` |

## Secret

`Secret[T]` has one field, `p *T`, set by `NewSecret`. A nil `p` is the zero value, and
`Reveal` then returns the zero `T`. Every rendering method returns `[REDACTED]` whatever `p` is
(FR-023).

## Test recorder (`slogxtest`)

### `Recorder`

| Field | Meaning |
|-------|---------|
| `s *store` | shared by the recorder and every recorder derived from it: `mu sync.Mutex`, `records []Record` |
| `level slog.Leveler` | minimum level; nil means every level is enabled |
| `ops []op` | the `WithAttrs` (values already resolved) and `WithGroup` operations, in order |

### `Record`

| Field | Type | Meaning |
|-------|------|---------|
| `Time` | `time.Time` | the record's time (zero if the record had none) |
| `Level` | `slog.Level` | the record's level |
| `Message` | `string` | the record's message |
| `Attrs` | `[]slog.Attr` | the normalized attribute tree (below) |

Normalization, applied when a record is captured (research R-13):

1. Logger attributes and groups are folded around the record's attributes, in order.
2. Every value is resolved with `slog.Value.Resolve`.
3. Attributes with an empty key and a non-group value are dropped. Groups with an empty key are
   inlined. Groups that end up with no attribute are dropped.
4. Groups deeper than 64 levels are replaced by the string `!DEPTH`.

`Record.Attr(path...)` walks `Attrs` one key per group level. When a key repeats at one level,
the last occurrence wins.

## Entity relationships

```text
context ──With──▶ ctxAttrs ──read by──▶ ContextHandler.Handle
context ──WithLevel──▶ level ──read by──▶ LevelHandler.Enabled

New ─builds─▶ Handler ─head─▶ LevelHandler ─▶ SamplingHandler ─▶ ContextHandler ─▶ RedactHandler ─▶ output
                    └─sampler─────────────────────▲ (shared by derived handlers; summaries ─▶ its root)

Recorder ─derives─▶ Recorder ... ─all share─▶ store ─holds─▶ []Record
```

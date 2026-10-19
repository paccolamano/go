# Contract: Record Semantics

**Module**: `github.com/paccolamano/go/slogx`

This contract defines what each handler does to a record, as observed in the output of a
standard JSON handler. The examples use JSON with the `time` and `level` keys left out. API
names are those of [public-api.md](public-api.md).

## Conformance (FR-003)

Every handler passes `testing/slogtest` when it wraps a conforming handler. This covers each
standalone handler, `New` and `slogxtest.Recorder`. `SamplingHandler` and `New` pass with a
`first` large enough that the suite drops no record. `ContextHandler` also passes with an
extraction function that always returns an attribute, which exercises the path for open groups.

## Context attributes (FR-010 to FR-016)

Sources, in precedence order:

1. attributes of the record at the top level, and logger attributes added before any group;
2. attributes attached with `With`, where the most recently attached value of a key wins;
3. attributes returned by extraction functions, where the first function configured wins.

A context attribute is written only if no source above it has the same key. Comparison is exact
and applies to the top level only. Duplicates among the record's own attributes are kept.

Placement: context attributes are always at the top level. They come after the logger's
top-level attributes and before the record's attributes and any open group.

| Logging call (context has `tenant=t1`; an extraction function returns `request_id=abc`) | Output attributes |
|---|---|
| `logger.InfoContext(ctx, "m", "rows", 3)` | `{"msg":"m","tenant":"t1","request_id":"abc","rows":3}` |
| `logger.WithGroup("db").With("table","orders").InfoContext(ctx, "q", "rows", 3)` | `{"msg":"q","tenant":"t1","request_id":"abc","db":{"table":"orders","rows":3}}` |
| `logger.InfoContext(ctx, "m", "request_id", "explicit")` | `{"msg":"m","tenant":"t1","request_id":"explicit"}` |
| `logger.With("request_id","w").InfoContext(ctx, "m")` | `{"msg":"m","request_id":"w","tenant":"t1"}` |
| `logger.WithGroup("db").InfoContext(ctx, "m", "request_id", "x")` | `{"msg":"m","tenant":"t1","request_id":"abc","db":{"request_id":"x"}}` |
| `logger.Info("m")` (no context) | `{"msg":"m"}`, plus the extraction functions' output for `context.Background()` |

Extraction functions run only on `Handle`, never on `Enabled`. In their result, groups with an
empty key are inlined and attributes with an empty key are dropped.

## Redaction (FR-017 to FR-022)

- **Marker**: `[REDACTED]`, a string value. The key is kept.
- **Match**: `strings.EqualFold(attributeKey, configuredKey)`, on the whole key and at every
  depth. `password_hint` does not match `password`.
- **Sources covered**:
  - record attributes;
  - logger attributes (`With`);
  - context attributes, when composed by `New`;
  - attributes inside groups;
  - attributes inside the group a `LogValuer` resolves to.
- **Resolution order**: the key is checked first. A matching attribute's value is not resolved,
  so its `LogValue` is never called. Otherwise the value is resolved, then its group children
  are checked.
- **Groups**: a group attribute with a matching key becomes `key=[REDACTED]`.
  `logger.WithGroup(matching)` writes `matching=[REDACTED]` at the parent level on every record.
  It drops all the content of that group, both the logger's and the record's.
- **Not covered**: the message, and values that the output handler encodes on its own (structs,
  maps, `error` text). Use `Secret` for those.

| Keys `token`, `authorization` | Output |
|---|---|
| `"token","s3cr3t"` | `"token":"[REDACTED]"` |
| `slog.Group("header","Authorization","Bearer x","Accept","*/*")` | `"header":{"Authorization":"[REDACTED]","Accept":"*/*"}` |
| `"user", userValuer{name:"bob", token:"s"}` (resolves to a group) | `"user":{"name":"bob","token":"[REDACTED]"}` |
| `logger.WithGroup("TOKEN").Info("m","a",1)` | `{"msg":"m","TOKEN":"[REDACTED]"}` |

## Secret (FR-023)

`Secret[T]` renders as `[REDACTED]`:

- as a log value, through any handler;
- with every `fmt` verb, as a direct argument or as an exported struct field;
- through `encoding/json` and other `encoding.TextMarshaler` consumers, including as a struct
  field.

As an unexported struct field printed by `fmt`, it shows only a pointer address. `Reveal` is the
only way to read the value.

## Per-request level (FR-024 to FR-026)

| Context | Output handler threshold | Record level | Written? |
|---------|--------------------------|--------------|----------|
| no level | Info | Debug | no |
| `WithLevel(ctx, Debug)` | Info | Debug | yes |
| `WithLevel(WithLevel(ctx, Debug), Warn)` | Info | Info | no (innermost wins) |
| `WithLevel(ctx, Debug)`, logged with `logger.Debug` (no context) | Info | Debug | no |

## Sampling (FR-027 to FR-033)

- **Group**: `(level, message)`. Attributes, including logger attributes, are ignored. All the
  handlers derived from one sampler share the counts.
- **Interval**: a tumbling window that starts at the first record and restarts when a record
  arrives after it has ended. It is measured with the monotonic clock.
- **Rule**: for the n-th record of a group within an interval, the record is written if
  `n <= first` or `(n - first) % thereafter == 0`. Otherwise it is dropped and counted. With T
  records, `first + ⌊(T − first) / thereafter⌋` are written when `T > first`.
- **Errors**: records at `slog.LevelError` and above are always written and not counted, unless
  `WithSampleErrors()` is given.
- **Bound**: at most 1,024 groups are tracked per interval. A record of an untracked group is
  written.

### Sampling summary

One summary per group with drops is emitted when the interval ends. It is emitted by the first
record handled after that, by the sampler or any handler derived from it, or by `Flush`.

```json
{"time":"<emission time>","level":"WARN","msg":"records dropped by sampling",
 "sampling":{"msg":"db query failed","dropped":9891,"start":"<interval start>","end":"<interval end, or flush time>"}}
```

- The level is the group's level.
- Summaries are emitted in order of message, then level.
- Each summary is sent to the sampler's next handler as it was at construction, so it carries no
  logger attribute and no open group. On automatic emission the context is
  `context.Background()`, so no request attribute is added. `Flush(ctx)` passes `ctx` instead.
- Under `New`, summaries still go through context extraction (with a background context) and
  redaction.
- Summaries are never sampled. When the next handler returns an error for a summary, the error
  is joined to the result of the `Handle` or `Flush` call that emitted it.

## Placeholders (FR-005)

| Situation | Output |
|-----------|--------|
| A `LogValue` method panics | slog's placeholder from `slog.Value.Resolve`: an error value whose text starts with `LogValue panicked` |
| An extraction function panics | the attribute `"!CONTEXTFUNC":"panic: <value>"` in place of its attributes |
| A group is nested deeper than 64 levels (redaction, recorder) | the deeper value becomes the string `!DEPTH` |
| A nil context | handled as a context with no attribute and no level |

## Composition order (FR-035)

```text
LevelHandler → SamplingHandler → ContextHandler → RedactHandler → output
```

- **Level outermost**: `Enabled` honors `WithLevel` before the output handler's threshold.
- **Sampling before context**: dropped records cost no extraction and no redaction, and
  summaries carry no request attribute.
- **Context before redaction**: context attributes are redacted.

## Disabled records (FR-004)

When `Enabled` returns false, no handler of the module allocates memory, whether the context
carries attributes, a level or both.

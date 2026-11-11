# Contract: Record Semantics (slogx 0.2.0)

**Module**: `github.com/paccolamano/go/slogx`

This contract states what a record looks like, and where it goes, after this feature. Sections
not listed here keep the text of the
[record semantics of spec 006](../../006-slogx-handlers/contracts/record-semantics.md): context
attributes, redaction, `Secret` and placeholders. Its "Sampling" section and its sampling
summary no longer apply.

## Level decision (FR-001 to FR-005)

For a record at level `L`, logged with context `ctx`, `Handler.Enabled` returns:

1. `L >= C` when `ctx` carries the level `C` (`WithLevel`, the innermost one);
2. otherwise `L >= D.Level()` when the default level `D` is given (`WithDefaultLevel`), read at
   this call;
3. otherwise what the next handler's `Enabled` returns.

Consequences:

- The per-request level wins over the default level, below it and above it.
- With a default level, the next handler's `Enabled` is never called.
- No handler of the module checks the level after `Enabled`. A record enabled by the per-request
  level or by the default level reaches the output, whatever the output's own threshold.
- A default level held in a `*slog.LevelVar` applies its new value from the next call to
  `Enabled`.

| Default | Context level | Records written |
|---------|---------------|-----------------|
| Info | none | Info and above |
| Info | Debug | Debug and above |
| Info | Warn | Warn and above |
| none | none | what the next handler enables (0.1.0) |

## Fanout (FR-006 to FR-011)

- Every record that the fanout receives goes to every output, in the order given, each with its
  own copy of the record. No output's `Enabled` is called.
- Attributes added with `logger.With` and groups opened with `logger.WithGroup` reach every
  output.
- Context attributes and redaction are applied once, before the fanout: every output receives
  the same attributes, with the context attributes at the top level.
- An output that fails does not stop the next ones. `Handle` returns nil when none fails, the
  error unchanged when one fails, and `errors.Join` of the errors, in output order, when several
  fail.
- Used on its own, the fanout reports every level as enabled.
- The same output given twice receives each record twice. A fanout inside a fanout delivers each
  record once per occurrence of an output.
- A `New` used as one output adds its own context attributes to that output only. A key already
  present at the top level of the record it receives is not added again.

| Setup | Debug record with a context level Debug, default level Info | Info record, no context level |
|-------|------------------------------------------------------------|-------------------------------|
| `New(Fanout(stdout(nil options), otel), WithDefaultLevel(Info))` | stdout and otel | stdout and otel |
| `New(slog.NewMultiHandler(stdout(nil options), otel), WithDefaultLevel(Info))` | otel only | stdout and otel |

The second row is the behavior of the standard fan-out, which the documentation recommends only
for deliberately different thresholds per output.

## Context values (FR-012 to FR-015)

A context value configured as `WithContextValue(key, get)` adds, to an enabled record:

| Situation | Result |
|-----------|--------|
| `get` returns `(v, true)` | `key=v` at the top level, `v` logged as any attribute value |
| `get` returns `(_, false)` | nothing |
| `get` returns a zero value with true | `key=<zero value>` |
| the record has `key` at the top level | the record's value only; `get` is not called |
| a logger attribute added before any group has `key` | the logger's value only; `get` is not called |
| `key` is attached with `With` | the attached value only; `get` is not called |
| an earlier extraction function or context value produced `key` | the earlier value only; `get` is not called |
| `v` is a `Secret`, or `key` is a sensitive key | `key=[REDACTED]` |
| `get` panics | `!CONTEXTFUNC="panic: <value>"`, and the record is written |
| the record is disabled | nothing; `get` is not called, and nothing is allocated |

The attribute is placed among the context attributes in configuration order: after the attached
attributes, together with the results of the extraction functions, before the record's own
attributes.

## Composition order (FR-019)

```text
Handler (level) → context attributes → redaction (with WithRedactKeys) → next
```

`next` may be an output, `slog.NewMultiHandler`, or a `Fanout`. The reasons for the order are
those of spec 006, without sampling: the level is decided first, because `slog.Logger` asks only
the outermost handler; redaction comes after the context attributes, so that they are redacted
too.

## Disabled records (FR-021)

A disabled record allocates nothing, with any combination of a default level, context values,
sensitive keys and a fanout. No extraction function and no context value function is called for
it.

## Unchanged output (FR-020)

For a configuration that uses neither sampling nor `WithDefaultLevel`, `WithContextValue` and
`Fanout`, `New` writes the same bytes as `slogx/v0.1.0` for the same records. Former standalone
uses map to `New` with the matching option (see the migration table in
[public-api.md](public-api.md#changelog-entry)), and produce the same output for records whose
context carries no attribute and no level.

## Documentation statements (FR-011, FR-023)

The package documentation and the README state that:

- the fanout ignores the levels of its outputs, and requires a default level;
- the fanout must be given to `New` as the next handler directly, or it is not recognized;
- `slog.NewMultiHandler` is the tool for deliberately different thresholds per output: each
  output applies its own level, and the per-request level does not go below it;
- a `New` nested inside a fanout is not asked about levels, so its own level settings have no
  effect.

Neither mentions sampling, `Flush` or the removed standalone handlers.

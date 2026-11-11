# Data Model: A Simpler slogx, With a Threshold of Its Own (slogx 0.2.0)

**Feature**: [spec.md](spec.md) | **Plan**: [plan.md](plan.md) | **Date**: 2026-11-10

This feature changes the module's state in four places: the configuration, the `Handler`, the
sources of context attributes, and the new fanout. The context values (`With`, `WithLevel`), the
redaction handler, `Secret` and the test recorder do not change: see the
[data model of spec 006](../006-slogx-handlers/data-model.md). Every type below is immutable once
built, and safe for concurrent use.

## 1. Configuration (`config`, unexported)

Filled by the `Option` values given to `New`, in order. It lives only while `New` runs.

| Field | Type | Set by | Rule |
|-------|------|--------|------|
| `sources` | `[]source` | `WithContextFunc`, `WithContextValue` | appended in call order; order is precedence (§3) |
| `keys` | `[]string` | `WithRedactKeys` | accumulated over calls |
| `level` | `slog.Leveler` | `WithDefaultLevel` | the last valid call wins; nil means "not given" |
| `err` | `error` | any option | the first invalid argument; later problems are not recorded |

**Validation** (each option checks its own arguments, [research R-03](research.md#r-03-options-and-configuration-errors-fr-005-fr-007-fr-014-fr-017)):

| Option | Invalid when |
|--------|--------------|
| `WithDefaultLevel(l)` | `l` is nil, or a nil `*slog.LevelVar` |
| `WithContextFunc(fn)` | `fn` is nil |
| `WithContextValue(key, get)` | `key` is empty or only whitespace; `get` is nil |
| `WithRedactKeys(keys...)` | no key; a key empty or only whitespace |
| any | the `Option` itself is nil (`options[i]`) |

Removed from 0.1.0: the option set (`optionSet`), the applicability and required-option checks,
the `noKeys` flag, and the sampling fields (`first`, `thereafter`, `interval`, `sampleErrors`).

## 2. `Handler`

```text
Handler
├── level  slog.Leveler   // the default level, nil without WithDefaultLevel
└── next   slog.Handler   // contextHandler → [redactHandler →] next given to New
```

`WithAttrs` and `WithGroup` return a new `*Handler` with the same `level` and the derived `next`.
They return the receiver for no attribute or an empty group name, as in 0.1.0. The `sampler`
field is removed.

### Level decision (`Enabled`, FR-001 to FR-004)

| Context carries a level (`WithLevel`) | Default level given | Enabled when |
|---------------------------------------|---------------------|--------------|
| yes | either | `level >= context level` |
| no | yes | `level >= default.Level()`, read at this call |
| no | no | `next.Enabled(ctx, level)` (0.1.0 behavior) |

A nil context carries no level. Nothing below `Handler` checks the level again: the context
handler, the redaction handler and the fanout pass every record they receive.

### Construction (`New`)

```text
New(next, opts...)
  next == nil                         → ErrInvalidConfig ("next")
  apply opts in order                 → ErrInvalidConfig (nil option, or the first config.err)
  next is a *fanout:
    fanout.invalid != ""              → ErrInvalidConfig ("Fanout: <reason>")
    config.level == nil               → ErrInvalidConfig ("Fanout: requires WithDefaultLevel")
  build: h = next
         h = redactHandler{h, keys}   when keys are given
         h = contextHandler{root: h, next: h, sources}
         return &Handler{level, h}
```

## 3. Sources of context attributes (`source`, unexported)

Held in order by `contextHandler`, which replaces its former list of functions with them.

| Kind | Fields | Produces |
|------|--------|----------|
| extraction function | `fn func(context.Context) []slog.Attr` | the attributes `fn` returns, empty ones dropped, empty-key groups inlined |
| context value | `key string`, `get func(context.Context) (slog.Value, bool)` | `key=value` when `get` reports true; nothing otherwise |

`WithContextValue[T]` builds `get` from the caller's function: it calls it, and converts the value
with `slog.AnyValue`.

**Per enabled record**, after the attached attributes, in source order
([research R-07](research.md#r-07-context-values-fr-012-to-fr-015)):

1. A key is *taken* when the record has it at the top level, a logger attribute added before any
   group has it, an attached attribute has it, or an earlier source added it.
2. An extraction function is always called. Each attribute it returns is added unless its key is
   taken.
3. A context value whose key is taken is skipped without calling `get`. Otherwise `get` is
   called, and its attribute is added when `get` reports true.
4. A function that panics adds `!CONTEXTFUNC="panic: <value>"` instead, unless that key is
   taken.

No source is called for a disabled record, because `Handler.Enabled` returned false and
`slog.Logger` never called `Handle`.

## 4. Fanout (`fanout`, unexported; built by `Fanout`)

```text
fanout
├── outputs []slog.Handler   // a copy of the arguments, in order; may hold nil when invalid
└── invalid string           // why New refuses this fanout, or "" when valid
```

### Validity, computed by `Fanout`

| Arguments | `invalid` |
|-----------|-----------|
| none | `no output` |
| `outputs[i]` is nil (first such `i`) | `outputs[i]: must not be nil` |
| `outputs[i]` is an invalid fanout (first such `i`) | `outputs[i]: <its reason>` |
| otherwise | empty |

`WithAttrs` and `WithGroup` return a new `*fanout` that keeps `invalid` and applies the
attributes or the group to each non-nil output. They return the receiver for no attribute or an
empty group name.

### Behavior

| Method | Result |
|--------|--------|
| `Enabled` | always true (FR-010) |
| `Handle` | for each non-nil output, in order: `output.Handle(ctx, r.Clone())`, never `Enabled` |

**Errors returned by `Handle`** (FR-009):

| Outputs that fail | Returned |
|-------------------|----------|
| none | nil |
| one | that error, unchanged |
| several | `errors.Join` of them, in output order |

Every output is called, whatever the earlier ones returned.

## 5. Removed state

| 0.1.0 | Fate |
|-------|------|
| `LevelHandler{next}` | its `Enabled` moves into `Handler.Enabled` |
| `ContextHandler` | unexported as `contextHandler`; `fns` becomes `sources` |
| `RedactHandler` | unexported as `redactHandler`, unchanged |
| `SamplingHandler`, `sampler`, `groupKey`, `groupCount`, summaries | removed |
| `Handler.sampler` | removed |

## 6. Relationships

```text
slog.Logger ──Enabled/Handle──▶ Handler (level decision)
                                   │
                                   ▼
                             contextHandler (attached attributes, then sources)
                                   │
                                   ▼
                             redactHandler (with WithRedactKeys)
                                   │
                                   ▼
                     next ── an output, slog.NewMultiHandler, or fanout
                                                                  │
                                         ┌────────────────────────┼──────────────┐
                                         ▼                        ▼              ▼
                                    output 1                 output 2   …   (a nested New,
                                                                            a nested fanout)
```

- The context handler keeps two views of `next`: the root, before the first group, and the view
  with every group. A fanout gives both, because it derives each of its outputs.
- A fanout is recognized only as `next` itself. Behind another handler, it is an ordinary
  handler, and its `Enabled` is true.

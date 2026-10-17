# Contract: slog Attributes

**Requirement**: FR-030 | **Research**: [../research.md R-14](../research.md#r-14-slog-integration-fr-030)

## Detail attribute

`errors.Attr(err)` adds the key `error`. `slog.Any(key, errors.DetailValue(err))` uses a key the
developer chooses. After the handler resolves the value, it is a group:

| Attribute | Kind | Value |
|-----------|------|-------|
| `message` | string | `err.Error()` |
| `detail` | any | `errors.Describe(err)`, a `Detail` |

The group is resolved lazily. If the record is not emitted, for example because its level is
disabled, neither `Error()` nor `Describe` is called.

**JSON handler**:

```json
{"time":"…","level":"ERROR","msg":"request failed",
 "error":{"message":"handle request: load tenant 42: open config: file does not exist",
          "detail":{"levels":[{"text":"handle request","location":{…}}, …]}}}
```

**Text handler**: `detail` prints its text layout as a quoted string, because `Detail`
implements `fmt.Stringer`:

```text
time=… level=ERROR msg="request failed" error.message="handle request: …" error.detail="handle request\n    at …"
```

**No error**: `Attr(nil)` and `DetailValue(nil)` are empty groups, which every built-in handler
drops. The record is unchanged.

## Plain values

An error logged without the helper, as in `logger.Error("x", "err", err)`, must look exactly like
a standard error with the same message. Every library error implements `slog.LogValuer` and
resolves to a plain view:

| Handler | Output for a library error | Output for `stderrors.New` with the same message |
|---------|----------------------------|--------------------------------------------------|
| JSON | `"err":"ctx: root"` | `"err":"ctx: root"` |
| Text | `err="ctx: root"` | `err="ctx: root"` |
| default (`slog.Info` through the `log` package) | `err="ctx: root"` | `err="ctx: root"` |

The plain view is an `error`: `Error()` returns the message, and `Unwrap()` returns the original
error, so `errors.Is` and `errors.As` on the resolved attribute value still reach every cause.
It has no `Format` method, so handlers that print values with `%+v` print only the message.

A custom handler that type-asserts a resolved value directly to a concrete error type must use
`errors.As` instead (research R-14, Consequences).

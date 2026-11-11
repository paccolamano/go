# Changelog

All notable changes to this module are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this module adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [0.2.0] - 2026-11-11

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

## [0.1.0] - 2026-10-19

### Added

- `With` and `ContextHandler`, which add the attributes attached to a context to every record
  logged with it, at the top level. The record's own attributes win on duplicate keys.
- `WithContextFunc`, to add attributes computed from the context, such as a request identifier
  stored by another package.
- `RedactHandler` and `WithRedactKeys`, which write `[REDACTED]` as the value of sensitive keys,
  without regard to case and at every depth, including logger attributes, context attributes
  and `LogValuer` groups.
- `Secret`, a value that renders as `[REDACTED]` in logs, with `fmt` and in JSON.
- `WithLevel` and `LevelHandler`, a minimum level for the records of one context.
- `SamplingHandler`, `WithSampling`, `WithSampleErrors` and `Flush`, which sample repeated
  records by level and message and summarize the dropped ones, with exact counts and bounded
  memory.
- `New`, which composes the handlers in the correct order, and `ErrInvalidConfig`.
- `slogxtest.Recorder`, a handler that captures records for tests.

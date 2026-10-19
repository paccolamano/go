# Changelog

All notable changes to this module are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this module adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

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

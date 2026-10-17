# Changelog

All notable changes to this module are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this module adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- Drop-in parity with the standard `errors` package: `Is`, `As`, `AsType`, `Unwrap` and
  `ErrUnsupported`, the standard library's own value.
- `New`, `Errorf`, `Wrap`, `Wrapf`, `WithStack` and `Join`, with messages identical to the
  standard library's. Each records the location of its caller, and the first error of a chain
  also records its call stack.
- Errors created while packages are initialized, such as sentinel errors, record no location.
- `Skip`, to attribute locations to the caller of a helper function.
- `BaseError` and `Bind`, to define custom error types with a message, a cause, a location and
  a detail.
- `Describe`, with `Detail.String()` and the `%+v` verb for text and stable JSON field names for
  data.
- `Attr` and `DetailValue`, which log the message and the detail of an error through `slog`.
  Errors logged as plain values appear like standard library errors.

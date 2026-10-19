# Changelog

All notable changes to this module are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this module adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- `(*Error).With`, which derives a problem with more members, for example a request ID added
  when the problem is written. The original is not modified.
- `Item`, `WithErrors` and `(*Error).Errors`, which write and read the validation errors of
  RFC 9457 §3 in the `errors` extension member.
- `Pointer`, which builds a JSON Pointer (RFC 6901) in URI fragment form, escaping the
  reference tokens.

### Fixed

- `Write` removes the headers that describe the body the handler meant to send:
  `Content-Length`, `Content-Encoding`, `Content-Range`, `ETag`, `Last-Modified`,
  `Content-Disposition` and `Trailer`. **Behavior change**: `Write` used to keep these headers,
  and they contradicted the problem document. For example, a leftover `Content-Length`
  truncated the response, and a leftover `Content-Encoding: gzip` made clients fail to
  decompress plain JSON. Every other header is still kept.
- `Key.Lookup` returns the zero value and false for a nil `*Error`, instead of panicking.

## [0.1.0] - 2026-10-19

### Added

- `NewType`, `NewKey` and `Must`, to declare problem types and extension members. Type URIs,
  titles, status codes and extension names are checked against RFC 9457 when declared.
- Occurrences built with `Type.New` and `Status`, and the options `WithDetail`, `WithTitle`,
  `WithInstance`, `WithExtension` and `WithCause`.
- `Write`, which answers a request with any error as an `application/problem+json` document.
  Errors that are not problems, wrapping context and internal causes never reach the client:
  they are answered with the generic server error.
- `*Error` is a Go error, with its internal cause in the error tree, and `Type.Match` finds a
  problem of a given type anywhere in a tree.
- `Read` and `Parse`, which read problem documents with the RFC's rules for consumers, resolve
  relative URIs and read at most 1 MiB. `Key.Lookup` reads extension values with their Go type.

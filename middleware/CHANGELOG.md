# Changelog

All notable changes to this module are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this module adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- `middleware`: immutable `Chain` (`New`, `Append`, `Extend`, `Then`), the `Middleware`
  alias, the refusal standard (`Rejection`, `RejectFunc`) and `ErrInvalidConfig`.
- `accesslog`: one structured `slog` record per request, with optional query, headers,
  custom attributes, per-request level and skipping.
- `bodylimit`: 413 for declared oversized bodies, `*http.MaxBytesError` for undeclared
  ones.
- `compress`: gzip and deflate response compression with negotiation, minimum size,
  compressible types, streaming flushes, ETag weakening and pluggable codings.
- `contenttype`: 415 for request bodies with a missing or disallowed media type.
- `cors`: Fetch-compliant CORS with exact, subdomain-pattern, `null` and function-based
  origins, preflight handling and credentials.
- `decompress`: gzip and deflate request decoding with a decoded-size cap, lazy
  decoding errors (`ErrInvalidBody`) and pluggable decoders.
- `etag`: SHA-256 entity tags for buffered 200 responses and `If-None-Match` handling
  with 304.
- `maxconcurrency`: in-flight request limit with optional bounded waiting queue and
  `Retry-After`.
- `ratelimit`: per-client GCRA rate limiting (IPv6 by /64, custom keys) with
  `Retry-After` and lazy reclamation of idle clients.
- `realip`: client address resolution behind trusted proxies (ranges or hop count),
  supporting X-Forwarded-For, Forwarded and single-address headers.
- `recovery`: panic recovery with a 500 response or an aborted stream, structured
  logging and an optional reporter.
- `requestid`: request identifier generation, validation and propagation
  (`FromContext`).
- `secureheaders`: nine default security headers, HSTS, content security policy with
  report-only mode and per-request nonces (`NonceFromContext`).

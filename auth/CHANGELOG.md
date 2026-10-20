# Changelog

All notable changes to this module are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this module adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- `auth`: `Authenticator`, `Challenger`, `Principal`, `Pending` and `Store`; `Any`, which tries
  methods in order; `New`, the middleware that answers 401 with the methods' challenges, 429 and
  500, with `WithAnonymous` and `WithRejectFunc`; `RequireMethods`, with the RFC 9470 step-up
  challenge for tokens and 403 for other methods; `FromContext`; the sentinel errors
  `ErrNoCredentials`, `ErrInvalidCredentials`, `ErrTooManyAttempts`, `ErrInsufficientMethods`,
  `ErrNotFound` and `ErrInvalidConfig`.
- `memstore`: an in-memory `auth.Store` with an atomic `Incr` and no goroutine.
- `password`: `Hash` and `Checker` with argon2id in PHC format, verification of legacy bcrypt
  hashes, a dummy hash for unknown users, `WithParams`, `WithMaxLength`, `WithRehash` and the
  per-username `WithAttemptLimit`.
- `session`: `Manager` with `Issue`, `Authenticate`, `Revoke`, `IssuePending` and `Pending`;
  `__Host-` cookies, hashed identifiers, absolute and idle expiry, and the shared five-attempt
  second-factor budget.
- `totp`: `Verifier` with `Verify`, `Confirm` and `Enrolled` (RFC 6238, replay protection), and
  `NewEnrollment` with its `otpauth://` URI.
- `recovery`: `Generate` and `Codes` (single-use 80-bit codes stored as hashes), and `Verifier`.
- `basic`: HTTP Basic authentication (RFC 7617) on any `Checker`, with `WithRealm`.
- `apikey`: `Keys` with `Generate` and `Authenticate` for `<prefix>_<id>_<secret>` keys stored as
  SHA-256 hashes, and `WithHeader`.
- `jwt`: `Verifier` for tokens signed with RS256, PS256, ES256, EdDSA or HS256, with algorithms
  bound to keys, static keys or a cached and rate-limited JWKS, `WithType`, and `Claims`.

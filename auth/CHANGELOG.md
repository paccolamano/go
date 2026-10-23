# Changelog

All notable changes to this module are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this module adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [0.3.0] - 2026-10-23

### Added

- `session`: `Manager.RevokeSubject` revokes every session and pending login of a subject, on
  every instance, for example after a password change, when an account is disabled, or to log
  out of every device. A request authenticated with a session now reads the store twice: the
  session, and its subject's revocation state. Issuing a session or a pending login, and
  reading a pending login, also read the store once more. Sessions issued by 0.2.0 stay valid
  until their subject is revoked. 0.2.0 reads the sessions that this version writes, but does
  not check revocations.

## [0.2.0] - 2026-10-23

### Changed

- **Breaking**: `auth.Store` gains `Replace`, which stores a value only over a key that exists,
  checking and writing in one atomic step, and reports whether it stored it. Store adapters
  must implement it; with Redis it is one command, `SET` with `XX`, shown in the README.
  `memstore` implements it.

### Fixed

- `session`: a session deleted by `Revoke`, or by `Issue` at a new login, while a request was
  renewing it was written back to the store and accepted again, for example from a stolen
  cookie after the user logged out. Renewal now uses `Store.Replace`, and the request that was
  renewing the deleted session gets `auth.ErrNoCredentials`.

## [0.1.0] - 2026-10-20

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

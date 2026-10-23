# Contract: Public API (auth/v0.2.0 → v0.3.0)

**Spec**: [../spec.md](../spec.md) | **Store contract**: [store.md](store.md) | **Research**: [../research.md](../research.md) R-01 to R-11

This feature adds one exported method and changes doc comments. No signature changes, and no
identifier is removed or renamed.

## Package `session`

### `(*Manager).RevokeSubject` (new: compatible)

```go
// RevokeSubject revokes every session and pending login of subject, on every
// instance that shares the store: for example after a password change, when
// an account is disabled, or to log out of every device. Once it returns,
// requests that carry them get auth.ErrNoCredentials. Sessions and pending
// logins issued afterwards are not affected, so a handler that changes a
// password can call RevokeSubject and then Issue to keep its user signed in.
// RevokeSubject clears no cookie.
//
// A session issued after RevokeSubject returns is valid even when its
// credentials were checked before: call Issue right after checking them.
//
// It returns an error for an empty subject and for a store failure. Calling
// it again is harmless.
func (m *Manager) RevokeSubject(ctx context.Context, subject string) error
```

Errors:

| Cause | Error |
|-------|-------|
| `subject == ""` | `session: revoke subject: the subject is empty`; no store call |
| the store's `Set` fails, or `ctx` is cancelled | `session: revoke subject: <store error>`, matching it with `errors.Is` |

| Requirement | Covered by |
|-------------|------------|
| FR-001 | the signature: a context and a subject, no request |
| FR-002, FR-003, FR-004 | "every session and pending login", "issued afterwards are not affected" |
| FR-006 | the second paragraph |
| FR-008 | the errors, and "calling it again is harmless" |

### Package documentation (`session.go`; changed)

```go
// Package session keeps signed-in users in server-side sessions carried by a
// __Host- cookie.
//
// Session identifiers have 256 bits from crypto/rand; the store holds only
// their SHA-256 hash. A session expires after an absolute lifetime and after an
// idle timeout; activity renews it at most once every quarter of the idle
// timeout, so most requests cost two store reads, the session and the
// revocation state of its subject, and no write. Issue always creates a new
// identifier and deletes the session and pending login the request carried,
// which prevents session fixation. RevokeSubject ends every session and
// pending login of a subject, for example after a password change.
//
// Cookie sessions need protection against cross-site request forgery: wrap the
// routes that change state with http.CrossOriginProtection.
```

### `(*Manager).Authenticate` (doc comment changed; signature unchanged)

```go
// Authenticate authenticates r with its session cookie, reading only the
// store: the session, and the revocation state of its subject. A missing
// cookie, or one whose session is malformed, unknown, expired or revoked, by
// Revoke, Issue or RevokeSubject, returns auth.ErrNoCredentials, also when the
// session is deleted while this request renews it. A store failure returns an
// error.
```

### `(*Manager).Issue` (doc comment changed; signature unchanged)

```go
// Issue starts a session for p. It deletes the session and the pending login
// the request carried, writes a new session under a new identifier, sets the
// session cookie and clears the pending cookie. The session records the
// revocation state of p.Subject: a RevokeSubject that runs while Issue writes
// it revokes it. On a store failure it returns an error and sets no cookie.
```

### `(*Manager).IssuePending` (doc comment changed; signature unchanged)

```go
// IssuePending starts a pending login for p, a principal that passed its first
// factor and must pass a second one. The pending login has its own cookie,
// lives for the pending lifetime and never authenticates a request. It
// replaces any pending login the request carried, and, like Issue, records
// the revocation state of p.Subject.
```

### `(*Manager).Pending` (doc comment changed; signature unchanged)

```go
// Pending returns the pending login of r. It returns auth.ErrNoCredentials
// when the request carries none, or when it expired, was cancelled, or its
// subject was revoked after it was issued. A store failure returns an error.
```

## Package `auth`

### `Store` (doc comment changed; interface unchanged)

The first sentence names revocations among the library's state:

```go
// Store is a key-value store with expiry that holds the library's short-lived
// state: sessions, pending logins, revocations and counters. The library
// chooses every key and never stores a secret in clear.
```

The rest of the comment, and the five methods, do not change.

### Package documentation (`doc.go`, "# Store"; changed)

```go
// The Store holds sessions, pending logins, revocations and counters, for
// example in Redis. The library chooses every key, derives the keys of secrets
// from their SHA-256 hash, and never writes a secret in clear. Store.Incr must
// be atomic and must set the expiry only when it creates the counter.
// Store.Replace must write only over a key that exists, checking and writing
// in one atomic step, so that renewing a session never restores one that a
// logout deleted in the meantime. The Store must keep every key until it
// expires: a revocation evicted early stops revoking.
```

## Not exported

In `session/revocation.go`:

- `revocationMargin`, one hour;
- `(*Manager).generation`, which reads the state;
- `(*Manager).revoked`, which applies the validity rule;
- `revocationKey`.

`Manager` gains the field `revocationTTL`, computed in `New`. `sessionRecord` and
`pendingRecord` gain `Gen`.

## Expected `gorelease` report against `auth/v0.2.0`

- **Incompatible**: none.
- **Compatible**: one change, the `RevokeSubject` method added to `*session.Manager`.
- **Suggested version**: `v0.3.0`.

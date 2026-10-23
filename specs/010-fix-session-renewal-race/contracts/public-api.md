# Contract: Public API (auth/v0.1.0 → v0.2.0)

**Spec**: [../spec.md](../spec.md) | **Store contract**: [store.md](store.md) | **Research**: [../research.md](../research.md) R-01 to R-08

This feature changes one exported interface, adds one exported method, and changes two doc
comments. No identifier is removed or renamed.

## Package `auth`

### `Store` (changed: incompatible)

```go
// Store is a key-value store with expiry that holds the library's short-lived
// state: sessions, pending logins and counters. The library chooses every key
// and never stores a secret in clear.
//
// Implementations must guarantee:
//   - Get returns an error matching ErrNotFound when the key is absent or its
//     TTL has elapsed;
//   - Set, Replace and Incr receive ttl > 0;
//   - Replace stores the value with expiry now + ttl only when the key is
//     present and its TTL has not elapsed, checking and writing in one atomic
//     step, and reports whether it stored it; an absent or expired key is not
//     an error, and Replace never creates a key;
//   - Delete of an absent key returns nil;
//   - Incr creates the counter at 1 with expiry now + ttl and never extends
//     the expiry of an existing counter; N concurrent calls on the same new
//     key return each value from 1 to N exactly once;
//   - every operation honors the cancellation of ctx.
//
// The library never calls Get or Replace on a key it increments, nor Incr on a
// key it sets or replaces.
type Store interface {
	Get(ctx context.Context, key string) ([]byte, error)
	Set(ctx context.Context, key string, value []byte, ttl time.Duration) error
	Replace(ctx context.Context, key string, value []byte, ttl time.Duration) (bool, error)
	Delete(ctx context.Context, key string) error
	Incr(ctx context.Context, key string, ttl time.Duration) (int64, error)
}
```

| Requirement | Covered by |
|-------------|------------|
| FR-001, FR-002 | the `Replace` method and its rules |
| FR-003 | the separation sentence |

### Package documentation (`doc.go`, "# Store"; changed)

```go
// The Store holds sessions, pending logins and counters, for example in
// Redis. The library chooses every key, derives the keys of secrets from
// their SHA-256 hash, and never writes a secret in clear. Store.Incr must be
// atomic and must set the expiry only when it creates the counter.
// Store.Replace must write only over a key that exists, checking and writing
// in one atomic step, so that renewing a session never restores one that a
// logout deleted in the meantime.
```

## Package `memstore`

### `(*Store).Replace` (new: compatible)

```go
// Replace stores a copy of value under key for ttl when key is present and
// unexpired, and reports whether it did. It never creates a key, and an
// expired key stays absent.
func (s *Store) Replace(ctx context.Context, key string, value []byte, ttl time.Duration) (bool, error)
```

Errors: the wrapped `ctx.Err()` for a cancelled context, and an error for `ttl <= 0`, as `Set`
returns (FR-004).

## Package `session`

### `(*Manager).Authenticate` (doc comment changed; signature unchanged)

```go
// Authenticate authenticates r with its session cookie, reading only the
// store. A missing cookie, or one whose session is malformed, unknown, expired
// or revoked, returns auth.ErrNoCredentials, also when the session is deleted
// while this request renews it. A store failure returns an error.
```

The behavior change is the fix (FR-006, FR-007). `Issue`, `Revoke`, `IssuePending` and `Pending`
keep their signatures, doc comments and observable behavior. The error text of a failed `Issue`
loses a duplicated prefix ([research R-03](../research.md#r-03-renewal-with-the-conditional-write-fr-006-fr-007-fr-009-fr-010)).

## Not public: `internal/storetest`

`OpReplace`, `(*Recorder).Replace`, which records the value as for `OpSet`, and
`Failing.Replace`. They keep both test doubles satisfying `auth.Store`.

## Expected `gorelease` report against `auth/v0.1.0`

- **Incompatible**: one change, the method added to the `auth.Store` interface.
- **Compatible**: one change, the `Replace` method added to `*memstore.Store`.
- **Suggested version**: `v0.2.0`.

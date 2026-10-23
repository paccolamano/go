# Contract: Store (changes from auth/v0.1.0)

**Base**: [007 contracts/store.md](../../007-http-auth/contracts/store.md) | **Data model**: [../data-model.md](../data-model.md) | **Research**: [../research.md](../research.md) R-02, R-04, R-05

This document lists only what changes in the store contract. Everything else in the base contract
still applies.

## 1. Operations

The base table gains one row:

| Operation | Must | Must not |
|-----------|------|----------|
| `Replace(ctx, key, value, ttl)` | when `key` is present and its TTL has not elapsed, store `value` with expiry `now + ttl` and return `true, nil`; otherwise store nothing and return `false, nil`. Check and write in one atomic step. `ttl > 0` | create a key; revive an expired key; let a concurrent `Delete` land between the check and the write; report an absent key as an error; keep a reference to `value` that the caller can later mutate (in-process stores) |

Further rules, changed or added:

- **Atomicity**: after a `Replace` and a `Delete` of the same key run concurrently, the key is
  absent, whichever runs first.
- **Errors**: an absent or expired key is `false, nil`. Any other failure is a non-nil error,
  which the module turns into 500, never into 401 (FR-010).
- **Separation**: the module never calls `Get` or `Replace` on a key it increments, and never
  calls `Incr` on a key it sets or replaces.
- **Creation**: the module calls `Set` only on keys derived from a fresh identifier. It writes
  over a key it has read only with `Replace` (FR-008).

## 2. Keys

One row of the base table changes:

| Kind | Operation | TTL |
|------|-----------|-----|
| `session` | `Get` (authentication), `Set` (issuance), **`Replace` (renewal)**, `Delete` (logout, new login) | `min(rw + idle, exp) − now` |

Every other kind keeps its operations.

## 3. Redis mapping

The adapter in the README maps each operation to one command, or to one script, on one key, so
it works with Redis Cluster:

| Operation | Redis | go-redis v9 call |
|-----------|-------|------------------|
| `Get` | `GET key` | `C.Get(ctx, key).Bytes()`; `redis.Nil` → `auth.ErrNotFound` |
| `Set` | `SET key value PX ttl` | `C.Set(ctx, key, value, ttl).Err()` |
| `Replace` | `SET key value PX ttl XX` | `C.SetXX(ctx, key, value, ttl).Result()`; a nil reply is `false, nil` |
| `Delete` | `DEL key` | `C.Del(ctx, key).Err()` |
| `Incr` | script: `INCR`, then `PEXPIRE` when the value is 1 | `incr.Run(...).Int64()` |

go-redis sends `EX` instead of `PX` when the TTL is a whole number of seconds. Both forms are
atomic.

## 4. In-memory store (`memstore`)

Added to the base section:

- `Replace` implements §1 under the store's mutex, so the check and the write are atomic.
- It copies the bytes, rejects `ttl <= 0`, and honors a cancelled context, as `Set` does.
- An expired entry that `Replace` finds is deleted, as `Get` deletes one, and `Replace` returns
  `false`.
- `Replace` on a counter replaces it with bytes, as `Set` does. The module never does this.

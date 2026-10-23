# Contract: Store (changes from auth/v0.2.0)

**Base**: [007 contracts/store.md](../../007-http-auth/contracts/store.md), as changed by [010 contracts/store.md](../../010-fix-session-renewal-race/contracts/store.md) | **Data model**: [../data-model.md](../data-model.md) | **Research**: [../research.md](../research.md) R-03, R-04, R-06, R-08, R-09

The `auth.Store` interface does not change, and adapters need no change. This document lists how
the module uses the store differently, and the two requirements it now states. Everything else in
the base contracts still applies.

## 1. Operations

No operation is added or changed. The separation rule still holds: the module never calls `Get` or
`Replace` on a key it increments, nor `Incr` on a key it sets or replaces.

One rule of spec 010 gains an exception. The module calls `Set` only on keys derived from a fresh
identifier, **except** for the revocation state. A revocation writes a new value over it without
reading it, so no lost update is possible.

## 2. Keys

The base table gains one row, and two rows gain an operation:

| Kind | Digest input | Suffix | Operation | TTL |
|------|--------------|--------|-----------|-----|
| **`session-gen`** (new) | subject | none | `Get` (authentication, pending login read, issuances), `Set` (revocation) | `max(lifetime, pendingLifetime) + 1 h` |
| `session` | session identifier | none | `Get`, `Set`, `Replace`, `Delete`, unchanged | unchanged |
| `pending` | pending identifier | none | `Get`, `Set`, `Delete`, unchanged | unchanged |

The new key, `auth:session-gen:<43 characters>`, is 60 bytes, within the 80-byte bound. It
contains no subject in clear.

## 3. Values

- **`session-gen`**: 43 base64url characters, random, not a secret.
- **`session` and `pending`**: the JSON objects gain an optional `gen` string. It is omitted when
  the subject had no revocation state at issuance. `v` stays `1`.

```json
{"v":1,"sub":"user-42","amr":["pwd"],"iat":1792396800,"exp":1792425600,"rw":1792396800,"gen":"<43 characters>"}
```

A value whose `v` is not 1, or that does not decode, still fails with an error, answered with
500.

## 4. Requirements on the deployment of the store

Spec 007 left these implicit. Revocation depends on them, and the README states them:

| Requirement | Why | With Redis |
|-------------|-----|------------|
| A key stays until its TTL elapses | an evicted revocation state stops revoking; evictions already weaken attempt limits and replay protection | the default `maxmemory-policy`, `noeviction`, never evicts keys. The `volatile-*` policies can evict any key of the module, since every key has a TTL |
| A read sees every write that completed before it | revoking and then issuing in one request must read the new state | read from the primary, the default of go-redis clients; not from replicas |

## 5. Reference adapter (README) and in-memory store

Unchanged. The Redis adapter maps `Get` and `Set` of the new key kind to the same single
commands, `GET` and `SET … PX`, and each touches one key, so Redis Cluster still works.
`memstore` meets both requirements of §4: it evicts nothing before expiry, and its mutex orders
every operation.

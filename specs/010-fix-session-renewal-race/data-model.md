# Data Model: Deleted Sessions Stay Deleted During Renewal (auth)

**Feature**: [spec.md](spec.md) | **Research**: [research.md](research.md)

This feature adds no state and does not change the format of any stored value. It adds one
operation to the store contract and changes which operation renewal uses. The model of spec 007
([007 data-model.md](../007-http-auth/data-model.md)) still applies. This document describes
only what changes.

## 1. Store operations

| Operation | Stores when | Result for an absent or expired key | Atomic with |
|-----------|-------------|-------------------------------------|-------------|
| `Get` | never | `auth.ErrNotFound` | — |
| `Set` | always: creates or replaces | creates the key | — |
| **`Replace`** (new) | only when the key is present and unexpired | `false, nil`; nothing stored | every other operation on the key, `Delete` in particular |
| `Delete` | never | `nil` | — |
| `Incr` | always: creates at 1, or adds 1 | creates the counter at 1 | concurrent `Incr` calls |

Validation, as for `Set` and `Incr`: `ttl > 0`, and a cancelled context gives an error.

Separation: the module never calls `Get` or `Replace` on a key it increments, nor `Incr` on a
key it sets or replaces.

## 2. Lifecycle of a session key

States of the key `auth:session:<hash of the identifier>`:

```text
              Set (Issue, new identifier)
   Absent ───────────────────────────────► Live
     ▲                                      │ │
     │  Delete (Revoke, or Issue carrying   │ │ Replace (renewal): stays Live,
     │  it), or TTL elapsed                 │ │ new activity time and TTL
     └──────────────────────────────────────┘ ◄┘

   Replace on Absent: stores nothing, stays Absent, returns false.
```

- **Invariant**: once a session key becomes Absent, it never becomes Live again. Only `Set`
  creates a key, and the module calls `Set` only with a fresh 256-bit identifier
  ([research R-06](research.md#r-06-read-then-write-audit-fr-008)).
- **Before this feature**: renewal used `Set`, which takes the key from Absent to Live. That was
  the defect.

## 3. Interleavings of a renewal and a deletion

A request R renews the session: it calls `Get`, then `Replace`. A deletion D, from a logout or a
new login, calls `Delete` on the same key. Each row is one order of the store calls:

| Order | R's result | Key afterwards | Later requests with the identifier |
|-------|------------|----------------|------------------------------------|
| D, then R's `Get` | no credentials (`Get`: not found) | Absent | no credentials |
| R's `Get`, D, then R's `Replace` | no credentials (`Replace`: `false`) | Absent | no credentials |
| R's `Get`, R's `Replace`, then D | authenticated | Absent | no credentials |

With `Set` in place of `Replace`, the second row ends with the key Live. The request is
authenticated, and so is every later request with the identifier, until the absolute lifetime.

Two renewals R1 and R2 of the same session, with no deletion: both `Replace` calls store, in
either order, records that differ at most in the activity time. The key stays Live, and both
requests are authenticated.

A key whose store TTL elapses between R's `Get` and R's `Replace` behaves like the second row: no
credentials, and the key stays Absent.

## 4. Session record

Unchanged: `{"v":1,"sub":…,"amr":[…],"iat":…,"exp":…,"rw":…}`
([007 contracts/store.md §3](../007-http-auth/contracts/store.md#3-values)). Renewal still
rewrites `rw` only, and the TTL is still min(rw + idle, exp) − now, with a floor of one second.

# Data Model: Revoke Every Session of a Subject (auth)

**Feature**: [spec.md](spec.md) | **Research**: [research.md](research.md)

This feature adds one key kind to the store and one optional field to the session and pending
login records. The model of spec 007 ([007 data-model.md](../007-http-auth/data-model.md)), with
the change of spec 010 ([010 data-model.md](../010-fix-session-renewal-race/data-model.md)),
still applies. This document describes only what changes.

## 1. Revocation state

| Property | Value |
|----------|-------|
| Key | `auth:session-gen:<digest>`, where the digest is the SHA-256 of the subject in unpadded base64url (60 bytes) |
| Value | 43 base64url characters: 256 random bits, fresh at each revocation; not a secret |
| TTL | `max(lifetime, pendingLifetime) + 1 h`; 9 hours with the defaults |
| Written by | `RevokeSubject`, with `Set`, without reading the key first |
| Read by | `Authenticate`, `Pending`, `Issue` and `IssuePending`, with `Get` |
| Never | incremented, replaced with `Replace`, or deleted by the module |

Lifecycle of the key of one subject:

```text
                 Set (RevokeSubject)                 Set (RevokeSubject)
   Absent ───────────────────────────► Present(G1) ──────────────────────► Present(G2)
     ▲                                     │                                    │
     │              TTL elapsed            │            TTL elapsed             │
     └─────────────────────────────────────┴────────────────────────────────────┘

   Absent: the subject was never revoked, or its last revocation expired.
   Each Set writes a new value and a new TTL; the old value is gone.
```

## 2. Records

Both records gain `gen`, the revocation state that the issuance read, omitted when it was absent.
The format version stays 1.

| Record | Fields (v0.2.0) | Added |
|--------|-----------------|-------|
| Session | `v`, `sub`, `amr`, `iat`, `exp`, `rw` | `gen` (optional) |
| Pending login | `v`, `sub`, `amr`, `iat` | `gen` (optional) |

```json
{"v":1,"sub":"user-42","amr":["pwd","otp","mfa"],"iat":1792396800,"exp":1792425600,"rw":1792397700,"gen":"3q2-7wAAAAA…"}
```

```json
{"v":1,"sub":"user-42","amr":["pwd"],"iat":1792396800}
```

- A record of a subject that was never revoked has no `gen`, and is byte for byte a record of
  0.2.0.
- A record of 0.2.0 decodes with `gen` empty.
- 0.2.0 decodes a record with `gen`, ignoring the field.
- Renewal rewrites `rw` only; `gen` keeps the value read at issuance.

## 3. Validity rule

A session or pending login that passed its expiry checks is checked against the current state of
its subject:

| Current state | Record's `gen` | Outcome |
|---------------|----------------|---------|
| absent | any, empty included | valid |
| present, `G` | `G` | valid |
| present, `G` | anything else, empty included | revoked: `auth.ErrNoCredentials` |
| read fails | any | error: 500 for a request |

## 4. Interleavings

Each row is one order of the store calls of two operations on the same subject. R is a
revocation: one `Set` of the state. The state is absent before R.

### Issuance I (`Get` state, then `Set` record)

| Order | Record's `gen` | The new session afterwards |
|-------|----------------|----------------------------|
| R, then I's `Get` | R's value | valid |
| I's `Get`, R, then I's `Set` | empty | revoked (FR-005, the prudent outcome) |
| I's `Get`, I's `Set`, then R | empty | revoked |

`IssuePending` behaves the same way for pending logins.

### Authentication A of an existing session, due for renewal (`Get` record, `Get` state, `Replace` record)

| Order | A's result | Later requests |
|-------|------------|----------------|
| R, then A's record `Get` | no credentials | no credentials |
| A's record `Get`, R, then A's state `Get` | no credentials | no credentials |
| A's state `Get`, R, then A's `Replace` | authenticated, record renewed | no credentials |
| A's `Replace`, then R | authenticated | no credentials |

A request that read the state before the revocation completed may still be served. Every request
that starts after it completes is not.

### Reading a pending login P (`Get` record, `Get` state), then completing it

| Order | P's result | Session issued after the second factor |
|-------|------------|----------------------------------------|
| R, then P's state `Get` | no credentials | none: the second factor cannot be verified |
| P's state `Get`, then R, then the issuance | the pending login | valid: the window of FR-006, accepted |

### Two revocations R1 and R2

Both write a fresh value; the later `Set` stays. Every record issued before either of them holds
an older value, or none, and is revoked. A record issued between them holds R1's value. It is
revoked once R2 completes.

## 5. Store calls per operation

| Operation | Reads (v0.2.0 → v0.3.0) | Writes (unchanged unless noted) |
|-----------|-------------------------|---------------------------------|
| `Authenticate`, live session | 1 → 2 | `Replace` at most once per renewal interval |
| `Authenticate`, expired session | 1 → 1 | none |
| `Authenticate`, revoked session | — → 2 | none |
| `Issue` | 0 → 1 | `Delete` × 0–2, `Set` |
| `IssuePending` | 0 → 1 | `Delete` × 0–1, `Set` |
| `Pending` | 1 → 2 | none |
| `RevokeSubject` (new) | 0 | `Set` × 1 |

## 6. Retention timeline

With the default lifetimes, for a revocation R at time 0:

```text
 0h        R: state written, TTL 9h
 ≤ 0h      sessions issued before R: valid at most until 8h; pending logins at most until 5 min
 ≈ 0h      an issuance in flight during R: valid at most until 8h plus the in-flight gap
 9h        state expires; every record issued before R has expired;
           sessions issued after R carry R's value and stay valid (absent state)
```

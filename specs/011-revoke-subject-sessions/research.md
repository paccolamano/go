# Research: Revoke Every Session of a Subject (auth)

**Feature**: [spec.md](spec.md) | **Plan**: [plan.md](plan.md) | **Date**: 2026-10-23

The Technical Context had no open questions. The language, toolchain, module and dependencies are
those of the existing `auth` module (specs 007 and 010). The request settles the approach, a
per-subject generation checked at every request, and the clarification of 2026-10-23 settles the
window of FR-006. This research records the design decisions, and the evidence behind them.

**Spike**: on 2026-10-23 this design was implemented in a scratch copy of `auth/v0.2.0`, outside
the repository.

- **Red**: the record field, `RevokeSubject` and the reads at issuance were in place, but
  `Authenticate` and `Pending` did not check the revocation state. 9 of the 12 new tests failed.
  The 3 that passed (`ThenIssue`, `WindowAccepted`, `Retention`) check what must stay valid.
- **Green**: with the check, the whole suite passed under `-race -shuffle=on`, with 97.3%
  statement coverage for the module and 92.7% for `session`.
- **Lint**: `golangci-lint` with the repository configuration reported two issues, a gofmt
  alignment and a `wrapcheck` on the store error, then 0 after the fixes recorded in R-02.
- **Benchmark**: `BenchmarkSessionAuthenticate` went from about 2958 ns to 3585 ns per request.
  `BenchmarkCheck` of `password` takes about 18 ms, so a session check is about 0.02% of it.
- **Invariant**: `TestStoreSetCreatesOnly` failed until it exempted the revocation state (R-09).
- **Not in the spike**: the README, the CHANGELOG and the doc comments of R-11.

## R-01 Release level and API compatibility

- **Decision**: release `auth/v0.3.0`.
  - The feature adds one exported method, `(*session.Manager).RevokeSubject`, and changes no
    signature, interface or exported type. `gorelease` reports one compatible change and suggests
    a MINOR version.
  - The store contract does not change, so store adapters keep building (spec User Story 4,
    scenario 3).
- **Changelog draft** (`auth/CHANGELOG.md`, section `[Unreleased]`):

  ```markdown
  ### Added

  - `session`: `Manager.RevokeSubject` revokes every session and pending login of a subject, on
    every instance, for example after a password change, when an account is disabled, or to log
    out of every device. A request authenticated with a session now reads the store twice: the
    session, and its subject's revocation state. Issuing a session or a pending login, and
    reading a pending login, also read the store once more. Sessions issued by 0.2.0 stay valid
    until their subject is revoked. 0.2.0 reads the sessions that this version writes, but does
    not check revocations.
  ```

- **Rationale**: Principle II. The extra store read changes the cost of every request, so the
  changelog states it next to the addition.
- **Alternatives rejected**: a PATCH release (0.2.1). The feature adds API.

## R-02 The operation: name, signature, errors (FR-001, FR-008)

- **Decision**:

  ```go
  func (m *Manager) RevokeSubject(ctx context.Context, subject string) error
  ```

  - An empty subject returns `errors.New("session: revoke subject: the subject is empty")`,
    without touching the store. This matches `Issue`, which rejects a principal without a subject
    with a plain error.
  - It writes the revocation state with one `Set` (R-03). A store failure returns
    `fmt.Errorf("session: revoke subject: %w", err)`. A cancelled context is a store failure, as
    in every store operation.
  - It clears no cookie, and needs no request or response.
  - Errors from reading the revocation state are wrapped once, in the helper, as "read revocation
    state: …". Each caller adds its own prefix: "session: …" in `Authenticate` and `Pending`,
    "session: issue: …" in `Issue`, and "session: issue pending: …" in `IssuePending`. The spike
    first returned the store error unwrapped, which `wrapcheck` reported.
- **Rationale**:
  - **Name**: the request suggests `RevokeSubject`. It pairs with `Revoke`, which ends the
    sessions of one request, and "subject" is the field of `auth.Principal` that the developer
    passes.
  - **A context, not a request**: an administrative action or a background job has no request of
    the user at hand (spec User Story 2). Methods bound to a request take their context from it.
    This method is not bound to one, so it takes a context first (Principle IV).
  - **No `auth.ErrInvalidConfig`** for an empty subject: that error is for constructors.
- **Alternatives rejected**:
  - **`RevokeAll(w, r)`**, bound to the current request: it covers neither User Story 2 nor a
    job, and clearing the current cookie is already `Revoke`.
  - **A variant that keeps the current session**: revoking and then calling `Issue` does the same
    in two calls, and the current browser gets a new identifier (spec Assumptions).
  - **Returning how many sessions were revoked**: the store holds no index of a subject's
    sessions, so the number is unknown.

## R-03 The revocation state (FR-005, FR-007, FR-012, FR-014)

- **Decision**:
  - **Key**: `storekey.Key("session-gen", subject)`, that is `auth:session-gen:<digest>`, where
    the digest is the SHA-256 of the subject in unpadded base64url. The key is 60 bytes, within
    the 80-byte bound of the base contract, and does not contain the subject in clear.
  - **Value**: a fresh `newID()`, the same 256-bit random value, in 43 base64url characters, that
    session identifiers use. It is not a secret: knowing it does not let anyone write a record.
  - **Write**: `RevokeSubject` calls `Set` and never reads the key first. The new value does not
    depend on the old one, so two concurrent revocations need no coordination: the last write
    stays, and every record issued before either revocation differs from it.
  - **Reads**: `Get` only. The module never increments this key, so the separation rule of the
    contract holds (R-09).
- **Rationale**:
  - A random value changes at each revocation and never repeats, so equality alone decides,
    whatever the clocks say (FR-007). Under virtual time a revocation and two issuances can
    happen at one instant, and the outcome still follows their order (SC-004).
  - One `Get` and one `Set`, both existing operations, keep the store contract unchanged
    (FR-014).
- **Alternatives rejected**:
  - **A revocation time compared with the session's issuance time**: it compares the clocks of
    different instances, against FR-007. In the request's common case, revoke then issue in the
    same request, both times fall in the same second, so a record in Unix seconds cannot tell
    them apart.
  - **A counter with `Incr`**: the contract forbids reading a key that the module increments,
    and `Incr` never extends the expiry of an existing counter. The counter would expire on the
    schedule of the first revocation, even right after a later one. The later revocation would
    then be lost, and the sessions it revoked would authenticate again.
  - **An index of each subject's sessions, to delete them**: it needs set operations that the
    contract does not have. Adding them would break every adapter, and keeping the index in one
    value would need a read-modify-write, which races.
  - **The state in the developer's database, through a lookup**: sessions must read only the
    store (FR-041 of spec 007), and the database would get one more query per request.

## R-04 The validity rule and the retention of the state (FR-002, FR-012, FR-013)

- **Decision**:
  - **Validity**: a session or pending login is revoked when the subject's revocation state is
    present and differs from the `gen` its record observed at issuance. An absent state revokes
    nothing.

    ```go
    revoked := current != "" && current != rec.Gen
    ```

  - **Retention**: `RevokeSubject` writes the state with a TTL of
    `max(lifetime, pendingLifetime) + revocationMargin`. `revocationMargin` is one hour, so the
    default is 9 hours. The Manager computes the TTL once, in `New`.
- **Rationale**:
  - **An absent state revokes nothing**, as the request states. Sessions issued after a revocation
    carry its value, and must keep working after the state expires (FR-013). A rule that rejected
    any record with a `gen` and no state would log those users out at that moment.
  - **What the retention must cover**: every record issued before the revocation must be expired
    before the state expires. The candidates are:
    - sessions issued before the revocation, which live at most the absolute lifetime;
    - pending logins issued before it, which live at most the pending lifetime;
    - a session whose issuance read the state before the revocation and wrote the record after
      it (FR-005). It starts slightly after the revocation, and carries the old value.
  - **What the margin covers**: two time differences.
    - An issuance in flight: the gap between its read of the state and its write.
    - Clock differences between instances. They lengthen a session in the computation of its
      store TTL at renewal, and in the check of its absolute expiry.

    A one-hour margin covers both with room to spare. A clock difference of an hour would
    already break the absolute lifetime (FR-035 of spec 007).
  - **Cost**: one key of 60 bytes, with a value of 43 bytes, per revoked subject, for at most 9
    hours by default.
- **Alternatives rejected**:
  - **No expiry**: the store would grow with every revocation, against FR-012.
  - **The lifetime alone, without a margin**: the issuance in flight of FR-005, and clock
    differences, could outlive the state by a few seconds.
  - **Refreshing the state's TTL at each issuance**: it needs a read-then-write, which can
    overwrite a concurrent revocation with the old value.

## R-05 Where the state is read, and the cost (FR-003, FR-005, FR-009, FR-010, FR-011)

- **Decision**:

  | Operation | Reads the state | Where | Outcome |
  |-----------|-----------------|-------|---------|
  | `Authenticate` | yes | after decoding the record and checking its expiry, before renewal | revoked: `auth.ErrNoCredentials`, and no renewal |
  | `Pending` | yes | after decoding the record and checking its expiry | revoked: `auth.ErrNoCredentials` |
  | `Issue` | yes | after deleting the carried session and pending login, right before the write | the value goes into the record |
  | `IssuePending` | yes | after deleting the carried pending login, right before the write | the value goes into the record |
  | `Pending.Attempt`, `Revoke` | no | — | unchanged |

  A failure reading the state is returned as an error: 500 for a request, and no cookie for an
  issuance (FR-011).
- **Rationale**:
  - **After the expiry checks**: an expired session still costs one read, as today, and only a
    live session pays the second.
  - **Before renewal**: a revoked session is never renewed, so it costs no write.
  - **Right before the write in `Issue`**: the gap between reading the state and writing the
    record is where a concurrent revocation discards the new session (FR-005). Reading last makes
    that gap as short as possible.
  - **Not in `Attempt`**: the pending login was checked when it was read. FR-006 accepts the
    milliseconds between reading it and issuing the session.
  - **Cost**: a session request goes from one store read to two (FR-010), and the benchmark adds
    about 0.6 µs on the in-memory store. Renewal still writes at most once per renewal interval.
- **Alternatives rejected**:
  - **Checking only at renewal**, every 7.5 minutes by default: no extra read on most requests,
    but a revocation would take up to 7.5 minutes. The request requires the check at every
    request.
  - **An option to turn the check off**: it would save one read only by silently giving up
    revocation, and a revocation called with the check off would do nothing (spec Assumptions).
  - **Deleting a revoked record when it is detected**: it costs a write for each request carrying
    a stale cookie. The record expires anyway, within the idle timeout.

## R-06 Record format and compatibility (FR-016, FR-017)

- **Decision**: `sessionRecord` and `pendingRecord` gain:

  ```go
  Gen string `json:"gen,omitempty"`
  ```

  The format version stays 1, and `decodeRecord` does not change.
- **Rationale**:
  - **Rollback**: `decodeRecord` of `auth/v0.2.0` is the same function. It checks `v == 1`, and
    `encoding/json` ignores unknown fields, so 0.2.0 decodes the new records. Sessions issued by
    this release keep working after a rollback, without revocation checks (FR-017).
  - **Upgrade**: a record of 0.2.0 has no `gen`, so it decodes as `""`. It stays valid until the
    first revocation of its subject, which writes a value different from `""` (FR-016).
  - **`omitempty`**: the record of a subject that was never revoked is byte for byte a 0.2.0
    record, which a test pins.
  - **A rolling upgrade** mixes both behaviors. Instances running 0.2.0 do not check revocations
    until they are upgraded. The README states it.
- **Alternatives rejected**:
  - **Version 2**: 0.2.0 rejects an unknown version with an error, answered with 500. A rollback
    would then fail every session issued by this release until it expired.

## R-07 The window between the check of the first factor and the issuance (FR-006)

- **Decision**: accepted and documented, as the clarification of 2026-10-23 decides.
  - The doc comment of `RevokeSubject` says that a session issued after it returns is valid,
    even when its credentials were checked before, and tells developers to call `Issue` right
    after checking them.
  - The README gives the same advice in the revocation section.
  - `TestRevokeSubject_WindowAccepted` pins the behavior with a pending login. The login is read
    before the revocation and completed after it, and its session authenticates.
- **Rationale**: the module does not see when the developer checks a password. The clarification
  rejected the alternatives, which compare clocks or cover only pending logins.

## R-08 What the feature needs from the store (FR-018)

- **Decision**: the README and the contract state two requirements of the store, which spec 007
  left implicit:
  1. **Keys stay until they expire.** The revocation state is the only record of a revocation,
     and an absent state revokes nothing (R-04). A store that evicts it early lets the revoked
     sessions authenticate again. Eviction already weakens attempt limits and replay protection
     today. The default `maxmemory-policy` of Redis, `noeviction`, never evicts keys. The
     `volatile-*` policies can evict any key of this module, since every key has an expiry.
  2. **A read sees every write that completed before it.** The common flow revokes and then
     issues in the same request, and the issuance must read the new state. A store that serves
     reads from lagging replicas could miss it:
     - the new session would carry the old state, and be revoked as soon as the replica caught
       up;
     - and a revoked session would authenticate until then.

     A go-redis client reads from the primary unless it is configured to read from replicas.
- **Shared namespace**: every Manager on one store shares the revocation states, because the key
  depends only on the subject.
  - Two Managers of one application, for example with different cookie names, revoke together,
    since the subject identifies the user.
  - Two applications that share one database already need a key prefix in the adapter, as the
    README says.
- **Alternatives rejected**: making these requirements part of `auth.Store` as new methods or
  capabilities. They concern how the store is deployed, not its interface.

## R-09 Store invariants and secrets (FR-014, FR-015)

- **Separation rule**: the revocation key is read with `Get` and written with `Set`, never
  incremented, so "the module never calls `Get` or `Replace` on a key it increments, nor `Incr`
  on a key it sets or replaces" still holds.
- **`TestStoreSetCreatesOnly`** (spec 010, R-06) asserts that every `Set` targets a key that no
  earlier store call used. A revocation overwrites its key on purpose, after issuances have read
  it.
  - In the spike, the test failed with "Set over the key "auth:session-gen:…", which an earlier
    call used".
  - **Decision**: the test skips `Set` calls on `auth:session-gen:` keys, with a comment saying
    that a revocation writes a new value without reading the old one. It also asserts that at
    least one revocation ran. Every other key keeps the invariant: renewal still writes with
    `Replace`, and every other `Set` targets a fresh identifier.
- **Secrets** (SC-008): `runEveryFlow` gains a revocation of `user-1` and a new session issued
  after it. `TestStoreHoldsNoSecrets` then scans the revocation key, which hashes the subject,
  and a record that carries `gen`. The `gen` value is random, not derived from a secret.

## R-10 Testing approach (FR-020, SC-001 to SC-009; Principle III)

All new tests are in `session/revocation_test.go`, as `TestRevokeSubject_<Scenario>`, unless
noted. Time is virtual (`testing/synctest`) wherever lifetimes or renewal intervals matter.

| Test | Covers |
|------|--------|
| `EndsSessions` | US1 scenarios 1 and 3: three sessions of a subject, two of them due for renewal, give no credentials after a revocation, and a session of another subject still authenticates (FR-002, FR-004, SC-001, SC-002) |
| `ThenIssue` | US1 scenario 2: revoke, then issue in the same request; the new session authenticates every 20 minutes until its absolute lifetime, and the replaced one does not (FR-004, SC-002) |
| `ServedAsAnonymous` | US1 scenario 4: under `auth.WithAnonymous`, a revoked cookie reaches the handler with no principal |
| `SharedStore` | US2 scenario 1: two managers on one store; a revocation through one ends the sessions of both (FR-002) |
| `Pending` | US3: a pending login issued before the revocation reads as cancelled; one of another subject, and one issued after, work and complete (FR-003, FR-004) |
| `WindowAccepted` | FR-006: a pending login read before the revocation and completed after it yields a valid session (R-07) |
| `Interleavings` | FR-005, FR-020, edge cases: a revocation at each point of an issuance, a renewal and the reading of a pending login, through the `interleaved` wrapper; the table states the outcome of the interleaved operation and of later requests |
| `SameInstant` | SC-004, FR-007: an issuance, a revocation and another issuance at one virtual instant; the first session is revoked and the second is not |
| `Concurrent` | SC-003: 100 requests during a revocation, any outcome but an error; 100 requests that start after it, all with no credentials; `-race` |
| `Retention` | FR-012, FR-013, SC-007: the state is present 8 hours after the revocation and absent at 9 hours; a session issued 2 hours after the revocation authenticates past 9 hours |
| `Errors` | US2 scenarios 2 and 4, FR-008, FR-011, SC-006: an empty subject makes no store call; with failing reads and writes of the state, `RevokeSubject` fails; `Authenticate` fails and the middleware answers 500; `Issue` and `IssuePending` fail and set no cookie; `Pending` fails. A retry succeeds, and a cancelled context returns `context.Canceled` |
| `PreviousRelease` | US4 scenario 1, FR-016: a session and a pending login written in the 0.2.0 format work, then give no credentials after a revocation |
| `TestRecord_Fields` (`record_internal_test.go`) | the field sets with and without `gen` |
| `TestRecord_PreviousRelease` (`record_internal_test.go`) | US4 scenario 2, FR-017: a record of this release decodes into the 0.2.0 shape; a 0.2.0 record decodes with an empty `gen`; the record of a never revoked subject is byte for byte the 0.2.0 one |
| `TestAuthenticate_Principal` (`session_test.go`) | FR-010, SC-005: exactly two `Get` calls per request, the session and the revocation state |
| `TestStoreHoldsNoSecrets`, `TestStoreSetCreatesOnly` (root) | SC-008 and the invariants of R-09 |
| `ExampleManager_RevokeSubject` | the primary new API (Principle III): a password change on a laptop ends the phone's session and keeps the laptop signed in |

- **Test helpers** (`session/helpers_test.go`):
  - `interleaved` gains the point `beforeRevocationRead`, which fires on the `Get` of a key that
    starts with `auth:session-gen:`. This places a revocation between the session read and the
    revocation read of a renewal, or between the two reads of `Pending`.
  - A new `failingRevocation` wrapper fails only the `Get` and `Set` of revocation keys, because
    `storetest.Failing` fails an operation on every key.
- **Unchanged**: `FuzzSessionCookie`, because cookie parsing does not change. No new code parses
  external input. `BenchmarkSessionAuthenticate` now measures the two reads.
- **SC-005**: a test that uses a session once per second for a full idle timeout, and counts the
  writes, already exists as `TestRenewal_Writes`. Its counts do not change, because a live session
  is never revoked there.

## R-11 Documentation (FR-018, FR-019)

- **README**:
  - "Sessions and CSRF": "so most requests cost one store read and no write" becomes "so most
    requests cost two store reads, the session and its user's revocation state, and no write".
  - A new section, "Revoking every session of a user", after "Sessions and CSRF". It names the
    three cases and shows a `POST /password` handler that calls `RevokeSubject` and then `Issue`.
    It also covers:
    - the extra read per request;
    - the window of FR-006, with the advice to call `Issue` right after checking the password;
    - the retention, the session lifetime plus one hour;
    - rolling upgrades and rollbacks (R-06).
  - "Store": the two requirements of R-08, after the adapter.
- **Doc comments**: the texts in [contracts/public-api.md](contracts/public-api.md):
  - `RevokeSubject`;
  - the package comment of `session`;
  - `Authenticate`, `Issue`, `IssuePending` and `Pending`;
  - `auth.Store`, whose state now includes revocations, and the "# Store" section of `doc.go`.
- **CHANGELOG**: the entry drafted in R-01.
- **The artifacts of specs 007 and 010** stay as they are. This feature's
  [contracts/store.md](contracts/store.md) lists the changes, as spec 010 did for spec 007.

## R-12 Dependencies

- **Decision**: no new dependency. `session/revocation.go` imports `context`, `errors`, `fmt`,
  `time`, `auth` and `internal/storekey`, which the package already uses.
- **Rationale**: Principle V. `go.mod` and `go.sum` do not change.

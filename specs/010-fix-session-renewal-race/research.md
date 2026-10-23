# Research: Deleted Sessions Stay Deleted During Renewal (auth)

**Feature**: [spec.md](spec.md) | **Plan**: [plan.md](plan.md) | **Date**: 2026-10-22

The Technical Context had no open questions. The language, toolchain, module and dependencies are
those of the existing `auth` module (spec 007), and the request settles the approach: a
conditional write in the store contract. This research records the design decisions, and the
evidence behind them:

- **Reproduction**: a probe test in a scratch copy of `auth/v0.1.0` ran a `Revoke` between the
  `Get` and the `Set` of a renewal, under `testing/synctest`. The revoked session authenticated
  the next request ("session revoked during renewal is alive again").
- **Spike**: this design was implemented in the working tree on 2026-10-22, then reverted to
  follow the Spec Kit flow. With it, `make check MODULE=auth` passed:
  - `golangci-lint` with the repository configuration reported 0 issues;
  - the tests passed under `-race -shuffle=on`, with 96.0% statement coverage for the module;
  - the regression test of R-07 failed before the session change, for both a logout and a new
    login ("request renewing a session deleted meanwhile: <nil>, want ErrNoCredentials"), and
    passed after it.
- **Not in the spike**: the tests that this plan adds on top of the spike were not run. These are
  the concurrent tests of SC-002 and SC-005, the invariant test of R-06, the error-forwarding
  tests of R-07 and the extended example.

## R-01 Release level and API compatibility

- **Decision**: release `auth/v0.2.0`.
  - Adding a method to the exported interface `auth.Store` is an incompatible change: an adapter
    written for `auth/v0.1.0` no longer satisfies the interface, and its build fails until it
    gains the method (spec User Story 3, scenario 5).
  - At 0.x, a MINOR release may break compatibility. Principle II requires a new major version
    only from v1.0.0.
  - `gorelease` in the pull request workflow reports the change against `auth/v0.1.0` and
    suggests v0.2.0. The `api-compat` status enforces compatibility only at v1 or later.
- **Changelog draft** (`auth/CHANGELOG.md`, section `[Unreleased]`):

  ```markdown
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
  ```

- **Rationale**: Principle II. The changelog records every user-visible change in the same
  change, and states how to upgrade an adapter.
- **Alternatives rejected**: a PATCH release (0.1.1). The fix changes the exported API.

## R-02 The conditional write: name, signature and semantics (FR-001 to FR-003)

- **Decision**: add to `auth.Store`:

  ```go
  Replace(ctx context.Context, key string, value []byte, ttl time.Duration) (bool, error)
  ```

  - It stores `value` with expiry now + `ttl` only when `key` is present and its TTL has not
    elapsed, and returns `true`. Otherwise it stores nothing and returns `false, nil`. It never
    creates a key.
  - Checking and writing are one atomic step with respect to every other operation on the key,
    `Delete` in particular.
  - As for `Set` and `Incr`, `ttl > 0`. It honors the cancellation of `ctx`, and any failure is a
    non-nil error.
  - The contract's separation rule becomes: the module never calls `Get` or `Replace` on a key it
    increments, nor `Incr` on a key it sets or replaces.
- **Rationale**:
  - **Name**: `Replace` is the name that key-value stores use for this operation: Memcached's
    `replace` command stores only over an existing key. It reads naturally next to `Set`, and the
    doc comment states the atomicity. `SetXX` would be Redis jargon, and `SetIfExists` is longer
    without being clearer.
  - **A boolean result**: an absent key is a normal outcome of a race, not a failure, so it is
    not an error. The Redis adapter is one line, because go-redis returns exactly `(bool, error)`
    for `SET XX` (R-04). A `bool` also keeps "absent" from being mixed up with a store failure,
    which the module must answer with 500 (FR-010).
- **Alternatives rejected**:
  - `Replace(...) error`, returning `auth.ErrNotFound` for an absent key. It matches `Get`, but
    the Redis adapter grows to four lines that turn `false` into an error, and every adapter must
    remember to do it.
  - **An optional interface**, detected with a type assertion. Existing adapters would build and
    keep the defect silently (spec Assumptions, "A required operation").
  - **Compare-and-set on the value**, which writes only if the value is unchanged. It is stronger
    than needed: two concurrent renewals of the same session both write the same record with the
    same activity time, to the second. It is also harder to implement in adapters, since it needs
    a script in Redis.

## R-03 Renewal with the conditional write (FR-006, FR-007, FR-009, FR-010)

- **Decision**: in `(*session.Manager).Authenticate`, when the renewal interval has passed:
  1. set the record's time of last activity to now and encode the record;
  2. call `store.Replace(ctx, key, b, m.ttl(rec, now))`;
  3. on an error, return `fmt.Errorf("session: renew: %w", err)`, answered with 500;
  4. on `false`, return `auth.ErrNoCredentials`: the session was deleted, or expired in the store,
     since the `Get`;
  5. on `true`, return the principal.

  The private `write` helper, shared today by renewal and `Issue`, is split. A new
  `ttl(rec, now)` method computes min(rw + idle, exp) − now, with the existing one-second floor.
  `Issue` calls `store.Set` with it, because it writes a key under a new identifier, and renewal
  calls `store.Replace` with it. The error of a failed `Issue` loses a duplicated prefix: it was
  "session: issue: session: write: …", and becomes "session: issue: …".
- **Rationale**:
  - A deletion that lands before the `Replace` makes it store nothing. A deletion that lands
    after it removes the renewed record. In both orders the session is gone once the deletion
    completes (FR-007, data model §2).
  - The cost per request is unchanged: one `Get`, and a `Replace` in place of the `Set` once per
    renewal interval (FR-009).
  - Rejecting the renewing request is the spec's decision (Assumptions, "The renewing request is
    rejected"). It also takes no extra store call.
- **Alternatives rejected**:
  - **A separate activity key**: renewal writes only a small `session-rw:<hash>` key, and the
    record itself is never rewritten. No change to `auth.Store` would be needed, because a
    recreated activity key is useless without its record. But every request would read two keys,
    which breaks one read per request (SC-006 of spec 007, FR-009). The request also chose the
    conditional write.
  - **A tombstone** that `Revoke` writes in place of a `Delete`: an unconditional renewal `Set`
    overwrites the tombstone too, so a conditional write is still needed.
  - **Reading again after the write**, and deleting the record when it changed: a deletion can
    land after the second read too, so the race stays open.
  - **Store-specific transactions** (`WATCH` and `MULTI`, Lua scripts): they leak one store's
    model into a contract that must also fit other stores.

## R-04 The Redis adapter in the README (FR-005)

- **Decision**: the README adapter gains one method:

  ```go
  // Replace writes only over an existing key: SET with XX.
  func (s redisStore) Replace(ctx context.Context, key string, value []byte, ttl time.Duration) (bool, error) {
  	return s.C.SetXX(ctx, key, value, ttl).Result()
  }
  ```

  The paragraph after the adapter says that `SET` with `XX` checks and writes in one command, so
  a session renewed while a logout deletes it stays deleted. It also says that `SetXX` reports
  `false`, without an error, for an absent key. The introduction changes "about twenty lines" to
  "about thirty lines".
- **Evidence**: the source of go-redis v9.11.0, read from the local module cache:
  - `cmdable.SetXX` (`string_commands.go:274`) sends `set key value px|ex ttl xx`, as one
    command;
  - `BoolCmd.readReply` (`command.go:887`) turns the nil reply of a `SET` that did not store into
    `false` with a nil error.
- **Rationale**: one command, on one key, keeps the adapter atomic and compatible with Redis
  Cluster, like the other four methods.
- **Verification**: the module does not depend on go-redis, so no gate compiles the README
  adapter. [quickstart.md §6](quickstart.md#6-optional-the-readme-redis-adapter-compiles)
  describes an optional manual check.

## R-05 The in-memory store (FR-004)

- **Decision**: `(*memstore.Store).Replace`:
  - returns `ctx.Err()`, wrapped, for a cancelled context, and `errBadTTL` for `ttl <= 0`, like
    `Set`;
  - under the mutex, it returns `false` for an absent key. For an expired key it deletes the
    entry, as `Get` does, and returns `false`;
  - otherwise it stores a copy of `value` with expiry now + `ttl`, runs the periodic sweep as
    `Set` does, and returns `true`.

  Like `Set`, a `Replace` on a counter replaces the counter with bytes, as `SET XX` does in Redis.
  The module never does this (R-02).
- **Rationale**: one mutex already serializes every operation, so the check and the write are
  atomic with no further work.

## R-06 Read-then-write audit (FR-008)

Every store call in the library code of `auth/v0.1.0`:

| Package | Operation | Key | Read before in the same operation? |
|---------|-----------|-----|------------------------------------|
| `session` | `Issue`: `Delete` × 2, then `Set` | carried session and pending login, then a **new** identifier | no |
| `session` | `Authenticate`: `Get`, then `Set` at renewal | the carried session | **yes: the defect** |
| `session` | `Revoke`: `Delete` × 2 | carried session and pending login | no |
| `session` | `IssuePending`: `Delete`, then `Set` | carried pending login, then a **new** identifier | no |
| `session` | `Pending`: `Get` | the carried pending login | no write |
| `session` | `Attempt`: `Incr`, `Delete` on failure | attempt counter, pending login | counter only |
| `password` | `Check`: `Incr`, `Delete` on success | attempt counter | counter only |
| `totp` | `check`: `Incr` | used-code marker | counter only |
| `recovery` | `check`: `Incr` | used-code guard | counter only |

- **Decision**: renewal is the only read-then-write, and R-03 changes it. After the fix, every
  `Set` of the module targets a key that no earlier store call has used, since it is derived
  from a fresh 256-bit identifier.
- **Test**: `TestStoreSetCreatesOnly`, in the root package, runs every flow, with a renewal,
  against a `storetest.Recorder`. It asserts that the key of each recorded `Set` appears in no
  earlier call. The flows of `TestStoreHoldsNoSecrets` are moved into a helper that both tests share,
  and the helper gains a renewal. A renewed record is then also scanned for secrets (SC-003 of
  spec 007).

## R-07 Testing approach (FR-013, SC-001 to SC-006; Principle III)

- **Regression test** (`session`, `TestRenewal_DoesNotRestoreDeleted`): a table of deletions,
  each a logout or a new login, crossed with the point where the deletion lands:
  - **Wrapper**: an `interleaved` wrapper of `*memstore.Store` runs the deletion once, at the
    chosen point of the renewing request: before its `Get`, between its `Get` and its write, or
    after its write.
  - **Hooks on both writes**: the hooks wrap both `Set` and `Replace`, so the test compiles, and
    fails, against the unfixed renewal, which calls `Set`.
  - **Expected**: the renewing request has no credentials when the deletion lands before the read
    or between the read and the write. It is authenticated when the deletion lands after the
    write. Every later request with the deleted identifier has no credentials, and after a login
    the new session authenticates.
  - **Expiry in the store**: one more row lets the session's store TTL elapse between the read
    and the write. The hook advances virtual time past the idle timeout. The renewing request has
    no credentials, and the key stays absent (spec Edge Cases).
  - **Anonymous routes**: `TestRenewal_DeletedServedAsAnonymous` sends the renewing request
    through `auth.New` with `auth.WithAnonymous`, with a logout landing between the read and the
    write. The handler runs with no principal (spec User Story 1, scenario 4).
  - **Time**: virtual time (`testing/synctest`) passes the renewal interval without waiting.
- **Concurrency** (`session`, `TestRenewal_ConcurrentLogout`, SC-002): in a `synctest` bubble,
  100 goroutines authenticate one session whose renewal interval has passed, while another
  goroutine revokes it. After all of
  them return, a request with the identifier has no credentials. The suite runs under `-race`.
  Without the fix this test is not certain to fail, because the window is too short. The
  deterministic test above is the regression test.
- **Existing session tests**: `TestRenewal_Writes` counts renewal writes as `OpReplace` instead
  of `OpSet`, and its failing-store case fails `OpReplace`. `TestIssue_Errors` keeps failing
  `OpSet`, because `Issue` still uses `Set`.
- **In-memory store** (`memstore`):
  - `TestStore_Contract` gains `Replace` on a present key, an absent key, `ttl` 0 and a
    cancelled context;
  - `TestExpiry_TTL` checks that `Replace` sets a new expiry and does not revive an expired key;
  - a new `TestReplaceParallel_NeverRevives` runs a `Replace` and a `Delete` of one key
    concurrently 100 times, and finds the key absent after each run (SC-005);
  - `ExampleNew` gains a `Replace` over the existing key and one over an absent key.
- **Test doubles** (`internal/storetest`): `OpReplace`, `Recorder.Replace`, which records the
  value as for `OpSet`, and `Failing.Replace`. The package had 78.9% statement coverage at
  `auth/v0.1.0`, below the per-package 80% of Principle III and with no justification. Its tests
  now also cover the error-forwarding branches, with a `Recorder` over a `Failing` store, and
  `Replace`. This brings the package above 80% rather than adding a justification.
- **Unchanged**: `FuzzSessionCookie` and the session benchmarks, because the cookie parsing and
  the request path without renewal do not change. No new code parses external input.

## R-08 Documentation (FR-011, FR-012)

- **Decision**:
  - the `auth.Store` doc comment gains the `Replace` rules of R-02, and the separation rule
    extends to `Replace`;
  - the package documentation (`doc.go`, "# Store") says that `Store.Replace` must check and
    write atomically, so that renewing a session never restores one that a logout deleted;
  - the `Authenticate` doc comment says that a session deleted while the request renews it gives
    `auth.ErrNoCredentials`;
  - the README changes as R-04 describes, and the CHANGELOG as R-01 drafts.
- **The artifacts of spec 007** stay as they are. They record the design of `auth/v0.1.0`, and
  this feature's [contracts/store.md](contracts/store.md) describes the changes, as spec 005 did
  for spec 004.

## R-09 Dependencies

- **Decision**: no new dependency. The library code uses only what `memstore`, `session` and
  `auth` already import. The README adapter uses go-redis, which is the developer's dependency,
  not the module's.
- **Rationale**: Principle V. `go.mod` and `go.sum` do not change.

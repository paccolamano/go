---

description: "Task list for the session renewal race fix in the auth module"
---

# Tasks: Deleted Sessions Stay Deleted During Renewal (auth)

**Input**: Design documents from `specs/010-fix-session-renewal-race/`

**Prerequisites**: [plan.md](plan.md), [spec.md](spec.md), [research.md](research.md),
[data-model.md](data-model.md), [contracts/](contracts/), [quickstart.md](quickstart.md)

**Tests**: REQUIRED. Constitution Principle III is non-negotiable, and FR-013 asks for them:

- Tests are written first and observed failing. A compilation failure counts as red.
- The fix has a regression test that fails without it (T010, T019).
- Coverage must be ≥ 80% per package, `internal/storetest` included, with `-race -shuffle=on`.
- `memstore.Replace` appears in a runnable example (T020).

Every test task below precedes the implementation it covers.

**Organization**: tasks are grouped by user story (spec.md US1–US3). All paths are relative to the
repository root. All code lives in the existing module `auth/`. Nothing outside `auth/` changes,
apart from ticking the boxes of this file.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: can run in parallel with other [P] tasks of the same phase: different files, and no
  dependency on an incomplete task.
- **[Story]**: US1–US3 from spec.md. Setup, Foundational and Polish tasks have no story label.

## Conventions for every task

- **Commands** run from `auth/` with `GOWORK=off`, unless a task says otherwise. `make` targets run
  from the repository root.
- **Packages**: tests use the external test packages (`session_test`, `memstore_test`,
  `storetest_test`, `auth_test`). Reuse the existing helpers:
  - `auth/session/helpers_test.go`: `mustManager`, `request`, `cookie`, `issue`, `alice`,
    `requestCookie`, and the constants `sessionCookie` and `pendingCookie`;
  - `auth/session/session_test.go`: `errStore`;
  - `auth/integration_test.go`: `newApp`, `withCookies`, `currentCode`, `encodings` and
    `totpSecret`.
- **Test names**: the names of [plan.md](plan.md#source-code-repository-root), chosen so that the
  `-run` patterns of [quickstart.md §4](quickstart.md#4-scenario-checks-per-user-story) select
  them: `TestRenewal_DoesNotRestoreDeleted`, `TestRenewal_DeletedServedAsAnonymous`,
  `TestRenewal_ConcurrentLogout`, `TestStoreSetCreatesOnly` and `TestReplaceParallel_NeverRevives`.
- **Test style**:
  - table-driven, with `t.Run`;
  - virtual time through `testing/synctest`, and no `time.Sleep` outside a `synctest` bubble;
  - goroutines are joined with `sync.WaitGroup` (`wg.Go`), and they call only `t.Error`, never
    `t.Fatal`;
  - contexts come from `t.Context()` (`usetesting`), and requests from the `request` helper or
    `httptest.NewRequestWithContext` (`noctx`);
  - errors are compared with `errors.Is`;
  - helpers take `testing.TB` and call `tb.Helper()` first (`thelper`).
- **Lint**: golangci-lint must report zero findings, and the change adds **no** `//nolint`
  directive. Library code wraps every error it returns from another package's call with `%w`
  (`wrapcheck`). Imports group `github.com/paccolamano/go` last (`goimports`).
- **Documentation**: when [contracts/public-api.md](contracts/public-api.md) gives the text of a
  doc comment, use it verbatim.
- **Commits**: do not commit, push or open a pull request unless the maintainer asks for it.

---

## Phase 1: Setup (Shared Infrastructure)

**Purpose**: work on the feature branch from a known state.

- [X] T001 From the repository root, confirm that the current branch is `fix/auth-session-renewal-race`, which has no commits of its own (`git log --oneline main..HEAD` prints nothing). Confirm also that `git status --short` lists only `go.work.sum` and `specs/010-fix-session-renewal-race/` as untracked, with no change under `auth/`. If the maintainer asks for the Spec Kit branch name, rename the branch before any commit with `git branch -m 010-fix-session-renewal-race`.

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: add the conditional write to the store contract, to the in-memory store and to the test
doubles (FR-001 to FR-004). User Stories 1 and 2 cannot use `Replace` before it exists.
User Story 3, scenarios 1 to 4 (the in-memory store), is verified here. Phase 5 completes it with
the documentation and the upgrade path.

**⚠️ CRITICAL**: no user story work begins until this phase is complete.

- [X] T002 Record the baseline. Run `make check MODULE=auth`, which must report `PASS` for lint and test. Then run `GOWORK=off go test -cover ./internal/storetest ./session ./memstore` from `auth/`, and record the three percentages: at `auth/v0.1.0` they are 78.9%, 91.9% and 98.1%. Run `GOWORK=off go test -run '^$' -bench BenchmarkSessionAuthenticate -benchmem -count 5 ./session`, and record the median ns/op and allocs/op for T027.

### Tests for the conditional write ⚠️ (write first, see them fail)

- [X] T003 [P] Extend the tests of `auth/memstore/memstore_test.go` (FR-001, FR-002, FR-004; US3 scenarios 1–4; SC-005):
  1. **`TestStore_Contract`**, after the check `Set did not replace`:
     - `Replace(ctx, "k", in, time.Minute)` with `in := []byte("v3")` returns `true, nil`;
     - after `in[0] = 'X'`, `Get(ctx, "k")` returns `v3`, so the value was copied;
     - `Replace(ctx, "absent", []byte("v"), time.Minute)` returns `false, nil`, and `Get(ctx, "absent")` then matches `auth.ErrNotFound`.
  2. **`TestStore_Contract`**, next to the existing `ttl` checks: `Replace(ctx, "k", nil, 0)` returns a non-nil error.
  3. **`TestStore_Contract`**, next to the other cancelled-context checks: `Replace(cancelled, "k", nil, time.Minute)` returns an error that matches `context.Canceled`.
  4. **`TestExpiry_TTL`**, before the counter loop, inside the same `synctest` bubble:
     - `Set(ctx, "r", []byte("v"), time.Minute)`, then `time.Sleep(30 * time.Second)`;
     - `Replace(ctx, "r", []byte("w"), time.Minute)` returns `true, nil`;
     - after `time.Sleep(59 * time.Second)`, which is 89 seconds after the `Set`, `Get(ctx, "r")` returns `w`, because `Replace` set a new expiry;
     - after `time.Sleep(time.Second)`, `Replace(ctx, "r", []byte("x"), time.Minute)` returns `false, nil`, because the key expired, and `Get(ctx, "r")` matches `auth.ErrNotFound`.
  5. **New `TestReplaceParallel_NeverRevives`**, 100 iterations on a fresh `memstore.New()`:
     - `Set(ctx, "k", []byte("v"), time.Minute)`;
     - start two goroutines with `wg.Go`: one calls `Replace(ctx, "k", []byte("w"), time.Minute)` and reports a non-nil error with `t.Error`, the other calls `Delete(ctx, "k")` and does the same;
     - after `wg.Wait()`, `Get(ctx, "k")` must match `auth.ErrNotFound`. Report the iteration number on failure.

  Run `GOWORK=off go test ./memstore` and observe it failing to compile: `s.Replace undefined`, and the `var _ auth.Store` assertion still compiles.
- [X] T004 [P] Extend the tests of `auth/internal/storetest/storetest_test.go` (the `Replace` doubles; research R-07 coverage):
  1. **`TestRecorder`**:
     - after the `Get`, call `r.Replace(ctx, "k", []byte("w"), time.Minute)`, which must return `true, nil`;
     - insert `{Op: storetest.OpReplace, Key: "k", Value: []byte("w")}` into the expected calls, between the `OpGet` and the `OpIncr`.
  2. **`TestFailing`**:
     - add `storetest.OpReplace` to `Ops`, and assert that `f.Replace(ctx, "k", []byte("v"), time.Minute)` matches `boom`;
     - after the forwarded `Set`, assert that `(storetest.Failing{Next: f.Next}).Replace(ctx, "k", []byte("w"), time.Minute)` returns `true, nil`.
  3. **New `TestFailing_Forwards`**: `f := storetest.Failing{Next: memstore.New()}`, with no `Ops`, forwards every operation:
     - `Set` of `k` succeeds, and `Get` returns its value;
     - `Replace` returns `true, nil`;
     - `Incr` returns 1;
     - `Delete` succeeds;
     - `Get(ctx, "k")` then matches `auth.ErrNotFound`, so the forwarded error is wrapped and still matches.
  4. **New `TestErrorForwarding`**, table-driven over the five operations, with `boom := errors.New("boom")` and `all := []storetest.Op{OpGet, OpSet, OpReplace, OpDelete, OpIncr}`. For each operation, assert that its error matches `boom` through three stores:
     - `failing := storetest.Failing{Next: memstore.New(), Err: boom, Ops: all}`, directly;
     - `&storetest.Recorder{Next: failing}`, which must also record the call;
     - `storetest.Failing{Next: failing}`, with no `Ops`, which forwards the error.

     These cases cover the error branches that `auth/v0.1.0` left untested.

  Run `GOWORK=off go test ./internal/storetest` and observe it failing to compile: `OpReplace` and `Replace` are undefined.

### Implementation of the conditional write

- [X] T005 In `auth/store.go`, replace the doc comment and the method set of `Store` with the text of [contracts/public-api.md § Store](contracts/public-api.md#store-changed-incompatible), verbatim. That adds `Replace(ctx context.Context, key string, value []byte, ttl time.Duration) (bool, error)` between `Set` and `Delete`, and the rules "Set, Replace and Incr receive ttl > 0", the `Replace` bullet, and "The library never calls Get or Replace on a key it increments, nor Incr on a key it sets or replaces." (FR-001 to FR-003).
- [X] T006 [P] In `auth/memstore/memstore.go`, add `(*Store).Replace` between `Set` and `Delete`, with the doc comment of [contracts/public-api.md § memstore](contracts/public-api.md#storereplace-new-compatible) (research R-05; FR-004):
  1. if `ctx.Err()` is non-nil, return `false, fmt.Errorf("memstore: replace: %w", err)`;
  2. if `ttl <= 0`, return `false, errBadTTL`;
  3. take `now := time.Now()`, lock `s.mu` and defer the unlock;
  4. if the key is absent, return `false, nil`;
  5. if `!now.Before(e.expires)`, delete the entry and return `false, nil`;
  6. otherwise store `entry{value: append([]byte(nil), value...), expires: now.Add(ttl)}`, call `s.sweep(now)`, and return `true, nil`.
- [X] T007 In `auth/internal/storetest/storetest.go`, after T005 (research R-07):
  - add `OpReplace Op = "replace"` to the constant block, after `OpSet`, and let `gofmt` realign the block;
  - change the `Call` comment to "Value is set only for OpSet and OpReplace.";
  - add `(*Recorder).Replace`, after `Set`. It records `Call{Op: OpReplace, Key: key, Value: slices.Clone(value)}`, forwards the call to `Next`, and returns `false, fmt.Errorf("storetest: %w", err)` on an error;
  - add `(Failing).Replace`, after `Set`. It returns `false, f.Err` when `f.fails(OpReplace)`, and otherwise forwards the call, wrapping the error the same way.

  Doc comments: "Replace records and forwards the call." and "Replace fails or forwards the call."
- [X] T008 Run `GOWORK=off go vet ./... && GOWORK=off go test -race ./...` from `auth/`. Every package must pass, `session` and the root package unchanged. Then run `GOWORK=off go test -cover ./internal/storetest ./memstore`: `internal/storetest` must be at 80% or above, and `memstore` must not drop below its T002 value.

**Checkpoint**: the store contract has `Replace`, and every user story can begin.

---

## Phase 3: User Story 1 - Logging out ends the session even while a request renews it (Priority: P1) 🎯 MVP

**Goal**: renewal writes with `Replace`, so a logout that lands between the renewal's read and write
deletes the session for good (FR-006 to FR-010).

**Independent Test**: with virtual time, the `interleaved` store runs a logout between the `Get` and
the write of a renewal. The renewing request and every later request with the identifier have no
credentials.

### Tests for User Story 1 ⚠️ (write first, see them fail)

- [X] T009 [US1] In `auth/session/helpers_test.go`, add the `interleaved` store wrapper (research R-07):
  - a type `point int`, with the constants `beforeRead`, `betweenReadAndWrite` and `afterWrite`;
  - `type interleaved struct { *memstore.Store; at point; run func() }`;
  - `fire()`, which calls `run` once: it saves `run`, sets the field to nil, then calls the saved function, so that store calls made by the deletion itself do not fire again;
  - `Get`, which calls `fire()` first when `at == beforeRead`, then forwards;
  - `Set` and `Replace`, which call `fire()` before forwarding when `at == betweenReadAndWrite`, and after forwarding when `at == afterWrite`, and return the forwarded result.

  Wrapping both writes makes the test compile, and fail, against the unfixed renewal, which calls `Set`. A doc comment explains that the wrapper simulates a concurrent request whose store calls land at the chosen point of a renewal. Add the imports `context`, `time` and `github.com/paccolamano/go/auth/memstore`.
- [X] T010 [US1] In `auth/session/session_test.go`, add `TestRenewal_DoesNotRestoreDeleted`, after `TestRenewal_Writes` (FR-006, FR-007, FR-013; US1 scenarios 1–3; SC-001). It is a table over `name`, `at point`, `del func(t *testing.T, m *session.Manager, c *http.Cookie) *http.Cookie` and `wantRenewed bool`, with these rows:
  - `"logout before read"`: `beforeRead`, `wantRenewed: false`;
  - `"logout between read and write"`: `betweenReadAndWrite`, `wantRenewed: false`;
  - `"logout after write"`: `afterWrite`, `wantRenewed: true`;
  - `"store expiry between read and write"`: `betweenReadAndWrite`, `wantRenewed: false`.

  The logout rows call `m.Revoke(httptest.NewRecorder(), request(t, c))`, report its error with `t.Error`, and return nil. The expiry row calls `time.Sleep(30 * time.Minute)`, past the idle timeout, and returns nil.

  Each subtest runs in `synctest.Test`:
  1. Create `store := &interleaved{Store: memstore.New()}`, `m := mustManager(t, store)` and `c := issue(t, m, alice())`.
  2. Call `time.Sleep(8 * time.Minute)`, past the 7m30s renewal interval.
  3. Arm the wrapper: set `store.at` from the row, and set `store.run` to a function that calls `del(t, m, c)` and keeps the returned cookie.
  4. Call `p, err := m.Authenticate(request(t, c))`:
     - with `wantRenewed`, `err` must be nil and `p.Subject` must be `"user-1"`;
     - otherwise, `err` must match `auth.ErrNoCredentials`.
  5. Assert that `store.run == nil`, so the deletion ran.
  6. A second `m.Authenticate(request(t, c))` must match `auth.ErrNoCredentials`.
- [X] T011 [US1] In `auth/session/session_test.go`, add `TestRenewal_DeletedServedAsAnonymous` (US1 scenario 4), in `synctest.Test`:
  - set up the store, the manager, the session cookie and the 8-minute sleep as in T010;
  - arm the wrapper at `betweenReadAndWrite`, with a deletion that calls `m.Revoke`;
  - `mw, err := auth.New(m, auth.WithAnonymous())`;
  - serve `request(t, c)` through `mw(h)`, where `h` sets `served = true` and `_, hasPrincipal = auth.FromContext(r.Context())`.

  Assert that the response code is `http.StatusOK`, that `served` is true, and that `hasPrincipal` is false.
- [X] T012 [US1] In `auth/session/session_test.go`, add `TestRenewal_ConcurrentLogout` (FR-007; SC-002; spec Edge Cases), in `synctest.Test`, with a plain `memstore.New()`. Add the import `sync`.
  1. Issue a session `c`, then call `time.Sleep(8 * time.Minute)`.
  2. **Concurrent renewals, no logout**: with `wg.Go`, start 100 goroutines that each call `m.Authenticate(request(t, c))` and report any error with `t.Error`. After `wg.Wait()`, `m.Authenticate(request(t, c))` must succeed.
  3. **Concurrent renewals and a logout**: call `time.Sleep(8 * time.Minute)` again, then start 100 goroutines that each call `m.Authenticate(request(t, c))`. Each reports, with `t.Error`, any error that does not match `auth.ErrNoCredentials`. One more goroutine calls `m.Revoke(httptest.NewRecorder(), request(t, c))` and reports its error. After `wg.Wait()`, `m.Authenticate(request(t, c))` must match `auth.ErrNoCredentials`.

  Without the fix this test is not certain to fail, because the window is too short. T010 is the regression test, and this one guards the outcome under `-race`.
- [X] T013 [US1] In `auth/session/session_test.go`, adapt `TestRenewal_Writes` (FR-009, FR-010; SC-003, SC-004):
  - replace the 4 occurrences of `storetest.OpSet` inside this function with `storetest.OpReplace`: the three counts, and the `Ops` of the failing manager. Leave `TestIssue_Errors` unchanged, because `Issue` keeps `Set`;
  - in the failing-renewal subtest, after the `errStore` assertion, also serve the request through `mw, _ := auth.New(failing)`, wrapping `http.NotFoundHandler()`. Assert status 500 (`http.StatusInternalServerError`), never 401.
- [X] T014 [P] [US1] In `auth/integration_test.go`, add the FR-008 invariant (research R-06):
  1. Move the body of `TestStoreHoldsNoSecrets` into a helper `runEveryFlow(t *testing.T, rs *storetest.Recorder) (secrets, inKeys []string)`. The helper calls `t.Helper()` first. It runs every flow, from the attempt limit to the recovery code, then checks that the flows ran (the existing `len(calls) == 0 || len(cookies) < 3` check). It returns the `secrets` and `inKeys` slices built at the end of the current body.
  2. In the helper, right after `collect(done)` and before the `Revoke`, add a renewal:
     - call `time.Sleep(8 * time.Minute)`;
     - `if _, err := a.sessions.Authenticate(withCookies(t, done)); err != nil { t.Fatalf("renewal: %v", err) }`.
  3. `TestStoreHoldsNoSecrets` keeps its `synctest.Test`, calls `runEveryFlow(t, rs)`, and runs its existing scan loops over `rs.Calls()`.
  4. Add `TestStoreSetCreatesOnly`, in `synctest.Test`:
     - call `runEveryFlow(t, rs)` with `rs := &storetest.Recorder{Next: memstore.New()}`;
     - walk `rs.Calls()` with a `seen` set: every `OpSet` must target a key not in `seen` (report the key with `t.Errorf`), and every call adds its key to `seen`;
     - assert `rs.Count(storetest.OpReplace) >= 1`, so a renewal ran.
- [X] T015 [US1] Run `GOWORK=off go test -race ./session .` from `auth/` and observe the expected failures:
  - `TestRenewal_DoesNotRestoreDeleted/logout_between_read_and_write` and `.../store_expiry_between_read_and_write` fail: the renewing request is authenticated;
  - the `before read` and `after write` rows pass;
  - `TestRenewal_DeletedServedAsAnonymous` fails: the handler sees a principal;
  - `TestRenewal_Writes` fails: 0 replaces at 7m30s;
  - `TestStoreSetCreatesOnly` fails: a `Set` targets the session key read earlier, and there are no `OpReplace` calls;
  - `TestRenewal_ConcurrentLogout` may pass, as noted in T012.

  Do not continue until these failures are seen.

### Implementation for User Story 1

- [X] T016 [US1] Change renewal in `auth/session/session.go` and issuance in `auth/session/issue.go` (research R-03):
  1. **The doc comment**: replace the doc comment of `Authenticate` with the text of [contracts/public-api.md § session](contracts/public-api.md#managerauthenticate-doc-comment-changed-signature-unchanged), verbatim.
  2. **The renewal branch** (`if now.Sub(lastWrite) >= m.idle/4`):
     - set `rec.RW = now.Unix()`;
     - `b, err := encodeRecord(rec)`, returning `nil, err` on an error;
     - add the comment `// Replace, never Set: a session deleted since the Get, by a logout or a new login, must stay deleted.`;
     - `ok, err := m.store.Replace(ctx, key, b, m.ttl(rec, now))`;
     - on `err != nil`, return `nil, fmt.Errorf("session: renew: %w", err)`;
     - on `!ok`, return `nil, auth.ErrNoCredentials`.
  3. **The `write` method**: replace it with `ttl(rec sessionRecord, now time.Time) time.Duration`, with the doc comment `// ttl returns the store TTL of a session record: min(rw + idle, exp) − now.`. It keeps the same computation, and returns `time.Second` when the result is not positive.
  4. **`Issue`**: replace the `m.write(...)` call with:
     - `b, err := encodeRecord(rec)`, returning `err` on an error;
     - `if err := m.store.Set(r.Context(), storekey.Key("session", id), b, m.ttl(rec, now)); err != nil { return fmt.Errorf("session: issue: %w", err) }`.

  Nothing else in the package changes.
- [X] T017 [US1] In `auth/CHANGELOG.md`, under `## [Unreleased]`, add a `### Fixed` section with the `session` entry drafted in [research R-01](research.md#r-01-release-level-and-api-compatibility), verbatim (FR-012).
- [X] T018 [US1] Run `GOWORK=off go test -race -shuffle=on ./...` from `auth/`. Every test of T010–T014 must pass, and every existing test must still pass. Then run `make check MODULE=auth` from the repository root, which must report `PASS`.

**Checkpoint**: a logout during a renewal deletes the session for good. This is the MVP.

---

## Phase 4: User Story 2 - A new login retires the previous session even while a request renews it (Priority: P1)

**Goal**: the same guarantee when the deletion comes from `Issue` on a request that carries the
renewed session (FR-007; US2 scenario 1).

**Independent Test**: the rows of T010 with a new login in place of the logout.

### Tests for User Story 2 ⚠️ (observe them failing against the unfixed renewal)

- [X] T019 [US2] Add three rows to the table of `TestRenewal_DoesNotRestoreDeleted` in `auth/session/session_test.go`: `"login before read"` (`beforeRead`), `"login between read and write"` (`betweenReadAndWrite`) and `"login after write"` (`afterWrite`, `wantRenewed: true`):
  - their `del` returns `issue(t, m, alice(), c)`: a new login on a request that carries `c`;
  - when `del` returned a cookie, the subtest also asserts, after the second `Authenticate` of `c`, that `m.Authenticate(request(t, newCookie))` succeeds with subject `"user-1"`.

  The fix of T016 is already in place, so observe the regression as follows, from the repository root:
  1. run `git stash push -- auth/session/session.go auth/session/issue.go`;
  2. run `(cd auth && GOWORK=off go test -run 'TestRenewal_DoesNotRestoreDeleted' ./session)`, and see `login_between_read_and_write` fail;
  3. run `git stash pop`, then the same command, and see every row pass.

  Never leave the stash applied.

**Checkpoint**: both deletions are safe. User Stories 1 and 2 together are the complete fix.

---

## Phase 5: User Story 3 - Store adapters provide the conditional write (Priority: P2)

**Goal**: a developer who upgrades knows what to add to an adapter. The README shows the Redis
implementation, and the package documentation and the changelog explain the change (FR-005,
FR-011, FR-012; US3 scenario 5). Scenarios 1 to 4 were verified in Phase 2 (T003).

**Independent Test**: the example and the documentation describe `Replace` exactly as the contract
does, and the README adapter implements every method of `auth.Store`.

- [X] T020 [P] [US3] In `auth/memstore/example_test.go`, extend `ExampleNew`. After printing the greeting, call `ok, _ := s.Replace(ctx, "greeting", []byte("hi"), time.Minute)` and print `ok`, then `ok, _ = s.Replace(ctx, "missing", []byte("x"), time.Minute)` and print `ok`. Before the counter loop, call `v, _ = s.Get(ctx, "greeting")` and print it. The expected output becomes `hello`, `true`, `false`, `hi`, `1`, `2`, `3`. Run `GOWORK=off go test -run ExampleNew -v ./memstore`.
- [X] T021 [P] [US3] In `auth/doc.go`, section `# Store`, replace the paragraph with the text of [contracts/public-api.md § Package documentation](contracts/public-api.md#package-documentation-docgo--store-changed), verbatim (FR-011).
- [X] T022 [P] [US3] In `auth/README.md`, section "Store" (research R-04; FR-005):
  - change "adapt Redis in about twenty lines" to "adapt Redis in about thirty lines";
  - in the adapter, between `Set` and `Delete`, add:

    ```go
    // Replace writes only over an existing key: SET with XX.
    func (s redisStore) Replace(ctx context.Context, key string, value []byte, ttl time.Duration) (bool, error) {
    	return s.C.SetXX(ctx, key, value, ttl).Result()
    }
    ```

  - in the paragraph after the adapter, after the sentence that ends "if the client died between them.", add: "`SET` with `XX` checks that the key exists and writes it in one command, so a session renewed while a logout deletes it stays deleted; `SetXX` reports `false`, with no error, when the key is absent."
- [X] T023 [US3] In `auth/CHANGELOG.md`, under `## [Unreleased]`, add a `### Changed` section before the `### Fixed` section of T017, with the **Breaking** entry drafted in [research R-01](research.md#r-01-release-level-and-api-compatibility), verbatim (FR-012).
- [X] T024 [US3] (Skipped on 2026-10-23: the maintainer did not ask for it.) Optional: skip this task unless the maintainer asks for it. It needs go-redis v9 in the module cache, or network access. Run the README adapter check of [quickstart.md §6](quickstart.md#6-optional-the-readme-redis-adapter-compiles) in a scratch directory outside the repository, with the adapter copied from `auth/README.md`. `GOWORK=off go vet .` must print nothing. Never commit the scratch module or its `replace` directive.

**Checkpoint**: the upgrade path is documented, and every story is complete.

---

## Phase 6: Polish & Cross-Cutting Concerns

**Purpose**: verify the whole change against the contracts and the quickstart, and run the gates.

- [X] T025 [P] Verify the exported surface against [contracts/public-api.md](contracts/public-api.md). From `auth/`, run each of these, and compare the output with the contract:
  - `GOWORK=off go doc -all . Store` shows the five methods and the new doc comment;
  - `GOWORK=off go doc ./memstore Store.Replace` shows the new doc comment;
  - `GOWORK=off go doc ./session Manager.Authenticate` shows the new doc comment.

  Then run `GOWORK=off go test -coverprofile=c.out ./... && go tool cover -func=c.out`. `Replace` in `memstore.go` and `storetest.go`, and `Authenticate`, `ttl` and `Issue` in `session/`, must not be at 0%. Delete `c.out` afterwards.
- [X] T026 [P] From `auth/`, check the dependencies and the platforms:
  - `GOWORK=off go mod tidy -diff` prints nothing;
  - `git diff --exit-code -- go.mod go.sum` passes;
  - for every `os` in linux, darwin and windows, and every `arch` in amd64 and arm64, `GOWORK=off GOOS=$os GOARCH=$arch go vet ./...` passes.
- [X] T027 [P] Run `GOWORK=off go test -run '^$' -bench BenchmarkSessionAuthenticate -benchmem -count 5 ./session` from `auth/`. allocs/op must equal the baseline of T002, and the median ns/op must be close to it, since the request path without renewal does not change. Record both medians for the pull request description (T030).
- [X] T028 Run the checks of [quickstart.md](quickstart.md) §2, §3, §4 (one row at a time) and §5. Every command must pass, and every package of §2 must be at 80% or above.
- [X] T029 Run `make check MODULE=auth` from the repository root:
  - golangci-lint reports zero findings, and `git diff -- auth | grep -c 'nolint'` prints 0;
  - `go test -race -shuffle=on` passes, with coverage ≥ 80%. Compare the module and package percentages with the baseline of T002.
- [X] T030 Draft the pull request for the changes in `auth/` and `specs/010-fix-session-renewal-race/`, for the maintainer, and do not open it without their go-ahead:
  - **Title**: `fix(auth)!: keep sessions deleted while a request renews them`.
  - **Description**:
    - the defect, and how a logout or a new login could leave a stolen session alive;
    - the breaking change: `auth.Store` gains `Replace`, with the one-line Redis implementation;
    - the coverage of `internal/storetest`, raised from 78.9%;
    - the benchmark medians of T027;
    - the attribution line required for pull request descriptions.

---

## Dependencies & Execution Order

### Phase dependencies

- **Setup (Phase 1)** and **Foundational (Phase 2)** run first: T001 → T002 → T003–T008.
- **US1 (Phase 3)** depends on Phase 2: renewal calls `Replace`, and the tests use `OpReplace`.
- **US2 (Phase 4)** depends on US1. Its rows extend the table of T010, and its red step stashes the
  fix of T016.
- **US3 (Phase 5)** depends on Phase 2 only, since its documentation describes `Replace`. T023
  follows T017, because both edit `CHANGELOG.md`.
- **Polish (Phase 6)** depends on every story.

The recommended order is the priority order: US1 → US2 → US3. Each phase leaves
`make check MODULE=auth` green.

### Within each phase

- Test tasks come first, and are observed failing. In Phase 2 that is a compilation failure. In
  Phase 3 it is T015. In Phase 4, the red step uses the stash of T019. The documentation tasks of
  US3 have no red step: the example output guards T020.
- **Shared files**: tasks on the same file never run in parallel.

  | File | Edited by |
  |------|-----------|
  | `auth/store.go` | T005 |
  | `auth/memstore/memstore.go` | T006 |
  | `auth/memstore/memstore_test.go` | T003 |
  | `auth/memstore/example_test.go` | T020 |
  | `auth/internal/storetest/storetest.go` | T007 |
  | `auth/internal/storetest/storetest_test.go` | T004 |
  | `auth/session/helpers_test.go` | T009 |
  | `auth/session/session_test.go` | T010, T011, T012, T013, T019 |
  | `auth/session/session.go`, `auth/session/issue.go` | T016 |
  | `auth/integration_test.go` | T014 |
  | `auth/doc.go` | T021 |
  | `auth/README.md` | T022 |
  | `auth/CHANGELOG.md` | T017, T023 |

### Parallel opportunities

- **Phase 2**: T003 and T004 together (different files). Then T005 and T006 together, then T007,
  which needs T005, then T008.
- **US1**: T009, then T010 → T011 → T012 → T013 (same file), with T014 in parallel with all of
  them. Then T015, T016, T017 and T018 in sequence.
- **US3**: T020, T021 and T022 together. Then T023.
- **Polish**: T025, T026 and T027 together. Then T028, T029 and T030.

---

## Parallel Example: Phase 2

```bash
# Write the failing tests together (different files):
Task: "T003 Replace tests in auth/memstore/memstore_test.go"
Task: "T004 Replace and error-forwarding tests in auth/internal/storetest/storetest_test.go"

# Then implement:
Task: "T005 Store.Replace in auth/store.go"
Task: "T006 (*memstore.Store).Replace in auth/memstore/memstore.go"
# then T007 (storetest doubles, needs T005) → T008 (vet, tests, coverage)
```

## Parallel Example: User Story 3

```bash
Task: "T020 ExampleNew with Replace in auth/memstore/example_test.go"
Task: "T021 # Store paragraph in auth/doc.go"
Task: "T022 Redis adapter Replace in auth/README.md"
# then T023 (CHANGELOG Changed, after T017)
```

---

## Requirement Coverage

| Requirement | Tasks |
|-------------|-------|
| FR-001 | T003.1, T003.4, T005, T006 |
| FR-002 | T003.2, T003.3, T006 |
| FR-003 | T005 |
| FR-004 | T003, T006 |
| FR-005 | T022, T024 |
| FR-006 | T010, T011, T016 |
| FR-007 | T010, T012, T016, T019 |
| FR-008 | T014, T016 |
| FR-009 | T013, T016, T027 |
| FR-010 | T013, T016 |
| FR-011 | T005, T016, T021, T025 |
| FR-012 | T017, T023 |
| FR-013 | T009, T010, T015, T019 |
| SC-001 | T010, T019 |
| SC-002 | T012 |
| SC-003 | T013 |
| SC-004 | T013 |
| SC-005 | T003.5 |
| SC-006 | T008, T025, T028, T029, and `gorelease` in CI |

Spec edge cases:

| Edge case | Tasks |
|-----------|-------|
| two concurrent renewals, no deletion | T012.2 |
| the store TTL elapses between the read and the write | T003.4, T010 |
| the store fails during the conditional write | T013 |
| a cancelled context | T003.3, plus the existing cancelled-context case of `TestAuthenticate_StoreError` |
| pending logins are unchanged | T014 (no `Set` over a used key), and the existing pending tests |
| issuance keeps the unconditional write | T013 (`TestIssue_Errors` keeps `OpSet`), T014 |

---

## Implementation Strategy

### MVP first (User Story 1)

1. Phase 1 and Phase 2: T001–T008. `Replace` exists in the contract, in `memstore` and in the
   test doubles.
2. Phase 3, US1: T009–T018. Renewal uses `Replace`, and a logout during a renewal is final.
3. **Stop and validate**: run `make check MODULE=auth`.

### Incremental delivery

1. US2 (P1): T019. The regression test also covers a new login.
2. US3 (P2): T020–T024. The example, the package documentation, the README adapter and the
   breaking-change entry.
3. Polish: contracts, platforms, benchmark, quickstart, gates, and the PR draft.

The fix ships as one pull request, because US1 without US3's documentation would leave adapter
authors without upgrade instructions. The release `auth/v0.2.0` (research R-01) is prepared in a
separate PR after the squash merge, as for v0.1.0. The changelog entries stay under
`[Unreleased]` until then.

---

## Notes

- Every task stays within `auth/`, except T024, which uses a scratch directory, and this file.
- Never weaken a test to make it pass. Fix the implementation, or update the spec and the
  contracts first if the behavior itself must change.
- The reproduction probe and the earlier spike live only in the session scratchpad, or were
  reverted. Re-implement from these tasks; do not copy files from them.

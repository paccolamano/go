---

description: "Task list for revoking every session of a subject in the auth module"
---

# Tasks: Revoke Every Session of a Subject (auth)

**Input**: Design documents from `specs/011-revoke-subject-sessions/`

**Prerequisites**: [plan.md](plan.md), [spec.md](spec.md), [research.md](research.md),
[data-model.md](data-model.md), [contracts/](contracts/), [quickstart.md](quickstart.md)

**Tests**: REQUIRED. Constitution Principle III is non-negotiable, and FR-020 asks for them:

- Tests are written first and observed failing. A compilation failure counts as red. Where a
  story adds no library code, a temporary mutation shows that its tests guard the behavior
  (T018, T026).
- Coverage must be ≥ 80% per package, with `-race -shuffle=on`.
- `RevokeSubject` appears in a runnable example (T009).

Every test task below precedes the implementation it covers.

**Organization**: tasks are grouped by user story (spec.md US1–US4). All paths are relative to the
repository root. All code lives in the existing module `auth/`. Nothing outside `auth/` changes,
apart from ticking the boxes of this file.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: can run in parallel with other [P] tasks of the same phase: different files, and no
  dependency on an incomplete task.
- **[Story]**: US1–US4 from spec.md. Setup, Foundational and Polish tasks have no story label.

## Conventions for every task

- **Commands** run from `auth/` with `GOWORK=off`, unless a task says otherwise. `make` targets run
  from the repository root.
- **Packages**: tests use the external test packages (`session_test`, `auth_test`), except
  `auth/session/record_internal_test.go`, which is in package `session`. Reuse the existing
  helpers:
  - `auth/session/helpers_test.go`: `mustManager`, `request`, `cookie`, `issue`, `alice`,
    `requestCookie`, the `interleaved` wrapper with its `point` constants, and the constants
    `sessionCookie` and `pendingCookie`;
  - `auth/session/session_test.go`: `errStore`;
  - `auth/session/pending_test.go`: `issuePending`, `mustPending` and `pass`;
  - `auth/integration_test.go`: `newApp`, `runEveryFlow` and `withCookies`.
- **Test names**: the names of [research R-10](research.md#r-10-testing-approach-fr-020-sc-001-to-sc-009-principle-iii),
  chosen so that the `-run` patterns of
  [quickstart.md §4](quickstart.md#4-scenario-checks-per-user-story) select them. The new tests
  live in a new file, `auth/session/revocation_test.go`, as `TestRevokeSubject_<Scenario>`.
- **Test style**:
  - table-driven where a test has cases, with `t.Run`;
  - virtual time through `testing/synctest`, and no `time.Sleep` outside a `synctest` bubble;
  - goroutines are joined with `sync.WaitGroup` (`wg.Go`), and they call only `t.Error`, never
    `t.Fatal`;
  - contexts come from `t.Context()` (`usetesting`), and requests from the `request` helper or
    `httptest.NewRequestWithContext` (`noctx`);
  - errors are compared with `errors.Is`;
  - helpers take `testing.TB` and call `tb.Helper()` first (`thelper`).
- **Lint**: golangci-lint must report zero findings, and the change adds **no** `//nolint`
  directive. Library code wraps every error it returns from another package's call with `%w`
  (`wrapcheck`): wrap the store error once, in `generation`, and let callers add their prefix.
  Imports group `github.com/paccolamano/go` last (`goimports`).
- **Documentation**: when [contracts/public-api.md](contracts/public-api.md) gives the text of a
  doc comment, use it verbatim.
- **Mutations**: a task that asks for a temporary mutation restores the file right after, and
  `git diff` must show no trace of it.
- **Commits**: do not commit, push or open a pull request unless the maintainer asks for it.
- **The spike**: a scratch copy of the module, made during planning, implemented this design. Do
  not copy files from it; re-implement from these tasks.

---

## Phase 1: Setup (Shared Infrastructure)

**Purpose**: work on the feature branch from a known state.

- [X] T001 From the repository root:
  1. confirm that `git status --short` lists only `go.work.sum` and
     `specs/011-revoke-subject-sessions/` as untracked, with no change under `auth/`;
  2. confirm that `main` contains the 0.2.0 release: `git merge-base --is-ancestor auth/v0.2.0 main`
     succeeds;
  3. create the branch with `git switch -c 011-revoke-subject-sessions main`. The untracked
     spec directory follows the switch;
  4. confirm that `git log --oneline -1` shows `chore(auth): prepare release 0.2.0 (#20)`.

  The working tree was on `auth-release-0.2.0`, whose release was squash-merged into `main`; do
  not build on it.

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: the record field and the test helpers that every story uses (data-model §2, research
R-06, R-10).

**⚠️ CRITICAL**: no user story work begins until this phase is complete.

- [X] T002 Record the baseline. Run `make check MODULE=auth`, which must report `PASS` for lint and test. Then run `GOWORK=off go test -cover . ./session`, and record both percentages: at `auth/v0.2.0` they are 99.2% and 91.5%. Run `GOWORK=off go test -run '^$' -bench BenchmarkSessionAuthenticate -benchmem -count 5 ./session`, and record the median ns/op, B/op and allocs/op for T032. At `auth/v0.2.0` they are about 3.0 µs, 833 B and 15 allocs.
- [X] T003 [P] In `auth/session/helpers_test.go`, extend the test wrappers (research R-10):
  1. add the import `strings`;
  2. add the constant `beforeRevocationRead` to the `point` block, after `afterWrite`;
  3. add `// revocationPrefix starts the store key of a subject's revocation state.` and `const revocationPrefix = "auth:session-gen:"`;
  4. in `(*interleaved).Get`, fire when `s.at == beforeRead || s.at == beforeRevocationRead && strings.HasPrefix(key, revocationPrefix)`, then forward as before. Extend the comment of `interleaved` to name the new point: "or before the read of a revocation state";
  5. add, at the end of the file:
     - `// failingRevocation fails the reads and writes of revocation states with errStore, and forwards every other call.`
     - `type failingRevocation struct{ auth.Store }`;
     - `Get`, which returns `nil, errStore` when the key starts with `revocationPrefix`, and otherwise forwards to the embedded store;
     - `Set`, which returns `errStore` for those keys, and otherwise forwards.

  Run `GOWORK=off go vet ./session`: it must pass.
- [X] T004 [P] In `auth/session/record_internal_test.go`, add two cases to the table of `TestRecord_Fields` (data-model §2):
  - `"session after a revocation"`: `sessionRecord{V: 1, Sub: "u", AMR: []string{"pwd"}, IAT: 1, EXP: 2, RW: 3, Gen: "g"}`, with the keys `amr`, `exp`, `gen`, `iat`, `rw`, `sub`, `v`;
  - `"pending after a revocation"`: `pendingRecord{V: 1, Sub: "u", AMR: []string{"pwd"}, IAT: 1, Gen: "g"}`, with the keys `amr`, `gen`, `iat`, `sub`, `v`.

  The two existing cases keep their keys, which proves that an empty `Gen` is omitted. Run `GOWORK=off go test ./session` and observe it failing to compile: `unknown field Gen`. Run `gofmt -w` on the file, since the longer names realign the table.
- [X] T005 In `auth/session/record.go` (research R-06):
  - add `Gen string \`json:"gen,omitempty"\`` as the last field of `sessionRecord` and of `pendingRecord`;
  - extend the comment of `sessionRecord` with "Gen is the revocation state of the subject when the session was issued.", and the comment of `pendingRecord` with "Gen is as in sessionRecord.";
  - keep `recordVersion` at 1, and leave `encodeRecord` and `decodeRecord` unchanged.

  Run `GOWORK=off go test -race ./session`: every test passes.

**Checkpoint**: the records can carry `gen`, and the test wrappers can target revocation states.

---

## Phase 3: User Story 1 - A password change ends the attacker's session (Priority: P1) 🎯 MVP

**Goal**: `RevokeSubject` exists, issuance records the revocation state, and authentication rejects
a session whose subject was revoked after it was issued (FR-001, FR-002, FR-004, FR-005, FR-007,
FR-009, FR-010).

**Independent Test**: with virtual time, issue sessions for two subjects, revoke one subject, issue
a new session for it in the same request, and authenticate each session.

### Tests for User Story 1 ⚠️ (write first, see them fail)

- [X] T006 [US1] Create `auth/session/revocation_test.go`, package `session_test`, with two helpers and four tests:
  - **Helpers**:
    - `func bob() *auth.Principal { return &auth.Principal{Subject: "user-2", Methods: []string{"pwd"}} }`;
    - `authenticates(tb testing.TB, m *session.Manager, c *http.Cookie) bool`, with the comment "authenticates reports whether c authenticates a request, and fails the test on any outcome other than a principal or auth.ErrNoCredentials." It calls `tb.Helper()`, then `m.Authenticate(request(tb, c))`. It returns true for a nil error, false for an error matching `auth.ErrNoCredentials`, and calls `tb.Fatalf("Authenticate: %v", err)` otherwise.
  - **`TestRevokeSubject_EndsSessions`** (US1 scenarios 1 and 3; FR-002, FR-004; SC-001, SC-002), in `synctest.Test`:
    1. `m := mustManager(t, memstore.New())`; issue `a1` and `a2` for `alice()`;
    2. `time.Sleep(8 * time.Minute)`, so `a1` and `a2` are due for renewal; then issue `a3` for `alice()` and `b` for `bob()`;
    3. `m.RevokeSubject(t.Context(), "user-1")` must return nil;
    4. none of `a1`, `a2`, `a3` authenticates (report each with `t.Errorf` and its index); `b` authenticates.
  - **`TestRevokeSubject_ThenIssue`** (US1 scenario 2; FR-004; SC-002), in `synctest.Test`:
    1. issue `current` for `alice()`; build `r := request(t, current)`;
    2. `m.RevokeSubject(r.Context(), "user-1")` must return nil;
    3. `fresh := issue(t, m, alice(), current)`, a new login on the request that carries `current`;
    4. `current` does not authenticate;
    5. for `elapsed` from 20 minutes while below 8 hours, in steps of 20 minutes: `time.Sleep(20 * time.Minute)`, then `fresh` must authenticate (report `elapsed` on failure);
    6. after one more `time.Sleep(20 * time.Minute)`, at 8 hours, `fresh` no longer authenticates: its absolute lifetime ended.
  - **`TestRevokeSubject_ServedAsAnonymous`** (US1 scenario 4), without `synctest`: issue `c`, revoke `user-1`, build `mw, err := auth.New(m, auth.WithAnonymous())`, and serve `request(t, c)` through a handler that sets `served = true` and `anonymous = !ok` from `auth.FromContext(r.Context())`. Assert that `served` and `anonymous` are both true.
  - **`TestRevokeSubject_SameInstant`** (SC-004; FR-007), in `synctest.Test`:
    1. `start := time.Now()`; `before := issue(t, m, alice())`; revoke `user-1`; `after := issue(t, m, alice())`;
    2. if `!time.Now().Equal(start)`, call `t.Fatal("virtual time moved")`;
    3. `before` does not authenticate, and `after` does.

  Imports for the file: `context`, `errors`, `fmt`, `net/http`, `net/http/httptest`, `strings`, `sync`, `testing`, `testing/synctest`, `time`, and the module packages `auth`, `internal/storekey`, `internal/storetest`, `memstore` and `session`. Drop the ones that no test of this phase uses yet, and add them back in the later phases.
- [X] T007 [US1] In `auth/session/revocation_test.go`, add `TestRevokeSubject_Interleavings` (FR-005, FR-020; data-model §4). It is a table over `name string`, `op string`, `at point`, `served bool` (the outcome of the interleaved operation itself) and `later bool` (whether the session works afterwards), with these rows:

  | name | op | at | served | later |
  |------|----|----|--------|-------|
  | `issue: revocation before the revocation read` | `issue` | `beforeRevocationRead` | true | true |
  | `issue: revocation between the revocation read and the write` | `issue` | `betweenReadAndWrite` | true | false |
  | `issue: revocation after the write` | `issue` | `afterWrite` | true | false |
  | `renewal: revocation before the session read` | `renew` | `beforeRead` | false | false |
  | `renewal: revocation before the revocation read` | `renew` | `beforeRevocationRead` | false | false |
  | `renewal: revocation between the revocation read and the write` | `renew` | `betweenReadAndWrite` | true | false |
  | `renewal: revocation after the write` | `renew` | `afterWrite` | true | false |

  Each subtest runs in `synctest.Test`:
  1. `s := &interleaved{Store: memstore.New()}`, `m := mustManager(t, s)`, and `revoke := func() { if err := m.RevokeSubject(t.Context(), "user-1"); err != nil { t.Error(err) } }`;
  2. `switch c.op`:
     - `"issue"`: arm with `s.at, s.run = c.at, revoke`; `sc := issue(t, m, alice())`; `authenticates(t, m, sc)` must equal `c.later`;
     - `"renew"`: `sc := issue(t, m, alice())`; `time.Sleep(8 * time.Minute)`; arm the wrapper; the first `authenticates(t, m, sc)` must equal `c.served`, and a second one must equal `c.later`;
  3. after the switch, `s.run` must be nil, or `t.Fatal("the revocation never ran")`.

  Phase 5 adds a `pending` row and case (T021).
- [X] T008 [US1] In `auth/session/session_test.go`, change `TestAuthenticate_Principal` (FR-010; SC-005): the request must make exactly two store calls, both `OpGet`. Replace the check with `if n := rec.Count(storetest.OpGet); n != 2 || len(rec.Calls()) != 2 { t.Fatalf("store calls = %v, want exactly two Gets: the session and its subject's revocation state", rec.Calls()) }`.
- [X] T009 [US1] In `auth/session/example_test.go`, add `ExampleManager_RevokeSubject` after `ExampleManager_Pending`:
  1. `sessions, _ := session.New(memstore.New())` and `ctx := context.Background()`;
  2. a local `signIn := func() *http.Request` that issues a session for `&auth.Principal{Subject: "user-42", Methods: []string{"pwd"}}` on a `POST /login` request, and returns a `GET /` request carrying the session cookie, `login.Result().Cookies()[0]`;
  3. `laptop, phone := signIn(), signIn()`;
  4. with the comment `// POST /password on the laptop, after the new hash was stored.`: `p, _ := sessions.Authenticate(laptop)`, `_ = sessions.RevokeSubject(ctx, p.Subject)`, `done := httptest.NewRecorder()` and `_ = sessions.Issue(done, laptop, p) // keeps the laptop signed in`;
  5. print the error of `sessions.Authenticate(phone)`;
  6. build a `GET /` request with `done.Result().Cookies()[0]`, authenticate it, and print `got.Subject` and the error.

  Expected output: `auth: no credentials`, then `user-42 <nil>`.
- [X] T010 [P] [US1] In `auth/integration_test.go` (research R-09; FR-014, FR-015; SC-008):
  1. in `runEveryFlow`, right after `_ = a.sessions.Revoke(httptest.NewRecorder(), withCookies(t, done))`, add a blank line, the comment `// Revocation of every session of the subject, then a new session.`, and:
     - `_ = a.sessions.RevokeSubject(ctx, "user-1")`;
     - `again := httptest.NewRecorder()`;
     - `_ = a.sessions.Issue(again, httptest.NewRequestWithContext(ctx, http.MethodPost, "/password", nil), p)`;
     - `collect(again)`;
  2. in `TestStoreSetCreatesOnly`, declare `revocations := 0` next to `seen`. At the top of the loop over `rs.Calls()`, add the comment `// A revocation overwrites the subject's revocation state on purpose: it writes a new value without reading the old one.`, and skip, with `continue`, every `storetest.OpSet` whose key starts with `"auth:session-gen:"`, after incrementing `revocations`;
  3. after the existing `OpReplace` check, add `if revocations == 0 { t.Error("no revocation ran") }`.
- [X] T011 [US1] Run `GOWORK=off go test ./session .` and observe it failing to compile: `m.RevokeSubject undefined` (type `*session.Manager` has no field or method `RevokeSubject`).

### Implementation for User Story 1

- [X] T012 [US1] Add the operation, without the check yet (research R-02 to R-04):
  1. Create `auth/session/revocation.go`, package `session`, with the imports `context`, `errors`, `fmt`, `time`, `github.com/paccolamano/go/auth` and `github.com/paccolamano/go/auth/internal/storekey`, containing:
     - `const revocationMargin = time.Hour`, with the comment "revocationMargin is how much longer than the longest session or pending login a revocation state is kept: it covers an issuance in flight during the revocation, and clock differences between instances.";
     - `(*Manager).RevokeSubject(ctx context.Context, subject string) error`, with the doc comment of [contracts/public-api.md § RevokeSubject](contracts/public-api.md#managerrevokesubject-new-compatible), verbatim. For `subject == ""` it returns `errors.New("session: revoke subject: the subject is empty")` without touching the store. Otherwise it calls `m.store.Set(ctx, revocationKey(subject), []byte(newID()), m.revocationTTL)`, and wraps a failure as `fmt.Errorf("session: revoke subject: %w", err)`;
     - `(*Manager).generation(ctx context.Context, subject string) (string, error)`, with the comment "generation returns the revocation state of subject: the value written by its last revocation, or "" when it was never revoked or that state expired." It calls `m.store.Get(ctx, revocationKey(subject))`. It returns `"", nil` for an error matching `auth.ErrNotFound`, `"", fmt.Errorf("read revocation state: %w", err)` for any other error, and `string(b), nil` otherwise;
     - `(*Manager).revoked(ctx context.Context, subject, gen string) (bool, error)`, with the comment "revoked reports whether subject was revoked after a record observed gen." It returns the error of `generation` unchanged, and otherwise `cur != "" && cur != gen, nil` (data-model §3);
     - `func revocationKey(subject string) string { return storekey.Key("session-gen", subject) }`.
  2. In `auth/session/session.go`, add the field `revocationTTL time.Duration` to `Manager`, after `pendingLifetime`, and set it in `New` to `max(cfg.lifetime, cfg.pendingLifetime) + revocationMargin`: "max(lifetime, pendingLifetime) + 1 h", 9 hours with the defaults (data-model §1).
  3. In `auth/session/issue.go`, in `Issue`, right after `deleteCarried` and before `id := newID()`: `gen, err := m.generation(r.Context(), p.Subject)`. On an error, return `fmt.Errorf("session: issue: %w", err)`. Then set `Gen: gen` in the `sessionRecord` literal.
- [X] T013 [US1] Run `GOWORK=off go test -race ./session .` and observe the behavioral failures, because nothing checks the state yet:
  - `TestRevokeSubject_EndsSessions`, `TestRevokeSubject_ServedAsAnonymous` and `TestRevokeSubject_SameInstant` fail: the revoked sessions authenticate;
  - in `TestRevokeSubject_Interleavings`, every row whose `later` is false fails, and so do the `renewal` rows whose `served` is false;
  - `TestAuthenticate_Principal` fails, with one `Get` instead of two;
  - `ExampleManager_RevokeSubject` fails: the phone is still signed in;
  - `TestRevokeSubject_ThenIssue`, the `issue: revocation before the revocation read` row, and the root tests pass.

  Do not continue until these failures are seen.
- [X] T014 [US1] In `auth/session/session.go` (research R-05):
  1. in `Authenticate`, right after the check `if !now.Before(exp) || !now.Before(lastWrite.Add(m.idle))` and before the renewal branch, add:

     ```go
     switch revoked, err := m.revoked(ctx, rec.Sub, rec.Gen); {
     case err != nil:
     	return nil, fmt.Errorf("session: %w", err)
     case revoked:
     	return nil, auth.ErrNoCredentials
     }
     ```

  2. replace the package comment, and the doc comment of `Authenticate`, with the texts of [contracts/public-api.md](contracts/public-api.md#package-documentation-sessiongo-changed), verbatim;
  3. in `auth/session/issue.go`, replace the doc comment of `Issue` with the text of [contracts/public-api.md § Issue](contracts/public-api.md#managerissue-doc-comment-changed-signature-unchanged), verbatim.
- [X] T015 [US1] In `auth/README.md` (research R-11; FR-018):
  1. in the list "It handles the parts that are easy to get wrong", after the bullet on session identifiers, add: "- every session of a user can be revoked at once, on every instance, for example after a password change;";
  2. in "Sessions and CSRF", replace "so most requests cost one store read and no write." with "so most requests cost two store reads, the session and its user's revocation state, and no write.";
  3. after the "Sessions and CSRF" section, before "## Second factor", add a section `## Revoking every session of a user` with:
     - the paragraph: "`sessions.RevokeSubject(ctx, subject)` ends every session and pending login of a user, on every instance: call it after a password change, when an account is disabled, or to log out of every device. It needs no request, so an administrative action or a background job can call it too. To keep the current browser signed in, issue a new session right after:";
     - a `go` block with a `POST /password` handler, wrapped in `authn`, that reads `p, _ := auth.FromContext(r.Context())`, has the comment `// Check the current password and store the new hash here.`, calls `sessions.RevokeSubject(r.Context(), p.Subject)`, answers `http.Error(w, "try again", http.StatusInternalServerError)` and returns on an error, and ends with `_ = sessions.Issue(w, r, p) // a new identifier, issued after the revocation`;
     - the paragraph: "Every request checks the user's revocation state, one more store read. A login whose password was checked before a revocation, and whose session is issued after it, is not revoked: call `Issue` right after checking the password. A revocation is kept for the session lifetime plus one hour, then expires; sessions issued after it keep working."
- [X] T016 [US1] Run `GOWORK=off go test -race -shuffle=on ./...`. Every test of T006–T010 passes, and every existing test still passes. Then run `make check MODULE=auth` from the repository root, which must report `PASS`. (2026-10-23: the tests passed; lint reported only `failingRevocation` as unused, because T003 adds it and its first user is `TestRevokeSubject_Errors` in T017. T020 runs the gate again once T017 uses it.)

**Checkpoint**: a password change can end every other session of the user and keep the current one.
This is the MVP.

---

## Phase 4: User Story 2 - Disabling an account ends its sessions at once (Priority: P1)

**Goal**: the revocation works from outside any request, across instances, under concurrency and
store failures, and its state expires on schedule (FR-001, FR-002, FR-008, FR-011, FR-012,
FR-013).

**Independent Test**: two managers on one store; a revocation through one, from a context not bound
to a request, ends the sessions of both.

### Tests for User Story 2 ⚠️

- [X] T017 [US2] In `auth/session/revocation_test.go`, add four tests:
  - **`TestRevokeSubject_SharedStore`** (US2 scenario 1): two managers `m1` and `m2` on one `memstore.New()`; issue `c1` through `m1` and `c2` through `m2`; `m2.RevokeSubject(context.WithoutCancel(t.Context()), "user-1")` returns nil; neither cookie authenticates through either manager.
  - **`TestRevokeSubject_Errors`** (US2 scenarios 2 and 4; FR-008, FR-011; SC-006), without `synctest`, with `ctx := t.Context()` and `store := memstore.New()`:
    1. `rec := &storetest.Recorder{Next: store}`: `mustManager(t, rec).RevokeSubject(ctx, "")` returns an error, and `len(rec.Calls())` is 0;
    2. `c := issue(t, mustManager(t, store), alice())`, and `failing := mustManager(t, failingRevocation{store})`;
    3. `failing.RevokeSubject(ctx, "user-1")` matches `errStore`;
    4. `failing.Authenticate(request(t, c))` matches `errStore`; through `mw, err := auth.New(failing)`, wrapping `http.NotFoundHandler()`, the response code is `http.StatusInternalServerError`;
    5. `failing.Issue(resp, request(t), alice())`, with `resp := httptest.NewRecorder()`, matches `errStore`, and `resp.Result().Cookies()` is empty;
    6. **retry**: `m := mustManager(t, store)`; `m.RevokeSubject(ctx, "user-1")` returns nil, and `c` no longer authenticates;
    7. **cancelled context**: `m.RevokeSubject(cancelled, "user-1")`, with a context cancelled before the call, matches `context.Canceled`.

    Phase 5 extends steps 2 and 5 to pending logins (T021).
  - **`TestRevokeSubject_Concurrent`** (SC-003), in `synctest.Test`:
    1. issue 10 sessions for `alice()`, then `time.Sleep(8 * time.Minute)`, so every one is due for renewal;
    2. `revoked := make(chan struct{})`; for `i` in `range 100`, with `c := cookies[i%len(cookies)]`, start two goroutines with `wg.Go`:
       - one calls `m.Authenticate(request(t, c))` and reports, with `t.Errorf("during the revocation: %v", err)`, any error that does not match `auth.ErrNoCredentials`;
       - one waits on `<-revoked`, then calls `m.Authenticate(request(t, c))`, and reports any error that does not match `auth.ErrNoCredentials`, including nil, with `t.Errorf("after the revocation: %v, want ErrNoCredentials", err)`;
    3. one more goroutine calls `m.RevokeSubject(t.Context(), "user-1")`, reports its error with `t.Error`, and closes `revoked` with `defer`;
    4. `wg.Wait()`.
  - **`TestRevokeSubject_Retention`** (FR-012, FR-013; SC-007), in `synctest.Test`, with `store := memstore.New()`, `m := mustManager(t, store)` and `key := storekey.Key("session-gen", "user-1")`:
    1. issue `old` for `alice()`, then revoke `user-1`;
    2. `time.Sleep(2 * time.Hour)`, then issue `later` for `alice()`;
    3. for `elapsed` from 2h20m to 9h20m inclusive, in steps of 20 minutes: `time.Sleep(20 * time.Minute)`; `later` must authenticate (report `elapsed`); then `_, err := store.Get(ctx, key)`:
       - at `elapsed == 8*time.Hour`, `err` must be nil: sessions issued before the revocation can still live;
       - at `elapsed >= 9*time.Hour`, `err` must match `auth.ErrNotFound`;
    4. `old` does not authenticate.
- [X] T018 [US2] Run `GOWORK=off go test -race -run 'RevokeSubject_(SharedStore|Errors|Concurrent|Retention)' -v ./session`: the four tests pass, because Phase 3 implemented the behavior. Show that they guard it, one temporary mutation at a time:
  1. set `revocationMargin` to `0 * time.Hour` in `auth/session/revocation.go` (a bare `0` leaves the `time` import unused and the build fails): `TestRevokeSubject_Retention` fails at 8h, because the state expires with the sessions it must still revoke. Restore it;
  2. in `generation`, return `"", nil` for every error instead of wrapping it: `TestRevokeSubject_Errors` fails at step 4, because a store failure turns into "not revoked". Restore it.

  `revocation.go` is a new, untracked file, so `git diff` cannot show a mutation: copy it aside before mutating, restore it with `command cp -f` (the interactive `cp -i` alias of the shell blocks), and compare it with `cmp`.

### Documentation for User Story 2

- [X] T019 [P] [US2] Document the store's part (research R-08; FR-018):
  1. in `auth/store.go`, replace the first sentence of the `Store` doc comment with the text of [contracts/public-api.md § Store](contracts/public-api.md#store-doc-comment-changed-interface-unchanged), verbatim. The rest of the comment and the methods do not change;
  2. in `auth/doc.go`, section `# Store`, replace the paragraph with the text of [contracts/public-api.md § Package documentation](contracts/public-api.md#package-documentation-docgo--store-changed), verbatim;
  3. in `auth/README.md`, section "Store", after the paragraph that ends "prefix them in the adapter if several applications share one database.", add: "The store must keep every key until it expires, and a read must see every write that completed before it. A revocation that the store evicts early stops revoking, and evictions also reset attempt limits: keep Redis on its default `maxmemory-policy`, `noeviction`, since the `volatile-*` policies can evict any key of this module. Read from the primary, not from replicas, so that a session issued right after a revocation sees it."
- [X] T020 [US2] Run `GOWORK=off go test -race -shuffle=on ./...` and `make check MODULE=auth`: both pass.

**Checkpoint**: revocation is safe to call from administrative code and background jobs.

---

## Phase 5: User Story 3 - A half-finished login does not outlive the revocation (Priority: P2)

**Goal**: pending logins record the revocation state, and reading one of a revoked subject gives no
credentials (FR-003, FR-004, FR-006).

**Independent Test**: issue a pending login, revoke its subject, then read the pending login.

### Tests for User Story 3 ⚠️ (write first, see them fail)

- [X] T021 [US3] In `auth/session/revocation_test.go`:
  1. add **`TestRevokeSubject_Pending`** (US3 scenarios 1–3; FR-003, FR-004), with `ctx := t.Context()`:
     - issue the pending logins `before` for `alice()` and `other` for `bob()`, then revoke `user-1`;
     - `m.Pending(request(t, before))` matches `auth.ErrNoCredentials`;
     - `m.Pending(request(t, other))` returns a nil error;
     - `after := issuePending(t, m, alice())`; `mustPending(t, m, after).Attempt(ctx, pass)` returns nil; and `issue(t, m, alice(), after)` authenticates;
  2. add **`TestRevokeSubject_WindowAccepted`** (FR-006; research R-07), with the comment "TestRevokeSubject_WindowAccepted pins FR-006: a pending login read before a revocation and completed after it yields a valid session.":
     - `pc := issuePending(t, m, alice())` and `pending := mustPending(t, m, pc)`;
     - revoke `user-1`;
     - `pending.Attempt(t.Context(), pass)` returns nil;
     - `issue(t, m, alice(), pc)` authenticates, or `t.Fatal("the session issued after the revocation was revoked")`;
  3. in **`TestRevokeSubject_Interleavings`**, add the row `{"pending: revocation before the revocation read", "pending", beforeRevocationRead, false, false}`, and the case `"pending"`: `pc := issuePending(t, m, alice())`; arm the wrapper; `_, err := m.Pending(request(t, pc))`; `err == nil` must equal `c.served`;
  4. in **`TestRevokeSubject_Errors`**:
     - in step 2, also issue `pc := issuePending(t, mustManager(t, store), alice())`;
     - turn step 5 into a loop over `map[string]func(http.ResponseWriter, *http.Request, *auth.Principal) error{"Issue": failing.Issue, "IssuePending": failing.IssuePending}`, with the same assertions for each, naming the function in the message;
     - after the loop, `failing.Pending(request(t, pc))` matches `errStore`.
- [X] T022 [US3] Run `GOWORK=off go test -race -run 'RevokeSubject_' -v ./session` and observe the failures:
  - `TestRevokeSubject_Pending`: the pending login issued before the revocation can be read;
  - `TestRevokeSubject_Interleavings/pending:_revocation_before_the_revocation_read`: `Pending` reads no revocation state yet, so the read succeeds and the revocation never runs;
  - `TestRevokeSubject_Errors`: `IssuePending` succeeds and sets a cookie, and `Pending` succeeds;
  - `TestRevokeSubject_WindowAccepted` passes, as it must.

  Do not continue until these failures are seen.

### Implementation for User Story 3

- [X] T023 [US3] In `auth/session/pending.go` (research R-05):
  1. in `IssuePending`, after deleting the carried pending login and before `id := newID()`: `gen, err := m.generation(ctx, p.Subject)`, returning `fmt.Errorf("session: issue pending: %w", err)` on an error; add `Gen: gen` to the `pendingRecord` literal;
  2. in `Pending`, right after the expiry check on `rec.IAT`, add the same `switch` as in `Authenticate` (T014), with `r.Context()`;
  3. replace the doc comments of `IssuePending` and `Pending` with the texts of [contracts/public-api.md](contracts/public-api.md#managerissuepending-doc-comment-changed-signature-unchanged), verbatim.
- [X] T024 [US3] Run `GOWORK=off go test -race -shuffle=on ./...` and `make check MODULE=auth`: both pass, the existing pending tests included.

**Checkpoint**: no pending login started before a revocation can complete after it, except within
the window of FR-006.

---

## Phase 6: User Story 4 - Upgrading logs nobody out (Priority: P3)

**Goal**: records of 0.2.0 keep working until their subject is revoked, and 0.2.0 can read the
records of this release (FR-016, FR-017).

**Independent Test**: authenticate a session record in the 0.2.0 format, before and after revoking
its subject; decode a record of this release into the 0.2.0 shape.

### Tests for User Story 4 ⚠️

- [X] T025 [US4] Add two tests:
  1. **`TestRevokeSubject_PreviousRelease`** in `auth/session/revocation_test.go` (US4 scenario 1; FR-016; SC-001):
     - `store := memstore.New()`, `m := mustManager(t, store)`, `now := time.Now().Unix()`;
     - `sid := strings.Repeat("A", 43)` and `pid := strings.Repeat("B", 42) + "A"`. An identifier must decode as canonical base64url, so its last character must leave no bits over: `"A"` does, `"B"` does not;
     - write two records in the 0.2.0 format with `store.Set`, each built with `fmt.Appendf` from a raw string literal:
       - under `storekey.Key("session", sid)`, the format `{"v":1,"sub":"user-1","amr":["pwd"],"iat":%d,"exp":%d,"rw":%d}` with `now, now+3600, now`, and a TTL of one hour;
       - under `storekey.Key("pending", pid)`, the format `{"v":1,"sub":"user-1","amr":["pwd"],"iat":%d}` with `now`, and a TTL of one minute;
     - build `sc := requestCookie(t, sessionCookie, sid)` and `pc := requestCookie(t, pendingCookie, pid)`;
     - `sc` authenticates, and `m.Pending(request(t, pc))` returns a nil error;
     - revoke `user-1`; `sc` no longer authenticates, and `m.Pending(request(t, pc))` matches `auth.ErrNoCredentials`.
  2. **`TestRecord_PreviousRelease`** in `auth/session/record_internal_test.go` (US4 scenario 2; FR-017), with the comment "TestRecord_PreviousRelease checks both directions of compatibility with auth/v0.2.0, whose decodeRecord is the same function and whose records have no gen field.":
     - declare a local `sessionV020` struct with the six fields and JSON tags of the 0.2.0 `sessionRecord` (`v`, `sub`, `amr`, `iat`, `exp`, `rw`);
     - encode `sessionRecord{V: 1, Sub: "u", AMR: []string{"pwd"}, IAT: 1, EXP: 2, RW: 3, Gen: "g"}`, decode it with `decodeRecord` into a `sessionV020`: no error, and `Sub`, `EXP` and `RW` match;
     - decode `{"v":1,"sub":"u","amr":["pwd"],"iat":1,"exp":2,"rw":3}` into a `sessionRecord`: no error, `Gen` is empty, and `Sub` is `"u"`;
     - encode the same record without `Gen`: the bytes equal `{"v":1,"sub":"u","amr":["pwd"],"iat":1,"exp":2,"rw":3}` exactly.
- [X] T026 [US4] Run `GOWORK=off go test -race -run 'RevokeSubject_PreviousRelease|TestRecord_' -v ./session`: both tests pass, since T005 added `omitempty` and kept version 1. Show that they guard compatibility, one temporary mutation at a time, and restore each:
  1. drop `,omitempty` from the `Gen` tag of `sessionRecord`: `TestRecord_PreviousRelease` fails on the exact bytes, and `TestRecord_Fields` fails on the field sets;
  2. set `recordVersion` to 2: `TestRevokeSubject_PreviousRelease` fails, because records of 0.2.0 no longer decode.

  Run `git diff -- auth/session/record.go` and confirm that only the change of T005 remains.

### Documentation for User Story 4

- [X] T027 [P] [US4] In `auth/README.md`, at the end of the section "Revoking every session of a user", add: "Sessions issued by 0.2.0 stay valid until their user is revoked. During a rolling upgrade from 0.2.0, or after a rollback to it, instances running 0.2.0 do not check revocations." (research R-06; FR-017, FR-018).
- [X] T028 [US4] Run `GOWORK=off go test -race -shuffle=on ./...` and `make check MODULE=auth`: both pass.

**Checkpoint**: every story is complete.

---

## Phase 7: Polish & Cross-Cutting Concerns

**Purpose**: the changelog, then verification against the contracts and the quickstart, and the
gates.

- [X] T029 In `auth/CHANGELOG.md`, under `## [Unreleased]`, add a `### Added` section with the entry drafted in [research R-01](research.md#r-01-release-level-and-api-compatibility), verbatim (FR-019).
- [X] T030 [P] Verify the exported surface against [contracts/public-api.md](contracts/public-api.md). From `auth/`, run each of these, and compare the output with the contract:
  - `GOWORK=off go doc ./session Manager.RevokeSubject`;
  - `GOWORK=off go doc ./session` shows the new package comment;
  - `GOWORK=off go doc ./session Manager.Authenticate`, `Manager.Issue`, `Manager.IssuePending` and `Manager.Pending`;
  - `GOWORK=off go doc . Store` and `GOWORK=off go doc .` (the "# Store" section).

  Then run `GOWORK=off go test -coverprofile=c.out ./... && go tool cover -func=c.out | grep revocation.go`: `RevokeSubject`, `generation`, `revoked` and `revocationKey` must be at 100%. Delete `c.out` afterwards.
- [X] T031 [P] From `auth/`, check the dependencies and the platforms:
  - `GOWORK=off go mod tidy -diff` prints nothing;
  - `git diff --exit-code -- go.mod go.sum` passes;
  - for every `os` in linux, darwin and windows, and every `arch` in amd64 and arm64, `GOWORK=off GOOS=$os GOARCH=$arch go vet ./...` passes.
- [X] T032 [P] Run `GOWORK=off go test -run '^$' -bench BenchmarkSessionAuthenticate -benchmem -count 5 ./session` and `GOWORK=off go test -run '^$' -bench 'BenchmarkCheck$' -benchtime=20x ./password` from `auth/` (SC-005). The session median must stay under 1% of the password check. Compare it with the baseline of T002: in the spike it rose by about 0.3–0.6 µs, from 833 to 1041 B/op and from 15 to 19 allocs/op, for the second read. Record the medians for the pull request description (T035).
- [X] T033 Run the checks of [quickstart.md](quickstart.md) §2, §3, §4 (one row at a time), §5 and §6. Every command must pass, and `session` must be at 80% or above.
- [X] T034 Run `make check MODULE=auth` from the repository root:
  - golangci-lint reports zero findings, and `git diff -- auth | grep -c 'nolint'` prints 0;
  - `go test -race -shuffle=on` passes, with coverage ≥ 80%. Compare the module and `session` percentages with the baseline of T002; the spike reached 97.3% and 92.7%.
- [X] T035 Draft the pull request for the changes in `auth/` and `specs/011-revoke-subject-sessions/`, for the maintainer, and do not open it without their go-ahead:
  - **Title**: `feat(auth): revoke every session of a subject`.
  - **Description**:
    - the gap: a session survives a password change or a disabled account for up to 8 hours, and the developer could not end it;
    - the design: a per-subject revocation state checked at every request, with no store contract change;
    - the cost: one more store read per session request, with the benchmark medians of T032;
    - the accepted window of FR-006, and the store requirements of research R-08;
    - compatibility with 0.2.0 in both directions;
    - the attribution line required for pull request descriptions.

---

## Dependencies & Execution Order

### Phase dependencies

- **Setup (Phase 1)** and **Foundational (Phase 2)** run first: T001 → T002 → T003–T005.
- **US1 (Phase 3)** depends on Phase 2: the tests use the wrappers of T003, and issuance writes
  `Gen` (T005).
- **US2 (Phase 4)** depends on US1. Its tests exercise the code of T012 and T014, and its red step
  mutates it.
- **US3 (Phase 5)** depends on US1: it reuses `generation` and `revoked`, and extends the tests of
  T007 and T017. It therefore follows US2.
- **US4 (Phase 6)** depends on US3, because `TestRevokeSubject_PreviousRelease` also checks a
  pending login.
- **Polish (Phase 7)** depends on every story.

The recommended order is the priority order: US1 → US2 → US3 → US4. Each phase leaves
`make check MODULE=auth` green.

### Within each phase

- Test tasks come first, and are observed failing: a compilation failure in Phase 2 (T004) and
  Phase 3 (T011), behavioral failures in T013 and T022, and temporary mutations in T018 and T026.
- **Shared files**: tasks on the same file never run in parallel.

  | File | Edited by |
  |------|-----------|
  | `auth/session/helpers_test.go` | T003 |
  | `auth/session/record_internal_test.go` | T004, T025 |
  | `auth/session/record.go` | T005 (T026 mutates and restores it) |
  | `auth/session/revocation_test.go` | T006, T007, T017, T021, T025 |
  | `auth/session/session_test.go` | T008 |
  | `auth/session/example_test.go` | T009 |
  | `auth/integration_test.go` | T010 |
  | `auth/session/revocation.go` | T012 (T018 mutates and restores it) |
  | `auth/session/session.go` | T012, T014 |
  | `auth/session/issue.go` | T012, T014 |
  | `auth/session/pending.go` | T023 |
  | `auth/store.go`, `auth/doc.go` | T019 |
  | `auth/README.md` | T015, T019, T027 |
  | `auth/CHANGELOG.md` | T029 |

### Parallel opportunities

- **Phase 2**: T003 and T004 together (different files), then T005.
- **US1**: T006 → T007 (same file); T008, T009 and T010 in parallel with them. Then T011, T012,
  T013, T014, T015 and T016 in sequence.
- **US2**: T017 → T018; T019 in parallel with both. Then T020.
- **US4**: T025 → T026; T027 in parallel with both. Then T028.
- **Polish**: T029, then T030, T031 and T032 together, then T033, T034 and T035.

---

## Parallel Example: Phase 2

```bash
# Write the helpers and the failing record test together (different files):
Task: "T003 beforeRevocationRead and failingRevocation in auth/session/helpers_test.go"
Task: "T004 gen cases of TestRecord_Fields in auth/session/record_internal_test.go"
# then T005 (the Gen field in auth/session/record.go)
```

## Parallel Example: User Story 1

```bash
# While T006 and T007 fill auth/session/revocation_test.go:
Task: "T008 two Gets in TestAuthenticate_Principal, auth/session/session_test.go"
Task: "T009 ExampleManager_RevokeSubject in auth/session/example_test.go"
Task: "T010 revocation in runEveryFlow and the exemption in auth/integration_test.go"
# then T011 (red: compilation) → T012 → T013 (red: behavior) → T014 → T015 → T016
```

---

## Requirement Coverage

| Requirement | Tasks |
|-------------|-------|
| FR-001 | T006, T012, T017 (empty subject, no request) |
| FR-002 | T006, T007, T014, T017 (`SharedStore`) |
| FR-003 | T021, T023 |
| FR-004 | T006 (`b`, `ThenIssue`), T021 (`other`, `after`) |
| FR-005 | T007 (`issue` rows), T012 |
| FR-006 | T021 (`WindowAccepted`), T012 (doc comment), T015 (README) |
| FR-007 | T006 (`SameInstant`) |
| FR-008 | T017 (`Errors`: retry, cancelled context), T012 |
| FR-009 | T008, T014 |
| FR-010 | T008, T032, and the unchanged `TestRenewal_Writes` |
| FR-011 | T017, T021 (`Errors`), T018 (mutation 2) |
| FR-012 | T017 (`Retention`), T018 (mutation 1), T012 |
| FR-013 | T017 (`Retention`) |
| FR-014 | T010, T031, and `gorelease` in CI |
| FR-015 | T010 (`TestStoreHoldsNoSecrets` over the new flow) |
| FR-016 | T025, T026 (mutation 2) |
| FR-017 | T025, T026 (mutation 1), T027 |
| FR-018 | T014, T015, T019, T023, T027, T030 |
| FR-019 | T029 |
| FR-020 | T003, T007, T017 (`Concurrent`), T021 |
| SC-001 | T006, T007, T021, T025 |
| SC-002 | T006 |
| SC-003 | T017 (`Concurrent`) |
| SC-004 | T006 (`SameInstant`) |
| SC-005 | T008, T032, and `TestRenewal_Writes` |
| SC-006 | T017, T021 (`Errors`) |
| SC-007 | T017 (`Retention`) |
| SC-008 | T010 |
| SC-009 | T030, T033, T034, and `gorelease` in CI |

Spec edge cases:

| Edge case | Tasks |
|-----------|-------|
| a subject revoked twice | T017 (`Errors`: the retry revokes again), T007 (`renewal` rows) |
| a revocation during an issuance | T007 (`issue` rows) |
| the window of FR-006 | T021 (`WindowAccepted`) |
| a request that read the state before the revocation | T007 (`renewal: revocation between the revocation read and the write`), T017 (`Concurrent`) |
| the revocation state expires | T017 (`Retention`) |
| a store that discards keys early | T019 (documentation) |
| the revocation clears no cookie | T006 (`ThenIssue`, `ServedAsAnonymous`), T009 |
| an empty subject | T017 (`Errors`, step 1) |
| clocks of different instances | T006 (`SameInstant`) |
| API keys, tokens and Basic are unaffected | no code path changes outside `session`; T031 |
| a rolling upgrade | T025, T027 |

---

## Implementation Strategy

### MVP first (User Story 1)

1. Phase 1 and Phase 2: T001–T005. The records can carry `gen`.
2. Phase 3, US1: T006–T016. `RevokeSubject` exists, and authentication checks it.
3. **Stop and validate**: run `make check MODULE=auth`.

### Incremental delivery

1. US2 (P1): T017–T020. Shared stores, failures, concurrency and retention are tested, and the
   store's part is documented.
2. US3 (P2): T021–T024. Pending logins of a revoked subject are cancelled.
3. US4 (P3): T025–T028. Compatibility with 0.2.0 is pinned and documented.
4. Polish: changelog, contracts, platforms, benchmark, quickstart, gates, and the PR draft.

The feature ships as one pull request. A release without US3 would leave pending logins outside
the revocation, which the spec requires. The release `auth/v0.3.0` (research R-01) is prepared in a
separate PR after the squash merge, as for v0.2.0. The changelog entry stays under
`[Unreleased]` until then.

---

## Notes

- Every task stays within `auth/`, except T001 and this file.
- Never weaken a test to make it pass. Fix the implementation, or update the spec and the
  contracts first if the behavior itself must change.
- The spike lives only in the session scratchpad. Re-implement from these tasks; do not copy files
  from it.

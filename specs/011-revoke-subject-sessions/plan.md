# Implementation Plan: Revoke Every Session of a Subject (auth)

**Branch**: `011-revoke-subject-sessions` (suggested; not yet created; the working tree is on `auth-release-0.2.0`) | **Date**: 2026-10-23 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `specs/011-revoke-subject-sessions/spec.md`

**Note**: This template is filled in by the `/speckit-plan` command; its definition describes the execution workflow.

## Summary

This feature changes the existing module `github.com/paccolamano/go/auth`, released as
`auth/v0.2.0`, and ships as `auth/v0.3.0`. It adds one operation that revokes every session and
pending login of a subject. Today nothing ends the sessions that a user opened elsewhere, for
example a stolen cookie after a password change.

- **API**: `(*session.Manager).RevokeSubject(ctx, subject) error`, which is additive. It needs no
  request, so it runs in a password change handler, an administrative action or a background job
  (R-02).
- **Revocation state**: one store key per subject, `auth:session-gen:<hash of the subject>`. Each
  revocation writes a fresh random value to it with `Set`, without reading it first. The key
  expires after the longer of the session and pending lifetimes, plus one hour (R-03, R-04).
- **Records**: session and pending login records gain an optional `gen` field, the revocation
  state observed at issuance. It is omitted when the subject was never revoked. The format version
  stays 1, so 0.2.0 reads the new records, and records of 0.2.0 read as "never revoked" (R-06).
- **Check**: `Authenticate` and `Pending` read the subject's revocation state at every call, after
  the expiry checks and before renewal. A record is revoked when the state is present and differs
  from its `gen`; an absent state never revokes. `Issue` and `IssuePending` read the state right
  before writing (R-05).
- **Cost**: a session request makes two store reads instead of one, and issuing a session,
  issuing a pending login and reading a pending login each make one more. Writes do not change
  (R-05).
- **Store**: the contract does not change. The documentation now states two requirements of the
  store: it keeps keys until they expire, and a read sees every write that completed before it
  (R-08).
- **Tests**:
  - deterministic interleavings of a revocation with an issuance, a renewal and the reading of a
    pending login, under virtual time;
  - a test where the revocation and the issuances happen at the same virtual instant;
  - 100 concurrent requests under `-race`;
  - retention, store failures, and compatibility with records of 0.2.0;
  - the store invariant test of spec 010, with one exemption for the revocation state (R-09,
    R-10).

The design was implemented once in a scratch copy of the module; the repository was not touched.
With the record field, `RevokeSubject` and the issuance reads in place, but without the check in
`Authenticate` and `Pending`, 9 of the 12 new tests failed. The 3 that passed check that sessions
issued after a revocation stay valid. With the check:

- `golangci-lint` reported 0 issues, after two fixes: a gofmt alignment and a `wrapcheck`;
- the suite passed under `-race -shuffle=on`, with 97.3% statement coverage for the module and
  92.7% for `session`;
- `BenchmarkSessionAuthenticate` went from about 3.0 µs to 3.6 µs per request, about 0.02% of one
  password check (18 ms).

## Technical Context

**Language/Version**: Go. The `go` directive stays `go 1.26.0`. CI also tests on 1.27. The tests
use `testing/synctest` and `sync.WaitGroup.Go`, which the module already uses.

**Primary Dependencies**: unchanged. The library code adds no import beyond those `session`
already has, and `go.mod` and `go.sum` do not change ([R-12](research.md#r-12-dependencies)).

**Storage**: the developer's `auth.Store`, whose contract does not change. The module uses one
more key kind, `session-gen`, with `Get` and `Set` only. Session and pending login records gain
an optional field ([contracts/store.md](contracts/store.md)).

**Testing**: the standard `testing` package, with table-driven tests and named subtests.

- `testing/synctest` for virtual time: renewal intervals, absolute lifetimes, the retention of
  the revocation state, and two operations at the same instant.
- The `interleaved` store wrapper of spec 010, with one more point, before the revocation read.
- A `failingRevocation` wrapper that fails only the reads and writes of revocation states.
- `-race`, with 100 concurrent requests while a revocation runs.
- `storetest.Recorder` for the store read counts and the store invariants.

Details: [research.md R-10](research.md#r-10-testing-approach-fr-020-sc-001-to-sc-009-principle-iii).

**Target Platform**: any platform Go supports. Pure Go, with no build constraints.

**Project Type**: library. One existing Go module, `auth/`, changed in the `session` package, in
two doc comments of the root package, and in one root-package test.

**Performance Goals**:

- A request authenticated with a session makes two store reads instead of one. It still calls no
  lookup function, and renewal still writes at most once per renewal interval (FR-010, SC-005).
- Verifying a session against the in-memory store costs about 3.6 µs, under 1% of a password
  check (SC-009 of spec 007).

**Constraints**:

- **No clock comparisons**: the outcome depends only on the order of the store operations
  (FR-007). Clocks affect only expiry, as they already do.
- **No new state** besides the revocation key, and no goroutine, global or logging in library
  code.
- **Error semantics**: a failure reading the revocation state is an error, never "no
  credentials" (FR-011).

**Scale/Scope** (from the spike):

- Library code, about 90 lines:
  - `session/revocation.go`: about 63 lines, new;
  - `session/session.go`, `issue.go`, `pending.go` and `record.go`: about 28 lines;
  - doc comments in `session/session.go`, `store.go` and `doc.go`.
- Tests: about 390 lines in a new `session/revocation_test.go`, and about 95 lines elsewhere.
- Documentation: the README and the CHANGELOG.

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

| Rule | Assessment | Status |
|------|------------|--------|
| **I. Independent modules** | Only `auth/` changes. No new dependency and no `replace`. The README and the CHANGELOG are updated. The module builds and tests with `GOWORK=off`. | ✅ |
| **II. SemVer & stable API** | One exported method is added, `(*session.Manager).RevokeSubject`; nothing changes incompatibly. `gorelease` reports one compatible addition, so the release is `auth/v0.3.0` (R-01). The CHANGELOG records it under "Added". Nothing is removed, so no deprecation is needed. Exported surface: one method, required by FR-001. | ✅ |
| **III. Test-first** | The revocation tests were observed failing before the check existed (9 of 12 in the spike; the other 3 check what must stay valid). `tasks.md` will order every test before its code. Virtual time, with no sleeps for synchronization. The concurrent test asserts outcomes that hold for every schedule. `RevokeSubject` gets `ExampleManager_RevokeSubject`. No new parser of external input, so no new fuzz target. `BenchmarkSessionAuthenticate` now measures the two reads. Coverage: 97.3% for the module, 92.7% for `session` in the spike. | ✅ |
| **IV. Idiomatic Go** | 0 lint issues in the spike, and no `//nolint`. `RevokeSubject` has a doc comment, and the doc comments of `Authenticate`, `Issue`, `IssuePending`, `Pending`, the package, `auth.Store` and `doc.go` describe the change. Errors are wrapped with `%w` ("session: revoke subject: …", "session: read revocation state: …"). `RevokeSubject` takes `context.Context` first. No panic, `init`, global or goroutine. | ✅ |
| **V. Simplicity & dependencies** | No dependency. No store contract change, no option, no index. The rejected alternatives are a revocation timestamp, a counter, a per-subject index of sessions, an option to turn the check off, and checking only at renewal (R-03, R-05). | ✅ |
| Go toolchain support | `go 1.26.0` is unchanged; the code uses the `max` built-in and `sync.WaitGroup.Go`, already used by the module. | ✅ |
| Platforms | Pure Go. | ✅ |
| Repository layout | No tooling change, no lint override and no `.module-exceptions` entry. | ✅ |
| Dependency hygiene | `go.mod` and `go.sum` are unchanged, so `go mod tidy -diff` stays clean. | ✅ |
| Vulnerability scanning / Secrets | No dependency to scan. The flows of `TestStoreHoldsNoSecrets` gain a revocation and a new session, so the revocation key and the records that carry `gen` are scanned for secrets (R-09). | ✅ |
| Local and PR gates | `make check MODULE=auth`. | ✅ |
| Commits / Release | The PR title is `feat(auth): revoke every session of a subject`, with the `auth` scope. The release is `auth/v0.3.0`. | ✅ |

**Result (pre-research)**: PASS. No new deviation.

**Result (post-design re-check)**: PASS.

- The data model adds one key kind and one optional record field, and the contracts add one
  method, mapped to FR-001 to FR-019.
- The research found one test that the feature must change on purpose: `TestStoreSetCreatesOnly`
  of spec 010 forbids a `Set` over a key that any earlier call used, and a revocation overwrites
  its key by design. In the spike the test failed without an exemption; R-09 adds the exemption
  and keeps the invariant for every other key.
- The spec needs no change: no decision of the research contradicts it.

## Project Structure

### Documentation (this feature)

```text
specs/011-revoke-subject-sessions/
├── spec.md               # feature specification
├── plan.md               # this file
├── research.md           # Phase 0: decisions R-01 … R-12, with spike evidence
├── data-model.md         # Phase 1: revocation state, records, validity rule, interleavings
├── quickstart.md         # Phase 1: validation guide
├── contracts/
│   ├── store.md          # changes to the store contract of specs 007 and 010: keys, values, requirements
│   └── public-api.md     # API additions and doc comment changes (v0.2.0 → v0.3.0)
├── checklists/
│   └── requirements.md   # spec quality checklist
└── tasks.md              # Phase 2 (/speckit-tasks, not created here)
```

### Source Code (repository root)

```text
auth/
├── CHANGELOG.md                      # [Unreleased]: Added (RevokeSubject), with the cost and compatibility notes
├── README.md                         # "Sessions and CSRF": two reads; new "Revoking every session" section; "Store": key requirements
├── doc.go                            # "# Store": the Store also holds revocations
├── store.go                          # Store doc: "sessions, pending logins, revocations and counters"
├── integration_test.go               # runEveryFlow: + revocation and new session; TestStoreSetCreatesOnly: exemption
└── session/
    ├── revocation.go                 # new: RevokeSubject, generation, revoked, revocationKey, revocationMargin
    ├── session.go                    # package doc; Manager.revocationTTL; Authenticate checks the revocation state
    ├── issue.go                      # Issue reads the revocation state and records it
    ├── pending.go                    # IssuePending records it; Pending checks it
    ├── record.go                     # sessionRecord and pendingRecord gain Gen (`gen,omitempty`)
    ├── helpers_test.go               # interleaved: + beforeRevocationRead; + failingRevocation
    ├── revocation_test.go            # new: TestRevokeSubject_* (R-10)
    ├── session_test.go               # TestAuthenticate_Principal: two Gets
    ├── record_internal_test.go       # TestRecord_Fields: gen cases; + TestRecord_PreviousRelease
    └── example_test.go               # + ExampleManager_RevokeSubject
```

**Structure Decision**: the existing module `auth/` is changed in place:

- the new operation and its helpers go in a new file, `session/revocation.go`, because
  `session.go`, `issue.go` and `pending.go` each own one existing concern;
- `issue.go` keeps `Revoke`, which ends the sessions of one request;
- the tests go in a new `session/revocation_test.go`, next to `session_test.go` and
  `pending_test.go`;
- the shared test wrappers stay in `session/helpers_test.go`.

Nothing outside `auth/` and this spec directory changes.

## Complexity Tracking

No new violation. The one deviation of the module is unchanged. `Authenticate`, `Issue`,
`IssuePending`, `Pending` and `Revoke` take their context from the request, without a first
context parameter. Spec 007 records and justifies it in
[007 plan.md](../007-http-auth/plan.md#complexity-tracking). `RevokeSubject` is not bound to a
request, and takes `context.Context` as its first parameter.

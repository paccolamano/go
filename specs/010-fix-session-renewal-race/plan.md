# Implementation Plan: Deleted Sessions Stay Deleted During Renewal (auth)

**Branch**: `010-fix-session-renewal-race` (suggested; work currently on `fix/auth-session-renewal-race`) | **Date**: 2026-10-22 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `specs/010-fix-session-renewal-race/spec.md`

**Note**: This template is filled in by the `/speckit-plan` command; its definition describes the execution workflow.

## Summary

This feature changes the existing module `github.com/paccolamano/go/auth`, released as
`auth/v0.1.0`, and ships as `auth/v0.2.0`. It fixes one defect: a session deleted by a logout or
by a new login, while a request renews it, is written back to the store and accepted again.

- **Store contract**: `auth.Store` gains
  `Replace(ctx, key, value, ttl) (bool, error)`. It stores only over a key that is present and
  unexpired, checking and writing in one atomic step, and reports whether it stored. This is
  an incompatible change, allowed at 0.x (R-01, R-02).
- **Renewal**: `(*session.Manager).Authenticate` writes the renewed record with `Replace`. A
  `false` result means the session is gone, and gives `auth.ErrNoCredentials`. An error gives
  500. The `write` helper is split into a `ttl` method, so `Issue` keeps `Set` for its new
  identifier (R-03).
- **Adapters**: `memstore` implements `Replace` under its mutex (R-05). The README Redis adapter
  gains one line, `SetXX(...).Result()`, a single command checked against the go-redis v9.11.0
  source (R-04).
- **Audit**: renewal is the only place where the module reads a key and writes it back. An
  invariant test asserts that every `Set` targets a key that no earlier store call used (R-06).
- **Tests**:
  - a deterministic regression test runs a logout, a new login, or the store expiry at each point
    of a renewal, under virtual time;
  - concurrent tests cover the sessions and `memstore`;
  - `internal/storetest` gains `Replace`, and tests that bring the package from 78.9% to over 80%
    statement coverage (R-07).

The design was implemented once in the working tree, then reverted to follow the Spec Kit flow.
With it, `golangci-lint` reported 0 issues, the suite passed under `-race` with 96.0% module
coverage, and the regression test failed before the session change and passed after it.

## Technical Context

**Language/Version**: Go. The `go` directive stays `go 1.26.0`. CI also tests on 1.27. The tests
use `testing/synctest` and `sync.WaitGroup.Go`, which the module already uses.

**Primary Dependencies**: unchanged. The library code adds no import, and `go.mod` and `go.sum`
do not change ([R-09](research.md#r-09-dependencies)). The README adapter uses go-redis, which is
the developer's dependency.

**Storage**: the developer's `auth.Store`. The fix adds one operation to its contract, and
changes no key and no value format ([contracts/store.md](contracts/store.md)).

**Testing**: the standard `testing` package, with table-driven tests and named subtests.

- `testing/synctest` for virtual time.
- An `interleaved` store wrapper that runs a deletion at a chosen point of a renewal.
- `-race`, with 100 concurrent renewals and 100 concurrent `Replace` and `Delete` runs.
- `storetest.Recorder` for the write counts and for the invariant of R-06.

Details: [research.md R-07](research.md#r-07-testing-approach-fr-013-sc-001-to-sc-006-principle-iii).

**Target Platform**: any platform Go supports. Pure Go, with no build constraints.

**Project Type**: library. One existing Go module, `auth/`, changed in its root package and in
the `session`, `memstore` and `internal/storetest` packages.

**Performance Goals**: unchanged. A request still costs one store read, and a renewal one write,
now `Replace` instead of `Set`, at most once per renewal interval (FR-009, SC-003). The session
benchmarks do not exercise renewal, so they do not change.

**Constraints**:

- **Atomicity**: `Replace` is one atomic step in every adapter: the mutex in `memstore`, and one
  command in Redis.
- **No new state**, no goroutine, no global and no logging in library code.
- **Error semantics**: an absent key is `false, nil`, never an error. A store failure is never
  turned into "no credentials".

**Scale/Scope**:

- Library code, about 45 lines:
  - `store.go`: one method and five doc lines;
  - `memstore/memstore.go`: about 25 lines;
  - `session/session.go` and `session/issue.go`: about 15 lines changed;
  - `doc.go`: three lines;
  - `internal/storetest/storetest.go`: about 30 lines.
- Tests: about 250 lines.
- Documentation: the README and the CHANGELOG.

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

| Rule | Assessment | Status |
|------|------------|--------|
| **I. Independent modules** | Only `auth/` changes. No new dependency and no `replace`. The README and the CHANGELOG are updated. The module builds and tests with `GOWORK=off`. | ✅ |
| **II. SemVer & stable API** | The method added to `auth.Store` is incompatible. At 0.x, a MINOR release allows it, so the release is `auth/v0.2.0` (R-01). `gorelease` reports it, and the CHANGELOG marks it **Breaking** under "Changed", with how to upgrade an adapter. `(*memstore.Store).Replace` is an addition. Nothing is removed, so no deprecation is needed. Exported surface: one interface method and one concrete method, each required by FR-001 and FR-004. | ✅ |
| **III. Test-first** | `TestRenewal_DoesNotRestoreDeleted` reproduces the defect deterministically and fails without the fix (observed in the spike, R-07). `tasks.md` will order every test before its code. Virtual time, with no sleeps for synchronization. The concurrent tests assert outcomes that hold for every schedule. `memstore.Replace` is exercised by tests and by `ExampleNew`. No new parser, so no new fuzz target. The session benchmarks are unaffected. Coverage: the module stays above 80% (96.0% in the spike). `internal/storetest` rises from an unjustified 78.9% to over 80%. | ✅ |
| **IV. Idiomatic Go** | 0 lint issues in the spike, and no `//nolint`. The `Store` doc comment states the contract of `Replace`, and `memstore.Replace` has its own doc comment. Errors are wrapped with `%w` ("session: renew: …", "memstore: replace: …"). `Replace` takes `context.Context` first. No panic, `init`, global or goroutine. `Authenticate` keeps the request-context deviation recorded in spec 007. | ✅ |
| **V. Simplicity & dependencies** | No dependency. One operation, the smallest the fix needs. These were rejected as heavier or open to the same race (R-02, R-03): an optional interface, compare-and-set, a separate activity key costing two reads per request, tombstones, and reading again after the write. | ✅ |
| Go toolchain support | `go 1.26.0` is unchanged, and nothing newer is used. | ✅ |
| Platforms | Pure Go. A mutex and map operations behave the same on the six targets. | ✅ |
| Repository layout | No tooling change, no lint override and no `.module-exceptions` entry. | ✅ |
| Dependency hygiene | `go.mod` and `go.sum` are unchanged, so `go mod tidy -diff` stays clean. | ✅ |
| Vulnerability scanning / Secrets | No dependency to scan. Test values are fixed strings with no secrets. The renewed record is scanned for secrets by the extended `TestStoreHoldsNoSecrets` (R-06). | ✅ |
| Local and PR gates | `make check MODULE=auth`. | ✅ |
| Commits / Release | The PR title is `fix(auth)!: keep sessions deleted while a request renews them`, with the `auth` scope and `!` for the breaking store change. The release is `auth/v0.2.0`. | ✅ |

**Result (pre-research)**: PASS. No new deviation.

**Result (post-design re-check)**: PASS.

- The data model adds no state, and the contracts add one operation, mapped to FR-001 to FR-005.
- The research found one gap at `auth/v0.1.0`: the 78.9% coverage of `internal/storetest`,
  below the per-package 80% floor and not justified in spec 007. The feature closes it with
  tests instead of a justification (R-07).
- The spec needs no change: no decision of the research contradicts it.

## Project Structure

### Documentation (this feature)

```text
specs/010-fix-session-renewal-race/
├── spec.md               # feature specification
├── plan.md               # this file
├── research.md           # Phase 0: decisions R-01 … R-09, with reproduction and spike evidence
├── data-model.md         # Phase 1: store operations, session key lifecycle, interleavings
├── quickstart.md         # Phase 1: validation guide
├── contracts/
│   ├── store.md          # changes to the store contract of spec 007, and the Redis mapping
│   └── public-api.md     # API changes and new doc comments (v0.1.0 → v0.2.0)
├── checklists/
│   └── requirements.md   # spec quality checklist
└── tasks.md              # Phase 2 (/speckit-tasks, not created here)
```

### Source Code (repository root)

```text
auth/
├── CHANGELOG.md                      # [Unreleased]: Changed (Breaking: Store.Replace), Fixed (renewal race)
├── README.md                         # Redis adapter + Replace via SetXX; why one command; "about thirty lines"
├── doc.go                            # "# Store": Replace must check and write atomically
├── store.go                          # Store gains Replace; contract and separation rule
├── integration_test.go               # flows moved into a helper with a renewal; + TestStoreSetCreatesOnly (FR-008)
├── memstore/
│   ├── memstore.go                   # + (*Store).Replace
│   ├── memstore_test.go              # Contract and Expiry_TTL extended; + TestReplaceParallel_NeverRevives
│   └── example_test.go               # ExampleNew shows Replace on a present and an absent key
├── session/
│   ├── session.go                    # renewal with Replace; write → ttl(rec, now); Authenticate doc
│   ├── issue.go                      # Issue: encodeRecord + Set with m.ttl
│   ├── helpers_test.go               # + interleaved store wrapper
│   └── session_test.go               # + TestRenewal_DoesNotRestoreDeleted, TestRenewal_ConcurrentLogout,
│                                     #   TestRenewal_DeletedServedAsAnonymous; Renewal_Writes counts OpReplace
└── internal/storetest/
    ├── storetest.go                  # + OpReplace, Recorder.Replace, Failing.Replace
    └── storetest_test.go             # Replace recorded and failed; error forwarding through Recorder
```

**Structure Decision**: the existing module `auth/` is changed in place, in the files that own
each concern:

- the contract stays in `store.go`;
- the in-memory implementation stays in `memstore/memstore.go`;
- renewal stays in `session/session.go`, and issuance in `session/issue.go`;
- the test wrapper goes in `session/helpers_test.go`, next to the other session helpers.

No new file is needed. Nothing outside `auth/` and this spec directory changes.

## Complexity Tracking

No new violation. The one deviation of the module is unchanged. `Authenticate`, `Issue` and
`Revoke` take their context from the request, without a first context parameter. Spec 007
records and justifies it in [007 plan.md](../007-http-auth/plan.md#complexity-tracking). The new
store operation takes `context.Context` as its first parameter.

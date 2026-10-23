# Quickstart: Validating Subject Revocation (auth)

This guide proves that the feature works end to end. The expected behavior is defined in:

- [contracts/public-api.md](contracts/public-api.md);
- [contracts/store.md](contracts/store.md);
- [data-model.md](data-model.md).

This guide only says how to check it. Test names follow the `Test<Area>_<Scenario>` convention
of the module, and `tasks.md` fixes them.

## Prerequisites

- Go 1.26 or newer, and a C toolchain for `-race`.
- golangci-lint at the version pinned in `versions.env`.
- The working tree contains the changes. The checks run from `auth/` with `GOWORK=off`, unless
  stated otherwise.

## 1. Before the check (red)

With `RevokeSubject`, the `gen` field and the reads at issuance in place, but without the check in
`Authenticate` and `Pending`:

```bash
GOWORK=off go test -race -run 'TestRevokeSubject_' -v ./session
```

**Expected**: FAIL for every test that expects a revoked session or pending login to give "no
credentials". In the spike, 9 of 12 failed. `ThenIssue`, `WindowAccepted` and `Retention` pass,
because they check what must stay valid
([research](research.md), "Spike").

## 2. Local gates (constitution)

```bash
make check MODULE=auth     # from the repository root: lint with 0 findings, then -race -shuffle=on with coverage ≥ 80%
GOWORK=off go test -cover ./session
```

**Expected**: `PASS` for `auth` from both `lint` and `test`. `session` stays above 80% (92.7% in
the spike). No `.module-exceptions` entry is added.

## 3. Tidy and API compatibility

```bash
GOWORK=off go mod tidy -diff     # prints nothing
git diff --exit-code go.mod go.sum
```

`gorelease` runs in the pull request workflow, at the version pinned in `versions.env`.

**Expected**: no change to `go.mod` or `go.sum`. `gorelease` reports one compatible addition,
`RevokeSubject`, and suggests `v0.3.0`
([contracts/public-api.md](contracts/public-api.md#expected-gorelease-report-against-authv020)).

## 4. Scenario checks per user story

| Story | Command | What must hold |
|-------|---------|----------------|
| US1: a password change | `GOWORK=off go test -race -run 'RevokeSubject_(EndsSessions\|ThenIssue\|ServedAsAnonymous)' -v ./session` | Three sessions of the subject, fresh or due for renewal, give no credentials; another subject's session authenticates. A session issued right after the revocation, in the same request, authenticates until its absolute lifetime. A revoked cookie on an anonymous route reaches the handler with no principal. |
| US2: from outside a request | `GOWORK=off go test -race -run 'RevokeSubject_(SharedStore\|Errors)' -v ./session` | A revocation through one of two managers on one store ends the sessions of both. An empty subject makes no store call. A failing store makes `RevokeSubject`, `Authenticate` (500 through the middleware), `Issue`, `IssuePending` (no cookie) and `Pending` fail, never "no credentials". A retry after recovery revokes. |
| US3: pending logins | `GOWORK=off go test -race -run 'RevokeSubject_(Pending\|WindowAccepted)' -v ./session` | A pending login issued before the revocation reads as cancelled; one issued after, or of another subject, works. A pending login read before the revocation and completed after it yields a valid session (FR-006). |
| US4: upgrade and rollback | `GOWORK=off go test -race -run 'RevokeSubject_PreviousRelease\|TestRecord_' -v ./session` | Records in the 0.2.0 format authenticate, then give no credentials after a revocation. A record of this release decodes into the 0.2.0 shape. A never revoked subject's record is byte for byte the 0.2.0 one. |
| Interleavings and clocks | `GOWORK=off go test -race -run 'RevokeSubject_(Interleavings\|SameInstant\|Concurrent)' -v ./session` | Every row of [data-model.md §4](data-model.md#4-interleavings) holds. At one virtual instant, the session issued before the revocation is revoked and the one issued after is not. With 100 requests after a concurrent revocation, none is authenticated, and `-race` reports nothing. |
| Retention | `GOWORK=off go test -race -run 'RevokeSubject_Retention' -v ./session` | The state is stored 8 hours after the revocation and gone at 9 hours. A session issued 2 hours after the revocation authenticates past 9 hours. |
| Cost | `GOWORK=off go test -race -run 'Authenticate_Principal\|Renewal_Writes' -v ./session` | Exactly two `Get` calls per session request; renewal writes as often as before. |
| Store invariants and secrets | `GOWORK=off go test -race -run 'StoreSetCreatesOnly\|StoreHoldsNoSecrets' -v .` | A revocation ran. Apart from revocation states, every `Set` targets a key that no earlier call used. No key or value holds a secret, a subject in a key, or an identifier. |
| Example | `GOWORK=off go test -run 'ExampleManager_RevokeSubject' -v ./session` | The phone's session gives no credentials, and the laptop stays signed in. |

## 5. Benchmark (SC-005)

```bash
GOWORK=off go test -run '^$' -bench 'SessionAuthenticate' ./session
GOWORK=off go test -run '^$' -bench 'BenchmarkCheck$' -benchtime=20x ./password
```

**Expected**: a session check stays under 1% of a password check. In the spike they took about
3.6 µs and 18 ms.

## 6. Documentation review (FR-018, FR-019)

- `auth/session/revocation.go`, `session.go`, `issue.go` and `pending.go`: the doc comments match
  [public-api.md](contracts/public-api.md#package-session).
- `auth/store.go` and `auth/doc.go`: the store holds revocations, and must keep keys until they
  expire.
- `auth/README.md`:
  - "Sessions and CSRF" says two store reads;
  - "Revoking every session of a user" shows revoke-then-issue and states the cost, the window
    of FR-006, the retention and the rolling upgrade;
  - "Store" states the two requirements of [store.md §4](contracts/store.md#4-requirements-on-the-deployment-of-the-store).
- `auth/CHANGELOG.md`, `[Unreleased]`: "Added", as drafted in
  [research R-01](research.md#r-01-release-level-and-api-compatibility).

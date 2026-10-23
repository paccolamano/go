# Quickstart: Validating the Session Renewal Fix (auth)

This guide proves that the feature works end to end. The expected behavior is defined in:

- [contracts/store.md](contracts/store.md);
- [contracts/public-api.md](contracts/public-api.md);
- [data-model.md](data-model.md).

This guide only says how to check it. Test names follow the `Test<Area>_<Scenario>` convention
of the module, and `tasks.md` fixes them.

## Prerequisites

- Go 1.26 or newer, and a C toolchain for `-race`.
- golangci-lint at the version pinned in `versions.env`.
- The working tree contains the changes. The checks run from `auth/` with `GOWORK=off`, unless
  stated otherwise.

## 1. The defect, before the fix (red)

With the store, `memstore` and test changes in place, but renewal still calling `Set`:

```bash
GOWORK=off go test -race -run 'TestRenewal_DoesNotRestoreDeleted' -v ./session
```

**Expected**: FAIL in the cases where the deletion lands between the read and the write, for
both a logout and a new login. The renewing request is authenticated, and the deleted session
authenticates later requests.

## 2. Local gates (constitution)

```bash
make check MODULE=auth     # from the repository root: lint with 0 findings, then -race -shuffle=on with coverage ≥ 80%
GOWORK=off go test -cover ./internal/storetest ./session ./memstore
```

**Expected**: `PASS` for `auth` from both `lint` and `test`. Every package listed is at 80% or
above, `internal/storetest` included ([research R-07](research.md#r-07-testing-approach-fr-013-sc-001-to-sc-006-principle-iii)).
No `.module-exceptions` entry is added.

## 3. Tidy and API compatibility

```bash
GOWORK=off go mod tidy -diff     # prints nothing
git diff --exit-code go.mod go.sum
```

`gorelease` runs in the pull request workflow, at the version pinned in `versions.env`.

**Expected**: no change to `go.mod` or `go.sum`. `gorelease` reports the method added to
`auth.Store` as the only incompatible change, and suggests `v0.2.0`
([contracts/public-api.md](contracts/public-api.md#expected-gorelease-report-against-authv010)).

## 4. Scenario checks per user story

| Story | Command | What must hold |
|-------|---------|----------------|
| US1, US2: deletion during renewal | `GOWORK=off go test -race -run 'Renewal_' -v ./session` | A logout or a new login: <br>– landing before the read or between the read and the write, the renewing request has no credentials; <br>– landing after the write, it is authenticated. <br>In every case, later requests with the deleted identifier have no credentials, and after a login the new session authenticates. A store TTL that elapses between the read and the write gives no credentials, and the key stays absent. With 100 concurrent renewals and a logout, no request after the logout is authenticated, and `-race` reports nothing. Renewal writes once per 7.5 minutes, as `OpReplace`, and a failing `Replace` gives an error, not "no credentials". |
| US1: anonymous routes | `GOWORK=off go test -race -run 'Renewal_' -v ./session` | Under `auth.WithAnonymous`, a request whose renewal finds its session deleted reaches the handler with no principal. |
| US3: in-memory store | `GOWORK=off go test -race -run 'Store_Contract\|Expiry_TTL\|ReplaceParallel_\|ExampleNew' -v ./memstore` | `Replace` stores over a present key with a new expiry. It returns `false` and stores nothing for an absent or expired key. It rejects `ttl` 0 and a cancelled context. After 100 concurrent runs of `Replace` and `Delete` on one key, the key is absent each time. |
| FR-008: no read-then-write | `GOWORK=off go test -race -run 'StoreSetCreatesOnly\|StoreHoldsNoSecrets' -v .` | Across every flow, renewal included, each `Set` targets a key that no earlier store call used. The renewed record contains no secret. |
| Test doubles | `GOWORK=off go test -race -v ./internal/storetest` | `Recorder` records `Replace` with its value. `Failing` fails `OpReplace` on request and forwards it otherwise. Errors from the next store are wrapped. |

## 5. Documentation review (FR-005, FR-011, FR-012)

- `auth/store.go`: the `Store` doc comment matches
  [public-api.md](contracts/public-api.md#store-changed-incompatible).
- `auth/doc.go`, `auth/session/session.go` (`Authenticate`) and `auth/memstore/memstore.go`
  (`Replace`) match the same contract.
- `auth/README.md`, "Store": the adapter has `Replace` with `SetXX`, the paragraph explains the
  single command, and the introduction says "about thirty lines".
- `auth/CHANGELOG.md`, `[Unreleased]`: "Changed" with the breaking store operation, and "Fixed"
  with the session defect, as drafted in
  [research R-01](research.md#r-01-release-level-and-api-compatibility).

## 6. Optional: the README Redis adapter compiles

No gate covers this check, because the module does not depend on go-redis. It needs go-redis
v9 in the module cache, or network access to download it. In a scratch directory outside the
repository:

1. Create a module that requires `github.com/redis/go-redis/v9` and points
   `github.com/paccolamano/go/auth` to the local `auth/` with a `replace` directive. The
   directive stays in the scratch module only, never in a committed `go.mod`.
2. Copy the README adapter into a file. Add the imports, and the assertion
   `var _ auth.Store = redisStore{}`.
3. Run `GOWORK=off go vet .`

**Expected**: no output. The adapter satisfies `auth.Store`, `Replace` included.

# Quickstart: Validating the auth Module

**Plan**: [plan.md](plan.md) | **API**: [contracts/public-api.md](contracts/public-api.md) | **Semantics**: [contracts/http-semantics.md](contracts/http-semantics.md)

This guide proves the feature end to end once it is implemented. It lists the commands, the
tests that back each user story and success criterion, and the expected outcomes. It contains
no implementation code. The test names below are the ones `tasks.md` will create.

## Prerequisites

- Go 1.26 and Go 1.27 (`versions.env`: `GO_VERSION_OLDEST`, `GO_VERSION_NEWEST`).
- golangci-lint v2.14.0.
- For the security gates, run locally only when needed:
  - Trivy and Gitleaks, at the pinned versions;
  - Bearer, installed with `scripts/install-tool.sh bearer <dir>`;
  - Docker, for the pinned Semgrep image.
- No network access is needed by the tests. Key sets are served from memory.

## Setup

```bash
make work                 # adds ./auth to go.work
cd auth && GOWORK=off go mod tidy -diff   # expected: no output
```

`auth/go.mod` must require exactly `github.com/go-jose/go-jose/v4 v4.1.5` and
`golang.org/x/crypto v0.57.0`, plus `golang.org/x/sys` as an indirect requirement (SC-012).

## Local gates

```bash
make check MODULE=auth
```

Expected:

- golangci-lint reports 0 findings, with at most one justified `//nolint:contextcheck`
  (research R-21);
- `go test -race -shuffle=on ./...` passes;
- statement coverage is ≥ 80% (SC-014).

## Validation by user story

Run each one from `auth/` with `GOWORK=off go test -race -shuffle=on <packages> -run '<pattern>'`.

| Story | Packages | Tests | Expected |
|-------|----------|-------|----------|
| US1 Protect routes | `.` | `TestNew`, `TestAny`, `TestChallenges`, `TestRequireMethods`, `TestThirdPartyMethod` | statuses and challenges exactly as in [http-semantics.md](contracts/http-semantics.md) §1–3; a method defined in the test combines with no core change (SC-013) |
| US2 Passwords | `./password` | `TestHash`, `TestCheck`, `TestLegacyHashes`, `TestRehash`, `TestLength`, `TestAttemptLimit` | scenarios 1–13 of User Story 2; bcrypt with 73 bytes is rejected |
| US3 Sessions | `./session` | `TestIssue`, `TestAuthenticate`, `TestRenewal`, `TestExpiry`, `TestRevoke`, `TestCookie` | cookie attributes of §6; one `Get` per request and at most one `Set` per renewal interval (SC-006) |
| US4 Second factor | `./session ./totp ./recovery .` | `TestPending`, `TestAttempt`, `TestVerify`, `TestConfirm`, `TestEnrolled`, `TestLoginFlow` | 5 shared attempts; replay rejected; `["pwd","otp","mfa"]` and `["pwd","mfa"]`; RequireMethods("mfa") answers 403 for a password-only session |
| US5 API keys | `./apikey` | `TestGenerate`, `TestAuthenticate`, `TestHeader` | key format `<prefix>_<16>_<52>`; foreign Bearer credentials are none; 403 under RequireMethods("mfa") |
| US6 Tokens | `./jwt` | `TestVerify`, `TestClaims`, `TestType`, `TestForgeryCorpus`, `TestKeySet`, `TestStepUp` | the forgery corpus is rejected; key set behavior of §9; 401 with RFC 9470 challenge under RequireMethods("mfa") |
| US7 Basic | `./basic` | `TestAuthenticate`, `TestRealm` | challenge `Basic realm="api", charset="UTF-8"`; malformed headers invalid; 429 with the attempt limit |
| Store | `./memstore` | `TestStore`, `TestExpiry`, `TestIncrParallel`, `TestSweep` | contract of [store.md](contracts/store.md) §1 and §4 |

All the time-dependent tests run inside `testing/synctest` bubbles and take milliseconds of real
time (FR-070).

## Validation by success criterion

| Criterion | How | Expected |
|-----------|-----|----------|
| SC-001 timing | internal test `TestDummyHashParams` in `./password`; `go test -run '^$' -bench 'BenchmarkCheck' -count 5 ./password` | the dummy hash has the checker's parameters; `missing-user` and `wrong-password` differ by ≤ 5% |
| SC-002 failures → 500 | `TestInfrastructureFailures` in `.` (lookup, store and key server failures for every method) | always 500, never 401; attempt counters unchanged |
| SC-003 no secrets in the store | `TestStoreHoldsNoSecrets` in `.` (runs every flow, then scans every memstore key and value) | 0 occurrences of any password, identifier, key, secret or code, raw or encoded |
| SC-004 parallel attempts | `TestSecondFactorParallel` (`./totp`, `./recovery`), `TestAttemptLimitParallel` (`./password`) | 1 acceptance out of 50 identical codes; ≤ 5 evaluations per pending login; ≤ 10 password verifications |
| SC-005 fixation | `TestIssue` in `./session` | new identifier every time; the old one authenticates 0 requests |
| SC-006 store cost | `TestRenewal` in `./session`, with a counting store wrapper | 1 `Get` per request; writes ≤ number of renewal intervals |
| SC-007 forgeries | `TestForgeryCorpus` in `./jwt` | 100% rejected with 401; 100% of the control tokens accepted |
| SC-008 key set downloads | `TestKeySetRefreshBound` in `./jwt` | ≤ 1 download for 1,000 unknown `kid`s within an interval; 0 downloads while fresh |
| SC-009 costs | `go test -run '^$' -bench 'Benchmark(Check\|SessionAuthenticate\|APIKeyAuthenticate\|JWTAuthenticate)' ./...` | session, API key and token each < 1% of `BenchmarkCheck` (the spike measured 17 ms, against 34–67 µs for tokens) |
| SC-010 vectors | `TestRFC4226Vectors`, `TestRFC6238Vectors` in `./internal/hotp` | 100% pass |
| SC-011 logs | `TestNoSecretsInLogs` in `.` | the known secrets appear 0 times in `slog` output and in error messages |
| SC-012 dependencies | inspect `auth/go.mod` (Setup) | exactly the two direct requirements |
| SC-013 extensibility | `TestThirdPartyMethod` in `.` | passes with no change to the core |
| SC-014 fuzz and coverage | the fuzz targets below, and `make check` | no crash; coverage ≥ 80% |

## Fuzzing

Each target runs for 30 seconds locally. In CI the seed corpus runs as ordinary tests.

```bash
GOWORK=off go test -run '^$' -fuzz '^FuzzBasicHeader$'   -fuzztime 30s ./basic
GOWORK=off go test -run '^$' -fuzz '^FuzzSessionCookie$' -fuzztime 30s ./session
GOWORK=off go test -run '^$' -fuzz '^FuzzTOTPCode$'      -fuzztime 30s ./totp
GOWORK=off go test -run '^$' -fuzz '^FuzzRecoveryCode$'  -fuzztime 30s ./recovery
GOWORK=off go test -run '^$' -fuzz '^FuzzPHC$'           -fuzztime 30s ./password
GOWORK=off go test -run '^$' -fuzz '^FuzzAPIKey$'        -fuzztime 30s ./apikey
GOWORK=off go test -run '^$' -fuzz '^FuzzToken$'         -fuzztime 30s ./jwt
GOWORK=off go test -run '^$' -fuzz '^FuzzKeySet$'        -fuzztime 30s ./jwt
GOWORK=off go test -run '^$' -fuzz '^FuzzCacheControl$'  -fuzztime 30s ./jwt
```

Expected: no failure. Each target also checks its parser's invariants, for example that every
accepted PHC string re-encodes to itself.

## Security gates (as in CI)

From the repository root:

```bash
trivy fs --scanners vuln --severity HIGH,CRITICAL --exit-code 1 --ignorefile .trivyignore .
gitleaks git --redact --exit-code 1 .
bearer scan . --config-file bearer.yml
docker run --rm -v "$PWD:/src" -w /src "$SEMGREP_IMAGE" semgrep scan --metrics=off --error \
  --config p/golang --config p/trailofbits --config p/github-actions
```

Expected:

- **Trivy**, **Gitleaks** and **Semgrep**: 0 findings. Test keys and tokens are generated at run
  time, so no literal reaches the repository.
- **Bearer**: 0 findings, after the single justified entry in `bearer.ignore` for the
  `crypto/sha1` import of `auth/internal/hotp` (research R-21).

## End-to-end walkthrough

The examples run as tests (`go test -run Example ./...`) and are compile-checked documentation:

1. `ExampleNew`: a mux protected by `auth.New(auth.Any(sessions, keys, tokens))`. A
   `/login` handler calls `Check`, then `IssuePending` or `Issue`. A `/login/otp` handler calls
   `Pending`, `Verify` and `Issue`. `/logout` calls `Revoke`. `/admin` is wrapped in
   `RequireMethods("mfa")`.
2. `ExampleRequireMethods`: 403 for a password-only session, 200 after the second factor.
3. `password.ExampleChecker`, `session.ExampleManager`, `totp.ExampleNewEnrollment`,
   `recovery.ExampleGenerate`, `basic.ExampleNew`, `apikey.ExampleKeys_Generate`,
   `jwt.ExampleNew` (with static keys) and `memstore.ExampleNew`.

Expected: every example's `// Output:` matches.

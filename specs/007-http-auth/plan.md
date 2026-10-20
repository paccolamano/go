# Implementation Plan: HTTP Request Authentication (auth)

**Branch**: `007-http-auth` (suggested; work currently on `main`) | **Date**: 2026-10-20 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `specs/007-http-auth/spec.md`

**Note**: This template is filled in by the `/speckit-plan` command; its definition describes the execution workflow.

## Summary

This feature adds a new independent module, `github.com/paccolamano/go/auth`, which
authenticates HTTP requests. It has a small core and one package per method:

- **Core** (`auth`):
  - `Authenticator` and an optional `Challenger`;
  - `Principal{Subject, Methods, Extra}` and six sentinel errors;
  - `Any`, which combines methods in order;
  - `New`, the middleware: 401 with summed challenges, 429, 500, an anonymous option and a
    `RejectFunc`;
  - `RequireMethods`: 401 with RFC 9470 for tokens, 403 for the other methods;
  - `FromContext`, and the `Store` and `Pending` interfaces.
- **`memstore`**: an in-memory `Store` with an atomic `Incr` and no goroutine.
- **`password`**:
  - argon2id in PHC format (OWASP defaults, with a floor on configured parameters);
  - verification of legacy bcrypt hashes, rejecting passwords over 72 bytes;
  - a dummy hash for unknown users, and rehashing through a callback;
  - an optional per-username attempt limit in the store.
- **`session`**:
  - `__Host-` cookies with hashed identifiers in the store;
  - absolute and idle expiry, with renewal at most every quarter of the idle timeout;
  - fixation-safe `Issue`, pending logins, and the shared 5-attempt second-factor budget
    (`Pending.Attempt`).
- **`totp`** and **`recovery`**:
  - RFC 6238 on `crypto/hmac`, with per-step replay protection and enrollment through an
    `otpauth://` URI and a confirmed first code;
  - single-use 80-bit recovery codes stored as SHA-256 hashes, consumed through the developer's
    callback.
- **`basic`**, **`apikey`** and **`jwt`**:
  - HTTP Basic on the password checker;
  - `<prefix>_<id>_<secret>` keys stored as SHA-256 hashes;
  - token verification on go-jose v4, with algorithms bound to keys, mandatory `exp` and no
    `crit`, and a lazily downloaded, rate-limited key set cache.

Design decisions validated by a spike ([research.md](research.md)):

- **go-jose gaps** (R-17): it accepts `alg` mismatches between RSA algorithms, `crit: ["b64"]`
  and tokens without `exp`, and a single bad key fails a whole key set. The module adds a key
  binding, rejects `crit`, requires `exp` and parses keys one by one.
- **bcrypt** (R-10): `CompareHashAndPassword` accepts a 73-byte password whose first 72 bytes
  match, so the explicit length rule is required.
- **Costs**:
  - argon2id at the defaults takes 17.0 ms and 19.9 MB per check;
  - token verification takes 34–67 µs and an API key hash 79 ns, all under 1% of a password
    check (SC-009).
- **Scanners** (R-21):
  - Bearer has one finding, the `crypto/sha1` import required by RFC 6238, suppressed with a
    justification;
  - Semgrep, Trivy and golangci-lint are clean, apart from one `errcheck` pattern, which is
    resolved in the code.

## Technical Context

**Language/Version**: Go. The `go` directive is `go 1.26.0`, the oldest version the
constitution supports. CI also tests on 1.27. Features used:

- `testing/synctest`, `http.CrossOriginProtection` (in the README) and `sync.WaitGroup.Go`
  (Go 1.25);
- `testing.B.Loop` and `crypto/rand.Read` that never fails (Go 1.24);
- `context.WithoutCancel` (Go 1.21).

**Primary Dependencies**:

- `golang.org/x/crypto v0.57.0` (`argon2`, `bcrypt`);
- `github.com/go-jose/go-jose/v4 v4.1.5` (`jose`, `jwt`);
- `golang.org/x/sys v0.48.0`, an indirect requirement.

Nothing else, and no other module of this repository. Tests use the standard library only.
Justification: [research.md R-02](research.md#r-02-dependencies-principle-v).

**Storage**: none of its own. The short-lived state lives in the developer's `auth.Store`
([contracts/store.md](contracts/store.md)). Durable records stay in the developer's database
and are read through lookups ([data-model.md](data-model.md) §3).

**Testing**: the standard `testing` package:

- external test packages per package, and internal tests for HOTP, PHC bounds, the memstore
  sweep and the key set parser;
- `testing/synctest` for every time-dependent behavior, and an in-memory `RoundTripper` for key
  sets;
- 50-goroutine `-race` tests for every atomic guarantee;
- RFC 4226 and RFC 6238 vectors;
- 9 fuzz targets, 4 benchmark families and an `Example` for each primary API.

Details: [research.md R-20](research.md#r-20-testing-approach-principle-iii).

**Target Platform**: any platform Go supports. The code is pure Go with no build constraints,
for linux, darwin and windows on amd64 and arm64.

**Project Type**: library. One Go module (`auth/go.mod`) with 9 public packages and 5 internal
ones.

**Performance Goals** (SC-001, SC-006, SC-009):

- password check ≈ 17 ms at the defaults (the spike, 19 MiB, t=2, p=1), with unknown users
  within 5% of wrong passwords;
- session: 1 store read per request, and 1 write per renewal interval at most;
- API key: SHA-256 plus a constant-time compare, 79 ns, plus the developer's lookup;
- token: 34.5 µs (RS256), 48.7 µs (EdDSA) and 66.8 µs (ES256) with cached keys, and 0
  downloads while the key set is fresh.

**Constraints**:

- No package-level mutable state. The only package-level variables are sentinel errors.
  Limits and defaults are `const`.
- No `init`, no `panic`, no output and no logging of its own. No goroutine: key set downloads
  run in the request that triggers them (R-18). x/crypto's argon2 starts one goroutine per lane
  for each of its four slices, also with parallelism 1 (`argon2.go`, `processBlocks`), and joins
  them before returning, so none outlives the call.
- Secrets never reach the store, logs or error messages (FR-008, FR-022).

**Scale/Scope**:

- about 2,900 lines of library code in about 35 files;
- 118 exported identifiers ([contracts/public-api.md](contracts/public-api.md));
- 72 functional requirements and 14 success criteria.

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

| Rule | Assessment | Status |
|------|------------|--------|
| **I. Independent modules** | One new top-level module, `auth/`, with module path `github.com/paccolamano/go/auth`, its own `go.mod`, a README.md and a CHANGELOG.md. No `replace`, and no dependency on other modules of the repository: the rejection type is defined locally (R-06). It builds and tests with `GOWORK=off`. Its single purpose is authenticating HTTP requests. Authorization is out of scope. | ✅ |
| **II. SemVer & stable API** | Tags are `auth/vX.Y.Z`, starting at v0.1.0. The surface is the minimum the spec needs: every identifier in [public-api.md](contracts/public-api.md) maps to an FR. Every constructor that has settings takes `...Option`. The departures from the request's sketch are listed and justified. go-jose types never appear in the API, so its next major version cannot break ours (R-17). CHANGELOG from the first change. | ✅ |
| **III. Test-first** | `tasks.md` will order test tasks before implementation tasks. Table-driven subtests use the standard `testing` package. Tests are deterministic: `synctest` virtual time, in-memory key sets, generated keys, no network and no sleeps. The required fuzz targets cover every external input: Authorization headers, cookies, codes, PHC strings, API keys, tokens, key set documents and the key server's `Cache-Control` header. Benchmarks cover the code declared performance-sensitive (SC-009). There are examples for every primary API. Coverage target: ≥ 80%. | ✅ |
| **IV. Idiomatic Go** | The repository's golangci-lint config applies unchanged. The spike showed only `errcheck` on `Body.Close`, solved in code (R-21). At most one `//nolint:contextcheck`, with its reason, if the detached download context triggers it. Everything exported is documented. Errors are wrapped with `%w`, and the sentinels work with `errors.Is`. No `panic`, `init`, global state, output or logger. Store methods, lookups, `Check`, `Attempt`, `Verify`, `Confirm` and `Enrolled` take a `context.Context` as their first parameter. The methods bound to an HTTP request (`Authenticate`, `Issue`, `IssuePending`, `Pending`, `Revoke`) take the context from `r.Context()` instead: a deviation recorded in Complexity Tracking (spec FR-009, clarified 2026-10-20). No goroutines. | ✅ (one recorded deviation) |
| **V. Simplicity & dependencies** | Two dependencies, both explicitly requested and justified in R-02: why the standard library is insufficient, maintenance, license (BSD-3, Apache-2.0), footprint (only `x/sys` indirect), and a clean Trivy run. Every abstraction maps to a requirement: `Challenger` (FR-015, FR-017), `Pending` (FR-045), `Store` (FR-020), `basic.Checker` (a consumer-defined interface for tests). No cgo. | ✅ |
| Go toolchain support | `go 1.26.0` is not above `GO_VERSION_OLDEST=1.26`. Every feature used exists in 1.26, and x/crypto v0.57.0 also declares `go 1.26.0`. | ✅ |
| Platforms | Pure Go, with no OS-specific calls. No `.module-exceptions` entry. | ✅ |
| Repository layout | No tooling change beyond `make work` (`go.work`) and one `bearer.ignore` entry (below). The shared lint config is used without overrides. | ✅ |
| Dependency hygiene | `go mod tidy -diff` must be clean. The spike's tidy result is two direct requirements and `x/sys` indirect. | ✅ |
| Vulnerability scanning / Secrets | Trivy reports 0 HIGH and 0 CRITICAL on the chosen versions. Test keys and tokens are generated at run time, and RFC test-vector secrets are the RFC's ASCII strings. README examples use placeholders such as `acme_<id>_<secret>`. Gitleaks must pass without suppressions: a fixture that trips a rule is changed, not ignored. | ✅ |
| Finding suppression | One Bearer finding, `go_gosec_blocklist_sha1` on `auth/internal/hotp`, suppressed through `bearer ignore add` with a written justification: RFC 6238 compatibility, and HMAC-SHA-1 does not rely on collision resistance (RFC 6194). This is the tool's own mechanism, scoped to one fingerprint, as the constitution allows. | ✅ |
| Local and PR gates | `make check MODULE=auth` runs lint, then race and shuffle tests with the coverage gate. The tooling discovers the module on its own. | ✅ |
| Commits / Release | The PR title is `feat(auth): add HTTP request authentication module`. The release is `auth/v0.1.0`, with a CHANGELOG section. | ✅ |

**Result (pre-research)**: PASS. No deviation needs a Complexity Tracking entry.

**Result (post-design re-check)**: PASS.

- The data model and the three contracts add no dependency beyond R-02, and no global state,
  goroutine or module exception.
- Every exported identifier in [contracts/public-api.md](contracts/public-api.md) maps to a
  functional requirement (traceability table).
- The only scanner suppression is the Bearer `crypto/sha1` entry, and the only possible lint
  suppression is the `contextcheck` of R-21.
- Three refinements to the spec came out of the spike, and the spec and its checklist were
  updated (checklist, Iteration 3):
  - FR-015: challenges are guaranteed when a method defines one;
  - FR-045: an attempt is counted before its code is evaluated;
  - FR-069: any `crit` header is rejected.
- **Re-check after `/speckit-analyze`** (2026-10-20): PASS, with one recorded deviation.
  - Principle IV: the request-bound methods take their context from `r.Context()`. This is now
    recorded in Complexity Tracking, after the clarification of FR-009.
  - Principle III: the key server's `Cache-Control` header is external input, so it gets its own
    fuzz target, `FuzzCacheControl` (FR-004).

## Project Structure

### Documentation (this feature)

```text
specs/007-http-auth/
├── spec.md                  # feature specification
├── plan.md                  # this file
├── research.md              # Phase 0: decisions R-01 … R-22, with spike measurements
├── data-model.md            # Phase 1: principal, outcomes, store records, lifecycles, defaults
├── quickstart.md            # Phase 1: validation guide
├── contracts/
│   ├── public-api.md        # complete exported Go API, departures, FR traceability
│   ├── http-semantics.md    # statuses, challenges, cookies, per-method parsing, key set downloads
│   └── store.md             # Store contract, key layout, value format, memstore, Redis adapter
├── checklists/
│   └── requirements.md      # spec quality checklist
└── tasks.md                 # Phase 2 (/speckit-tasks, not created here)
```

### Source Code (repository root)

```text
go.work                          # + ./auth (make work)
bearer.ignore                    # + 1 entry: crypto/sha1 in auth/internal/hotp (R-21)

auth/
├── go.mod                       # module github.com/paccolamano/go/auth; go 1.26.0;
│                                # go-jose/v4 v4.1.5, x/crypto v0.57.0, x/sys (indirect)
├── go.sum
├── README.md                    # purpose, go get, full example, Redis adapter, CSRF, Basic cost,
│                                # middleware order, argon2 parameters, caveats (R-22)
├── CHANGELOG.md                 # Keep a Changelog; [Unreleased] → [0.1.0]
├── doc.go                       # package doc: outcomes, combination, challenges, statuses
├── auth.go                      # Authenticator, Challenger, Principal, Pending, sentinel errors
├── store.go                     # Store
├── any.go                       # Any, leaf flattening, decision with the deciding leaf
├── middleware.go                # New, Option, WithAnonymous, WithRejectFunc, Rejection,
│                                # RejectFunc, context state, FromContext
├── reject.go                    # challenge composition, rejection writing
├── require.go                   # RequireMethods
├── middleware_test.go           # US1: outcomes, statuses, anonymous, RejectFunc
├── any_test.go                  # US1-2…4, contract violations, nested Any
├── challenge_test.go            # FR-015: composition, dedup, same-scheme replacement
├── require_test.go              # FR-019: 401 step-up, 403, no principal, missing state
├── extension_test.go            # SC-013: TestThirdPartyMethod
├── integration_test.go          # TestLoginFlow, TestInfrastructureFailures, TestStoreHoldsNoSecrets,
│                                # TestNoSecretsInLogs (all methods, memstore, synctest)
├── example_test.go              # ExampleNew, ExampleRequireMethods, ExampleAny
├── internal/
│   ├── storekey/                # Key(kind, inputs...) → auth:<kind>:<digest>
│   ├── crockford/               # encode, decode, normalize (const alphabet)
│   ├── authheader/              # Credentials(r, scheme), Quote(value), Scheme(challenge)
│   ├── hotp/                    # generic HOTP (algorithm, digits); the only crypto/sha1 import;
│   │                            # TestRFC4226Vectors, TestRFC6238Vectors
│   └── storetest/               # test support: Recorder (records calls, keys, values) and
│                                # Failing (fails chosen operations) around any auth.Store
├── memstore/
│   ├── memstore.go              # Store, New, entries, lazy expiry, amortized sweep
│   ├── memstore_test.go         # contract, TestExpiry (synctest), TestIncrParallel
│   ├── memstore_internal_test.go# TestSweep
│   └── example_test.go
├── password/
│   ├── params.go                # Params, DefaultParams, floor, stored-hash bounds
│   ├── phc.go                   # argon2id PHC encode/parse, bcrypt detection
│   ├── hash.go                  # Hash, ErrEmpty, ErrTooLong
│   ├── checker.go               # Checker, NewChecker, Check, Checker.Hash, dummy hash, rehash
│   ├── options.go               # Option, WithParams, WithMaxLength, WithRehash,
│   │                            # WithAttemptLimit, AttemptLimit, LookupFunc, RehashFunc, Account
│   ├── *_test.go                # US2; TestDummyHashParams and FuzzPHC (internal);
│   │                            # TestAttemptLimitParallel; BenchmarkCheck
│   └── example_test.go
├── session/
│   ├── session.go               # Manager, New, options, Authenticate, Challenge, renewal
│   ├── issue.go                 # Issue, IssuePending, Revoke, cookies
│   ├── pending.go               # Pending, the auth.Pending implementation, Attempt
│   ├── record.go                # versioned JSON records
│   ├── *_test.go                # US3, US4 (pending, Attempt), FuzzSessionCookie,
│   │                            # BenchmarkSessionAuthenticate
│   └── example_test.go
├── totp/
│   ├── totp.go                  # Verifier, New, Enrolled, Verify, Confirm, replay (uses internal/hotp)
│   ├── enrollment.go            # Enrollment, NewEnrollment, URI, LogValue, String
│   ├── *_test.go                # US4, TestSecondFactorParallel, FuzzTOTPCode (codes computed
│   │                            # with internal/hotp)
│   └── example_test.go
├── recovery/
│   ├── codes.go                 # Codes, Generate, DefaultCount, normalization, hashing
│   ├── recovery.go              # Verifier, New, Verify, guard, consume
│   ├── *_test.go                # US4, TestSecondFactorParallel, FuzzRecoveryCode
│   └── example_test.go
├── basic/
│   ├── basic.go                 # Checker, Authenticator, New, WithRealm, parsing, challenge
│   ├── *_test.go                # US7, FuzzBasicHeader
│   └── example_test.go
├── apikey/
│   ├── apikey.go                # Keys, New, WithHeader, Authenticate, Challenge
│   ├── key.go                   # Key, Stored, Account, LookupFunc, Generate, parsing
│   ├── *_test.go                # US5, FuzzAPIKey, BenchmarkAPIKeyAuthenticate
│   └── example_test.go
└── jwt/
    ├── jwt.go                   # Verifier, New, options, Challenge, Claims
    ├── verify.go                # shape, parse, crit, typ, selection, claims (R-17)
    ├── keys.go                  # bound keys, static key validation
    ├── keyset.go                # remote cache, single-flight download, Cache-Control (R-18)
    ├── parse.go                 # per-key JWKS parsing
    ├── *_test.go                # US6, TestForgeryCorpus, TestKeySet, TestKeySetRefreshBound,
    │                            # TestStepUp, FuzzToken, FuzzKeySet and FuzzCacheControl
    │                            # (internal), BenchmarkJWTAuthenticate
    └── example_test.go
```

**Structure Decision**: one new top-level module, `auth/`, laid out as above.

- **The core** sits at the module root, so the import path is the request's `auth`.
- **Each method is a package**, imported only by the applications that use it.
- **The internal packages** each serve at least two consumers:
  - `storekey`: session, password, totp and recovery;
  - `crockford`: apikey and recovery;
  - `authheader`: basic, apikey, jwt and the core's challenge quoting;
  - `hotp`: totp, and the tests of totp and of the root package, which compute valid codes
    without a second `crypto/sha1` import;
  - `storetest`: the tests of the root package, password, session, totp and recovery. It has
    tests of its own, because the coverage gate counts every package.
- **The cross-method tests** live in the root package's external tests: the full login flow,
  infrastructure failures, the scan of the store and the scan of the logs (SC-002, SC-003,
  SC-011).
- **Nothing else changes** in the repository, except `go.work`, which `make work`
  regenerates, and one `bearer.ignore` entry added when `auth/internal/hotp/hotp.go` exists.

## Complexity Tracking

| Violation | Why Needed | Simpler Alternative Rejected Because |
|-----------|------------|-------------------------------------|
| Principle IV, "Functions that perform I/O or may block MUST accept a `context.Context` as their first parameter": `Authenticator.Authenticate(r)`, `session.Manager.Issue(w, r, p)`, `IssuePending(w, r, p)`, `Pending(r)` and `Revoke(w, r)` read or write the store, and `jwt` and `apikey` call the network or the developer's lookup, without a first context parameter. They use `r.Context()` and honor its cancellation (spec FR-009, clarified 2026-10-20). | The request's `Authenticator` interface is `Authenticate(r *http.Request)`, and every method is called from an HTTP handler, where the request already carries the one context that matters. This is the idiom of `net/http` itself (`Handler.ServeHTTP`, `Request.Context`). | An explicit `ctx` first parameter (`Authenticate(ctx, r)`, `Issue(ctx, w, r, p)`) duplicates `r.Context()`, lets callers pass a context that differs from the request's, and makes every third-party method implement a two-context signature (FR-017). Rejected by the user in the 2026-10-20 clarification. |

Two further items are allowed by the constitution without a deviation, and are recorded here for
review:

- the Bearer suppression of `crypto/sha1` in `auth/internal/hotp` (R-21);
- the possible `//nolint:contextcheck` on the detached key set download context (R-18, R-21).

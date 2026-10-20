---

description: "Task list for the auth HTTP request authentication module"
---

# Tasks: HTTP Request Authentication (auth)

**Input**: Design documents from `specs/007-http-auth/`

**Prerequisites**: [plan.md](plan.md), [spec.md](spec.md), [research.md](research.md),
[data-model.md](data-model.md), [contracts/](contracts/), [quickstart.md](quickstart.md)

**Tests**: REQUIRED. Constitution Principle III is non-negotiable:

- Tests are written first and observed failing. A compilation failure counts as red.
- Coverage must be ≥ 80%, with `-race -shuffle=on`.
- Every component that reads external input needs a fuzz test (FR-004).
- The performance-sensitive paths (SC-001, SC-009) need benchmarks.
- Every primary API needs an `Example`.

Every test task below precedes the implementation it covers.

**Organization**: tasks are grouped by user story (spec.md US1–US7). Every path is relative to the
repository root. All Go code lives in the module `auth/`.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: can run in parallel with other [P] tasks of the same phase: different files, and no
  dependency on an incomplete task.
- **[Story]**: US1–US7 from spec.md. Setup, Foundational and Polish tasks have no story label.

## Conventions for every task

- **Running Go**: every `go` command runs from `auth/` with `GOWORK=off`, for example
  `cd auth && GOWORK=off go test -race -shuffle=on ./...`.
- **Packages**:
  - behavior tests use external test packages (`auth_test`, `session_test` and so on);
  - tests of unexported code use the package itself, in files named `*_internal_test.go`.
- **Test names**: `Test<Feature>_<Scenario>`. The prefixes are those of
  [quickstart.md](quickstart.md), so its `-run` patterns select them. Fuzz targets and
  benchmarks use the names of quickstart.md. Benchmarks use `b.Loop()`.
- **Test style**:
  - table-driven, with `t.Run`;
  - every behavior that depends on time runs inside `synctest.Test`, where `time.Sleep`
    advances virtual time. No assertion uses the wall clock, and no test sleeps on real time;
  - goroutines are started with `sync.WaitGroup.Go` and joined with `Wait`;
  - HTTP uses `httptest.NewRequest` and `httptest.NewRecorder`, and never opens a socket.
- **argon2id in tests**: the `password` tests and the root integration tests use
  `password.WithParams(password.Params{Memory: 7168, Iterations: 5, Parallelism: 1})`, the floor
  (research R-20). Only examples and `BenchmarkCheck` use the defaults. `basic` tests use a stub
  `basic.Checker`.
- **Fixtures**:
  - keys and tokens are generated at test time;
  - no PEM block, JWT literal or real-looking API key is committed;
  - passwords are obviously fake (`hunter2`, `correct horse`);
  - the RFC vector secrets are the RFCs' ASCII strings.

  If Gitleaks, Bearer or Semgrep flag a fixture, change the fixture; never suppress the finding.
- **Configuration errors** wrap `auth.ErrInvalidConfig`, with messages of the form
  `auth: invalid configuration: <pkg>.<Func>: <setting>: <reason>`, for example
  `auth: invalid configuration: session.New: WithIdleTimeout: must be > 0 and ≤ lifetime`.
- **Other errors** have the form `<pkg>: <operation>: %w`. They never contain a password, code,
  key, token, identifier, digest, hash, username or subject (FR-008, research R-19).
- **Lint**: golangci-lint must report zero findings. The only `//nolint` allowed in library code
  is `//nolint:contextcheck // the key set download must outlive one client's cancellation (FR-063)`,
  in `auth/jwt/keyset.go`, and only if the linter actually reports it. Test helpers take
  `testing.TB` and call `tb.Helper()` first.
- **Documentation**: every exported identifier gets the doc comment of
  [contracts/public-api.md](contracts/public-api.md), expanded as needed. Every package gets a
  package doc comment in the task that creates its first file.
- **Contexts**: methods that receive an `*http.Request` (`Authenticate`, `Issue`, `IssuePending`, `Pending`, `Revoke`) take their context from `r.Context()` and have no `ctx` parameter. This is the deviation recorded in the plan's Complexity Tracking (spec FR-009). Every other I/O function takes `ctx context.Context` first.
- **Methods**: the RFC 8176 values `"pwd"`, `"otp"` and `"mfa"` are unexported constants of each
  package that sets them.

---

## Phase 1: Setup (Shared Infrastructure)

**Purpose**: create the module and register it with the repository tooling.

- [X] T001 Create `auth/go.mod` containing exactly `module github.com/paccolamano/go/auth` and `go 1.26.0`, with no `require` and no `replace`. The two dependencies are added in T038 and T081, by the tasks that first import them.
- [X] T002 [P] Create `auth/CHANGELOG.md` with the header text of `errors/CHANGELOG.md` and an empty `## [Unreleased]` section. The entries are added in T097.
- [X] T003 [P] Create `auth/README.md` with:
  - the title `# auth`;
  - one purpose paragraph: "Authentication of HTTP requests: passwords, sessions, a second factor with TOTP and recovery codes, HTTP Basic, API keys and JWT. The library owns protocol and cryptography; your database stays yours.";
  - `go get github.com/paccolamano/go/auth`;
  - "Minimum Go version: 1.26";
  - the placeholder headings "Usage", "Store", "Passwords", "Sessions and CSRF", "Second factor", "HTTP Basic", "API keys", "JWT", "Middleware order" and "Caveats", which are completed in T096.
- [X] T004 Run `make work` from the repository root, so that `go.work` lists `./auth`. Then confirm that `scripts/structure-check.sh auth` reports PASS (depends on T001–T003).

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: the core types, the internal helpers, the in-memory store and the store test support,
which every story uses.

**⚠️ CRITICAL**: no user story work can begin until this phase is complete.

- [X] T005 Create `auth/doc.go` with the package doc comment for `package auth`: "Package auth authenticates HTTP requests. A small core combines authentication methods and turns their outcome into a response; each method lives in its own sub-package." It is expanded in T095.
- [X] T006 [P] Create `auth/auth.go` and `auth/store.go` with the core declarations of [contracts/public-api.md](contracts/public-api.md) § Package `auth`:
  - in `auth/auth.go`:
    - the six sentinel errors, with exactly the messages `"auth: no credentials"`, `"auth: invalid credentials"`, `"auth: too many attempts"`, `"auth: insufficient authentication methods"`, `"auth: not found"` and `"auth: invalid configuration"`;
    - `Authenticator`, `Challenger` (doc: "cause is ErrNoCredentials, ErrInvalidCredentials or ErrInsufficientMethods; "" means none for that cause");
    - `Principal{Subject, Methods, Extra}`, documented per data-model.md §1: Subject "Non-empty"; Methods "RFC 8176 values, in the order they were added, without duplicates"; Extra "Data specific to the method that produced the principal";
    - `Pending`, whose `Attempt` doc summarizes [contracts/http-semantics.md](contracts/http-semantics.md) §6;
  - in `auth/store.go`: `Store`, with the rules of [contracts/store.md](contracts/store.md) §1 in its doc comment: "`Get` returns `ErrNotFound` when the key is absent or its TTL has elapsed"; "`ttl > 0`"; "`Delete` of an absent key returns nil"; "`Incr` creates the counter at 1 with expiry `now + ttl` and never extends the expiry of an existing counter; N concurrent calls return each value from 1 to N exactly once".

  These are declarations only. US1 tests them.
- [X] T007 [P] Write failing tests in `auth/internal/storekey/storekey_test.go` (`package storekey_test`) for `Key(kind string, parts ...string) string`:
  - the result matches `^auth:<kind>:[A-Za-z0-9_-]{43}$`;
  - it is deterministic;
  - different parts give different keys, including `("a", "bc")` versus `("ab", "c")`, because the parts are joined with a NUL byte before hashing;
  - no part appears as a substring of the key;
  - the digest equals `base64.RawURLEncoding.EncodeToString(sha256(parts joined by "\x00"))`;
  - every kind of [contracts/store.md](contracts/store.md) §2 gives a key of at most 80 bytes, after appending `:` and a 12-digit step.
- [X] T008 Implement `auth/internal/storekey/storekey.go` until T007 passes, with the package doc "Package storekey derives store keys that never contain the secrets they are derived from."
- [X] T009 [P] Write failing tests in `auth/internal/crockford/crockford_test.go` (`package crockford_test`):
  - `Encode(b []byte) string` uses the alphabet `0123456789abcdefghjkmnpqrstvwxyz`, with no padding: 10 bytes give 16 characters and 32 bytes give 52 characters;
  - `Decode(s string) ([]byte, error)` accepts only lowercase alphabet characters, rejects `i`, `l`, `o`, `u`, uppercase letters and any other byte, and round-trips with `Encode` for every length from 0 to 40;
  - `Normalize(s string) string` lowercases, removes spaces and hyphens, and maps `i` and `l` to `1` and `o` to `0`: `"ABCD-EFGH IJKL-MNOP"` becomes `"abcdefgh1jk1mn0p"`.
- [X] T010 Implement `auth/internal/crockford/crockford.go` until T009 passes. The alphabet is a `const`, and the `*base32.Encoding` is built by an unexported function, not stored in a package-level variable (research R-21).
- [X] T011 [P] Write failing tests in `auth/internal/authheader/authheader_test.go` (`package authheader_test`):
  - `Credentials(r *http.Request, scheme string) (token string, present bool)`:
    - `present` is true only when the first `Authorization` value's scheme equals `scheme` without regard to case: `"Bearer x"`, `"bearer x"` and `"BEARER x"` all match `Bearer`;
    - `token` is the rest, with leading spaces trimmed, and may be empty for `"Bearer"`;
    - a missing header or another scheme gives `"", false`;
  - `Quote(v string) string` returns a quoted string with `"` and `\` escaped: `a"b\c` becomes `"a\"b\\c"`;
  - `Scheme(challenge string) string` returns the first token, lowercased: `Bearer error="x"` gives `bearer`.
- [X] T012 Implement `auth/internal/authheader/authheader.go` until T011 passes.
- [X] T013 [P] Write failing tests in `auth/memstore/memstore_test.go` (`package memstore_test`):
  1. `TestStore_Contract`:
     - `Get` of an absent key returns an error matching `auth.ErrNotFound`;
     - `Set` then `Get` returns the value, and a second `Set` replaces it;
     - mutating the slice passed to `Set`, or the one returned by `Get`, does not change the stored value;
     - `Delete` of an absent key returns nil;
     - `Set` and `Incr` with `ttl <= 0` return errors;
     - `Incr` on a key written with `Set` returns an error;
     - every operation with a cancelled context returns an error matching `context.Canceled`.
  2. `TestExpiry_TTL`, in `synctest.Test`:
     - `Set` with a TTL of 1 minute: found after 59 s, `ErrNotFound` after 60 s;
     - `Incr` with a TTL of 10 s returns 1, then 2 after 5 s, then 1 after 5 more seconds, because the expiry is not extended (FR-021).
  3. `TestIncrParallel_Atomic`: 50 goroutines call `Incr` on one new key, and the returned values are exactly {1, …, 50} (FR-021, FR-071).
- [X] T014 [P] Write a failing internal test in `auth/memstore/memstore_internal_test.go` (`package memstore`): `TestSweep_ReclaimsExpired`, in `synctest.Test`. Set 1,000 keys with a TTL of 1 s, sleep 2 minutes, `Set` one more key, and assert that the internal map holds exactly 1 entry ([contracts/store.md](contracts/store.md) §4: "a sweep of the whole map, which runs inside a write when at least one minute has passed since the previous sweep").
- [X] T015 Implement `auth/memstore/memstore.go` until T013 and T014 pass:
  - the package doc "Package memstore is an in-memory auth.Store for tests and single-instance deployments.";
  - `type Store`, `func New() *Store`, and the four methods;
  - one `sync.Mutex`, entries holding either bytes or a counter plus an expiry `time.Time`;
  - lazy removal on access, and the amortized sweep;
  - no goroutine.
- [X] T016 Add `ExampleNew` in `auth/memstore/example_test.go`: `Set`, `Get` and `Incr`, printing the value and the counter (depends on T015).
- [X] T017 [P] Write failing tests in `auth/internal/storetest/storetest_test.go` (`package storetest_test`), over `memstore.New()`:
  - `Recorder{Next auth.Store}` forwards every call and records `Call{Op, Key, Value}` in order, for `Op` values `OpGet`, `OpSet`, `OpDelete` and `OpIncr`;
  - `Calls()` returns a copy, `Count(op)` counts, and `Reset()` clears;
  - `Failing{Next auth.Store, Err error, Ops []Op}` returns `Err` for the listed operations without calling `Next`, and forwards the others;
  - both are safe for concurrent use: 50 goroutines, under `-race`.
- [X] T018 Implement `auth/internal/storetest/storetest.go` until T017 passes, with the package doc "Package storetest wraps an auth.Store to record or fail its calls, for tests."

**Checkpoint**: `cd auth && GOWORK=off go test -race ./internal/... ./memstore/...` passes. The
foundation is ready, and user stories can now begin.

---

## Phase 3: User Story 1 - Protect routes and tell failures apart (Priority: P1) 🎯 MVP

**Goal**: `New`, `Any`, challenges, rejection, `RequireMethods` and `FromContext` behave exactly
as [contracts/http-semantics.md](contracts/http-semantics.md) §1–3 (FR-011 to FR-019).

**Independent Test**: stub methods returning each outcome, alone and combined. Inspect the
status, the `WWW-Authenticate` field lines, the `RejectFunc` arguments and the principal seen by
the handler.

### Tests for User Story 1 ⚠️ (write first, see them fail)

- [X] T019 [P] [US1] Create the shared test helpers in `auth/helpers_test.go` (`package auth_test`):
  - `type stub struct { p *auth.Principal; err error; challenges map[error]string; calls *atomic.Int32 }`, implementing `Authenticate` (it increments `calls` when not nil) and `Challenge` (a lookup in `challenges`);
  - `type plain struct{ stub }`, which embeds `stub` but hides `Challenge`, as a method without challenges;
  - `serve(tb, mw func(http.Handler) http.Handler, r *http.Request) (*httptest.ResponseRecorder, *auth.Principal, bool)`. Its next handler records `auth.FromContext(r.Context())` and writes `200 ok`.
- [X] T020 [P] [US1] Write failing tests in `auth/middleware_test.go`:
  1. `TestNew_Outcomes`, a table over the outcomes of [contracts/http-semantics.md](contracts/http-semantics.md) §1:
     - principal → 200, and `FromContext` returns it;
     - `ErrNoCredentials` → 401;
     - `ErrNoCredentials` with `WithAnonymous()` → 200, and `FromContext` returns `nil, false`;
     - `ErrInvalidCredentials`, with and without `WithAnonymous()` → 401;
     - `ErrTooManyAttempts` → 429 with no `WWW-Authenticate`;
     - an error wrapping both `ErrTooManyAttempts` and `ErrInvalidCredentials` → 429;
     - `errors.New("db down")` → 500 with no `WWW-Authenticate`;
     - `(nil, nil)` → 500;
     - `&Principal{Subject: ""}` → 500;
     - errors wrapped with `fmt.Errorf("x: %w", …)` are classified like the errors they wrap.
  2. `TestNew_RejectFunc`:
     - the function receives `Rejection{Status, Err}`, with `errors.Is(rej.Err, …)` true for the expected sentinel and, for 500, the original error;
     - `WWW-Authenticate` is already set when the function runs;
     - a function that writes nothing produces the status with an empty body.
  3. `TestNew_DefaultResponse`: `Content-Type: text/plain; charset=utf-8`, and the body is `http.StatusText(status)`. The 500 body does not contain `"db down"`.
  4. `TestNew_Config`: each of these returns an error matching `auth.ErrInvalidConfig`, and none panics:
     - `New(nil)`;
     - `New(Any())`;
     - `New(Any(nil))`;
     - `New(Any(stub, Any()))`;
     - `New(stub, WithRejectFunc(nil))`;
     - a nil `Option`.
  5. `TestFromContext_NoState`: `auth.FromContext(context.Background())` returns `nil, false`.
- [X] T021 [P] [US1] Write failing tests in `auth/any_test.go`:
  - `TestAny_Order` (US1 scenario 2): A returns none and B a principal, so the result is B's principal and C has 0 calls;
  - `TestAny_InvalidStops` (scenario 3): B returns invalid, so the response is 401 and C has 0 calls;
  - `TestAny_FailureStops` (scenario 4): B returns an error, so the response is 500 and C has 0 calls;
  - `TestAny_AllNone`: every method returns none, so `Any.Authenticate` returns `ErrNoCredentials`;
  - `TestAny_Nested`: `Any(A, Any(B, C))` behaves exactly like `Any(A, B, C)`, including for the challenges;
  - `TestAny_Standalone`: `Any(...).Authenticate(r)` works without `New`.
- [X] T022 [P] [US1] Write failing tests in `auth/challenge_test.go`, checking `rec.Result().Header.Values("WWW-Authenticate")`, one field line per challenge, against [contracts/http-semantics.md](contracts/http-semantics.md) §2:
  - `TestChallenges_Plain` (US1 scenario 5): `Basic realm="api", charset="UTF-8"` and `Bearer`, in leaf order;
  - `TestChallenges_Dedup`: two leaves returning `Bearer` give one `Bearer`;
  - `TestChallenges_InvalidReplacesScheme`: the second example of §2 gives exactly `Cookie cookie-name="__Host-session"` and `Bearer error="invalid_token"`. A leaf without `Challenger` contributes nothing;
  - `TestChallenges_None`: when no leaf has a challenge, the header is absent on 401;
  - `TestChallenges_NotOnOtherStatuses`: no `WWW-Authenticate` on 429 and 500.
- [X] T023 [P] [US1] Write failing tests in `auth/require_test.go` for the table of [contracts/http-semantics.md](contracts/http-semantics.md) §3:
  - `TestRequireMethods_NoState`: used without `New`, it answers 500;
  - `TestRequireMethods_Anonymous`: with `WithAnonymous()` and no credentials, it answers 401 with the plain challenges through the configured `RejectFunc`, and `Err` matches `ErrNoCredentials`;
  - `TestRequireMethods_Satisfied`: methods `["pwd","otp","mfa"]` with `RequireMethods("mfa")` and with `RequireMethods("pwd","mfa")` give 200. An empty list with any principal gives 200;
  - `TestRequireMethods_StepUp`: the deciding leaf returns `Bearer error="insufficient_user_authentication"` for `ErrInsufficientMethods`, so the response is 401 with exactly that one challenge, and `Err` matches `ErrInsufficientMethods`. The deciding leaf is found inside a nested `Any`;
  - `TestRequireMethods_Forbidden`: the deciding leaf has no step-up challenge, so the response is 403 with no `WWW-Authenticate` (US1 scenario 8);
  - `TestRequireMethods_EmptyValue`: `RequireMethods("")` rejects every principal;
  - `TestRequireMethods_CopiesList`: mutating the slice passed with `RequireMethods(list...)` after construction changes nothing.
- [X] T024 [P] [US1] Write a failing test in `auth/extension_test.go`: `TestThirdPartyMethod` (SC-013, FR-017).
  - Define in the test a `headerToken` method that reads `X-Test-Token`. It returns none when the header is absent, invalid for any value other than `"good"`, and `&auth.Principal{Subject: "svc"}` for `"good"`. Its `Challenge` returns `TestToken` for none and `TestToken error="bad"` for invalid.
  - Combine it with `Any(headerToken{}, stub)`. Assert success, the summed challenges, and the replacement on invalid, with no code outside the test.

### Implementation for User Story 1

- [X] T025 [US1] Implement `auth/any.go`:
  - `Any`, returning an unexported `*anyAuth` that implements `Authenticate`;
  - an unexported `leaves(a Authenticator) []Authenticator`, which flattens nested `*anyAuth`;
  - an unexported `decide(a, r) (*Principal, Authenticator, error)`, which returns the deciding leaf;
  - a `validate(a) error` that reports nil methods and empty `Any`s at any depth (research R-04).
- [X] T026 [US1] Implement `auth/middleware.go`:
  - `Option`, `WithAnonymous`, `WithRejectFunc`, `Rejection` and `RejectFunc`;
  - `New`: it validates, precomputes the plain challenges, and classifies with `errors.Is` in the order none, too many attempts, invalid, other;
  - the unexported context state `{principal, decider, challenges, reject}`, and `FromContext`.
- [X] T027 [US1] Implement `auth/reject.go`:
  - `plainChallenges(leaves)`: the non-empty values, deduplicated, in order;
  - `invalidChallenges(leaves, rejecting)`, with the same-scheme rule, using `authheader.Scheme`;
  - `writeRejection(w, r, fn, rej, challenges)`, which sets one `WWW-Authenticate` line per challenge, calls `fn` or the default, and sends the status when `fn` wrote nothing. Detecting writes may follow `middleware`'s rejection writer pattern.

  T020–T022 pass.
- [X] T028 [US1] Implement `auth/require.go` with `RequireMethods` until T023 passes, then confirm that T024 passes.
- [X] T029 [US1] Add `ExampleAny` and `ExampleFromContext` in `auth/example_test.go`, using a toy method defined in the example. The full-flow `ExampleNew` and `ExampleRequireMethods` are added in T094.

**Checkpoint**: `cd auth && GOWORK=off go test -race -run 'TestNew|TestAny|TestChallenges|TestRequireMethods|TestThirdPartyMethod|TestFromContext|Example' .` passes.

---

## Phase 4: User Story 2 - Hash and check passwords safely (Priority: P1)

**Goal**: argon2id hashing, bcrypt verification, the dummy hash, rehashing, length limits and the
optional attempt limit (FR-024 to FR-031).

**Independent Test**: a map-backed lookup holding current, outdated and bcrypt hashes, and the
memstore for the attempt limit.

### Tests for User Story 2 ⚠️ (write first, see them fail)

- [X] T030 [P] [US2] Write failing internal tests in `auth/password/phc_internal_test.go` (`package password`):
  1. `TestPHC_RoundTrip`: `encodePHC` produces exactly `$argon2id$v=19$m=<m>,t=<t>,p=<p>$<salt>$<key>` with unpadded standard base64, and `parsePHC` returns the same parameters, salt and key.
  2. `TestPHC_Bounds`: the "Accepted when stored" column of data-model.md §4 is enforced verbatim:
     - memory "1 to 2,097,152";
     - iterations "1 to 100";
     - parallelism "1 to 64";
     - salt "8 to 64 bytes";
     - key "16 to 64 bytes";
     - version "19 only".

     Each bound is tested at, just inside and just outside.
  3. `TestPHC_Malformed`: wrong segment count, padded base64, bad base64, non-numeric parameters, duplicated or missing parameters, `$argon2i$` and `$argon2d$` all produce an error.
  4. `TestBcryptPrefix`: `$2a$`, `$2b$` and `$2y$` are detected as bcrypt; `$2x$` and `$1$` are unknown.
  5. `TestParams_Floor`: the "Floor when configured" column, "memory ≥ 7,168, and m × t ≥ 35,840", parallelism "1 to 64". `{7168,5,1}` and `{19456,2,1}` pass. `{7168,4,1}`, `{7167,10,1}`, `{19456,2,0}` and `{19456,2,65}` fail.
  6. `TestOutdated`: a hash is outdated when its algorithm is bcrypt, or when any parameter, the salt length or the key length differs from the current values.
  7. `FuzzPHC`: `parsePHC` never panics, and every input it accepts re-encodes to itself.
- [X] T031 [P] [US2] Write failing tests in `auth/password/hash_test.go`:
  - `TestHash_Format`: `password.Hash("hunter2")` starts with `$argon2id$v=19$m=19456,t=2,p=1$`, and its salt and key decode to 16 and 32 bytes;
  - `TestHash_Random`: two hashes of the same password differ, and both verify through a checker (US2 scenario 1);
  - `TestHash_Errors`: `""` gives `ErrEmpty`; 1,025 bytes give `ErrTooLong`; 1,024 bytes succeed;
  - `TestHash_CheckerParams`: `(*Checker).Hash` with the floor parameters starts with `$argon2id$v=19$m=7168,t=5,p=1$`.
- [X] T032 [P] [US2] Write failing tests in `auth/password/checker_test.go`, with the floor parameters and a map lookup that records its context:
  - `TestCheck_Success` (US2 scenario 2): the subject, `Methods` `["pwd"]`, and the request context reaches the lookup;
  - `TestCheck_Wrong` (scenario 3) and `TestCheck_Unknown` (scenario 4): both give errors matching `auth.ErrInvalidCredentials`;
  - `TestCheck_LookupError` (scenario 5): the error matches the lookup's error, and does not match `ErrInvalidCredentials`;
  - `TestCheck_EmptySubject`: a lookup returning an `Account` with an empty `Subject` gives an error that is not invalid;
  - `TestCheck_Malformed` (scenario 9): a malformed stored hash, an unknown algorithm or out-of-bound parameters give an error that is not invalid;
  - `TestLegacyHashes_Bcrypt`: a hash from `bcrypt.GenerateFromPassword` at `bcrypt.MinCost` verifies (scenario 6). A 73-byte password whose first 72 bytes are correct is invalid (scenario 8);
  - `TestRehash_Called`: for a bcrypt hash and for an argon2id hash at `m=7168,t=6,p=1`, the callback receives the subject and a hash with the current parameters that verifies;
  - `TestRehash_NotCalled`: not after a wrong password, nor for a current hash;
  - `TestLength_Limits`: with `WithMaxLength(256)`, a 257-byte password is invalid, and a 256-byte password of 64 four-byte characters is accepted and the lookup is not called (scenario 7). An empty password or username is invalid;
  - `TestCheck_Config`: each of these gives an error matching `auth.ErrInvalidConfig`:
    - `NewChecker(nil)`;
    - `WithRehash(nil)`;
    - `WithMaxLength` outside "256 to 4,096 bytes" (255 and 4,097 fail);
    - `WithParams` below the floor;
    - a nil `Option`.
- [X] T033 [P] [US2] Write failing tests in `auth/password/limit_test.go`, with `memstore` wrapped in `storetest.Recorder` or `storetest.Failing`, inside `synctest.Test`:
  - `TestAttemptLimit_Blocks` (US2 scenario 10): `Max: 10`. After 10 wrong checks, the eleventh gives an error matching `auth.ErrTooManyAttempts`, even with the correct password. The same holds for an unknown username. After `Window` (15 min) elapses, checks are verified again;
  - `TestAttemptLimit_Clears` (scenario 11): 3 failures, then a success; then 10 more failures are allowed before blocking;
  - `TestAttemptLimit_StoreError` (scenario 12): a failing `Incr` gives an error matching neither `ErrInvalidCredentials` nor `ErrTooManyAttempts`;
  - `TestAttemptLimit_LookupFirst`: a failing lookup produces 0 `OpIncr` calls (FR-030);
  - `TestAttemptLimit_Defaults`: `AttemptLimit{Store: s}` blocks at the eleventh failure and resets after 15 minutes;
  - `TestAttemptLimit_Config`: a nil `Store` or a negative `Max` or `Window` gives an error matching `auth.ErrInvalidConfig`;
  - `TestAttemptLimit_Keys`: no recorded key contains the username;
  - `TestAttemptLimitParallel` (scenario 13, SC-004): 50 goroutines check one username with wrong passwords. At most 10 results match `ErrInvalidCredentials`, and the others match `ErrTooManyAttempts`.
- [X] T034 [P] [US2] Write a failing internal test in `auth/password/dummy_internal_test.go`: `TestDummyHashParams` (SC-001). The checker's dummy hash parses to exactly the checker's parameters, salt length and key length, both with the defaults and with `WithParams`.
- [X] T035 [P] [US2] Write `auth/password/bench_test.go`: `BenchmarkCheck/{existing-user,missing-user,wrong-password}` with the default parameters and a map lookup.

### Implementation for User Story 2

- [X] T036 [US2] Implement `auth/password/params.go`:
  - the package doc "Package password hashes passwords with argon2id and checks them, also against legacy bcrypt hashes.";
  - `Params` and `DefaultParams()`, returning `{Memory: 19456, Iterations: 2, Parallelism: 1}`;
  - the unexported floor and stored-bound checks of data-model.md §4;
  - the constants for a 16-byte salt and a 32-byte key.
- [X] T037 [US2] Implement `auth/password/phc.go` (`encodePHC`, `parsePHC`, bcrypt detection, `outdated`) until T030 passes.
- [X] T038 [US2] Implement `auth/password/hash.go`:
  - `ErrNotFound = auth.ErrNotFound`, `ErrEmpty` and `ErrTooLong`;
  - `Hash`, with the default maximum of 1,024 bytes.

  Add the dependency with `GOWORK=off go get golang.org/x/crypto@v0.57.0`. T031 passes, except `TestHash_CheckerParams`.
- [X] T039 [US2] Implement `auth/password/options.go`:
  - `Option`, `WithParams`, `WithMaxLength`, `WithRehash` and `WithAttemptLimit`;
  - `Account`, `LookupFunc`, `RehashFunc` and `AttemptLimit`;
  - the validation of data-model.md §10: "Maximum password length … 256 to 4,096 bytes"; "Attempt limit … `Store` not nil; `Max` ≥ 0 and `Window` ≥ 0, where 0 selects the default (10 per 15 min)".
- [X] T040 [US2] Implement `auth/password/checker.go`: `Checker`, `NewChecker`, which computes the dummy hash at construction, `Check`, following the steps of [contracts/http-semantics.md](contracts/http-semantics.md) §8 (the derived argon2id key is compared with `subtle.ConstantTimeCompare`, FR-007), and `(*Checker).Hash`. T031–T034 pass.
- [X] T041 [US2] Add `ExampleHash`, `ExampleChecker`, `ExampleWithAttemptLimit` and `ExampleWithRehash` in `auth/password/example_test.go`, with the default parameters.

**Checkpoint**: `cd auth && GOWORK=off go test -race ./password/...` passes.

---

## Phase 5: User Story 3 - Keep a signed-in user in a session (Priority: P1)

**Goal**: `session.Manager` with `Issue`, `Authenticate`, renewal and `Revoke`, using hardened
cookies and hashed identifiers (FR-032 to FR-037 and FR-041).

**Independent Test**: memstore wrapped in `storetest.Recorder`, `synctest`, and
`httptest` requests carrying the issued cookies.

### Tests for User Story 3 ⚠️ (write first, see them fail)

- [X] T042 [P] [US3] Write failing internal tests in `auth/session/record_internal_test.go` (`package session`):
  - session records encode to JSON with exactly the fields `v`, `sub`, `amr`, `iat`, `exp` and `rw`, and pending records with `v`, `sub`, `amr` and `iat` ([contracts/store.md](contracts/store.md) §3);
  - both decode back unchanged;
  - `"v":2`, invalid JSON and an empty value give errors.
- [X] T043 [P] [US3] Write failing tests in `auth/session/session_test.go`, inside `synctest.Test`, with `memstore` wrapped in `storetest.Recorder`:
  - `TestCookie_Attributes` (US3 scenario 1): `Name` `__Host-session`, `Path` `/`, `MaxAge` 28800, `Secure`, `HttpOnly`, `SameSite` `http.SameSiteLaxMode`, and `Domain` empty. The value matches `^[A-Za-z0-9_-]{43}$`. `WithCookieName("app")` gives `__Host-app`;
  - `TestIssue_StoreHasNoID` (scenario 2): no recorded key or value contains the cookie value;
  - `TestIssue_Fixation` (scenario 3, SC-005): issuing again on a request carrying session S sets a new value, and S then returns `ErrNoCredentials`;
  - `TestIssue_Errors`:
    - a nil principal or an empty subject gives an error and no `Set-Cookie`;
    - a failing `OpDelete` or `OpSet` gives an error and no `Set-Cookie`;
  - `TestAuthenticate_Principal` (scenario 1): the subject and methods recorded at issuance. Exactly 1 `OpGet` per request (SC-006);
  - `TestAuthenticate_NoCredentials` (scenario 9): a missing cookie, a malformed value with 0 store calls, and an unknown value all give errors matching `auth.ErrNoCredentials`. With `auth.New(sessions, auth.WithAnonymous())`, a request with a stale cookie reaches the handler;
  - `TestAuthenticate_StoreError` (scenario 8): a failing `OpGet`, and an undecodable record, give errors that are not `ErrNoCredentials`. Through `auth.New`, the response is 500. A request whose context is already cancelled gives an error matching `context.Canceled`: the context comes from `r.Context()` (FR-009);
  - `TestExpiry_Idle` and `TestExpiry_Absolute` (scenario 5): with the defaults, idle 30 min and lifetime 8 h:
    - no request for 30 min gives none;
    - one request every 5 min keeps the session valid until 8 h, then none;
  - `TestRenewal_Writes` (scenario 6, SC-006):
    - requests every second for 7 min 29 s cause 0 `OpSet`, and the request at 7 min 30 s causes exactly 1;
    - one request per second for 30 min causes at most 4 `OpSet`;
    - a failing renewal `OpSet` gives an error;
  - `TestRevoke_Session` (scenario 4): the record is deleted, both cookies are cleared with `MaxAge < 0`, the old cookie gives none, and revoking a request without cookies returns nil and still clears them;
  - `TestManager_Challenge`: `Cookie cookie-name="__Host-session"` for `ErrNoCredentials` and `ErrInvalidCredentials`, and `""` for `ErrInsufficientMethods`;
  - `TestManager_Config`: each rule of data-model.md §10 gives an error matching `auth.ErrInvalidConfig`:
    - lifetime "> 0";
    - idle timeout "> 0 and ≤ lifetime";
    - pending lifetime "> 0 and ≤ 1 h";
    - cookie base name "HTTP token, at most 64 bytes";
    - a nil store;
    - a nil `Option`.
- [X] T044 [P] [US3] Write `FuzzSessionCookie` in `auth/session/fuzz_test.go`: an arbitrary cookie value never panics `Authenticate`. Only the values of issued sessions authenticate. Values that do not match the identifier shape cause 0 store calls.
- [X] T045 [P] [US3] Write `BenchmarkSessionAuthenticate` in `auth/session/bench_test.go`, over `memstore`, with no renewal due.

### Implementation for User Story 3

- [X] T046 [US3] Implement `auth/session/record.go`, with versioned JSON records and times in Unix seconds, until T042 passes.
- [X] T047 [US3] Implement `auth/session/session.go`:
  - the package doc "Package session keeps signed-in users in server-side sessions carried by a __Host- cookie.";
  - `Option`, `WithCookieName`, `WithLifetime`, `WithIdleTimeout`, `WithPendingLifetime`, `Manager` and `New`, with the defaults 8 h, 30 min, 5 min and `session`;
  - `Authenticate`, with renewal at `idle/4` and the TTL `min(now + idle, exp) − now`, following research R-13;
  - `Challenge`;
  - keys from `storekey.Key("session", id)` and `storekey.Key("pending", id)`.
- [X] T048 [US3] Implement `auth/session/issue.go`:
  - `Issue`, which deletes the request's session and pending login, writes the new record, sets the session cookie and clears the pending cookie;
  - `Revoke`;
  - the cookie helpers, with the attributes of [contracts/http-semantics.md](contracts/http-semantics.md) §6;
  - identifiers from 32 `crypto/rand` bytes in `base64.RawURLEncoding`.

  T043–T045 pass.
- [X] T049 [US3] Add `ExampleManager` in `auth/session/example_test.go`: issue, authenticate and revoke with `httptest` and `memstore`.

**Checkpoint**: `cd auth && GOWORK=off go test -race ./session/...` passes. With US1 and US2,
a password login with sessions works end to end.

---

## Phase 6: User Story 4 - Require a second factor (Priority: P2)

**Goal**: pending logins with the shared 5-attempt budget, TOTP with replay protection and
enrollment, single-use recovery codes, and `RequireMethods("mfa")` on sessions (FR-038 to FR-040
and FR-042 to FR-050).

**Independent Test**: memstore, `synctest`, secrets and hashes in maps, and codes computed with
`internal/hotp`.

### Tests for User Story 4 ⚠️ (write first, see them fail)

- [X] T050 [P] [US4] Write failing tests in `auth/session/pending_test.go`, inside `synctest.Test`:
  - `TestPending_Cookie`: `IssuePending` sets `__Host-session-pending` with `MaxAge` 300 and the attributes of the session cookie;
  - `TestPending_NotAuthenticated` (US4 scenario 1): a request with only the pending cookie gives `ErrNoCredentials` from `Authenticate`;
  - `TestPending_Read`: `Pending(r).Principal()` has the subject and `["pwd"]`;
  - `TestPending_Expired` (scenario 6): after 5 min, `Pending(r)` gives `ErrNoCredentials`;
  - `TestPending_Replaced`: a second `IssuePending` deletes the first;
  - `TestPending_DeletedByIssueAndRevoke`: `Issue` and `Revoke` both delete the pending login and clear its cookie (FR-040);
  - `TestAttempt_Budget`, following [contracts/http-semantics.md](contracts/http-semantics.md) §6:
    - 4 failing verifications, then a succeeding one, give `nil`;
    - 5 failing verifications: the fifth returns an error matching both `auth.ErrTooManyAttempts` and `auth.ErrInvalidCredentials`, and `Pending(r)` then gives `ErrNoCredentials` (scenario 5);
    - a sixth call returns `ErrTooManyAttempts` without calling `verify`;
  - `TestAttempt_StoreError`: a failing `OpIncr` returns the error without calling `verify`, and 5 attempts remain afterwards;
  - `TestAttempt_OtherError`: a `verify` returning `errors.New("x")` returns it, and the attempt stays counted;
  - `TestAttempt_Parallel` (SC-004): 50 goroutines call `Attempt` with a failing `verify`, which is called at most 5 times.
- [X] T051 [P] [US4] Write failing tests in `auth/internal/hotp/hotp_test.go` (`package hotp_test`) for `Value(alg Algorithm, secret []byte, counter uint64, digits int) string`:
  1. `TestRFC4226Vectors`: `SHA1`, secret `"12345678901234567890"`, 6 digits. Counters 0 to 9 give `755224 287082 359152 969429 338314 254676 287922 162583 399871 520489`.
  2. `TestRFC6238Vectors`: 8 digits, counter `t / 30`. The secrets are `"12345678901234567890"` (SHA1), `"12345678901234567890123456789012"` (SHA256) and `"1234567890123456789012345678901234567890123456789012345678901234"` (SHA512). Times and values (SHA1, SHA256, SHA512):
     - 59 → 94287082, 46119246, 90693936;
     - 1111111109 → 07081804, 68084774, 25091201;
     - 1111111111 → 14050471, 67062674, 99943326;
     - 1234567890 → 89005924, 91819424, 93441116;
     - 2000000000 → 69279037, 90698825, 38618901;
     - 20000000000 → 65353130, 77737706, 47863826.
  3. Leading zeros are kept: `07081804` has 8 characters.
- [X] T052 [P] [US4] Write failing tests in `auth/totp/totp_test.go`, inside `synctest.Test`. They use a real `session.Manager` over `memstore` for pending logins, and `hotp.Value(hotp.SHA1, secret, uint64(time.Now().Unix()/30)+k, 6)` for codes:
  - `TestVerify_Success` (scenario 2): methods `["pwd","otp","mfa"]`;
  - `TestVerify_Window` (scenario 3): the steps −1, 0 and +1 are accepted, and −2 and +2 are rejected;
  - `TestVerify_Replay` (scenario 4): the same code a second time gives invalid, sequentially, in parallel, and from another pending login of the same subject;
  - `TestVerify_Malformed`: `"12345"`, `"1234567"`, `"12a456"`, `" 123456"` and `"１２３４５６"` (full-width digits) are invalid and counted;
  - `TestVerify_NotEnrolled`: a lookup returning `totp.ErrNotFound` gives an error matching `auth.ErrNotFound`, not counted (scenario 10);
  - `TestVerify_LookupError` and `TestVerify_StoreError`: not invalid, and not counted when the failure happens before counting (scenario 10, FR-045);
  - `TestEnrolled_States` (scenario 10): true, false and an error, as in [contracts/http-semantics.md](contracts/http-semantics.md) §7;
  - `TestConfirm_FirstCode` (scenario 9): the correct code returns nil, and a wrong code gives invalid. The confirmed code is then rejected by `Verify` as a replay. A secret under 16 bytes gives an error;
  - `TestSecondFactorParallel` (SC-004): 50 goroutines verify the same valid code against 50 pending logins of one subject, and exactly 1 succeeds;
  - `TestTOTP_Config`: `New(nil, s)` and `New(lookup, nil)` give errors matching `auth.ErrInvalidConfig`.
- [X] T053 [P] [US4] Write failing tests in `auth/totp/enrollment_test.go`:
  - `TestEnrollment_Secret`: `NewEnrollment("Acme", "alice@example.com")` gives a 20-byte `Secret()`;
  - `TestEnrollment_URI`: parsed with `url.Parse`, scheme `otpauth`, host `totp`, label `Acme:alice@example.com`, and the query parameters `secret` (unpadded uppercase base32 of `Secret()`), `issuer=Acme`, `algorithm=SHA1`, `digits=6` and `period=30`. `"Acme Corp"` is escaped in the label;
  - `TestEnrollment_Invalid`: an empty issuer or account, or one containing `:`, gives an error;
  - `TestEnrollment_Redacted`: `slog.New(slog.NewJSONHandler(&buf, nil)).Info("e", "e", enrollment)` and `fmt.Sprint(enrollment)` contain `[REDACTED]`, and neither contains the base32 secret.
- [X] T054 [P] [US4] Write `FuzzTOTPCode` in `auth/totp/fuzz_test.go`: `Verify` with an arbitrary code never panics, and succeeds only when the code equals one of the three valid codes.
- [X] T055 [P] [US4] Write failing tests in `auth/recovery/codes_test.go`:
  - `TestGenerate_Count`: `Generate(0)` and `Generate(101)` give errors ("`n` in 1 to 100"), `Generate(DefaultCount)` gives 10 codes, and `DefaultCount == 10`;
  - `TestGenerate_Format`: each code matches `^[0-9a-hjkmnp-tv-z]{4}(-[0-9a-hjkmnp-tv-z]{4}){3}$`, and the codes are unique;
  - `TestGenerate_Hashes`: `Hashes()[i]` is the lowercase hex SHA-256 of the code without hyphens;
  - `TestCodes_Redacted`: as in T053, for `slog` and `fmt`.
- [X] T056 [P] [US4] Write failing tests in `auth/recovery/recovery_test.go`, inside `synctest.Test`, with a real `session.Manager`:
  - `TestVerify_Success` (scenario 7): methods `["pwd","mfa"]`, and `consume` is called once with the subject and the code's hash;
  - `TestVerify_Normalization`: the uppercase code, the code with spaces, the code without hyphens, and the code with `o`, `i` and `l` substituted for `0` and `1` all succeed;
  - `TestVerify_Malformed`: the wrong length and characters outside the alphabet are invalid and counted;
  - `TestVerify_Consumed`: `consume` returning `recovery.ErrNotFound` gives invalid, and `consume` returning `errors.New("db")` gives an error that is not invalid;
  - `TestVerify_LookupError`: not counted;
  - `TestSecondFactorParallel` (SC-004): 50 goroutines submit the same code with a deliberately non-atomic `consume` fake, and exactly 1 succeeds;
  - `TestRecovery_Config`: a nil lookup, a nil `consume` or a nil store gives an error matching `auth.ErrInvalidConfig`.
- [X] T057 [P] [US4] Write `FuzzRecoveryCode` in `auth/recovery/fuzz_test.go`: arbitrary input never panics, and succeeds only for a stored code under some normalization.
- [X] T058 [P] [US4] Write a failing test in `auth/integration_test.go` (`package auth_test`): `TestLoginFlow`, inside `synctest.Test`. It uses `memstore`, `password.NewChecker` with the floor parameters, `session`, `totp`, `recovery`, `auth.New(sessions)` and `auth.RequireMethods("mfa")`, and covers:
  - an enrolled user: password, then `IssuePending`, then TOTP `Verify`, then `Issue`. `/admin` gives 200;
  - a password-only session on `/admin` gives 403, with `Rejection.Err` matching `auth.ErrInsufficientMethods` (US4 scenario 11);
  - shared budget (scenario 8): 3 wrong TOTP codes and 2 wrong recovery codes cancel the pending login;
  - the methods: `["pwd","otp","mfa"]` after TOTP and `["pwd","mfa"]` after a recovery code.

### Implementation for User Story 4

- [X] T059 [US4] Implement `auth/internal/hotp/hotp.go`:
  - the package doc "Package hotp computes HOTP values (RFC 4226).";
  - `type Algorithm int`, with the constants `SHA1`, `SHA256` and `SHA512`;
  - `Value`, on `crypto/hmac` with dynamic truncation.

  This file is the module's only `crypto/sha1` import (research R-21). T051 passes.
- [X] T060 [US4] Implement `auth/session/pending.go`:
  - `IssuePending`;
  - `Pending(r)`;
  - the unexported pending type implementing `auth.Pending`. `Attempt` follows [contracts/http-semantics.md](contracts/http-semantics.md) §6, with the counter key `storekey.Key("pending-attempts", id)` and the budget constant 5.

  T050 passes.
- [X] T061 [US4] Implement `auth/totp/enrollment.go`:
  - the package doc "Package totp verifies time-based one-time codes (RFC 6238) as a second factor.";
  - `Enrollment`, `NewEnrollment`, `Secret`, `URI`, `LogValue` and `String`, following data-model.md §7.

  T053 passes.
- [X] T062 [US4] Implement `auth/totp/totp.go`:
  - `ErrNotFound = auth.ErrNotFound`, `LookupFunc`, `Verifier` and `New`;
  - `Enrolled`, `Verify` and `Confirm`, following [contracts/http-semantics.md](contracts/http-semantics.md) §7. The replay key is `storekey.Key("totp-used", subject)` + `":" + step`, with a TTL of 2 min.

  T052 and T054 pass.
- [X] T063 [US4] Implement `auth/recovery/codes.go`:
  - the package doc "Package recovery generates and verifies single-use recovery codes as a second factor.";
  - `DefaultCount`, `Codes`, `Generate`, `Plain`, `Hashes`, `LogValue` and `String`;
  - normalization and hashing with `internal/crockford`.

  T055 passes.
- [X] T064 [US4] Implement `auth/recovery/recovery.go`:
  - `ErrNotFound = auth.ErrNotFound`, `LookupFunc`, `ConsumeFunc`, `Verifier`, `New` and `Verify`;
  - the guard key `storekey.Key("recovery-used", subject, hash)`, with a TTL of 10 min.

  T056–T058 pass.
- [X] T065 [P] [US4] Add `ExampleNewEnrollment`, `ExampleVerifier_Confirm`, `ExampleVerifier_Enrolled` and `ExampleVerifier_Verify` in `auth/totp/example_test.go`, and `ExampleGenerate` and `ExampleVerifier_Verify` in `auth/recovery/example_test.go` (FR-010).
- [X] T066 [US4] Add `ExampleManager_Pending` in `auth/session/example_test.go`: the pending login, a second factor, then `Issue` (after T049).

**Checkpoint**: `cd auth && GOWORK=off go test -race ./session/... ./totp/... ./recovery/... ./internal/hotp/... && GOWORK=off go test -race -run TestLoginFlow .` passes.

---

## Phase 7: User Story 5 - Authenticate API clients with API keys (Priority: P2)

**Goal**: `apikey.Keys` generates `<prefix>_<id>_<secret>` keys and authenticates them against
SHA-256 hashes (FR-055 to FR-058).

**Independent Test**: generated keys stored in a map lookup, sent as Bearer credentials and in a
custom header.

### Tests for User Story 5 ⚠️ (write first, see them fail)

- [X] T067 [P] [US5] Write failing tests in `auth/apikey/apikey_test.go`:
  - `TestGenerate_Format` (US5 scenario 1): the key matches `^acme_[0-9a-hjkmnp-tv-z]{16}_[0-9a-hjkmnp-tv-z]{52}$`, `Stored.ID` is the middle part, and `Stored.Hash` is the 32-byte SHA-256 of the secret part decoded with `internal/crockford`. The key does not appear in `Stored`;
  - `TestGenerate_Redacted`: `slog` output of the `Key` contains `[REDACTED]` and not the key, and `fmt.Sprint(key)` prints the key;
  - `TestAuthenticate_Success` (scenario 2): the subject is the owner, the methods are empty, the lookup receives the ID and the request context, and `bearer` works in any case;
  - `TestAuthenticate_Invalid` (scenario 3): a wrong secret, an unknown ID, an `ExpiresAt` equal to now, and an `ExpiresAt` in the past all give invalid. A zero `ExpiresAt` never expires;
  - `TestAuthenticate_NotMine` (scenario 4): no header, `Bearer x.y.z`, and `Bearer other_…` all give `ErrNoCredentials`;
  - `TestAuthenticate_Malformed` (scenario 5): `acme_`, `acme_x_y`, a wrong part length and an uppercase character all give invalid;
  - `TestAuthenticate_LookupError` (scenario 7): through `auth.New`, the response is 500. An empty `Subject` also gives 500;
  - `TestHeader_Custom` (scenario 6): `WithHeader("X-API-Key")` reads that header and ignores `Authorization`. The challenge is `ApiKey header="X-API-Key"`;
  - `TestKeys_Challenge`: `Bearer` for none, `Bearer error="invalid_token"` for invalid, and `""` for `ErrInsufficientMethods`;
  - `TestKeys_RequireMFA` (scenario 8): `auth.New(keys)` plus `RequireMethods("mfa")` gives 403;
  - `TestKeys_Config`: each of these gives an error matching `auth.ErrInvalidConfig`:
    - a prefix outside "1–32 ASCII letters and digits, starting with a letter" (`""`, `"1acme"`, `"ac_me"`, 33 characters);
    - a nil lookup;
    - `WithHeader` that is not "valid header name, not `Authorization`".
- [X] T068 [P] [US5] Write `FuzzAPIKey` in `auth/apikey/fuzz_test.go`: an arbitrary `Authorization` value never panics, and only generated keys authenticate.
- [X] T069 [P] [US5] Write `BenchmarkAPIKeyAuthenticate` in `auth/apikey/bench_test.go`, with a map lookup.

### Implementation for User Story 5

- [X] T070 [US5] Implement `auth/apikey/key.go`:
  - the package doc "Package apikey generates API keys and authenticates requests that carry them, storing only a hash of the secret.";
  - `ErrNotFound = auth.ErrNotFound`, `Key` with `LogValue`, `Stored`, `Account` and `LookupFunc`;
  - the unexported generation (10 + 32 random bytes) and the parser.
- [X] T071 [US5] Implement `auth/apikey/apikey.go`: `Option`, `WithHeader`, `Keys`, `New`, `Generate`, `Authenticate` (following [contracts/http-semantics.md](contracts/http-semantics.md) §5) and `Challenge`. T067–T069 pass.
- [X] T072 [US5] Add `ExampleKeys_Generate` and `ExampleNew` in `auth/apikey/example_test.go`.

**Checkpoint**: `cd auth && GOWORK=off go test -race ./apikey/...` passes.

---

## Phase 8: User Story 6 - Accept access tokens from an identity provider (Priority: P2)

**Goal**: `jwt.Verifier` verifies compact JWS tokens with keys bound to algorithms, from static
keys or a lazily downloaded and rate-limited key set (FR-059 to FR-069).

**Independent Test**: tokens signed at test time, and key sets served by an in-memory
`RoundTripper`, inside `synctest`.

### Tests for User Story 6 ⚠️ (write first, see them fail)

- [X] T073 [P] [US6] Create the test helpers in `auth/jwt/helpers_test.go` (`package jwt_test`), importing go-jose as `jose` and `josejwt`:
  - `testKeys()`, through `sync.OnceValue`: RSA 2048, ECDSA P-256 and Ed25519 key pairs, plus a 32-byte HS256 secret;
  - `sign(tb, alg, key, kid string, headers map[jose.HeaderKey]any, claims map[string]any) string`;
  - `validClaims()`: `iss` `https://id.example.test`, `aud` `orders-api`, `sub` `u1`, `exp` now + 1 h;
  - `type fakeIdP`, an `http.RoundTripper` serving a JWKS document with a configurable status, body, `Cache-Control` and transport error. It counts requests (`atomic.Int32`) and records the `Accept` header;
  - `newVerifier(tb, idp, opts...)`, with `WithJWKS("https://id.example.test/jwks")` and `WithHTTPClient(&http.Client{Transport: idp})`.
- [X] T074 [P] [US6] Write failing tests in `auth/jwt/verify_test.go`:
  - `TestVerify_Success` (US6 scenario 1): static keys for RS256, PS256, ES256, EdDSA and HS256, and the JWKS; `Subject` = `sub` and `Methods` = `amr`;
  - `TestVerify_Shape`: the table of [contracts/http-semantics.md](contracts/http-semantics.md) §9 "Credential shape". No header and `Basic …` give none, an API-key-shaped token gives none, and 5 segments give invalid;
  - `TestVerify_Claims` (scenarios 3 and 4):
    - a missing `exp` is invalid;
    - `exp` 61 s ago with the default leeway is invalid, and 59 s ago is accepted;
    - `nbf` 2 min ahead is invalid;
    - `iat` 2 min ahead is invalid;
    - a wrong `iss` is invalid;
    - an `aud` of `["x","orders-api"]` is accepted, and `["x"]` is invalid;
    - an empty `sub` is invalid;
    - an `amr` of `"pwd"` (a string) or `[1]` is invalid;
  - `TestType_Required` (scenario 5): with `WithType("at+jwt")`, `typ` `JWT` and a missing `typ` are invalid, while `application/at+jwt` and `AT+JWT` are accepted;
  - `TestVerify_KidSelection`: two keys bound to different algorithms without `kid` give the unique match. Two keys with the same algorithm and no `kid` in the token give invalid;
  - `TestVerify_Challenge` (scenario 10): through `auth.New`, the response is 401 with exactly `Bearer error="invalid_token"`;
  - `TestClaims_Read` (scenario 1): `jwt.Claims(p)` returns all claims, including `roles`; for a principal from another method, it returns `nil, false`;
  - `TestVerify_StaticNoNetwork` (scenario 11): with only `WithKey`, a `fakeIdP` client sees 0 requests;
  - `TestVerifier_Config`: each of these gives an error matching `auth.ErrInvalidConfig`:
    - a missing issuer or audience;
    - neither keys nor JWKS, or both;
    - `WithJWKS("http://…")`, or a URL without a host;
    - `WithKey` with an algorithm outside the list, a key type that does not match, RSA 1,024 bits, a P-384 key, or an HS256 secret of 31 bytes;
    - `WithLeeway` outside "0 to 5 min";
    - `WithRefreshInterval` outside "1 s to 1 h";
    - `WithHTTPClient(nil)`;
    - `WithType("")`.
- [X] T075 [P] [US6] Write a failing test in `auth/jwt/forgery_test.go`: `TestForgeryCorpus` (SC-007, scenarios 2, 6 and 12). Each case is rejected with 401 through `auth.New`, while valid control tokens for each algorithm are accepted:
  - `alg` `none`;
  - HS256 signed with the RSA public key's PKIX DER bytes as the secret, and again with its PEM bytes;
  - a PS256 token against an RS256-bound key;
  - a missing `exp`;
  - expiry beyond the leeway;
  - the wrong issuer;
  - the audience missing;
  - the wrong `typ`;
  - `crit: ["b64"]` with `b64: true`;
  - `crit: ["exp"]`;
  - an embedded `jwk` of an attacker key that signed the token;
  - a `jku` header pointing to an attacker URL, which the `fakeIdP` must never receive;
  - a 5-segment token.
- [X] T076 [P] [US6] Write failing tests in `auth/jwt/keyset_test.go`, inside `synctest.Test`, following [contracts/http-semantics.md](contracts/http-semantics.md) §9 "Key set downloads":
  - `TestKeySet_Lazy` (scenario 7): 0 requests after `New`, 1 after the first verification, and 0 more while fresh. `Accept` is `application/jwk-set+json, application/json`;
  - `TestKeySet_CacheControl`:
    - `max-age=120` refetches after 2 min;
    - `no-cache` and `max-age=0` refetch after 1 min;
    - `max-age=999999` is clamped to 24 h;
    - no header gives 1 h;
  - `TestKeySet_UnknownKid` (scenario 8): an unknown `kid` refetches once. A rotated key that is present after the refetch is accepted;
  - `TestKeySetRefreshBound` (SC-008): 1,000 tokens with distinct unknown `kid`s within 30 s cause at most 1 download, and they are invalid;
  - `TestKeySet_Unavailable` (scenario 9): a transport error, status 500, an invalid JSON body, a body over 1 MiB, and a set with no usable key all give 500 through `auth.New` when there are no keys;
  - `TestKeySet_StaleOnFailure`: after expiry, a failing refresh keeps the previous keys in use, and the next attempt waits for the refresh interval. An unknown `kid` while the last download failed gives 500;
  - `TestKeySet_SingleFlight`: 50 concurrent verifications on an empty cache cause exactly 1 request;
  - `TestKeySet_DetachedContext`: when the first caller's context is cancelled during the download, the other waiters still get the keys.
- [X] T077 [P] [US6] Write failing internal tests in `auth/jwt/parse_internal_test.go` (`package jwt`) for `parseKeySet`, following research R-18:
  - `TestParseKeySet_Skips`: these keys are skipped while the others are kept:
    - an `OKP/X25519` key;
    - an unknown `kty`;
    - an `oct` key;
    - a private RSA key;
    - `use: "enc"`;
    - RSA under 2,048 bits;
    - EC P-384;
    - `alg: "RS256"` on an EC key;
    - `alg: "HS256"`;
  - `TestParseKeySet_ImpliedAlg`: an RSA key without `alg` is bound to RS256, P-256 to ES256, and Ed25519 to EdDSA;
  - `TestParseKeySet_NoUsable`: an error;
  - `FuzzKeySet`: never panics, and every key returned is bound to an allowed algorithm.

  In `auth/jwt/keyset_internal_test.go` (`package jwt`), for `cacheLifetime(header string) time.Duration` (FR-004, FR-062):
  - `TestCacheLifetime`:
    - `""` gives 1 h;
    - `max-age=120` gives 2 min;
    - `public, MAX-AGE=600` gives 10 min;
    - `no-cache`, `no-store` and `max-age=0` give 1 min;
    - `max-age=999999` gives 24 h;
    - `max-age=-5`, `max-age=abc` and `max-age=99999999999999999999` give 1 h;
  - `FuzzCacheControl`: never panics, and the result is always between 1 min and 24 h.
- [X] T078 [P] [US6] Write a failing test in `auth/jwt/stepup_test.go`: `TestStepUp` (US6 scenario 13), through `auth.New` and `RequireMethods("mfa")`. An `amr` of `["pwd"]` gives 401 with exactly `Bearer error="insufficient_user_authentication", error_description="A different authentication level is required"`; `["pwd","mfa"]` gives 200.
- [X] T079 [P] [US6] Write `FuzzToken` in `auth/jwt/fuzz_test.go`: an arbitrary `Authorization` value never panics, and only the seed tokens that were signed validly authenticate.
- [X] T080 [P] [US6] Write `BenchmarkJWTAuthenticate/{RS256,ES256,EdDSA}` in `auth/jwt/bench_test.go`, with static keys.

### Implementation for User Story 6

- [X] T081 [US6] Implement `auth/jwt/keys.go`:
  - the package doc "Package jwt verifies JSON Web Tokens issued by an identity provider. It only verifies; it never issues.";
  - the unexported bound key `{kid, alg, key}`;
  - the static key validation of data-model.md §9 "Bound key".

  Add the dependency with `GOWORK=off go get github.com/go-jose/go-jose/v4@v4.1.5`.
- [X] T082 [US6] Implement `auth/jwt/parse.go` (`parseKeySet`: `{"keys":[…]}` decoded as raw messages, then each key on its own) until T077 passes.
- [X] T083 [US6] Implement `auth/jwt/keyset.go`:
  - the remote cache (research R-18): a mutex, the keys and their expiry, the instant and error of the last attempt, and an in-flight channel;
  - `cacheLifetime`, the `Cache-Control` parsing and clamping that T077 specifies;
  - a download in the caller's goroutine with `context.WithoutCancel` plus 10 s, status 200 only, a 1 MiB limit;
  - closing the body in a deferred function that joins the close error;
  - the refresh interval.
- [X] T084 [US6] Implement `auth/jwt/verify.go`: the shape check, `josejwt.ParseSigned` with `[RS256, PS256, ES256, EdDSA, HS256]`, rejection of `crit`, the `typ` comparison, key selection, `Claims`, required `exp`, `ValidateWithLeeway`, `sub` and `amr`, following [contracts/http-semantics.md](contracts/http-semantics.md) §9 "Verification".
- [X] T085 [US6] Implement `auth/jwt/jwt.go`:
  - `Option` and the eight options, with the defaults leeway 1 min, refresh 30 s and a client with a 10 s timeout;
  - `Verifier`, `New`, `Authenticate` and `Challenge`, with the three challenges of [contracts/http-semantics.md](contracts/http-semantics.md) §2;
  - `Claims`, reading `Principal.Extra`, which holds an unexported type.

  T074–T080 pass.
- [X] T086 [US6] Add `ExampleNew`, with a static ES256 key generated in the example, and `ExampleClaims` in `auth/jwt/example_test.go`.

**Checkpoint**: `cd auth && GOWORK=off go test -race ./jwt/...` passes.

---

## Phase 9: User Story 7 - Serve simple clients with HTTP Basic (Priority: P3)

**Goal**: `basic.Authenticator` on any `basic.Checker` (FR-051 to FR-054).

**Independent Test**: a stub checker, with `Authorization` headers valid, wrong, malformed and of
other schemes.

### Tests for User Story 7 ⚠️ (write first, see them fail)

- [X] T087 [P] [US7] Write failing tests in `auth/basic/basic_test.go`:
  - `TestAuthenticate_Success` (US7 scenario 1): the stub receives the request context, the username and the password, and the principal passes through;
  - `TestAuthenticate_Rejected` (scenario 2): through `auth.New(basic.New(stub, basic.WithRealm("api")))`, the response is 401 with exactly `Basic realm="api", charset="UTF-8"`;
  - `TestAuthenticate_Malformed` (scenario 3): invalid base64, base64 without padding, no colon, an empty username, and invalid UTF-8 all give invalid;
  - `TestAuthenticate_NotMine` (scenario 4): no header and `Bearer x` give none, while `basic`, `BASIC` and `Basic` are all recognized;
  - `TestAuthenticate_Colons` (scenario 5): the password `"pa:ss:wörd"` reaches the checker as typed;
  - `TestAuthenticate_RequireMFA` (scenario 6): with `RequireMethods("mfa")`, the response is 403;
  - `TestAuthenticate_AttemptLimit` (scenario 7): a real `password.Checker` with the floor parameters and `WithAttemptLimit` over `memstore`; the eleventh request gives 429;
  - `TestRealm_Rules`: the default challenge is `Basic realm="restricted", charset="UTF-8"`. `WithRealm` outside "1 to 128 printable ASCII characters", or a nil checker, gives an error matching `auth.ErrInvalidConfig`. A realm `a"b\c` is escaped as `realm="a\"b\\c"`.
- [X] T088 [P] [US7] Write `FuzzBasicHeader` in `auth/basic/fuzz_test.go`: an arbitrary `Authorization` value never panics, and the stub is called only when the value decodes to `user:pass` with a non-empty user.

### Implementation for User Story 7

- [X] T089 [US7] Implement `auth/basic/basic.go`:
  - the package doc "Package basic implements HTTP Basic authentication (RFC 7617) on a password checker. It offers no second factor.";
  - `Checker`, `Option`, `WithRealm`, `Authenticator`, `New`, `Authenticate` (following [contracts/http-semantics.md](contracts/http-semantics.md) §4) and `Challenge`.

  T087 and T088 pass.
- [X] T090 [US7] Add `ExampleNew` in `auth/basic/example_test.go`, with a stub checker.

**Checkpoint**: `cd auth && GOWORK=off go test -race ./basic/...` passes.

---

## Phase 10: Polish & Cross-Cutting Concerns

**Purpose**: the cross-method success criteria, documentation, release notes and every gate.

- [X] T091 Add `TestInfrastructureFailures` (SC-002, FR-014) to `auth/integration_test.go`, inside `synctest.Test`. For each case, the response through `auth.New` is 500, never 401, and no attempt counter changes, as checked with `storetest.Recorder`:
  - session `OpGet` failing;
  - a failing password lookup, through `basic`;
  - a failing password attempt `OpIncr`;
  - a failing API key lookup;
  - a key server returning 503 with no cached keys.

  For the second factor, a failing TOTP lookup and a failing recovery lookup return non-invalid errors, and the pending attempt counter is unchanged.
- [X] T092 Add `TestStoreHoldsNoSecrets` (SC-003, FR-022) to `auth/integration_test.go`. Run every flow of `TestLoginFlow`, plus TOTP enrollment (`NewEnrollment`, then `Confirm`, FR-047), the attempt limit and a revocation, through a `storetest.Recorder` over `memstore`. Collect every key and value, and assert that none contains any of the following, raw or in hex, standard base64 or base64url:
  - the password;
  - any session or pending cookie value;
  - the TOTP secret (also in base32);
  - any TOTP code or recovery code (also normalized);
  - the username;
  - the subject.
- [X] T093 Add `TestNoSecretsInLogs` (SC-011, FR-008) to `auth/integration_test.go`:
  - log an `apikey.Key`, a `*totp.Enrollment` and a `*recovery.Codes` through `slog.NewJSONHandler` and `slog.NewTextHandler`; the output contains `[REDACTED]` and none of the secrets;
  - collect the error strings of every failure path of T091 and of the invalid-credential paths: a wrong password, a wrong code, a wrong key, a forged token, and the configuration errors for an HS256 key and a realm; none contains a password, code, key, token, username or secret.
- [X] T094 Add the full-flow `ExampleNew` and `ExampleRequireMethods` to `auth/example_test.go` (after T029). Build a mux with `auth.New(auth.Any(sessions, keys, tokens))`, with the `/login`, `/login/otp`, `/logout` and `/admin` handlers of [quickstart.md](quickstart.md) "End-to-end walkthrough", driven by `httptest`. Print the statuses: 403 for a password-only session on `/admin`, then 200 after the second factor.
- [X] T095 Expand `auth/doc.go` into the full package documentation:
  - the four outcomes and their statuses;
  - `Any` and its order;
  - challenges, with the composition and the same-scheme rule;
  - `RequireMethods` and step-up;
  - `Pending` and the shared budget;
  - the `Store` contract in short;
  - the sub-packages and what each needs: a lookup, the store, or neither.

  Re-read every sub-package's package doc for completeness.
- [X] T096 [P] Complete `auth/README.md` under the headings of T003, following research R-22:
  - the full example of T094;
  - the Redis adapter of [contracts/store.md](contracts/store.md) §5, with its notes;
  - CSRF with `http.CrossOriginProtection` around the session routes;
  - Basic: the CPU and memory cost and the missing second factor;
  - the middleware order: CORS before `auth`, and `ratelimit` and `maxconcurrency` around password routes;
  - argon2id parameters and how to raise them to RFC 9106;
  - canonical usernames and the lockout trade-off;
  - the legacy-hash timing caveat, `localhost` cookies and the key-rotation delay.

  Use placeholders such as `acme_<id>_<secret>`, never real-looking keys.
- [X] T097 [P] Fill `auth/CHANGELOG.md` `## [Unreleased]` → `### Added` with one entry per package (`auth`, `memstore`, `password`, `session`, `totp`, `recovery`, `basic`, `apikey`, `jwt`), naming its main exported identifiers.
- [X] T098 Run `bearer scan . --config-file bearer.yml` from the repository root, with Bearer installed by `scripts/install-tool.sh bearer <dir>`. Expect exactly one finding, `go_gosec_blocklist_sha1`, on `auth/internal/hotp/hotp.go`. Suppress it with `bearer ignore add <fingerprint> --comment "RFC 6238 requires HMAC-SHA-1 for authenticator-app compatibility; HMAC-SHA-1 does not rely on SHA-1 collision resistance (RFC 6194)"`, which writes `bearer.ignore`. Re-run: 0 findings. Any other finding is fixed in code, not suppressed.
- [X] T099 In `auth/`:
  - run `GOWORK=off go mod tidy -diff`, which must print nothing;
  - confirm that `go.mod` requires exactly `github.com/go-jose/go-jose/v4 v4.1.5` and `golang.org/x/crypto v0.57.0` directly, plus `golang.org/x/sys` as indirect (SC-012);
  - run `scripts/cross-build.sh auth` from the repository root, for linux, darwin and windows × amd64 and arm64, with no failure.
- [X] T100 Run each fuzz target of [quickstart.md](quickstart.md) "Fuzzing" for 30 s, from `auth/`. Fix any failure, and commit the failing input under `auth/<package>/testdata/fuzz/<Target>/` as a regression seed.
- [X] T101 Run the benchmarks of [quickstart.md](quickstart.md) from `auth/` with `-count 5`: `auth/password/bench_test.go`, `auth/session/bench_test.go`, `auth/apikey/bench_test.go` and `auth/jwt/bench_test.go`. Record the medians in the PR description. Check:
  - `BenchmarkCheck/missing-user` within 5% of `BenchmarkCheck/wrong-password` (SC-001);
  - `BenchmarkSessionAuthenticate`, `BenchmarkAPIKeyAuthenticate` and every `BenchmarkJWTAuthenticate/*` each under 1% of `BenchmarkCheck/existing-user` (SC-009).

  Optimize if a target is missed. Never change a target to make it pass.
- [X] T102 Verify the exported surface against [contracts/public-api.md](contracts/public-api.md):
  - `GOWORK=off go doc -all` for each of the nine public packages (`auth/`, `auth/memstore`, `auth/password`, `auth/session`, `auth/totp`, `auth/recovery`, `auth/basic`, `auth/apikey` and `auth/jwt`) lists exactly the contract's identifiers, 118 in total, each with a doc comment;
  - `GOWORK=off go test -coverprofile=c.out ./... && go tool cover -func=c.out` shows no exported function or method at 0%.
- [X] T103 Run the remaining security gates of [quickstart.md](quickstart.md) "Security gates" from the repository root: Trivy (0 HIGH or CRITICAL), Gitleaks (0 findings) and Semgrep with the pinned image and the `p/golang`, `p/trailofbits` and `p/github-actions` rulesets (0 findings). Fix any finding by changing code or fixtures.
- [X] T104 Run the local gates with `make check MODULE=auth`. golangci-lint must report zero findings, with at most the one `//nolint:contextcheck` allowed by Conventions, and `go test -race -shuffle=on` must pass with coverage ≥ 80% (SC-014). Fix any finding before pushing.

---

## Dependencies & Execution Order

### Phase dependencies

- **Setup (Phase 1)**: no dependencies.
- **Foundational (Phase 2)**: depends on Setup, and blocks every user story. All stories use
  `auth/auth.go` and `auth/store.go`. The internal helpers and `memstore` serve US2–US7, and
  `storetest` serves US2–US4 and Polish.
- **US1 (Phase 3)**: depends on Foundational only.
- **US2 (Phase 4)**: depends on Foundational. Its own tests do not use the middleware.
- **US3 (Phase 5)**: depends on Foundational and on US1, because T043 uses `auth.New` for the
  anonymous and 500 scenarios.
- **US4 (Phase 6)**: depends on US1, US2 and US3, because pending logins extend `session` and
  `TestLoginFlow` uses all three.
- **US5 (Phase 7)**: depends on Foundational and US1 (`auth.New`, `RequireMethods`). It is
  independent of US2–US4.
- **US6 (Phase 8)**: depends on Foundational and US1. It is independent of US2–US5.
- **US7 (Phase 9)**: depends on US1, and on US2 for T087's attempt-limit scenario.
- **Polish (Phase 10)**: depends on every story.

Recommended order: US1 → US2 → US3 → US4 → US5 → US6 → US7. Once US1 is done, US5 and US6 can
proceed in parallel with US2–US4. Each story adds a complete, testable increment, and each leaves
the earlier stories' tests green.

### Within each story

- Test tasks are written first and observed failing. A compilation failure counts.
- Implementation follows its tests, in the order listed: `any.go` → `middleware.go` →
  `reject.go` → `require.go` (US1); `params.go` → `phc.go` → `hash.go` → `options.go` →
  `checker.go` (US2); `record.go` → `session.go` → `issue.go` (US3); `hotp` → `pending.go` →
  `enrollment.go` → `totp.go` → `codes.go` → `recovery.go` (US4); `key.go` → `apikey.go` (US5);
  `keys.go` → `parse.go` → `keyset.go` → `verify.go` → `jwt.go` (US6).
- Example tasks depend on the implementation they show.
- Some files are edited by several tasks, which never run in parallel:
  - `auth/example_test.go`: T029, T094;
  - `auth/integration_test.go`: T058, T091, T092, T093;
  - `auth/session/example_test.go`: T049, T066;
  - `auth/README.md`: T003, T096;
  - `auth/CHANGELOG.md`: T002, T097;
  - `auth/doc.go`: T005, T095;
  - `auth/go.mod` and `auth/go.sum`: T001, T038, T081, T099.

### Parallel opportunities

- **Setup**: T002 and T003.
- **Foundational**: T006, T007, T009, T011, T013, T014 and T017 together. Then each
  implementation follows its own test: T008, T010, T012, T015 → T016, and T018.
- **US1**: T019–T024 together, then T025 → T026 → T027 → T028 → T029.
- **US2**: T030–T035 together, then T036 → T037 → T038 → T039 → T040 → T041.
- **US3**: T042–T045 together, then T046 → T047 → T048 → T049.
- **US4**: T050–T058 together. Then T059 first; T060 next; then T061 → T062 and T063 → T064 as
  two streams; then T065 and T066.
- **US5**: T067–T069 together, then T070 → T071 → T072.
- **US6**: T073–T080 together, then T081 → T082 → T083 → T084 → T085 → T086.
- **US7**: T087 and T088 together, then T089 → T090.
- **Across stories**: after US1, the streams US2 → US3 → US4, US5, and US6 can run in parallel,
  in different packages.
- **Polish**: T096 and T097 with T091–T095. Then T098–T104 in order.

---

## Parallel Example: User Story 4

```bash
# Write all failing tests for US4 together (different files):
Task: "T050 pending tests in auth/session/pending_test.go"
Task: "T051 RFC vectors in auth/internal/hotp/hotp_test.go"
Task: "T052 TOTP tests in auth/totp/totp_test.go"
Task: "T053 enrollment tests in auth/totp/enrollment_test.go"
Task: "T055 code tests in auth/recovery/codes_test.go"
Task: "T056 recovery tests in auth/recovery/recovery_test.go"
Task: "T058 TestLoginFlow in auth/integration_test.go"

# Then implement: hotp and pending first, then TOTP and recovery as two streams:
"T059 hotp.go" → "T060 pending.go" → ("T061 → T062 totp" ‖ "T063 → T064 recovery") → "T065, T066 examples"
```

## Parallel Example: three streams after User Story 1

```bash
# After Phase 3, in different packages:
Stream A: US2 (auth/password) → US3 (auth/session) → US4 (session, totp, recovery, internal/hotp)
Stream B: US5 (auth/apikey)
Stream C: US6 (auth/jwt)
# US7 (auth/basic) after US2; Polish after all.
```

---

## Requirement Coverage

| Requirement | Tasks |
|-------------|-------|
| FR-001 | T001, T004, T102 |
| FR-002, SC-012 | T001, T038, T081, T099 |
| FR-003 | T020, T032, T033, T043, T052, T056, T067, T074, T087 |
| FR-004, SC-014 | T030, T044, T054, T057, T068, T077 (incl. `FuzzCacheControl`), T079, T088, T100, T104 |
| FR-005 | T013, T017, T076 (single flight), T104 (`-race`) |
| FR-006 | T043, T053, T055, T067 |
| FR-007 | T032, T040, T052, T056, T067 |
| FR-008, SC-011 | T053, T055, T067, T093 |
| FR-009 | T013, T032, T043, T076 (request-bound methods use `r.Context()`; plan, Complexity Tracking) |
| FR-010 | T016, T029, T041, T049, T065, T066, T072, T086, T090, T094, T096 |
| FR-011 to FR-014 | T020, T021, T025, T026 |
| FR-015 | T022, T027, T043, T067, T074, T087 |
| FR-016 | T020, T023, T027 |
| FR-017, SC-013 | T024 |
| FR-018 | T020, T026 |
| FR-019 | T023, T028, T058, T067, T078, T087 |
| FR-020, FR-021 | T006, T013, T015 |
| FR-022, SC-003 | T007, T043, T092 |
| FR-023 | T013, T014, T015 |
| FR-024 to FR-029 | T030–T032, T036–T040 |
| FR-030 | T033, T039, T040, T087 |
| FR-031 | T087, T089 |
| FR-032 to FR-037, FR-041, SC-005, SC-006 | T042, T043, T046–T048 |
| FR-038 to FR-040 | T050, T060 |
| FR-042 to FR-047, SC-010 | T051–T054, T059, T061, T062 |
| FR-045, SC-004 | T033, T050, T052, T056, T058 |
| FR-048 to FR-050 | T055–T058, T063, T064 |
| FR-051 to FR-054 | T087–T089 |
| FR-055 to FR-058 | T067–T071 |
| FR-059 to FR-069, SC-007, SC-008 | T073–T085 |
| FR-070 | T013, T014, T033, T043, T050, T052, T076 |
| FR-071 | T013, T033, T050, T052, T056 |
| FR-072, SC-001, SC-009 | T034, T035, T045, T069, T080, T101 |
| SC-002 | T091 |

---

## Implementation Strategy

### MVP first (User Stories 1–3)

The three P1 stories together make the smallest useful product: routes protected by a password
login with sessions.

1. Phase 1: Setup (T001–T004).
2. Phase 2: Foundational (T005–T018). This is critical: it blocks everything else.
3. Phase 3: US1 (T019–T029). The core alone is testable with stubs.
4. Phase 4: US2 (T030–T041).
5. Phase 5: US3 (T042–T049).
6. **Stop and validate**: `make check MODULE=auth` passes, and a login handler built from
   `Check` and `Issue` protects routes behind `auth.New(sessions)`.

### Incremental delivery

1. Setup + Foundational: the module builds; the store and the helpers are tested.
2. + US1: the core (with stub methods).
3. + US2 + US3: password login with sessions (MVP).
4. + US4: the second factor and `RequireMethods("mfa")`.
5. + US5: API keys. It can be developed in parallel after US1.
6. + US6: tokens. It can be developed in parallel after US1.
7. + US7: HTTP Basic.
8. Polish: the cross-method criteria, documentation, CHANGELOG, Bearer suppression and every
   gate. Then release `auth/v0.1.0`.

Each increment leaves `make check MODULE=auth` green.

---

## Notes

- [P] tasks touch different files and depend on no incomplete task.
- Commit after each task or logical group. The PR title is
  `feat(auth): add HTTP request authentication module`.
- Do not weaken a cost target (T101) or a security assertion to make a test pass. If a target
  proves unreachable, update spec.md and research.md first.
- The dependency versions are fixed by research R-02. Upgrading them is a separate change, after
  their release is at least 14 days old.

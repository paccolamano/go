# Research: HTTP Request Authentication (auth)

**Feature**: [spec.md](spec.md) | **Plan**: [plan.md](plan.md) | **Date**: 2026-10-19

Each section records a decision, its rationale and the alternatives rejected. Standard library
APIs were checked against the local Go 1.27 toolchain, and all of them are available in Go 1.26,
the module's minimum version.

The risky decisions were validated by a throwaway spike, kept outside this repository: a module
with this module's path and the two chosen dependencies at the versions of R-02, tested with
`-race`, linted with the repository's golangci-lint configuration and scanned with the pinned
Bearer, Semgrep and Trivy versions. The measurements come from that spike, run on Go 1.27.0,
linux/amd64 and an Intel i7-11800H, over 3 runs.

## R-01 Module layout, versioning and Go version

- **Decision**:
  - One module, `github.com/paccolamano/go/auth`, in the top-level directory `auth/`.
  - Packages:
    - `auth`: the core;
    - `auth/memstore`;
    - `auth/password`;
    - `auth/session`;
    - `auth/totp`;
    - `auth/recovery`;
    - `auth/basic`;
    - `auth/apikey`;
    - `auth/jwt`.
  - Five internal packages hold code that several packages share:
    - `internal/storekey` derives store keys from secrets;
    - `internal/crockford` encodes and normalizes Crockford base32, for API keys and recovery
      codes;
    - `internal/authheader` parses `Authorization` credentials and quotes challenge parameters;
    - `internal/hotp` computes HOTP values (R-14);
    - `internal/storetest` wraps any store to record or fail its calls, for tests (R-20).
  - The `go` directive is `go 1.26.0`. CI tests on 1.26 and 1.27. `go.work` is regenerated with
    `make work`.
  - Releases start at `auth/v0.1.0` and stay at 0.x until the API is declared stable.
- **Rationale**:
  - FR-001 asks for one module, one package for the core and one per method.
  - Sub-packages keep each method's API small. An application that uses only sessions never
    compiles go-jose, because Go builds only the imported packages.
  - Each internal package has two or more consumers, so none of them is a speculative layer.
- **Alternatives rejected**:
  - **A separate module for `jwt`**, so that non-JWT users would not even download go-jose. The
    request asks for one module. go-jose has no dependencies of its own (R-02). With module graph
    pruning it only adds one `go.sum` entry for consumers who never import `auth/jwt`.
  - **All the methods in package `auth`**: one large API, and every method's options in one
    namespace.

## R-02 Dependencies (Principle V)

- **Decision**: the module requires exactly:
  - `golang.org/x/crypto v0.57.0`, for the `argon2` and `bcrypt` packages;
  - `github.com/go-jose/go-jose/v4 v4.1.5`, for JWS parsing, signature verification, JWK
    parsing and claims validation.

  Each version is the newest one published at least 14 days before the plan, the same policy
  `versions.env` applies to tools: v0.57.0 on 2026-09-08 and v4.1.5 on 2026-09-03. The newer
  v0.58.0 and v4.1.6 are under 14 days old. Tests use only the standard library.
- **Rationale**:
  - **The standard library is insufficient.** It has neither argon2 nor bcrypt. It has no JOSE
    support: no JWS compact parsing, no JWK decoding of RSA, EC and OKP keys, no claims
    validation.
  - **x/crypto**:
    - license BSD-3-Clause, compatible with MIT; maintained by the Go team.
    - Its `go.mod` (`go 1.26.0`) requires `x/net`, `x/sys`, `x/term` and `x/text`. After
      `go mod tidy`, the spike's `go.mod` lists only `golang.org/x/sys v0.48.0 // indirect`,
      because `argon2` and `bcrypt` import only `x/sys/cpu` and x/crypto's own `blake2b` and
      `blowfish`.
  - **go-jose v4**:
    - license Apache-2.0, compatible with MIT for a dependency;
    - `go.mod` at `go 1.24.0`, with no requirements at all;
    - maintained, and used by `coreos/go-oidc`;
    - preferred to `golang-jwt/jwt` because it also parses JWK and JWK Sets (request).
  - **Trivy**, run with the pinned database on the spike module, reports 0 HIGH and 0 CRITICAL
    vulnerabilities.
  - **Memory note** (user feedback): established libraries are preferred over in-house
    protocol and crypto code, provided they are justified as above.
- **Alternatives rejected**:
  - **Hand-written JOSE on `crypto/*`**: it is exactly the code the request moves to a library.
  - **`golang-jwt/jwt/v5`**: it would need a second library, or our own code, for JWK Sets.
  - **The newest versions** (v0.58.0, v4.1.6): under 14 days old. Their changelogs are reviewed
    when the next release is cut.

## R-03 Core types and sentinel errors

- **Decision**:
  - The core exposes:
    - `Authenticator`, with `Authenticate(*http.Request) (*Principal, error)`, as in the request;
    - an optional `Challenger` interface (R-05);
    - `Principal{Subject string; Methods []string; Extra any}`;
    - six sentinel errors: `ErrNoCredentials`, `ErrInvalidCredentials`, `ErrTooManyAttempts`,
      `ErrInsufficientMethods`, `ErrNotFound` and `ErrInvalidConfig`.
  - `Extra` carries data specific to the method that produced the principal, read through that
    method's package. The only producer is `jwt`, whose `jwt.Claims(p)` reads its claims.
  - Lookup functions report "not found" with `auth.ErrNotFound`. Each package that takes a
    lookup re-exports it under the name of the request, `password.ErrNotFound`,
    `apikey.ErrNotFound` and so on, as `var ErrNotFound = auth.ErrNotFound`. These are the same
    value.
  - Every constructor of the module wraps `auth.ErrInvalidConfig` (FR-003).
- **Rationale**:
  - **`Extra`**: a principal comes from exactly one method, so one slot is enough. It lets
    `jwt.Claims(p)` keep the request's shape without putting JWT claims in the core.
    - The slot holds an unexported type of `jwt`, so only `jwt` can create a value that
      `Claims` recognizes.
    - Sessions store only the subject and the methods (R-13), so `Extra` is never persisted.
  - **One `ErrNotFound`**: the core, the store and every lookup agree on one value, so that
    `errors.Is` works across packages. The spike's golangci-lint run accepts
    `var ErrNotFound = core.ErrNotFound` under `gochecknoglobals`.
  - **`ErrTooManyAttempts`** is in the core because the middleware maps it to 429 (FR-014),
    whichever package returns it.
- **Alternatives rejected**:
  - **A `Value(key)`/`WithValue` map on `Principal`, like `context`**: more API than one slot,
    and no current use needs more than one value.
  - **A `Claims map[string]any` field in the core**: it ties the core to tokens.
  - **A separate "not found" sentinel per package**: developers would mix them up, and the
    request's `password.ErrNotFound` and `apikey.ErrNotFound` keep working as aliases.

## R-04 Combining methods with `Any`

- **Decision**:
  - `Any(methods ...Authenticator) Authenticator` returns an unexported type, which tries the
    methods in order (FR-013).
  - Inside package `auth`, that type also reports which leaf method decided the request. Nested
    `Any`s are flattened into one ordered list of leaves.
  - `New` validates the whole tree: a nil method and an `Any` with no methods are configuration
    errors. This keeps `Any` single-valued, as the request writes it in
    `auth.New(auth.Any(sessions, keys, tokens))`.
  - Classification uses `errors.Is`, in this order:
    1. `ErrNoCredentials` → try the next method;
    2. `ErrTooManyAttempts` → stop, 429;
    3. `ErrInvalidCredentials` → stop, 401;
    4. anything else → stop, 500.

    A nil principal with a nil error, or a principal with an empty subject, becomes an internal
    contract error, answered with 500 (FR-013).
- **Rationale**:
  - Challenges (R-05) and step-up (R-07) need the deciding method. Keeping that knowledge
    unexported avoids exposing a second authentication interface.
  - `ErrTooManyAttempts` is tested before `ErrInvalidCredentials` because the second-factor
    budget returns an error that wraps both (R-12).
- **Alternatives rejected**:
  - **An exported `*Combination` type**: no caller needs its methods, so returning the interface
    keeps the surface minimal (Principle II).
  - **`Any` returning `(Authenticator, error)`**: it breaks the request's one-line composition,
    and `New` can validate just as well.

## R-05 Challenges

- **Decision**:
  - A method contributes challenges by implementing
    `Challenger { Challenge(cause error) string }`. `cause` is one of `ErrNoCredentials`,
    `ErrInvalidCredentials` and `ErrInsufficientMethods`, and "" means "none for this cause".
  - The challenges of the module's methods are:

    | Method | No credentials | Invalid credentials | Insufficient methods |
    |--------|----------------|---------------------|----------------------|
    | `basic` | `Basic realm="<realm>", charset="UTF-8"` | same | "" (403) |
    | `apikey`, Authorization | `Bearer` | `Bearer error="invalid_token"` | "" (403) |
    | `apikey`, custom header | `ApiKey header="<name>"` | same | "" (403) |
    | `jwt` | `Bearer` | `Bearer error="invalid_token"` | `Bearer error="insufficient_user_authentication", error_description="A different authentication level is required"` |
    | `session` | `Cookie cookie-name="<cookie name>"` | never asked (R-13) | "" (403) |

  - Composition:
    - the middleware walks the leaves in order and collects each leaf's challenge for
      `ErrNoCredentials`, skipping "" and exact duplicates;
    - when leaf L rejects invalid credentials, L's challenge for `ErrInvalidCredentials` takes
      the place of L's plain challenge, and every other challenge with the same scheme is
      dropped (the scheme is the first token, compared without regard to case);
    - each challenge is written as its own `WWW-Authenticate` field line;
    - when no leaf defines a challenge, the header is omitted (FR-015).
  - Parameter values are written as quoted strings, escaping `"` and `\` (RFC 9110 §5.6.4).
- **Rationale**:
  - **`Bearer` dedup**: `apikey` and `jwt` both use Bearer, so `Any(keys, tokens)` sends one
    `Bearer` challenge, not two. Without the scheme rule, an invalid token would produce both
    `Bearer` and `Bearer error="invalid_token"`, which contradict each other.
  - **One field line per challenge**: clients that split a single header on commas misread
    `realm="a, b"`.
  - **Sessions and custom-header keys** have no registered scheme. `Cookie` with `cookie-name`
    follows the parameter naming of the expired `draft-broyer-http-cookie-auth`, the only
    precedent. `ApiKey` names the header the client must send.
  - **No `realm` on Bearer**: RFC 6750 makes it optional, and the method has nothing meaningful
    to put in it.
- **Alternatives rejected**:
  - **A fixed fallback challenge from the core when no leaf defines one**: it would invent a
    scheme on behalf of methods written outside the module.
  - **One method per cause** (`NoCredentialsChallenge()` and the like): three methods for third
    parties to implement, where one switch suffices.

## R-06 The middleware, statuses and rejection

- **Decision**:
  - `New(a Authenticator, opts ...Option) (func(http.Handler) http.Handler, error)` returns a
    plain function type, which matches the `middleware.Middleware` alias without importing it.
    The options are `WithAnonymous()` and `WithRejectFunc(RejectFunc)`.
  - Statuses (FR-014): no credentials → 401 or anonymous pass-through; invalid → 401, also with
    anonymous; too many attempts → 429; any other error → 500.
  - `auth.Rejection{Status int; Err error}` and
    `auth.RejectFunc func(http.ResponseWriter, *http.Request, Rejection)` have the shape of
    `middleware`'s types.
  - The middleware sets `WWW-Authenticate` first, then calls the function. If the function
    writes neither a status nor a body, the status is sent with an empty body.
  - The default writes `http.StatusText(status)` as `text/plain; charset=utf-8`.
  - The cause of a 500 is passed to the function but never written by the default.
  - On every request that passes, the middleware stores in the context a small state:
    - the principal, or none in anonymous mode;
    - the deciding leaf;
    - the plain challenges;
    - the rejection function.

    `FromContext` and `RequireMethods` (R-07) read it.
- **Rationale**:
  - **Same conventions as `middleware`** (request): functional options, the shared invalid
    configuration error, and a customizable rejection.
  - **Own types instead of importing `middleware`** (spec Assumptions): the request allows
    exactly two dependencies, and `middleware` is at 0.x. A problem-document writer reads the
    same two fields in both modules.
  - **The state in the context** lets `RequireMethods` reuse the rejection function and the
    challenges, so the developer configures them once.
- **Alternatives rejected**:
  - **Depending on `middleware` v0.1.0 for `RejectFunc`**: a third dependency, and coupling to
    a 0.x API.
  - **Writing the cause of a 500 in the body**: it can contain infrastructure details.

## R-07 Requirement of methods

- **Decision**:
  - `RequireMethods(methods ...string) func(http.Handler) http.Handler` keeps the request's
    single-value shape. It copies the list, and a request passes when every listed value is in
    `Principal.Methods`. An empty list requires only a principal.
  - Rejections:
    - no state in the context, because the authentication middleware did not run: 500 with the
      default response, which makes the wiring mistake visible at once;
    - no principal (anonymous mode): 401 with the stored plain challenges and
      `ErrNoCredentials`;
    - a missing method: `ErrInsufficientMethods`. When the deciding leaf's challenge for
      `ErrInsufficientMethods` is not "", the response is 401 with that challenge. Otherwise it
      is 403 (FR-019, clarified).
  - The stored rejection function writes all three responses.
- **Rationale**:
  - The status follows the clarification: RFC 9470 for tokens, and 403 elsewhere, because the
    request is authenticated.
  - The distinct cause lets a browser application redirect to its second-factor page.
- **Alternatives rejected**:
  - **`RequireMethods` with options for its own rejection function**: a second place to
    configure the same thing, and the request writes it without options.

## R-08 Store contract and in-memory store

- **Decision**:
  - `auth.Store` has the request's four methods. Its contract is in
    [contracts/store.md](contracts/store.md):
    - `Get` returns `ErrNotFound` for absent or expired keys;
    - `Set` and `Incr` take a TTL greater than 0;
    - `Delete` of an absent key returns nil;
    - `Incr` is atomic. It creates the counter at 1 with the TTL, and later increments keep the
      original expiry.
  - The library never reads a counter with `Get` and never increments a value written with
    `Set`. An adapter therefore needs no rule for mixing the two.
  - `memstore.Store` (`memstore.New()`):
    - a mutex-guarded map of entries, each holding bytes or a counter and an expiry instant;
    - it copies values on `Set` and on `Get`;
    - expired entries are removed when they are touched, and by a sweep of the whole map run
      inside a write whenever a sweep interval has passed since the previous one. There is no
      goroutine, and memory is bounded by the live entries plus at most one interval's worth of
      expired ones;
    - `Incr` on a key that holds bytes returns an error;
    - time comes from `time.Now`, which `testing/synctest` virtualizes.
  - The README's Redis adapter is about twenty lines: `GET`, `SET` with `PX`, `DEL`, and `Incr`
    through a short Lua script that runs `INCR` and, when the result is 1, `PEXPIRE`. Redis runs
    the script atomically.
- **Rationale**:
  - "TTL set at creation" is what a fixed-window attempt limit needs. It is also the natural
    semantics of Redis `INCR` plus `EXPIRE NX`.
  - The amortized sweep satisfies FR-023 without background work (FR-005).
- **Alternatives rejected**:
  - **A cleanup goroutine with a `Close` method**: lifecycle management for a test helper.
  - **Extending the TTL on every increment**: an attacker who keeps failing would keep the
    window open forever, and legitimate users would wait longer than documented.
  - **A Store conformance suite exported for adapters**: not requested.

## R-09 Store keys and value format

- **Decision**:
  - Keys have the form `auth:<kind>:<digest>`, where `<digest>` is the unpadded base64url
    encoding of SHA-256 of the input, 43 characters. The kinds are:

    | Kind | Input of the digest | Written by |
    |------|---------------------|------------|
    | `session` | session identifier | `session` |
    | `pending` | pending identifier | `session` |
    | `pending-attempts` | pending identifier | `session` (R-12) |
    | `password-attempts` | username as passed to `Check` | `password` (R-11) |
    | `totp-used` | subject, then `:<step>` in decimal after the digest | `totp` |
    | `recovery-used` | subject, a NUL byte, then the code hash | `recovery` |

  - Session and pending values are JSON objects with a format version:
    `{"v":1,"sub":…,"amr":[…],"iat":…,"exp":…,"rw":…}`, with times as Unix seconds. A value
    whose `v` is unknown, or that does not decode, is an error (spec edge case). It is never
    accepted as valid.
- **Rationale**:
  - **Digests** keep identifiers out of the store (FR-022). Hashing subjects and usernames too
    keeps personal data such as e-mail addresses out of keys, and makes every key safe for any
    store's key syntax.
  - **The `auth:` namespace** groups the library's keys. An adapter can add an application
    prefix when several applications share one Redis.
  - **JSON with `v`** is readable when debugging, and versioned as the request asks.
- **Alternatives rejected**:
  - **A binary encoding**: a negligible size gain, for values of about 150 bytes.
  - **Subjects in clear in keys**: personal data in the store, and possible `:` characters.

## R-10 Password hashing

- **Decision**:
  - Defaults (`password.DefaultParams()`): argon2id, version 19, memory 19,456 KiB (19 MiB),
    2 iterations, parallelism 1, a 16-byte salt and a 32-byte key. This is the first
    configuration recommended by the OWASP Password Storage Cheat Sheet.
  - Floor for configured parameters (`ErrInvalidConfig` otherwise): memory ≥ 7,168 KiB and
    memory × iterations ≥ 35,840 KiB. Every OWASP configuration meets it:
    46 MiB × 1, 19 × 2, 12 × 3, 9 × 4 and 7 × 5.
  - Bounds for stored argon2id hashes:
    - memory ≤ 2 GiB, the first option of RFC 9106;
    - iterations 1 to 100;
    - parallelism 1 to 64;
    - salt 8 to 64 bytes and key 16 to 64 bytes.

    A hash outside these bounds is malformed (FR-027). The PHC string format is
    `$argon2id$v=19$m=<m>,t=<t>,p=<p>$<salt>$<hash>`, with unpadded standard base64.
  - Stored bcrypt hashes with the prefixes `$2a$`, `$2b$` and `$2y$` are verified with
    `bcrypt.CompareHashAndPassword`. The result is then rejected when the password is longer
    than 72 bytes.
  - The default maximum password length is 1,024 bytes, configurable with `WithMaxLength` from
    256 to 4,096 bytes (FR-028, clarified 2026-10-20). The limit is in bytes, which is
    deterministic. 256 bytes hold 64 UTF-8 characters of up to 4 bytes each, so every allowed
    value meets FR-028's 64 characters; the default holds at least 256.
  - `password.Hash(plain)` uses the defaults. `(*Checker).Hash(plain)` uses the checker's
    parameters. Both return `ErrEmpty` and `ErrTooLong`.
- **Rationale**:
  - **Measured** (spike):

    | Configuration | Time | Time with `-race` | Memory per hash |
    |---------------|------|-------------------|-----------------|
    | 19 MiB, t=2, p=1 (default) | 17.0 ms | 35.9 ms | 19.9 MB |
    | 7 MiB, t=5, p=1 | 14.0 ms | 21.7 ms | 7.3 MB |
    | 46 MiB, t=1, p=1 | 21.4 ms | 45.0 ms | 48.2 MB |
    | 64 MiB, t=3, p=4 (RFC 9106 option 2) | 29.0 ms | 73.5 ms | 67.1 MB |

    The RFC 9106 option costs 3.4 times the memory for each concurrent check. With Basic, which
    checks a password at every request, that bounds concurrency much sooner. The README shows
    how to raise the parameters.
  - **bcrypt**: the spike confirmed that `CompareHashAndPassword` accepts a 73-byte password
    whose first 72 bytes are correct, while `GenerateFromPassword` refuses it. The explicit
    length check is therefore required.
    - The comparison still runs before the check, so a long password costs the same bcrypt work
      as any other.
    - bcrypt hashes are always outdated (R-11), so they are rehashed at the next login.
  - **The floor** addresses the request's "weak parameters" without forbidding any configuration
    OWASP recommends. Stored hashes below the floor still verify, and they are rehashed.
- **Alternatives rejected**:
  - **RFC 9106 option 2 as the default**: see the memory note above.
  - **A length limit in characters**: the limit would depend on normalization, and the module
    does not normalize (spec Assumptions).
  - **`Hash(plain, opts...)` sharing the checker's options**: options such as `WithRehash`
    would be invalid there, and the parameters could drift from the checker's.

## R-11 Password checker

- **Decision**: `NewChecker(lookup, opts...)` computes the checker's dummy hash at construction,
  with its current parameters. `Check(ctx, username, plain)` then works as follows:
  1. An empty password, a password over the maximum length, or an empty username returns
     `ErrInvalidCredentials` before any work. This depends only on the input, so it reveals
     nothing about accounts.
  2. It calls the lookup:
     - `ErrNotFound` selects the dummy hash;
     - any other error is wrapped and returned, and nothing is counted;
     - an account with an empty subject is a contract error.
  3. With the attempt limit set, it counts the attempt with
     `Incr(auth:password-attempts:<digest>, window)`. A store error is returned, and the attempt
     is not counted. A value above `Max` returns `ErrTooManyAttempts` without verifying.
  4. It verifies the password against the stored hash, or against the dummy hash, and compares
     the derived keys in constant time.
  5. On success:
     - with the attempt limit set, it deletes the counter; a delete error is returned;
     - when the hash's algorithm or parameters differ from the current ones and `WithRehash`
       is set, it calls `RehashFunc(ctx, subject, newHash)`, which returns nothing (FR-029);
     - it returns `&auth.Principal{Subject: …, Methods: []string{"pwd"}}`.
  6. On failure, it returns `ErrInvalidCredentials`. A malformed stored hash returns a non-auth
     error (FR-027).
- **Attempt limit** (FR-030, clarified):
  - It is enabled with
    `WithAttemptLimit(password.AttemptLimit{Store, Max, Window})`. A zero `Max` or `Window`
    selects the default, 10 failures per 15 minutes.
  - The count is a reservation, taken before verification. Parallel requests therefore verify at
    most `Max` passwords per window (User Story 2, scenario 13). A success clears the count.
- **Rationale**:
  - **Lookup before counting**: this satisfies "a lookup failure does not count". The cost is a
    database read for blocked usernames, which is negligible next to argon2id.
  - **Counting non-existing usernames** is what keeps the limit from revealing which accounts
    exist (FR-030).
  - **The dummy hash at construction** adds one hash computation at startup (17 ms) and removes
    any lazy-initialization race.
  - **A rehash callback that returns nothing**: FR-029 says its failure must not fail the login,
    so the error has no caller to go to. The developer handles it inside the callback.
- **Alternatives rejected**:
  - **Counting only failures, after verification**: N parallel requests would all be verified
    before any is counted, so the bound would not hold.
  - **Keying the counter by subject**: case variants of the username map to one subject for
    existing accounts and to several counters for missing ones, so the limit would reveal which
    accounts exist. The README tells developers to pass canonical usernames instead (spec edge
    case).

## R-12 Pending logins and the second-factor budget

- **Decision**:
  - The core defines:

    ```go
    type Pending interface {
        Principal() *Principal
        Attempt(ctx context.Context, verify func(context.Context) error) error
    }
    ```

  - `session.Manager.Pending(r)` returns a `Pending`. `totp.Verifier.Verify` and
    `recovery.Verifier.Verify` take one.
  - `Attempt` works as follows:
    1. It reserves an attempt with `Incr(auth:pending-attempts:<digest>, pendingLifetime)`. A
       store error is returned, and no attempt is counted.
    2. A value above 5 deletes the pending login and returns `ErrTooManyAttempts`.
    3. Otherwise it calls `verify`. When `verify` returns `ErrInvalidCredentials` and this was
       the fifth attempt, it deletes the pending login and returns an error that wraps both
       `ErrTooManyAttempts` and `ErrInvalidCredentials`.
    4. Any other result of `verify` is returned unchanged.
  - The budget of 5 is a constant of `session`. Time-based and recovery codes go through the
    same `Attempt`, so they share it (FR-045).
- **Rationale**:
  - The budget lives in one place, the producer of pending logins. The two second factors only
    supply the check, and a future factor, such as WebAuthn, would plug in the same way.
  - Reserving before evaluating bounds parallel attempts at 5 (SC-004, FR-045 as refined).
  - Wrapping both errors on the fifth failure lets the developer either show "wrong code" or
    restart the login, with ordinary `errors.Is` checks.
- **Alternatives rejected**:
  - **A concrete `session.Pending` imported by `totp` and `recovery`**: it couples the second
    factors to cookie sessions.
  - **Exposing the pending store key so that `totp` counts attempts itself**: it duplicates the
    budget logic in two packages and leaks the key layout.
  - **Lazy cancellation at the sixth attempt**: `Pending(r)` would keep returning a login that
    can no longer succeed (FR-039).

## R-13 Sessions

- **Decision**:
  - `session.New(store, opts...)` returns a `*Manager`. Options: `WithCookieName` (base name,
    default `session`), `WithLifetime` (absolute, default 8 h), `WithIdleTimeout` (default
    30 min) and `WithPendingLifetime` (default 5 min).
  - The cookies are `__Host-<name>` and `__Host-<name>-pending`. Both have `Secure`,
    `HttpOnly`, `SameSite=Lax`, `Path=/` and no `Domain`. `Max-Age` is the absolute lifetime, or
    the pending lifetime for the pending cookie.
  - Identifiers are 32 bytes from `crypto/rand`, in unpadded base64url (43 characters). A cookie
    value of any other shape means "no credentials", before any store access.
  - The renewal interval is a quarter of the idle timeout:
    - at issuance, the record is written with `rw` = now and a TTL of
      `min(now + idle, exp) − now`;
    - `Authenticate` reads the record and checks `exp` and `rw + idle` against the clock, in
      case an adapter keeps entries past their TTL;
    - when `now − rw ≥ idle/4`, it rewrites the record with `rw` = now. A renewal write error is
      returned (500).
    - With the defaults, a session expires between 22.5 and 30 minutes after its last use.
  - `Issue`:
    1. deletes the session and the pending login carried by the request; on an error it returns
       without issuing;
    2. writes the new record;
    3. sets the cookie and clears the pending cookie.

    `IssuePending` replaces only a previous pending login. `Revoke` deletes both the session and
    the pending login and clears both cookies, even when the delete fails, and then returns the
    error.
  - A cookie whose session is unknown, expired or malformed returns `ErrNoCredentials` (spec
    Assumptions). `Challenge(ErrInvalidCredentials)` is therefore never needed. It returns the
    plain challenge.
  - Validation: lifetimes are greater than 0, idle ≤ lifetime and pending ≤ 1 h. The cookie base
    name is an HTTP token (RFC 9110 §5.6.2) of at most 64 bytes.
- **Rationale**:
  - **Defaults** follow the OWASP Session Management Cheat Sheet: an idle timeout of 15 to
    30 minutes for low-risk applications, and an absolute timeout of 4 to 8 hours for
    applications used during a working day.
  - **A quarter of the idle timeout** bounds writes to 4 per idle period (SC-006). It also keeps
    the effective idle timeout within 25% of the configured one.
  - **Deleting old state before writing the new session** is the fixation-safe order: if the
    delete fails, no new session exists.
- **Alternatives rejected**:
  - **Renewing at every request**: one store write per request, which the request forbids.
  - **Browser-session cookies without `Max-Age`**: browsers that restore sessions keep them
    indefinitely, and the store already enforces the lifetimes.

## R-14 Time-based codes

- **Decision**:
  - HOTP (RFC 4226) is implemented on `crypto/hmac`, with dynamic truncation, in
    `internal/hotp`. Its function is generic over an algorithm constant (SHA-1, SHA-256 or
    SHA-512) and the number of digits.
    - Production uses HMAC-SHA-1 with 6 digits.
    - Tests call the function with SHA-256 and SHA-512 and with 8 digits, for the vectors of
      RFC 6238 Appendix B.
    - The tests of `totp` and the root package's integration test compute valid codes through
      the same package. No test file imports `crypto/sha1`.
  - TOTP uses a 30-second step from the Unix epoch and accepts the steps t − 1, t and t + 1.
    All three codes are computed and compared in constant time. A code that is not exactly 6
    ASCII digits is a failed attempt.
  - Replay protection: `Incr(auth:totp-used:<digest>:<step>, 4 × 30 s)` after a match. A value
    above 1 is a replay, returned as `ErrInvalidCredentials`. The TTL covers the whole window of
    a step (90 s) plus one step of clock difference between instances.
  - Enrollment:
    - `totp.NewEnrollment(issuer, account)` returns an `*Enrollment` holding a 20-byte secret
      (160 bits) and its URI,
      `otpauth://totp/<issuer>:<account>?secret=<base32>&issuer=<issuer>&algorithm=SHA1&digits=6&period=30`,
      with the label and query escaped. An issuer or account that is empty or contains `:` is an
      error.
    - `(*Verifier).Confirm(ctx, subject, secret, code)` verifies the first code, with replay
      protection and no attempt budget. The developer then stores the secret as active.
  - `Verify(ctx, pending, code)` first looks up the secret. "Not found" returns an error that
    wraps `ErrNotFound`, and nothing is counted. Then it calls `pending.Attempt`. On success it
    returns the pending methods plus `otp` and `mfa`.
- **Rationale**:
  - **HMAC-SHA-1** is what authenticator apps support universally. SHA-1's collision weakness
    does not affect HMAC, whose security rests on the hash as a PRF (RFC 6194). The parameters
    are exactly those of the request.
  - **Replay per subject and step** satisfies RFC 6238 §5.2. The atomic increment makes it hold
    under parallel requests (FR-046).
  - **Confirm without a budget**: the person confirming already holds the new secret, so
    guessing gains nothing.
- **Alternatives rejected**:
  - **A third-party TOTP library**: about 40 lines on `crypto/hmac`, and a third dependency.
  - **Remembering only the last used step**: it needs a read and a write in sequence. That is
    not atomic on the store contract, and the increment already gives the guarantee.

## R-15 Recovery codes

- **Decision**:
  - `recovery.Generate(n)` (1 ≤ n ≤ 100; `recovery.DefaultCount` = 10) returns `*Codes`. Each
    code is 10 bytes from `crypto/rand` (80 bits), as 16 Crockford base32 characters shown in
    groups of 4, `xxxx-xxxx-xxxx-xxxx`.
  - `Codes.Plain()` returns the codes and `Codes.Hashes()` their SHA-256 hashes in lowercase
    hex.
  - Input normalization:
    - lower-casing;
    - removing spaces and hyphens;
    - mapping `i` and `l` to `1` and `o` to `0` (Crockford's decoding rules);
    - after that, anything other than 16 valid characters is a failed attempt.
  - `recovery.New(lookup, consume, store)`:
    - `LookupFunc(ctx, subject) ([]string, error)` returns the hashes of unused codes;
    - `ConsumeFunc(ctx, subject, hash) error` marks a code as used, and returns `ErrNotFound`
      when it already was.
  - `Verify(ctx, pending, code)`:
    1. It looks up the hashes. A failure is not counted.
    2. Inside `pending.Attempt`, it hashes the input and compares it with every stored hash in
       constant time, without stopping early.
    3. On a match, it takes `Incr(auth:recovery-used:<digest>, 10 min)` as a guard against
       parallel use; a value above 1 is invalid.
    4. It calls `consume`. `ErrNotFound` from `consume` is invalid.
    5. On success it returns the pending methods plus `mfa` (spec Assumptions).
- **Rationale**:
  - **80 bits** put an offline search of a stolen SHA-256 hash out of reach (about 2^80 hashes).
    A slow hash is therefore not needed, and comparing against 10 stored hashes stays cheap.
  - **Crockford's alphabet** has no `i`, `l`, `o` or `u`, so codes typed by hand are not
    misread.
  - **The guard and `consume`** together give "at most once" even when the developer's
    `consume` is not atomic. A developer who implements it with a conditional update gets the
    guarantee from the database too.
- **Alternatives rejected**:
  - **argon2id for recovery codes**: 10 slow hashes per attempt, for no gain at 80 bits.
  - **A configurable code length**: not requested.

## R-16 HTTP Basic and API keys

- **Basic**:
  - `basic.New(checker, opts...)` takes a consumer-defined interface,
    `basic.Checker { Check(ctx, username, password string) (*auth.Principal, error) }`. The
    password checker satisfies it, and tests can use a stub without running argon2id.
  - Parsing (FR-051, FR-052):
    - the scheme is matched without regard to case;
    - the credentials are standard base64 with padding, split at the first colon, and both
      parts must be valid UTF-8;
    - an empty username is invalid.
  - The default realm is `restricted`. `WithRealm` rejects an empty realm and control or
    non-ASCII characters, and escapes `"` and `\` in the challenge.
- **API keys**:
  - `apikey.New(prefix, lookup, opts...)`. The prefix is 1 to 32 ASCII letters and digits,
    starting with a letter. `WithHeader(name)` reads the whole value of a custom header instead
    of `Authorization: Bearer`.
  - `(*Keys).Generate() (Key, Stored)`. The key is `<prefix>_<id>_<secret>`:
    - `id` is 10 random bytes, as 16 Crockford base32 characters;
    - `secret` is 32 random bytes (256 bits), as 52 characters.

    `Stored{ID string; Hash []byte}` holds the identifier and the SHA-256 of the 32 secret
    bytes. The alphabet contains no `_`, so parsing is unambiguous.
  - `Generate` returns no error: since Go 1.24, `crypto/rand.Read` never returns one. This is a
    deliberate departure from the request's `key, stored, err := keys.Generate()`.
  - `Key` is a `string` type whose `LogValue` returns `[REDACTED]`. `fmt` and templates still
    print it, so it can be shown to the user once.
  - Verification:
    - the prefix is checked: a credential without it is `ErrNoCredentials`;
    - a shape mismatch is invalid;
    - the lookup is called with the identifier. `ErrNotFound` is invalid, and any other error
      is returned;
    - the 32-byte hashes are compared in constant time, and a past `ExpiresAt` is invalid;
    - the result is `&auth.Principal{Subject: account.Subject}`, with no methods.
- **Rationale**:
  - Base32 instead of base64url, because base64url's alphabet contains `_`, the separator.
  - Measured in the spike: SHA-256 plus a constant-time compare takes 79 ns, so verification is
    dominated by the developer's lookup (SC-009).
- **Alternatives rejected**:
  - **A CRC checksum suffix, as some vendors use for scanners**: not requested. The prefix
    already makes keys recognizable.

## R-17 Token verification

- **Spike findings on go-jose v4.1.5**:
  - `alg: none` and algorithms outside the list are rejected by `jwt.ParseSigned`.
  - `crit: ["exp"]` is rejected, but `crit: ["b64"]` is **accepted**: `b64` is the only critical
    extension go-jose supports. The module therefore rejects any token whose header has `crit`
    (FR-069 as refined). `crit` is visible in `Headers[0].ExtraHeaders`.
  - A token without `exp` passes `ValidateWithLeeway`, so an explicit check is required.
  - A PS256 token verifies with an RSA key intended for RS256: go-jose does not bind keys to
    algorithms. The binding is the module's (FR-060).
  - HS256 verified with an `*rsa.PublicKey` fails in go-jose's primitive, and the binding
    rejects it earlier anyway.
  - A token with an embedded `jwk` parses, and verification uses only the key passed in. The
    module never reads `jwk`, `jku`, `x5u` or `x5c`.
  - Unmarshalling a whole `JSONWebKeySet` fails if any key is unsupported (`OKP/X25519`, an
    unknown `kty`), so keys are parsed one by one (R-18). An `oct` key parses as `[]byte` with
    `IsPublic() == false`.
  - A five-part token (JWE) fails `ParseSigned`.
- **Decision**:
  1. `jwt.New(opts...)`, with the options:
     - `WithIssuer` and `WithAudience`, both required;
     - exactly one of `WithJWKS(url)` and one or more `WithKey(kid, alg, key)`;
     - `WithHTTPClient`, `WithLeeway` (default 1 min, go-jose's default),
       `WithRefreshInterval` (default 30 s) and `WithType`.
  2. `WithKey` checks the key type against the algorithm:
     - `*rsa.PublicKey` of at least 2,048 bits for RS256 and PS256;
     - `*ecdsa.PublicKey` on P-256 for ES256;
     - `ed25519.PublicKey` for EdDSA;
     - a `[]byte` of at least 32 bytes for HS256.
  3. Shape: a Bearer credential with 2 dots is a candidate; one with 4 dots is a JWE, rejected as
     invalid; any other is `ErrNoCredentials`.
  4. `jwt.ParseSigned(token, [RS256, PS256, ES256, EdDSA, HS256])`. A header with `crit` is
     rejected. `typ` is checked when configured: the `application/` prefix is stripped and the
     comparison ignores case.
  5. Key selection, among keys bound to the header's `alg`:
     - with a `kid`, the keys with that `kid`;
     - without one, all of them.

     Exactly one candidate is required; zero is "unknown" (R-18), and more than one is invalid.
  6. `tok.Claims(key, &std, &all)`. Then:
     - `exp` must be present;
     - `ValidateWithLeeway(Expected{Issuer, AnyAudience: {aud}, Time: time.Now()}, leeway)`;
     - `sub` must not be empty;
     - `amr` must be absent or a list of strings.
  7. The result is `&auth.Principal{Subject: sub, Methods: amr, Extra: claims{all}}`.
     `jwt.Claims(p)` returns `all`.

  Every failure in steps 4 to 6 is `ErrInvalidCredentials` and produces the challenge
  `Bearer error="invalid_token"`. The challenge has no description, so it does not reveal why
  the token was rejected (FR-068).
- **Rationale**:
  - Each step adds only what go-jose does not do, as the request asks.
  - Measured verification costs, parse plus signature plus claims:

    | Algorithm | Time | Allocations |
    |-----------|------|-------------|
    | RS256 | 34.5 µs | 7.1 kB in 110 allocations |
    | EdDSA | 48.7 µs | 5.4 kB in 99 allocations |
    | ES256 | 66.8 µs | 6.7 kB in 121 allocations |

    All are far below 1% of a password check, 170 µs (SC-009).
- **Alternatives rejected**:
  - **Exposing go-jose types such as `*jose.JSONWebKeySet` in options**: our API would break
    with go-jose's next major version.
  - **Inferring the algorithm of a static RSA key**: RS256 and PS256 share key types, so static
    keys always state their algorithm.

## R-18 Key sets: download and cache

- **Decision**:
  - `WithJWKS` requires an `https` URL with a host. Nothing is downloaded at construction.
  - The cache is guarded by a mutex. It holds:
    - the bound keys and the instant they expire;
    - the instant and error of the last download attempt;
    - an in-flight channel.
  - Downloads run in the goroutine of the request that triggers them:
    - the context is `context.WithoutCancel(ctx)` with a 10-second timeout, so that one client
      disconnecting does not fail the others;
    - concurrent callers wait on the channel or on their own context;
    - there is no background goroutine.
  - Request: `GET` with `Accept: application/jwk-set+json, application/json`. Only status 200
    is accepted. The body is read up to 1 MiB, and a larger body is an error.
  - Parsing: `{"keys":[…]}` is decoded as raw messages, then each key on its own. A key is
    skipped when:
    - it fails to parse;
    - it is not public (symmetric or private keys);
    - its `use` is set to anything other than `sig`;
    - it is an RSA key under 2,048 bits, or an EC key on a curve other than P-256;
    - its declared `alg` is not allowed for its type.

    Keys without `alg` are bound to RS256 (RSA), ES256 (P-256) or EdDSA (Ed25519). A document
    with no usable key is an error.
  - Lifetime: the `max-age` from `Cache-Control`, with `no-cache` or `no-store` meaning the
    minimum. The result is clamped to between 1 minute and 24 hours. Without `max-age` it is
    1 hour.
  - Refresh: a download happens when the cache is empty or expired, or when a `kid` is unknown,
    and in every case at most once per refresh interval, counting failed attempts too. Results:
    - when the download fails, the previous keys stay in use;
    - an unknown `kid` that cannot be resolved because the last download failed is an error
      (500);
    - an unknown `kid` within the interval after a successful download is
      `ErrInvalidCredentials`.
- **Rationale**:
  - "At most once per interval", counting failures, bounds the load on the identity provider
    both under forged `kid`s and during its outages (FR-063). 30 s also bounds the delay
    before a newly rotated key is accepted (spec edge case).
  - Keeping the previous keys during an outage, and answering 500 when the decision depends on
    the unreachable server, both follow FR-064.
  - The minimum lifetime keeps `no-cache` from turning every request into a download.
- **Alternatives rejected**:
  - **Refetching on every unknown `kid`, as `go-oidc` does**: forged `kid`s become load on the
    key server.
  - **A background refresh goroutine**: forbidden by FR-005, and it would need a `Close`.
  - **Honoring `Expires` and `Age`**: `max-age` is what identity providers send, and the bounds
    make the rest irrelevant.

## R-19 Secrets in logs and errors

- **Decision**: three exported types carry secrets: `apikey.Key`, `*totp.Enrollment` and
  `*recovery.Codes`. Each implements `slog.LogValuer` and `fmt.Stringer`, both returning
  `[REDACTED]`. The secret is available only through explicit accessors: `string(key)`,
  `Secret()` and `URI()`, and `Plain()`.
  - Session and pending identifiers never leave the cookie and the module's internals.
  - Error messages name the operation and wrap the cause. They never include usernames,
    passwords, codes, keys, tokens, identifiers, digests or hashes.
- **Rationale**: FR-008. `Key` deliberately has no `String` method, so that it can be printed
  for the user; its `LogValue` covers structured logs.
- **Alternatives rejected**:
  - **Depending on `slogx` for its `Secret[T]`**: a third dependency, for three short methods.

## R-20 Testing approach (Principle III)

- **Decision**:
  - The standard `testing` package only, with external test packages per package. Internal
    tests cover the HOTP function, PHC parsing bounds, the memstore sweep and the JWKS parser.
  - Time: `testing/synctest` for the memstore, sessions, pending logins, TOTP steps, token
    expiry and leeway, and the key set cache.
  - Key sets: an `http.Client` whose `Transport` is an in-memory `RoundTripper`. It serves
    chosen bodies, `Cache-Control` headers, statuses and failures, and counts requests. There is
    no network.
  - Concurrency: `sync.WaitGroup.Go` with 50 goroutines, under `-race`, for:
    - `memstore.Incr` atomicity;
    - the password attempt limit;
    - the second-factor budget;
    - TOTP replay;
    - recovery reuse;
    - concurrent key set downloads.
  - Vectors: RFC 4226 Appendix D, with 6-digit HOTP, and RFC 6238 Appendix B, with SHA-1,
    SHA-256 and SHA-512 and 8 digits.
  - Fuzz targets: `FuzzBasicHeader`, `FuzzSessionCookie`, `FuzzTOTPCode`, `FuzzRecoveryCode`,
    `FuzzPHC`, `FuzzAPIKey`, `FuzzToken`, `FuzzKeySet` and `FuzzCacheControl`. Each asserts no
    panic and the invariants of its parser.
  - Benchmarks: `BenchmarkCheck` (existing user, missing user, wrong password),
    `BenchmarkSessionAuthenticate`, `BenchmarkAPIKeyAuthenticate` and `BenchmarkJWTAuthenticate`
    for RS256, ES256 and EdDSA.
  - SC-001 is verified in two ways:
    - an internal test asserts that the dummy hash has exactly the checker's parameters;
    - `BenchmarkCheck` compares the missing-user and wrong-password cases.
  - argon2id in tests:
    - the tests of `password` and the integration test use the floor parameters (7 MiB, t=5:
      14 ms, or 22 ms with `-race`);
    - `basic` tests use a stub checker;
    - examples use the defaults.
  - Keys and tokens are generated at test time, so that no private key or token literal is
    committed (Gitleaks).
  - Every primary API has an `Example`.
  - Coverage target: ≥ 80% for the module.
- **Rationale**: the repository's hermeticity rules, and the request's list of correctness
  requirements, which maps one to one onto the tests above.
- **Alternatives rejected**:
  - **`httptest.Server` for key sets**: it opens a real socket.
  - **Wall-clock timing assertions for SC-001**: they are flaky. Equal work plus a benchmark
    comparison is deterministic.

## R-21 Lint and security scanners

- **Spike results** with the repository configuration:
  - **golangci-lint**: one finding, `errcheck` on `defer resp.Body.Close()`. The fetch closes
    the body in a deferred function that joins the close error into the returned error.
    `var ErrNotFound = auth.ErrNotFound` passes `gochecknoglobals`.
  - **Bearer 2.1.1**: one MEDIUM finding, `go_gosec_blocklist_sha1`, on the `crypto/sha1`
    import of the HOTP code, which lives in `internal/hotp/hotp.go` (R-14). It is suppressed with
    `bearer ignore add <fingerprint> --comment …`. The justification: RFC 6238 requires
    HMAC-SHA-1 for compatibility with authenticator apps, and HMAC-SHA-1 does not depend on
    SHA-1's collision resistance (RFC 6194).
    - Only that file imports `crypto/sha1`. Tests select SHA-1 through the package's algorithm
      constant, so no test file needs the import.
    - SHA-256 for API keys and recovery codes, and the cookie code, produced no finding.
  - **Semgrep**, with the pinned image and the `p/golang` and `p/trailofbits` rulesets: 0
    findings.
  - **Trivy**: 0 HIGH and 0 CRITICAL (R-02).
- **Decision**:
  - The Crockford encoding is created by a function over a `const` alphabet, not by a
    package-level variable (`gochecknoglobals`).
  - Expected `nolint` directives are none. If `contextcheck` flags the detached download context
    of R-18, one `//nolint:contextcheck` with its reason is allowed.
- **Rationale**: a scanner finding may be suppressed only through the tool's own mechanism, with
  a justification (constitution, Finding suppression).

## R-22 Documentation

- **Decision**:
  - The README covers:
    - purpose, `go get` and the minimum Go version;
    - a full example: `Any(sessions, keys, tokens)`, login, second factor and logout
      handlers, and `RequireMethods`;
    - the Redis adapter (R-08);
    - CSRF protection with `http.CrossOriginProtection`, wrapped around the routes that use
      sessions;
    - Basic: the CPU and memory cost of each check, the missing second factor, and the advice
      to require "mfa" on sensitive routes;
    - middleware order: CORS before `auth` so that preflights are answered,
      `ratelimit` and `maxconcurrency` before `auth`, around the routes that check passwords;
    - argon2id parameters and how to raise them;
    - canonical usernames for the attempt limit, and the lockout trade-off;
    - the legacy-hash timing caveat, `localhost` cookies and key rotation delay.
  - Package documentation covers each method's challenges and statuses. A CHANGELOG starts
    `[0.1.0]`.
- **Rationale**: FR-010 and the edge cases that the spec assigns to documentation.

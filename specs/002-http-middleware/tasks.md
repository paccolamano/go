---

description: "Task list for the reusable HTTP middleware module"
---

# Tasks: Reusable HTTP Middleware Module

**Input**: Design documents from `specs/002-http-middleware/`

**Prerequisites**: [plan.md](plan.md), [spec.md](spec.md), [research.md](research.md),
[data-model.md](data-model.md), [contracts/](contracts/), [quickstart.md](quickstart.md)

**Tests**: REQUIRED. Constitution Principle III is non-negotiable:

- Tests are written first and observed failing.
- Coverage must be ≥ 80%, with `-race -shuffle=on`.
- Parsers of external input need fuzz tests.
- Performance-sensitive paths need benchmarks.
- Every primary API needs an `Example`.

Every test task below precedes the implementation it covers.

**Organization**: tasks are grouped by user story (spec.md US1–US5). Every path is relative to
the repository root. All Go code lives in the single module `middleware/`, whose only `go.mod`
is `middleware/go.mod`. Each middleware is a package in its own folder, without a `go.mod`.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: can run in parallel with other [P] tasks: different files, and no dependency on an
  incomplete task.
- **[Story]**: US1–US5 from spec.md. Setup, Foundational and Polish tasks have no story label.

## Conventions for every task

- **Test names**:
  - `Test<Feature>_<Scenario>`, e.g. `TestChain_Order`, `TestReject_FallbackStatus`,
    `TestIntegration_PanicIsLoggedAndServed`;
  - fuzz targets use the exact names in [quickstart.md §4](quickstart.md);
  - benchmarks use `b.Loop()`.
- **Test style**:
  - table-driven with `t.Run`;
  - requests built with `httptest.NewRequestWithContext(t.Context(), …)` (`httptest.NewRequest`
    is forbidden by the `noctx` linter);
  - time-dependent tests use `testing/synctest` (`synctest.Test`), never real sleeps;
  - connection-level tests use `httptest.NewServer` on loopback, closed with `t.Cleanup`.
- **Configuration errors**: `New` returns `(nil, err)` with
  `errors.Is(err, middleware.ErrInvalidConfig)`. The message format is
  `"middleware: invalid configuration: <pkg>: <Setting>: <reason>"`, built as
  `fmt.Errorf("%w: <pkg>: <Setting>: <reason>", middleware.ErrInvalidConfig)`.
- **Options**: `type Option func(*config)` with an unexported `config`. Defaults are applied
  first, then the options in order (for the same setting the last one wins). `New` validates
  everything once, after all options have been applied (research R-04).
- **Refusals**: every refusal goes through `respwriter.Reject` (T009), with the package's
  sentinel ([contracts/rejection.md](contracts/rejection.md)).
- **Documentation**: every package has a package doc comment that covers FR-012: its mandatory
  settings, every option with its default, its recommended chain position (research R-21) and
  its interactions with the other middleware. Every exported identifier is documented.
- **Lint exceptions**: `//nolint` is allowed only as `//nolint:forbidigo // <reason>` on the
  three justified `panic` sites (chain nil checks, recovery re-panics) and as
  `//nolint:wrapcheck // io contract` on pass-through I/O methods.

---

## Phase 1: Setup (Shared Infrastructure)

**Purpose**: create the single module and register it with the repository tooling.

- [X] T001 Create `middleware/go.mod` containing exactly `module github.com/paccolamano/go/middleware` and `go 1.26.0`, with no `require` and no `replace`. This is the module's only `go.mod`; no subfolder may ever get one.
- [X] T002 [P] Create `middleware/CHANGELOG.md` in Keep a Changelog format with an empty `## [Unreleased]` section (entries are added in T081).
- [X] T003 [P] Create `middleware/README.md` with:
  - the purpose ("reusable, option-configured HTTP server middleware");
  - `go get github.com/paccolamano/go/middleware`;
  - "Minimum Go version: 1.26";
  - placeholder headings "Usage" and "Recommended order", completed in T080.
- [X] T004 Run `make work` from the repository root so `go.work` lists `./middleware`. Then confirm that `find middleware -name go.mod` prints only `middleware/go.mod` and that `scripts/structure-check.sh middleware` reports PASS (depends on T001–T003).

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: the root types and the internal packages that every user story builds on.

**⚠️ CRITICAL**: no user story work can begin until this phase is complete.

- [X] T005 Create `middleware/doc.go`, holding a short package doc comment for `package middleware` (expanded in T076). Create `middleware/middleware.go`, declaring exactly as in [contracts/public-api.md](contracts/public-api.md#middleware-root):
  - `type Middleware = func(http.Handler) http.Handler` (an alias);
  - `type Rejection struct { Status int; Err error }`;
  - `type RejectFunc func(w http.ResponseWriter, r *http.Request, rej Rejection)`;
  - `var ErrInvalidConfig = errors.New("middleware: invalid configuration")`;
  - doc comments stating that fields may be added to `Rejection` in minor releases.
  These are declarations only; their behavior is covered by T008 and T014.
- [X] T006 [P] Write failing tests in `middleware/internal/httpheader/token_test.go` and the fuzz target `FuzzSplitList` in `middleware/internal/httpheader/fuzz_test.go`, covering:
  - `ValidToken(s string) bool` (RFC 9110 tchar, non-empty);
  - `ValidValue(s string) bool` (rejects CR, LF, NUL);
  - `SplitList(values []string) []string`: comma-separated items across multiple header lines, OWS trimmed, empty items dropped, order preserved;
  - `HasToken(h http.Header, name, token string) bool`, case-insensitive;
  - `AddVary(h http.Header, token string)`, which adds the token only if it is not already present in any `Vary` value.
- [X] T007 Implement `middleware/internal/httpheader/token.go` until T006 passes. Add a package doc comment ("HTTP header field syntax helpers").
- [X] T008 [P] Write failing tests in `middleware/internal/respwriter/respwriter_test.go`.
  - Observer behavior, quoting data-model.md:
    - "WriteHeader(code): 100–199 except 101 → forwarded, nothing recorded";
    - "otherwise → status = code, started = true (first call only)";
    - "Write(p): if !started → status = 200, started = true; bytes += n";
    - "Hijack(): hijacked = true, started = true; status = 101 if 0".
  - Capabilities: `Unwrap()` returns the underlying writer. On a loopback `httptest.NewServer`:
    - `Flush()` delivers data to the client before the handler returns;
    - `http.NewResponseController(w).SetWriteDeadline` reaches the server through `Unwrap`;
    - `Hijack()` works through the wrapper.
    On an `httptest.ResponseRecorder`, `Hijack()` returns an error that matches `http.ErrNotSupported`.
  - `TestReject_*` cases for `Reject(w, r, fn, rej)`:
    1. With a nil `fn`, the response is `http.Error(w, http.StatusText(rej.Status), rej.Status)`: plain text with `X-Content-Type-Options: nosniff`.
    2. A custom `fn` receives `rej` unchanged, and its own status and body are sent.
    3. A custom `fn` that writes nothing still yields `rej.Status` with an empty body.
    4. A custom `fn` that only sets headers still yields `rej.Status`, with those headers.
- [X] T009 Implement `middleware/internal/respwriter/respwriter.go` and `middleware/internal/respwriter/reject.go` until T008 passes.
  - `respwriter.go`: `type Writer` with `Status() int`, `Bytes() int64`, `Started() bool`, `Hijacked() bool`, `Unwrap()`, and `Flush()`/`Hijack()` delegating via `http.ResponseController`.
  - `reject.go`: `func Reject(w http.ResponseWriter, r *http.Request, fn middleware.RejectFunc, rej middleware.Rejection)`, which wraps `w` in an observer only on this path and writes `rej.Status` when `fn` wrote nothing.
  - Pass-through `Write`/`Flush` errors carry `//nolint:wrapcheck // io contract`.
- [X] T010 [P] Write failing tests in `middleware/internal/netaddr/netaddr_test.go`:
  - `ParseRemoteAddr(s string) (netip.Addr, bool)`:
    - `"1.2.3.4:5"` → 1.2.3.4;
    - `"[::1]:80"` → ::1;
    - `"[fe80::1%eth0]:1"` → the zone is stripped;
    - `"[::ffff:1.2.3.4]:9"` → unmapped to 1.2.3.4;
    - `""` and `"@"` → `false`.
  - `Key(a netip.Addr) string`: IPv4 → the address string; IPv6 → the string of its `/64` prefix, so two addresses in the same /64 share a key.
- [X] T011 Implement `middleware/internal/netaddr/netaddr.go` until T010 passes.
- [X] T012 [P] Write failing tests in `middleware/internal/mediatype/mediatype_test.go` and the fuzz target `FuzzMediaTypeMatch` in `middleware/internal/mediatype/fuzz_test.go`.
  - `ParsePatterns(patterns []string) (Matcher, error)` accepts `type/subtype`, `type/*`, `type/*+suffix` and `*/*`, with lowercase tokens only, and rejects anything else.
  - `Matcher.Match(contentType string) bool` parses with `mime.ParseMediaType`, ignores parameters, is case-insensitive, and returns false for an empty or unparseable value.
  - The fuzz target asserts: no panic, and `Match` never returns true for an unparseable input.
- [X] T013 Implement `middleware/internal/mediatype/mediatype.go` until T012 passes.

**Checkpoint**: the foundation is ready. User stories can now begin.

---

## Phase 3: User Story 1 - Compose a request pipeline in a chosen order (Priority: P1) 🎯 MVP

**Goal**: an immutable, ordered chain plus the shared conventions: options, configuration errors
and the refusal standard (the conventions are delivered by T005–T009).

**Independent Test**: build a chain from three test middleware that record when they run, and
check the request and response order. Extend the chain and check that the original is
unchanged. Check the panics on nil. The refusal standard (scenarios 8–9) is covered at helper
level by T008. Its run across all six real middleware is T066.

### Tests for User Story 1 ⚠️ (write first, see them fail)

- [X] T014 [P] [US1] Write failing tests in `middleware/chain_test.go` (package `middleware_test`):
  1. `TestChain_Order`: request A→B→C→H, response unwinds C→B→A (FR-013).
  2. `TestChain_Empty`: an empty or zero `Chain` returns the same `h` (FR-014).
  3. `TestChain_AppendDoesNotAlias`: two `Append`s on the same base give independent chains, and the base is unchanged (FR-015).
  4. `TestChain_Extend`: order is c's middleware, then other's.
  5. `TestChain_ThenBuildsOnce`: each middleware func runs once per `Then`, never per request.
  6. `TestChain_NilPanics`: `New(nil)` panics with `middleware: nil middleware at position N`; `Append(nil)` panics; `Then(nil)` panics with `middleware: nil handler` (FR-016).
  7. `TestChain_ThirdPartyFuncs`: a plain `func(http.Handler) http.Handler`, and a `[]func(http.Handler) http.Handler` passed with `...`, are accepted (scenario 7).

### Implementation for User Story 1

- [X] T015 [US1] Implement `middleware/chain.go` until T014 passes.
  - `type Chain struct{ mws []Middleware }`.
  - `New` copies its input with `slices.Clone`.
  - `Append` uses `slices.Concat`, so it always gets a new backing array.
  - `Extend` returns a new chain.
  - `Then` wraps from the last middleware to the first and returns `h` itself when the chain is empty.
  - The nil checks panic with the exact messages above and carry `//nolint:forbidigo // programming error; mirrors net/http.ServeMux.Handle (research R-03)`.
- [X] T016 [P] [US1] Write `ExampleNew`, `ExampleChain_Append` and `ExampleChain_Then` in `middleware/example_test.go`, with deterministic `// Output:` comments produced by small recording middleware.

**Checkpoint**: the chain works and is tested on its own. The MVP is deliverable.

---

## Phase 4: User Story 2 - Keep the service observable and resilient (Priority: P1)

**Goal**: requestid, recovery, accesslog and realip, which every production service needs.

**Independent Test**: serve realip → requestid → accesslog → recovery with a capturing
`slog.Handler`, a handler that panics on one path, trusted and untrusted peers, and forged
`X-Forwarded-For` headers (T031).

### Tests for User Story 2 ⚠️

- [X] T017 [P] [US2] Write failing tests in `middleware/requestid/requestid_test.go`.
  - Options (data-model.md): `WithHeader` requires a "valid token"; `WithGenerator` must be "non-nil"; each invalid case gives `ErrInvalidConfig`.
  - The default header is `X-Request-ID`, and incoming identifiers are trusted by default.
  - A valid incoming identifier is reused. A valid identifier is "a string of 1 to 128 bytes, each byte in 0x21 to 0x7E".
  - Invalid incoming identifiers are replaced: empty, 129 bytes, containing a space, a CR or a byte ≥ 0x7F.
  - Only the first header value is considered.
  - `WithTrustIncoming(false)` always generates a new identifier.
  - `WithGenerator` output is used when it is valid; otherwise `rand.Text()` is used (26 characters).
  - The response header is already set when `next` runs.
  - `FromContext` returns `(id, true)` inside the handler and `("", false)` on a bare context (FR-038–FR-042).
- [X] T018 [P] [US2] Write the fuzz target `FuzzRequestID` in `middleware/requestid/fuzz_test.go`. For any incoming header value, there is no panic, and the identifier exposed to the handler and in the response always satisfies the 1–128 bytes, 0x21–0x7E rule.
- [X] T019 [P] [US2] Write failing tests in `middleware/recovery/recovery_test.go`.
  - Options: logger "non-nil"; `WithReporter` "non-nil"; `WithRejectFunc` "non-nil".
  - **Panic before writing** gives a default plain-text 500 (FR-034):
    - `Content-Length`, `Content-Type`, `Content-Encoding`, `Content-Range`, `ETag`, `Last-Modified`, `Content-Disposition` and `Trailer` set by the handler are removed;
    - `Cache-Control: no-store` is set;
    - a header set by an outer middleware (`X-Request-ID`) is kept;
    - the body never contains the panic value (FR-011).
  - **Log record** per [contracts/log-records.md](contracts/log-records.md#recovery): message `panic recovered`, `slog.LevelError`, keys `panic`, `stack` (containing the panicking function's name), `method`, `path`, `request_id` (when requestid ran) and `response_started=false`.
  - `WithReporter` is called after logging, with the value and the stack.
  - `WithRejectFunc` receives `Rejection{Status: 500, Err}` with `errors.Is(Err, recovery.ErrPanic)`.
  - `panic(http.ErrAbortHandler)` is re-raised unchanged and not logged (FR-037).
  - `panic(nil)` is recovered.
  - **Loopback server**: a handler panics after flushing its first bytes. The client sees a truncated or reset response, the record has `response_started=true` (FR-035), and the next request succeeds.
- [X] T020 [P] [US2] Write failing tests in `middleware/accesslog/accesslog_test.go` with a capturing `slog.Handler`.
  - Options: logger "non-nil"; `WithLevel`, `WithAttrs` and `WithSkip` "non-nil"; `WithHeaders` "valid tokens, at least one name".
  - Exactly one record `http request` per request, after the response completes. Its keys follow [contracts/log-records.md](contracts/log-records.md#accesslog):
    - `method`, `path`, `proto`, `status`, `bytes`, `duration`;
    - `client`: the IP without the port, or the raw `RemoteAddr` when it cannot be parsed;
    - `user_agent`, only when the header is present;
    - `request_id`, only when requestid ran earlier.
  - The query string is omitted by default and present with `WithQuery()` when it is non-empty.
  - With `WithHeaders`, a group `header` holds the canonical names, with multiple values joined by `", "`.
  - Status values:
    - status 200 when the handler wrote without `WriteHeader`;
    - 200 and 0 bytes when it wrote nothing;
    - a 103 response is not recorded as the status;
    - a hijack gives status 101.
  - Panic: a record with `panic=true` and status 500, and the panic still propagates (FR-024).
  - Options at runtime: `WithLevel` sets the level; `WithAttrs` attributes come last; `WithSkip` suppresses the record; the callbacks receive the correct `Result`.
  - With a disabled level, no record is produced.
- [X] T021 [P] [US2] Write failing tests in `middleware/realip/realip_test.go`.
  - Options (data-model.md):
    - trust "non-zero. `TrustRanges`: each entry is a CIDR or a single IP, at least one. `TrustHops`: n ≥ 1";
    - `WithHeader` "valid token; `Forwarded` selects RFC 7239 parsing";
    - the invalid cases (a zero `Trust`, a bad CIDR, `TrustHops(0)`, a bad name) give `ErrInvalidConfig`.
  - Behavior, every row of [contracts/http-behavior.md](contracts/http-behavior.md#realip):
    - untrusted peer → unchanged;
    - `XFF: forged, client, proxy2` with proxy2 trusted → `RemoteAddr == "client:0"`;
    - every entry trusted → leftmost;
    - hops n=2 with `forged, client, cdn` → client;
    - fewer entries than hops → unchanged;
    - a malformed entry on the walk → unchanged;
    - several XFF lines → read as one list, in order.
  - `Forwarded: for="[2001:db8::1]:4711"` → `[2001:db8::1]:0`; `for=unknown` and `for=_hidden` → unchanged.
  - With `WithHeader("X-Real-IP")`, a single address is used.
  - An IPv4-mapped address is unmapped.
  - `PeerFromContext` returns the original `RemoteAddr` after a rewrite, and `false` when nothing was rewritten (FR-047–FR-052).
- [X] T022 [P] [US2] Write the fuzz targets `FuzzForwardedFor` and `FuzzForwarded` in `middleware/realip/fuzz_test.go`. They assert: no panic; any rewritten `RemoteAddr` parses with `netip.ParseAddrPort` and has port 0; with an untrusted peer, nothing is ever rewritten.

### Implementation for User Story 2

- [X] T023 [US2] Implement `middleware/requestid/options.go` and `middleware/requestid/requestid.go` until T017 and T018 pass, following research R-12:
  - the context key is the unexported `type ctxKey struct{}`;
  - `FromContext(ctx) (string, bool)`;
  - the header is set before `next` runs.
- [X] T024 [US2] Implement `middleware/recovery/options.go` and `middleware/recovery/recovery.go` until T019 passes, following research R-11:
  - `var ErrPanic = errors.New("recovery: handler panicked")`;
  - a `respwriter.Writer` observer;
  - `requestid.FromContext` for `request_id`;
  - `respwriter.Reject` with `Rejection{500, ErrPanic}` when the response has not started; otherwise `panic(http.ErrAbortHandler)`;
  - both re-panics carry `//nolint:forbidigo // net/http abort protocol (research R-11)`.
  Depends on T023.
- [X] T025 [US2] Implement `middleware/accesslog/options.go` and `middleware/accesslog/accesslog.go` until T020 passes, following research R-09:
  - the exported `Result{Status int; Bytes int64; Duration time.Duration; Panicked bool}`;
  - a deferred emission with a `completed` flag and no `recover()`;
  - `Logger.Enabled` checked before building attributes;
  - `LogAttrs(r.Context(), …)`;
  - `netaddr.ParseRemoteAddr` for `client`;
  - `requestid.FromContext` for `request_id`.
  Depends on T023.
- [X] T026 [US2] Implement `middleware/realip/parse.go` (X-Forwarded-For list, RFC 7239 `for=` parsing with quotes, brackets and ports, `unknown`/obfuscated treated as malformed, `netip` normalization with `Unmap()` and zone stripping), `middleware/realip/options.go` (`Trust`, `TrustRanges`, `TrustHops`, `WithHeader`) and `middleware/realip/realip.go` (rightmost-untrusted and hop-count algorithms, `r.WithContext` copy, `RemoteAddr = netip.AddrPortFrom(client, 0).String()`, `PeerFromContext`), until T021 and T022 pass.
- [X] T027 [P] [US2] Write runnable examples in `middleware/requestid/example_test.go` (`ExampleNew`, `ExampleFromContext`).
- [X] T028 [P] [US2] Write runnable examples in `middleware/recovery/example_test.go` (`ExampleNew`, `ExampleWithRejectFunc`).
- [X] T029 [P] [US2] Write runnable examples in `middleware/accesslog/example_test.go` (`ExampleNew` with a text handler without time, `ExampleWithSkip`) and `BenchmarkAccessLog` (discard handler) in `middleware/accesslog/bench_test.go`.
- [X] T030 [P] [US2] Write runnable examples in `middleware/realip/example_test.go` (`ExampleNew` with `TrustRanges("10.0.0.0/8")`, and `ExampleTrustHops`).
- [X] T031 [US2] Write `TestIntegration_*` in `middleware/integration_test.go` (package `middleware_test`), covering US2 scenarios 1–7 end to end through `middleware.New(realip, requestid, accesslog, recovery).Then(h)` (depends on T015 and T023–T026):
  - a panic gives a 500, with access and recovery records sharing the `request_id`, and the next request succeeds;
  - a panic after the first byte gives a truncated response (loopback);
  - the identifier is generated, reused or replaced;
  - a trusted proxy chain resolves the client, and the access log `client` shows the resolved IP;
  - a forged XFF from an untrusted peer is ignored.

**Checkpoint**: US1 and US2 work together, each tested on its own.

---

## Phase 5: User Story 3 - Harden the HTTP surface (Priority: P2)

**Goal**: secureheaders (CSP with report-only and nonce, 9 default headers), cors, bodylimit and
contenttype.

**Independent Test**: apply each middleware alone to a handler that records whether it was
invoked, then send compliant and non-compliant requests (US3 scenarios 1–7).

### Tests for User Story 3 ⚠️

- [X] T032 [P] [US3] Write failing tests in `middleware/secureheaders/secureheaders_test.go`.
  - **Defaults**: exactly these 9 headers, and no `Strict-Transport-Security` or `Content-Security-Policy-Report-Only`:
    - `Content-Security-Policy: default-src 'none'; frame-ancestors 'none'`
    - `Cross-Origin-Embedder-Policy: require-corp`
    - `Cross-Origin-Opener-Policy: same-origin`
    - `Cross-Origin-Resource-Policy: same-origin`
    - `Permissions-Policy: accelerometer=(), camera=(), display-capture=(), geolocation=(), gyroscope=(), hid=(), magnetometer=(), microphone=(), midi=(), payment=(), serial=(), usb=(), xr-spatial-tracking=()`
    - `Referrer-Policy: no-referrer`
    - `X-Content-Type-Options: nosniff`
    - `X-Frame-Options: DENY`
    - `X-Permitted-Cross-Domain-Policies: none`
  - **Option validation** (data-model.md), each failure giving `ErrInvalidConfig`:
    - `WithHeader`: "name is a token and is not `Content-Security-Policy`, `Content-Security-Policy-Report-Only` or `Strict-Transport-Security`; value has no CR, LF or NUL";
    - `WithoutHeader`: "name is a token; removing an absent header is a no-op";
    - `WithHSTS`: "`MaxAge ≥ 1s`; `Preload` requires `IncludeSubDomains` and `MaxAge ≥ 365 × 24h`";
    - `WithCSP` and `WithCSPReportOnly`: "non-empty; no CR, LF or NUL; every `;`-separated directive starts with `[a-z-]+`";
    - `WithCSPNonce`: "at least one of the policies contains `{nonce}`";
    - the cross-check: "a policy containing `{nonce}` requires `WithCSPNonce()`".
  - **Header behavior**:
    - `WithHSTS` renders `max-age=<seconds>[; includeSubDomains][; preload]`;
    - `WithoutHeader("Content-Security-Policy")` removes the CSP;
    - report-only and enforced policies coexist (scenario 7).
  - **Nonce**: two requests get different 24-character standard-base64 nonces, substituted for every `{nonce}` in both policies, and `NonceFromContext` equals the nonce in that response's header (scenario 6, FR-044).
  - **Handler interplay**: a handler that sets or deletes a header wins (FR-046). A handler calling `Header().Add` on one response does not affect the next response.
- [X] T033 [P] [US3] Write failing tests in `middleware/cors/cors_test.go`.
  - **Option validation** (data-model.md):
    - origins: "each entry is an exact origin, `"*"` (alone), a `scheme://*.domain[:port]` pattern, or `"null"`; normalized to lowercase; no path, query or fragment";
    - an empty list without `WithOriginFunc` is an error;
    - `WithOriginFunc`: "non-nil; not combined with `"*"`";
    - `WithMethods`: "valid tokens, at least one";
    - `WithHeaders`: "valid tokens, or a single `"*"`";
    - `WithExposedHeaders`: "valid tokens";
    - `WithCredentials`: "not with `"*"` (FR-032)";
    - `WithMaxAge`: "≥ 0; 0 omits the header; sent in whole seconds".
  - **Behavior**: every row of [contracts/http-behavior.md](contracts/http-behavior.md#cors).
    - In list mode, `Vary: Origin` is on every response.
    - An allowed simple request gets the grant headers and `next` runs; a disallowed origin gets no grant headers and `next` runs.
    - In `"*"` mode, every response gets `Access-Control-Allow-Origin: *` and no `Vary: Origin`.
    - An allowed preflight gets 204 with Allow-Methods, Allow-Headers and `Max-Age: 600`, plus `Vary: Origin, Access-Control-Request-Method, Access-Control-Request-Headers`, and `next` is not invoked. A disallowed preflight gets 204 with only `Vary`.
    - OPTIONS without `Access-Control-Request-Method` is an ordinary request.
    - `null` matches only an explicit entry.
    - The subdomain pattern does not match the apex domain.
    - With `WithHeaders("*")`, the requested headers are echoed.
    - CORS-safelisted methods always pass.
- [X] T034 [P] [US3] Write the fuzz target `FuzzOrigin` in `middleware/cors/fuzz_test.go`. It asserts:
  - parsing and matching never panic;
  - normalization is idempotent;
  - `https://*.example.com` never matches `https://example.com`, nor any other scheme or port.
- [X] T035 [P] [US3] Write failing tests in `middleware/bodylimit/bodylimit_test.go`.
  - Options: `maxBytes` "> 0"; `WithRejectFunc` "non-nil".
  - A declared `ContentLength > max` gives a default plain-text 413. A custom `RejectFunc` receives `Rejection{413, Err}` with `errors.Is(Err, bodylimit.ErrTooLarge)`. `next` is not invoked and the body is not read: assert it with a counting body.
  - A chunked body over the limit makes the handler's `Read` return `*http.MaxBytesError` (`errors.As`).
  - A body within the limit, or no body at all (`http.NoBody`), passes unchanged (FR-017–FR-020).
- [X] T036 [P] [US3] Write failing tests in `middleware/contenttype/contenttype_test.go`.
  - Options: allowed is "at least one; each `type/subtype`, `type/*` or `*/*`, lowercase tokens"; `WithRejectFunc` "non-nil".
  - Body detection is "`ContentLength > 0`, or `ContentLength == -1` and `Body != http.NoBody`".
  - A missing, unparseable or disallowed type gives 415, with `Rejection{415, Err}` and `errors.Is(Err, contenttype.ErrUnsupported)`.
  - `application/json; charset=utf-8` passes, and `APPLICATION/JSON` passes too.
  - The `text/*` wildcard works.
  - A GET without a body passes regardless of its `Content-Type` (FR-079–FR-081).

### Implementation for User Story 3

- [X] T037 [US3] Implement `middleware/secureheaders/options.go` and `middleware/secureheaders/secureheaders.go` until T032 passes, following research R-13:
  - `HSTS`, `WithHeader`, `WithoutHeader`, `WithHSTS`, `WithCSP`, `WithCSPReportOnly`, `WithCSPNonce` and `NonceFromContext`;
  - the nonce is 16 bytes from `crypto/rand`, base64 std encoded, and the request is copied with `r.WithContext` only when the nonce is enabled;
  - the header value slices are allocated per request;
  - the package doc warns that nonce pages must not be served from shared caches, and explains how to relax COEP (`credentialless` or `WithoutHeader`) for HTML pages that embed cross-origin resources.
- [X] T038 [US3] Implement `middleware/cors/origin.go` (origin parsing, lowercase normalization, exact, pattern and `null` matching) and `middleware/cors/options.go` and `middleware/cors/cors.go` (header values precomputed at construction, the Vary rules, the preflight answered with 204) until T033 and T034 pass, following research R-10.
- [X] T039 [US3] Implement `middleware/bodylimit/options.go` and `middleware/bodylimit/bodylimit.go` until T035 passes, following research R-08:
  - `var ErrTooLarge = errors.New("bodylimit: request body too large")`;
  - `respwriter.Reject`;
  - `http.MaxBytesReader` for undeclared sizes.
- [X] T040 [US3] Implement `middleware/contenttype/options.go` and `middleware/contenttype/contenttype.go` until T036 passes, following research R-20:
  - `var ErrUnsupported = errors.New("contenttype: unsupported media type")`;
  - `internal/mediatype`;
  - `respwriter.Reject`.
- [X] T041 [P] [US3] Write runnable examples in `middleware/secureheaders/example_test.go`: `ExampleNew`, `ExampleWithCSPNonce` (which reads `NonceFromContext` in a handler) and `ExampleWithHSTS`.
- [X] T042 [P] [US3] Write runnable examples in `middleware/cors/example_test.go` (`ExampleNew` with credentials and a subdomain pattern).
- [X] T043 [P] [US3] Write runnable examples in `middleware/bodylimit/example_test.go` (`ExampleNew`, plus a handler mapping `*http.MaxBytesError` to 413).
- [X] T044 [P] [US3] Write runnable examples in `middleware/contenttype/example_test.go` (`ExampleNew`).

**Checkpoint**: US3 is complete and testable on its own.

---

## Phase 6: User Story 4 - Protect capacity under load and abuse (Priority: P2)

**Goal**: per-client rate limiting and in-flight concurrency limiting.

**Independent Test**: with `testing/synctest` fake time and handlers that block on channels,
exceed the rate and the concurrency limits, then check the refusals, the recovery of the
allowance, and that no slot leaks (US4 scenarios 1–6).

### Tests for User Story 4 ⚠️

- [X] T045 [P] [US4] Write failing tests in `middleware/ratelimit/ratelimit_test.go`, using `synctest.Test`.
  - **Options** (data-model.md), each failure giving `ErrInvalidConfig`:
    - `limit` "> 0";
    - `period` "> 0, and `period / limit ≥ 1ns`";
    - `WithBurst` "> 0";
    - `WithKey` and `WithRejectFunc` "non-nil".
  - **Limits**:
    - with 10 per minute, the 11th request from one client gets 429, with `Retry-After` ≥ 1 and `Rejection{429, Err}` where `errors.Is(Err, ratelimit.ErrLimitExceeded)`;
    - another client is unaffected;
    - after `time.Sleep` inside the bubble, the allowance recovers;
    - the burst size is honored;
    - `Retry-After` equals `ceil((newTAT - cap - now) / 1s)`, with a minimum of 1 (data-model GCRA).
  - **Keys**:
    - `WithKey` by API key gives separate allowances;
    - two IPv6 addresses in the same /64 share one allowance;
    - an empty key shares one allowance;
    - an unparseable `RemoteAddr` is used raw.
  - **Concurrency**: parallel requests are race-free.
- [X] T046 [P] [US4] Write failing internal tests (package `ratelimit`) in `middleware/ratelimit/store_test.go`.
  - **Sweep**: the first request to a shard after `nextSweep` deletes every key with "TAT <= now", then sets `nextSweep = now + cap`.
  - **`TestRateLimit_ReclaimsIdleKeys`** (SC-008): 1,000,000 distinct keys, then fake time advanced beyond 2 × cap, then follow-up traffic that reaches all 64 shards. The number of retained keys must be 0.
- [X] T047 [P] [US4] Write failing tests in `middleware/maxconcurrency/maxconcurrency_test.go`, using `synctest.Test` and blocking handlers.
  - **Options** (data-model.md):
    - `limit` "> 0";
    - `WithQueue` "size > 0, maxWait > 0";
    - `WithRetryAfter` "≥ 1s; sent as whole seconds, rounded up";
    - `WithRejectFunc` "non-nil".
  - **Limit** (scenario 4): with limit 2, the third request gets 503 immediately, with `Rejection{503, Err}` and `errors.Is(Err, maxconcurrency.ErrOverloaded)`. When a slot frees, the next request is admitted.
  - **`Retry-After`**: absent by default and rounded up when configured.
  - **Queue** (scenario 5): a queued request proceeds when a slot frees. It gets 503 when `maxWait` elapses or the queue is full. When the client context is cancelled while waiting, it gets 503 and the handler is not invoked.
  - **Release** (scenario 6): slots are released after a panic or a cancellation.
  - **Load** (SC-006): at 10 times the limit, the number of handlers running at once (tracked with an atomic max) never exceeds the limit. After 10,000 requests that include panics and cancellations, an internal check shows `len(sem) == 0`.

### Implementation for User Story 4

- [X] T048 [US4] Implement the ratelimit package in `middleware/ratelimit/` until T045 and T046 pass, following research R-18:
  - `middleware/ratelimit/gcra.go`: a pure decision function implementing the data-model state machine;
  - `middleware/ratelimit/store.go`: 64 shards, each with a `sync.Mutex`, a `map[string]time.Time` and a `nextSweep` time; a per-instance `maphash.Seed`; lazy sweeping; no goroutines;
  - `middleware/ratelimit/options.go`;
  - `middleware/ratelimit/ratelimit.go`: `var ErrLimitExceeded = errors.New("ratelimit: rate limit exceeded")`, the default key from `netaddr.ParseRemoteAddr` and `netaddr.Key`, `Retry-After` then `respwriter.Reject`.
- [X] T049 [US4] Implement `middleware/maxconcurrency/options.go` and `middleware/maxconcurrency/maxconcurrency.go` until T047 passes, following research R-19:
  - `var ErrOverloaded = errors.New("maxconcurrency: too many concurrent requests")`;
  - the semaphore `chan struct{}` and the `atomic.Int64` count of waiters;
  - a `select` on the semaphore, a timer and `r.Context().Done()`;
  - `defer` release immediately after acquisition;
  - the package doc notes that long-lived connections (WebSocket, SSE) hold a slot until their handler returns.
- [X] T050 [P] [US4] Write runnable examples in `middleware/ratelimit/example_test.go` (`ExampleNew`, `ExampleWithKey`) and `BenchmarkRateLimit` (`b.RunParallel`, many keys) in `middleware/ratelimit/bench_test.go`.
- [X] T051 [P] [US4] Write runnable examples in `middleware/maxconcurrency/example_test.go` (`ExampleNew`, `ExampleWithQueue`).

**Checkpoint**: US4 is complete and testable on its own.

---

## Phase 7: User Story 5 - Reduce bandwidth and avoid redundant transfers (Priority: P3)

**Goal**: response compression, request decompression and entity tags with conditional
requests.

**Independent Test**: serve fixed textual and binary payloads of several sizes, with and
without `Accept-Encoding` and `If-None-Match`, plus compressed request bodies (US5 scenarios
1–6).

### Tests for User Story 5 ⚠️

- [X] T052 [P] [US5] Write failing tests in `middleware/internal/httpheader/accept_test.go`, plus the fuzz target `FuzzAcceptEncoding` added to `middleware/internal/httpheader/fuzz_test.go`, for `Negotiate(acceptEncoding []string, preference []string) string`. Cases (research R-15):
  - q-values are honored;
  - `x-gzip` counts as `gzip`;
  - `*` matches codings not named explicitly;
  - `q=0` excludes a coding;
  - ties go to the server's preference;
  - an absent or empty header, or nothing acceptable, gives `""` (identity);
  - a malformed q makes that entry ignored;
  - the fuzz target asserts: no panic, and the result is always `""` or an element of `preference`.
- [X] T053 [P] [US5] Write failing tests in `middleware/internal/httpheader/etag_test.go`, plus the fuzz target `FuzzEntityTagList` added to `middleware/internal/httpheader/fuzz_test.go`, for `MatchIfNoneMatch(values []string, etag string) bool`. Cases:
  - `*` matches;
  - lists of strong and weak tags are supported;
  - the comparison is weak (`W/"x"` matches `"x"`);
  - a malformed list gives no match;
  - the fuzz target asserts there is no panic.
- [X] T054 [P] [US5] Write failing tests in `middleware/compress/compress_test.go`, with a fixture `middleware/compress/testdata/fixture.json` (about 10 KiB of realistic JSON).
  - **Options** (data-model.md):
    - `WithLevel` "−2 … 9; applies to the built-in codings";
    - `WithMinSize` "≥ 0";
    - `WithContentTypes` "valid patterns, at least one";
    - `WithCoding` "name is a token, lowercase; constructor non-nil; registering an existing name replaces it";
    - `WithPreference` "known names, no duplicates, at least one".
  - **Behavior**: every row of [contracts/http-behavior.md](contracts/http-behavior.md#compress).
    - The fixture with `Accept-Encoding: gzip` gives `Content-Encoding: gzip`, no `Content-Length`, `Vary: Accept-Encoding` exactly once, and a body that round-trips.
    - `deflate` uses zlib.
    - A body under 1024 bytes is not compressed. Nor is `image/png`, a request without `Accept-Encoding`, `identity`, or `gzip;q=0`.
    - Nor are responses that are already encoded, 204, 206 or with `Content-Range`, 304, HEAD, or `Cache-Control: no-transform`.
    - A strong `ETag` becomes `W/"…"`.
    - A missing `Content-Type` is sniffed from the uncompressed bytes.
    - A custom `WithCoding` is preferred over gzip; `WithPreference("gzip")` disables deflate.
    - A 103 response is forwarded; a hijack detaches the wrapper.
    - `SetWriteDeadline` reaches the server via `Unwrap`.
  - **Ratio** (SC-010): the gzip size of the fixture is ≤ 30% of the original.
- [X] T055 [P] [US5] Write a failing streaming test in `middleware/compress/stream_test.go` on a loopback `httptest.NewServer`. The handler writes an event and flushes, then waits on a channel. The client reads and decompresses that event before signalling the handler to write the next one, which proves that flushed data arrives promptly and compressed (scenario 3, FR-059).
- [X] T056 [P] [US5] Write failing tests in `middleware/decompress/decompress_test.go`.
  - **Options** (data-model.md): `WithMaxSize` "> 0"; `WithDecoder` "name is a token, lowercase, not `identity`; non-nil"; `WithRejectFunc` "non-nil".
  - **Decoding**: gzip and deflate bodies are decoded. `Content-Encoding` and `Content-Length` are removed, and `ContentLength == -1`.
  - **Refusals**: `br` gets 415, with `Accept-Encoding: gzip, deflate` and `Rejection{415, Err}` where `errors.Is(Err, decompress.ErrUnsupportedEncoding)`. Stacked `gzip, gzip` also gets 415.
  - **Read errors**:
    - a decoded size above the cap gives `*http.MaxBytesError`;
    - corrupt data, a truncated stream, or a bad gzip header (detected lazily, on the first `Read`) gives errors with `errors.Is(err, decompress.ErrInvalidBody)`.
  - **Pass-through**: identity, no coding, or no body.
  - **Extension**: a custom `WithDecoder` coding works and appears in `Accept-Encoding` on a 415.
- [X] T057 [P] [US5] Write failing tests in `middleware/etag/etag_test.go`.
  - **Options**: `WithMaxSize` "> 0".
  - **Behavior**: every row of [contracts/http-behavior.md](contracts/http-behavior.md#etag).
    - A GET returning 200 with a body ≤ 1 MiB gets `ETag` equal to `"` + base64url(SHA-256(body)[:16]) + `"`, which is 22 characters, identical across two instances.
    - `WithWeak()` adds the `W/` prefix.
    - A matching `If-None-Match` (weak comparison, list, or `*`) gives 304 with an empty body. `Content-Type`, `Content-Length` and `Content-Encoding` are removed; `ETag`, `Cache-Control`, `Vary`, `Expires` and `Content-Location` are kept.
    - A handler ETag is kept and still evaluated.
    - Bodies over the maximum, a handler flush, a non-200 status, or `Content-Range` pass through without a tag.
    - POST is unaffected.
    - A HEAD with an empty body gets no tag.
  - **Repetition** (SC-011): repeated conditional requests give 304 with 0 body bytes every time.

### Implementation for User Story 5

- [X] T058 [US5] Implement `middleware/internal/httpheader/accept.go` (`Negotiate`) and `middleware/internal/httpheader/etag.go` (`MatchIfNoneMatch`) until T052 and T053 pass.
- [X] T059 [US5] Implement the compress package in `middleware/compress/` until T054 and T055 pass, following research R-15:
  - `middleware/compress/writer.go`: the PENDING, PASSTHROUGH, COMPRESSING, CLOSED and DETACHED state machine from data-model.md; per-instance `sync.Pool`s per coding; `Unwrap`, `Flush` and `Hijack`; the sniffing step;
  - `middleware/compress/options.go`, where the default types are `text/*`, `application/json`, `application/*+json`, `application/javascript`, `application/xml`, `application/*+xml`, `application/wasm` and `image/svg+xml`;
  - `middleware/compress/compress.go`, with the exported `Writer` interface;
  - the package doc covers BREACH and `no-transform` as a per-response opt-out.
  Depends on T058.
- [X] T060 [US5] Implement the decompress package in `middleware/decompress/` until T056 passes, following research R-16:
  - `middleware/decompress/reader.go`: lazy decoder creation, wrapping with `fmt.Errorf("%w: %w", ErrInvalidBody, err)` except for `io.EOF` and `*http.MaxBytesError`, the `http.MaxBytesReader` cap;
  - `middleware/decompress/options.go`;
  - `middleware/decompress/decompress.go`: `ErrInvalidBody`, `ErrUnsupportedEncoding`, and the `Accept-Encoding` header followed by `respwriter.Reject`.
- [X] T061 [US5] Implement the etag package in `middleware/etag/` until T057 passes, following research R-17:
  - `middleware/etag/writer.go`: the BUFFERING, CONDITIONAL, PASSTHROUGH and NOT_MODIFIED state machine from data-model.md, plus `Unwrap`, `Flush` and `Hijack`;
  - `middleware/etag/options.go`;
  - `middleware/etag/etag.go`.
  Depends on T058.
- [X] T062 [US5] Write `TestIntegration_ETagBehindCompress` in `middleware/bandwidth_integration_test.go` (package `middleware_test`), covering scenario 6: with `middleware.New(compress, etag)`, the compressed and uncompressed representations carry different validators (`W/"x"` and `"x"`), and an `If-None-Match` with the weak tag still gives 304 (depends on T015, T059 and T061).
- [X] T063 [P] [US5] Write runnable examples in `middleware/compress/example_test.go` (`ExampleNew`, and `ExampleWithCoding` with a custom coding stub) and `BenchmarkCompress` (fixture, pooled writers, `-benchmem`) in `middleware/compress/bench_test.go`.
- [X] T064 [P] [US5] Write runnable examples in `middleware/decompress/example_test.go` (`ExampleNew`, plus handler code mapping `ErrInvalidBody` to 400 and `*http.MaxBytesError` to 413).
- [X] T065 [P] [US5] Write runnable examples in `middleware/etag/example_test.go` (`ExampleNew`) and `BenchmarkETag` in `middleware/etag/bench_test.go`.

**Checkpoint**: all user stories work on their own.

---

## Phase 8: Polish & Cross-Cutting Concerns

**Purpose**: cross-package guarantees, documentation, release readiness and the full gate run.

- [X] T066 [P] Write `TestReject_SharedFunc` and `TestReject_FallbackStatus` in `middleware/rejection_integration_test.go` (package `middleware_test`), covering US1 scenarios 8–9 across the real middleware.
  - One JSON `RejectFunc` is set on bodylimit, contenttype, decompress, ratelimit, maxconcurrency and recovery.
  - Each refusal returns JSON with its status: 413, 415, 415, 429, 503 and 500.
  - `errors.Is(rej.Err, …)` distinguishes `contenttype.ErrUnsupported` from `decompress.ErrUnsupportedEncoding`.
  - The protocol headers are already present when the function runs (`Retry-After`, `Accept-Encoding`).
  - A `RejectFunc` that writes nothing still yields each status.
- [X] T067 [P] Write `TestConventions_InvalidConfig` in `middleware/conventions_test.go` (package `middleware_test`), covering US1 scenarios 4 and 6 across all 13 packages:
  - each `New` with only its mandatory settings succeeds;
  - one invalid setting per package returns an error matching `errors.Is(err, middleware.ErrInvalidConfig)`, whose message starts with `middleware: invalid configuration: <pkg>:`.
- [X] T068 [P] Write `TestOrdering_*` in `middleware/ordering_test.go` (package `middleware_test`). Each test documents an edge-case pitfall:
  - accesslog placed before realip logs the proxy address;
  - recovery placed after a panicking middleware does not catch it;
  - cors placed after ratelimit gives 429 responses without CORS headers;
  - the recommended order (research R-21) avoids all three.
- [X] T069 [P] Write `BenchmarkRecommendedChain` in `middleware/bench_test.go` (package `middleware_test`). It builds every non-buffering middleware with defaults, in recommended order: realip, requestid, accesslog (discard handler), recovery, secureheaders, cors, ratelimit, maxconcurrency, bodylimit, decompress and contenttype. The target is under 100 µs/op (SC-005). Report `-benchmem`.
- [X] T070 [P] Write `ExampleNew_recommended` in `middleware/example_recommended_test.go`, building the full recommended chain of research R-21 (realip → requestid → accesslog → recovery → secureheaders → cors → ratelimit → maxconcurrency → bodylimit → decompress → contenttype → compress → etag) with a shared `RejectFunc`.
- [X] T071 [P] Audit the package docs of `middleware/accesslog`, `middleware/bodylimit`, `middleware/compress` and `middleware/contenttype` against FR-012: mandatory settings, every option with its default, the recommended position, interactions, and a runnable example. Fix any gaps.
- [X] T072 [P] Audit the package docs of `middleware/cors`, `middleware/decompress`, `middleware/etag` and `middleware/maxconcurrency` against FR-012. Fix any gaps.
- [X] T073 [P] Audit the package docs of `middleware/ratelimit`, `middleware/realip`, `middleware/recovery`, `middleware/requestid` and `middleware/secureheaders` against FR-012. These docs must also cover:
  - ratelimit: reclamation needs follow-up traffic, and keys are per instance and per process;
  - recovery: panics in background goroutines started by handlers cannot be recovered;
  - secureheaders: caching of nonce pages, and relaxing COEP.
  Fix any gaps.
- [X] T074 Run `make work` again if needed, then `cd middleware && GOWORK=off go mod tidy -diff`, which must print nothing. Confirm that `find middleware -name go.mod` still prints only `middleware/go.mod`.
- [X] T075 Run the cross-build check from [quickstart.md §2](quickstart.md): `GOOS` linux, darwin and windows × `GOARCH` amd64 and arm64, `go vet ./...` in `middleware/`. There must be no failures.
- [X] T076 Expand `middleware/doc.go` into the full root package documentation:
  - the conventions: constructors and options, `ErrInvalidConfig`, the refusal standard (`Rejection`, `RejectFunc`, `WithRejectFunc`), and the context accessors;
  - the recommended order (research R-21);
  - the ordering pitfalls from spec.md Edge Cases.
- [X] T077 Run `go test -race -run 'Reject|Conventions|Integration|Ordering' ./...` in `middleware/` and confirm that T066–T068 and T031/T062 pass together (cross-story validation of US1 scenarios 4, 6, 8 and 9).
- [X] T078 Run the fuzz targets of [quickstart.md §4](quickstart.md) in `middleware/` for 30 s each (`FuzzForwardedFor`, `FuzzForwarded`, `FuzzAcceptEncoding`, `FuzzEntityTagList`, `FuzzMediaTypeMatch`, `FuzzOrigin`, `FuzzRequestID`). Commit any new failing inputs under the package's `testdata/fuzz/` as regression seeds, after fixing them.
- [X] T079 Run the benchmarks of [quickstart.md §5](quickstart.md) in `middleware/` and record the results in the PR description: `BenchmarkRecommendedChain` < 100 µs/op, plus the ratelimit, compress and etag benchmarks.
- [X] T080 Complete `middleware/README.md`:
  - a minimal usage example (a chain of requestid, accesslog and recovery);
  - the list of the 13 packages, one line each;
  - the recommended order;
  - the refusal-customization standard, with a link to the package docs;
  - the minimum Go version (1.26).
- [X] T081 Fill `middleware/CHANGELOG.md` `## [Unreleased]` → `### Added` with one entry for the root chain and one per package (13 entries).
- [X] T082 Run the local gates `make check MODULE=middleware`. golangci-lint must report zero findings, with only the justified `//nolint` directives. `go test -race -shuffle=on` must pass with coverage ≥ 80%. Fix any finding before pushing.

---

## Dependencies & Execution Order

### Phase dependencies

- **Setup (Phase 1)**: no dependencies.
- **Foundational (Phase 2)**: depends on Setup. It blocks every user story.
- **US1 (Phase 3)**: depends on Foundational.
- **US2 to US5 (Phases 4–7)**: each depends on Foundational and can start in parallel with the
  others. Their packages do not depend on each other, except for the shared items below.
- **Polish (Phase 8)**: depends on every story it touches. T066 needs US2 to US5, and T067 needs
  all 13 packages.

### Cross-story and within-story dependencies

- T031 (US2 integration) and T062 (US5 integration) use `middleware.New`, so they depend on T015
  (US1). Only these integration tests depend on US1; the package code does not.
- Within US2, T024 (recovery) and T025 (accesslog) import `requestid.FromContext`, so they depend
  on T023.
- Within US5, T059 (compress) and T061 (etag) depend on T058 (httpheader additions).
- Every implementation task depends on the test tasks of the same package, which are written and
  observed failing first.
- Example tasks depend on the implementation of the same package.

### Parallel opportunities

- **Setup**: T002 and T003.
- **Foundational**: the test tasks T006, T008, T010 and T012 together. Then the four
  implementations, which touch different packages, can proceed in parallel streams.
- **US2**: the test tasks T017–T022 together. Implementation runs as two streams:
  - T023 → (T024 and T025 in parallel);
  - T026 in parallel with all of them.
- **US3**: the test tasks T032–T036 together. The four implementations (T037–T040) are separate
  packages. The examples T041–T044 run together.
- **US4**: the test tasks T045–T047 together. T048 and T049 run in parallel (separate packages).
- **US5**: the test tasks T052–T057 together. T060 runs in parallel with T058 → (T059 and T061).
- **Polish**: T066–T073 together.

---

## Parallel Example: User Story 2

```bash
# Write all failing tests for US2 together:
Task: "T017 requestid tests in middleware/requestid/requestid_test.go"
Task: "T018 FuzzRequestID in middleware/requestid/fuzz_test.go"
Task: "T019 recovery tests in middleware/recovery/recovery_test.go"
Task: "T020 accesslog tests in middleware/accesslog/accesslog_test.go"
Task: "T021 realip tests in middleware/realip/realip_test.go"
Task: "T022 FuzzForwardedFor/FuzzForwarded in middleware/realip/fuzz_test.go"

# Then implement in two streams:
Stream A: "T023 requestid" → "T024 recovery" + "T025 accesslog"
Stream B: "T026 realip"
```

## Parallel Example: User Story 3

```bash
Task: "T032 secureheaders tests"  Task: "T033 cors tests"  Task: "T034 FuzzOrigin"
Task: "T035 bodylimit tests"      Task: "T036 contenttype tests"
# then T037, T038, T039 and T040 in four independent package streams
```

---

## Implementation Strategy

### MVP first (User Story 1)

1. Phase 1: Setup (T001–T004).
2. Phase 2: Foundational (T005–T013). This is critical: it blocks everything else.
3. Phase 3: US1 (T014–T016).
4. **Stop and validate**: run `GOWORK=off go test -race ./...` in `middleware/`, then
   `make check MODULE=middleware`. The chain, the configuration-error convention and the
   refusal helper are usable.

### Incremental delivery

1. US2 (P1): observability and resilience. This is the minimum production baseline.
2. US3 (P2): hardening.
3. US4 (P2): capacity protection.
4. US5 (P3): bandwidth.
5. Polish: the cross-package guarantees, the docs, and the release of `middleware/v0.1.0` after the
   PR is squash-merged.

Each step leaves `make check MODULE=middleware` green, so the work can pause after any story.

---

## Notes

- Every task keeps to the single module `middleware/`. Never add a `go.mod` in a subfolder.
- In TDD order, a task's tests must fail before its implementation task starts. Compilation
  failure counts as red.
- Never weaken a test to make it pass. Fix the implementation, or update the spec and contracts
  first if the behavior itself must change.
- Commit after each task or logical group. The PR title is
  `feat(middleware): add reusable HTTP middleware module`.

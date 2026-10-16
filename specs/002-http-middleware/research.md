# Research: Reusable HTTP Middleware Module

**Feature**: [spec.md](spec.md) | **Plan**: [plan.md](plan.md) | **Date**: 2026-10-16

Each section records a decision, its rationale and the alternatives rejected. Standard library
APIs were checked with `go doc` against the local Go 1.27 toolchain and are all available in
Go 1.26, the module's minimum version.

## R-01 Module layout, versioning and Go version

- **Decision**:
  - One module, `github.com/paccolamano/go/middleware`, in the top-level directory
    `middleware/`. The root package provides the chain. There is one subpackage per middleware
    and an `internal/` tree for shared code.
  - There is **exactly one `go.mod`**, at `middleware/go.mod`. Each middleware has a dedicated
    folder (`middleware/<name>/`) that is a package of this module, imported as
    `github.com/paccolamano/go/middleware/<name>`. No folder may contain its own `go.mod`,
    because that would make it a nested module, versioned and released separately.
  - The `go` directive is `go 1.26.0`. CI tests on 1.26 and 1.27.
  - Releases start at `middleware/v0.1.0` and stay at 0.x until the API is declared stable.
  - `go.work` is regenerated with `make work`.
- **Rationale**: this is what the spec asks for (FR-001, Assumptions: single module) and it
  complies with Principle I: one top-level directory, its own `go.mod`, a single purpose (HTTP
  server middleware). A Go build compiles only the packages that are imported, so a project
  that imports `cors` does not compile `compress`. With zero dependencies, the module graph is
  not affected either.
- **Alternatives rejected**:
  - One module per middleware (13 modules): 13 READMEs, CHANGELOGs, tags and release streams for
    code that evolves together, and the shared `internal/` code would have to be published as a
    module of its own.

## R-02 Middleware type and chain API

- **Decision**:
  - The root package declares `type Middleware = func(http.Handler) http.Handler`, a type
    **alias**.
  - `Chain` is an immutable value type, built with `New(...Middleware)`, extended with
    `Append(...Middleware)` and `Extend(Chain)`, and applied with `Then(http.Handler)`.
  - The first middleware is the outermost. `Then` calls each middleware function once, when the
    chain is applied, and never per request.
  - `Append` and `Extend` always allocate a new backing array.
  - The zero `Chain` is a valid empty chain.
- **Rationale**:
  - An alias makes any third-party `func(http.Handler) http.Handler` and any library middleware
    interchangeable, including slices of them (FR-002, US1-7). A defined type would reject
    `[]func(http.Handler) http.Handler` in a variadic call.
  - Allocating a new array on every `Append` and `Extend` prevents the classic aliasing bug
    where two `Append` calls on the same base chain overwrite each other (FR-015).
- **Alternatives rejected**:
  - A defined `Middleware` type: worse interoperability.
  - A mutable builder (`chain.Use(m)`): violates FR-015.
  - A `ThenFunc` convenience method: `Then(http.HandlerFunc(f))` already covers it, and the
    exported surface stays minimal (Principle II).

## R-03 Absent middleware or handler (FR-016)

- **Decision**: `New`, `Append` and `Then` **panic** with a descriptive message when given a nil
  middleware or a nil handler. Each `panic` carries `//nolint:forbidigo // <reason>`.
- **Rationale**:
  - A nil handler or middleware only comes from code, never from runtime input, so it is a
    programming error and not a "recoverable condition" (Principle IV). This is the same
    convention as `net/http.ServeMux.Handle`, which panics on a nil handler.
  - A typical cause is ignoring the error of a constructor (`m, _ := cors.New(...)`); the panic
    at chain construction points at that bug at startup (FR-016).
  - Returning errors from `New`, `Append` and `Then` would make every chain expression a
    two-value call and remove the ability to write `mux.Handle("/", chain.Then(h))`.
- **Alternatives rejected**:
  - Skipping nil entries silently: violates FR-016.
  - Falling back to `http.NotFoundHandler()` for a nil handler: hides the bug and only shows it
    at request time.

## R-04 Construction, options and configuration errors (FR-003 to FR-007)

- **Decision**:
  - Every middleware package exports
    `func New(<mandatory...>, opts ...Option) (middleware.Middleware, error)`.
  - `type Option func(*config)` takes an unexported config struct. Options are named `WithX`.
  - Defaults are set first. Then every option is applied in order: for the same setting, the
    last option wins. Finally, `New` validates the complete configuration, so contradictions
    between options are caught as well (e.g. any origin plus credentials).
  - Every validation failure wraps the root sentinel `middleware.ErrInvalidConfig` with the
    message `"<pkg>: <Option or parameter>: <reason>"`. Example:
    `middleware: invalid configuration: cors: WithCredentials: cannot be combined with any origin`.
  - The configuration is copied into the middleware closure and never mutated afterwards.
  - Per-instance state (pools, rate-limit tables, semaphores) lives in the closure. There are
    no package-level variables except sentinel errors.
- **Rationale**:
  - New options are new functions, so adding them is a compatible change for `gorelease`
    (FR-004, SC-003).
  - Option arguments can come from configuration files, so invalid values are recoverable
    conditions and must be returned as errors (FR-005, Principle IV).
  - A single sentinel lets callers and tests use `errors.Is` across all packages.
  - The unexported config type means users cannot build options the library does not define.
- **Alternatives rejected**:
  - Exported config structs (`cors.Config{...}`): every new field is visible, and zero values
    cannot be told apart from "unset".
  - `Must*` variants that panic: they add API surface and the linter forbids `panic`.
  - Options returning errors (`func(*config) error`): validation still has to happen in `New`
    for cross-option rules, so this only adds noise.
  - One sentinel per package: 13 sentinels with the same meaning.

## R-05 Customizing refusal and failure responses (FR-010, FR-011)

The full standard is in [contracts/rejection.md](contracts/rejection.md).

- **Decision**:
  - The root package declares the standard:

    ```go
    type Rejection struct {
        Status int   // HTTP status the middleware would send
        Err    error // cause: matches the middleware's sentinel with errors.Is
    }
    type RejectFunc func(w http.ResponseWriter, r *http.Request, rej Rejection)
    ```

  - Every middleware that can refuse or fail a request accepts
    `WithRejectFunc(middleware.RejectFunc)`, with the same name and type everywhere. Each
    exposes a sentinel for its condition:

    | Package | `Status` | `Err` | Headers set before the call |
    |---------|----------|-------|------------------------------|
    | bodylimit | 413 | `bodylimit.ErrTooLarge` | — |
    | contenttype | 415 | `contenttype.ErrUnsupported` | — |
    | decompress | 415 | `decompress.ErrUnsupportedEncoding` | `Accept-Encoding` |
    | ratelimit | 429 | `ratelimit.ErrLimitExceeded` | `Retry-After` |
    | maxconcurrency | 503 | `maxconcurrency.ErrOverloaded` | `Retry-After` (if configured) |
    | recovery | 500 | `recovery.ErrPanic` | `Cache-Control: no-store`; representation headers removed |

  - **Call sequence**:
    1. The middleware sets the condition's protocol headers.
    2. It calls the `RejectFunc`, the custom one or the default.
    3. If the function wrote neither a status nor a body, the middleware writes `rej.Status`
       with an empty body. The rejection path detects this with the `internal/respwriter`
       observer, so normal requests pay nothing.
  - The default `RejectFunc` is `http.Error(w, http.StatusText(rej.Status), rej.Status)`: plain
    text with `X-Content-Type-Options: nosniff`.
  - A custom function may write a different status, for example 503 instead of 429. That choice
    belongs to the developer.
  - **Extension rules**: new fields, such as a retry delay, may be added to `Rejection` in minor
    releases. A finer condition is exposed as a new error that wraps the existing sentinel (for
    example a future `ErrQueueFull` wrapping `ErrOverloaded`), so existing `errors.Is` checks keep
    matching.
- **Rationale**:
  - A struct parameter can grow without breaking existing functions (FR-004). Positional
    parameters cannot.
  - The status alone is ambiguous: 415 comes from both contenttype and decompress, and future 503
    causes would add more cases. Sentinels with `errors.Is` are the idiomatic way to branch on a
    cause (Principle IV).
  - One signature for every middleware means one function covers the whole chain, for example a
    JSON writer that reads the request ID with `requestid.FromContext`.
  - The fallback guarantees that a refused request never looks like a successful empty 200. This
    matters for security with ratelimit and bodylimit.
  - The default body contains only the standard reason phrase, never `Err.Error()` or other
    details (FR-011).
- **Alternatives rejected**:
  - `func(w, r, status int)`, the previous draft: cannot be extended, and the status is
    ambiguous.
  - `http.Handler`: cannot see the status or the cause.
  - A default set once for the whole chain, either through a root middleware that stores the
    function in the context or through a package-level setter: the first is implicit coupling,
    the second is global state (Principle IV). Passing the same function variable to each
    `WithRejectFunc` is just as short and explicit.
  - Built-in JSON or problem-details (RFC 9457) formats: the format belongs to the application,
    so the default stays neutral plain text.
  - Exposing the panic value in recovery's `Rejection`: it belongs in the logs and in
    `WithReporter`, not near the response (FR-011).

## R-06 Wrapping the response writer and preserving its capabilities (FR-009)

- **Decision**: there are two kinds of wrapper.
  - **Observer**, in `internal/respwriter`, used by accesslog and recovery. It records the
    status, the bytes written, whether the response started and whether it was hijacked.
  - **Transformers**, each private to its package: compress and etag.

  Every wrapper:
  - implements `Unwrap() http.ResponseWriter`, so `http.NewResponseController` reaches the
    server's deadline, full-duplex and hijack controls;
  - implements `Flush()` (transformers flush their own state first) and `Hijack()`, both
    delegating through `http.ResponseController`;
  - treats `WriteHeader` with a status from 100 to 199 other than 101 as **informational**: the
    status is forwarded immediately and not recorded as the final status, mirroring net/http.

  On hijack, the wrappers step aside: an observed response counts as started, with status 101 if
  none was set.

  Middleware that only touch headers (requestid, secureheaders, cors) or the request (bodylimit,
  decompress, contenttype, realip, ratelimit, maxconcurrency) do not wrap the writer at all.
- **Rationale**:
  - `ResponseController` with `Unwrap` is the standard mechanism since Go 1.20. Implementing
    `Flush` and `Hijack` directly also keeps handlers working when they still use type
    assertions (`w.(http.Flusher)`), such as SSE code and older WebSocket libraries.
  - Writing every capability combination (the httpsnoop approach) is unnecessary once these two
    methods are always present and degrade to `http.ErrNotSupported` or a no-op.
  - Not wrapping when it is not needed keeps the per-request cost to a minimum (SC-005).
- **Alternatives rejected**:
  - `github.com/felixge/httpsnoop`: a third-party dependency (Principle V) for capabilities the
    standard library now handles.
  - Exposing `io.ReaderFrom` for sendfile: only a performance optimization, and file serving
    behind accesslog is not a stated use case. It can be added later without breaking changes.
  - `http.Pusher`: HTTP/2 push was removed from browsers.

## R-07 Logging

- **Decision**:
  - accesslog and recovery require a non-nil `*slog.Logger` (`ErrInvalidConfig` otherwise).
  - They log with `Logger.LogAttrs(r.Context(), level, msg, attrs...)`, guarded by
    `Logger.Enabled`, so context-aware handlers (trace IDs) work and disabled levels cost
    nothing.
  - Record schemas, with short flat keys, are fixed in
    [contracts/log-records.md](contracts/log-records.md).
  - Both middleware read the request ID through `requestid.FromContext`.
- **Rationale**: this follows Principle IV (injected logger, no global logger, no
  stdout/stderr). Short flat keys keep the records readable with any `slog` handler. The
  intra-module dependency on `requestid` is a single small package and creates no cycle.
- **Alternatives rejected**:
  - OpenTelemetry semantic-convention keys (`http.request.method`, ...): verbose, and the spec
    does not require them. Developers who need them can add attributes (FR-025).
  - A logging interface: `*slog.Logger` is the standard, and any backend plugs in through
    `slog.Handler`.

## R-08 bodylimit

- **Decision**:
  - When `r.ContentLength > limit`, the middleware rejects with 413 without reading the body.
  - Otherwise, when the request has a body, it replaces `r.Body` with
    `http.MaxBytesReader(w, r.Body, limit)`.
  - The handler recognizes an overflow with `errors.As(err, new(*http.MaxBytesError))`.
- **Rationale**:
  - `MaxBytesReader` is the standard mechanism. It also tells the server to close the
    connection after an overflow, so the rest of the body is never processed (FR-019).
  - Using the standard error type means decompress can raise the same condition for its decoded
    limit (FR-062).
- **Alternatives rejected**:
  - A package-specific error type: duplicates `*http.MaxBytesError`, and code written for the
    standard library would not recognize it.

## R-09 accesslog

- **Decision**:
  - The duration is measured with `time.Now`/`time.Since`, which use the monotonic clock.
  - A deferred function emits the record. A `completed` flag, set after `next.ServeHTTP`
    returns, tells a normal return apart from a panic. **No `recover()`** is used: the panic
    propagates untouched, with its original stack (FR-024).
  - On panic the record carries `panic=true`. Its status is 500 when the handler had not written
    a status, because that is what recovery sends; without recovery, the server aborts the
    response.
  - When the handler wrote nothing, the record shows status 200 and 0 bytes. `bytes` counts what
    was written to the writer at accesslog's position, so it is compressed bytes when accesslog
    is outside compress.
  - Options:
    - `WithLevel(func(*http.Request, Result) slog.Level)`, default `slog.LevelInfo`;
    - `WithAttrs(func(*http.Request, Result) []slog.Attr)`;
    - `WithSkip(func(*http.Request) bool)`;
    - `WithQuery()`;
    - `WithHeaders(names ...string)`.

    `Result` is a struct `{Status int; Bytes int64; Duration time.Duration; Panicked bool}`.
    New fields can be added to it compatibly.
- **Rationale**: logging without `recover` avoids re-panicking, which keeps the stack intact and
  avoids a `//nolint`. A struct parameter for the callbacks leaves room for more response data
  later without changing function signatures (FR-004).
- **Alternatives rejected**:
  - `recover()` followed by `panic(v)`: more complex, and adds a lint exception.
  - Separate callback parameters (status, bytes, ...): every new field would be a breaking
    change.

## R-10 cors

- **Decision**:
  - **Origin entries**, validated and normalized to lowercase:
    - exact `scheme://host[:port]`;
    - `"*"`, which must be the only entry;
    - `scheme://*.domain[:port]`, which matches one or more subdomain labels but not the apex;
    - `"null"`, accepted only literally.

    `WithOriginFunc(func(origin string) bool)` adds a decision function, OR-ed with the list.
    An empty list without a function is a configuration error. `"*"` combined with
    `WithOriginFunc` or with `WithCredentials` is also a configuration error (FR-032).
  - **Defaults**:
    - methods: `GET, HEAD, POST`;
    - allowed headers: none;
    - exposed headers: none;
    - credentials: off;
    - max age: **10 minutes**. `WithMaxAge(0)` omits the header; a negative value is an error.
  - **Headers**: `WithHeaders("*")` allows any requested header, by echoing the request list.
    Header names are compared case-insensitively. Methods are compared case-sensitively, as RFC
    9110 requires. The CORS-safelisted methods always pass the method check, as in the Fetch
    standard.
  - **Any-origin mode** (`"*"`): every response carries `Access-Control-Allow-Origin: *`,
    whether or not it has an Origin header. The response therefore does not depend on Origin
    and needs no `Vary`.
  - **List and function mode**: every response gets `Vary: Origin`. Preflight responses also
    vary on `Access-Control-Request-Method` and `Access-Control-Request-Headers` (FR-031).
  - **Preflight** (OPTIONS with Origin and `Access-Control-Request-Method`): the middleware
    always answers 204 itself. It adds the grant headers only if the origin, the method and all
    requested headers are allowed.
  - **Precomputation**: the joined header values are computed once, at construction.
- **Rationale**: these rules follow the Fetch standard. Sending `Vary` everywhere in list mode
  prevents cache poisoning between origins. Rejecting `*` with credentials closes the usual
  misconfiguration.
- **Alternatives rejected**:
  - Reflecting any origin with credentials: a security hole.
  - Passing preflight requests on to the handler: the spec requires the middleware to answer.
  - Private Network Access: out of scope.
  - Regex origins: the subdomain wildcard plus the decision function cover the need without a
    regex engine.

## R-11 recovery

- **Decision**:
  - A deferred `recover()`. When the value is `http.ErrAbortHandler`, it is re-panicked
    unchanged and not logged (FR-037).
  - Any other value is logged at `slog.LevelError` with the value, `debug.Stack()`, the method,
    the path and the request ID (FR-036). The optional
    `WithReporter(func(r *http.Request, value any, stack []byte))` is then called.
  - **Response not started**: the middleware removes the representation headers set by the
    handler (`Content-Length`, `Content-Type`, `Content-Encoding`, `Content-Range`, `ETag`,
    `Last-Modified`, `Content-Disposition`, `Trailer`), sets `Cache-Control: no-store`, and
    calls the `RejectFunc` with `Rejection{Status: 500, Err: ErrPanic}` (FR-034).
  - **Response already started**: it panics with `http.ErrAbortHandler`. The server then aborts
    the connection or stream without logging it again, and the client sees a truncated response
    instead of an apparently complete one (FR-035).
  - Both re-panics carry `//nolint:forbidigo // net/http abort protocol`.
  - Go 1.21+ turns `panic(nil)` into `*runtime.PanicNilError`, so it is recovered like any other
    value.
- **Rationale**: `http.ErrAbortHandler` is the documented way to abort a response, and the only
  one that works for both HTTP/1.1 and HTTP/2. Clearing the representation headers avoids
  mismatched `Content-Length` or `Content-Encoding` on the 500, and `no-store` keeps caches from
  storing it. Headers set by outer middleware (CORS, request ID, security headers) are kept.
- **Alternatives rejected**:
  - Hijacking and closing the connection: does not work for HTTP/2.
  - Writing a 500 after the headers were sent: impossible, and it would corrupt the body.

## R-12 requestid

- **Decision**:
  - Header `X-Request-ID` by default; `WithHeader(name)` validates the name as an HTTP token.
  - `WithTrustIncoming(bool)` (default `true`) and `WithGenerator(func() string)`.
  - Only the first header value is considered. It is valid when it is 1 to 128 bytes long and
    every byte is in 0x21 to 0x7E.
  - The default generator is `crypto/rand.Text()`: 26 base32 characters with at least 128 bits
    of randomness, cryptographically unpredictable (FR-041). A developer generator whose output
    is invalid falls back to `rand.Text()`, and the documentation says so.
  - The identifier is stored in the context under an unexported key type and read with
    `FromContext(ctx) (string, bool)`. The response header is set before `next` runs (FR-042).
- **Rationale**: the standard library already offers a unique, unpredictable and log-safe
  generator, so no UUID dependency is needed. The byte-range rule blocks CR/LF and control
  characters, which prevents log and header injection.
- **Alternatives rejected**:
  - `github.com/google/uuid`: a dependency.
  - Hand-formatting UUIDv4 strings: more code for a cosmetic benefit. Developers who want UUIDs
    can plug in a generator.

## R-13 secureheaders (defaults fixed here)

- **Decision**: the nine default headers below follow the OWASP Secure Headers Project and the
  OWASP REST Security Cheat Sheet (FR-043).

  | Header | Default |
  |--------|---------|
  | Content-Security-Policy | `default-src 'none'; frame-ancestors 'none'` |
  | Cross-Origin-Embedder-Policy | `require-corp` |
  | Cross-Origin-Opener-Policy | `same-origin` |
  | Cross-Origin-Resource-Policy | `same-origin` |
  | Permissions-Policy | `accelerometer=(), camera=(), display-capture=(), geolocation=(), gyroscope=(), hid=(), magnetometer=(), microphone=(), midi=(), payment=(), serial=(), usb=(), xr-spatial-tracking=()` |
  | Referrer-Policy | `no-referrer` |
  | X-Content-Type-Options | `nosniff` |
  | X-Frame-Options | `DENY` |
  | X-Permitted-Cross-Domain-Policies | `none` |

  - **Content security policy** (FR-044):
    - `WithCSP(policy)` replaces the enforced `Content-Security-Policy`.
    - `WithCSPReportOnly(policy)` sets `Content-Security-Policy-Report-Only`, which is absent by
      default. The two may coexist: the browser enforces the first and only reports violations
      of the second.
    - A policy is validated when the middleware is built:
      - it is not empty and contains no CR, LF or NUL;
      - every `;`-separated directive is non-empty and starts with a directive name
        (`[a-z-]+`).
    - Removing the enforced policy is done with `WithoutHeader("Content-Security-Policy")`.
  - **Per-request nonce** (FR-044):
    - `WithCSPNonce()` generates, for every request, 16 bytes from `crypto/rand` (128 bits, the
      minimum the CSP specification recommends), encoded in standard base64 (the CSP
      `base64-value` grammar).
    - The nonce replaces every `{nonce}` placeholder in both policies. A policy uses it as
      `script-src 'nonce-{nonce}' 'strict-dynamic'`.
    - It is stored in the request context and read with
      `NonceFromContext(ctx) (string, bool)`, so templates can add `nonce="…"` to inline scripts
      (R-24).
    - **Validation**:
      - `WithCSPNonce()` when neither policy contains `{nonce}` is a configuration error;
      - a policy containing `{nonce}` without `WithCSPNonce()` is also an error, since the
        literal placeholder would reach the browser.
    - The request is copied with `r.WithContext` only when the nonce is enabled, so the default
      path stays allocation-light.
  - **One way per header**: `WithHeader(name, value)` sets or adds any other header, for
    example a different Permissions-Policy or COEP `credentialless`. It returns a configuration
    error for the three headers that have dedicated options: `Content-Security-Policy`,
    `Content-Security-Policy-Report-Only` and `Strict-Transport-Security`. Otherwise the
    nonce-placeholder validation and the HSTS rules could be bypassed. `WithoutHeader(name)`
    removes any header. Names must be tokens, and values must not contain CR, LF or NUL.
  - `WithHSTS(HSTS{MaxAge, IncludeSubDomains, Preload})` enables Strict-Transport-Security
    (FR-045). `MaxAge` must be at least 1 second. `Preload` requires `IncludeSubDomains` and a
    `MaxAge` of at least 1 year, the preload-list requirements.
  - The headers are written before `next` runs, so the handler's own values win (FR-046). A new
    value slice is allocated for each request, so a handler calling `Add` never writes into a
    slice shared between requests.
- **Rationale**:
  - **Permissions-Policy** turns off sensor, device, media-capture, location and payment
    features. Three features stay enabled because turning them off breaks common HTML apps:
    `fullscreen` (video players), `autoplay`, and `publickey-credentials-get` (passkey login).
    `cross-origin-isolated` is also left out: disabling it would cancel the isolation that
    COEP plus COOP provide.
  - **COEP `require-corp`** together with COOP gives cross-origin isolation, and it has no
    effect on JSON responses. HTML pages that embed third-party resources relax it with
    `WithHeader("Cross-Origin-Embedder-Policy", "credentialless")` or remove it. The
    documentation explains this (Edge Cases).
  - **Nonces** are the mechanism the CSP specification and Google's "strict CSP" guidance
    recommend for inline scripts. Generating them in the middleware keeps policy and nonce
    consistent per response, which handlers cannot easily guarantee on their own.
- **Alternatives rejected**:
  - `X-XSS-Protection`: deprecated, and OWASP recommends not setting it.
  - `Cache-Control: no-store`: caching policy, which conflicts with etag. For nonce pages the
    documentation recommends caching rules, but secureheaders does not set them.
  - A structured CSP builder (a map of directives): policies are usually copied verbatim from
    generators or documentation, so a builder adds API surface without adding safety.
    Validation of the syntax is enough.
  - Hash-based CSP (`'sha256-…'`): it needs the inline content when the middleware is built.
    It is already possible with a static `WithCSP` policy.
  - Generating a nonce even when it is not referenced: it would cost an allocation and a random
    read per request for nothing.
  - The full OWASP Permissions-Policy list, including `fullscreen`, `autoplay`, `sync-xhr`,
    `clipboard-*` and obsolete features: it breaks common UX, and browsers warn about
    unrecognized features.
  - COEP `credentialless` as the default: weaker isolation, and the user chose `require-corp`.
  - One option per header: about 11 option functions instead of a generic pair, plus dedicated
    options only where validation adds value (CSP, HSTS).

## R-14 realip

- **Decision**:
  - `New(trust Trust, opts ...Option)`, where `Trust` is opaque and built by
    `TrustRanges(cidrsOrIPs ...string)` or `TrustHops(n int)`. Parsing is deferred to `New`,
    which returns the errors. A zero `Trust` is a configuration error (FR-047).
  - `WithHeader(name)` selects the source. The default is `X-Forwarded-For`. `Forwarded` is
    parsed according to RFC 7239 (`for=` parameters, quoted and bracketed). Any other header is
    read as a comma-separated list, so a single-address header such as `X-Real-IP` or
    `CF-Connecting-IP` is a list of one (FR-050).
  - All occurrences of the header are joined in order.
  - Algorithm (FR-048, FR-049):
    - **Ranges mode**: if the peer is not in a trusted range, nothing changes. Otherwise the
      list is walked from the right: trusted entries are skipped and the first untrusted one is
      the client. If every entry is trusted, the leftmost one is the client.
    - **Hops mode**: the client is the N-th entry from the right. If there are fewer than N
      entries, nothing changes.
    - In both modes, if the selected entry, or any entry walked before it, is malformed,
      nothing changes (FR-051).
  - Entries are parsed with `net/netip`: ports, brackets and zones are stripped, and the address
    is passed through `Unmap()`. `unknown` and obfuscated identifiers (`_hidden`) count as
    malformed.
  - The middleware sets `r.RemoteAddr = netip.AddrPortFrom(client, 0).String()` on a shallow
    copy made by `r.WithContext`. That context stores the original `RemoteAddr`, which
    `PeerFromContext(ctx) (string, bool)` returns (FR-052).
- **Rationale**: the rightmost-untrusted algorithm with explicit trust is the established safe
  approach, since any entry left of a trusted proxy may be forged. Rewriting `RemoteAddr` means
  every downstream component (accesslog, ratelimit, third-party code, `net.SplitHostPort` users)
  gets the right value without being aware of realip. Port 0 keeps the `host:port` format.
- **Alternatives rejected**:
  - Leftmost-entry selection: trivially spoofable.
  - Consulting several headers: more ways to spoof.
  - Rewriting `RemoteAddr` without a port: breaks `net.SplitHostPort` callers.
  - A context-only accessor: every consumer would have to know about realip.

## R-15 compress

- **Decision**:
  - **Codings**: built-in `gzip` (`compress/gzip`) and `deflate` (`compress/zlib`, which is
    what HTTP "deflate" means, RFC 9110). `WithCoding(name, func(io.Writer) (Writer, error))`
    registers or replaces a coding, where `type Writer interface { io.WriteCloser;
    Flush() error; Reset(io.Writer) }`. The writers of gzip, zlib, brotli and zstd all satisfy
    it.
  - **Preference**: `WithPreference(names...)` sets the ordered set of codings offered.
    Codings that are not listed are disabled, and unknown names are a configuration error. The
    default order is the custom codings, in registration order, then `gzip`, then `deflate`
    (FR-054).
  - **Negotiation**, in `internal/httpheader` (FR-055):
    - the `Accept-Encoding` header is parsed with q-values;
    - `x-gzip` counts as `gzip`;
    - `*` applies to codings not named explicitly, and `q=0` excludes a coding;
    - the highest q wins, ties go to the server's preference, and the result is identity when
      nothing applies.
    - A request without `Accept-Encoding` is not compressed. RFC 9110 allows any coding in that
      case, but common practice avoids surprising non-browser clients.
  - **Decision point** (FR-056): the response is buffered until it reaches `minSize`. The
    defaults are **1024 bytes**, `WithMinSize`, and level `flate.DefaultCompression`
    (`WithLevel`, validated in −2..9). The decision is taken at the first of:
    - a handler `Content-Length` (decided immediately);
    - the buffer reaching `minSize`;
    - a `Flush`. A flush means streaming, so the response is compressed if its type qualifies,
      whatever its size;
    - the end of the handler.
  - **The response is compressed only if all of these hold**:
    - the status is not 1xx, 204, 206 or 304, and the method is not HEAD;
    - there is no `Content-Encoding` and no `Content-Range`;
    - `Cache-Control` does not contain `no-transform`, which doubles as a per-response opt-out,
      for example for BREACH-sensitive responses;
    - the media type matches the compressible list. When the handler set no `Content-Type`, it
      is sniffed from the uncompressed buffer with `http.DetectContentType` and set; otherwise
      net/http would sniff the compressed bytes.
  - **Default compressible types**: `text/*`, `application/json`, `application/*+json`,
    `application/javascript`, `application/xml`, `application/*+xml`, `application/wasm`,
    `image/svg+xml`. `WithContentTypes` replaces the list. Matching is done in
    `internal/mediatype`.
  - **Headers** (FR-057, FR-058): `Vary: Accept-Encoding` is added to every response that
    passes through, unless the token is already present. When compressing, the middleware also
    sets `Content-Encoding`, deletes `Content-Length`, and turns a strong `ETag` `"x"` into a
    weak one, `W/"x"`.
  - **Pooling**: a `sync.Pool` per coding, per middleware instance (FR-007). Writers are reset
    onto the response and returned to the pool after `Close`.
  - **Flush** (FR-059): flushes the compressor, then the underlying writer through
    `ResponseController`.
  - **Hijack**: the wrapper stops writing.
- **Rationale**:
  - Weakening the ETag, as nginx does, keeps `If-None-Match` working: weak comparison still
    matches, so a 304 is still possible. Adding a suffix such as `-gzip` would never match the
    tag computed by an inner etag middleware.
  - Sniffing before compressing preserves net/http's `Content-Type` behavior.
  - Pooling avoids allocating about 800 KB per gzip writer on every request.
- **Alternatives rejected**:
  - `github.com/klauspost/compress` and `andybalholm/brotli` built in: third-party dependencies
    (Principle V). Developers can plug them in with `WithCoding`.
  - Compressing without `Accept-Encoding`: risky for naive clients.
  - Raw deflate (RFC 1951): incompatible with the HTTP "deflate" coding.

## R-16 decompress

- **Decision**:
  - `WithMaxSize(int64)`, default **10 MiB** (FR-062).
  - `WithDecoder(name, func(io.Reader) (io.ReadCloser, error))` registers additional codings.
    gzip and deflate (zlib) are built in.
  - Only requests that carry a body are considered.
  - `Content-Encoding` is parsed as a token list. An empty list or `identity` passes through:
    the identity coding is removed and nothing else changes (FR-063). More than one coding, or
    an unknown one, is rejected with 415, and the response lists the supported codings in an
    `Accept-Encoding` header (RFC 7694, FR-061).
  - The decoder is created **lazily on the first `Read`**, so a corrupt header is reported to
    the handler as a read error, like corrupt data (FR-062).
  - The body becomes `http.MaxBytesReader(w, decoder, maxSize)`.
  - The middleware deletes `Content-Encoding` and `Content-Length` and sets
    `r.ContentLength = -1` (FR-060).
  - Decoder failures other than `io.EOF` and `*http.MaxBytesError` are wrapped as
    `fmt.Errorf("%w: %w", ErrInvalidBody, err)`, where `ErrInvalidBody` is a package sentinel.
- **Rationale**:
  - Lazy initialization keeps the handler in control of the error response, as the spec
    requires.
  - Reusing `*http.MaxBytesError` gives the same "too large" condition as bodylimit.
  - Rejecting stacked codings removes a decompression-bomb amplification path.
- **Alternatives rejected**:
  - Answering 400 in the middleware for an invalid gzip header: inconsistent with corrupt data
    found later in the body.
  - A compression-ratio limit: the absolute decoded cap already bounds memory and CPU.

## R-17 etag

- **Decision**:
  - Only GET and HEAD.
  - The wrapper **buffers** as long as the status is (or will be) 200, the handler has set no
    `ETag`, and the buffer stays within `WithMaxSize` (default **1 MiB**). Overflowing the
    buffer, a handler `Flush`, a non-200 status or `Content-Range` switch it to pass-through
    without a tag (FR-064).
  - At the end, the tag is the first 16 bytes of the SHA-256 of the buffer, encoded in
    unpadded base64url: `"<22 chars>"`, or `W/"<22 chars>"` with `WithWeak()` (FR-067).
  - When the handler set its own `ETag`, the decision happens at `WriteHeader`, without
    buffering (FR-066).
  - `If-None-Match` is parsed as `*` or a list of entity tags and compared weakly. On a match,
    the response is 304 with no body. Following `http.ServeContent`, the middleware deletes
    `Content-Type`, `Content-Length` and `Content-Encoding`, and keeps `ETag`, `Cache-Control`,
    `Vary`, `Expires` and `Content-Location` (FR-065).
  - A HEAD response with zero buffered bytes gets no generated tag, because the content is
    unknown.
- **Rationale**:
  - SHA-256 is deterministic with no seed, so replicas agree (FR-067). Collisions are
    negligible. It is hardware-accelerated on amd64 and arm64, with throughput far above typical
    response sizes. 128 bits are enough for a validator.
  - Weak comparison is what RFC 9110 prescribes for `If-None-Match`.
- **Alternatives rejected**:
  - `hash/maphash`: randomly seeded per process, so replicas would disagree.
  - FNV or CRC: higher collision risk for little speed gain.
  - Unbounded buffering: unbounded memory use.

## R-18 ratelimit

- **Decision**:
  - `New(limit int, period time.Duration, opts ...Option)`, with `WithBurst(n)` (default
    `limit`) and `WithKey(func(*http.Request) string)`.
  - The algorithm is **GCRA**, equivalent to a token bucket. Each key stores one timestamp (the
    theoretical arrival time, TAT). The emission interval is `T = period / limit` and the
    capacity is `burst × T` (FR-072). `Retry-After` is the seconds until the next request would
    be allowed, rounded up, with a minimum of 1 (FR-071).
  - **Default key**: the IP address parsed from `r.RemoteAddr` (after realip, if placed
    earlier). **IPv6 addresses are keyed by their /64 prefix**, since a single subscriber
    usually controls a whole /64. When `RemoteAddr` cannot be parsed, the raw string is the key.
    An empty key from `WithKey` uses one shared allowance (FR-070).
  - **State**: a fixed number of shards (64), each a mutex-protected `map[string]time.Time`. The
    shard is chosen with `hash/maphash` and a per-instance seed. No package-level state.
  - **Reclamation** (FR-073), with no background goroutine: each shard keeps a `nextSweep`
    deadline. The first request that reaches a shard after its deadline removes every entry
    whose TAT is in the past (fully recovered), then moves the deadline forward by the full
    recovery time (`burst × T`). Memory therefore stays bounded by the keys active within about
    two recovery windows. Reclamation needs incoming traffic, which is the only time memory
    matters. SC-008 is verified with follow-up traffic.
  - **Time**: `time.Now()`. Tests use `testing/synctest` fake time, so there is no clock option
    in the public API.
- **Rationale**:
  - GCRA needs 8 bytes per key and gives `Retry-After` directly, with no refill loop.
  - Sharding avoids a global lock under concurrent load (SC-005).
  - Lazy sweeping meets "no background activity the developer cannot stop" without a `Close`
    method in the API.
  - Keying IPv6 by /64 closes a trivial bypass.
- **Alternatives rejected**:
  - `golang.org/x/time/rate`: a third-party module (Principle V), one limiter object per key
    (larger memory), and it still needs map management.
  - A background janitor goroutine: needs a `Close` or context and adds a lifecycle to the API.
  - An LRU with a hard cap: either evicts active attackers (resetting their limits) or rejects
    new clients. Out of scope per the spec.
  - Sliding-window counters: more state per key.

## R-19 maxconcurrency

- **Decision**:
  - A semaphore: a `chan struct{}` with capacity `limit`.
  - **Non-blocking acquire**. On failure: if a queue is configured (`WithQueue(size, maxWait)`,
    both > 0), an atomic count of waiters is checked. When the queue is full, the request is
    rejected. Otherwise it waits on the semaphore, a timer of `maxWait`, and
    `r.Context().Done()` (FR-077).
  - An expired wait, a client that goes away, or a full queue all lead to the `RejectFunc` with 503,
    plus `Retry-After` when `WithRetryAfter(d)` is set. Even for a departed client, writing the
    503 keeps the access log accurate (status 503 instead of an empty 200).
  - The slot is released by a `defer` placed right after acquisition, so it is released on
    normal return, panic or client cancellation (FR-078).
- **Rationale**: a buffered channel is the idiomatic, allocation-free semaphore, and its `select`
  composes with the timer and the context.
- **Alternatives rejected**:
  - `golang.org/x/sync/semaphore`: a dependency, for weighted acquisition that is not needed.
  - An unbounded queue: unbounded goroutines and memory.

## R-20 contenttype

- **Decision**:
  - `New(allowed []string, opts ...Option)`. The patterns must be `type/subtype` or `type/*`,
    lowercase tokens, and the list must not be empty. `*/*` is accepted and means "any declared
    type".
  - A request carries a body when `r.ContentLength > 0`, or when `r.ContentLength == -1` and
    `r.Body != http.NoBody`.
  - The media type is parsed with `mime.ParseMediaType`. Parameters are ignored and the
    comparison is case-insensitive. A missing type, a parse error or a mismatch gives 415
    (FR-080, FR-081). Matching is shared with compress in `internal/mediatype`.
- **Rationale**: `mime.ParseMediaType` is the standard RFC 2045/9110 parser.
- **Alternatives rejected**: structured-suffix patterns (`application/*+json`). They are not
  required by the spec, and `internal/mediatype` already supports them for compress. They can be
  allowed later without breaking changes.

## R-21 Recommended chain order

The documented order, from outermost to innermost (FR-012, Edge Cases):

1. `realip`: every later middleware sees the real client.
2. `requestid`
3. `accesslog`: records refusals and recovery's 500.
4. `recovery`: catches panics from everything inside it.
5. `secureheaders`: refusals carry the headers too.
6. `cors`: answers preflights before any limiting, and refusals such as 429 stay readable by
   browsers.
7. `ratelimit`
8. `maxconcurrency`
9. `bodylimit`: caps the encoded size.
10. `decompress`: caps the decoded size.
11. `contenttype`
12. `compress`
13. `etag`: computes the tag on the uncompressed content, and compress then weakens it.
14. The handler.

## R-22 Testing approach (Principle III)

- **Style**: the standard `testing` package with table-driven subtests. Requests are built with
  `httptest.NewRequestWithContext(t.Context(), ...)`, as the `noctx` and `usetesting` linters
  require, and recorded with `httptest.ResponseRecorder`.
- **Time-dependent behavior** (ratelimit allowance and recovery, maxconcurrency waits): tested
  with **`testing/synctest`** (Go 1.25+). It gives a fake clock and deterministic scheduling with
  no real sleeps or injected clocks. Handlers that block use channels created inside the bubble.
- **Connection-level behavior** (flush and stream delivery, hijack, recovery abort, client
  disconnect): tested with `httptest.NewServer` on loopback in the same process. That is
  hermetic, with no external network, and `t.Cleanup` closes the servers.
- **Fuzz tests** for every parser of external input:
  - realip: X-Forwarded-For lists and the RFC 7239 `Forwarded` header;
  - `Accept-Encoding` and `Content-Encoding` token lists;
  - `If-None-Match` lists;
  - request ID validation;
  - CORS origin parsing and matching;
  - media type matching.

  Each fuzz test asserts invariants: no panics, a selected address is always a valid IP, and
  validation is idempotent.
- **Benchmarks** (`b.Loop`): the hot path of each middleware, and a full non-buffering chain
  with default settings (SC-005: under 0.1 ms per request), with `-benchmem`.
- **Examples**: at least one `Example` per package, plus a root example that builds the
  recommended chain.
- **Lint**: `wrapcheck` flags errors returned unwrapped from interface methods. The pass-through
  I/O methods (`Write`, `Read`, `Flush`, `Close` of the wrappers) must return the underlying
  errors unwrapped, because the `io` contracts require it (`io.EOF`). These lines carry
  `//nolint:wrapcheck // io contract`, scoped to each line.

## R-23 Dependencies

- **Decision**: only the standard library, so SC-012 holds. The `Writer` and decoder extension
  points let developers add brotli or zstd in their own projects.
- **Consequences**: Trivy reports nothing to scan. The SBOM lists only the module itself.
  `go mod tidy -diff` is trivially clean.

## R-24 Context accessors

- **Decision**: a package that stores data in the request context exposes a typed accessor
  `func XxxFromContext(ctx context.Context) (T, bool)`. The context key is always an unexported
  type (`type ctxKey struct{}`). Only three packages store anything:

  | Package | Accessor | Why it exists |
  |---------|----------|---------------|
  | requestid | `requestid.FromContext(ctx) (string, bool)` | The identifier is needed by handlers (error bodies, calls to downstream services) and by accesslog and recovery (FR-042). |
  | realip | `realip.PeerFromContext(ctx) (string, bool)` | The original peer address is needed for auditing and debugging proxies (FR-052). The resolved client address needs **no** accessor, because realip writes it into `r.RemoteAddr`, where all code already looks. |
  | secureheaders | `secureheaders.NonceFromContext(ctx) (string, bool)` | Templates need the per-request CSP nonce to mark inline scripts. It is present only when `WithCSPNonce()` is enabled (FR-044). |

  The other 10 middleware store nothing in the context, so they get no accessor. No setter
  (`NewContext`) is exported.
- **Rationale**:
  - An accessor is the only way to read the value when the key is unexported. The key must be
    unexported: a key with a built-in type, such as a string, can collide with other packages
    (staticcheck SA1029 flags it). An exported key variable would be package-level state
    (`gochecknoglobals`), and anyone could overwrite the value.
  - With `r.Context().Value(key).(string)`, the caller has to know the stored type and do the
    type assertion. The stored type would become part of the API, and every call site would
    need its own check for "missing". The accessor returns the value already typed, with an
    explicit `bool` for "middleware not in the chain".
  - The accessor is a single function per package, and it lets the internal representation
    change without breaking callers (Principle II).
  - This is the established Go pattern: `metadata.FromIncomingContext` and `peer.FromContext`
    in gRPC, `trace.SpanFromContext` in OpenTelemetry.
- **Alternatives rejected**:
  - Exported context keys, as `net/http.ServerContextKey` does: they expose the stored type and
    allow overwriting, and the linter would flag them as globals.
  - `realip.FromContext` for the client address: it duplicates `r.RemoteAddr`, so two sources
    of truth could disagree.
  - Setters such as `requestid.NewContext(ctx, id)`, for handler unit tests and non-HTTP entry
    points: no current requirement (Principle V). Tests can run the handler behind the real
    middleware. A setter can be added later in a minor release.

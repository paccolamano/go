# Contract: Public Go API

**Module**: `github.com/paccolamano/go/middleware` | **Minimum Go**: 1.26

This file lists the complete exported surface of the module at its first release. Anything not
listed here is unexported or lives under `internal/`. Later releases may only **add**
identifiers, such as new `With*` options or new fields in result structs (Principle II, FR-004).
The exact doc-comment wording is decided at implementation time.

Every package follows the same pattern:

```go
func New(<mandatory...>, opts ...Option) (middleware.Middleware, error)
type Option func(*config) // config is unexported
```

`New` returns `(nil, err)`, with `errors.Is(err, middleware.ErrInvalidConfig)`, for any invalid
setting.

**Context data**: context keys are never exported. A package that stores request-scoped data
exposes `XxxFromContext(ctx) (T, bool)`, where the `bool` is `false` when that middleware did
not run. Only `requestid.FromContext`, `realip.PeerFromContext` and
`secureheaders.NonceFromContext` exist; the other packages
store nothing in the context ([research.md R-24](../research.md#r-24-context-accessors)).

## `middleware` (root)

```go
// Middleware wraps a handler. It is an alias, so any func(http.Handler) http.Handler fits.
type Middleware = func(http.Handler) http.Handler

// Rejection describes a request that a middleware refuses or fails to serve.
// New fields may be added in minor releases. See contracts/rejection.md.
type Rejection struct {
    Status int   // HTTP status the middleware would send (413, 415, 429, 500, 503)
    Err    error // cause; errors.Is against the middleware's sentinel
}

// RejectFunc writes the response for a rejected request.
type RejectFunc func(w http.ResponseWriter, r *http.Request, rej Rejection)

// ErrInvalidConfig is wrapped by every configuration error returned by a constructor.
var ErrInvalidConfig = errors.New("middleware: invalid configuration")

// Chain is an immutable, ordered list of middleware. The zero value is an empty chain.
type Chain struct{ /* unexported */ }

func New(mws ...Middleware) Chain                // panics on a nil middleware (FR-016)
func (c Chain) Append(mws ...Middleware) Chain   // new chain; c is unchanged; panics on nil
func (c Chain) Extend(other Chain) Chain         // new chain: c's middleware, then other's
func (c Chain) Then(h http.Handler) http.Handler // panics on a nil handler; first = outermost
```

## `accesslog`

```go
func New(logger *slog.Logger, opts ...Option) (middleware.Middleware, error)

type Result struct {
    Status   int           // final status: 200 when the handler wrote without setting one;
                           // 500 when it panicked before writing a status
    Bytes    int64         // body bytes written at accesslog's position
    Duration time.Duration
    Panicked bool
}

func WithLevel(fn func(r *http.Request, res Result) slog.Level) Option
func WithAttrs(fn func(r *http.Request, res Result) []slog.Attr) Option
func WithSkip(fn func(r *http.Request) bool) Option // true = do not log
func WithQuery() Option
func WithHeaders(names ...string) Option
```

## `bodylimit`

```go
func New(maxBytes int64, opts ...Option) (middleware.Middleware, error)

// ErrTooLarge is the Rejection.Err of a declared body larger than the limit (413).
var ErrTooLarge = errors.New("bodylimit: request body too large")

func WithRejectFunc(fn middleware.RejectFunc) Option
// Overflow while reading an undeclared-size body: *http.MaxBytesError (errors.As).
```

## `compress`

```go
func New(opts ...Option) (middleware.Middleware, error)

// Writer is a compressing stream. *gzip.Writer, *zlib.Writer, brotli and zstd writers satisfy it.
type Writer interface {
    io.WriteCloser
    Flush() error
    Reset(w io.Writer)
}

func WithLevel(level int) Option            // built-in codings; -2..9
func WithMinSize(bytes int) Option
func WithContentTypes(patterns ...string) Option
func WithCoding(name string, newWriter func(w io.Writer) (Writer, error)) Option
func WithPreference(names ...string) Option
```

## `contenttype`

```go
func New(allowed []string, opts ...Option) (middleware.Middleware, error)

// ErrUnsupported is the Rejection.Err of a missing, unparseable or disallowed media type (415).
var ErrUnsupported = errors.New("contenttype: unsupported media type")

func WithRejectFunc(fn middleware.RejectFunc) Option
```

## `cors`

```go
func New(origins []string, opts ...Option) (middleware.Middleware, error)
func WithOriginFunc(fn func(origin string) bool) Option
func WithMethods(methods ...string) Option
func WithHeaders(headers ...string) Option // "*" = any requested header
func WithExposedHeaders(headers ...string) Option
func WithCredentials() Option
func WithMaxAge(d time.Duration) Option    // 0 omits Access-Control-Max-Age
```

## `decompress`

```go
func New(opts ...Option) (middleware.Middleware, error)

// ErrInvalidBody is wrapped by every failure to decode the request body.
var ErrInvalidBody = errors.New("decompress: invalid encoded body")

// ErrUnsupportedEncoding is the Rejection.Err of an unknown or stacked content coding (415).
var ErrUnsupportedEncoding = errors.New("decompress: unsupported content encoding")

func WithMaxSize(bytes int64) Option
func WithDecoder(name string, newReader func(r io.Reader) (io.ReadCloser, error)) Option
func WithRejectFunc(fn middleware.RejectFunc) Option
// Decoded size above the cap: *http.MaxBytesError (errors.As).
```

## `etag`

```go
func New(opts ...Option) (middleware.Middleware, error)
func WithMaxSize(bytes int) Option
func WithWeak() Option
```

## `maxconcurrency`

```go
func New(limit int, opts ...Option) (middleware.Middleware, error)

// ErrOverloaded is the Rejection.Err when no slot is available (503).
var ErrOverloaded = errors.New("maxconcurrency: too many concurrent requests")

func WithQueue(size int, maxWait time.Duration) Option
func WithRetryAfter(d time.Duration) Option
func WithRejectFunc(fn middleware.RejectFunc) Option
```

## `ratelimit`

```go
func New(limit int, period time.Duration, opts ...Option) (middleware.Middleware, error)

// ErrLimitExceeded is the Rejection.Err when a key exceeds its allowance (429).
var ErrLimitExceeded = errors.New("ratelimit: rate limit exceeded")

func WithBurst(n int) Option
func WithKey(fn func(r *http.Request) string) Option // "" = shared allowance
func WithRejectFunc(fn middleware.RejectFunc) Option
```

## `realip`

```go
func New(trust Trust, opts ...Option) (middleware.Middleware, error)

// Trust declares which proxies in front of the service are trusted. The zero value is invalid.
type Trust struct{ /* unexported */ }

func TrustRanges(cidrsOrIPs ...string) Trust // parsed and validated by New
func TrustHops(n int) Trust

func WithHeader(name string) Option // default X-Forwarded-For; "Forwarded" = RFC 7239

// PeerFromContext returns the original r.RemoteAddr when realip rewrote it.
func PeerFromContext(ctx context.Context) (string, bool)
```

## `recovery`

```go
func New(logger *slog.Logger, opts ...Option) (middleware.Middleware, error)

// ErrPanic is the Rejection.Err when the handler panicked before responding (500).
var ErrPanic = errors.New("recovery: handler panicked")

func WithReporter(fn func(r *http.Request, value any, stack []byte)) Option
func WithRejectFunc(fn middleware.RejectFunc) Option
```

## `requestid`

```go
func New(opts ...Option) (middleware.Middleware, error)
func WithHeader(name string) Option
func WithTrustIncoming(trust bool) Option
func WithGenerator(fn func() string) Option

// FromContext returns the request identifier assigned by the middleware.
func FromContext(ctx context.Context) (string, bool)
```

## `secureheaders`

```go
func New(opts ...Option) (middleware.Middleware, error)

type HSTS struct {
    MaxAge            time.Duration
    IncludeSubDomains bool
    Preload           bool
}

func WithHeader(name, value string) Option // any header except the CSP and HSTS ones
func WithoutHeader(name string) Option      // removes any header, including the default CSP
func WithHSTS(h HSTS) Option

// Content security policy. "{nonce}" in a policy is replaced by the per-request nonce.
func WithCSP(policy string) Option           // replaces the enforced Content-Security-Policy
func WithCSPReportOnly(policy string) Option // sets Content-Security-Policy-Report-Only
func WithCSPNonce() Option                   // a fresh 128-bit nonce for every request

// NonceFromContext returns the CSP nonce of the current request (only with WithCSPNonce).
func NonceFromContext(ctx context.Context) (string, bool)
```

## Usage shape (illustrative)

```go
rid, err := requestid.New()
// handle err …
logm, err := accesslog.New(logger, accesslog.WithSkip(isHealthCheck))
// handle err …
rec, err := recovery.New(logger)
// handle err …

base := middleware.New(rid, logm, rec)
api := base.Append(corsMW, rateMW)

mux.Handle("/api/", api.Then(apiHandler))
mux.Handle("/", base.Then(siteHandler))
```

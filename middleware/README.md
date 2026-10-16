# middleware

Reusable HTTP server middleware for Go, configured through functional options.
Every middleware has the shape `func(http.Handler) http.Handler`, so it works with
`net/http` and with any router built on it. The module has no dependency outside the
standard library.

```bash
go get github.com/paccolamano/go/middleware
```

Minimum Go version: 1.26

## Usage

```go
logger := slog.Default()

rid, err := requestid.New()
if err != nil {
	log.Fatal(err)
}
alog, err := accesslog.New(logger, accesslog.WithSkip(func(r *http.Request) bool {
	return r.URL.Path == "/healthz"
}))
if err != nil {
	log.Fatal(err)
}
rec, err := recovery.New(logger)
if err != nil {
	log.Fatal(err)
}

base := middleware.New(rid, alog, rec)

mux := http.NewServeMux()
mux.Handle("/", base.Then(appHandler))
```

`New`, `Append` and `Extend` build immutable chains: the first middleware is the
outermost. Every constructor validates its configuration and returns an error matching
`middleware.ErrInvalidConfig` when a setting is invalid.

## Packages

| Package | Purpose |
|---------|---------|
| `middleware` | `Chain`, the shared types and the conventions |
| `accesslog` | one structured `slog` record per request |
| `bodylimit` | rejects or caps oversized request bodies (413) |
| `compress` | gzip/deflate response compression, pluggable codings |
| `contenttype` | restricts request media types (415) |
| `cors` | Cross-Origin Resource Sharing, preflights included |
| `decompress` | decodes compressed request bodies, with a decoded-size cap |
| `etag` | entity tags and `304 Not Modified` |
| `maxconcurrency` | limits in-flight requests, with an optional bounded queue (503) |
| `ratelimit` | per-client rate limiting with GCRA (429) |
| `realip` | resolves the client address behind trusted proxies |
| `recovery` | recovers panics: 500 and a structured log record |
| `requestid` | assigns and propagates a request identifier |
| `secureheaders` | security headers: CSP (report-only, per-request nonce), HSTS and more |

## Recommended order

From the outermost to the innermost:

```text
realip → requestid → accesslog → recovery → secureheaders → cors → ratelimit
→ maxconcurrency → bodylimit → decompress → contenttype → compress → etag → handler
```

The package documentation of `middleware` explains the reasons and the pitfalls of other
orders.

## Customizing refusals

The middleware that refuse or fail requests (bodylimit, contenttype, decompress,
ratelimit, maxconcurrency, recovery) accept `WithRejectFunc`. The same function can
serve all of them:

```go
reject := func(w http.ResponseWriter, r *http.Request, rej middleware.Rejection) {
	code := "error"
	if errors.Is(rej.Err, ratelimit.ErrLimitExceeded) {
		code = "rate_limited"
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(rej.Status)
	_ = json.NewEncoder(w).Encode(map[string]string{"code": code})
}

rl, err := ratelimit.New(100, time.Minute, ratelimit.WithRejectFunc(reject))
```

Protocol headers such as `Retry-After` are already set when the function runs. If the
function writes nothing, the middleware still sends the status.

## Documentation

See the package documentation on [pkg.go.dev](https://pkg.go.dev/github.com/paccolamano/go/middleware)
for every option and its default.

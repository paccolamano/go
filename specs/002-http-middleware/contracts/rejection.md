# Contract: Customizing Refusal Responses

This is the single standard every middleware follows to let the developer change the response
it sends when it **refuses** or **fails** a request (FR-010, User Story 1 scenarios 8 and 9).
The design decision is [research.md R-05](../research.md#r-05-customizing-refusal-and-failure-responses-fr-010-fr-011).

## Types (root package `middleware`)

```go
// Rejection describes a request that a middleware refuses or fails to serve.
// New fields may be added in minor releases.
type Rejection struct {
    Status int   // HTTP status the middleware would send
    Err    error // cause; test it with errors.Is against the middleware's sentinel
}

// RejectFunc writes the response for a rejected request.
type RejectFunc func(w http.ResponseWriter, r *http.Request, rej Rejection)
```

Every middleware that can refuse or fail a request has the same option:

```go
func WithRejectFunc(fn middleware.RejectFunc) Option // nil → ErrInvalidConfig
```

## Middleware that use the standard

| Package | When | `Status` | `Err` (sentinel) | Headers already set when the function runs |
|---------|------|----------|------------------|--------------------------------------------|
| bodylimit | declared body larger than the limit | 413 | `bodylimit.ErrTooLarge` | — |
| contenttype | body with a missing, unparseable or disallowed media type | 415 | `contenttype.ErrUnsupported` | — |
| decompress | unknown or stacked content coding | 415 | `decompress.ErrUnsupportedEncoding` | `Accept-Encoding: <supported codings>` |
| ratelimit | allowance exceeded | 429 | `ratelimit.ErrLimitExceeded` | `Retry-After: <seconds>` |
| maxconcurrency | at capacity: no queue, queue full, wait expired, or client gone | 503 | `maxconcurrency.ErrOverloaded` | `Retry-After` (only with `WithRetryAfter`) |
| recovery | handler panic before the response started | 500 | `recovery.ErrPanic` | `Cache-Control: no-store`; representation headers removed |

Middleware that never refuse requests have no `WithRejectFunc`. Their responses are fixed by
the protocol:

- cors preflight: 204, no body;
- etag: 304, no body;
- recovery after the response started: aborted, so no response can be written.

The read errors returned to the handler are not refusals. The handler handles them:
`*http.MaxBytesError` from bodylimit and decompress, and `decompress.ErrInvalidBody`.

## Guarantees

1. **Call sequence**: the middleware sets the headers in the table above, then calls the
   function, then returns. The next handler is never invoked.
2. **Default**: without `WithRejectFunc`, the middleware calls
   `http.Error(w, http.StatusText(rej.Status), rej.Status)`. That is a plain-text body with only
   the standard reason phrase, plus `X-Content-Type-Options: nosniff`.
3. **Fallback**: if the function writes neither a status nor a body, the middleware writes
   `rej.Status` with an empty body. A refusal never turns into an empty 200.
4. **Freedom**: the function may write any status, headers and body. It may also override or
   delete the headers that were already set.
5. **Privacy**: no middleware puts `rej.Err.Error()`, panic values or configuration details in a
   response. A custom function that does so is the developer's responsibility (FR-011).
6. **Stability**:
   - Adding a field to `Rejection` is a compatible change.
   - Each sentinel stays matchable with `errors.Is`. A finer cause added later wraps the
     existing sentinel.
   - Existing functions keep compiling and keep working.

## Usage shape (illustrative)

```go
// One function for the whole chain.
reject := func(w http.ResponseWriter, r *http.Request, rej middleware.Rejection) {
    code := "error"
    switch {
    case errors.Is(rej.Err, ratelimit.ErrLimitExceeded):
        code = "rate_limited"
    case errors.Is(rej.Err, contenttype.ErrUnsupported):
        code = "unsupported_media_type"
    case errors.Is(rej.Err, decompress.ErrUnsupportedEncoding):
        code = "unsupported_encoding"
    }
    id, _ := requestid.FromContext(r.Context())
    w.Header().Set("Content-Type", "application/json")
    w.WriteHeader(rej.Status)
    _ = json.NewEncoder(w).Encode(map[string]string{"code": code, "request_id": id})
}

rl, err := ratelimit.New(100, time.Minute, ratelimit.WithRejectFunc(reject))
ct, err := contenttype.New([]string{"application/json"}, contenttype.WithRejectFunc(reject))
```

# Contract: HTTP Behavior

The observable behavior of each middleware on the wire, with default settings unless stated
otherwise. Each row maps to the spec's functional requirements and is covered by at least one
test.

The terms used below:

- "**next**": the wrapped handler.
- "**reject(s)**": the middleware calls its `RejectFunc` with a `Rejection{Status: s, Err:
  <package sentinel>}`, after setting the protocol headers listed in the row. Plain text by
  default; status `s` is sent even if the function writes nothing. See
  [rejection.md](rejection.md).
- A request "**has a body**" when `ContentLength > 0`, or when `ContentLength == -1` and
  `Body != http.NoBody`.

## Chain

| Situation | Behavior | FR |
|-----------|----------|----|
| `New(A, B, C).Then(H)` | Request order A → B → C → H; response unwinds H → C → B → A. | 013 |
| Empty chain | `Then(H)` returns `H`. | 014 |
| `base.Append(D)` | New chain A, B, D; `base` unchanged. | 015 |
| nil middleware or handler | Panics when `New`, `Append` or `Then` is called, never during a request. | 016 |

## bodylimit

| Situation | Behavior | FR |
|-----------|----------|----|
| `ContentLength > max` | reject(413); next is not invoked; the body is not read. | 018 |
| Undeclared size, reading past `max` | The handler's `Read` returns `*http.MaxBytesError`; the connection is not reused. | 019 |
| Within the limit, or no body | Passes through unchanged. | 020 |

## accesslog

| Situation | Behavior | FR |
|-----------|----------|----|
| Every request not skipped | Exactly one record after the response completes; schema in [log-records.md](log-records.md). | 022 |
| Query string | Omitted unless `WithQuery()`. | 023 |
| Header values | Omitted unless named in `WithHeaders`. | 023 |
| The handler wrote a body without a status | `status=200`. | 024 |
| The handler wrote nothing | `status=200`, `bytes=0`. | 024 |
| The handler panics | A record with `panic=true` (status 500 if none was written); the panic continues unchanged. | 024 |
| Informational responses (1xx except 101) | Forwarded, and not recorded as the status. | Edge |
| Hijacked connection | `status=101` if none was set. | Edge |

## cors

| Situation | Behavior | FR |
|-----------|----------|----|
| List/function mode, any response | `Vary: Origin` added. | 031 |
| Allowed origin, non-preflight | `Access-Control-Allow-Origin: <origin>`, plus `-Allow-Credentials: true` if enabled, plus `-Expose-Headers` if set; next is invoked. | 028 |
| Disallowed origin, or no Origin | No `Access-Control-*` grant headers; next is invoked. | 029 |
| `"*"` mode | `Access-Control-Allow-Origin: *` on every response; no `Vary: Origin`. | 028, 031 |
| Preflight (OPTIONS + Origin + `Access-Control-Request-Method`), all allowed | 204 with `-Allow-Origin`, `-Allow-Methods`, `-Allow-Headers` (if any requested), `-Allow-Credentials` (if enabled), `-Max-Age: 600`, and `Vary: Origin, Access-Control-Request-Method, Access-Control-Request-Headers`. next is not invoked. | 030, 031 |
| Preflight, anything not allowed | 204 with only the `Vary` header; next is not invoked. | 030 |
| OPTIONS without `Access-Control-Request-Method` | Treated as an ordinary request. | 030 |
| `Origin: null` | Matches only an explicit `"null"` entry. | 032 |

## recovery

| Situation | Behavior | FR |
|-----------|----------|----|
| Panic before the response started | Representation headers removed, `Cache-Control: no-store`, reject(500); logged; the server keeps serving. | 034, 036 |
| Panic after the response started | Logged, then `panic(http.ErrAbortHandler)`: the client sees a truncated or reset response. | 035, 036 |
| `panic(http.ErrAbortHandler)` raised by the handler | Re-raised unchanged; not logged. | 037 |
| Response body | Never contains the panic value or the stack. | 011 |

## requestid

| Situation | Behavior | FR |
|-----------|----------|----|
| No `X-Request-ID` | A new identifier (26 base32 characters) is generated. | 039, 041 |
| Valid incoming identifier (first value; 1–128 bytes in 0x21–0x7E) | Reused, unless `WithTrustIncoming(false)`. | 039, 040 |
| Invalid incoming identifier | Replaced by a generated one. | 039 |
| Every response | The `X-Request-ID` header is set before next runs; `FromContext` returns the identifier. | 042 |

## secureheaders

| Situation | Behavior | FR |
|-----------|----------|----|
| Every response (defaults) | The 9 headers listed below; no `Strict-Transport-Security` and no `Content-Security-Policy-Report-Only`. | 043, 045 |
| `WithCSP(p)` | `Content-Security-Policy: p`. | 044 |
| `WithCSPReportOnly(p)` | `Content-Security-Policy-Report-Only: p`, alongside the enforced policy unless that was removed. | 044 |
| `WithCSPNonce()` | Every response gets a different nonce (24 base64 characters) in place of `{nonce}` in both policies; `NonceFromContext` returns that same nonce to the handler. | 044 |
| `WithHeader` / `WithoutHeader` | The header is overridden, added or removed. `WithHeader` on a CSP or HSTS header is a configuration error. | 044 |
| `WithHSTS` | `Strict-Transport-Security: max-age=N[; includeSubDomains][; preload]`. | 045 |
| The handler sets or deletes one of these headers | The handler's choice wins. | 046 |

Default headers (rationale in [research.md R-13](../research.md#r-13-secureheaders-defaults-fixed-here)):

| Header | Value |
|--------|-------|
| Content-Security-Policy | `default-src 'none'; frame-ancestors 'none'` |
| Cross-Origin-Embedder-Policy | `require-corp` |
| Cross-Origin-Opener-Policy | `same-origin` |
| Cross-Origin-Resource-Policy | `same-origin` |
| Permissions-Policy | `accelerometer=(), camera=(), display-capture=(), geolocation=(), gyroscope=(), hid=(), magnetometer=(), microphone=(), midi=(), payment=(), serial=(), usb=(), xr-spatial-tracking=()` |
| Referrer-Policy | `no-referrer` |
| X-Content-Type-Options | `nosniff` |
| X-Frame-Options | `DENY` |
| X-Permitted-Cross-Domain-Policies | `none` |

## realip

| Situation | Behavior | FR |
|-----------|----------|----|
| Ranges mode, peer not trusted | `RemoteAddr` unchanged; headers ignored. | 048 |
| Ranges mode, peer trusted, `XFF: forged, client, proxy2` with proxy2 trusted | `RemoteAddr = "client:0"`. | 049 |
| Every entry trusted | Leftmost entry. | 049 |
| Hops mode n=2, `XFF: forged, client, cdn` | `client`. | 049 |
| Fewer entries than hops / malformed entry on the walk | `RemoteAddr` unchanged. | 049, 051 |
| `Forwarded: for="[2001:db8::1]:4711"` (with `WithHeader("Forwarded")`) | `[2001:db8::1]:0`. | 050, 051 |
| Any rewrite | `PeerFromContext` returns the original `RemoteAddr`. | 052 |

## compress

| Situation | Behavior | FR |
|-----------|----------|----|
| Every response passing through | `Vary: Accept-Encoding`, added once. | 057 |
| `Accept-Encoding: gzip`, `application/json` of 10 KiB | `Content-Encoding: gzip`, no `Content-Length`. | 055–057 |
| Body under 1024 bytes and no flush | Uncompressed. | 056 |
| Type not compressible (e.g. `image/png`) | Uncompressed. | 056 |
| No `Accept-Encoding`, `identity` only, or `gzip;q=0` and nothing else acceptable | Uncompressed. | 055 |
| `Content-Encoding` already set, 206, `Content-Range`, 204, 304, HEAD, or `Cache-Control: no-transform` | Uncompressed. | 056 |
| Strong `ETag: "x"` on a compressed response | `ETag: W/"x"`. | 058 |
| Handler `Flush` | The data written so far reaches the client compressed. | 059 |
| No `Content-Type` set by the handler | Sniffed from the uncompressed bytes and set before compressing. | 056 |

## decompress

| Situation | Behavior | FR |
|-----------|----------|----|
| `Content-Encoding: gzip` or `deflate` with a body | The handler reads the decoded body; `Content-Encoding` and `Content-Length` are removed; `ContentLength = -1`. | 060 |
| Unknown coding, or stacked codings (`gzip, gzip`) | `Accept-Encoding: gzip, deflate[, custom…]`, then reject(415); next is not invoked. | 061 |
| Decoded size over 10 MiB | The handler's `Read` returns `*http.MaxBytesError`. | 062 |
| Corrupt or truncated data (including a bad header) | The handler's `Read` returns an error wrapping `ErrInvalidBody`. | 062 |
| No coding, `identity`, or no body | Passes through unchanged. | 063 |

## etag

| Situation | Behavior | FR |
|-----------|----------|----|
| GET returning 200, body ≤ 1 MiB, no handler ETag | `ETag: "<22 chars>"`. | 064, 067 |
| Repeated with a matching `If-None-Match` (weak comparison, list, or `*`) | 304, no body; `Content-Type`, `Content-Length` and `Content-Encoding` removed; `ETag`, `Cache-Control`, `Vary`, `Expires` and `Content-Location` kept. | 065 |
| The handler sets an ETag | Kept; `If-None-Match` still evaluated. | 066 |
| Body over 1 MiB, a handler flush, a non-200 status, or `Content-Range` | Passed through without a tag. | 064 |
| Other methods | Pass through unchanged. | 068 |
| HEAD with no body written | No generated tag. | Edge |

## ratelimit

| Situation | Behavior | FR |
|-----------|----------|----|
| Within the allowance | next is invoked. | 072 |
| Allowance exceeded | `Retry-After: <seconds ≥ 1>`, then reject(429); next is not invoked; other keys unaffected. | 071 |
| Time passes | The allowance recovers continuously; up to `burst` back-to-back requests. | 072 |
| Default key | The IP from `RemoteAddr`; IPv6 grouped by /64. | 070 |
| `WithKey` returns `""` | One shared allowance. | 070 |

## maxconcurrency

| Situation | Behavior | FR |
|-----------|----------|----|
| Fewer than `limit` requests in flight | next is invoked. | 075 |
| At the limit, no queue | Immediately reject(503), with `Retry-After` if configured. | 076 |
| At the limit, queue has room | Waits up to `maxWait`; proceeds when a slot frees. | 077 |
| Wait expired, queue full, or client gone | reject(503); next is not invoked. | 077 |
| Request ends (normally, by panic or by cancellation) | Slot released. | 078 |

## contenttype

| Situation | Behavior | FR |
|-----------|----------|----|
| Has a body, type allowed (parameters ignored, case-insensitive) | Passes through. | 080 |
| Has a body; type missing, unparseable or not allowed | reject(415); next is not invoked. | 080 |
| No body | Passes through, whatever the `Content-Type`. | 081 |

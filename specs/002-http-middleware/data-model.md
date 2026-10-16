# Data Model: Reusable HTTP Middleware Module

**Feature**: [spec.md](spec.md) | **Plan**: [plan.md](plan.md) | **Date**: 2026-10-16

This module is a library and stores nothing persistently. Its "data" is the configuration of
each middleware, the per-request state carried in contexts and wrappers, and the per-instance
runtime state. The exported signatures are in [contracts/public-api.md](contracts/public-api.md).

## Shared conventions

| Concept | Representation | Rules |
|---------|----------------|-------|
| Middleware | `middleware.Middleware = func(http.Handler) http.Handler` | Built once by `New` and safe for concurrent use. Its configuration is fixed after `New` (FR-006). |
| Option | `type Option func(*config)` in each package | Defaults are applied first, then the options in order. For the same setting, the last option wins. |
| Configuration error | an error wrapping `middleware.ErrInvalidConfig` | Message: `"middleware: invalid configuration: <pkg>: <Setting>: <reason>"`. `New` returns `(nil, err)` (FR-005). |
| Rejection | `middleware.Rejection{Status int; Err error}` | `Err` is the sentinel of the refusing package (table in [contracts/rejection.md](contracts/rejection.md)). Fields may be added in minor releases; finer causes wrap the existing sentinel. |
| Refusal response | `middleware.RejectFunc(w, r, rej)`, set with `WithRejectFunc` | The default is `http.Error(w, http.StatusText(rej.Status), rej.Status)`. Protocol headers are set before the call. If the function writes nothing, `rej.Status` is sent with an empty body (FR-010). A nil `RejectFunc` is a configuration error. |
| Header names | canonical form (`textproto.CanonicalMIMEHeaderKey`) | Every name supplied by a developer is validated as an RFC 9110 token. |

## Chain (root)

| Field | Type | Notes |
|-------|------|-------|
| `mws` | `[]Middleware` (unexported) | The first entry is the outermost. The slice is never shared: `New`, `Append` and `Extend` copy it. |

- **Validation**: a nil middleware passed to `New` or `Append`, or a nil handler passed to
  `Then`, makes the call panic with `middleware: nil middleware at position N` or
  `middleware: nil handler` (FR-016, R-03).
- **Behavior**: `Then(h)` wraps from the last middleware to the first. The zero `Chain` is empty,
  and `Then(h)` on it returns `h` unchanged (FR-014).

## Per-middleware configuration

Mandatory settings are positional parameters of `New`. Options are `With*` functions.

### bodylimit

| Setting | Kind | Default | Validation |
|---------|------|---------|------------|
| `maxBytes int64` | mandatory | — | > 0 |
| `WithRejectFunc` | option | plain-text 413 | non-nil |

### accesslog

| Setting | Kind | Default | Validation |
|---------|------|---------|------------|
| `logger *slog.Logger` | mandatory | — | non-nil |
| `WithLevel(func(*http.Request, Result) slog.Level)` | option | always `slog.LevelInfo` | non-nil |
| `WithAttrs(func(*http.Request, Result) []slog.Attr)` | option | none | non-nil |
| `WithSkip(func(*http.Request) bool)` | option | never skip | non-nil |
| `WithQuery()` | option | query excluded | — |
| `WithHeaders(names ...string)` | option | none | valid tokens, at least one name |

**Result** (passed to the callbacks): `Status int`, `Bytes int64`, `Duration time.Duration`,
`Panicked bool`. New fields may be added in later minor releases.

### cors

| Setting | Kind | Default | Validation |
|---------|------|---------|------------|
| `origins []string` | mandatory (may be empty if `WithOriginFunc` is set) | — | each entry is an exact origin, `"*"` (alone), a `scheme://*.domain[:port]` pattern, or `"null"`; normalized to lowercase; no path, query or fragment |
| `WithOriginFunc(func(origin string) bool)` | option | none | non-nil; not combined with `"*"` |
| `WithMethods(...string)` | option | `GET, HEAD, POST` | valid tokens, at least one |
| `WithHeaders(...string)` | option | none | valid tokens, or a single `"*"` |
| `WithExposedHeaders(...string)` | option | none | valid tokens |
| `WithCredentials()` | option | off | not with `"*"` (FR-032) |
| `WithMaxAge(time.Duration)` | option | 10 min | ≥ 0; 0 omits the header; sent in whole seconds |

**Derived at construction**: the matching sets (exact origins, suffix patterns) and the joined
header values.

### recovery

| Setting | Kind | Default | Validation |
|---------|------|---------|------------|
| `logger *slog.Logger` | mandatory | — | non-nil |
| `WithReporter(func(r *http.Request, value any, stack []byte))` | option | none | non-nil |
| `WithRejectFunc` | option | plain-text 500 | non-nil |

### requestid

| Setting | Kind | Default | Validation |
|---------|------|---------|------------|
| `WithHeader(name string)` | option | `X-Request-ID` | valid token |
| `WithTrustIncoming(bool)` | option | `true` | — |
| `WithGenerator(func() string)` | option | `crypto/rand.Text` | non-nil |

- **Request identifier**: a string of 1 to 128 bytes, each byte in 0x21 to 0x7E (FR-039). It is
  stored in the request context under an unexported key and read with `FromContext`.
- When a custom generator returns an invalid value, `rand.Text()` is used instead.

### secureheaders

| Setting | Kind | Default | Validation |
|---------|------|---------|------------|
| header set | — | the 9 headers in [research.md R-13](research.md#r-13-secureheaders-defaults-fixed-here) | — |
| `WithHeader(name, value string)` | option | — | name is a token and is not `Content-Security-Policy`, `Content-Security-Policy-Report-Only` or `Strict-Transport-Security`; value has no CR, LF or NUL |
| `WithoutHeader(name string)` | option | — | name is a token; removing an absent header is a no-op |
| `WithHSTS(HSTS)` | option | disabled | `MaxAge ≥ 1s`; `Preload` requires `IncludeSubDomains` and `MaxAge ≥ 365 × 24h` |
| `WithCSP(policy string)` | option | `default-src 'none'; frame-ancestors 'none'` | non-empty; no CR, LF or NUL; every `;`-separated directive starts with `[a-z-]+` |
| `WithCSPReportOnly(policy string)` | option | absent | same as `WithCSP` |
| `WithCSPNonce()` | option | off | at least one of the policies contains `{nonce}` |
| *(cross-check)* | — | — | a policy containing `{nonce}` requires `WithCSPNonce()` |

**CSP nonce**: 16 bytes from `crypto/rand`, standard base64 (24 characters), one per request. It
replaces every `{nonce}` in both policies, is stored in the request context, and is read with
`NonceFromContext`.

**HSTS**: `MaxAge time.Duration`, `IncludeSubDomains bool`, `Preload bool`. Rendered as
`max-age=<seconds>[; includeSubDomains][; preload]`.

### realip

| Setting | Kind | Default | Validation |
|---------|------|---------|------------|
| `trust Trust` | mandatory | — | non-zero. `TrustRanges`: each entry is a CIDR or a single IP, at least one. `TrustHops`: n ≥ 1 |
| `WithHeader(name string)` | option | `X-Forwarded-For` | valid token; `Forwarded` selects RFC 7239 parsing |

- **Trust**: an opaque value holding either `[]netip.Prefix` (single IPs become /32 or /128) or a
  hop count.
- **Client address**: `netip.Addr`, unmapped, without zone or port. Written back as
  `r.RemoteAddr = "<addr>:0"` (IPv6 in brackets). The original `RemoteAddr` is stored in the
  context and read with `PeerFromContext`.

### compress

| Setting | Kind | Default | Validation |
|---------|------|---------|------------|
| `WithLevel(int)` | option | `flate.DefaultCompression` | −2 … 9; applies to the built-in codings |
| `WithMinSize(int)` | option | 1024 | ≥ 0 |
| `WithContentTypes(...string)` | option | the list in [research.md R-15](research.md#r-15-compress) | valid patterns, at least one |
| `WithCoding(name string, func(io.Writer) (Writer, error))` | option | — | name is a token, lowercase; constructor non-nil; registering an existing name replaces it |
| `WithPreference(...string)` | option | custom codings (in registration order), `gzip`, `deflate` | known names, no duplicates, at least one |

**Writer**: `io.WriteCloser` plus `Flush() error` plus `Reset(io.Writer)`.

### decompress

| Setting | Kind | Default | Validation |
|---------|------|---------|------------|
| `WithMaxSize(int64)` | option | 10 MiB | > 0 |
| `WithDecoder(name string, func(io.Reader) (io.ReadCloser, error))` | option | gzip, deflate built in | name is a token, lowercase, not `identity`; non-nil |
| `WithRejectFunc` | option | plain-text 415 | non-nil |

**Errors**: `ErrInvalidBody` is the sentinel wrapped by every decoder failure. `*http.MaxBytesError`
is returned when the decoded size exceeds the cap.

### etag

| Setting | Kind | Default | Validation |
|---------|------|---------|------------|
| `WithMaxSize(int)` | option | 1 MiB | > 0 |
| `WithWeak()` | option | strong tags | — |

**Entity tag**: `"` + base64url(SHA-256(body)[:16]) + `"`, optionally prefixed with `W/`.

### ratelimit

| Setting | Kind | Default | Validation |
|---------|------|---------|------------|
| `limit int` | mandatory | — | > 0 |
| `period time.Duration` | mandatory | — | > 0, and `period / limit ≥ 1ns` |
| `WithBurst(int)` | option | `limit` | > 0 |
| `WithKey(func(*http.Request) string)` | option | IP from `RemoteAddr` (IPv6 → /64) | non-nil |
| `WithRejectFunc` | option | plain-text 429 | non-nil |

**Runtime state per instance**:

- 64 shards, each holding `sync.Mutex`, `map[string]time.Time` (key → TAT) and a `nextSweep`
  time.
- A `maphash.Seed`.
- The derived values `T = period/limit` (emission interval) and `cap = burst × T`.

### maxconcurrency

| Setting | Kind | Default | Validation |
|---------|------|---------|------------|
| `limit int` | mandatory | — | > 0 |
| `WithQueue(size int, maxWait time.Duration)` | option | no queue | size > 0, maxWait > 0 |
| `WithRetryAfter(time.Duration)` | option | no header | ≥ 1s; sent as whole seconds, rounded up |
| `WithRejectFunc` | option | plain-text 503 | non-nil |

**Runtime state per instance**: `sem chan struct{}` with capacity `limit`, and `waiting
atomic.Int64`.

### contenttype

| Setting | Kind | Default | Validation |
|---------|------|---------|------------|
| `allowed []string` | mandatory | — | at least one; each `type/subtype`, `type/*` or `*/*`, lowercase tokens |
| `WithRejectFunc` | option | plain-text 415 | non-nil |

## State machines

### ratelimit: one key (GCRA)

`TAT` (theoretical arrival time) starts absent. A missing key behaves like `TAT = now`.

```text
request at time now:
  tat     = max(TAT, now)
  newTAT  = tat + T
  if newTAT - now <= cap:   TAT = newTAT   → ALLOW
  else:                     unchanged      → DENY, Retry-After = ceil((newTAT - cap - now) / 1s), min 1

A key is fully recovered when TAT <= now.
Sweep: on the first request to a shard after nextSweep, delete every key with TAT <= now,
then set nextSweep = now + cap.
```

### maxconcurrency: one request

```text
ARRIVING ──slot free──────────────────────────────▶ ADMITTED ──handler returns/panics──▶ RELEASED
    │
    ├─no slot, no queue───────────────────────────▶ REJECTED (503)
    ├─no slot, queue full (waiting ≥ size)────────▶ REJECTED (503)
    └─no slot, queue has room──▶ WAITING ─slot────▶ ADMITTED
                                     ├─maxWait elapsed──────▶ REJECTED (503)
                                     └─client context done──▶ REJECTED (503, handler not invoked)
```

### compress: response writer

```text
PENDING (buffering up to minSize; status recorded; 1xx forwarded immediately)
  ├─ ineligible (status 204/206/304, HEAD, Content-Encoding set, Content-Range set,
  │   no-transform, type not compressible, no acceptable coding) ──▶ PASSTHROUGH
  ├─ Content-Length < minSize at first write ──────────────────▶ PASSTHROUGH
  ├─ buffer ≥ minSize, or Flush, or Content-Length ≥ minSize ───▶ COMPRESSING
  ├─ handler returns with buffer < minSize ─────────────────────▶ PASSTHROUGH (buffer written once)
  └─ Hijack ───────────────────────────────────────────────────▶ DETACHED
PASSTHROUGH / COMPRESSING ──handler returns──▶ CLOSED (compressor closed and returned to pool)
Vary: Accept-Encoding is added in every state except DETACHED.
```

### etag: response writer (GET/HEAD only)

```text
BUFFERING (status 200 or not yet set; no ETag from the handler)
  ├─ WriteHeader(non-200) / Content-Range / Flush / buffer > maxSize ──▶ PASSTHROUGH (no tag)
  ├─ handler set ETag before WriteHeader ──▶ CONDITIONAL
  │       ├─ If-None-Match matches ──▶ NOT_MODIFIED (304, body discarded)
  │       └─ otherwise ──────────────▶ PASSTHROUGH
  └─ handler returns ──▶ compute the tag (skipped for HEAD with an empty buffer)
          ├─ If-None-Match matches ──▶ NOT_MODIFIED (304)
          └─ otherwise ──────────────▶ 200 + ETag + buffered body
```

### respwriter: observer (accesslog, recovery)

```text
fields: status (int; 0 until a final status), bytes (int64), started (bool), hijacked (bool)
WriteHeader(code): 100–199 except 101 → forwarded, nothing recorded
                   otherwise → status = code, started = true (first call only)
Write(p):          if !started → status = 200, started = true; bytes += n
Hijack():          hijacked = true, started = true; status = 101 if 0
```

## Context values

| Package | Key (unexported type) | Value | Accessor |
|---------|-----------------------|-------|----------|
| requestid | `ctxKey{}` | `string` (the effective identifier) | `requestid.FromContext(ctx) (string, bool)` |
| realip | `ctxKey{}` | `string` (the original `r.RemoteAddr`) | `realip.PeerFromContext(ctx) (string, bool)` |
| secureheaders | `ctxKey{}` | `string` (the CSP nonce, only with `WithCSPNonce`) | `secureheaders.NonceFromContext(ctx) (string, bool)` |

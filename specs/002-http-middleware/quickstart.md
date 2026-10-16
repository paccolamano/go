# Quickstart: Validating the Middleware Module

This guide proves the feature works end to end. The expected behavior is defined in
[contracts/http-behavior.md](contracts/http-behavior.md),
[contracts/log-records.md](contracts/log-records.md) and
[contracts/public-api.md](contracts/public-api.md); this guide only says how to check it.

## Prerequisites

- Go 1.26 or newer. A C toolchain, for `-race`.
- golangci-lint at the version pinned in `versions.env`.
- From the repository root, run `make work` once so `go.work` lists `./middleware`. This is
  only for editor navigation; no gate uses it.

## 1. Local gates (constitution)

```bash
make check MODULE=middleware      # golangci-lint with zero findings, then go test -race -shuffle=on with coverage ≥ 80%
```

**Expected**: a `PASS` line for `middleware` from both `lint` and `test`. No `.module-exceptions`
entry is needed.

## 2. Structure, tidy and cross-build (the same checks CI runs)

```bash
find middleware -name go.mod            # exactly one go.mod for the whole library
scripts/structure-check.sh middleware   # README, CHANGELOG, no replace, go directive ≤ 1.26
(cd middleware && GOWORK=off go mod tidy -diff)
for os in linux darwin windows; do for arch in amd64 arm64; do
  (cd middleware && GOWORK=off GOOS=$os GOARCH=$arch go vet ./...) || echo "FAIL $os/$arch"
done; done
```

**Expected**: `find` prints only `middleware/go.mod`. The structure check reports `PASS` for
`middleware`, and only `middleware`, with no nested modules. Tidy prints nothing, and there are
no `FAIL` lines.

## 3. Scenario checks per user story

Run these from `middleware/` with `GOWORK=off`. Test names follow the
`Test<Feature>_<Scenario>` convention fixed in `tasks.md`. The commands below select them by
package.

| Story | Command | What must hold |
|-------|---------|----------------|
| US1: chain and conventions | `go test -race -run 'Chain\|Example' .` | Execution order A → B → C → H → C → B → A; the empty chain is the identity; `Append` leaves the base intact; nil middleware panics at build time; every `New` with an invalid option returns an error that satisfies `errors.Is(err, middleware.ErrInvalidConfig)`. In `go test -race -run Reject ./...`, one shared JSON `RejectFunc` set on the 6 refusing middleware returns JSON with each status, and `errors.Is(rej.Err, …)` tells contenttype's 415 from decompress's 415. A `RejectFunc` that writes nothing still produces the status ([rejection.md](contracts/rejection.md)). |
| US2: observability and resilience | `go test -race ./recovery ./requestid ./accesslog ./realip` and `go test -race -run Integration .` | A panic gives a 500 plus a log record with stack and request ID, and the next request succeeds; a panic after the first byte truncates the response; the request ID is generated, reused or replaced as specified; a forged `X-Forwarded-For` from an untrusted peer is ignored. |
| US3: hardening | `go test -race ./secureheaders ./cors ./bodylimit ./contenttype` | 9 default headers (including CSP, Permissions-Policy and COEP) and no HSTS; with `WithCSPNonce` two requests get different nonces, each equal to its handler's `NonceFromContext`; `WithCSPReportOnly` adds the report-only header next to the enforced one; `WithCSPNonce` without a `{nonce}` placeholder, or `WithHeader("Content-Security-Policy", …)`, returns a configuration error; a preflight gets 204 and never reaches the handler; `"*"` with credentials returns a configuration error; a declared 5 MiB body gets 413; `text/plain` gets 415 and `application/json; charset=utf-8` passes. |
| US4: capacity | `go test -race ./ratelimit ./maxconcurrency` | The 11th request in a minute gets 429 with `Retry-After`; the allowance recovers (synctest fake time, no real waiting); the 3rd concurrent request gets 503; queued requests proceed or time out; zero slots leak after panics or cancellations. |
| US5: bandwidth | `go test -race ./compress ./decompress ./etag` | 10 KiB of JSON is gzip-encoded with `Vary`; small or PNG responses are not compressed; a flush streams compressed data (loopback server); a gzip request body is decoded and `br` gets 415; a repeated `If-None-Match` gets 304 with no body; the compressed tag is weakened. |

## 4. Fuzzing (parsers of external input)

```bash
cd middleware
GOWORK=off go test -run '^$' -fuzz='^FuzzForwardedFor$'     -fuzztime=30s ./realip
GOWORK=off go test -run '^$' -fuzz='^FuzzForwarded$'        -fuzztime=30s ./realip
GOWORK=off go test -run '^$' -fuzz='^FuzzAcceptEncoding$'   -fuzztime=30s ./internal/httpheader
GOWORK=off go test -run '^$' -fuzz='^FuzzEntityTagList$'    -fuzztime=30s ./internal/httpheader
GOWORK=off go test -run '^$' -fuzz='^FuzzMediaTypeMatch$'   -fuzztime=30s ./internal/mediatype
GOWORK=off go test -run '^$' -fuzz='^FuzzOrigin$'           -fuzztime=30s ./cors
GOWORK=off go test -run '^$' -fuzz='^FuzzRequestID$'        -fuzztime=30s ./requestid
```

**Expected**: no failures. The seed corpora also run in every normal `go test` invocation.

## 5. Performance (SC-005, SC-008, SC-010)

```bash
cd middleware
GOWORK=off go test -run '^$' -bench 'RecommendedChain' -benchmem .
GOWORK=off go test -run '^$' -bench . -benchmem ./ratelimit ./compress ./etag
```

**Expected**:

- `BenchmarkRecommendedChain` reports **< 100 µs/op**. Typical results should be in the low
  microseconds.
- The ratelimit memory test (`TestRateLimit_ReclaimsIdleKeys`, run as part of `go test`) shows
  zero retained keys after 1,000,000 idle clients plus follow-up traffic.
- The compression ratio test asserts that the 10 KiB JSON fixture compresses to at most 30% of
  its original size.

## 6. Manual smoke test (optional)

Build a throwaway program from the README example: the recommended chain around a handler that
returns 10 KiB of JSON, listening on `:8080`. Then:

```bash
curl -si -H 'Accept-Encoding: gzip' localhost:8080/ | head -20      # Content-Encoding: gzip, Vary, X-Request-ID, security headers
curl -si -X OPTIONS -H 'Origin: https://app.example.com' \
     -H 'Access-Control-Request-Method: PUT' localhost:8080/       # 204 + Access-Control-Allow-*
etag=$(curl -s -D - -o /dev/null localhost:8080/ | awk 'tolower($1)=="etag:"{print $2}' | tr -d '\r')
curl -si -H "If-None-Match: $etag" localhost:8080/ | head -1       # HTTP/1.1 304 Not Modified
```

**Expected**: one `http request` log record per call, in the format of
[log-records.md](contracts/log-records.md).

## 7. Release readiness

- `middleware/README.md` contains the purpose, `go get github.com/paccolamano/go/middleware`, a
  minimal example, the minimum Go version (1.26) and the recommended order.
- `middleware/CHANGELOG.md` has an `[Unreleased]` section listing every package.
- The PR title is `feat(middleware): add reusable HTTP middleware module`, and every PR status is
  green.

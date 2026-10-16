// Package secureheaders sets security-related response headers.
//
// The defaults follow the OWASP Secure Headers Project and suit API
// responses:
//
//	Content-Security-Policy: default-src 'none'; frame-ancestors 'none'
//	Cross-Origin-Embedder-Policy: require-corp
//	Cross-Origin-Opener-Policy: same-origin
//	Cross-Origin-Resource-Policy: same-origin
//	Permissions-Policy: accelerometer=(), camera=(), display-capture=(), geolocation=(), gyroscope=(), hid=(), magnetometer=(), microphone=(), midi=(), payment=(), serial=(), usb=(), xr-spatial-tracking=()
//	Referrer-Policy: no-referrer
//	X-Content-Type-Options: nosniff
//	X-Frame-Options: DENY
//	X-Permitted-Cross-Domain-Policies: none
//
// Strict-Transport-Security is disabled by default; enable it with [WithHSTS].
// The headers are set before the next handler runs, so a handler can override
// or delete any of them.
//
// HTML pages usually need a different content security policy: set it with
// [WithCSP], trial a stricter one with [WithCSPReportOnly], and use
// [WithCSPNonce] with "'nonce-{nonce}'" in the policy to allow inline scripts
// that carry the nonce returned by [NonceFromContext]. A response whose policy
// carries a nonce must not be served from a shared cache, or every visitor
// would receive the same nonce; this middleware does not set caching headers.
//
// The default Cross-Origin-Embedder-Policy prevents HTML pages from loading
// cross-origin images or scripts that do not allow it through
// Cross-Origin-Resource-Policy or CORS. Pages that embed such resources can use
// WithHeader("Cross-Origin-Embedder-Policy", "credentialless") or
// WithoutHeader("Cross-Origin-Embedder-Policy").
//
// Mandatory settings: none.
//
// Options and defaults:
//   - [WithHeader], [WithoutHeader]: the headers above.
//   - [WithHSTS]: disabled.
//   - [WithCSP]: the default policy above.
//   - [WithCSPReportOnly]: no report-only policy.
//   - [WithCSPNonce]: disabled.
//
// Recommended position: after recovery and before cors, so that every
// response, including refusals of the middleware after it, carries the
// headers.
package secureheaders

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"net/http"
	"strings"

	"github.com/paccolamano/go/middleware"
)

type ctxKey struct{}

type header struct {
	name     string
	value    string
	hasNonce bool
}

// New returns a middleware that sets security headers on every response.
func New(opts ...Option) (middleware.Middleware, error) {
	var cfg config
	for _, opt := range opts {
		opt(&cfg)
	}
	values, err := cfg.headers()
	if err != nil {
		return nil, err
	}
	headers := make([]header, 0, len(values))
	for name, value := range values {
		headers = append(headers, header{name: name, value: value,
			hasNonce: strings.Contains(value, noncePlaceholder)})
	}
	nonce := cfg.nonce
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var n string
			if nonce {
				n = newNonce()
				r = r.WithContext(context.WithValue(r.Context(), ctxKey{}, n))
			}
			h := w.Header()
			for _, hd := range headers {
				v := hd.value
				if hd.hasNonce {
					v = strings.ReplaceAll(v, noncePlaceholder, n)
				}
				h[hd.name] = []string{v}
			}
			next.ServeHTTP(w, r)
		})
	}, nil
}

// NonceFromContext returns the content security policy nonce of the current
// request. It reports false unless the middleware runs with WithCSPNonce.
func NonceFromContext(ctx context.Context) (string, bool) {
	n, ok := ctx.Value(ctxKey{}).(string)
	return n, ok
}

// newNonce returns 128 random bits in standard base64.
func newNonce() string {
	b := make([]byte, 16)
	// crypto/rand.Read never returns an error (Go 1.24+).
	_, _ = rand.Read(b)
	return base64.StdEncoding.EncodeToString(b)
}

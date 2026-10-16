// Package requestid assigns a correlation identifier to every request.
//
// The identifier is taken from the request header (X-Request-ID by default)
// when it is valid and incoming identifiers are trusted; otherwise a new one
// is generated. It is set on the response header before the next handler runs
// and made available to downstream code through [FromContext].
//
// A valid identifier is 1 to 128 bytes long and contains only printable ASCII
// characters other than space (0x21 to 0x7E), so it cannot inject content into
// logs or headers.
//
// Mandatory settings: none.
//
// Options and defaults:
//   - [WithHeader]: X-Request-ID.
//   - [WithTrustIncoming]: true.
//   - [WithGenerator]: crypto/rand.Text (26 characters, at least 128 bits of
//     randomness).
//
// Recommended position: near the outside of the chain, after realip and before
// accesslog and recovery, which include the identifier in their log records.
package requestid

import (
	"context"
	"crypto/rand"
	"net/http"

	"github.com/paccolamano/go/middleware"
)

type ctxKey struct{}

const maxLength = 128

// New returns a middleware that assigns a request identifier.
func New(opts ...Option) (middleware.Middleware, error) {
	cfg := defaultConfig()
	for _, opt := range opts {
		opt(&cfg)
	}
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id := ""
			if cfg.trust {
				if v := r.Header.Get(cfg.header); valid(v) {
					id = v
				}
			}
			if id == "" {
				id = generate(cfg.generator)
			}
			w.Header().Set(cfg.header, id)
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxKey{}, id)))
		})
	}, nil
}

// FromContext returns the request identifier assigned by the middleware. It
// reports false when the middleware did not run for this request.
func FromContext(ctx context.Context) (string, bool) {
	id, ok := ctx.Value(ctxKey{}).(string)
	return id, ok
}

func generate(gen func() string) string {
	if id := gen(); valid(id) {
		return id
	}
	return rand.Text()
}

func valid(id string) bool {
	if id == "" || len(id) > maxLength {
		return false
	}
	for i := range len(id) {
		if id[i] < 0x21 || id[i] > 0x7e {
			return false
		}
	}
	return true
}

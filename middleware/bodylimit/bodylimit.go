// Package bodylimit limits the size of request bodies.
//
// A request whose declared Content-Length exceeds the limit is refused with
// 413 Content Too Large before its body is read and without invoking the next
// handler. When the size is not declared in advance, reading past the limit
// fails with an *http.MaxBytesError, which the handler can recognize with
// errors.As and map to 413; the rest of the body is not delivered and the
// connection is not reused.
//
// Mandatory settings: the maximum body size in bytes.
//
// Options and defaults:
//   - [WithRejectFunc]: a plain-text 413 response, with Rejection.Err
//     matching [ErrTooLarge].
//
// Recommended position: after the middleware that limit traffic (ratelimit,
// maxconcurrency) and before decompress, so that it bounds the encoded size
// while decompress bounds the decoded size.
package bodylimit

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/paccolamano/go/middleware"
	"github.com/paccolamano/go/middleware/internal/respwriter"
)

// ErrTooLarge is the Rejection.Err of a declared body larger than the limit.
var ErrTooLarge = errors.New("bodylimit: request body too large")

// Option configures the middleware returned by New.
type Option func(*config)

type config struct {
	reject    middleware.RejectFunc
	rejectSet bool
}

// WithRejectFunc replaces the default plain-text 413 response.
func WithRejectFunc(fn middleware.RejectFunc) Option {
	return func(c *config) {
		c.reject = fn
		c.rejectSet = true
	}
}

// New returns a middleware that limits request bodies to maxBytes.
func New(maxBytes int64, opts ...Option) (middleware.Middleware, error) {
	if maxBytes <= 0 {
		return nil, fmt.Errorf("%w: bodylimit: maxBytes: must be greater than 0", middleware.ErrInvalidConfig)
	}
	var cfg config
	for _, opt := range opts {
		opt(&cfg)
	}
	if cfg.rejectSet && cfg.reject == nil {
		return nil, fmt.Errorf("%w: bodylimit: WithRejectFunc: the function must not be nil", middleware.ErrInvalidConfig)
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.ContentLength > maxBytes {
				respwriter.Reject(w, r, cfg.reject, middleware.Rejection{Status: http.StatusRequestEntityTooLarge, Err: ErrTooLarge})
				return
			}
			if r.Body != nil && r.Body != http.NoBody {
				r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
			}
			next.ServeHTTP(w, r)
		})
	}, nil
}

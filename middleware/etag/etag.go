// Package etag adds entity tags to responses and answers conditional requests
// with 304 Not Modified.
//
// For a 200 response to GET or HEAD without an ETag, the middleware buffers
// the body (up to a maximum size) and sets ETag to a hash of it: the first 16
// bytes of its SHA-256, base64url-encoded and quoted. Identical content always
// gets the same tag, across instances and restarts, so replicas agree. When
// the request's If-None-Match matches the tag (weak comparison, lists and "*"
// supported), the response becomes 304 with no body; ETag, Cache-Control,
// Vary, Expires and Content-Location are kept, and Content-Type,
// Content-Length and Content-Encoding are removed. When the handler sets its
// own ETag, it is kept and If-None-Match is still evaluated.
//
// Responses larger than the maximum size, responses the handler flushes,
// non-200 responses, partial content and other methods pass through without a
// generated tag. A HEAD response gets a tag only when the handler writes the
// same body as for GET.
//
// Mandatory settings: none.
//
// Options and defaults:
//   - [WithMaxSize]: 1 MiB.
//   - [WithWeak]: strong tags.
//
// Recommended position: last before the handler, just inside compress, so
// that the tag is computed on the uncompressed content and compress weakens it
// for the compressed representation.
package etag

import (
	"fmt"
	"net/http"

	"github.com/paccolamano/go/middleware"
)

// Option configures the middleware returned by New.
type Option func(*config)

type config struct {
	maxSize int
	weak    bool
}

// WithMaxSize sets the largest body, in bytes, buffered to compute a tag. The
// default is 1 MiB.
func WithMaxSize(bytes int) Option {
	return func(c *config) { c.maxSize = bytes }
}

// WithWeak generates weak tags (W/"…") instead of strong ones.
func WithWeak() Option {
	return func(c *config) { c.weak = true }
}

// New returns a middleware that adds entity tags and handles If-None-Match.
func New(opts ...Option) (middleware.Middleware, error) {
	cfg := config{maxSize: 1 << 20}
	for _, opt := range opts {
		opt(&cfg)
	}
	if cfg.maxSize <= 0 {
		return nil, fmt.Errorf("%w: etag: WithMaxSize: must be greater than 0", middleware.ErrInvalidConfig)
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodGet && r.Method != http.MethodHead {
				next.ServeHTTP(w, r)
				return
			}
			ew := &responseWriter{w: w, r: r, maxSize: cfg.maxSize, weak: cfg.weak}
			next.ServeHTTP(ew, r)
			// Not deferred: after a panic the buffered data must not be sent,
			// so that an outer recovery can still answer 500.
			ew.finish()
		})
	}, nil
}

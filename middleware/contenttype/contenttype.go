// Package contenttype restricts the media types of request bodies.
//
// A request that carries a body (a declared length greater than zero, or a
// body of unknown length) is refused with 415 Unsupported Media Type when its
// Content-Type is missing, unparseable or not allowed. Matching ignores
// parameters such as charset and is case-insensitive. Requests without a body
// pass regardless of their Content-Type.
//
// Mandatory settings: the allowed media types, each "type/subtype", "type/*"
// or "*/*", in lowercase.
//
// Options and defaults:
//   - [WithRejectFunc]: a plain-text 415 response, with Rejection.Err
//     matching [ErrUnsupported].
//
// Recommended position: after bodylimit and decompress, before the handler.
package contenttype

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/paccolamano/go/middleware"
	"github.com/paccolamano/go/middleware/internal/mediatype"
	"github.com/paccolamano/go/middleware/internal/respwriter"
)

// ErrUnsupported is the Rejection.Err of a missing, unparseable or disallowed
// media type.
var ErrUnsupported = errors.New("contenttype: unsupported media type")

// Option configures the middleware returned by New.
type Option func(*config)

type config struct {
	reject    middleware.RejectFunc
	rejectSet bool
}

// WithRejectFunc replaces the default plain-text 415 response.
func WithRejectFunc(fn middleware.RejectFunc) Option {
	return func(c *config) {
		c.reject = fn
		c.rejectSet = true
	}
}

// New returns a middleware that accepts request bodies only of the allowed
// media types.
func New(allowed []string, opts ...Option) (middleware.Middleware, error) {
	for _, a := range allowed {
		if strings.Contains(a, "*+") {
			return nil, fmt.Errorf("%w: contenttype: allowed: %q: structured-suffix patterns are not supported", middleware.ErrInvalidConfig, a)
		}
	}
	matcher, err := mediatype.ParsePatterns(allowed)
	if err != nil {
		return nil, fmt.Errorf("%w: contenttype: allowed: %w", middleware.ErrInvalidConfig, err)
	}
	var cfg config
	for _, opt := range opts {
		opt(&cfg)
	}
	if cfg.rejectSet && cfg.reject == nil {
		return nil, fmt.Errorf("%w: contenttype: WithRejectFunc: the function must not be nil", middleware.ErrInvalidConfig)
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if hasBody(r) && !matcher.Match(r.Header.Get("Content-Type")) {
				respwriter.Reject(w, r, cfg.reject, middleware.Rejection{Status: http.StatusUnsupportedMediaType, Err: ErrUnsupported})
				return
			}
			next.ServeHTTP(w, r)
		})
	}, nil
}

func hasBody(r *http.Request) bool {
	return r.ContentLength > 0 || (r.ContentLength == -1 && r.Body != nil && r.Body != http.NoBody)
}

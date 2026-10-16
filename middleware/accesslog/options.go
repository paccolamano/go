package accesslog

import (
	"fmt"
	"log/slog"
	"net/http"

	"github.com/paccolamano/go/middleware"
	"github.com/paccolamano/go/middleware/internal/httpheader"
)

// Option configures the middleware returned by New.
type Option func(*config)

type config struct {
	level      func(r *http.Request, res Result) slog.Level
	levelSet   bool
	attrs      func(r *http.Request, res Result) []slog.Attr
	attrsSet   bool
	skip       func(r *http.Request) bool
	skipSet    bool
	query      bool
	headers    []string
	headersSet bool
}

// WithLevel sets the function that chooses the level of each record. The
// default logs every request at slog.LevelInfo.
func WithLevel(fn func(r *http.Request, res Result) slog.Level) Option {
	return func(c *config) {
		c.level = fn
		c.levelSet = true
	}
}

// WithAttrs sets a function returning custom attributes, appended after the
// standard ones, for example an authenticated user identifier.
func WithAttrs(fn func(r *http.Request, res Result) []slog.Attr) Option {
	return func(c *config) {
		c.attrs = fn
		c.attrsSet = true
	}
}

// WithSkip sets a function that excludes requests from logging when it
// returns true, for example health checks. By default every request is
// logged.
func WithSkip(fn func(r *http.Request) bool) Option {
	return func(c *config) {
		c.skip = fn
		c.skipSet = true
	}
}

// WithQuery includes the query string in the records. It is excluded by
// default because it may carry credentials.
func WithQuery() Option {
	return func(c *config) { c.query = true }
}

// WithHeaders includes the values of the named request headers, in a "header"
// group. No header is logged by default.
func WithHeaders(names ...string) Option {
	return func(c *config) {
		c.headers = names
		c.headersSet = true
	}
}

func (c *config) validate() error {
	switch {
	case c.levelSet && c.level == nil:
		return fmt.Errorf("%w: accesslog: WithLevel: the function must not be nil", middleware.ErrInvalidConfig)
	case c.attrsSet && c.attrs == nil:
		return fmt.Errorf("%w: accesslog: WithAttrs: the function must not be nil", middleware.ErrInvalidConfig)
	case c.skipSet && c.skip == nil:
		return fmt.Errorf("%w: accesslog: WithSkip: the function must not be nil", middleware.ErrInvalidConfig)
	case c.headersSet && len(c.headers) == 0:
		return fmt.Errorf("%w: accesslog: WithHeaders: at least one header name is required", middleware.ErrInvalidConfig)
	}
	canonical := make([]string, 0, len(c.headers))
	for _, name := range c.headers {
		if !httpheader.ValidToken(name) {
			return fmt.Errorf("%w: accesslog: WithHeaders: %q is not a valid header name", middleware.ErrInvalidConfig, name)
		}
		canonical = append(canonical, http.CanonicalHeaderKey(name))
	}
	c.headers = canonical
	return nil
}

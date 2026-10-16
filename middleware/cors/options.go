package cors

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/paccolamano/go/middleware"
	"github.com/paccolamano/go/middleware/internal/httpheader"
)

// Option configures the middleware returned by New.
type Option func(*config)

type config struct {
	originFunc    func(origin string) bool
	originFuncSet bool
	methods       []string
	headers       []string
	exposed       []string
	credentials   bool
	maxAge        time.Duration
}

func defaultConfig() config {
	return config{
		methods: []string{http.MethodGet, http.MethodHead, http.MethodPost},
		maxAge:  10 * time.Minute,
	}
}

// WithOriginFunc adds a function that decides whether an origin is allowed,
// in addition to the origins passed to New. It is never consulted for the
// "null" origin and cannot be combined with "*".
func WithOriginFunc(fn func(origin string) bool) Option {
	return func(c *config) {
		c.originFunc = fn
		c.originFuncSet = true
	}
}

// WithMethods sets the methods allowed in preflight requests. The default is
// GET, HEAD and POST. The CORS-safelisted methods GET, HEAD and POST always
// pass the preflight method check.
func WithMethods(methods ...string) Option {
	return func(c *config) { c.methods = methods }
}

// WithHeaders sets the request headers allowed in preflight requests, compared
// case-insensitively. A single "*" allows any requested header. By default
// only the CORS-safelisted headers are allowed.
func WithHeaders(headers ...string) Option {
	return func(c *config) { c.headers = headers }
}

// WithExposedHeaders sets the response headers that browsers expose to
// scripts. None by default.
func WithExposedHeaders(headers ...string) Option {
	return func(c *config) { c.exposed = headers }
}

// WithCredentials allows credentials (cookies, HTTP authentication) in
// cross-origin requests. It cannot be combined with "*".
func WithCredentials() Option {
	return func(c *config) { c.credentials = true }
}

// WithMaxAge sets how long browsers may cache a preflight response, sent in
// whole seconds. The default is 10 minutes; 0 omits the header.
func WithMaxAge(d time.Duration) Option {
	return func(c *config) { c.maxAge = d }
}

// policy is the validated configuration, with the header values precomputed.
type policy struct {
	origins      originMatcher
	methods      map[string]struct{}
	anyHeader    bool
	headers      map[string]struct{}
	credentials  bool
	allowMethods string
	allowHeaders string
	exposed      string
	maxAge       string
}

func newPolicy(origins []string, c config) (*policy, error) {
	m, err := parseOrigins(origins)
	if err != nil {
		return nil, invalid("origins", "%s", err)
	}
	switch {
	case c.originFuncSet && c.originFunc == nil:
		return nil, invalid("WithOriginFunc", "the function must not be nil")
	case m.any && c.originFunc != nil:
		return nil, invalid("WithOriginFunc", `cannot be combined with "*"`)
	case m.any && c.credentials:
		return nil, invalid("WithCredentials", `cannot be combined with "*"`)
	case !m.any && len(m.exact) == 0 && len(m.patterns) == 0 && c.originFunc == nil:
		return nil, invalid("origins", "at least one origin or WithOriginFunc is required")
	case len(c.methods) == 0:
		return nil, invalid("WithMethods", "at least one method is required")
	case c.maxAge < 0:
		return nil, invalid("WithMaxAge", "must not be negative")
	}
	m.fn = c.originFunc
	p := &policy{origins: m, credentials: c.credentials, methods: map[string]struct{}{}, headers: map[string]struct{}{}}
	for _, method := range c.methods {
		if !httpheader.ValidToken(method) {
			return nil, invalid("WithMethods", "%q is not a valid method", method)
		}
		p.methods[method] = struct{}{}
	}
	p.allowMethods = strings.Join(c.methods, ", ")
	if len(c.headers) == 1 && c.headers[0] == "*" {
		p.anyHeader = true
	} else {
		canonical := make([]string, 0, len(c.headers))
		for _, h := range c.headers {
			if h == "*" || !httpheader.ValidToken(h) {
				return nil, invalid("WithHeaders", `%q is not a valid header name (use a single "*" for any header)`, h)
			}
			p.headers[strings.ToLower(h)] = struct{}{}
			canonical = append(canonical, http.CanonicalHeaderKey(h))
		}
		p.allowHeaders = strings.Join(canonical, ", ")
	}
	exposed := make([]string, 0, len(c.exposed))
	for _, h := range c.exposed {
		if !httpheader.ValidToken(h) {
			return nil, invalid("WithExposedHeaders", "%q is not a valid header name", h)
		}
		exposed = append(exposed, http.CanonicalHeaderKey(h))
	}
	p.exposed = strings.Join(exposed, ", ")
	if secs := int64(c.maxAge / time.Second); secs > 0 {
		p.maxAge = strconv.FormatInt(secs, 10)
	}
	return p, nil
}

func invalid(setting, format string, args ...any) error {
	return fmt.Errorf("%w: cors: %s: %s", middleware.ErrInvalidConfig, setting, fmt.Sprintf(format, args...))
}

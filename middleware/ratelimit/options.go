package ratelimit

import (
	"net/http"

	"github.com/paccolamano/go/middleware"
)

// Option configures the middleware returned by New.
type Option func(*config)

type config struct {
	burst     int
	burstSet  bool
	key       func(r *http.Request) string
	keySet    bool
	reject    middleware.RejectFunc
	rejectSet bool
}

// WithBurst sets how many requests a client can send back to back. The
// default is the limit passed to New.
func WithBurst(n int) Option {
	return func(c *config) {
		c.burst = n
		c.burstSet = true
	}
}

// WithKey sets the function that identifies the client, for example by API
// key or user. Requests for which it returns "" share one allowance. The
// default key is the IP address from r.RemoteAddr, grouping IPv6 addresses by
// their /64 prefix.
func WithKey(fn func(r *http.Request) string) Option {
	return func(c *config) {
		c.key = fn
		c.keySet = true
	}
}

// WithRejectFunc replaces the default plain-text 429 response. Retry-After is
// already set when the function runs.
func WithRejectFunc(fn middleware.RejectFunc) Option {
	return func(c *config) {
		c.reject = fn
		c.rejectSet = true
	}
}

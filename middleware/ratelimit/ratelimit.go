// Package ratelimit limits the rate of requests per client.
//
// The algorithm is GCRA, equivalent to a token bucket: a client can send up
// to the burst size back to back, and its allowance recovers continuously at
// limit requests per period. A request beyond the allowance is refused with
// 429 Too Many Requests and a Retry-After header with the number of seconds
// until a request would be allowed. Other clients are unaffected.
//
// Clients are identified by the IP address of r.RemoteAddr, so realip should
// run before this middleware behind proxies. IPv6 addresses are grouped by
// their /64 prefix, since a single subscriber usually controls a whole /64.
//
// The state is in memory, per middleware instance and per process: replicas
// of a service do not share their counters, and the same instance placed in
// two chains shares its counters across them. The state of a client is
// reclaimed once its allowance has fully recovered, by later requests that
// reach the same shard; no background goroutine is used, so reclamation needs
// follow-up traffic.
//
// Mandatory settings: the number of requests allowed per period.
//
// Options and defaults:
//   - [WithBurst]: the limit.
//   - [WithKey]: the client IP address (IPv6 by /64).
//   - [WithRejectFunc]: a plain-text 429 response, with Rejection.Err
//     matching [ErrLimitExceeded].
//
// Recommended position: after realip, secureheaders and cors, before
// maxconcurrency and the middleware that read the request body.
package ratelimit

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/paccolamano/go/middleware"
	"github.com/paccolamano/go/middleware/internal/netaddr"
	"github.com/paccolamano/go/middleware/internal/respwriter"
)

// ErrLimitExceeded is the Rejection.Err when a client exceeds its allowance.
var ErrLimitExceeded = errors.New("ratelimit: rate limit exceeded")

// New returns a middleware that allows limit requests per period and client.
func New(limit int, period time.Duration, opts ...Option) (middleware.Middleware, error) {
	if limit <= 0 {
		return nil, fmt.Errorf("%w: ratelimit: limit: must be greater than 0", middleware.ErrInvalidConfig)
	}
	if period <= 0 || period/time.Duration(limit) < time.Nanosecond {
		return nil, fmt.Errorf("%w: ratelimit: period: must be greater than 0 and at least 1ns per request", middleware.ErrInvalidConfig)
	}
	cfg := config{burst: limit, key: defaultKey}
	for _, opt := range opts {
		opt(&cfg)
	}
	switch {
	case cfg.burstSet && cfg.burst <= 0:
		return nil, fmt.Errorf("%w: ratelimit: WithBurst: must be greater than 0", middleware.ErrInvalidConfig)
	case cfg.keySet && cfg.key == nil:
		return nil, fmt.Errorf("%w: ratelimit: WithKey: the function must not be nil", middleware.ErrInvalidConfig)
	case cfg.rejectSet && cfg.reject == nil:
		return nil, fmt.Errorf("%w: ratelimit: WithRejectFunc: the function must not be nil", middleware.ErrInvalidConfig)
	}
	interval := period / time.Duration(limit)
	s := newStore(interval, time.Duration(cfg.burst)*interval)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			allowed, retry := s.allow(cfg.key(r), time.Now())
			if !allowed {
				w.Header().Set("Retry-After", strconv.FormatInt(retrySeconds(retry), 10))
				respwriter.Reject(w, r, cfg.reject, middleware.Rejection{Status: http.StatusTooManyRequests, Err: ErrLimitExceeded})
				return
			}
			next.ServeHTTP(w, r)
		})
	}, nil
}

func defaultKey(r *http.Request) string {
	if addr, ok := netaddr.ParseRemoteAddr(r.RemoteAddr); ok {
		return netaddr.Key(addr)
	}
	return r.RemoteAddr
}

// retrySeconds rounds d up to whole seconds, with a minimum of one.
func retrySeconds(d time.Duration) int64 {
	secs := int64((d + time.Second - 1) / time.Second)
	return max(secs, 1)
}

// Package maxconcurrency limits the number of requests processed at the same
// time, so that a service degrades predictably under load instead of
// collapsing.
//
// When the limit is reached, a new request is refused immediately with 503
// Service Unavailable, without invoking the next handler. With [WithQueue], a
// bounded number of requests can instead wait for a slot, up to a maximum
// time; a waiting request is refused when the wait expires, when the queue is
// full or when its client goes away. A slot is released when its request ends,
// however it ends: normal completion, panic or client disconnection.
//
// Long-lived requests, such as WebSocket connections or server-sent event
// streams, hold their slot until their handler returns: apply the middleware
// only to routes where that is acceptable. The limit is per middleware
// instance: the same instance placed in two chains shares its slots.
//
// Mandatory settings: the maximum number of concurrent requests.
//
// Options and defaults:
//   - [WithQueue]: no queue.
//   - [WithRetryAfter]: no Retry-After header.
//   - [WithRejectFunc]: a plain-text 503 response, with Rejection.Err
//     matching [ErrOverloaded].
//
// Recommended position: after ratelimit and before bodylimit.
package maxconcurrency

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/paccolamano/go/middleware"
	"github.com/paccolamano/go/middleware/internal/respwriter"
)

// ErrOverloaded is the Rejection.Err when no slot is available.
var ErrOverloaded = errors.New("maxconcurrency: too many concurrent requests")

// Option configures the middleware returned by New.
type Option func(*config)

type config struct {
	queueSize  int
	maxWait    time.Duration
	queueSet   bool
	retryAfter time.Duration
	retrySet   bool
	reject     middleware.RejectFunc
	rejectSet  bool
}

// WithQueue lets up to size requests wait for a slot, each for at most
// maxWait.
func WithQueue(size int, maxWait time.Duration) Option {
	return func(c *config) {
		c.queueSize, c.maxWait, c.queueSet = size, maxWait, true
	}
}

// WithRetryAfter adds a Retry-After header to refusals, in whole seconds
// rounded up. It must be at least one second.
func WithRetryAfter(d time.Duration) Option {
	return func(c *config) {
		c.retryAfter, c.retrySet = d, true
	}
}

// WithRejectFunc replaces the default plain-text 503 response.
func WithRejectFunc(fn middleware.RejectFunc) Option {
	return func(c *config) {
		c.reject, c.rejectSet = fn, true
	}
}

// New returns a middleware that processes at most limit requests at a time.
func New(limit int, opts ...Option) (middleware.Middleware, error) {
	if limit <= 0 {
		return nil, fmt.Errorf("%w: maxconcurrency: limit: must be greater than 0", middleware.ErrInvalidConfig)
	}
	var cfg config
	for _, opt := range opts {
		opt(&cfg)
	}
	switch {
	case cfg.queueSet && (cfg.queueSize <= 0 || cfg.maxWait <= 0):
		return nil, fmt.Errorf("%w: maxconcurrency: WithQueue: size and maxWait must be greater than 0", middleware.ErrInvalidConfig)
	case cfg.retrySet && cfg.retryAfter < time.Second:
		return nil, fmt.Errorf("%w: maxconcurrency: WithRetryAfter: must be at least 1s", middleware.ErrInvalidConfig)
	case cfg.rejectSet && cfg.reject == nil:
		return nil, fmt.Errorf("%w: maxconcurrency: WithRejectFunc: the function must not be nil", middleware.ErrInvalidConfig)
	}
	retryAfter := ""
	if cfg.retrySet {
		retryAfter = strconv.FormatInt(int64((cfg.retryAfter+time.Second-1)/time.Second), 10)
	}
	l := &limiter{sem: make(chan struct{}, limit), queueSize: int64(cfg.queueSize), maxWait: cfg.maxWait}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !l.acquire(r) {
				if retryAfter != "" {
					w.Header().Set("Retry-After", retryAfter)
				}
				respwriter.Reject(w, r, cfg.reject, middleware.Rejection{Status: http.StatusServiceUnavailable, Err: ErrOverloaded})
				return
			}
			defer l.release()
			next.ServeHTTP(w, r)
		})
	}, nil
}

type limiter struct {
	sem       chan struct{}
	waiting   atomic.Int64
	queueSize int64
	maxWait   time.Duration
}

func (l *limiter) acquire(r *http.Request) bool {
	select {
	case l.sem <- struct{}{}:
		return true
	default:
	}
	if l.queueSize == 0 {
		return false
	}
	if l.waiting.Add(1) > l.queueSize {
		l.waiting.Add(-1)
		return false
	}
	defer l.waiting.Add(-1)
	timer := time.NewTimer(l.maxWait)
	defer timer.Stop()
	select {
	case l.sem <- struct{}{}:
		return true
	case <-timer.C:
		return false
	case <-r.Context().Done():
		return false
	}
}

func (l *limiter) release() {
	<-l.sem
}

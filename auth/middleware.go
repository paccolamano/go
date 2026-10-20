package auth

import (
	"context"
	"errors"
	"fmt"
	"net/http"
)

// Rejection describes a request that the authentication middleware or
// RequireMethods refuses.
//
// Fields may be added in minor releases; code that reads a Rejection keeps
// compiling and working.
type Rejection struct {
	// Status is the HTTP status code: 401, 403, 429 or 500.
	Status int
	// Err identifies the cause. Compare it with errors.Is against
	// ErrNoCredentials, ErrInvalidCredentials, ErrTooManyAttempts or
	// ErrInsufficientMethods; for a 500 it is the failure. It must not be
	// written to clients verbatim.
	Err error
}

// RejectFunc writes the response for a refused request. The middleware sets
// WWW-Authenticate before calling it. If the function writes neither a status
// nor a body, the middleware sends Rejection.Status with an empty body.
type RejectFunc func(w http.ResponseWriter, r *http.Request, rej Rejection)

// Option configures New.
type Option func(*config)

type config struct {
	anonymous bool
	reject    RejectFunc
	rejectSet bool
}

// WithAnonymous lets requests without credentials through, with no principal
// in their context. Requests with invalid credentials are still rejected.
func WithAnonymous() Option {
	return func(c *config) { c.anonymous = true }
}

// WithRejectFunc replaces the default plain-text response for refused
// requests, for example with an RFC 9457 problem document. RequireMethods uses
// the same function.
func WithRejectFunc(fn RejectFunc) Option {
	return func(c *config) {
		c.reject = fn
		c.rejectSet = true
	}
}

// state is what the middleware stores in the request context.
type state struct {
	principal  *Principal
	decider    Authenticator
	challenges []string
	reject     RejectFunc
}

type stateKey struct{}

// New returns the authentication middleware. For each request it runs a and:
//   - stores the principal in the request context, readable with FromContext,
//     and calls the next handler;
//   - rejects a request with no credentials with 401 and the methods'
//     challenges, unless WithAnonymous is set;
//   - rejects invalid credentials with 401, also with WithAnonymous;
//   - rejects a request refused by an attempt limit with 429;
//   - answers any other error with 500, passing it to the RejectFunc but never
//     writing it to the response.
func New(a Authenticator, opts ...Option) (func(http.Handler) http.Handler, error) {
	if err := validate(a); err != nil {
		return nil, err
	}
	var cfg config
	for _, o := range opts {
		if o == nil {
			return nil, fmt.Errorf("%w: auth.New: options: must not be nil", ErrInvalidConfig)
		}
		o(&cfg)
	}
	if cfg.rejectSet && cfg.reject == nil {
		return nil, fmt.Errorf("%w: auth.New: WithRejectFunc: the function must not be nil", ErrInvalidConfig)
	}
	ls := leaves(a)
	plain := plainChallenges(ls)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			p, i, err := decide(ls, r)
			st := &state{principal: p, challenges: plain, reject: cfg.reject}
			if i >= 0 {
				st.decider = ls[i]
			}
			switch {
			case err == nil:
			case errors.Is(err, ErrNoCredentials):
				if !cfg.anonymous {
					writeRejection(w, r, cfg.reject, Rejection{Status: http.StatusUnauthorized, Err: err}, plain)
					return
				}
			case errors.Is(err, ErrTooManyAttempts):
				writeRejection(w, r, cfg.reject, Rejection{Status: http.StatusTooManyRequests, Err: err}, nil)
				return
			case errors.Is(err, ErrInvalidCredentials):
				writeRejection(w, r, cfg.reject, Rejection{Status: http.StatusUnauthorized, Err: err}, invalidChallenges(ls, i))
				return
			default:
				writeRejection(w, r, cfg.reject, Rejection{Status: http.StatusInternalServerError, Err: err}, nil)
				return
			}
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), stateKey{}, st)))
		})
	}, nil
}

// FromContext returns the principal that the authentication middleware stored
// in ctx. It reports false when the middleware did not run or let an anonymous
// request through.
func FromContext(ctx context.Context) (*Principal, bool) {
	st, ok := ctx.Value(stateKey{}).(*state)
	if !ok || st.principal == nil {
		return nil, false
	}
	return st.principal, true
}

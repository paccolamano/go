package auth

import (
	"errors"
	"fmt"
	"net/http"
)

// errContract reports a method that returned neither a valid principal nor an
// error.
var errContract = errors.New("auth: authenticator returned neither a principal with a subject nor an error")

// anyAuth is the combination returned by Any. Its leaves are flattened at
// construction, so nested combinations behave like one ordered list.
type anyAuth struct {
	methods []Authenticator
	leaves  []Authenticator
}

// Any combines methods, tried in order:
//   - the first principal is the result;
//   - an error matching ErrNoCredentials moves on to the next method;
//   - any other error stops the combination and is its result.
//
// When every method reports no credentials, so does the combination. Nested
// combinations behave like one flat list. New rejects a combination without
// methods or with a nil method.
func Any(methods ...Authenticator) Authenticator {
	a := &anyAuth{methods: append([]Authenticator(nil), methods...)}
	a.leaves = leaves(a)
	return a
}

// Authenticate tries the combined methods in order.
func (a *anyAuth) Authenticate(r *http.Request) (*Principal, error) {
	p, _, err := decide(a.leaves, r)
	return p, err
}

// leaves flattens nested combinations into their methods, in order.
func leaves(a Authenticator) []Authenticator {
	c, ok := a.(*anyAuth)
	if !ok {
		return []Authenticator{a}
	}
	var out []Authenticator
	for _, m := range c.methods {
		out = append(out, leaves(m)...)
	}
	return out
}

// decide runs the leaves in order and returns the outcome and the index of the
// leaf that decided it, or -1 when every leaf reported no credentials.
func decide(ls []Authenticator, r *http.Request) (*Principal, int, error) {
	for i, l := range ls {
		p, err := l.Authenticate(r)
		switch {
		case err == nil && (p == nil || p.Subject == ""):
			return nil, i, fmt.Errorf("%T: %w", l, errContract)
		case err == nil:
			return p, i, nil
		case errors.Is(err, ErrNoCredentials):
			continue
		default:
			return nil, i, fmt.Errorf("auth: %w", err)
		}
	}
	return nil, -1, ErrNoCredentials
}

// validate reports a nil method, or a combination without methods, at any
// depth.
func validate(a Authenticator) error {
	if a == nil {
		return fmt.Errorf("%w: auth.New: authenticator: must not be nil", ErrInvalidConfig)
	}
	c, ok := a.(*anyAuth)
	if !ok {
		return nil
	}
	if len(c.methods) == 0 {
		return fmt.Errorf("%w: auth.New: Any: needs at least one method", ErrInvalidConfig)
	}
	for _, m := range c.methods {
		if err := validate(m); err != nil {
			return err
		}
	}
	return nil
}

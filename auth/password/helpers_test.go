package password_test

import (
	"context"
	"sync"
	"testing"

	"github.com/paccolamano/go/auth/password"
)

// floor is the cheapest configuration the checker accepts, used to keep tests
// fast.
func floor() password.Option {
	return password.WithParams(password.Params{Memory: 7168, Iterations: 5, Parallelism: 1})
}

// accounts is a map-backed lookup.
type accounts struct {
	mu    sync.Mutex
	m     map[string]password.Account
	err   error
	calls int
	ctxs  []context.Context
}

func (a *accounts) lookup(ctx context.Context, username string) (password.Account, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.calls++
	a.ctxs = append(a.ctxs, ctx)
	if a.err != nil {
		return password.Account{}, a.err
	}
	acct, ok := a.m[username]
	if !ok {
		return password.Account{}, password.ErrNotFound
	}
	return acct, nil
}

func mustChecker(tb testing.TB, lookup password.LookupFunc, opts ...password.Option) *password.Checker {
	tb.Helper()
	c, err := password.NewChecker(lookup, opts...)
	if err != nil {
		tb.Fatalf("NewChecker: %v", err)
	}
	return c
}

func mustHash(tb testing.TB, c *password.Checker, plain string) string {
	tb.Helper()
	h, err := c.Hash(plain)
	if err != nil {
		tb.Fatalf("Hash: %v", err)
	}
	return h
}

package recovery_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync"
	"testing"

	"github.com/paccolamano/go/auth"
	"github.com/paccolamano/go/auth/recovery"
	"github.com/paccolamano/go/auth/session"
)

// codes stands in for the developer's table of recovery code hashes.
type codes struct {
	mu       sync.Mutex
	unused   map[string][]string
	err      error
	consumed []string
	// racy makes consume ignore whether the code was already used, as a
	// careless implementation might.
	racy bool
}

func (c *codes) lookup(_ context.Context, subject string) ([]string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.err != nil {
		return nil, c.err
	}
	return slices.Clone(c.unused[subject]), nil
}

func (c *codes) consume(_ context.Context, subject, hash string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.consumed = append(c.consumed, hash)
	i := slices.Index(c.unused[subject], hash)
	if i < 0 {
		if c.racy {
			return nil
		}
		return recovery.ErrNotFound
	}
	c.unused[subject] = slices.Delete(c.unused[subject], i, i+1)
	return nil
}

func mustSessions(tb testing.TB, store auth.Store) *session.Manager {
	tb.Helper()
	m, err := session.New(store)
	if err != nil {
		tb.Fatal(err)
	}
	return m
}

func newPending(tb testing.TB, m *session.Manager, subject string) auth.Pending {
	tb.Helper()
	rec := httptest.NewRecorder()
	ctx := tb.Context()
	if err := m.IssuePending(rec, httptest.NewRequestWithContext(ctx, http.MethodPost, "/login", nil), &auth.Principal{Subject: subject, Methods: []string{"pwd"}}); err != nil {
		tb.Fatal(err)
	}
	r := httptest.NewRequestWithContext(ctx, http.MethodPost, "/login/recovery", nil)
	for _, c := range rec.Result().Cookies() {
		r.AddCookie(c)
	}
	p, err := m.Pending(r)
	if err != nil {
		tb.Fatal(err)
	}
	return p
}

func mustGenerate(tb testing.TB, n int) *recovery.Codes {
	tb.Helper()
	c, err := recovery.Generate(n)
	if err != nil {
		tb.Fatal(err)
	}
	return c
}

package session_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/paccolamano/go/auth"
	"github.com/paccolamano/go/auth/internal/storetest"
	"github.com/paccolamano/go/auth/memstore"
	"github.com/paccolamano/go/auth/session"
)

// issuePending issues a pending login and returns its cookie.
func issuePending(tb testing.TB, m *session.Manager, p *auth.Principal, cookies ...*http.Cookie) *http.Cookie {
	tb.Helper()
	rec := httptest.NewRecorder()
	if err := m.IssuePending(rec, request(tb, cookies...), p); err != nil {
		tb.Fatalf("IssuePending: %v", err)
	}
	c := cookie(rec, pendingCookie)
	if c == nil {
		tb.Fatal("IssuePending set no pending cookie")
	}
	return c
}

func mustPending(tb testing.TB, m *session.Manager, c *http.Cookie) auth.Pending {
	tb.Helper()
	p, err := m.Pending(request(tb, c))
	if err != nil {
		tb.Fatalf("Pending: %v", err)
	}
	return p
}

func fail(context.Context) error { return auth.ErrInvalidCredentials }
func pass(context.Context) error { return nil }

func TestPending_Cookie(t *testing.T) {
	c := issuePending(t, mustManager(t, memstore.New()), alice())
	if c.MaxAge != 300 || c.Path != "/" || !c.Secure || !c.HttpOnly || c.SameSite != http.SameSiteLaxMode || c.Domain != "" {
		t.Fatalf("pending cookie = %+v", c)
	}
}

func TestPending_NotAuthenticated(t *testing.T) {
	m := mustManager(t, memstore.New())
	c := issuePending(t, m, alice())
	if _, err := m.Authenticate(request(t, c)); !errors.Is(err, auth.ErrNoCredentials) {
		t.Fatalf("Authenticate with only a pending cookie: %v", err)
	}
}

func TestPending_Read(t *testing.T) {
	m := mustManager(t, memstore.New())
	p := mustPending(t, m, issuePending(t, m, alice())).Principal()
	if p.Subject != "user-1" || !slices.Equal(p.Methods, []string{"pwd"}) {
		t.Fatalf("Principal = %+v", p)
	}
	if _, err := m.Pending(request(t)); !errors.Is(err, auth.ErrNoCredentials) {
		t.Fatalf("Pending without cookie: %v", err)
	}
}

func TestPending_Expired(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		m := mustManager(t, memstore.New())
		c := issuePending(t, m, alice())
		time.Sleep(5 * time.Minute)
		if _, err := m.Pending(request(t, c)); !errors.Is(err, auth.ErrNoCredentials) {
			t.Fatalf("after 5 minutes: %v", err)
		}
	})
}

func TestPending_Replaced(t *testing.T) {
	m := mustManager(t, memstore.New())
	first := issuePending(t, m, alice())
	second := issuePending(t, m, alice(), first)
	if _, err := m.Pending(request(t, first)); !errors.Is(err, auth.ErrNoCredentials) {
		t.Fatalf("first pending login after a second: %v", err)
	}
	mustPending(t, m, second)
}

func TestPending_DeletedByIssueAndRevoke(t *testing.T) {
	m := mustManager(t, memstore.New())
	c := issuePending(t, m, alice())
	rec := httptest.NewRecorder()
	if err := m.Issue(rec, request(t, c), alice()); err != nil {
		t.Fatal(err)
	}
	if got := cookie(rec, pendingCookie); got == nil || got.MaxAge >= 0 {
		t.Fatalf("pending cookie not cleared by Issue: %+v", got)
	}
	if _, err := m.Pending(request(t, c)); !errors.Is(err, auth.ErrNoCredentials) {
		t.Fatalf("pending login after Issue: %v", err)
	}

	c = issuePending(t, m, alice())
	if err := m.Revoke(httptest.NewRecorder(), request(t, c)); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Pending(request(t, c)); !errors.Is(err, auth.ErrNoCredentials) {
		t.Fatalf("pending login after Revoke: %v", err)
	}
}

func TestAttempt_Budget(t *testing.T) {
	ctx := t.Context()
	m := mustManager(t, memstore.New())

	c := issuePending(t, m, alice())
	p := mustPending(t, m, c)
	for range 4 {
		_ = p.Attempt(ctx, fail)
	}
	if err := p.Attempt(ctx, pass); err != nil {
		t.Fatalf("fifth attempt succeeding: %v", err)
	}

	c = issuePending(t, m, alice())
	p = mustPending(t, m, c)
	var err error
	for range 5 {
		err = p.Attempt(ctx, fail)
	}
	if !errors.Is(err, auth.ErrTooManyAttempts) || !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Fatalf("fifth failure = %v, want both ErrTooManyAttempts and ErrInvalidCredentials", err)
	}
	if _, err := m.Pending(request(t, c)); !errors.Is(err, auth.ErrNoCredentials) {
		t.Fatalf("pending login after five failures: %v", err)
	}
	called := false
	err = p.Attempt(ctx, func(context.Context) error { called = true; return nil })
	if !errors.Is(err, auth.ErrTooManyAttempts) || called {
		t.Fatalf("sixth attempt = %v, verify called %v", err, called)
	}
}

func TestAttempt_CancelError(t *testing.T) {
	ctx := t.Context()
	store := memstore.New()
	c := issuePending(t, mustManager(t, store), alice())
	p := mustPending(t, mustManager(t, storetest.Failing{Next: store, Err: errStore, Ops: []storetest.Op{storetest.OpDelete}}), c)
	var err error
	for range 5 {
		err = p.Attempt(ctx, fail)
	}
	if !errors.Is(err, auth.ErrTooManyAttempts) || !errors.Is(err, errStore) {
		t.Fatalf("fifth failure with a failing delete = %v, want both errors", err)
	}
}

func TestAttempt_StoreError(t *testing.T) {
	ctx := t.Context()
	store := memstore.New()
	m := mustManager(t, store)
	c := issuePending(t, m, alice())
	failing := mustPending(t, mustManager(t, storetest.Failing{Next: store, Err: errStore, Ops: []storetest.Op{storetest.OpIncr}}), c)
	called := false
	if err := failing.Attempt(ctx, func(context.Context) error { called = true; return nil }); !errors.Is(err, errStore) || called {
		t.Fatalf("Attempt with failing Incr = %v, verify called %v", err, called)
	}
	p := mustPending(t, m, c)
	for i := range 4 {
		if err := p.Attempt(ctx, fail); errors.Is(err, auth.ErrTooManyAttempts) {
			t.Fatalf("attempt %d already exhausted the budget", i+1)
		}
	}
	if err := p.Attempt(ctx, pass); err != nil {
		t.Fatalf("fifth attempt: %v", err)
	}
}

func TestAttempt_OtherError(t *testing.T) {
	ctx := t.Context()
	m := mustManager(t, memstore.New())
	p := mustPending(t, m, issuePending(t, m, alice()))
	boom := errors.New("x")
	if err := p.Attempt(ctx, func(context.Context) error { return boom }); !errors.Is(err, boom) {
		t.Fatalf("Attempt = %v, want the verify error", err)
	}
	for range 3 {
		_ = p.Attempt(ctx, fail)
	}
	if err := p.Attempt(ctx, fail); !errors.Is(err, auth.ErrTooManyAttempts) {
		t.Fatalf("the failure was not counted: %v", err)
	}
}

func TestAttempt_Parallel(t *testing.T) {
	ctx := t.Context()
	m := mustManager(t, memstore.New())
	p := mustPending(t, m, issuePending(t, m, alice()))
	var (
		wg    sync.WaitGroup
		calls atomic.Int32
	)
	for range 50 {
		wg.Go(func() {
			_ = p.Attempt(ctx, func(context.Context) error { calls.Add(1); return auth.ErrInvalidCredentials })
		})
	}
	wg.Wait()
	if n := calls.Load(); n > 5 {
		t.Fatalf("verify called %d times, want at most 5", n)
	}
}

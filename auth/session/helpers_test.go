package session_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/paccolamano/go/auth"
	"github.com/paccolamano/go/auth/memstore"
	"github.com/paccolamano/go/auth/session"
)

const (
	sessionCookie = "__Host-session"
	pendingCookie = "__Host-session-pending"
)

func mustManager(tb testing.TB, store auth.Store, opts ...session.Option) *session.Manager {
	tb.Helper()
	m, err := session.New(store, opts...)
	if err != nil {
		tb.Fatalf("session.New: %v", err)
	}
	return m
}

func request(tb testing.TB, cookies ...*http.Cookie) *http.Request {
	tb.Helper()
	r := httptest.NewRequestWithContext(tb.Context(), http.MethodGet, "/", nil)
	for _, c := range cookies {
		if c != nil {
			r.AddCookie(c)
		}
	}
	return r
}

func cookie(rec *httptest.ResponseRecorder, name string) *http.Cookie {
	for _, c := range rec.Result().Cookies() {
		if c.Name == name {
			return c
		}
	}
	return nil
}

// issue issues a session for p on a request carrying cookies and returns the
// session cookie.
func issue(tb testing.TB, m *session.Manager, p *auth.Principal, cookies ...*http.Cookie) *http.Cookie {
	tb.Helper()
	rec := httptest.NewRecorder()
	if err := m.Issue(rec, request(tb, cookies...), p); err != nil {
		tb.Fatalf("Issue: %v", err)
	}
	c := cookie(rec, sessionCookie)
	if c == nil {
		tb.Fatal("Issue set no session cookie")
	}
	return c
}

func alice() *auth.Principal { return &auth.Principal{Subject: "user-1", Methods: []string{"pwd"}} }

// requestCookie returns a cookie as a client sends it: a name and a value.
func requestCookie(tb testing.TB, name, value string) *http.Cookie {
	tb.Helper()
	cs, err := http.ParseCookie(name + "=" + value)
	if err != nil || len(cs) != 1 {
		tb.Fatalf("ParseCookie: %v", err)
	}
	return cs[0]
}

// point is where interleaved runs its function within a renewal.
type point int

const (
	beforeRead point = iota
	betweenReadAndWrite
	afterWrite
	beforeRevocationRead
)

// revocationPrefix starts the store key of a subject's revocation state.
const revocationPrefix = "auth:session-gen:"

// interleaved simulates a concurrent request whose store calls land at a
// chosen point of a renewal: it runs run once, at the point named by at:
// before the Get, between the Get and the write, after the write, or before
// the read of a revocation state.
type interleaved struct {
	*memstore.Store
	at  point
	run func()
}

// fire calls run once. Store calls made by run itself do not fire again.
func (s *interleaved) fire() {
	if f := s.run; f != nil {
		s.run = nil
		f()
	}
}

func (s *interleaved) Get(ctx context.Context, key string) ([]byte, error) {
	if s.at == beforeRead || s.at == beforeRevocationRead && strings.HasPrefix(key, revocationPrefix) {
		s.fire()
	}
	return s.Store.Get(ctx, key)
}

func (s *interleaved) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	if s.at == betweenReadAndWrite {
		s.fire()
	}
	err := s.Store.Set(ctx, key, value, ttl)
	if s.at == afterWrite {
		s.fire()
	}
	return err
}

func (s *interleaved) Replace(ctx context.Context, key string, value []byte, ttl time.Duration) (bool, error) {
	if s.at == betweenReadAndWrite {
		s.fire()
	}
	ok, err := s.Store.Replace(ctx, key, value, ttl)
	if s.at == afterWrite {
		s.fire()
	}
	return ok, err
}

// failingRevocation fails the reads and writes of revocation states with
// errStore, and forwards every other call.
type failingRevocation struct{ auth.Store }

func (s failingRevocation) Get(ctx context.Context, key string) ([]byte, error) {
	if strings.HasPrefix(key, revocationPrefix) {
		return nil, errStore
	}
	return s.Store.Get(ctx, key)
}

func (s failingRevocation) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	if strings.HasPrefix(key, revocationPrefix) {
		return errStore
	}
	return s.Store.Set(ctx, key, value, ttl)
}

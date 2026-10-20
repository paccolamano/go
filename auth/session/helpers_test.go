package session_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/paccolamano/go/auth"
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

package totp_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/paccolamano/go/auth"
	"github.com/paccolamano/go/auth/internal/hotp"
	"github.com/paccolamano/go/auth/session"
	"github.com/paccolamano/go/auth/totp"
)

var secret = []byte("12345678901234567890")

// secrets is a map-backed secret lookup.
type secrets struct {
	mu  sync.Mutex
	m   map[string][]byte
	err error
}

func (s *secrets) lookup(_ context.Context, subject string) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return nil, s.err
	}
	b, ok := s.m[subject]
	if !ok {
		return nil, totp.ErrNotFound
	}
	return b, nil
}

// code returns the code of the step that is offset steps from now.
func code(offset int64) string {
	return hotp.Value(hotp.SHA1, secret, uint64(time.Now().Unix()/30+offset), 6)
}

func mustVerifier(tb testing.TB, lookup totp.LookupFunc, store auth.Store) *totp.Verifier {
	tb.Helper()
	v, err := totp.New(lookup, store)
	if err != nil {
		tb.Fatalf("totp.New: %v", err)
	}
	return v
}

func mustSessions(tb testing.TB, store auth.Store) *session.Manager {
	tb.Helper()
	m, err := session.New(store)
	if err != nil {
		tb.Fatal(err)
	}
	return m
}

// newPending issues a pending login for subject and returns it.
func newPending(tb testing.TB, m *session.Manager, subject string) auth.Pending {
	tb.Helper()
	rec := httptest.NewRecorder()
	ctx := tb.Context()
	if err := m.IssuePending(rec, httptest.NewRequestWithContext(ctx, http.MethodPost, "/login", nil), &auth.Principal{Subject: subject, Methods: []string{"pwd"}}); err != nil {
		tb.Fatal(err)
	}
	r := httptest.NewRequestWithContext(ctx, http.MethodPost, "/login/otp", nil)
	for _, c := range rec.Result().Cookies() {
		r.AddCookie(c)
	}
	p, err := m.Pending(r)
	if err != nil {
		tb.Fatal(err)
	}
	return p
}

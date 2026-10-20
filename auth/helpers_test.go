package auth_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/paccolamano/go/auth"
)

// stub is a method that returns a fixed outcome and fixed challenges.
type stub struct {
	p          *auth.Principal
	err        error
	challenges map[error]string
	calls      *atomic.Int32
}

func (s stub) Authenticate(*http.Request) (*auth.Principal, error) {
	if s.calls != nil {
		s.calls.Add(1)
	}
	return s.p, s.err
}

func (s stub) Challenge(cause error) string { return s.challenges[cause] }

// plain is a method without challenges.
type plain struct {
	p   *auth.Principal
	err error
}

func (s plain) Authenticate(*http.Request) (*auth.Principal, error) { return s.p, s.err }

func principal(subject string, methods ...string) *auth.Principal {
	return &auth.Principal{Subject: subject, Methods: methods}
}

func newRequest(tb testing.TB) *http.Request {
	tb.Helper()
	return httptest.NewRequestWithContext(tb.Context(), http.MethodGet, "/", nil)
}

func mustNew(tb testing.TB, a auth.Authenticator, opts ...auth.Option) func(http.Handler) http.Handler {
	tb.Helper()
	mw, err := auth.New(a, opts...)
	if err != nil {
		tb.Fatalf("New: %v", err)
	}
	return mw
}

// serve runs r through mw and a handler that records the principal it sees
// and writes "200 ok".
func serve(tb testing.TB, mw func(http.Handler) http.Handler, r *http.Request) (*httptest.ResponseRecorder, *auth.Principal, bool) {
	tb.Helper()
	var (
		p  *auth.Principal
		ok bool
	)
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p, ok = auth.FromContext(r.Context())
		_, _ = io.WriteString(w, "ok")
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	return rec, p, ok
}

func challenges(rec *httptest.ResponseRecorder) []string {
	return rec.Result().Header.Values("WWW-Authenticate")
}

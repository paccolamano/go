package basic_test

import (
	"context"
	"encoding/base64"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/paccolamano/go/auth"
	"github.com/paccolamano/go/auth/basic"
	"github.com/paccolamano/go/auth/memstore"
	"github.com/paccolamano/go/auth/password"
)

// stubChecker records its calls and returns a fixed outcome.
type stubChecker struct {
	mu    sync.Mutex
	calls [][2]string
	ctxs  []context.Context
	p     *auth.Principal
	err   error
}

func (s *stubChecker) Check(ctx context.Context, username, password string) (*auth.Principal, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, [2]string{username, password})
	s.ctxs = append(s.ctxs, ctx)
	return s.p, s.err
}

func basicHeader(user, pass string) string {
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(user+":"+pass))
}

func request(tb testing.TB, authorization string) *http.Request {
	tb.Helper()
	r := httptest.NewRequestWithContext(tb.Context(), http.MethodGet, "/", nil)
	if authorization != "" {
		r.Header.Set("Authorization", authorization)
	}
	return r
}

func mustBasic(tb testing.TB, c basic.Checker, opts ...basic.Option) *basic.Authenticator {
	tb.Helper()
	a, err := basic.New(c, opts...)
	if err != nil {
		tb.Fatal(err)
	}
	return a
}

type ctxKey struct{}

func TestAuthenticate_Success(t *testing.T) {
	stub := &stubChecker{p: &auth.Principal{Subject: "user-1", Methods: []string{"pwd"}}}
	a := mustBasic(t, stub)
	r := request(t, basicHeader("alice", "hunter2"))
	r = r.WithContext(context.WithValue(r.Context(), ctxKey{}, "request"))
	p, err := a.Authenticate(r)
	if err != nil || p != stub.p {
		t.Fatalf("Authenticate = %v, %v", p, err)
	}
	if stub.calls[0] != [2]string{"alice", "hunter2"} || stub.ctxs[0].Value(ctxKey{}) != "request" {
		t.Fatalf("checker got %v and the wrong context", stub.calls[0])
	}
}

func TestAuthenticate_Rejected(t *testing.T) {
	a := mustBasic(t, &stubChecker{err: auth.ErrInvalidCredentials}, basic.WithRealm("api"))
	mw, _ := auth.New(a)
	rec := httptest.NewRecorder()
	mw(http.NotFoundHandler()).ServeHTTP(rec, request(t, basicHeader("alice", "wrong")))
	want := []string{`Basic realm="api", charset="UTF-8"`}
	if got := rec.Result().Header.Values("WWW-Authenticate"); rec.Code != 401 || !slices.Equal(got, want) {
		t.Fatalf("got %d %q", rec.Code, got)
	}
}

func TestAuthenticate_Malformed(t *testing.T) {
	stub := &stubChecker{p: &auth.Principal{Subject: "x"}}
	a := mustBasic(t, stub)
	noPad := strings.TrimRight(base64.StdEncoding.EncodeToString([]byte("al:b")), "=")
	for _, h := range []string{
		"Basic !!!",
		"Basic " + noPad,
		"Basic " + base64.StdEncoding.EncodeToString([]byte("nocolon")),
		"Basic " + base64.StdEncoding.EncodeToString([]byte(":pass")),
		"Basic " + base64.StdEncoding.EncodeToString([]byte("al\xffice:pass")),
		"Basic " + base64.StdEncoding.EncodeToString([]byte("alice:pa\xffss")),
		"Basic",
	} {
		if _, err := a.Authenticate(request(t, h)); !errors.Is(err, auth.ErrInvalidCredentials) {
			t.Errorf("%q: %v, want invalid", h, err)
		}
	}
	if len(stub.calls) != 0 {
		t.Fatalf("the checker was called %d times for malformed headers", len(stub.calls))
	}
}

func TestAuthenticate_NotMine(t *testing.T) {
	stub := &stubChecker{p: &auth.Principal{Subject: "x"}}
	a := mustBasic(t, stub)
	for _, h := range []string{"", "Bearer x"} {
		if _, err := a.Authenticate(request(t, h)); !errors.Is(err, auth.ErrNoCredentials) {
			t.Errorf("%q: %v, want ErrNoCredentials", h, err)
		}
	}
	for _, scheme := range []string{"basic", "BASIC", "Basic"} {
		h := scheme + " " + base64.StdEncoding.EncodeToString([]byte("a:b"))
		if _, err := a.Authenticate(request(t, h)); err != nil {
			t.Errorf("scheme %q: %v", scheme, err)
		}
	}
}

func TestAuthenticate_Colons(t *testing.T) {
	stub := &stubChecker{p: &auth.Principal{Subject: "x"}}
	if _, err := mustBasic(t, stub).Authenticate(request(t, basicHeader("alice", "pa:ss:wörd"))); err != nil {
		t.Fatal(err)
	}
	if stub.calls[0] != [2]string{"alice", "pa:ss:wörd"} {
		t.Fatalf("checker got %q", stub.calls[0])
	}
}

func TestAuthenticate_RequireMFA(t *testing.T) {
	a := mustBasic(t, &stubChecker{p: &auth.Principal{Subject: "x", Methods: []string{"pwd"}}})
	mw, _ := auth.New(a)
	rec := httptest.NewRecorder()
	mw(auth.RequireMethods("mfa")(http.NotFoundHandler())).ServeHTTP(rec, request(t, basicHeader("a", "b")))
	if rec.Code != 403 {
		t.Fatalf("status %d, want 403", rec.Code)
	}
}

func TestAuthenticate_AttemptLimit(t *testing.T) {
	var hash string
	checker, err := password.NewChecker(func(context.Context, string) (password.Account, error) {
		return password.Account{Subject: "user-1", Hash: hash}, nil
	}, password.WithParams(password.Params{Memory: 7168, Iterations: 5, Parallelism: 1}),
		password.WithAttemptLimit(password.AttemptLimit{Store: memstore.New(), Max: 10}))
	if err != nil {
		t.Fatal(err)
	}
	hash, _ = checker.Hash("correct horse")
	mw, _ := auth.New(mustBasic(t, checker))
	h := mw(http.NotFoundHandler())
	for range 10 {
		h.ServeHTTP(httptest.NewRecorder(), request(t, basicHeader("alice", "wrong")))
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, request(t, basicHeader("alice", "correct horse")))
	if rec.Code != 429 {
		t.Fatalf("status %d, want 429", rec.Code)
	}
}

func TestRealm_Rules(t *testing.T) {
	stub := &stubChecker{}
	if got := mustBasic(t, stub).Challenge(auth.ErrNoCredentials); got != `Basic realm="restricted", charset="UTF-8"` {
		t.Fatalf("default challenge = %q", got)
	}
	if got := mustBasic(t, stub, basic.WithRealm(`a"b\c`)).Challenge(auth.ErrInvalidCredentials); got != `Basic realm="a\"b\\c", charset="UTF-8"` {
		t.Fatalf("escaped challenge = %q", got)
	}
	if got := mustBasic(t, stub).Challenge(auth.ErrInsufficientMethods); got != "" {
		t.Fatalf("step-up challenge = %q", got)
	}
	for _, realm := range []string{"", strings.Repeat("a", 129), "tab\there", "nonascii é", "nl\n"} {
		if _, err := basic.New(stub, basic.WithRealm(realm)); !errors.Is(err, auth.ErrInvalidConfig) {
			t.Errorf("realm %q: %v", realm, err)
		}
	}
	if _, err := basic.New(nil); !errors.Is(err, auth.ErrInvalidConfig) {
		t.Errorf("nil checker: %v", err)
	}
	if _, err := basic.New(stub, nil); !errors.Is(err, auth.ErrInvalidConfig) {
		t.Errorf("nil option: %v", err)
	}
}

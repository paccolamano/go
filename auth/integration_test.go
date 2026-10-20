package auth_test

import (
	"bytes"
	"context"
	"encoding/base32"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/paccolamano/go/auth"
	"github.com/paccolamano/go/auth/apikey"
	"github.com/paccolamano/go/auth/basic"
	"github.com/paccolamano/go/auth/internal/hotp"
	"github.com/paccolamano/go/auth/internal/storetest"
	"github.com/paccolamano/go/auth/jwt"
	"github.com/paccolamano/go/auth/memstore"
	"github.com/paccolamano/go/auth/password"
	"github.com/paccolamano/go/auth/recovery"
	"github.com/paccolamano/go/auth/session"
	"github.com/paccolamano/go/auth/totp"
)

var totpSecret = []byte("12345678901234567890")

// app wires every second-factor package to one store, as an application would.
type app struct {
	store     auth.Store
	checker   *password.Checker
	sessions  *session.Manager
	otp       *totp.Verifier
	recovery  *recovery.Verifier
	codes     *recovery.Codes
	unused    []string
	rejection auth.Rejection
	admin     http.Handler
}

func newApp(t *testing.T, store auth.Store) *app {
	t.Helper()
	a := &app{store: store}
	var hash string
	var err error
	a.checker, err = password.NewChecker(func(_ context.Context, username string) (password.Account, error) {
		if username != "alice" {
			return password.Account{}, password.ErrNotFound
		}
		return password.Account{Subject: "user-1", Hash: hash}, nil
	}, password.WithParams(password.Params{Memory: 7168, Iterations: 5, Parallelism: 1}))
	if err != nil {
		t.Fatal(err)
	}
	if hash, err = a.checker.Hash("correct horse"); err != nil {
		t.Fatal(err)
	}
	if a.sessions, err = session.New(store); err != nil {
		t.Fatal(err)
	}
	if a.otp, err = totp.New(func(_ context.Context, subject string) ([]byte, error) {
		if subject != "user-1" {
			return nil, totp.ErrNotFound
		}
		return totpSecret, nil
	}, store); err != nil {
		t.Fatal(err)
	}
	if a.codes, err = recovery.Generate(recovery.DefaultCount); err != nil {
		t.Fatal(err)
	}
	a.unused = a.codes.Hashes()
	if a.recovery, err = recovery.New(
		func(context.Context, string) ([]string, error) { return slices.Clone(a.unused), nil },
		func(_ context.Context, _, hash string) error {
			i := slices.Index(a.unused, hash)
			if i < 0 {
				return recovery.ErrNotFound
			}
			a.unused = slices.Delete(a.unused, i, i+1)
			return nil
		}, store); err != nil {
		t.Fatal(err)
	}
	mw, err := auth.New(a.sessions, auth.WithRejectFunc(func(w http.ResponseWriter, _ *http.Request, rej auth.Rejection) {
		a.rejection = rej
	}))
	if err != nil {
		t.Fatal(err)
	}
	a.admin = mw(auth.RequireMethods("mfa")(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})))
	return a
}

func currentCode(offset int64) string {
	return hotp.Value(hotp.SHA1, totpSecret, uint64(time.Now().Unix()/30+offset), 6)
}

func withCookies(tb testing.TB, rec *httptest.ResponseRecorder) *http.Request {
	tb.Helper()
	r := httptest.NewRequestWithContext(tb.Context(), http.MethodGet, "/", nil)
	for _, c := range rec.Result().Cookies() {
		if c.MaxAge >= 0 {
			r.AddCookie(c)
		}
	}
	return r
}

// login checks the password and issues a pending login, returning the request
// that carries it.
func (a *app) login(t *testing.T) *http.Request {
	t.Helper()
	p, err := a.checker.Check(t.Context(), "alice", "correct horse")
	if err != nil {
		t.Fatal(err)
	}
	if enrolled, err := a.otp.Enrolled(t.Context(), p.Subject); err != nil || !enrolled {
		t.Fatalf("Enrolled = %v, %v", enrolled, err)
	}
	rec := httptest.NewRecorder()
	if err := a.sessions.IssuePending(rec, httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/login", nil), p); err != nil {
		t.Fatal(err)
	}
	return withCookies(t, rec)
}

func (a *app) adminStatus(t *testing.T, r *http.Request) int {
	t.Helper()
	rec := httptest.NewRecorder()
	a.admin.ServeHTTP(rec, r)
	return rec.Code
}

func TestLoginFlow(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		a := newApp(t, memstore.New())
		ctx := t.Context()

		// Password, pending login, TOTP, session.
		r := a.login(t)
		if a.adminStatus(t, r) != 401 {
			t.Fatal("a pending login reached /admin")
		}
		pending, err := a.sessions.Pending(r)
		if err != nil {
			t.Fatal(err)
		}
		p, err := a.otp.Verify(ctx, pending, currentCode(0))
		if err != nil || !slices.Equal(p.Methods, []string{"pwd", "otp", "mfa"}) {
			t.Fatalf("TOTP Verify = %+v, %v", p, err)
		}
		rec := httptest.NewRecorder()
		if err := a.sessions.Issue(rec, r, p); err != nil {
			t.Fatal(err)
		}
		if code := a.adminStatus(t, withCookies(t, rec)); code != 200 {
			t.Fatalf("/admin after the second factor = %d", code)
		}

		// A password-only session is refused with 403.
		rec = httptest.NewRecorder()
		if err := a.sessions.Issue(rec, httptest.NewRequestWithContext(ctx, http.MethodPost, "/login", nil), &auth.Principal{Subject: "user-1", Methods: []string{"pwd"}}); err != nil {
			t.Fatal(err)
		}
		if code := a.adminStatus(t, withCookies(t, rec)); code != 403 || !errors.Is(a.rejection.Err, auth.ErrInsufficientMethods) {
			t.Fatalf("/admin with a password-only session = %d, %v", code, a.rejection.Err)
		}

		// TOTP and recovery codes share the five attempts.
		r = a.login(t)
		pending, _ = a.sessions.Pending(r)
		for range 3 {
			if _, err := a.otp.Verify(ctx, pending, currentCode(5)); !errors.Is(err, auth.ErrInvalidCredentials) {
				t.Fatalf("wrong TOTP code: %v", err)
			}
		}
		_, _ = a.recovery.Verify(ctx, pending, "zzzz-zzzz-zzzz-zzzz")
		if _, err := a.recovery.Verify(ctx, pending, "yyyy-yyyy-yyyy-yyyy"); !errors.Is(err, auth.ErrTooManyAttempts) {
			t.Fatalf("fifth wrong code: %v, want ErrTooManyAttempts", err)
		}
		if _, err := a.sessions.Pending(r); !errors.Is(err, auth.ErrNoCredentials) {
			t.Fatalf("pending login after five failures: %v", err)
		}

		// A recovery code counts as the second factor.
		pending, _ = a.sessions.Pending(a.login(t))
		p, err = a.recovery.Verify(ctx, pending, a.codes.Plain()[0])
		if err != nil || !slices.Equal(p.Methods, []string{"pwd", "mfa"}) {
			t.Fatalf("recovery Verify = %+v, %v", p, err)
		}
	})
}

func TestInfrastructureFailures(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		errDown := errors.New("down")
		statusOf := func(a auth.Authenticator, r *http.Request) int {
			mw, err := auth.New(a)
			if err != nil {
				t.Fatal(err)
			}
			rec := httptest.NewRecorder()
			mw(http.NotFoundHandler()).ServeHTTP(rec, r)
			return rec.Code
		}
		basicRequest := func() *http.Request {
			r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil)
			r.SetBasicAuth("alice", "correct horse")
			return r
		}
		floor := password.WithParams(password.Params{Memory: 7168, Iterations: 5, Parallelism: 1})

		// Session store read fails.
		store := memstore.New()
		good, _ := session.New(store)
		rec := httptest.NewRecorder()
		_ = good.Issue(rec, httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/login", nil), &auth.Principal{Subject: "user-1"})
		failingSessions, _ := session.New(storetest.Failing{Next: store, Err: errDown, Ops: []storetest.Op{storetest.OpGet}})
		if code := statusOf(failingSessions, withCookies(t, rec)); code != 500 {
			t.Errorf("session store failure: %d", code)
		}

		// Password lookup fails, through Basic; nothing is counted.
		counter := &storetest.Recorder{Next: memstore.New()}
		failingLookup, _ := password.NewChecker(func(context.Context, string) (password.Account, error) { return password.Account{}, errDown },
			floor, password.WithAttemptLimit(password.AttemptLimit{Store: counter}))
		b, _ := basic.New(failingLookup)
		if code := statusOf(b, basicRequest()); code != 500 || counter.Count(storetest.OpIncr) != 0 {
			t.Errorf("password lookup failure: %d, %d increments", code, counter.Count(storetest.OpIncr))
		}

		// The attempt counter fails.
		failingCounter, _ := password.NewChecker(func(context.Context, string) (password.Account, error) {
			return password.Account{Subject: "user-1", Hash: "$argon2id$v=19$m=7168,t=5,p=1$c2FsdHNhbHRzYWx0c2FsdA$a2V5a2V5a2V5a2V5a2V5a2V5a2V5a2V5a2V5a2V5a2U"}, nil
		}, floor, password.WithAttemptLimit(password.AttemptLimit{Store: storetest.Failing{Next: memstore.New(), Err: errDown, Ops: []storetest.Op{storetest.OpIncr}}}))
		b, _ = basic.New(failingCounter)
		if code := statusOf(b, basicRequest()); code != 500 {
			t.Errorf("attempt counter failure: %d", code)
		}

		// API key lookup fails.
		keys, _ := apikey.New("acme", func(context.Context, string) (apikey.Account, error) { return apikey.Account{}, errDown })
		key, _ := keys.Generate()
		r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil)
		r.Header.Set("Authorization", "Bearer "+string(key))
		if code := statusOf(keys, r); code != 500 {
			t.Errorf("API key lookup failure: %d", code)
		}

		// The key server is down and no keys are cached.
		tokens, _ := jwt.New(jwt.WithIssuer("https://id.example.test"), jwt.WithAudience("api"),
			jwt.WithJWKS("https://id.example.test/jwks"), jwt.WithHTTPClient(&http.Client{Transport: downIdP{}}))
		r = httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil)
		r.Header.Set("Authorization", "Bearer eyJhbGciOiJFUzI1NiJ9.e30.c2ln")
		if code := statusOf(tokens, r); code != 500 {
			t.Errorf("key server down: %d", code)
		}

		// Second-factor lookups fail: not invalid, and not counted.
		rs := &storetest.Recorder{Next: memstore.New()}
		a := newApp(t, rs)
		pending, _ := a.sessions.Pending(a.login(t))
		failingOTP, _ := totp.New(func(context.Context, string) ([]byte, error) { return nil, errDown }, rs)
		failingCodes, _ := recovery.New(func(context.Context, string) ([]string, error) { return nil, errDown },
			func(context.Context, string, string) error { return nil }, rs)
		rs.Reset()
		if _, err := failingOTP.Verify(t.Context(), pending, currentCode(0)); !errors.Is(err, errDown) || errors.Is(err, auth.ErrInvalidCredentials) {
			t.Errorf("TOTP lookup failure: %v", err)
		}
		if _, err := failingCodes.Verify(t.Context(), pending, a.codes.Plain()[0]); !errors.Is(err, errDown) || errors.Is(err, auth.ErrInvalidCredentials) {
			t.Errorf("recovery lookup failure: %v", err)
		}
		if n := rs.Count(storetest.OpIncr); n != 0 {
			t.Errorf("%d attempts counted after lookup failures", n)
		}
	})
}

func TestStoreHoldsNoSecrets(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		rs := &storetest.Recorder{Next: memstore.New()}
		a := newApp(t, rs)
		ctx := t.Context()
		var cookies, codes []string
		collect := func(rec *httptest.ResponseRecorder) {
			for _, c := range rec.Result().Cookies() {
				if c.Value != "" {
					cookies = append(cookies, c.Value)
				}
			}
		}

		// Attempt limit on the same store.
		limited, _ := password.NewChecker(func(context.Context, string) (password.Account, error) {
			return password.Account{}, password.ErrNotFound
		},
			password.WithParams(password.Params{Memory: 7168, Iterations: 5, Parallelism: 1}),
			password.WithAttemptLimit(password.AttemptLimit{Store: rs}))
		_, _ = limited.Check(ctx, "alice", "wrong guess")

		// Enrollment.
		e, _ := totp.NewEnrollment("Acme", "alice")
		enrollCode := hotp.Value(hotp.SHA1, e.Secret(), uint64(time.Now().Unix()/30), 6)
		codes = append(codes, enrollCode)
		_ = a.otp.Confirm(ctx, "user-2", e.Secret(), enrollCode) // another user enrolls

		// Password, pending login, TOTP, session, revocation.
		p, _ := a.checker.Check(ctx, "alice", "correct horse")
		login := httptest.NewRecorder()
		_ = a.sessions.IssuePending(login, httptest.NewRequestWithContext(ctx, http.MethodPost, "/login", nil), p)
		collect(login)
		r := withCookies(t, login)
		pending, _ := a.sessions.Pending(r)
		code := currentCode(0)
		codes = append(codes, code)
		p, _ = a.otp.Verify(ctx, pending, code)
		done := httptest.NewRecorder()
		_ = a.sessions.Issue(done, r, p)
		collect(done)
		_ = a.sessions.Revoke(httptest.NewRecorder(), withCookies(t, done))

		// Recovery code.
		login = httptest.NewRecorder()
		_ = a.sessions.IssuePending(login, httptest.NewRequestWithContext(ctx, http.MethodPost, "/login", nil), &auth.Principal{Subject: "user-1", Methods: []string{"pwd"}})
		collect(login)
		pending, _ = a.sessions.Pending(withCookies(t, login))
		_, _ = a.recovery.Verify(ctx, pending, a.codes.Plain()[0])
		codes = append(codes, a.codes.Plain()[0], strings.ReplaceAll(a.codes.Plain()[0], "-", ""))

		secrets := []string{"correct horse", "wrong guess", string(totpSecret), string(e.Secret())}
		secrets = append(append(secrets, cookies...), codes...)
		inKeys := append(slices.Clone(secrets), "alice", "user-1")
		calls := rs.Calls()
		if len(calls) == 0 || len(cookies) < 3 {
			t.Fatalf("flows did not run: %d calls, %d cookies", len(calls), len(cookies))
		}
		for _, call := range calls {
			for _, s := range inKeys {
				for _, enc := range encodings(s) {
					if strings.Contains(call.Key, enc) {
						t.Errorf("key %q contains %q", call.Key, enc)
					}
				}
			}
			for _, s := range secrets {
				for _, enc := range encodings(s) {
					if strings.Contains(string(call.Value), enc) {
						t.Errorf("value %q contains %q", call.Value, enc)
					}
				}
			}
		}
	})
}

func TestNoSecretsInLogs(t *testing.T) {
	ctx := t.Context()
	keys, _ := apikey.New("acme", func(context.Context, string) (apikey.Account, error) { return apikey.Account{}, apikey.ErrNotFound })
	key, _ := keys.Generate()
	e, _ := totp.NewEnrollment("Acme", "alice")
	codes, _ := recovery.Generate(3)

	var buf bytes.Buffer
	for _, h := range []slog.Handler{slog.NewJSONHandler(&buf, nil), slog.NewTextHandler(&buf, nil)} {
		slog.New(h).Info("generated", "key", key, "enrollment", e, "codes", codes)
	}
	secrets := []string{string(key), base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(e.Secret()), e.URI()}
	secrets = append(secrets, codes.Plain()...)
	for _, s := range secrets {
		if strings.Contains(buf.String(), s) {
			t.Errorf("log output contains %q", s)
		}
	}
	if !strings.Contains(buf.String(), "[REDACTED]") {
		t.Error("log output is not redacted")
	}

	// Error messages of failure paths never carry credentials.
	var errs []error
	a := newApp(t, memstore.New())
	_, err := a.checker.Check(ctx, "alice", "wrong-password-x")
	errs = append(errs, err)
	pending, _ := a.sessions.Pending(a.login(t))
	_, err = a.otp.Verify(ctx, pending, "123456")
	errs = append(errs, err)
	_, err = a.recovery.Verify(ctx, pending, "abcd-efgh-jkmn-pqrs")
	errs = append(errs, err)
	r := httptest.NewRequestWithContext(ctx, http.MethodGet, "/", nil)
	r.Header.Set("Authorization", "Bearer "+string(key))
	_, err = keys.Authenticate(r)
	errs = append(errs, err)
	forged := "eyJhbGciOiJub25lIn0.eyJzdWIiOiJ4In0."
	tokens, _ := jwt.New(jwt.WithIssuer("https://i.test"), jwt.WithAudience("a"), jwt.WithKey("", "HS256", []byte(strings.Repeat("k", 32))))
	r = httptest.NewRequestWithContext(ctx, http.MethodGet, "/", nil)
	r.Header.Set("Authorization", "Bearer "+forged)
	_, err = tokens.Authenticate(r)
	errs = append(errs, err)
	_, err = jwt.New(jwt.WithIssuer("https://i.test"), jwt.WithAudience("a"), jwt.WithKey("", "HS256", []byte("short-hs256-secret")))
	errs = append(errs, err)
	_, err = basic.New(a.checker, basic.WithRealm("realm\nwith-secret"))
	errs = append(errs, err)

	leaks := []string{"wrong-password-x", "correct horse", "123456", "abcd-efgh-jkmn-pqrs", string(key), forged, "short-hs256-secret", "with-secret", "alice"}
	for _, err := range errs {
		if err == nil {
			t.Fatal("a failure path returned no error")
		}
		for _, s := range leaks {
			if strings.Contains(err.Error(), s) {
				t.Errorf("error %q contains %q", err, s)
			}
		}
	}
}

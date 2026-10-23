package session_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/paccolamano/go/auth"
	"github.com/paccolamano/go/auth/internal/storekey"
	"github.com/paccolamano/go/auth/internal/storetest"
	"github.com/paccolamano/go/auth/memstore"
	"github.com/paccolamano/go/auth/session"
)

var (
	errStore = errors.New("store down")
	idShape  = regexp.MustCompile(`^[A-Za-z0-9_-]{43}$`)
)

func TestCookie_Attributes(t *testing.T) {
	m := mustManager(t, memstore.New())
	c := issue(t, m, alice())
	if c.Path != "/" || c.MaxAge != 28800 || !c.Secure || !c.HttpOnly || c.SameSite != http.SameSiteLaxMode || c.Domain != "" {
		t.Fatalf("cookie = %+v", c)
	}
	if !idShape.MatchString(c.Value) {
		t.Fatalf("value %q is not a 43-character base64url identifier", c.Value)
	}
	app := mustManager(t, memstore.New(), session.WithCookieName("app"))
	rec := httptest.NewRecorder()
	if err := app.Issue(rec, request(t), alice()); err != nil || cookie(rec, "__Host-app") == nil {
		t.Fatalf("WithCookieName: %v, cookies %v", err, rec.Result().Cookies())
	}
}

func TestIssue_StoreHasNoID(t *testing.T) {
	rec := &storetest.Recorder{Next: memstore.New()}
	c := issue(t, mustManager(t, rec), alice())
	for _, call := range rec.Calls() {
		if strings.Contains(call.Key, c.Value) || strings.Contains(string(call.Value), c.Value) {
			t.Fatalf("store call %+v contains the session identifier", call)
		}
	}
}

func TestIssue_Fixation(t *testing.T) {
	m := mustManager(t, memstore.New())
	old := issue(t, m, alice())
	fresh := issue(t, m, alice(), old)
	if fresh.Value == old.Value {
		t.Fatal("Issue reused the identifier")
	}
	if _, err := m.Authenticate(request(t, old)); !errors.Is(err, auth.ErrNoCredentials) {
		t.Fatalf("old session after a new login: %v, want ErrNoCredentials", err)
	}
	if _, err := m.Authenticate(request(t, fresh)); err != nil {
		t.Fatalf("new session: %v", err)
	}
}

func TestIssue_Errors(t *testing.T) {
	store := memstore.New()
	m := mustManager(t, store)
	for _, p := range []*auth.Principal{nil, {Subject: ""}} {
		rec := httptest.NewRecorder()
		if err := m.Issue(rec, request(t), p); err == nil || len(rec.Result().Cookies()) != 0 {
			t.Errorf("Issue(%v) = %v with cookies %v", p, err, rec.Result().Cookies())
		}
	}
	old := issue(t, m, alice())
	for _, op := range []storetest.Op{storetest.OpDelete, storetest.OpSet} {
		failing := mustManager(t, storetest.Failing{Next: store, Err: errStore, Ops: []storetest.Op{op}})
		rec := httptest.NewRecorder()
		if err := failing.Issue(rec, request(t, old), alice()); !errors.Is(err, errStore) || cookie(rec, sessionCookie) != nil {
			t.Errorf("failing %s: %v with cookie %v", op, err, cookie(rec, sessionCookie))
		}
	}
}

func TestAuthenticate_Principal(t *testing.T) {
	rec := &storetest.Recorder{Next: memstore.New()}
	m := mustManager(t, rec)
	c := issue(t, m, &auth.Principal{Subject: "user-1", Methods: []string{"pwd", "otp", "mfa"}})
	rec.Reset()
	p, err := m.Authenticate(request(t, c))
	if err != nil || p.Subject != "user-1" || !slices.Equal(p.Methods, []string{"pwd", "otp", "mfa"}) {
		t.Fatalf("Authenticate = %+v, %v", p, err)
	}
	if n := rec.Count(storetest.OpGet); n != 1 || len(rec.Calls()) != 1 {
		t.Fatalf("store calls = %v, want exactly one Get", rec.Calls())
	}
}

func TestAuthenticate_NoCredentials(t *testing.T) {
	rec := &storetest.Recorder{Next: memstore.New()}
	m := mustManager(t, rec)
	unknown := requestCookie(t, sessionCookie, strings.Repeat("A", 43))
	for name, r := range map[string]*http.Request{
		"missing":   request(t),
		"malformed": request(t, requestCookie(t, sessionCookie, "short")),
		"unknown":   request(t, unknown),
	} {
		rec.Reset()
		if _, err := m.Authenticate(r); !errors.Is(err, auth.ErrNoCredentials) {
			t.Errorf("%s: %v, want ErrNoCredentials", name, err)
		}
		if name != "unknown" && len(rec.Calls()) != 0 {
			t.Errorf("%s: %d store calls, want 0", name, len(rec.Calls()))
		}
	}

	mw, err := auth.New(m, auth.WithAnonymous())
	if err != nil {
		t.Fatal(err)
	}
	served := false
	h := mw(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { served = true }))
	h.ServeHTTP(httptest.NewRecorder(), request(t, unknown))
	if !served {
		t.Fatal("a stale cookie blocked an anonymous route")
	}
}

func TestAuthenticate_StoreError(t *testing.T) {
	store := memstore.New()
	c := issue(t, mustManager(t, store), alice())

	failing := mustManager(t, storetest.Failing{Next: store, Err: errStore, Ops: []storetest.Op{storetest.OpGet}})
	if _, err := failing.Authenticate(request(t, c)); err == nil || errors.Is(err, auth.ErrNoCredentials) {
		t.Fatalf("failing Get: %v", err)
	}
	mw, _ := auth.New(failing)
	rec := httptest.NewRecorder()
	mw(http.NotFoundHandler()).ServeHTTP(rec, request(t, c))
	if rec.Code != 500 {
		t.Fatalf("status = %d, want 500", rec.Code)
	}

	_ = store.Set(t.Context(), storekey.Key("session", c.Value), []byte("garbage"), time.Hour)
	if _, err := mustManager(t, store).Authenticate(request(t, c)); err == nil || errors.Is(err, auth.ErrNoCredentials) {
		t.Fatalf("undecodable record: %v", err)
	}

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	r := request(t, issue(t, mustManager(t, store), alice())).WithContext(ctx)
	if _, err := mustManager(t, store).Authenticate(r); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled request context: %v, want context.Canceled", err)
	}
}

func TestExpiry_Idle(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		m := mustManager(t, memstore.New())
		c := issue(t, m, alice())
		time.Sleep(30 * time.Minute)
		if _, err := m.Authenticate(request(t, c)); !errors.Is(err, auth.ErrNoCredentials) {
			t.Fatalf("after 30 idle minutes: %v", err)
		}
	})
}

func TestExpiry_Absolute(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		m := mustManager(t, memstore.New())
		c := issue(t, m, alice())
		for elapsed := 5 * time.Minute; elapsed < 8*time.Hour; elapsed += 5 * time.Minute {
			time.Sleep(5 * time.Minute)
			if _, err := m.Authenticate(request(t, c)); err != nil {
				t.Fatalf("at %v: %v", elapsed, err)
			}
		}
		time.Sleep(5 * time.Minute)
		if _, err := m.Authenticate(request(t, c)); !errors.Is(err, auth.ErrNoCredentials) {
			t.Fatalf("at 8h: %v, want ErrNoCredentials", err)
		}
	})
}

func TestRenewal_Writes(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		rec := &storetest.Recorder{Next: memstore.New()}
		m := mustManager(t, rec)
		c := issue(t, m, alice())
		rec.Reset()
		for range 449 {
			time.Sleep(time.Second)
			if _, err := m.Authenticate(request(t, c)); err != nil {
				t.Fatal(err)
			}
		}
		if n := rec.Count(storetest.OpReplace); n != 0 {
			t.Fatalf("%d writes within 7m29s, want 0", n)
		}
		time.Sleep(time.Second)
		_, _ = m.Authenticate(request(t, c))
		if n := rec.Count(storetest.OpReplace); n != 1 {
			t.Fatalf("%d writes at 7m30s, want 1", n)
		}

		rec.Reset()
		for range 1800 {
			time.Sleep(time.Second)
			if _, err := m.Authenticate(request(t, c)); err != nil {
				t.Fatal(err)
			}
		}
		if n := rec.Count(storetest.OpReplace); n > 4 {
			t.Fatalf("%d writes in 30 minutes, want at most 4", n)
		}
	})

	synctest.Test(t, func(t *testing.T) {
		store := memstore.New()
		c := issue(t, mustManager(t, store), alice())
		time.Sleep(8 * time.Minute)
		failing := mustManager(t, storetest.Failing{Next: store, Err: errStore, Ops: []storetest.Op{storetest.OpReplace}})
		if _, err := failing.Authenticate(request(t, c)); !errors.Is(err, errStore) {
			t.Fatalf("failing renewal: %v", err)
		}
		mw, err := auth.New(failing)
		if err != nil {
			t.Fatal(err)
		}
		rec := httptest.NewRecorder()
		mw(http.NotFoundHandler()).ServeHTTP(rec, request(t, c))
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("failing renewal answered %d, want 500", rec.Code)
		}
	})
}

func TestRenewal_DoesNotRestoreDeleted(t *testing.T) {
	logout := func(t *testing.T, m *session.Manager, c *http.Cookie) *http.Cookie {
		t.Helper()
		if err := m.Revoke(httptest.NewRecorder(), request(t, c)); err != nil {
			t.Error(err)
		}
		return nil
	}
	login := func(t *testing.T, m *session.Manager, c *http.Cookie) *http.Cookie {
		t.Helper()
		return issue(t, m, alice(), c)
	}
	expire := func(t *testing.T, _ *session.Manager, _ *http.Cookie) *http.Cookie {
		t.Helper()
		time.Sleep(30 * time.Minute) // past the idle timeout
		return nil
	}
	for _, tt := range []struct {
		name        string
		at          point
		del         func(t *testing.T, m *session.Manager, c *http.Cookie) *http.Cookie
		wantRenewed bool
	}{
		{"logout before read", beforeRead, logout, false},
		{"logout between read and write", betweenReadAndWrite, logout, false},
		{"logout after write", afterWrite, logout, true},
		{"login before read", beforeRead, login, false},
		{"login between read and write", betweenReadAndWrite, login, false},
		{"login after write", afterWrite, login, true},
		{"store expiry between read and write", betweenReadAndWrite, expire, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				store := &interleaved{Store: memstore.New()}
				m := mustManager(t, store)
				c := issue(t, m, alice())
				time.Sleep(8 * time.Minute) // past the 7m30s renewal interval
				var fresh *http.Cookie
				store.at = tt.at
				store.run = func() { fresh = tt.del(t, m, c) }

				p, err := m.Authenticate(request(t, c))
				switch {
				case tt.wantRenewed && (err != nil || p.Subject != "user-1"):
					t.Fatalf("renewing request: %+v, %v; want user-1", p, err)
				case !tt.wantRenewed && !errors.Is(err, auth.ErrNoCredentials):
					t.Fatalf("renewing request: %+v, %v; want ErrNoCredentials", p, err)
				}
				if store.run != nil {
					t.Fatal("the deletion did not run")
				}
				if _, err := m.Authenticate(request(t, c)); !errors.Is(err, auth.ErrNoCredentials) {
					t.Fatalf("deleted session afterwards: %v, want ErrNoCredentials", err)
				}
				if fresh != nil {
					if p, err := m.Authenticate(request(t, fresh)); err != nil || p.Subject != "user-1" {
						t.Fatalf("new session: %+v, %v; want user-1", p, err)
					}
				}
			})
		})
	}
}

func TestRenewal_DeletedServedAsAnonymous(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		store := &interleaved{Store: memstore.New()}
		m := mustManager(t, store)
		c := issue(t, m, alice())
		time.Sleep(8 * time.Minute)
		store.at = betweenReadAndWrite
		store.run = func() {
			if err := m.Revoke(httptest.NewRecorder(), request(t, c)); err != nil {
				t.Error(err)
			}
		}
		mw, err := auth.New(m, auth.WithAnonymous())
		if err != nil {
			t.Fatal(err)
		}
		var served, hasPrincipal bool
		h := mw(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
			served = true
			_, hasPrincipal = auth.FromContext(r.Context())
		}))
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, request(t, c))
		if rec.Code != http.StatusOK || !served || hasPrincipal {
			t.Fatalf("status %d, served %v, principal %v; want 200, served with no principal", rec.Code, served, hasPrincipal)
		}
	})
}

func TestRenewal_ConcurrentLogout(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		m := mustManager(t, memstore.New())
		c := issue(t, m, alice())

		// Concurrent renewals with no logout: every request is authenticated.
		time.Sleep(8 * time.Minute)
		var wg sync.WaitGroup
		for range 100 {
			wg.Go(func() {
				if _, err := m.Authenticate(request(t, c)); err != nil {
					t.Error(err)
				}
			})
		}
		wg.Wait()
		if _, err := m.Authenticate(request(t, c)); err != nil {
			t.Fatalf("session after concurrent renewals: %v", err)
		}

		// Concurrent renewals and a logout: the session ends.
		time.Sleep(8 * time.Minute)
		for range 100 {
			wg.Go(func() {
				if _, err := m.Authenticate(request(t, c)); err != nil && !errors.Is(err, auth.ErrNoCredentials) {
					t.Error(err)
				}
			})
		}
		wg.Go(func() {
			if err := m.Revoke(httptest.NewRecorder(), request(t, c)); err != nil {
				t.Error(err)
			}
		})
		wg.Wait()
		if _, err := m.Authenticate(request(t, c)); !errors.Is(err, auth.ErrNoCredentials) {
			t.Fatalf("session after a concurrent logout: %v, want ErrNoCredentials", err)
		}
	})
}

func TestRevoke_Session(t *testing.T) {
	m := mustManager(t, memstore.New())
	c := issue(t, m, alice())
	rec := httptest.NewRecorder()
	if err := m.Revoke(rec, request(t, c)); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{sessionCookie, pendingCookie} {
		if got := cookie(rec, name); got == nil || got.MaxAge >= 0 {
			t.Fatalf("cookie %s not cleared: %+v", name, got)
		}
	}
	if _, err := m.Authenticate(request(t, c)); !errors.Is(err, auth.ErrNoCredentials) {
		t.Fatalf("revoked session: %v", err)
	}
	rec = httptest.NewRecorder()
	if err := m.Revoke(rec, request(t)); err != nil || cookie(rec, sessionCookie) == nil {
		t.Fatalf("Revoke without cookies: %v, cookies %v", err, rec.Result().Cookies())
	}

	store := memstore.New()
	c = issue(t, mustManager(t, store), alice())
	failing := mustManager(t, storetest.Failing{Next: store, Err: errStore, Ops: []storetest.Op{storetest.OpDelete}})
	rec = httptest.NewRecorder()
	if err := failing.Revoke(rec, request(t, c)); !errors.Is(err, errStore) || cookie(rec, sessionCookie) == nil {
		t.Fatalf("failing delete: %v, cookies %v", err, rec.Result().Cookies())
	}
}

func TestManager_Challenge(t *testing.T) {
	m := mustManager(t, memstore.New())
	want := `Cookie cookie-name="__Host-session"`
	if got := m.Challenge(auth.ErrNoCredentials); got != want {
		t.Errorf("Challenge(none) = %q", got)
	}
	if got := m.Challenge(auth.ErrInvalidCredentials); got != want {
		t.Errorf("Challenge(invalid) = %q", got)
	}
	if got := m.Challenge(auth.ErrInsufficientMethods); got != "" {
		t.Errorf("Challenge(insufficient) = %q, want empty", got)
	}
}

func TestManager_Config(t *testing.T) {
	store := memstore.New()
	for name, opts := range map[string][]session.Option{
		"lifetime zero":      {session.WithLifetime(0)},
		"idle zero":          {session.WithIdleTimeout(0)},
		"idle over lifetime": {session.WithLifetime(time.Hour), session.WithIdleTimeout(2 * time.Hour)},
		"pending zero":       {session.WithPendingLifetime(0)},
		"pending over 1h":    {session.WithPendingLifetime(time.Hour + time.Second)},
		"cookie empty":       {session.WithCookieName("")},
		"cookie not a token": {session.WithCookieName("a b")},
		"cookie with ;":      {session.WithCookieName("a;b")},
		"cookie over 64":     {session.WithCookieName(strings.Repeat("a", 65))},
		"nil option":         {nil},
	} {
		if _, err := session.New(store, opts...); !errors.Is(err, auth.ErrInvalidConfig) {
			t.Errorf("%s: %v, want ErrInvalidConfig", name, err)
		}
	}
	if _, err := session.New(nil); !errors.Is(err, auth.ErrInvalidConfig) {
		t.Errorf("nil store: %v", err)
	}
}

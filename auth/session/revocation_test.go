package session_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
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

func bob() *auth.Principal { return &auth.Principal{Subject: "user-2", Methods: []string{"pwd"}} }

// authenticates reports whether c authenticates a request, and fails the test
// on any outcome other than a principal or auth.ErrNoCredentials.
func authenticates(tb testing.TB, m *session.Manager, c *http.Cookie) bool {
	tb.Helper()
	_, err := m.Authenticate(request(tb, c))
	switch {
	case err == nil:
		return true
	case errors.Is(err, auth.ErrNoCredentials):
		return false
	}
	tb.Fatalf("Authenticate: %v", err)
	return false
}

func TestRevokeSubject_EndsSessions(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		m := mustManager(t, memstore.New())
		a1, a2 := issue(t, m, alice()), issue(t, m, alice())
		time.Sleep(8 * time.Minute) // a1 and a2 are due for renewal
		a3 := issue(t, m, alice())
		b := issue(t, m, bob())

		if err := m.RevokeSubject(t.Context(), "user-1"); err != nil {
			t.Fatal(err)
		}
		for i, c := range []*http.Cookie{a1, a2, a3} {
			if authenticates(t, m, c) {
				t.Errorf("session %d of the revoked subject authenticated", i+1)
			}
		}
		if !authenticates(t, m, b) {
			t.Error("the session of another subject was revoked")
		}
	})
}

func TestRevokeSubject_ThenIssue(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		m := mustManager(t, memstore.New())
		current := issue(t, m, alice())

		// POST /password: revoke, then keep the current browser signed in.
		r := request(t, current)
		if err := m.RevokeSubject(r.Context(), "user-1"); err != nil {
			t.Fatal(err)
		}
		fresh := issue(t, m, alice(), current)
		if authenticates(t, m, current) {
			t.Error("the replaced session authenticated")
		}
		for elapsed := 20 * time.Minute; elapsed < 8*time.Hour; elapsed += 20 * time.Minute {
			time.Sleep(20 * time.Minute)
			if !authenticates(t, m, fresh) {
				t.Fatalf("the session issued after the revocation failed at %v", elapsed)
			}
		}
		time.Sleep(20 * time.Minute)
		if authenticates(t, m, fresh) {
			t.Fatal("the session outlived its absolute lifetime")
		}
	})
}

func TestRevokeSubject_ServedAsAnonymous(t *testing.T) {
	m := mustManager(t, memstore.New())
	c := issue(t, m, alice())
	if err := m.RevokeSubject(t.Context(), "user-1"); err != nil {
		t.Fatal(err)
	}
	mw, err := auth.New(m, auth.WithAnonymous())
	if err != nil {
		t.Fatal(err)
	}
	served, anonymous := false, false
	mw(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		_, ok := auth.FromContext(r.Context())
		served, anonymous = true, !ok
	})).ServeHTTP(httptest.NewRecorder(), request(t, c))
	if !served || !anonymous {
		t.Fatalf("served = %v, anonymous = %v; want an anonymous request", served, anonymous)
	}
}

func TestRevokeSubject_SameInstant(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		m := mustManager(t, memstore.New())
		start := time.Now()
		before := issue(t, m, alice())
		if err := m.RevokeSubject(t.Context(), "user-1"); err != nil {
			t.Fatal(err)
		}
		after := issue(t, m, alice())
		if !time.Now().Equal(start) {
			t.Fatal("virtual time moved")
		}
		if authenticates(t, m, before) {
			t.Error("the session issued before the revocation, at the same instant, authenticated")
		}
		if !authenticates(t, m, after) {
			t.Error("the session issued after the revocation, at the same instant, was revoked")
		}
	})
}

func TestRevokeSubject_Interleavings(t *testing.T) {
	for _, c := range []struct {
		name   string
		op     string
		at     point
		served bool // the outcome of the interleaved operation itself
		later  bool // whether the session or pending login works afterwards
	}{
		{"issue: revocation before the revocation read", "issue", beforeRevocationRead, true, true},
		{"issue: revocation between the revocation read and the write", "issue", betweenReadAndWrite, true, false},
		{"issue: revocation after the write", "issue", afterWrite, true, false},
		{"renewal: revocation before the session read", "renew", beforeRead, false, false},
		{"renewal: revocation before the revocation read", "renew", beforeRevocationRead, false, false},
		{"renewal: revocation between the revocation read and the write", "renew", betweenReadAndWrite, true, false},
		{"renewal: revocation after the write", "renew", afterWrite, true, false},
		{"pending: revocation before the revocation read", "pending", beforeRevocationRead, false, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				s := &interleaved{Store: memstore.New()}
				m := mustManager(t, s)
				revoke := func() {
					if err := m.RevokeSubject(t.Context(), "user-1"); err != nil {
						t.Error(err)
					}
				}
				switch c.op {
				case "issue":
					s.at, s.run = c.at, revoke
					sc := issue(t, m, alice())
					if got := authenticates(t, m, sc); got != c.later {
						t.Errorf("later request authenticated = %v, want %v", got, c.later)
					}
				case "renew":
					sc := issue(t, m, alice())
					time.Sleep(8 * time.Minute)
					s.at, s.run = c.at, revoke
					if got := authenticates(t, m, sc); got != c.served {
						t.Errorf("renewing request authenticated = %v, want %v", got, c.served)
					}
					if got := authenticates(t, m, sc); got != c.later {
						t.Errorf("later request authenticated = %v, want %v", got, c.later)
					}
				case "pending":
					pc := issuePending(t, m, alice())
					s.at, s.run = c.at, revoke
					_, err := m.Pending(request(t, pc))
					if got := err == nil; got != c.served {
						t.Errorf("pending login read = %v (%v), want %v", got, err, c.served)
					}
				}
				if s.run != nil {
					t.Fatal("the revocation never ran")
				}
			})
		})
	}
}

func TestRevokeSubject_SharedStore(t *testing.T) {
	store := memstore.New()
	m1, m2 := mustManager(t, store), mustManager(t, store)
	c1, c2 := issue(t, m1, alice()), issue(t, m2, alice())
	if err := m2.RevokeSubject(context.WithoutCancel(t.Context()), "user-1"); err != nil {
		t.Fatal(err)
	}
	for _, m := range []*session.Manager{m1, m2} {
		for _, c := range []*http.Cookie{c1, c2} {
			if authenticates(t, m, c) {
				t.Error("a session authenticated after the revocation through the other manager")
			}
		}
	}
}

func TestRevokeSubject_Errors(t *testing.T) {
	ctx := t.Context()
	store := memstore.New()
	rec := &storetest.Recorder{Next: store}
	if err := mustManager(t, rec).RevokeSubject(ctx, ""); err == nil || len(rec.Calls()) != 0 {
		t.Fatalf("empty subject: %v, %d store calls; want an error and none", err, len(rec.Calls()))
	}

	c := issue(t, mustManager(t, store), alice())
	pc := issuePending(t, mustManager(t, store), alice())
	failing := mustManager(t, failingRevocation{store})
	if err := failing.RevokeSubject(ctx, "user-1"); !errors.Is(err, errStore) {
		t.Errorf("RevokeSubject on a failing store: %v", err)
	}
	if _, err := failing.Authenticate(request(t, c)); !errors.Is(err, errStore) {
		t.Errorf("Authenticate: %v, want the store error", err)
	}
	mw, err := auth.New(failing)
	if err != nil {
		t.Fatal(err)
	}
	resp := httptest.NewRecorder()
	mw(http.NotFoundHandler()).ServeHTTP(resp, request(t, c))
	if resp.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", resp.Code)
	}
	for name, issueFn := range map[string]func(http.ResponseWriter, *http.Request, *auth.Principal) error{
		"Issue":        failing.Issue,
		"IssuePending": failing.IssuePending,
	} {
		resp := httptest.NewRecorder()
		if err := issueFn(resp, request(t), alice()); !errors.Is(err, errStore) || len(resp.Result().Cookies()) != 0 {
			t.Errorf("%s: %v, %d cookies; want the store error and none", name, err, len(resp.Result().Cookies()))
		}
	}
	if _, err := failing.Pending(request(t, pc)); !errors.Is(err, errStore) {
		t.Errorf("Pending: %v, want the store error", err)
	}

	// Revoking again once the store recovers.
	m := mustManager(t, store)
	if err := m.RevokeSubject(ctx, "user-1"); err != nil {
		t.Fatal(err)
	}
	if authenticates(t, m, c) {
		t.Error("session authenticated after the retried revocation")
	}

	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if err := m.RevokeSubject(cancelled, "user-1"); !errors.Is(err, context.Canceled) {
		t.Errorf("cancelled context: %v, want context.Canceled", err)
	}
}

func TestRevokeSubject_Concurrent(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		m := mustManager(t, memstore.New())
		cookies := make([]*http.Cookie, 10)
		for i := range cookies {
			cookies[i] = issue(t, m, alice())
		}
		time.Sleep(8 * time.Minute) // every session is due for renewal
		revoked := make(chan struct{})
		var wg sync.WaitGroup
		for i := range 100 {
			c := cookies[i%len(cookies)]
			wg.Go(func() {
				if _, err := m.Authenticate(request(t, c)); err != nil && !errors.Is(err, auth.ErrNoCredentials) {
					t.Errorf("during the revocation: %v", err)
				}
			})
			wg.Go(func() {
				<-revoked
				if _, err := m.Authenticate(request(t, c)); !errors.Is(err, auth.ErrNoCredentials) {
					t.Errorf("after the revocation: %v, want ErrNoCredentials", err)
				}
			})
		}
		wg.Go(func() {
			defer close(revoked)
			if err := m.RevokeSubject(t.Context(), "user-1"); err != nil {
				t.Error(err)
			}
		})
		wg.Wait()
	})
}

func TestRevokeSubject_Retention(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		store := memstore.New()
		m := mustManager(t, store)
		ctx := t.Context()
		key := storekey.Key("session-gen", "user-1")
		old := issue(t, m, alice())
		if err := m.RevokeSubject(ctx, "user-1"); err != nil {
			t.Fatal(err)
		}
		time.Sleep(2 * time.Hour)
		later := issue(t, m, alice())
		for elapsed := 2*time.Hour + 20*time.Minute; elapsed <= 9*time.Hour+20*time.Minute; elapsed += 20 * time.Minute {
			time.Sleep(20 * time.Minute)
			if !authenticates(t, m, later) {
				t.Fatalf("the session issued after the revocation failed at %v", elapsed)
			}
			_, err := store.Get(ctx, key)
			switch {
			case elapsed == 8*time.Hour && err != nil:
				t.Fatalf("revocation state gone at 8h, while sessions issued before it can live: %v", err)
			case elapsed >= 9*time.Hour && !errors.Is(err, auth.ErrNotFound):
				t.Fatalf("revocation state still stored at %v: %v", elapsed, err)
			}
		}
		if authenticates(t, m, old) {
			t.Fatal("the session issued before the revocation authenticated")
		}
	})
}

func TestRevokeSubject_Pending(t *testing.T) {
	m := mustManager(t, memstore.New())
	ctx := t.Context()
	before := issuePending(t, m, alice())
	other := issuePending(t, m, bob())
	if err := m.RevokeSubject(ctx, "user-1"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Pending(request(t, before)); !errors.Is(err, auth.ErrNoCredentials) {
		t.Errorf("pending login issued before the revocation: %v, want ErrNoCredentials", err)
	}
	if _, err := m.Pending(request(t, other)); err != nil {
		t.Errorf("pending login of another subject: %v", err)
	}

	after := issuePending(t, m, alice())
	if err := mustPending(t, m, after).Attempt(ctx, pass); err != nil {
		t.Fatalf("pending login issued after the revocation: %v", err)
	}
	if !authenticates(t, m, issue(t, m, alice(), after)) {
		t.Error("the session completing a later pending login was revoked")
	}
}

// TestRevokeSubject_WindowAccepted pins FR-006: a pending login read before a
// revocation and completed after it yields a valid session.
func TestRevokeSubject_WindowAccepted(t *testing.T) {
	m := mustManager(t, memstore.New())
	pc := issuePending(t, m, alice())
	pending := mustPending(t, m, pc)
	if err := m.RevokeSubject(t.Context(), "user-1"); err != nil {
		t.Fatal(err)
	}
	if err := pending.Attempt(t.Context(), pass); err != nil {
		t.Fatal(err)
	}
	if !authenticates(t, m, issue(t, m, alice(), pc)) {
		t.Fatal("the session issued after the revocation was revoked")
	}
}

func TestRevokeSubject_PreviousRelease(t *testing.T) {
	store := memstore.New()
	m := mustManager(t, store)
	ctx := t.Context()
	now := time.Now().Unix()
	// An identifier decodes as canonical base64url only when its last
	// character leaves no bits over: "A" does, "B" does not.
	sid, pid := strings.Repeat("A", 43), strings.Repeat("B", 42)+"A"
	if err := store.Set(ctx, storekey.Key("session", sid),
		fmt.Appendf(nil, `{"v":1,"sub":"user-1","amr":["pwd"],"iat":%d,"exp":%d,"rw":%d}`, now, now+3600, now), time.Hour); err != nil {
		t.Fatal(err)
	}
	if err := store.Set(ctx, storekey.Key("pending", pid),
		fmt.Appendf(nil, `{"v":1,"sub":"user-1","amr":["pwd"],"iat":%d}`, now), time.Minute); err != nil {
		t.Fatal(err)
	}
	sc, pc := requestCookie(t, sessionCookie, sid), requestCookie(t, pendingCookie, pid)

	if !authenticates(t, m, sc) {
		t.Fatal("a session of the previous release did not authenticate")
	}
	if _, err := m.Pending(request(t, pc)); err != nil {
		t.Fatalf("a pending login of the previous release: %v", err)
	}
	if err := m.RevokeSubject(ctx, "user-1"); err != nil {
		t.Fatal(err)
	}
	if authenticates(t, m, sc) {
		t.Error("a session of the previous release survived the revocation")
	}
	if _, err := m.Pending(request(t, pc)); !errors.Is(err, auth.ErrNoCredentials) {
		t.Errorf("a pending login of the previous release: %v, want ErrNoCredentials", err)
	}
}

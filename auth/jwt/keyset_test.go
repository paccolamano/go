package jwt_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	jose "github.com/go-jose/go-jose/v4"

	"github.com/paccolamano/go/auth"
	"github.com/paccolamano/go/auth/jwt"
)

func status(tb testing.TB, v *jwt.Verifier, token string) int {
	tb.Helper()
	mw, _ := auth.New(v)
	rec := httptest.NewRecorder()
	mw(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})).ServeHTTP(rec, bearer(tb, token))
	return rec.Code
}

func TestKeySet_Lazy(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		k := testKeys()
		idp := newIdP(t, jwk(&k.ec.PublicKey, "k1", "ES256"))
		v := newVerifier(t, idp)
		if n := idp.requests.Load(); n != 0 {
			t.Fatalf("%d requests at construction", n)
		}
		tok := sign(t, jose.ES256, k.ec, "k1", nil, validClaims())
		for range 3 {
			if code := status(t, v, tok); code != 200 {
				t.Fatalf("status %d", code)
			}
		}
		if n := idp.requests.Load(); n != 1 {
			t.Fatalf("%d requests, want 1", n)
		}
		if idp.accept != "application/jwk-set+json, application/json" {
			t.Fatalf("Accept = %q", idp.accept)
		}
	})
}

func TestKeySet_CacheControl(t *testing.T) {
	for _, c := range []struct {
		header        string
		before, after time.Duration
	}{
		{"max-age=120", 119 * time.Second, 120 * time.Second},
		{"no-cache", 59 * time.Second, time.Minute},
		{"max-age=0", 59 * time.Second, time.Minute},
		{"max-age=999999", 24*time.Hour - time.Second, 24 * time.Hour},
		{"", time.Hour - time.Second, time.Hour},
	} {
		t.Run(c.header, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				k := testKeys()
				idp := newIdP(t, jwk(&k.ec.PublicKey, "k1", "ES256"))
				idp.set(func(f *fakeIdP) { f.cacheControl = c.header })
				v := newVerifier(t, idp)
				claims := with(validClaims(), "exp", time.Now().Add(48*time.Hour).Unix())
				tok := sign(t, jose.ES256, k.ec, "k1", nil, claims)
				_ = status(t, v, tok)
				time.Sleep(c.before)
				_ = status(t, v, tok)
				if n := idp.requests.Load(); n != 1 {
					t.Fatalf("%d requests before %v, want 1", n, c.before)
				}
				time.Sleep(c.after - c.before)
				_ = status(t, v, tok)
				if n := idp.requests.Load(); n != 2 {
					t.Fatalf("%d requests at %v, want 2", n, c.after)
				}
			})
		})
	}
}

func TestKeySet_UnknownKid(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		k := testKeys()
		idp := newIdP(t, jwk(&k.ec.PublicKey, "k1", "ES256"))
		v := newVerifier(t, idp)
		_ = status(t, v, sign(t, jose.ES256, k.ec, "k1", nil, validClaims()))
		time.Sleep(30 * time.Second)
		idp.setKeys(t, jwk(&k.ec.PublicKey, "k1", "ES256"), jwk(&k.another.PublicKey, "k2", "ES256"))
		if code := status(t, v, sign(t, jose.ES256, k.another, "k2", nil, validClaims())); code != 200 {
			t.Fatalf("rotated key: status %d", code)
		}
		if n := idp.requests.Load(); n != 2 {
			t.Fatalf("%d requests, want 2", n)
		}
	})
}

func TestKeySetRefreshBound(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		k := testKeys()
		idp := newIdP(t, jwk(&k.ec.PublicKey, "k1", "ES256"))
		v := newVerifier(t, idp)
		_ = status(t, v, sign(t, jose.ES256, k.ec, "k1", nil, validClaims()))
		time.Sleep(30 * time.Second)
		before := idp.requests.Load()
		for i := range 1000 {
			if code := status(t, v, sign(t, jose.ES256, k.ec, "forged-"+string(rune('a'+i%26))+strings.Repeat("x", i%7), nil, validClaims())); code != 401 {
				t.Fatalf("unknown kid: status %d", code)
			}
		}
		if n := idp.requests.Load() - before; n > 1 {
			t.Fatalf("%d downloads for 1,000 unknown kids, want at most 1", n)
		}
	})
}

func TestKeySet_Unavailable(t *testing.T) {
	k := testKeys()
	good := func(f *fakeIdP) {}
	for name, setup := range map[string]func(f *fakeIdP){
		"transport error": func(f *fakeIdP) { f.err = errors.New("dial: refused") },
		"status 500":      func(f *fakeIdP) { f.status = 500 },
		"invalid JSON":    func(f *fakeIdP) { f.body = []byte("{not json") },
		"over 1 MiB":      func(f *fakeIdP) { f.body = []byte(`{"keys":[],"pad":"` + strings.Repeat("x", 1<<20) + `"}`) },
		"no usable key":   func(f *fakeIdP) { f.body = []byte(`{"keys":[{"kty":"oct","k":"c2VjcmV0"}]}`) },
		"control":         good,
	} {
		t.Run(name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				idp := newIdP(t, jwk(&k.ec.PublicKey, "k1", "ES256"))
				idp.set(setup)
				want := 500
				if name == "control" {
					want = 200
				}
				if code := status(t, newVerifier(t, idp), sign(t, jose.ES256, k.ec, "k1", nil, validClaims())); code != want {
					t.Fatalf("status %d, want %d", code, want)
				}
			})
		})
	}
}

func TestKeySet_StaleOnFailure(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		k := testKeys()
		idp := newIdP(t, jwk(&k.ec.PublicKey, "k1", "ES256"))
		idp.set(func(f *fakeIdP) { f.cacheControl = "max-age=60" })
		v := newVerifier(t, idp)
		tok := sign(t, jose.ES256, k.ec, "k1", nil, validClaims())
		_ = status(t, v, tok)
		time.Sleep(61 * time.Second)
		idp.set(func(f *fakeIdP) { f.err = errors.New("down") })
		if code := status(t, v, tok); code != 200 {
			t.Fatalf("stale keys during an outage: status %d", code)
		}
		if code := status(t, v, tok); code != 200 || idp.requests.Load() != 2 {
			t.Fatalf("second request: status %d, %d downloads", code, idp.requests.Load())
		}
		if code := status(t, v, sign(t, jose.ES256, k.another, "k9", nil, validClaims())); code != 500 {
			t.Fatalf("unknown kid after a failed download: status %d, want 500", code)
		}
	})
}

func TestKeySet_SingleFlight(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		k := testKeys()
		idp := newIdP(t, jwk(&k.ec.PublicKey, "k1", "ES256"))
		v := newVerifier(t, idp)
		tok := sign(t, jose.ES256, k.ec, "k1", nil, validClaims())
		var (
			wg sync.WaitGroup
			ok atomic.Int32
		)
		for range 50 {
			wg.Go(func() {
				if _, err := v.Authenticate(bearer(t, tok)); err == nil {
					ok.Add(1)
				}
			})
		}
		wg.Wait()
		if n := idp.requests.Load(); n != 1 || ok.Load() != 50 {
			t.Fatalf("%d downloads and %d successes, want 1 and 50", n, ok.Load())
		}
	})
}

func TestKeySet_DetachedContext(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		k := testKeys()
		idp := newIdP(t, jwk(&k.ec.PublicKey, "k1", "ES256"))
		block := make(chan struct{})
		idp.set(func(f *fakeIdP) { f.block = block })
		v := newVerifier(t, idp)
		tok := sign(t, jose.ES256, k.ec, "k1", nil, validClaims())

		ctx, cancel := context.WithCancel(t.Context())
		var wg sync.WaitGroup
		first := httptest.NewRequestWithContext(ctx, http.MethodGet, "/", nil)
		first.Header.Set("Authorization", "Bearer "+tok)
		wg.Go(func() { _, _ = v.Authenticate(first) })
		synctest.Wait()
		var second error
		wg.Go(func() { _, second = v.Authenticate(bearer(t, tok)) })
		synctest.Wait()
		cancel()
		synctest.Wait()
		close(block)
		wg.Wait()
		if second != nil {
			t.Fatalf("the waiter failed after the first caller left: %v", second)
		}
		if n := idp.requests.Load(); n != 1 {
			t.Fatalf("%d downloads, want 1", n)
		}
	})
}

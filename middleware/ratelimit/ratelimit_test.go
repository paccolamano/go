package ratelimit_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/paccolamano/go/middleware"
	"github.com/paccolamano/go/middleware/ratelimit"
)

func mustNew(t *testing.T, limit int, period time.Duration, opts ...ratelimit.Option) middleware.Middleware {
	t.Helper()
	mw, err := ratelimit.New(limit, period, opts...)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return mw
}

func request(t *testing.T, h http.Handler, remoteAddr string, header http.Header) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil)
	req.RemoteAddr = remoteAddr
	for k, v := range header {
		req.Header[k] = v
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

var ok = http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})

func TestRateLimit_InvalidConfig(t *testing.T) {
	tests := []struct {
		name   string
		limit  int
		period time.Duration
		opts   []ratelimit.Option
	}{
		{"zero limit", 0, time.Minute, nil},
		{"negative limit", -1, time.Minute, nil},
		{"zero period", 10, 0, nil},
		{"interval below 1ns", 10, 5 * time.Nanosecond, nil},
		{"zero burst", 10, time.Minute, []ratelimit.Option{ratelimit.WithBurst(0)}},
		{"nil key func", 10, time.Minute, []ratelimit.Option{ratelimit.WithKey(nil)}},
		{"nil reject func", 10, time.Minute, []ratelimit.Option{ratelimit.WithRejectFunc(nil)}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mw, err := ratelimit.New(tt.limit, tt.period, tt.opts...)
			if !errors.Is(err, middleware.ErrInvalidConfig) || mw != nil {
				t.Fatalf("New = %v, %v; want nil, ErrInvalidConfig", mw, err)
			}
			if !strings.HasPrefix(err.Error(), "middleware: invalid configuration: ratelimit: ") {
				t.Errorf("error message = %q", err)
			}
		})
	}
}

func TestRateLimit_LimitAndRecovery(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var rej middleware.Rejection
		h := mustNew(t, 10, time.Minute, ratelimit.WithRejectFunc(func(w http.ResponseWriter, _ *http.Request, r middleware.Rejection) {
			rej = r
			w.WriteHeader(r.Status)
		}))(ok)
		for i := range 10 {
			if rec := request(t, h, "192.0.2.1:1000", nil); rec.Code != http.StatusOK {
				t.Fatalf("request %d refused with %d", i+1, rec.Code)
			}
		}
		rec := request(t, h, "192.0.2.1:1001", nil)
		if rec.Code != http.StatusTooManyRequests {
			t.Fatalf("11th request = %d, want 429", rec.Code)
		}
		if !errors.Is(rej.Err, ratelimit.ErrLimitExceeded) || rej.Status != http.StatusTooManyRequests {
			t.Errorf("rejection = %+v", rej)
		}
		// T = 6s; the 11th request would conform at 6s from now.
		if got := rec.Header().Get("Retry-After"); got != "6" {
			t.Errorf("Retry-After = %q, want 6", got)
		}
		if rec := request(t, h, "198.51.100.9:1000", nil); rec.Code != http.StatusOK {
			t.Errorf("another client was refused with %d", rec.Code)
		}

		time.Sleep(6 * time.Second)
		if rec := request(t, h, "192.0.2.1:1000", nil); rec.Code != http.StatusOK {
			t.Errorf("after 6s the request was refused with %d", rec.Code)
		}
		if rec := request(t, h, "192.0.2.1:1000", nil); rec.Code != http.StatusTooManyRequests {
			t.Errorf("the allowance recovered more than one request: %d", rec.Code)
		}
	})
}

func TestRateLimit_DefaultResponse(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		h := mustNew(t, 1, time.Hour)(ok)
		request(t, h, "192.0.2.1:1", nil)
		rec := request(t, h, "192.0.2.1:1", nil)
		if rec.Code != http.StatusTooManyRequests || !strings.Contains(rec.Body.String(), "Too Many Requests") {
			t.Errorf("response = %d %q", rec.Code, rec.Body.String())
		}
		if got := rec.Header().Get("Retry-After"); got != "3600" {
			t.Errorf("Retry-After = %q, want 3600", got)
		}
	})
}

func TestRateLimit_RetryAfterRoundsUp(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		h := mustNew(t, 2, time.Second)(ok) // T = 500ms, burst 2
		request(t, h, "192.0.2.1:1", nil)
		request(t, h, "192.0.2.1:1", nil)
		if got := request(t, h, "192.0.2.1:1", nil).Header().Get("Retry-After"); got != "1" {
			t.Errorf("Retry-After = %q, want 1 (500ms rounded up)", got)
		}
	})
}

func TestRateLimit_Burst(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		h := mustNew(t, 60, time.Minute, ratelimit.WithBurst(3))(ok) // 1 per second, bursts of 3
		for i := range 3 {
			if rec := request(t, h, "192.0.2.1:1", nil); rec.Code != http.StatusOK {
				t.Fatalf("burst request %d refused", i+1)
			}
		}
		if rec := request(t, h, "192.0.2.1:1", nil); rec.Code != http.StatusTooManyRequests {
			t.Fatalf("4th request = %d, want 429", rec.Code)
		}
		time.Sleep(time.Second)
		if rec := request(t, h, "192.0.2.1:1", nil); rec.Code != http.StatusOK {
			t.Errorf("after 1s = %d, want 200", rec.Code)
		}
	})
}

func TestRateLimit_Keys(t *testing.T) {
	t.Run("custom key", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			h := mustNew(t, 1, time.Hour, ratelimit.WithKey(func(r *http.Request) string {
				return r.Header.Get("X-Api-Key")
			}))(ok)
			for _, key := range []string{"alpha", "beta"} {
				if rec := request(t, h, "192.0.2.1:1", http.Header{"X-Api-Key": {key}}); rec.Code != http.StatusOK {
					t.Errorf("key %s refused", key)
				}
			}
			if rec := request(t, h, "192.0.2.1:1", http.Header{"X-Api-Key": {"alpha"}}); rec.Code != http.StatusTooManyRequests {
				t.Errorf("second alpha request = %d, want 429", rec.Code)
			}
			// No key: one shared allowance.
			request(t, h, "192.0.2.1:1", nil)
			if rec := request(t, h, "198.51.100.1:1", nil); rec.Code != http.StatusTooManyRequests {
				t.Errorf("requests without a key do not share an allowance: %d", rec.Code)
			}
		})
	})
	t.Run("ipv6 /64", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			h := mustNew(t, 1, time.Hour)(ok)
			request(t, h, "[2001:db8:1:2::1]:1", nil)
			if rec := request(t, h, "[2001:db8:1:2:ffff::9]:1", nil); rec.Code != http.StatusTooManyRequests {
				t.Errorf("same /64 = %d, want 429", rec.Code)
			}
			if rec := request(t, h, "[2001:db8:1:3::1]:1", nil); rec.Code != http.StatusOK {
				t.Errorf("other /64 = %d, want 200", rec.Code)
			}
		})
	})
	t.Run("unparseable remote address", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			h := mustNew(t, 1, time.Hour)(ok)
			request(t, h, "@sock", nil)
			if rec := request(t, h, "@sock", nil); rec.Code != http.StatusTooManyRequests {
				t.Errorf("same raw address = %d, want 429", rec.Code)
			}
			if rec := request(t, h, "@other", nil); rec.Code != http.StatusOK {
				t.Errorf("other raw address = %d, want 200", rec.Code)
			}
		})
	})
}

func TestRateLimit_Concurrent(t *testing.T) {
	h := mustNew(t, 1000, time.Hour)(ok)
	var wg sync.WaitGroup
	var mu sync.Mutex
	allowed := 0
	for i := range 50 {
		wg.Go(func() {
			for range 40 {
				rec := request(t, h, "192.0.2."+strconv.Itoa(i%5)+":1", nil)
				if rec.Code == http.StatusOK {
					mu.Lock()
					allowed++
					mu.Unlock()
				}
			}
		})
	}
	wg.Wait()
	if allowed != 2000 {
		t.Errorf("allowed %d of 2000 requests within a burst of 1000 per key", allowed)
	}
}

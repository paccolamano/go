package maxconcurrency_test

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

	"github.com/paccolamano/go/middleware"
	"github.com/paccolamano/go/middleware/maxconcurrency"
)

func mustNew(t *testing.T, limit int, opts ...maxconcurrency.Option) middleware.Middleware {
	t.Helper()
	mw, err := maxconcurrency.New(limit, opts...)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return mw
}

// gate is a handler that blocks until released and counts how many
// invocations run at the same time.
type gate struct {
	release chan struct{}
	running atomic.Int64
	peak    atomic.Int64
	calls   atomic.Int64
}

func newGate() *gate { return &gate{release: make(chan struct{})} }

func (g *gate) ServeHTTP(http.ResponseWriter, *http.Request) {
	g.calls.Add(1)
	n := g.running.Add(1)
	for {
		p := g.peak.Load()
		if n <= p || g.peak.CompareAndSwap(p, n) {
			break
		}
	}
	<-g.release
	g.running.Add(-1)
}

// start serves a request in a new goroutine and returns its recorder, which
// is safe to read after wg.Wait.
func start(ctx context.Context, wg *sync.WaitGroup, h http.Handler) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(ctx, http.MethodGet, "/", nil)
	wg.Go(func() { h.ServeHTTP(rec, req) })
	return rec
}

func TestMaxConcurrency_InvalidConfig(t *testing.T) {
	tests := []struct {
		name  string
		limit int
		opts  []maxconcurrency.Option
	}{
		{"zero limit", 0, nil},
		{"zero queue size", 1, []maxconcurrency.Option{maxconcurrency.WithQueue(0, time.Second)}},
		{"zero max wait", 1, []maxconcurrency.Option{maxconcurrency.WithQueue(1, 0)}},
		{"retry after below 1s", 1, []maxconcurrency.Option{maxconcurrency.WithRetryAfter(500 * time.Millisecond)}},
		{"nil reject func", 1, []maxconcurrency.Option{maxconcurrency.WithRejectFunc(nil)}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mw, err := maxconcurrency.New(tt.limit, tt.opts...)
			if !errors.Is(err, middleware.ErrInvalidConfig) || mw != nil {
				t.Fatalf("New = %v, %v; want nil, ErrInvalidConfig", mw, err)
			}
			if !strings.HasPrefix(err.Error(), "middleware: invalid configuration: maxconcurrency: ") {
				t.Errorf("error message = %q", err)
			}
		})
	}
}

func TestMaxConcurrency_RejectsAtLimit(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		g := newGate()
		var rej middleware.Rejection
		h := mustNew(t, 2, maxconcurrency.WithRejectFunc(func(w http.ResponseWriter, _ *http.Request, r middleware.Rejection) {
			rej = r
			w.WriteHeader(r.Status)
		}))(g)
		var wg sync.WaitGroup
		first := start(t.Context(), &wg, h)
		second := start(t.Context(), &wg, h)
		synctest.Wait()

		third := httptest.NewRecorder()
		h.ServeHTTP(third, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil))
		if third.Code != http.StatusServiceUnavailable || !errors.Is(rej.Err, maxconcurrency.ErrOverloaded) {
			t.Errorf("third = %d with %+v, want 503 with ErrOverloaded", third.Code, rej)
		}
		if third.Header().Get("Retry-After") != "" {
			t.Error("Retry-After set without WithRetryAfter")
		}

		g.release <- struct{}{} // one request completes
		synctest.Wait()
		fourth := start(t.Context(), &wg, h)
		synctest.Wait()
		if got := g.calls.Load(); got != 3 {
			t.Errorf("handler calls = %d, want 3 (the fourth request is admitted)", got)
		}
		close(g.release)
		wg.Wait()
		for i, rec := range []*httptest.ResponseRecorder{first, second, fourth} {
			if rec.Code != http.StatusOK {
				t.Errorf("admitted request %d = %d", i, rec.Code)
			}
		}
	})
}

func TestMaxConcurrency_RetryAfterRoundsUp(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		g := newGate()
		h := mustNew(t, 1, maxconcurrency.WithRetryAfter(1500*time.Millisecond))(g)
		var wg sync.WaitGroup
		start(t.Context(), &wg, h)
		synctest.Wait()
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil))
		if rec.Code != http.StatusServiceUnavailable || rec.Header().Get("Retry-After") != "2" {
			t.Errorf("response = %d Retry-After=%q, want 503 and 2", rec.Code, rec.Header().Get("Retry-After"))
		}
		if !strings.Contains(rec.Body.String(), "Service Unavailable") {
			t.Errorf("default body = %q", rec.Body.String())
		}
		close(g.release)
		wg.Wait()
	})
}

func TestMaxConcurrency_Queue(t *testing.T) {
	t.Run("proceeds when a slot frees", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			g := newGate()
			h := mustNew(t, 1, maxconcurrency.WithQueue(1, time.Minute))(g)
			var wg sync.WaitGroup
			start(t.Context(), &wg, h)
			synctest.Wait()
			queued := start(t.Context(), &wg, h)
			synctest.Wait()
			if g.calls.Load() != 1 {
				t.Fatal("the queued request ran before a slot was free")
			}
			time.Sleep(30 * time.Second)
			g.release <- struct{}{}
			synctest.Wait()
			if g.calls.Load() != 2 {
				t.Fatal("the queued request did not run after a slot freed")
			}
			close(g.release)
			wg.Wait()
			if queued.Code != http.StatusOK {
				t.Errorf("queued request = %d, want 200", queued.Code)
			}
		})
	})
	t.Run("wait expires", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			g := newGate()
			h := mustNew(t, 1, maxconcurrency.WithQueue(1, time.Minute))(g)
			var wg sync.WaitGroup
			start(t.Context(), &wg, h)
			synctest.Wait() // the first request holds the slot
			queued := start(t.Context(), &wg, h)
			synctest.Wait()
			time.Sleep(time.Minute + time.Second)
			synctest.Wait()
			if queued.Code != http.StatusServiceUnavailable || g.calls.Load() != 1 {
				t.Errorf("queued = %d, calls = %d; want 503 and 1", queued.Code, g.calls.Load())
			}
			close(g.release)
			wg.Wait()
		})
	})
	t.Run("queue full", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			g := newGate()
			h := mustNew(t, 1, maxconcurrency.WithQueue(1, time.Minute))(g)
			var wg sync.WaitGroup
			start(t.Context(), &wg, h)
			synctest.Wait()
			start(t.Context(), &wg, h)
			synctest.Wait()
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil))
			if rec.Code != http.StatusServiceUnavailable {
				t.Errorf("third request = %d, want 503 (queue full)", rec.Code)
			}
			close(g.release)
			wg.Wait()
		})
	})
	t.Run("client goes away", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			g := newGate()
			h := mustNew(t, 1, maxconcurrency.WithQueue(1, time.Minute))(g)
			var wg sync.WaitGroup
			start(t.Context(), &wg, h)
			synctest.Wait()
			ctx, cancel := context.WithCancel(t.Context())
			queued := start(ctx, &wg, h)
			synctest.Wait()
			cancel()
			synctest.Wait()
			if queued.Code != http.StatusServiceUnavailable || g.calls.Load() != 1 {
				t.Errorf("queued = %d, calls = %d; want 503 and the handler not invoked", queued.Code, g.calls.Load())
			}
			close(g.release)
			wg.Wait()
		})
	})
}

func TestMaxConcurrency_ReleasesSlots(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		const limit = 4
		var running, peak atomic.Int64
		hold := make(chan struct{})
		h := mustNew(t, limit)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
			n := running.Add(1)
			defer running.Add(-1)
			for {
				p := peak.Load()
				if n <= p || peak.CompareAndSwap(p, n) {
					break
				}
			}
			time.Sleep(time.Millisecond)
			switch r.URL.Path {
			case "/panic":
				panic("boom")
			case "/cancel":
				<-r.Context().Done()
			case "/block":
				<-hold
			}
		}))
		serve := func(ctx context.Context, path string) *httptest.ResponseRecorder {
			defer func() { _ = recover() }()
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequestWithContext(ctx, http.MethodGet, path, nil))
			return rec
		}

		// 10× overload with panics and cancellations.
		var wg sync.WaitGroup
		for i := range 10_000 {
			path := []string{"/ok", "/panic", "/cancel"}[i%3]
			ctx, cancel := context.WithCancel(t.Context())
			wg.Go(func() {
				defer cancel()
				if path == "/cancel" {
					go func() {
						time.Sleep(2 * time.Millisecond)
						cancel()
					}()
				}
				serve(ctx, path)
			})
		}
		wg.Wait()
		if p := peak.Load(); p > limit {
			t.Errorf("peak concurrency = %d, want at most %d", p, limit)
		}

		// No slot leaked: limit blocking requests are all admitted, and one
		// more is refused.
		for range limit {
			wg.Go(func() { serve(t.Context(), "/block") })
		}
		synctest.Wait()
		if n := running.Load(); n != limit {
			t.Errorf("%d requests running after the load, want %d: slots leaked", n, limit)
		}
		if rec := serve(t.Context(), "/ok"); rec.Code != http.StatusServiceUnavailable {
			t.Errorf("request beyond the limit = %d, want 503", rec.Code)
		}
		close(hold)
		wg.Wait()
	})
}

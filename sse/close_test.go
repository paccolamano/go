package sse_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/paccolamano/go/sse"
)

// TestShutdown_Hook checks that a graceful shutdown with the broker's Close as
// its hook completes before its deadline, and that every client sees a
// complete response (SC-007).
func TestShutdown_Hook(t *testing.T) {
	e2e(t, func(e *env) {
		t := e.T
		const n = 1000
		b := newBroker(t)
		results := serveBroker(e, b)
		e.Server.RegisterOnShutdown(b.Close)
		ends := make([]<-chan error, 0, n)
		for range n {
			_, errc := subscribe(e, t.Context(), "t", nil)
			ends = append(ends, errc)
		}

		ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
		defer cancel()
		start := time.Now()
		if err := e.Server.Shutdown(ctx); err != nil {
			t.Fatalf("Shutdown = %v", err)
		}
		if took := time.Since(start); took >= 30*time.Second {
			t.Fatalf("Shutdown waited %v", took)
		}
		for i, end := range ends {
			if err := <-end; err != nil {
				t.Fatalf("client %d did not see a complete response: %v", i, err)
			}
		}
		for range n {
			if err := <-results; !errors.Is(err, sse.ErrClosed) {
				t.Fatalf("Serve = %v, want ErrClosed", err)
			}
		}
	})
}

func TestClose_ServeAfter(t *testing.T) {
	b := newBroker(t, sse.WithRetry(2*time.Second))
	b.Close()
	rec := httptest.NewRecorder()
	err := b.Serve(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil), "t")
	if !errors.Is(err, sse.ErrClosed) {
		t.Fatalf("Serve = %v, want ErrClosed", err)
	}
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "text/event-stream" {
		t.Fatalf("status %d, content type %q", rec.Code, rec.Header().Get("Content-Type"))
	}
	if got := rec.Body.String(); got != "retry: 2000\n\n" {
		t.Fatalf("body %q, want only the reconnection time", got)
	}
}

func TestClose_PublishAfter(t *testing.T) {
	b := newBroker(t)
	b.Close()
	if err := b.Publish(sse.Event{Data: "late"}, "t"); !errors.Is(err, sse.ErrClosed) {
		t.Fatalf("Publish = %v, want ErrClosed", err)
	}
}

func TestClose_Twice(t *testing.T) {
	b := newBroker(t)
	var wg sync.WaitGroup
	for range 10 {
		wg.Go(func() {
			for range 100 {
				if err := b.Publish(sse.Event{Data: "x"}, "t"); err != nil && !errors.Is(err, sse.ErrClosed) {
					t.Error(err)
				}
			}
		})
		wg.Go(b.Close)
	}
	wg.Wait()
	b.Close()
}

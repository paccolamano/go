package health_test

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/paccolamano/go/lifecycle/health"
)

// blockUntil returns a check that ignores its context until release is closed.
func blockUntil(release <-chan struct{}) func(context.Context) error {
	return func(context.Context) error {
		<-release
		return nil
	}
}

func TestEvaluation_Timeout(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		release := make(chan struct{})
		defer close(release)
		logs := newLogRecorder()
		h := mustNew(t, []health.Check{{Name: "slow", Func: blockUntil(release)}}, health.WithLogger(slog.New(logs)))

		start := time.Now()
		rec := serve(t.Context(), h, http.MethodGet, "/readyz")
		if elapsed := time.Since(start); elapsed != time.Second {
			t.Fatalf("answered after %v, want 1s", elapsed)
		}
		if rec.Code != http.StatusServiceUnavailable || !strings.Contains(rec.Body.String(), "[-]slow failed") {
			t.Fatalf("response = %d %q, want slow failed", rec.Code, rec.Body.String())
		}
		recs := logs.all()
		if len(recs) != 1 || recs[0].attrs["reason"].String() != "timeout" || recs[0].attrs["timeout"].Duration() != time.Second {
			t.Fatalf("records = %+v, want reason=timeout timeout=1s", recs)
		}
	})
}

func TestEvaluation_CustomTimeout(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		release := make(chan struct{})
		defer close(release)
		h := mustNew(t, []health.Check{{Name: "slow", Func: blockUntil(release), Timeout: 3 * time.Second}})

		start := time.Now()
		serve(t.Context(), h, http.MethodGet, "/readyz")
		if elapsed := time.Since(start); elapsed != 3*time.Second {
			t.Fatalf("answered after %v, want 3s", elapsed)
		}
	})
}

func TestEvaluation_TimeoutWhenContextHonored(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		logs := newLogRecorder()
		h := mustNew(t, []health.Check{{Name: "slow", Func: func(ctx context.Context) error {
			<-ctx.Done()
			return ctx.Err()
		}}}, health.WithLogger(slog.New(logs)))

		serve(t.Context(), h, http.MethodGet, "/readyz")
		if recs := logs.all(); len(recs) != 1 || recs[0].attrs["reason"].String() != "timeout" {
			t.Fatalf("records = %+v, want reason=timeout", recs)
		}
	})
}

func TestEvaluation_Panic(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var calls atomic.Int32
		logs := newLogRecorder()
		h := mustNew(t, []health.Check{{Name: "bad", Func: counted(&calls, func(context.Context) error {
			panic("boom-value")
		})}}, health.WithLogger(slog.New(logs)))

		if rec := serve(t.Context(), h, http.MethodGet, "/readyz"); rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("status = %d, want 503", rec.Code)
		}
		recs := logs.all()
		if len(recs) != 1 || recs[0].attrs["reason"].String() != "panic" || recs[0].attrs["panic"].String() != "boom-value" ||
			recs[0].attrs["stack"].String() == "" {
			t.Fatalf("records = %+v, want reason=panic with the value and a stack", recs)
		}
		if rec := serve(t.Context(), h, http.MethodGet, "/readyz"); rec.Code != http.StatusServiceUnavailable || calls.Load() != 2 {
			t.Fatalf("second request = %d after %d calls, want 503 after 2", rec.Code, calls.Load())
		}
	})
}

func TestEvaluation_StillRunning(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var calls atomic.Int32
		logs := newLogRecorder()
		h := mustNew(t, []health.Check{{Name: "stuck", Func: counted(&calls, func(context.Context) error {
			if calls.Load() == 1 {
				time.Sleep(10 * time.Second) // the first call ignores its context
			}
			return nil
		})}}, health.WithLogger(slog.New(logs)))

		start := time.Now()
		if rec := serve(t.Context(), h, http.MethodGet, "/readyz"); rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("first request = %d, want 503", rec.Code)
		}
		time.Sleep(time.Until(start.Add(2 * time.Second)))
		before := time.Now()
		if rec := serve(t.Context(), h, http.MethodGet, "/readyz"); rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("second request = %d, want 503", rec.Code)
		}
		if calls.Load() != 1 || time.Since(before) != 0 {
			t.Fatalf("calls = %d, answered after %v; want 1 and at once", calls.Load(), time.Since(before))
		}
		recs := logs.all()
		if len(recs) != 2 || recs[1].attrs["reason"].String() != "still running" {
			t.Fatalf("records = %+v, want the second with reason=still running", recs)
		}
		time.Sleep(time.Until(start.Add(11 * time.Second)))
		if rec := serve(t.Context(), h, http.MethodGet, "/readyz"); rec.Code != http.StatusOK || calls.Load() != 2 {
			t.Fatalf("after the check returned: status %d, calls %d; want 200 and 2", rec.Code, calls.Load())
		}
	})
}

func TestEvaluation_Shared(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var calls atomic.Int32
		h := mustNew(t, []health.Check{{Name: "db", Func: counted(&calls, func(context.Context) error {
			time.Sleep(500 * time.Millisecond)
			return nil
		})}})

		var wg sync.WaitGroup
		codes := make([]int, 100)
		bodies := make([]string, 100)
		for i := range 100 {
			wg.Go(func() {
				rec := serve(t.Context(), h, http.MethodGet, "/readyz?verbose")
				codes[i], bodies[i] = rec.Code, rec.Body.String()
			})
		}
		wg.Wait()
		if calls.Load() != 1 {
			t.Fatalf("calls = %d, want 1 for 100 concurrent requests", calls.Load())
		}
		for i := range 100 {
			if codes[i] != http.StatusOK || bodies[i] != "[+]db ok\nok\n" {
				t.Fatalf("request %d = %d %q, want 200 and the shared result", i, codes[i], bodies[i])
			}
		}
	})
}

func TestEvaluation_NewAfterComplete(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var calls atomic.Int32
		h := mustNew(t, []health.Check{{Name: "db", Func: counted(&calls, ok)}})
		serve(t.Context(), h, http.MethodGet, "/readyz")
		serve(t.Context(), h, http.MethodGet, "/readyz")
		if calls.Load() != 2 {
			t.Fatalf("calls = %d, want 2: a completed result is never reused", calls.Load())
		}
	})
}

func TestEvaluation_CancelWhenAllGone(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		logs := newLogRecorder()
		var checkErr error
		h := mustNew(t, []health.Check{{Name: "db", Timeout: 10 * time.Second, Func: func(ctx context.Context) error {
			<-ctx.Done()
			checkErr = ctx.Err()
			return checkErr
		}}}, health.WithLogger(slog.New(logs)))

		ctx, cancel := context.WithCancel(t.Context())
		rec := make(chan int, 1)
		go func() {
			r := serve(ctx, h, http.MethodGet, "/readyz")
			rec <- r.Body.Len()
		}()
		time.Sleep(time.Second)
		cancel()
		if n := <-rec; n != 0 {
			t.Fatalf("wrote %d body bytes for a request that had gone, want 0", n)
		}
		synctest.Wait()
		if !errors.Is(checkErr, context.Canceled) {
			t.Fatalf("check context = %v, want canceled", checkErr)
		}
		if recs := logs.all(); len(recs) != 0 {
			t.Fatalf("records = %+v, want none for an abandoned evaluation", recs)
		}
	})
}

func TestEvaluation_KeptWhileWaiting(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		h := mustNew(t, []health.Check{{Name: "db", Timeout: 10 * time.Second, Func: func(ctx context.Context) error {
			select {
			case <-time.After(2 * time.Second):
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		}}})

		ctx1, cancel1 := context.WithCancel(t.Context())
		gone := make(chan struct{})
		go func() {
			serve(ctx1, h, http.MethodGet, "/readyz")
			close(gone)
		}()
		second := make(chan int, 1)
		go func() { second <- serve(t.Context(), h, http.MethodGet, "/readyz").Code }()
		time.Sleep(time.Second)
		cancel1()
		<-gone
		if code := <-second; code != http.StatusOK {
			t.Fatalf("remaining request = %d, want 200: the evaluation must go on while a request waits", code)
		}
	})
}

type ctxKey struct{}

func TestEvaluation_ContextValues(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var got any
		h := mustNew(t, []health.Check{{Name: "db", Func: func(ctx context.Context) error {
			got = ctx.Value(ctxKey{})
			return nil
		}}})
		serve(context.WithValue(t.Context(), ctxKey{}, "v"), h, http.MethodGet, "/readyz")
		if got != "v" {
			t.Fatalf("value = %v, want v", got)
		}
	})
}

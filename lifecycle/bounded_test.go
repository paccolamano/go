package lifecycle_test

import (
	"errors"
	"log/slog"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/paccolamano/go/lifecycle"
)

func TestDeadline_IgnoresCancellation(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		log := newEventLog()
		stuck := &plainComp{name: "stuck", log: log, ignoreCancel: true, release: make(chan struct{})}
		defer close(stuck.release)
		b := newStopper("b", log, stopReturns)
		app := mustNew(t, lifecycle.WithStopTimeout(5*time.Second))
		app.Add("a", &plainComp{name: "a", log: log})
		app.Add("b", b)
		app.Add("stuck", stuck)

		start := time.Now()
		err := stopAfterLaunch(t, app)
		if elapsed := time.Since(start); elapsed != 5*time.Second {
			t.Fatalf("Run returned after %v, want 5s", elapsed)
		}
		if ce := componentError(err, "stuck"); ce == nil || !errors.Is(ce, lifecycle.ErrStopTimeout) {
			t.Fatalf("Run = %v, want ErrStopTimeout for stuck", err)
		}
		synctest.Wait()
		if b.stopCalls.Load() != 1 {
			t.Fatal("b did not receive Stop")
		}
		if !log.has("a", "canceled") {
			t.Fatal("a did not receive the cancellation of its context")
		}
	})
}

func TestDeadline_StopNeverReturns(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		log := newEventLog()
		q := newStopper("q", log, stopNever)
		defer close(q.release)
		app := mustNew(t, lifecycle.WithStopTimeout(5*time.Second))
		app.Add("q", q)

		start := time.Now()
		err := stopAfterLaunch(t, app)
		if elapsed := time.Since(start); elapsed != 5*time.Second {
			t.Fatalf("Run returned after %v, want 5s", elapsed)
		}
		if ce := componentError(err, "q"); ce == nil || !errors.Is(ce, lifecycle.ErrStopTimeout) {
			t.Fatalf("Run = %v, want ErrStopTimeout for q", err)
		}
	})
}

func TestExpired_RemainingNotWaited(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		log := newEventLog()
		var closed atomic.Bool
		slow := &plainComp{name: "slow", log: log, ignoreCancel: true, release: make(chan struct{})}
		defer close(slow.release)
		app := mustNew(t, lifecycle.WithStopTimeout(5*time.Second))
		app.Add("closer", lifecycle.Closer(func() { closed.Store(true) }))
		app.Add("p", &plainComp{name: "p", log: log, workAfterCancel: time.Second})
		app.Add("slow", slow)

		start := time.Now()
		err := stopAfterLaunch(t, app)
		if elapsed := time.Since(start); elapsed != 5*time.Second {
			t.Fatalf("Run returned after %v, want 5s without waiting for the remaining components", elapsed)
		}
		for _, name := range []string{"slow", "p"} {
			if ce := componentError(err, name); ce == nil || !errors.Is(ce, lifecycle.ErrStopTimeout) {
				t.Errorf("Run = %v, want ErrStopTimeout for %s", err, name)
			}
		}
		synctest.Wait()
		if !closed.Load() {
			t.Fatal("the close function did not run after the deadline")
		}
		if !log.has("p", "canceled") {
			t.Fatal("p did not receive the cancellation of its context")
		}
		time.Sleep(time.Second)
		synctest.Wait()
		if !log.has("p", "returned") {
			t.Fatal("p did not return after its cancellation")
		}
	})
}

func TestDeadline_Log(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		rec := newLogRecorder()
		stuck := &plainComp{name: "stuck", log: newEventLog(), ignoreCancel: true, release: make(chan struct{})}
		defer close(stuck.release)
		app := mustNew(t, lifecycle.WithStopTimeout(5*time.Second), lifecycleLogger(rec))
		app.Add("stuck", stuck)

		_ = stopAfterLaunch(t, app)
		recs := rec.find("lifecycle: component not stopped in time")
		if len(recs) != 1 || recs[0].level != slog.LevelError || recs[0].attrs["component"].String() != "stuck" ||
			recs[0].attrs["duration"].Kind() != slog.KindDuration {
			t.Fatalf("records = %+v, want one at level ERROR with component=stuck and a duration", recs)
		}
	})
}

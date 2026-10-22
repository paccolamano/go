package lifecycle_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/paccolamano/go/lifecycle"
)

// stopAfterLaunch runs app, triggers the stop once every component is running,
// and returns the result of Run.
func stopAfterLaunch(t *testing.T, app *lifecycle.App) error {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	done := runAsync(ctx, app)
	synctest.Wait()
	cancel()
	return <-done
}

func TestStopper_InFlightCompletes(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		log := newEventLog()
		q := newStopper("q", log, stopReturns)
		q.item = 3 * time.Second
		app := mustNew(t)
		app.Add("q", q)

		if err := stopAfterLaunch(t, app); err != nil {
			t.Fatalf("Run = %v, want nil", err)
		}
		if !q.itemDone.Load() || !q.itemCtxValid.Load() {
			t.Fatalf("item done %v with a valid context %v, want both", q.itemDone.Load(), q.itemCtxValid.Load())
		}
		if log.has("q", "canceled") {
			t.Fatal("the context of q was cancelled")
		}
	})
}

func TestStopper_WaitsForRun(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		log := newEventLog()
		q := newStopper("q", log, stopReturns)
		q.item = 2 * time.Second
		app := mustNew(t)
		app.Add("first", &plainComp{name: "first", log: log})
		app.Add("q", q)

		if err := stopAfterLaunch(t, app); err != nil {
			t.Fatalf("Run = %v, want nil", err)
		}
		stopAt, _ := log.at("q", "stop")
		firstAt, _ := log.at("first", "canceled")
		if got := firstAt - stopAt; got != 2*time.Second {
			t.Fatalf("first stopped %v after q's Stop, want 2s", got)
		}
		if log.has("q", "canceled") {
			t.Fatal("the context of q was cancelled")
		}
	})
}

func TestStopper_DeadlineCancels(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		log := newEventLog()
		q := newStopper("q", log, stopReturns)
		q.item = time.Hour
		app := mustNew(t, lifecycle.WithStopTimeout(5*time.Second))
		app.Add("q", q)

		err := stopAfterLaunch(t, app)
		if !errors.Is(err, lifecycle.ErrStopTimeout) {
			t.Fatalf("Run = %v, want ErrStopTimeout", err)
		}
		var ce *lifecycle.ComponentError
		if !errors.As(err, &ce) || ce.Name != "q" {
			t.Fatalf("ComponentError = %v, want name q", ce)
		}
		synctest.Wait()
		stopAt, _ := log.at("q", "stop")
		canceledAt, ok := log.at("q", "canceled")
		if !ok || canceledAt-stopAt != 5*time.Second {
			t.Fatalf("context cancelled %v after Stop (seen %v), want 5s", canceledAt-stopAt, ok)
		}
	})
}

func TestStopper_StopError(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		log := newEventLog()
		q := newStopper("q", log, stopFails)
		q.stopErr = errors.New("boom")
		q.item = time.Second
		app := mustNew(t)
		app.Add("q", q)

		err := stopAfterLaunch(t, app)
		if err == nil || !strings.Contains(err.Error(), `lifecycle: component "q": stop: boom`) {
			t.Fatalf("Run = %v, want the stop error of q", err)
		}
		if !log.has("q", "returned") {
			t.Fatal("Run returned before q's Run")
		}
	})
}

func TestStopper_StopContextErrorSuppressed(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		log := newEventLog()
		q := newStopper("q", log, stopCtxErr)
		q.item = time.Hour
		app := mustNew(t, lifecycle.WithStopTimeout(5*time.Second))
		app.Add("q", q)

		err := stopAfterLaunch(t, app)
		if !errors.Is(err, lifecycle.ErrStopTimeout) {
			t.Fatalf("Run = %v, want ErrStopTimeout", err)
		}
		if strings.Contains(err.Error(), "stop:") {
			t.Fatalf("Run = %q, want no stop error for the context's own error", err)
		}
		synctest.Wait()
	})
}

func TestStopper_WithoutStop(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		log := newEventLog()
		app := mustNew(t)
		app.Add("p", &plainComp{name: "p", log: log})

		if err := stopAfterLaunch(t, app); err != nil {
			t.Fatalf("Run = %v, want nil", err)
		}
		if at, ok := log.at("p", "canceled"); !ok || at != 0 {
			t.Fatalf("p cancelled at %v (seen %v), want at the start of its turn", at, ok)
		}
	})
}

func TestStopper_CanceledIsClean(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		log := newEventLog()
		app := mustNew(t)
		app.Add("plain", &plainComp{name: "plain", log: log})
		app.Add("wrapped", &plainComp{name: "wrapped", log: log, afterErr: fmt.Errorf("wrap: %w", context.Canceled)})

		if err := stopAfterLaunch(t, app); err != nil {
			t.Fatalf("Run = %v, want nil", err)
		}
	})
}

func TestStopper_OtherErrorAfterStop(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		log := newEventLog()
		app := mustNew(t)
		app.Add("p", &plainComp{name: "p", log: log, afterErr: errors.New("x")})

		err := stopAfterLaunch(t, app)
		if err == nil || !strings.Contains(err.Error(), `lifecycle: component "p": x`) {
			t.Fatalf("Run = %v, want the error of p", err)
		}
	})
}

package lifecycle_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"testing"
	"testing/synctest"
	"time"

	"github.com/paccolamano/go/lifecycle"
)

func TestDrain_ServingContinues(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		log := newEventLog()
		ln := newPipeNet()
		srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = io.WriteString(w, "ok")
		})}
		probe := newStopper("probe", log, stopReturns)
		app := mustNew(t, lifecycle.WithDrainDelay(10*time.Second))
		app.Add("worker", &plainComp{name: "worker", log: log})
		app.Add("http", lifecycle.HTTPServer(srv, lifecycle.WithListener(ln)))
		app.Add("probe", probe) // stopped first: it marks the end of the drain

		ctx, cancel := context.WithCancel(t.Context())
		done := runAsync(ctx, app)
		synctest.Wait()
		cancel()
		synctest.Wait()
		if err := app.Ready(t.Context()); !errors.Is(err, lifecycle.ErrNotRunning) {
			t.Fatalf("Ready right after the trigger = %v, want ErrNotRunning", err)
		}

		time.Sleep(5 * time.Second)
		if r := <-getAsync(t, t.Context(), newPipeClient(ln), baseURL+"/"); r.err != nil || r.status != http.StatusOK {
			t.Fatalf("request during the drain = %+v, want 200", r)
		}
		if err := <-done; err != nil {
			t.Fatalf("Run = %v, want nil", err)
		}
		if at, ok := log.at("probe", "stop"); !ok || at != 10*time.Second {
			t.Fatalf("first stop at %v (seen %v), want 10s", at, ok)
		}
	})
}

func TestDrain_DefaultZero(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		log := newEventLog()
		app := mustNew(t)
		app.Add("q", newStopper("q", log, stopReturns))

		if err := stopAfterLaunch(t, app); err != nil {
			t.Fatalf("Run = %v, want nil", err)
		}
		if at, ok := log.at("q", "stop"); !ok || at != 0 {
			t.Fatalf("first stop at %v (seen %v), want 0", at, ok)
		}
	})
}

func TestDrain_FailureTrigger(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		log := newEventLog()
		app := mustNew(t, lifecycle.WithDrainDelay(10*time.Second))
		app.Add("a", newStopper("a", log, stopReturns))
		app.Add("b", &plainComp{name: "b", log: log, returnAfter: time.Second, err: errors.New("boom")})

		if err := app.Run(t.Context()); err == nil {
			t.Fatal("Run = nil, want the failure of b")
		}
		if at, ok := log.at("a", "stop"); !ok || at != 11*time.Second {
			t.Fatalf("a stopped at %v (seen %v), want 11s", at, ok)
		}
	})
}

func TestDrain_ReturnsDuringDrain(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		log := newEventLog()
		late := errors.New("late")
		app := mustNew(t, lifecycle.WithDrainDelay(10*time.Second))
		app.Add("a", &plainComp{name: "a", log: log, returnAfter: 5 * time.Second, err: late})
		app.Add("b", &plainComp{name: "b", log: log})

		err := stopAfterLaunch(t, app)
		if ce := componentError(err, "a"); ce == nil || !errors.Is(ce, late) {
			t.Fatalf("Run = %v, want the later failure of a", err)
		}
		if log.has("a", "canceled") {
			t.Fatal("a was stopped again after it had returned")
		}
	})
}

func TestDrain_Log(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		rec := newLogRecorder()
		app := mustNew(t, lifecycle.WithDrainDelay(10*time.Second), lifecycleLogger(rec))
		app.Add("w", &plainComp{name: "w", log: newEventLog()})

		if err := stopAfterLaunch(t, app); err != nil {
			t.Fatalf("Run = %v, want nil", err)
		}
		recs := rec.find("lifecycle: drain started")
		if len(recs) != 1 || recs[0].attrs["delay"].Kind() != slog.KindDuration || recs[0].attrs["delay"].Duration() != 10*time.Second {
			t.Fatalf("drain records = %+v, want one with delay=10s", recs)
		}
	})
}

func TestReady_States(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		app := mustNew(t, lifecycle.WithDrainDelay(10*time.Second))
		app.Add("w", &plainComp{name: "w", log: newEventLog()})

		check := func(when, want string) {
			t.Helper()
			err := app.Ready(t.Context())
			if want == "" {
				if err != nil {
					t.Fatalf("Ready %s = %v, want nil", when, err)
				}
				return
			}
			if !errors.Is(err, lifecycle.ErrNotRunning) || err.Error() != want {
				t.Fatalf("Ready %s = %v, want %q", when, err, want)
			}
		}

		check("before Run", "lifecycle: not running: starting")
		ctx, cancel := context.WithCancel(t.Context())
		done := runAsync(ctx, app)
		synctest.Wait()
		check("while running", "")
		cancel()
		synctest.Wait()
		check("during the drain", "lifecycle: not running: stopping")
		if err := <-done; err != nil {
			t.Fatalf("Run = %v, want nil", err)
		}
		check("after Run", "lifecycle: not running: stopped")
	})
}

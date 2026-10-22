package lifecycle_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/paccolamano/go/lifecycle"
)

// componentError returns the *ComponentError named name among the errors joined
// by Run.
func componentError(err error, name string) *lifecycle.ComponentError {
	var joined interface{ Unwrap() []error }
	if !errors.As(err, &joined) {
		return nil
	}
	for _, e := range joined.Unwrap() {
		var ce *lifecycle.ComponentError
		if errors.As(e, &ce) && ce.Name == name {
			return ce
		}
	}
	return nil
}

func TestFailure_Error(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		log := newEventLog()
		rec := newLogRecorder()
		boom := errors.New("boom")
		b := newStopper("b", log, stopReturns)
		b.returnAfter, b.err = time.Second, boom
		app := mustNew(t, lifecycleLogger(rec))
		app.Add("a", &plainComp{name: "a", log: log})
		app.Add("b", b)
		app.Add("c", &plainComp{name: "c", log: log})

		err := app.Run(t.Context())
		ce := componentError(err, "b")
		if ce == nil || !errors.Is(ce, boom) {
			t.Fatalf("Run = %v, want a ComponentError for b wrapping boom", err)
		}
		if got := log.order("canceled"); !slices.Equal(got, []string{"c", "a"}) {
			t.Fatalf("stop order = %v, want [c a]", got)
		}
		if n := b.stopCalls.Load(); n != 0 {
			t.Fatalf("b's Stop called %d times, want 0", n)
		}
		triggers := rec.find("lifecycle: stop triggered")
		if len(triggers) != 1 || triggers[0].attrs["trigger"].String() != "component" || triggers[0].attrs["component"].String() != "b" {
			t.Fatalf("stop triggered records = %+v, want trigger=component component=b", triggers)
		}
	})
}

func TestFailure_ExitedEarly(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		log := newEventLog()
		app := mustNew(t)
		app.Add("a", &plainComp{name: "a", log: log})
		app.Add("b", &plainComp{name: "b", log: log, returnAfter: time.Second})

		err := app.Run(t.Context())
		if ce := componentError(err, "b"); ce == nil || !errors.Is(ce, lifecycle.ErrExitedEarly) {
			t.Fatalf("Run = %v, want ErrExitedEarly for b", err)
		}
		if !log.has("a", "canceled") {
			t.Fatal("a was not stopped")
		}
	})
}

func TestFailure_Panic(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		log := newEventLog()
		app := mustNew(t)
		app.Add("a", &plainComp{name: "a", log: log})
		app.Add("b", &plainComp{name: "b", log: log, returnAfter: time.Second, panicValue: "bad"})

		err := app.Run(t.Context())
		var pe *lifecycle.PanicError
		if ce := componentError(err, "b"); ce == nil || !errors.As(ce, &pe) {
			t.Fatalf("Run = %v, want a PanicError for b", err)
		}
		if pe.Value != "bad" || !strings.Contains(string(pe.Stack), "plainComp") {
			t.Fatalf("PanicError = %v with stack %q, want value bad and the panicking frame", pe.Value, pe.Stack)
		}
		if !log.has("a", "canceled") {
			t.Fatal("a was not stopped")
		}
	})
}

func TestFailure_StopPanic(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		log := newEventLog()
		q := newStopper("q", log, stopPanics)
		q.item = time.Second
		app := mustNew(t)
		app.Add("q", q)

		err := stopAfterLaunch(t, app)
		var pe *lifecycle.PanicError
		if !errors.As(err, &pe) || pe.Value != "stop panic" {
			t.Fatalf("Run = %v, want a PanicError from Stop", err)
		}
		if !strings.Contains(err.Error(), `lifecycle: component "q": stop: panic: stop panic`) {
			t.Fatalf("Run = %q, want the stop error of q", err)
		}
		if !log.has("q", "returned") {
			t.Fatal("Run returned before q's Run")
		}
	})
}

func TestFailure_Simultaneous(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		log := newEventLog()
		e1, e2 := errors.New("e1"), errors.New("e2")
		app := mustNew(t)
		app.Add("a", &plainComp{name: "a", log: log})
		app.Add("b", &plainComp{name: "b", log: log, returnAfter: time.Second, err: e1})
		app.Add("c", &plainComp{name: "c", log: log, returnAfter: time.Second, err: e2})

		err := app.Run(t.Context())
		if !errors.Is(err, e1) || !errors.Is(err, e2) {
			t.Fatalf("Run = %v, want both failures", err)
		}
		if got := log.order("canceled"); !slices.Equal(got, []string{"a"}) {
			t.Fatalf("cancellations = %v, want a once", got)
		}
	})
}

func TestFailure_DuringStop(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		log := newEventLog()
		late := errors.New("late")
		app := mustNew(t)
		app.Add("a", &plainComp{name: "a", log: log, returnAfter: 3 * time.Second, err: late})
		app.Add("b", &plainComp{name: "b", log: log, workAfterCancel: 5 * time.Second})

		err := stopAfterLaunch(t, app)
		if ce := componentError(err, "a"); ce == nil || !errors.Is(ce, late) {
			t.Fatalf("Run = %v, want the later failure of a", err)
		}
		if log.has("a", "canceled") {
			t.Fatal("a was stopped again after it had returned")
		}
	})
}

func TestFailure_ReadyAfterTrigger(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		log := newEventLog()
		app := mustNew(t)
		app.Add("a", &plainComp{name: "a", log: log, workAfterCancel: 5 * time.Second})
		app.Add("b", &plainComp{name: "b", log: log, returnAfter: time.Second})

		done := runAsync(t.Context(), app)
		time.Sleep(time.Second)
		synctest.Wait()
		if err := app.Ready(t.Context()); !errors.Is(err, lifecycle.ErrNotRunning) || !strings.Contains(err.Error(), "stopping") {
			t.Fatalf("Ready = %v, want not running: stopping", err)
		}
		<-done
	})
}

func TestFailure_StartStopStartError(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var stopped bool
		app := mustNew(t)
		app.Add("svc", lifecycle.StartStop(
			func(context.Context) error {
				time.Sleep(time.Second)
				return errors.New("start failed")
			},
			func(context.Context) error {
				stopped = true
				return nil
			},
		))

		err := app.Run(t.Context())
		if ce := componentError(err, "svc"); ce == nil || !strings.Contains(ce.Error(), "start failed") {
			t.Fatalf("Run = %v, want the start failure of svc", err)
		}
		if stopped {
			t.Fatal("stop was called after start failed")
		}
	})
}

package lifecycle_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/paccolamano/go/lifecycle"
)

func TestStartStop_StartAtLaunch(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var started bool
		var value any
		app := mustNew(t)
		app.Add("svc", lifecycle.StartStop(
			func(ctx context.Context) error {
				started = true
				value = ctx.Value(ctxKey{})
				return nil
			},
			func(context.Context) error { return nil },
		))

		ctx, cancel := context.WithCancel(context.WithValue(t.Context(), ctxKey{}, "v"))
		done := runAsync(ctx, app)
		synctest.Wait()
		if !started || value != "v" {
			t.Fatalf("started %v with value %v, want true and v", started, value)
		}
		cancel()
		if err := <-done; err != nil {
			t.Fatalf("Run = %v, want nil", err)
		}
	})
}

func TestStartStop_StopAtTurn(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		log := newEventLog()
		var hasDeadline bool
		app := mustNew(t)
		app.Add("first", &plainComp{name: "first", log: log})
		app.Add("svc", lifecycle.StartStop(
			func(context.Context) error { return nil },
			func(ctx context.Context) error {
				_, hasDeadline = ctx.Deadline()
				log.add("svc", "stop")
				return nil
			},
		))

		if err := stopAfterLaunch(t, app); err != nil {
			t.Fatalf("Run = %v, want nil", err)
		}
		if !hasDeadline {
			t.Fatal("stop received no deadline")
		}
		if log.index("svc", "stop") > log.index("first", "canceled") {
			t.Fatal("svc was stopped after first")
		}
	})
}

func TestStartStop_StopWaitsForStart(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		log := newEventLog()
		app := mustNew(t)
		app.Add("svc", lifecycle.StartStop(
			func(context.Context) error {
				time.Sleep(2 * time.Second)
				log.add("svc", "started")
				return nil
			},
			func(context.Context) error {
				log.add("svc", "stop")
				return nil
			},
		))

		ctx, cancel := context.WithCancel(t.Context())
		cancel() // the stop is triggered while start is still running
		if err := app.Run(ctx); err != nil {
			t.Fatalf("Run = %v, want nil", err)
		}
		if !log.has("svc", "stop") || log.index("svc", "stop") < log.index("svc", "started") {
			t.Fatalf("events = %+v, want stop after start has returned", log.all())
		}
	})
}

func TestStartStop_StopSkippedAfterStartFailure(t *testing.T) {
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

		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		err := app.Run(ctx)
		if err == nil || !strings.Contains(err.Error(), "start failed") {
			t.Fatalf("Run = %v, want the start error", err)
		}
		if stopped {
			t.Fatal("stop was called after start failed")
		}
	})
}

func TestStartStop_Nil(t *testing.T) {
	noop := func(context.Context) error { return nil }
	for name, c := range map[string]lifecycle.Component{
		"nil start": lifecycle.StartStop(nil, noop),
		"nil stop":  lifecycle.StartStop(noop, nil),
	} {
		t.Run(name, func(t *testing.T) {
			app := mustNew(t)
			app.Add("svc", c)
			if err := app.Run(t.Context()); !errors.Is(err, lifecycle.ErrInvalidConfig) {
				t.Fatalf("Run = %v, want ErrInvalidConfig", err)
			}
		})
	}
}

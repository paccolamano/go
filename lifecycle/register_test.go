package lifecycle_test

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"testing/synctest"

	"github.com/paccolamano/go/lifecycle"
)

func TestRegister_Validation(t *testing.T) {
	tests := []struct {
		name     string
		add      func(app *lifecycle.App, log *eventLog)
		wantMsg  string
		wantName string
	}{
		{
			name: "empty name",
			add: func(app *lifecycle.App, log *eventLog) {
				app.Add("a", &plainComp{name: "a", log: log})
				app.Add("", &plainComp{name: "x", log: log})
			},
			wantMsg: "component at position 2: empty name",
		},
		{
			name: "duplicate name",
			add: func(app *lifecycle.App, log *eventLog) {
				app.Add("a", &plainComp{name: "a", log: log})
				app.Add("a", &plainComp{name: "a2", log: log})
			},
			wantMsg:  "name already used",
			wantName: "a",
		},
		{
			name: "nil component",
			add: func(app *lifecycle.App, log *eventLog) {
				app.Add("a", &plainComp{name: "a", log: log})
				app.Add("b", nil)
			},
			wantMsg:  "nil component",
			wantName: "b",
		},
		{
			name: "nil function",
			add: func(app *lifecycle.App, log *eventLog) {
				app.Add("a", &plainComp{name: "a", log: log})
				app.Add("f", lifecycle.Func(nil))
			},
			wantMsg:  "nil function",
			wantName: "f",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			log := newEventLog()
			app := mustNew(t)
			tt.add(app, log)

			err := app.Run(t.Context())
			if !errors.Is(err, lifecycle.ErrInvalidConfig) {
				t.Fatalf("Run = %v, want an error wrapping ErrInvalidConfig", err)
			}
			if !strings.Contains(err.Error(), tt.wantMsg) {
				t.Errorf("Run = %q, want it to contain %q", err, tt.wantMsg)
			}
			if tt.wantName != "" {
				var ce *lifecycle.ComponentError
				if !errors.As(err, &ce) || ce.Name != tt.wantName {
					t.Errorf("ComponentError = %v, want name %q", ce, tt.wantName)
				}
			}
			if got := log.order("run"); len(got) != 0 {
				t.Errorf("launched %v, want nothing", got)
			}
			if err := app.Ready(t.Context()); !errors.Is(err, lifecycle.ErrNotRunning) || !strings.Contains(err.Error(), "stopped") {
				t.Errorf("Ready = %v, want not running: stopped", err)
			}
		})
	}
}

func TestRegister_AfterStart(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		log := newEventLog()
		rec := newLogRecorder()
		app := mustNew(t, lifecycle.WithLogger(slog.New(rec)))
		app.Add("a", &plainComp{name: "a", log: log})

		ctx, cancel := context.WithCancel(t.Context())
		done := runAsync(ctx, app)
		synctest.Wait()

		app.Add("late", &plainComp{name: "late", log: log})
		recs := rec.find("lifecycle: registration refused")
		if len(recs) != 1 {
			t.Fatalf("refusal logged %d times, want 1", len(recs))
		}
		if recs[0].level != slog.LevelError || recs[0].attrs["component"].String() != "late" {
			t.Errorf("refusal record = %+v, want level ERROR and component=late", recs[0])
		}

		cancel()
		err := <-done
		if !errors.Is(err, lifecycle.ErrInvalidConfig) || !strings.Contains(err.Error(), "registered after start") {
			t.Fatalf("Run = %v, want the refused registration", err)
		}
		var ce *lifecycle.ComponentError
		if !errors.As(err, &ce) || ce.Name != "late" {
			t.Errorf("ComponentError = %v, want name late", ce)
		}
		if log.has("late", "run") {
			t.Error("the late component was launched")
		}
	})
}

func TestRegister_AlreadyStarted(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		log := newEventLog()
		app := mustNew(t)
		app.Add("a", &plainComp{name: "a", log: log})

		ctx, cancel := context.WithCancel(t.Context())
		done := runAsync(ctx, app)
		synctest.Wait()

		if err := app.Run(t.Context()); !errors.Is(err, lifecycle.ErrAlreadyStarted) {
			t.Fatalf("second Run = %v, want ErrAlreadyStarted", err)
		}
		if log.has("a", "canceled") {
			t.Fatal("the second Run affected the first")
		}
		cancel()
		if err := <-done; err != nil {
			t.Fatalf("first Run = %v, want nil", err)
		}
		if err := app.Run(t.Context()); !errors.Is(err, lifecycle.ErrAlreadyStarted) {
			t.Fatalf("Run after return = %v, want ErrAlreadyStarted", err)
		}
	})
}

func TestRegister_Concurrent(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		log := newEventLog()
		app := mustNew(t)
		var wg sync.WaitGroup
		for i := range 50 {
			wg.Go(func() {
				name := fmt.Sprintf("c%02d", i)
				app.Add(name, &plainComp{name: name, log: log})
			})
		}
		wg.Wait()

		ctx, cancel := context.WithCancel(t.Context())
		done := runAsync(ctx, app)
		synctest.Wait()
		if got := len(log.order("run")); got != 50 {
			t.Fatalf("launched %d components, want 50", got)
		}
		cancel()
		if err := <-done; err != nil {
			t.Fatalf("Run = %v, want nil", err)
		}
		if got := len(log.order("returned")); got != 50 {
			t.Fatalf("%d components returned, want 50", got)
		}
	})
}

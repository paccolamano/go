package lifecycle_test

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"sync"
	"testing"
	"testing/synctest"
	"time"
)

// splitMix64 is a small deterministic generator for seeded random orders.
// Semgrep's p/golang rules forbid math/rand, and crypto/rand cannot be seeded.
type splitMix64 uint64

func (s *splitMix64) next() uint64 {
	*s += 0x9e3779b97f4a7c15
	z := uint64(*s)
	z = (z ^ (z >> 30)) * 0xbf58476d1ce4e5b9
	z = (z ^ (z >> 27)) * 0x94d049bb133111eb
	return z ^ (z >> 31)
}

func (s *splitMix64) shuffle(names []string) {
	for i := len(names) - 1; i > 0; i-- {
		j := int(s.next() % uint64(i+1))
		names[i], names[j] = names[j], names[i]
	}
}

func TestOrder_Reverse(t *testing.T) {
	for n := 1; n <= 50; n++ {
		t.Run(fmt.Sprint(n), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				rng := splitMix64(n)
				names := make([]string, n)
				for i := range names {
					names[i] = fmt.Sprintf("c%02d", i)
				}
				rng.shuffle(names)

				log := newEventLog()
				app := mustNew(t)
				for _, name := range names {
					app.Add(name, &plainComp{name: name, log: log, workAfterCancel: time.Millisecond})
				}
				ctx, cancel := context.WithCancel(t.Context())
				done := runAsync(ctx, app)
				synctest.Wait()
				cancel()
				if err := <-done; err != nil {
					t.Fatalf("Run = %v, want nil", err)
				}

				want := slices.Clone(names)
				slices.Reverse(want)
				if got := log.order("canceled"); !slices.Equal(got, want) {
					t.Fatalf("stop order = %v, want %v", got, want)
				}
				for i := 1; i < len(want); i++ {
					if log.index(want[i], "canceled") < log.index(want[i-1], "returned") {
						t.Fatalf("%s was stopped before %s returned", want[i], want[i-1])
					}
				}
			})
		})
	}
}

func TestOrder_NoComponents(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		app := mustNew(t)
		ctx, cancel := context.WithCancel(t.Context())
		done := runAsync(ctx, app)
		synctest.Wait()
		select {
		case err := <-done:
			t.Fatalf("Run returned %v before its context was cancelled", err)
		default:
		}
		cancel()
		if err := <-done; err != nil {
			t.Fatalf("Run = %v, want nil", err)
		}
	})
}

func TestContext_NotPropagated(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		log := newEventLog()
		var firstCtx context.Context
		app := mustNew(t)
		app.Add("first", &plainComp{name: "first", log: log, onRun: func(ctx context.Context) { firstCtx = ctx }})
		app.Add("last", &plainComp{name: "last", log: log, workAfterCancel: time.Second})

		ctx, cancel := context.WithCancel(t.Context())
		done := runAsync(ctx, app)
		synctest.Wait()
		cancel()
		synctest.Wait() // "last" is now working after its cancellation

		if !log.has("last", "canceled") {
			t.Fatal("last was not stopped first")
		}
		if err := firstCtx.Err(); err != nil {
			t.Fatalf("first's context = %v while last stops, want still valid", err)
		}
		if err := <-done; err != nil {
			t.Fatalf("Run = %v, want nil", err)
		}
		if firstCtx.Err() == nil {
			t.Fatal("first's context was never cancelled")
		}
	})
}

type ctxKey struct{}

func TestContext_Values(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		log := newEventLog()
		var mu sync.Mutex
		var got []any
		app := mustNew(t)
		for _, name := range []string{"a", "b"} {
			app.Add(name, &plainComp{name: name, log: log, onRun: func(ctx context.Context) {
				mu.Lock()
				defer mu.Unlock()
				got = append(got, ctx.Value(ctxKey{}))
			}})
		}
		ctx, cancel := context.WithCancel(context.WithValue(t.Context(), ctxKey{}, "v"))
		done := runAsync(ctx, app)
		synctest.Wait()
		cancel()
		if err := <-done; err != nil {
			t.Fatalf("Run = %v, want nil", err)
		}
		if len(got) != 2 || got[0] != "v" || got[1] != "v" {
			t.Fatalf("values seen = %v, want [v v]", got)
		}
	})
}

func TestContext_AlreadyDone(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		log := newEventLog()
		app := mustNew(t)
		app.Add("a", &plainComp{name: "a", log: log})
		app.Add("b", &plainComp{name: "b", log: log})

		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		if err := app.Run(ctx); err != nil {
			t.Fatalf("Run = %v, want nil", err)
		}
		if got := log.order("run"); len(got) != 2 {
			t.Fatalf("launched %v, want a and b", got)
		}
		if got := log.order("canceled"); !slices.Equal(got, []string{"b", "a"}) {
			t.Fatalf("stop order = %v, want [b a]", got)
		}
	})
}

func TestOrder_Log(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		log := newEventLog()
		rec := newLogRecorder()
		app := mustNew(t, lifecycleLogger(rec))
		app.Add("a", &plainComp{name: "a", log: log})
		app.Add("b", &plainComp{name: "b", log: log})

		ctx, cancel := context.WithCancel(t.Context())
		done := runAsync(ctx, app)
		synctest.Wait()
		cancel()
		if err := <-done; err != nil {
			t.Fatalf("Run = %v, want nil", err)
		}

		triggers := rec.find("lifecycle: stop triggered")
		if len(triggers) != 1 || triggers[0].attrs["trigger"].String() != "context" {
			t.Fatalf("stop triggered records = %+v, want one with trigger=context", triggers)
		}
		stopped := rec.find("lifecycle: component stopped")
		if len(stopped) != 2 {
			t.Fatalf("component stopped records = %d, want 2", len(stopped))
		}
		for i, want := range []string{"b", "a"} {
			if got := stopped[i].attrs["component"].String(); got != want {
				t.Errorf("record %d component = %q, want %q", i, got, want)
			}
			if stopped[i].attrs["duration"].Kind() != slog.KindDuration {
				t.Errorf("record %d has no duration", i)
			}
		}
	})
}

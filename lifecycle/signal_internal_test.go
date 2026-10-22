package lifecycle

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"os"
	"strings"
	"sync"
	"syscall"
	"testing"
	"testing/synctest"
	"time"
)

// fakeSignals replaces signal.Notify and signal.Stop for an App. Registering a
// channel created inside a synctest bubble with the real signal.Notify is a fatal
// error of the runtime (research R-09), so the tests deliver fake signals.
type fakeSignals struct {
	mu       sync.Mutex
	ch       chan<- os.Signal
	sigs     []os.Signal
	notified int
	stopped  int
	stopCh   chan<- os.Signal
}

func (f *fakeSignals) install(a *App) {
	a.notify = func(c chan<- os.Signal, sig ...os.Signal) {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.ch, f.sigs = c, sig
		f.notified++
	}
	a.stopNotify = func(c chan<- os.Signal) {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.stopCh = c
		f.stopped++
	}
}

func (f *fakeSignals) send(tb testing.TB, s os.Signal) {
	tb.Helper()
	f.mu.Lock()
	c := f.ch
	f.mu.Unlock()
	if c == nil {
		tb.Fatal("no channel registered")
	}
	c <- s
}

// syncBuffer is a bytes.Buffer safe for concurrent writes by a log handler.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// waiter is a component that runs until its context is cancelled.
func waiter() Func {
	return func(ctx context.Context) error {
		<-ctx.Done()
		return nil
	}
}

func newTestApp(tb testing.TB, f *fakeSignals, opts ...Option) *App {
	tb.Helper()
	a, err := New(opts...)
	if err != nil {
		tb.Fatalf("New: %v", err)
	}
	f.install(a)
	return a
}

func runInBackground(ctx context.Context, a *App) <-chan error {
	done := make(chan error, 1)
	go func() { done <- a.Run(ctx) }()
	return done
}

func TestSignal_Trigger(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var f fakeSignals
		var buf syncBuffer
		a := newTestApp(t, &f, WithSignals(syscall.SIGTERM), WithLogger(slog.New(slog.NewTextHandler(&buf, nil))))
		a.Add("w", waiter())

		done := runInBackground(t.Context(), a)
		synctest.Wait()
		f.send(t, syscall.SIGTERM)
		if err := <-done; err != nil {
			t.Fatalf("Run = %v, want nil", err)
		}
		if out := buf.String(); !strings.Contains(out, "trigger=signal") || !strings.Contains(out, "signal=terminated") {
			t.Fatalf("log = %q, want trigger=signal signal=terminated", out)
		}
	})
}

func TestSignal_NotConfigured(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var f fakeSignals
		a := newTestApp(t, &f)
		a.Add("w", waiter())
		ctx, cancel := context.WithCancel(t.Context())
		done := runInBackground(ctx, a)
		synctest.Wait()
		cancel()
		if err := <-done; err != nil {
			t.Fatalf("Run = %v, want nil", err)
		}
		if f.notified != 0 || f.stopped != 0 {
			t.Fatalf("notify called %d times and stop %d times, want 0", f.notified, f.stopped)
		}
	})
}

func TestSignal_Dedupe(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var f fakeSignals
		a := newTestApp(t, &f, WithSignals(os.Interrupt, os.Interrupt))
		a.Add("w", waiter())
		ctx, cancel := context.WithCancel(t.Context())
		done := runInBackground(ctx, a)
		synctest.Wait()
		cancel()
		if err := <-done; err != nil {
			t.Fatalf("Run = %v, want nil", err)
		}
		if len(f.sigs) != 1 || f.sigs[0] != os.Interrupt {
			t.Fatalf("notify signals = %v, want [interrupt]", f.sigs)
		}
	})
}

// stuckUntil is a component that ignores its context until release is closed.
func stuckUntil(release <-chan struct{}) Func {
	return func(context.Context) error {
		<-release
		return nil
	}
}

func TestForced_SecondSignal(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var f fakeSignals
		var buf syncBuffer
		release := make(chan struct{})
		defer close(release)
		var firstCanceled bool
		a := newTestApp(t, &f, WithSignals(syscall.SIGTERM), WithLogger(slog.New(slog.NewTextHandler(&buf, nil))))
		a.Add("first", Func(func(ctx context.Context) error {
			<-ctx.Done()
			firstCanceled = true
			return nil
		}))
		a.Add("slow", stuckUntil(release))

		done := runInBackground(t.Context(), a)
		synctest.Wait()
		f.send(t, syscall.SIGTERM)
		synctest.Wait() // the App waits for slow
		forcedAt := time.Now()
		f.send(t, syscall.SIGTERM)
		err := <-done
		if !errors.Is(err, ErrForced) {
			t.Fatalf("Run = %v, want ErrForced", err)
		}
		if elapsed := time.Since(forcedAt); elapsed != 0 {
			t.Fatalf("Run returned %v after the second signal, want at once", elapsed)
		}
		synctest.Wait()
		if !firstCanceled {
			t.Fatal("first did not receive the cancellation of its context")
		}
		if out := buf.String(); !strings.Contains(out, `level=WARN msg="lifecycle: stop forced"`) {
			t.Fatalf("log = %q, want the forced stop at level WARN", out)
		}
	})
}

func TestForced_DuringDrain(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var f fakeSignals
		a := newTestApp(t, &f, WithSignals(syscall.SIGTERM), WithDrainDelay(10*time.Second))
		a.Add("w", waiter())

		done := runInBackground(t.Context(), a)
		synctest.Wait()
		start := time.Now()
		f.send(t, syscall.SIGTERM)
		time.Sleep(2 * time.Second)
		f.send(t, syscall.SIGTERM)
		if err := <-done; !errors.Is(err, ErrForced) {
			t.Fatalf("Run = %v, want ErrForced", err)
		}
		if elapsed := time.Since(start); elapsed != 2*time.Second {
			t.Fatalf("Run returned after %v, want 2s: the drain must end at the second signal", elapsed)
		}
	})
}

func TestForced_AfterOtherTrigger(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var f fakeSignals
		release := make(chan struct{})
		defer close(release)
		a := newTestApp(t, &f, WithSignals(syscall.SIGTERM))
		a.Add("slow", stuckUntil(release))

		ctx, cancel := context.WithCancel(t.Context())
		done := runInBackground(ctx, a)
		synctest.Wait()
		cancel()
		synctest.Wait()
		f.send(t, syscall.SIGTERM)
		synctest.Wait()
		select {
		case err := <-done:
			t.Fatalf("Run returned %v after one signal, want it still stopping", err)
		default:
		}
		f.send(t, syscall.SIGTERM)
		if err := <-done; !errors.Is(err, ErrForced) {
			t.Fatalf("Run = %v, want ErrForced", err)
		}
	})
}

func TestSignal_Restore(t *testing.T) {
	t.Run("normal stop", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			var f fakeSignals
			a := newTestApp(t, &f, WithSignals(os.Interrupt))
			a.Add("w", waiter())
			ctx, cancel := context.WithCancel(t.Context())
			done := runInBackground(ctx, a)
			synctest.Wait()
			cancel()
			if err := <-done; err != nil {
				t.Fatalf("Run = %v, want nil", err)
			}
			if f.notified != 1 || f.stopped != 1 || f.stopCh != f.ch {
				t.Fatalf("notify %d, stop %d, same channel %v; want 1, 1, true", f.notified, f.stopped, f.stopCh == f.ch)
			}
		})
	})
	t.Run("invalid registration", func(t *testing.T) {
		var f fakeSignals
		a := newTestApp(t, &f, WithSignals(os.Interrupt))
		a.Add("", waiter())
		if err := a.Run(t.Context()); err == nil {
			t.Fatal("Run = nil, want the invalid registration")
		}
		if f.notified != 0 || f.stopped != 0 {
			t.Fatalf("notify %d, stop %d, want 0 and 0", f.notified, f.stopped)
		}
	})
}

//go:build unix

package lifecycle_test

import (
	"context"
	"errors"
	"os"
	"syscall"
	"testing"

	"github.com/paccolamano/go/lifecycle"
)

// These tests deliver real signals to the test process. They run outside
// synctest bubbles, where signal.Notify cannot be used (research R-09), and
// synchronize only on channels.

func TestRealSignal_Trigger(t *testing.T) {
	running := make(chan struct{})
	app := mustNew(t, lifecycle.WithSignals(syscall.SIGUSR1))
	app.Add("w", lifecycle.Func(func(ctx context.Context) error {
		close(running)
		<-ctx.Done()
		return nil
	}))

	done := runAsync(t.Context(), app)
	<-running // the signals are registered before any component is launched
	if err := syscall.Kill(os.Getpid(), syscall.SIGUSR1); err != nil {
		t.Fatalf("kill: %v", err)
	}
	if err := <-done; err != nil {
		t.Fatalf("Run = %v, want nil", err)
	}
}

// blockingStop is a component whose Stop blocks until it is released.
type blockingStop struct {
	running  chan struct{}
	stopping chan struct{}
	release  chan struct{}
}

func (b *blockingStop) Run(context.Context) error {
	close(b.running)
	<-b.release
	return nil
}

func (b *blockingStop) Stop(context.Context) error {
	close(b.stopping)
	<-b.release
	return nil
}

func TestRealSignal_Forced(t *testing.T) {
	b := &blockingStop{running: make(chan struct{}), stopping: make(chan struct{}), release: make(chan struct{})}
	defer close(b.release)
	app := mustNew(t, lifecycle.WithSignals(syscall.SIGUSR1))
	app.Add("b", b)

	done := runAsync(t.Context(), app)
	<-b.running
	if err := syscall.Kill(os.Getpid(), syscall.SIGUSR1); err != nil {
		t.Fatalf("kill: %v", err)
	}
	<-b.stopping
	if err := syscall.Kill(os.Getpid(), syscall.SIGUSR1); err != nil {
		t.Fatalf("kill: %v", err)
	}
	if err := <-done; !errors.Is(err, lifecycle.ErrForced) {
		t.Fatalf("Run = %v, want ErrForced", err)
	}
}

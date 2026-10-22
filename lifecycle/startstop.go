package lifecycle

import (
	"context"
	"fmt"
	"sync"
)

// StartStop returns a component for services with separate start and stop
// operations, such as a job queue client or a scheduler. start is called when
// the component is launched, with its context, and must return once the service
// has started; an error is a failure of the component, which is then not
// stopped. stop is called at the component's turn, with the deadline of the stop
// sequence, once start has returned; it is not called when start failed.
func StartStop(start, stop func(context.Context) error) Component {
	return &startStop{start: start, stop: stop, started: make(chan struct{}), released: make(chan struct{})}
}

type startStop struct {
	start    func(context.Context) error
	stop     func(context.Context) error
	started  chan struct{} // closed when start has returned
	startErr error         // the result of start, valid once started is closed
	once     sync.Once
	released chan struct{}
}

func (s *startStop) validate() error {
	switch {
	case s.start == nil:
		return fmt.Errorf("%w: StartStop: nil start", ErrInvalidConfig)
	case s.stop == nil:
		return fmt.Errorf("%w: StartStop: nil stop", ErrInvalidConfig)
	}
	return nil
}

// Run calls start, then waits until the stop, or until its context is cancelled.
func (s *startStop) Run(ctx context.Context) error {
	s.startErr = s.start(ctx)
	close(s.started)
	if s.startErr != nil {
		return fmt.Errorf("lifecycle: start: %w", s.startErr)
	}
	select {
	case <-s.released:
	case <-ctx.Done():
	}
	return nil
}

// Stop waits until start has returned, then calls stop once and releases Run. It
// does not call stop when start failed.
func (s *startStop) Stop(ctx context.Context) error {
	select {
	case <-s.started:
	case <-ctx.Done():
		return fmt.Errorf("lifecycle: start not finished: %w", ctx.Err())
	}
	if s.startErr != nil {
		return nil
	}
	var err error
	s.once.Do(func() {
		defer close(s.released)
		err = s.stop(ctx)
	})
	return err
}

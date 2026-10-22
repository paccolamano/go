package lifecycle

import (
	"context"
	"fmt"
	"sync"
)

// Closer returns a component whose stop calls f once, such as the Close or
// Shutdown method of a client: a function with no result, with an error, or with
// a context and an error, which receives the deadline of the stop sequence. Its
// Run only waits for the stop, so register the client where it must be closed:
// before the components that use it.
//
// A named function type, such as context.CancelFunc, must be converted:
// Closer(func() { cancel() }).
func Closer[F func() | func() error | func(context.Context) error](f F) Component {
	c := &closer{released: make(chan struct{})}
	switch f := any(f).(type) {
	case func():
		if f != nil {
			c.close = func(context.Context) error { f(); return nil }
		}
	case func() error:
		if f != nil {
			c.close = func(context.Context) error { return f() }
		}
	case func(context.Context) error:
		c.close = f
	}
	return c
}

type closer struct {
	close    func(context.Context) error
	once     sync.Once
	released chan struct{}
}

func (c *closer) validate() error {
	if c.close == nil {
		return fmt.Errorf("%w: Closer: nil function", ErrInvalidConfig)
	}
	return nil
}

// Run waits until the stop, or until its context is cancelled.
func (c *closer) Run(ctx context.Context) error {
	select {
	case <-c.released:
	case <-ctx.Done():
	}
	return nil
}

// Stop calls the close function once, then releases Run.
func (c *closer) Stop(ctx context.Context) error {
	var err error
	c.once.Do(func() {
		defer close(c.released)
		err = c.close(ctx)
	})
	return err
}

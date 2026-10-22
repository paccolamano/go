package lifecycle

import (
	"context"
	"fmt"
)

// Component is a part of the process that runs until it is stopped, such as a
// server, a worker or the client of a database. Run blocks while the component
// works and returns when it has stopped. Its context carries the values of the
// context passed to App.Run, and is cancelled only when the App's stop sequence
// says so: at the start of the component's turn when it does not implement
// Stopper, and only when the deadline expires when it does.
type Component interface {
	Run(ctx context.Context) error
}

// Stopper is implemented by components that can stop accepting new work on
// request. Stop receives a context whose deadline is the end of the stop
// sequence. It may return at once, as a consumer that cancels its subscription,
// or block until the component has stopped, as an HTTP server's graceful
// shutdown. The App waits for both Stop and Run to return. Stop is called only
// after Run has been called, but possibly while Run is still starting.
type Stopper interface {
	Stop(ctx context.Context) error
}

// Func adapts a function to a Component, like http.HandlerFunc. The function
// must return when its context is cancelled.
type Func func(ctx context.Context) error

// Run calls f(ctx).
func (f Func) Run(ctx context.Context) error { return f(ctx) }

func (f Func) validate() error {
	if f == nil {
		return fmt.Errorf("%w: nil function", ErrInvalidConfig)
	}
	return nil
}

// validator is implemented by Func and the adapters, so that Add reports an
// invalid one before anything is launched.
type validator interface {
	validate() error
}

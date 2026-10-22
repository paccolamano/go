package lifecycle

import (
	"errors"
	"fmt"
	"strings"
)

// Errors reported by an App (research R-04).
var (
	// ErrAlreadyStarted is returned by Run when the App has already been run.
	ErrAlreadyStarted = errors.New("lifecycle: already started")
	// ErrExitedEarly reports a component whose Run returned no error before its
	// stop was requested.
	ErrExitedEarly = errors.New("lifecycle: exited before being stopped")
	// ErrStopTimeout reports a component whose Run had not returned when the
	// deadline of the stop sequence expired.
	ErrStopTimeout = errors.New("lifecycle: not stopped in time")
	// ErrForced reports a stop forced by a second signal.
	ErrForced = errors.New("lifecycle: stop forced by a second signal")
	// ErrNotRunning is wrapped by the error Ready returns outside the running
	// state. Its message names the state: starting, stopping or stopped.
	ErrNotRunning = errors.New("lifecycle: not running")
)

// ComponentError reports an outcome of the named component: a failure, an early
// exit, a stop error, a timeout or a refused registration.
type ComponentError struct {
	Name string
	Err  error
}

// Error returns `lifecycle: component "<name>": <err>`, without repeating a
// leading "lifecycle: " of the wrapped error.
func (e *ComponentError) Error() string {
	msg := "<nil>"
	if e.Err != nil {
		msg = strings.TrimPrefix(e.Err.Error(), "lifecycle: ")
	}
	return fmt.Sprintf("lifecycle: component %q: %s", e.Name, msg)
}

// Unwrap returns the wrapped error.
func (e *ComponentError) Unwrap() error { return e.Err }

// PanicError reports a panic recovered from a component's Run or Stop. Stack is
// the stack trace of the goroutine that panicked.
type PanicError struct {
	Value any
	Stack []byte
}

// Error returns "panic: <value>".
func (e *PanicError) Error() string { return fmt.Sprintf("panic: %v", e.Value) }

// outcomes collects what Run reports, in the order of the joined error
// (contracts/lifecycle-semantics.md, "Result").
type outcomes struct {
	trigger error
	later   []error
	stops   []error
	forced  bool
	refused []error
}

func (o *outcomes) err() error {
	var errs []error
	if o.trigger != nil {
		errs = append(errs, o.trigger)
	}
	errs = append(errs, o.later...)
	errs = append(errs, o.stops...)
	if o.forced {
		errs = append(errs, ErrForced)
	}
	errs = append(errs, o.refused...)
	return errors.Join(errs...)
}

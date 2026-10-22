package lifecycle

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"sync"
	"sync/atomic"
)

// state is the state of an App, as reported by Ready.
type state int32

const (
	stateStarting state = iota
	stateRunning
	stateStopping
	stateStopped
)

func (s state) String() string {
	switch s {
	case stateRunning:
		return "running"
	case stateStopping:
		return "stopping"
	case stateStopped:
		return "stopped"
	default:
		return "starting"
	}
}

// registration is a component added to an App, with its name.
type registration struct {
	name string
	c    Component
}

// App runs components: launched in registration order, stopped one at a time in
// reverse order. An App runs once. It is safe for concurrent use.
type App struct {
	cfg   config
	state atomic.Int32

	mu      sync.Mutex
	started bool
	added   int
	names   map[string]struct{}
	regs    []registration
	invalid []error
	refused []error

	// notify and stopNotify are signal.Notify and signal.Stop. Internal tests
	// replace them, because signal.Notify cannot be used inside a synctest bubble.
	notify     func(c chan<- os.Signal, sig ...os.Signal)
	stopNotify func(c chan<- os.Signal)
}

// New returns an App configured by opts. An invalid option is reported as an
// error wrapping ErrInvalidConfig.
func New(opts ...Option) (*App, error) {
	cfg, err := newConfig(opts)
	if err != nil {
		return nil, err
	}
	return &App{
		cfg:        cfg,
		names:      map[string]struct{}{},
		notify:     signal.Notify,
		stopNotify: signal.Stop,
	}, nil
}

// Add registers c under name. Components are launched in registration order and
// stopped in reverse order, so register them in the order you open or start
// them. An empty name, a name already used, a nil component or an invalid
// adapter is reported by Run, which then launches nothing. A registration made
// after Run has started is refused: the component is never launched, and Run
// reports the refusal when it returns.
func (a *App) Add(name string, c Component) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.started {
		err := &ComponentError{Name: name, Err: fmt.Errorf("%w: registered after start", ErrInvalidConfig)}
		a.refused = append(a.refused, err)
		a.cfg.logger.LogAttrs(context.Background(), slog.LevelError, "lifecycle: registration refused",
			slog.String("component", name), slog.Any("error", err))
		return
	}
	a.added++
	if err := a.check(name, c); err != nil {
		a.invalid = append(a.invalid, err)
		return
	}
	a.names[name] = struct{}{}
	a.regs = append(a.regs, registration{name: name, c: c})
}

// check validates one registration. The caller holds a.mu.
func (a *App) check(name string, c Component) error {
	if name == "" {
		return fmt.Errorf("%w: component at position %d: empty name", ErrInvalidConfig, a.added)
	}
	if _, used := a.names[name]; used {
		return &ComponentError{Name: name, Err: fmt.Errorf("%w: name already used", ErrInvalidConfig)}
	}
	if c == nil {
		return &ComponentError{Name: name, Err: fmt.Errorf("%w: nil component", ErrInvalidConfig)}
	}
	if v, ok := c.(validator); ok {
		if err := v.validate(); err != nil {
			return &ComponentError{Name: name, Err: err}
		}
	}
	return nil
}

// start marks the App as started and returns its registrations, or false when it
// had already been started.
func (a *App) start() (regs []registration, invalid error, ok bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.started {
		return nil, nil, false
	}
	a.started = true
	return a.regs, errors.Join(a.invalid...), true
}

// refusedAfterStart returns the registrations refused since Run started.
func (a *App) refusedAfterStart() []error {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]error(nil), a.refused...)
}

func (a *App) setState(s state) { a.state.Store(int32(s)) }

// Ready returns nil while the App is running, from the moment every component
// has been launched until a stop is triggered. Otherwise it returns an error
// wrapping ErrNotRunning that names the state: starting, stopping or stopped.
// It has the shape of a health check, so it can be given to a readiness probe;
// its context is not used.
func (a *App) Ready(context.Context) error {
	s := state(a.state.Load())
	if s == stateRunning {
		return nil
	}
	return fmt.Errorf("%w: %s", ErrNotRunning, s)
}

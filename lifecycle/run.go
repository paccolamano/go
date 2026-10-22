package lifecycle

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"runtime/debug"
	"time"
)

// member is one launched component.
type member struct {
	name   string
	c      Component
	cancel context.CancelFunc
	done   chan struct{} // closed when Run has returned
	err    error         // the result of Run, valid once done is closed

	// Fields below are owned by the runner's goroutine.
	stopRequested bool // its turn has begun
	handled       bool // it returned before its turn, and that was reported
}

// runner holds the state of one call to Run. Its fields are used only by the
// goroutine that called Run.
type runner struct {
	a       *App
	ctx     context.Context // the context passed to Run, for logs
	members []*member
	exits   chan *member // members whose Run has returned
	sigs    chan os.Signal
	nsig    int // signals received
	out     outcomes

	// stopCtx carries the deadline of the stop sequence, the end of the drain plus
	// the stop timeout. forceStop cancels it with the cause ErrForced.
	stopCtx   context.Context
	forceStop context.CancelCauseFunc
}

// Run launches every component and blocks until they have been stopped, or until
// the deadline of the stop sequence has expired. The stop is triggered by the end
// of ctx, by a configured signal, or by a component whose Run returns before it
// was asked to stop. Run returns nil when the stop was triggered by ctx or by a
// signal and every component stopped cleanly. An App runs once: later calls
// return ErrAlreadyStarted.
func (a *App) Run(ctx context.Context) error {
	regs, invalid, ok := a.start()
	if !ok {
		return ErrAlreadyStarted
	}
	if invalid != nil {
		a.setState(stateStopped)
		return invalid
	}
	r := &runner{a: a, ctx: ctx, exits: make(chan *member, len(regs))}
	return r.run(ctx, regs)
}

func (r *runner) run(ctx context.Context, regs []registration) error {
	r.sigs = r.listen()
	defer r.unlisten(r.sigs)

	r.launch(ctx, regs)
	r.a.setState(stateRunning)
	r.waitTrigger()
	r.a.setState(stateStopping)
	release := r.startStopContext(ctx)
	defer release()
	r.drain()
	r.stopAll()
	r.a.setState(stateStopped)

	r.out.refused = r.a.refusedAfterStart()
	return r.out.err()
}

// launch starts every component in its own goroutine, in registration order.
// Each one gets the values of ctx, never its cancellation (FR-013).
func (r *runner) launch(ctx context.Context, regs []registration) {
	base := context.WithoutCancel(ctx)
	for _, reg := range regs {
		ctx, cancel := context.WithCancel(base)
		m := &member{name: reg.name, c: reg.c, cancel: cancel, done: make(chan struct{})}
		r.members = append(r.members, m)
		running := make(chan struct{})
		go func() {
			defer func() {
				if v := recover(); v != nil {
					m.err = &PanicError{Value: v, Stack: debug.Stack()}
				}
				close(m.done)
				r.exits <- m // buffered for every member: never blocks
			}()
			close(running)
			m.err = m.c.Run(ctx)
		}()
		// Run is called before the next component is launched and before any
		// trigger is acted upon, so Stop never comes before Run.
		<-running
	}
}

// waitTrigger blocks until the first trigger and logs it.
func (r *runner) waitTrigger() {
	select {
	case <-r.ctx.Done():
		r.log(slog.LevelInfo, "lifecycle: stop triggered", slog.String("trigger", "context"))
	case s := <-r.sigs:
		r.nsig++
		r.log(slog.LevelInfo, "lifecycle: stop triggered", slog.String("trigger", "signal"), slog.String("signal", s.String()))
	case m := <-r.exits:
		r.log(slog.LevelInfo, "lifecycle: stop triggered", slog.String("trigger", "component"), slog.String("component", m.name))
		r.early(m, true)
	}
}

// startStopContext creates the stop context at the trigger. The returned
// function releases it.
func (r *runner) startStopContext(ctx context.Context) func() {
	deadline := time.Now().Add(r.a.cfg.drainDelay + r.a.cfg.stopTimeout)
	withDeadline, cancel := context.WithDeadline(context.WithoutCancel(ctx), deadline)
	r.stopCtx, r.forceStop = context.WithCancelCause(withDeadline)
	return func() {
		r.forceStop(nil)
		cancel()
	}
}

// onSignal counts a signal received after the trigger. The second signal
// received forces the stop, whatever the trigger was.
func (r *runner) onSignal() {
	r.nsig++
	if r.nsig < 2 || r.out.forced {
		return
	}
	r.out.forced = true
	r.log(slog.LevelWarn, "lifecycle: stop forced")
	r.forceStop(ErrForced)
}

// onExit handles a member whose Run has returned while the runner was waiting
// for something else: unless its turn has begun, it returned early.
func (r *runner) onExit(m *member) {
	if m.stopRequested || m.handled {
		return
	}
	r.early(m, false)
}

// early reports a member whose Run returned before its turn: a failure, or
// ErrExitedEarly when it returned no error. The first one is the trigger.
func (r *runner) early(m *member, trigger bool) {
	m.handled = true
	err := m.err
	if err == nil {
		err = ErrExitedEarly
		r.log(slog.LevelError, "lifecycle: component exited early", slog.String("component", m.name))
	} else {
		r.log(slog.LevelError, "lifecycle: component failed", slog.String("component", m.name), slog.Any("error", err))
	}
	ce := &ComponentError{Name: m.name, Err: err}
	if trigger {
		r.out.trigger = ce
		return
	}
	r.out.later = append(r.out.later, ce)
}

// drain waits for the drain delay, while Ready already fails and every component
// keeps working, so that load balancers stop sending traffic before the first
// stop. Components that return meanwhile are reported.
func (r *runner) drain() {
	d := r.a.cfg.drainDelay
	if d <= 0 {
		return
	}
	r.log(slog.LevelInfo, "lifecycle: drain started", slog.Duration("delay", d))
	timer := time.NewTimer(d)
	defer timer.Stop()
	for {
		select {
		case <-timer.C:
			return
		case m := <-r.exits:
			r.onExit(m)
		case <-r.sigs:
			r.onSignal()
		case <-r.stopCtx.Done(): // forced
			return
		}
	}
}

// stopAll stops the components one at a time, last registered first. Once the
// stop context has ended, the remaining components are stopped without waiting.
func (r *runner) stopAll() {
	for i := len(r.members) - 1; i >= 0; i-- {
		m := r.members[i]
		select {
		case <-m.done:
			// It returned before its turn, maybe without its exit being seen yet.
			if !m.handled {
				r.early(m, false)
			}
			continue
		default:
		}
		if r.stopCtx.Err() != nil {
			r.abandon(m)
			continue
		}
		r.stopOne(m)
	}
}

// abandon stops m after the deadline has expired, or after a forced stop: it
// gets Stop, with the ended context, and the cancellation of its context, but
// the App does not wait for it (FR-024). A Run that has not returned at once is
// reported as not stopped in time.
func (r *runner) abandon(m *member) {
	m.stopRequested = true
	if s, ok := m.c.(Stopper); ok {
		go func() {
			defer func() {
				if v := recover(); v != nil {
					r.log(slog.LevelError, "lifecycle: component stop failed",
						slog.String("component", m.name), slog.Any("panic", v))
				}
			}()
			if err := s.Stop(r.stopCtx); err != nil && !r.isStopContextError(err) {
				r.log(slog.LevelError, "lifecycle: component stop failed",
					slog.String("component", m.name), slog.Any("error", err))
			}
		}()
	}
	m.cancel()
	select {
	case <-m.done:
	default:
		r.addStop(m, ErrStopTimeout)
		r.log(slog.LevelError, "lifecycle: component not stopped in time", slog.String("component", m.name))
	}
}

// stopOne applies the stop contract to m (contracts/lifecycle-semantics.md): a
// Stopper gets Stop and keeps a valid context until the deadline; any other
// component gets the cancellation of its context at once. The App waits for Stop
// and Run, until the deadline.
func (r *runner) stopOne(m *member) {
	start := time.Now()
	m.stopRequested = true
	var stopDone chan error
	if s, ok := m.c.(Stopper); ok {
		stopDone = make(chan error, 1)
		go func() {
			defer func() {
				if v := recover(); v != nil {
					stopDone <- &PanicError{Value: v, Stack: debug.Stack()}
				}
			}()
			stopDone <- s.Stop(r.stopCtx)
		}()
	} else {
		m.cancel()
	}

	var stopErr error
	sd, rd := stopDone, m.done
	for sd != nil || rd != nil {
		select {
		case stopErr = <-sd:
			sd = nil
		case <-rd:
			rd = nil
		case e := <-r.exits:
			r.onExit(e)
		case <-r.sigs:
			r.onSignal()
		case <-r.stopCtx.Done():
			m.cancel()
			r.addStop(m, ErrStopTimeout)
			r.log(slog.LevelError, "lifecycle: component not stopped in time",
				slog.String("component", m.name), slog.Duration("duration", time.Since(start)))
			return
		}
	}
	m.cancel()

	if stopErr != nil && !r.isStopContextError(stopErr) {
		r.addStop(m, fmt.Errorf("stop: %w", stopErr))
		r.log(slog.LevelError, "lifecycle: component stop failed",
			slog.String("component", m.name), slog.Any("error", stopErr))
	}
	if m.err != nil && !errors.Is(m.err, context.Canceled) {
		r.addStop(m, m.err)
		r.log(slog.LevelError, "lifecycle: component failed",
			slog.String("component", m.name), slog.Any("error", m.err))
	}
	r.log(slog.LevelInfo, "lifecycle: component stopped",
		slog.String("component", m.name), slog.Duration("duration", time.Since(start)))
}

// isStopContextError reports whether err is the stop context's own error, after
// it has ended: the timeout or the forced stop is reported instead (research R-04).
func (r *runner) isStopContextError(err error) bool {
	ctxErr := r.stopCtx.Err()
	return ctxErr != nil && errors.Is(err, ctxErr)
}

func (r *runner) addStop(m *member, err error) {
	r.out.stops = append(r.out.stops, &ComponentError{Name: m.name, Err: err})
}

func (r *runner) log(level slog.Level, msg string, attrs ...slog.Attr) {
	r.a.cfg.logger.LogAttrs(r.ctx, level, msg, attrs...)
}

package health

import (
	"context"
	"errors"
	"log/slog"
	"runtime/debug"
)

// outcomeKind is the result of one check in one evaluation.
type outcomeKind int

const (
	passed outcomeKind = iota
	failed
	timedOut
	panicked
	stillRunning
)

type outcome struct {
	kind       outcomeKind
	err        error
	panicValue any
	stack      []byte
}

// evaluation is one run of every check, shared by the requests that arrive while
// it is in progress (research R-16).
type evaluation struct {
	ctx       context.Context
	cancel    context.CancelFunc
	waiters   int
	outcomes  []outcome
	remaining int
	done      chan struct{} // closed when every outcome is recorded
	abandoned bool          // every waiter left before done
}

// join returns the evaluation in progress, or starts one, and counts the caller
// as a waiter.
func (h *Handler) join(reqCtx context.Context) *evaluation {
	h.mu.Lock()
	e := h.current
	immediate := false
	if e == nil {
		e = h.start(reqCtx)
		// When every check is still running, nothing was started and the
		// evaluation is already complete.
		if e.remaining == 0 {
			immediate = true
		} else {
			h.current = e
		}
	}
	e.waiters++
	h.mu.Unlock()
	if immediate {
		h.finish(e)
	}
	return e
}

// start launches the checks that are not still running. The caller holds h.mu.
func (h *Handler) start(reqCtx context.Context) *evaluation {
	ctx, cancel := context.WithCancel(context.WithoutCancel(reqCtx))
	e := &evaluation{
		ctx:       ctx,
		cancel:    cancel,
		outcomes:  make([]outcome, len(h.checks)),
		remaining: len(h.checks),
		done:      make(chan struct{}),
	}
	for i, c := range h.checks {
		if h.running[i] {
			e.outcomes[i] = outcome{kind: stillRunning}
			e.remaining--
			continue
		}
		h.running[i] = true
		go h.run(ctx, e, i, c)
	}
	return e
}

// run runs one check with ctx, the evaluation's context, bounded by its timeout
// even when it ignores its context, and records its outcome.
func (h *Handler) run(ctx context.Context, e *evaluation, i int, c check) {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	result := make(chan outcome, 1)
	go func() {
		defer func() {
			if v := recover(); v != nil {
				result <- outcome{kind: panicked, panicValue: v, stack: debug.Stack()}
			}
			h.mu.Lock()
			h.running[i] = false
			h.mu.Unlock()
		}()
		if err := c.fn(ctx); err != nil {
			result <- outcome{kind: failed, err: err}
			return
		}
		result <- outcome{kind: passed}
	}()

	var o outcome
	select {
	case o = <-result:
	case <-ctx.Done():
		o = outcome{kind: timedOut}
	}
	if o.kind == failed && errors.Is(ctx.Err(), context.DeadlineExceeded) {
		o = outcome{kind: timedOut} // the check returned its context's error
	}
	h.record(e, i, o)
}

// record stores an outcome. The last one detaches the evaluation, so that the
// next request starts a new one, and finishes it.
func (h *Handler) record(e *evaluation, i int, o outcome) {
	h.mu.Lock()
	e.outcomes[i] = o
	e.remaining--
	last := e.remaining == 0
	if last && h.current == e {
		h.current = nil
	}
	h.mu.Unlock()
	if last {
		h.finish(e)
	}
}

// finish logs the failures, unless every waiter has left, then releases the
// waiters: a response is never written before its failures are logged.
func (h *Handler) finish(e *evaluation) {
	h.mu.Lock()
	abandoned := e.abandoned
	h.mu.Unlock()
	if !abandoned {
		h.logFailures(e)
	}
	close(e.done)
	e.cancel()
}

// leave stops counting the caller as a waiter. The last waiter to leave an
// evaluation still in progress cancels it.
func (h *Handler) leave(e *evaluation) {
	h.mu.Lock()
	defer h.mu.Unlock()
	e.waiters--
	if e.waiters > 0 {
		return
	}
	select {
	case <-e.done:
		return
	default:
	}
	e.abandoned = true
	e.cancel()
	if h.current == e {
		h.current = nil
	}
}

// logFailures logs each failed check of a completed evaluation once.
func (h *Handler) logFailures(e *evaluation) {
	for i, o := range e.outcomes {
		c := h.checks[i]
		attrs := []slog.Attr{slog.String("check", c.name)}
		switch o.kind {
		case passed:
			continue
		case failed:
			attrs = append(attrs, slog.String("reason", "error"), slog.Any("error", o.err))
		case timedOut:
			attrs = append(attrs, slog.String("reason", "timeout"), slog.Duration("timeout", c.timeout))
		case panicked:
			attrs = append(attrs, slog.String("reason", "panic"), slog.Any("panic", o.panicValue), slog.String("stack", string(o.stack)))
		case stillRunning:
			attrs = append(attrs, slog.String("reason", "still running"))
		}
		h.logger.LogAttrs(e.ctx, slog.LevelWarn, "health: check failed", attrs...)
	}
}

package lifecycle_test

import (
	"context"
	"log/slog"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/paccolamano/go/lifecycle"
)

// entry is one event of a test component: run, stop, canceled or returned.
type entry struct {
	name string
	kind string
	at   time.Duration
}

// eventLog is a concurrency-safe ordered list of component events. Times are
// measured from its creation, in virtual time inside synctest bubbles.
type eventLog struct {
	mu      sync.Mutex
	start   time.Time
	entries []entry
}

func newEventLog() *eventLog { return &eventLog{start: time.Now()} }

func (l *eventLog) add(name, kind string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.entries = append(l.entries, entry{name: name, kind: kind, at: time.Since(l.start)})
}

func (l *eventLog) all() []entry {
	l.mu.Lock()
	defer l.mu.Unlock()
	return slices.Clone(l.entries)
}

// order returns the names of the events of kind, in order.
func (l *eventLog) order(kind string) []string {
	var out []string
	for _, e := range l.all() {
		if e.kind == kind {
			out = append(out, e.name)
		}
	}
	return out
}

// at returns the time of the first event of name and kind.
func (l *eventLog) at(name, kind string) (time.Duration, bool) {
	for _, e := range l.all() {
		if e.name == name && e.kind == kind {
			return e.at, true
		}
	}
	return 0, false
}

// index returns the position of the first event of name and kind, or -1.
func (l *eventLog) index(name, kind string) int {
	for i, e := range l.all() {
		if e.name == name && e.kind == kind {
			return i
		}
	}
	return -1
}

func (l *eventLog) has(name, kind string) bool { return l.index(name, kind) >= 0 }

// plainComp implements only Component.
type plainComp struct {
	name string
	log  *eventLog

	// returnAfter, when positive, makes Run return on its own after this delay,
	// with err, or panic with panicValue when it is not nil.
	returnAfter time.Duration
	err         error
	panicValue  any

	// ignoreCancel makes Run ignore the cancellation of its context and wait for
	// release instead.
	ignoreCancel bool
	release      chan struct{}

	// workAfterCancel keeps Run working this long after its cancellation.
	workAfterCancel time.Duration
	// afterErr, when set, is returned after the cancellation instead of the
	// context's error.
	afterErr error

	// onRun is called when Run starts, with its context.
	onRun func(ctx context.Context)
}

func (c *plainComp) Run(ctx context.Context) error {
	c.log.add(c.name, "run")
	defer c.log.add(c.name, "returned")
	if c.onRun != nil {
		c.onRun(ctx)
	}
	var early <-chan time.Time
	if c.returnAfter > 0 {
		early = time.After(c.returnAfter)
	}
	done := ctx.Done()
	if c.ignoreCancel {
		done = nil
	}
	select {
	case <-early:
		if c.panicValue != nil {
			panic(c.panicValue)
		}
		return c.err
	case <-c.release:
		return nil
	case <-done:
		c.log.add(c.name, "canceled")
	}
	if c.workAfterCancel > 0 {
		time.Sleep(c.workAfterCancel)
	}
	if c.afterErr != nil {
		return c.afterErr
	}
	return ctx.Err()
}

// stopMode selects the behavior of stopperComp.Stop.
type stopMode int

const (
	stopReturns  stopMode = iota // Stop returns at once
	stopWaitsRun                 // Stop returns once Run has returned
	stopNever                    // Stop returns only when release is closed
	stopFails                    // Stop returns stopErr
	stopCtxErr                   // Stop waits for its context and returns its error
	stopPanics                   // Stop panics
)

// stopperComp implements Component and Stopper. Once Stop has been called, Run
// processes an item of the given duration, then returns runErr.
type stopperComp struct {
	name    string
	log     *eventLog
	mode    stopMode
	stopErr error
	release chan struct{}

	item   time.Duration
	runErr error

	// returnAfter, when positive, makes Run return on its own after this delay,
	// with err, or panic with panicValue when it is not nil.
	returnAfter time.Duration
	err         error
	panicValue  any

	stopped      chan struct{}
	runDone      chan struct{}
	once         sync.Once
	stopCalls    atomic.Int32
	itemCtxValid atomic.Bool
	itemDone     atomic.Bool
}

func newStopper(name string, log *eventLog, mode stopMode) *stopperComp {
	return &stopperComp{
		name:    name,
		log:     log,
		mode:    mode,
		release: make(chan struct{}),
		stopped: make(chan struct{}),
		runDone: make(chan struct{}),
	}
}

func (c *stopperComp) Run(ctx context.Context) error {
	c.log.add(c.name, "run")
	defer close(c.runDone)
	defer c.log.add(c.name, "returned")
	var early <-chan time.Time
	if c.returnAfter > 0 {
		early = time.After(c.returnAfter)
	}
	select {
	case <-early:
		if c.panicValue != nil {
			panic(c.panicValue)
		}
		return c.err
	case <-c.stopped:
	case <-ctx.Done():
		c.log.add(c.name, "canceled")
		return ctx.Err()
	}
	if c.item > 0 {
		select {
		case <-time.After(c.item):
			c.itemDone.Store(true)
			c.itemCtxValid.Store(ctx.Err() == nil)
		case <-ctx.Done():
			c.log.add(c.name, "canceled")
			return ctx.Err()
		}
	}
	return c.runErr
}

func (c *stopperComp) Stop(ctx context.Context) error {
	c.log.add(c.name, "stop")
	c.stopCalls.Add(1)
	c.once.Do(func() { close(c.stopped) })
	switch c.mode {
	case stopWaitsRun:
		<-c.runDone
	case stopNever:
		<-c.release
	case stopFails:
		return c.stopErr
	case stopCtxErr:
		<-ctx.Done()
		return ctx.Err()
	case stopPanics:
		panic("stop panic")
	}
	return nil
}

// mustNew returns an App built with opts, failing the test on error.
func mustNew(tb testing.TB, opts ...lifecycle.Option) *lifecycle.App {
	tb.Helper()
	app, err := lifecycle.New(opts...)
	if err != nil {
		tb.Fatalf("lifecycle.New: %v", err)
	}
	return app
}

// runAsync runs app in its own goroutine and delivers the result of Run.
func runAsync(ctx context.Context, app *lifecycle.App) <-chan error {
	ch := make(chan error, 1)
	go func() { ch <- app.Run(ctx) }()
	return ch
}

// record is one log record kept by logRecorder.
type record struct {
	level slog.Level
	msg   string
	attrs map[string]slog.Value
}

// logRecorder is a concurrency-safe slog.Handler that keeps every record.
type logRecorder struct {
	mu   *sync.Mutex
	recs *[]record
}

func newLogRecorder() *logRecorder {
	return &logRecorder{mu: &sync.Mutex{}, recs: &[]record{}}
}

func (h *logRecorder) Enabled(context.Context, slog.Level) bool { return true }

func (h *logRecorder) Handle(_ context.Context, r slog.Record) error {
	rec := record{level: r.Level, msg: r.Message, attrs: map[string]slog.Value{}}
	r.Attrs(func(a slog.Attr) bool {
		rec.attrs[a.Key] = a.Value.Resolve()
		return true
	})
	h.mu.Lock()
	defer h.mu.Unlock()
	*h.recs = append(*h.recs, rec)
	return nil
}

func (h *logRecorder) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *logRecorder) WithGroup(string) slog.Handler      { return h }

// lifecycleLogger returns the option that logs an App's events to rec.
func lifecycleLogger(rec *logRecorder) lifecycle.Option {
	return lifecycle.WithLogger(slog.New(rec))
}

// find returns the records with message msg, in order.
func (h *logRecorder) find(msg string) []record {
	h.mu.Lock()
	defer h.mu.Unlock()
	var out []record
	for _, r := range *h.recs {
		if r.msg == msg {
			out = append(out, r)
		}
	}
	return out
}

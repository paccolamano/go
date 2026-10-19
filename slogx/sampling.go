package slogx

import (
	"cmp"
	"context"
	"errors"
	"log/slog"
	"slices"
	"sync"
	"time"
)

// maxGroups bounds the number of groups a sampler tracks in an interval.
const maxGroups = 1024

// summaryMessage is the message of the records that summarize the drops.
const summaryMessage = "records dropped by sampling"

// SamplingHandler samples repeated records. Records are grouped by level and
// message; their attributes do not matter. In each interval, the first records
// of a group are written, then one every thereafter, as configured with
// WithSampling: with first=10 and thereafter=100, a burst of 10,000 identical
// records writes 109 of them. Records at slog.LevelError and above are always
// written, unless WithSampleErrors is given.
//
// After an interval with drops, one summary record per group reports them:
// its level is the group's, its message is "records dropped by sampling", and
// its group "sampling" holds the original message (msg), the number of dropped
// records (dropped) and the period they cover (start, end). Summaries are
// emitted by the first record handled after the interval ends, or by Flush,
// which should be called at shutdown. They go to the next handler as it was
// when the SamplingHandler was created, with a context of their own, so they
// carry no attribute of the logger or of any request.
//
// At most 1,024 groups are tracked in an interval; the records of any other
// group are written. The handlers derived with WithAttrs and WithGroup share
// the counts. A SamplingHandler starts no goroutine.
type SamplingHandler struct {
	next slog.Handler
	s    *sampler
}

// NewSamplingHandler returns a SamplingHandler that passes records to next.
//
// Accepted options: WithSampling, which is required, and WithSampleErrors.
func NewSamplingHandler(next slog.Handler, opts ...Option) (*SamplingHandler, error) {
	if next == nil {
		return nil, errNilHandler("NewSamplingHandler")
	}
	cfg, err := newConfig("NewSamplingHandler", optSampling|optSampleErrors, optSampling, opts)
	if err != nil {
		return nil, err
	}
	return newSamplingHandler(next, cfg), nil
}

func newSamplingHandler(next slog.Handler, cfg config) *SamplingHandler {
	return &SamplingHandler{next: next, s: newSampler(next, cfg.first, cfg.thereafter, cfg.interval, cfg.sampleErrors)}
}

// Enabled reports whether the next handler handles records at level.
func (h *SamplingHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.next.Enabled(ctx, level)
}

// Handle emits the summaries of an interval that has ended, then passes r to
// the next handler unless it is dropped. The next handler's error is returned
// unchanged, joined with the errors of the summaries if they failed.
func (h *SamplingHandler) Handle(ctx context.Context, r slog.Record) error {
	pass, sums := h.s.decide(r.Level, r.Message, time.Now())
	var err error
	if len(sums) > 0 {
		err = h.s.emit(context.Background(), sums) //nolint:contextcheck // summaries must not carry request attributes (FR-030)
	}
	switch {
	case !pass:
		return err
	case err == nil:
		return handle(ctx, h.next, r)
	default:
		return errors.Join(err, handle(ctx, h.next, r))
	}
}

// WithAttrs returns a SamplingHandler, sharing the counts, whose next handler
// has the attributes.
func (h *SamplingHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	if len(attrs) == 0 {
		return h
	}
	return &SamplingHandler{next: h.next.WithAttrs(attrs), s: h.s}
}

// WithGroup returns a SamplingHandler, sharing the counts, whose next handler
// has the group open.
func (h *SamplingHandler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	return &SamplingHandler{next: h.next.WithGroup(name), s: h.s}
}

// Flush ends the current interval: it emits the summaries of the groups with
// drops, with ctx, and starts a new interval. Call it at shutdown with a
// context that carries no request attribute, so that no drop goes unreported.
func (h *SamplingHandler) Flush(ctx context.Context) error {
	return h.s.flush(ctx)
}

type groupKey struct {
	level slog.Level
	msg   string
}

type groupCount struct{ n, dropped int64 }

// sampler holds the counts shared by a SamplingHandler and the handlers
// derived from it.
type sampler struct {
	root         slog.Handler // receives the summaries
	first        int64
	thereafter   int64
	interval     time.Duration
	sampleErrors bool

	mu     sync.Mutex
	start  time.Time // start of the current interval; zero before the first record
	groups map[groupKey]groupCount
}

func newSampler(root slog.Handler, first, thereafter int, interval time.Duration, sampleErrors bool) *sampler {
	return &sampler{
		root:         root,
		first:        int64(first),
		thereafter:   int64(thereafter),
		interval:     interval,
		sampleErrors: sampleErrors,
		groups:       map[groupKey]groupCount{},
	}
}

// decide reports whether a record of level and msg, handled at now, is
// written, and returns the summaries of the interval that has ended, if any.
func (s *sampler) decide(level slog.Level, msg string, now time.Time) (bool, []slog.Record) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var sums []slog.Record
	switch {
	case s.start.IsZero():
		s.start = now
	case now.Sub(s.start) >= s.interval:
		sums = s.rollLocked(now, s.start.Add(s.interval))
	}
	if level >= slog.LevelError && !s.sampleErrors {
		return true, sums
	}
	key := groupKey{level: level, msg: msg}
	g, tracked := s.groups[key]
	if !tracked && len(s.groups) >= maxGroups {
		return true, sums
	}
	g.n++
	pass := g.n <= s.first || (g.n-s.first)%s.thereafter == 0
	if !pass {
		g.dropped++
	}
	s.groups[key] = g
	return pass, sums
}

// rollLocked returns the summaries of the current interval, which ends at end,
// and starts a new interval at now. s.mu must be held.
func (s *sampler) rollLocked(now, end time.Time) []slog.Record {
	type entry struct {
		key   groupKey
		count groupCount
	}
	var entries []entry
	for k, g := range s.groups {
		if g.dropped > 0 {
			entries = append(entries, entry{k, g})
		}
	}
	slices.SortFunc(entries, func(a, b entry) int {
		return cmp.Or(cmp.Compare(a.key.msg, b.key.msg), cmp.Compare(a.key.level, b.key.level))
	})
	sums := make([]slog.Record, 0, len(entries))
	for _, e := range entries {
		r := slog.NewRecord(now, e.key.level, summaryMessage, 0)
		r.AddAttrs(slog.Group("sampling",
			slog.String("msg", e.key.msg),
			slog.Int64("dropped", e.count.dropped),
			slog.Time("start", s.start),
			slog.Time("end", end),
		))
		sums = append(sums, r)
	}
	clear(s.groups)
	s.start = now
	return sums
}

// flush ends the current interval now and emits its summaries with ctx.
func (s *sampler) flush(ctx context.Context) error {
	now := time.Now()
	s.mu.Lock()
	if s.start.IsZero() {
		s.mu.Unlock()
		return nil
	}
	sums := s.rollLocked(now, now)
	s.mu.Unlock()
	return s.emit(ctx, sums)
}

// emit passes the summaries to the root handler and joins their errors.
func (s *sampler) emit(ctx context.Context, sums []slog.Record) error {
	var errs []error
	for _, r := range sums {
		if err := handle(ctx, s.root, r); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// trackedGroups returns the number of groups tracked in the current interval.
func (s *sampler) trackedGroups() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.groups)
}

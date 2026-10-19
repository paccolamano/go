// Package slogxtest provides a slog.Handler that records what is logged, for
// tests:
//
//	rec := slogxtest.NewRecorder()
//	logger := slog.New(rec)
//	// ... code under test logs with logger ...
//	r, ok := rec.Last()
//	if v, found := r.Attr("request_id"); !ok || r.Message != "panic recovered" || !found || v.String() != "abc" {
//		t.Errorf("unexpected record %+v", r)
//	}
package slogxtest

import (
	"context"
	"log/slog"
	"sync"

	"github.com/paccolamano/go/slogx/internal/attrs"
)

// Recorder is a slog.Handler that keeps every record it handles, in order. It
// honors WithAttrs and WithGroup: the recorders they return share the records
// of the recorder they derive from, and each captured record holds the
// attributes and groups of the logger that produced it. A Recorder is safe for
// concurrent use.
type Recorder struct {
	s     *store
	level slog.Leveler
	ops   []op
}

type store struct {
	mu      sync.Mutex
	records []Record
}

// op is a WithGroup (group != "") or a WithAttrs operation.
type op struct {
	group string
	attrs []slog.Attr
}

// Option configures a Recorder.
type Option func(*Recorder)

// WithLevel sets the minimum level of the records the Recorder handles. Without
// it, or with nil, every level is enabled.
func WithLevel(level slog.Leveler) Option {
	return func(r *Recorder) { r.level = level }
}

// NewRecorder returns an empty Recorder.
func NewRecorder(opts ...Option) *Recorder {
	r := &Recorder{s: &store{}}
	for _, opt := range opts {
		if opt != nil {
			opt(r)
		}
	}
	return r
}

// Enabled reports whether level is at least the minimum level.
func (r *Recorder) Enabled(_ context.Context, level slog.Level) bool {
	return r.level == nil || level >= r.level.Level()
}

// Handle captures rec. It always returns nil.
func (r *Recorder) Handle(_ context.Context, rec slog.Record) error {
	inner := make([]slog.Attr, 0, rec.NumAttrs())
	rec.Attrs(func(a slog.Attr) bool {
		inner = append(inner, a)
		return true
	})
	for i := len(r.ops) - 1; i >= 0; i-- {
		o := r.ops[i]
		if o.group != "" {
			inner = []slog.Attr{{Key: o.group, Value: slog.GroupValue(inner...)}}
			continue
		}
		inner = append(o.attrs[:len(o.attrs):len(o.attrs)], inner...)
	}
	captured := Record{Time: rec.Time, Level: rec.Level, Message: rec.Message, Attrs: normalize(inner, 0)}
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	r.s.records = append(r.s.records, captured)
	return nil
}

// normalize resolves every value, drops empty attributes and empty groups, and
// inlines groups with an empty key. A group value nested deeper than
// attrs.MaxDepth is replaced by attrs.DepthPlaceholder. depth is the number of
// groups above as.
func normalize(as []slog.Attr, depth int) []slog.Attr {
	out := make([]slog.Attr, 0, len(as))
	for _, a := range as {
		a.Value = a.Value.Resolve()
		if a.Value.Kind() != slog.KindGroup {
			if a.Key != "" {
				out = append(out, a)
			}
			continue
		}
		if depth >= attrs.MaxDepth {
			if a.Key != "" {
				out = append(out, slog.String(a.Key, attrs.DepthPlaceholder))
			}
			continue
		}
		group := normalize(a.Value.Group(), depth+1)
		switch {
		case len(group) == 0:
		case a.Key == "":
			out = append(out, group...)
		default:
			out = append(out, slog.Attr{Key: a.Key, Value: slog.GroupValue(group...)})
		}
	}
	return out
}

// WithAttrs returns a Recorder that adds attrs, resolved now, to the records it
// captures.
func (r *Recorder) WithAttrs(as []slog.Attr) slog.Handler {
	if len(as) == 0 {
		return r
	}
	resolved := make([]slog.Attr, len(as))
	for i, a := range as {
		a.Value = a.Value.Resolve()
		resolved[i] = a
	}
	c := *r
	c.ops = append(r.ops[:len(r.ops):len(r.ops)], op{attrs: resolved})
	return &c
}

// WithGroup returns a Recorder that captures attributes inside the group name.
func (r *Recorder) WithGroup(name string) slog.Handler {
	if name == "" {
		return r
	}
	c := *r
	c.ops = append(r.ops[:len(r.ops):len(r.ops)], op{group: name})
	return &c
}

// Records returns a copy of the captured records, in the order they were
// handled.
func (r *Recorder) Records() []Record {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	return append([]Record(nil), r.s.records...)
}

// Len returns the number of captured records.
func (r *Recorder) Len() int {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	return len(r.s.records)
}

// Last returns the last captured record, and false when there is none.
func (r *Recorder) Last() (Record, bool) {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	if len(r.s.records) == 0 {
		return Record{}, false
	}
	return r.s.records[len(r.s.records)-1], true
}

package slogx

import (
	"context"
	"log/slog"
	"strings"

	"github.com/paccolamano/go/slogx/internal/attrs"
)

// redacted replaces the values of sensitive keys and the rendering of Secret.
const redacted = "[REDACTED]"

// RedactHandler replaces the values of sensitive keys with [REDACTED] before
// passing records to the next handler.
//
// A key matches when it equals one of the sensitive keys without regard to
// case; it must match as a whole, never as a substring. Matching applies at
// every depth and to every attribute: those of the record, those added to the
// logger, those inside groups and those inside the groups that LogValuer values
// resolve to. The key is checked before the value is resolved, so the LogValue
// method of a sensitive value is never called. An attribute or a group whose
// key matches is written as key=[REDACTED], content included; so is a group
// opened on the logger with WithGroup, which then drops everything logged in it.
//
// The message is never changed, and values are not inspected beyond their keys
// and groups: a struct, a map or an error encoded by the output handler is
// passed through as it is. Wrap such values in a Secret.
type RedactHandler struct {
	next   slog.Handler
	keys   []string
	sealed bool // a group opened on the logger matched: its content is dropped
}

// NewRedactHandler returns a RedactHandler that passes records to next.
//
// Accepted options: WithRedactKeys, which is required.
func NewRedactHandler(next slog.Handler, opts ...Option) (*RedactHandler, error) {
	if next == nil {
		return nil, errNilHandler("NewRedactHandler")
	}
	cfg, err := newConfig("NewRedactHandler", optRedactKeys, optRedactKeys, opts)
	if err != nil {
		return nil, err
	}
	return &RedactHandler{next: next, keys: cfg.keys}, nil
}

// Enabled reports whether the next handler handles records at level.
func (h *RedactHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.next.Enabled(ctx, level)
}

// Handle redacts the attributes of r and passes it to the next handler.
func (h *RedactHandler) Handle(ctx context.Context, r slog.Record) error {
	if h.sealed {
		return handle(ctx, h.next, slog.NewRecord(r.Time, r.Level, r.Message, r.PC))
	}
	work := false
	r.Attrs(func(a slog.Attr) bool {
		work = h.needsWork(a, 0)
		return !work
	})
	if !work {
		return handle(ctx, h.next, r)
	}
	out := slog.NewRecord(r.Time, r.Level, r.Message, r.PC)
	r.Attrs(func(a slog.Attr) bool {
		out.AddAttrs(h.redact(a, 0))
		return true
	})
	return handle(ctx, h.next, out)
}

// WithAttrs returns a RedactHandler whose next handler has the attributes,
// redacted.
func (h *RedactHandler) WithAttrs(as []slog.Attr) slog.Handler {
	if h.sealed || len(as) == 0 {
		return h
	}
	out := make([]slog.Attr, len(as))
	for i, a := range as {
		out[i] = h.redact(a, 0)
	}
	return &RedactHandler{next: h.next.WithAttrs(out), keys: h.keys}
}

// WithGroup returns a RedactHandler whose next handler has the group open.
// When name is a sensitive key, the group is written as name=[REDACTED] and
// everything logged in it is dropped.
func (h *RedactHandler) WithGroup(name string) slog.Handler {
	if h.sealed || name == "" {
		return h
	}
	if h.match(name) {
		marker := []slog.Attr{slog.String(name, redacted)}
		return &RedactHandler{next: h.next.WithAttrs(marker), keys: h.keys, sealed: true}
	}
	return &RedactHandler{next: h.next.WithGroup(name), keys: h.keys}
}

func (h *RedactHandler) match(key string) bool {
	for _, k := range h.keys {
		if strings.EqualFold(k, key) {
			return true
		}
	}
	return false
}

// needsWork reports whether redact would change a: its key matches, its value
// must be resolved, or a group below it needs work. depth is the number of
// groups above a.
func (h *RedactHandler) needsWork(a slog.Attr, depth int) bool {
	if h.match(a.Key) || a.Value.Kind() == slog.KindLogValuer {
		return true
	}
	if a.Value.Kind() != slog.KindGroup {
		return false
	}
	if depth >= attrs.MaxDepth {
		return true
	}
	for _, c := range a.Value.Group() {
		if h.needsWork(c, depth+1) {
			return true
		}
	}
	return false
}

// redact returns a with its value replaced when its key matches, and otherwise
// with its value resolved and its group redacted recursively. depth is the
// number of groups above a.
func (h *RedactHandler) redact(a slog.Attr, depth int) slog.Attr {
	if h.match(a.Key) {
		return slog.String(a.Key, redacted)
	}
	a.Value = a.Value.Resolve()
	if a.Value.Kind() != slog.KindGroup {
		return a
	}
	if depth >= attrs.MaxDepth {
		a.Value = slog.StringValue(attrs.DepthPlaceholder)
		return a
	}
	group := a.Value.Group()
	out := make([]slog.Attr, len(group))
	for i, c := range group {
		out[i] = h.redact(c, depth+1)
	}
	a.Value = slog.GroupValue(out...)
	return a
}

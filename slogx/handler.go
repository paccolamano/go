package slogx

import (
	"context"
	"log/slog"
)

// Handler is the slog.Handler built by New. It passes each record through, in
// order:
//
//  1. the level, decided once for the whole pipeline (see Handler.Enabled);
//  2. the context attributes, written at the top level of the record;
//  3. the redaction of the sensitive keys given with WithRedactKeys, so that
//     the context attributes are redacted too;
//  4. the next handler given to New.
type Handler struct {
	level slog.Leveler // the default level, nil without WithDefaultLevel
	next  slog.Handler // the context handler, then the redaction handler, then the next handler given to New
}

// New returns a Handler that passes records to next, typically a
// slog.JSONHandler or a slog.TextHandler. The context attributes and the
// per-request level are always active, and every option applies.
//
// It returns an error wrapping ErrInvalidConfig when next is nil, when an
// option is invalid, or when next is a Fanout that is invalid or comes without
// WithDefaultLevel. A Fanout is recognized only as next itself.
func New(next slog.Handler, opts ...Option) (*Handler, error) {
	if next == nil {
		return nil, invalid("next", "must not be nil")
	}
	cfg, err := newConfig(opts)
	if err != nil {
		return nil, err
	}
	if f, ok := next.(*fanout); ok {
		switch {
		case f.invalid != "":
			return nil, invalid("Fanout", f.invalid)
		case cfg.level == nil:
			return nil, invalid("Fanout", "requires WithDefaultLevel")
		}
	}
	h := next
	if len(cfg.keys) > 0 {
		h = &redactHandler{next: h, keys: cfg.keys}
	}
	return &Handler{level: cfg.level, next: &contextHandler{root: h, next: h, sources: cfg.sources}}, nil
}

// Enabled reports whether records at level are handled for ctx:
//
//  1. a level attached to ctx with WithLevel decides alone;
//  2. otherwise, the default level given with WithDefaultLevel decides, read
//     at this call;
//  3. otherwise, the next handler decides.
//
// No handler after it checks the level again: an enabled record reaches the
// next handler whatever the next handler's own level.
func (h *Handler) Enabled(ctx context.Context, level slog.Level) bool {
	if threshold, ok := levelFrom(ctx); ok {
		return level >= threshold
	}
	if h.level != nil {
		return level >= h.level.Level()
	}
	return h.next.Enabled(ctx, level)
}

// Handle passes r through the pipeline.
func (h *Handler) Handle(ctx context.Context, r slog.Record) error {
	return handle(ctx, h.next, r)
}

// WithAttrs returns a Handler whose pipeline has the attributes.
func (h *Handler) WithAttrs(attrs []slog.Attr) slog.Handler {
	if len(attrs) == 0 {
		return h
	}
	return &Handler{level: h.level, next: h.next.WithAttrs(attrs)}
}

// WithGroup returns a Handler whose pipeline has the group open.
func (h *Handler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	return &Handler{level: h.level, next: h.next.WithGroup(name)}
}

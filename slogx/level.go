package slogx

import (
	"context"
	"log/slog"
)

// LevelHandler applies the level attached to a context with WithLevel. For a
// record logged with such a context, it enables the record when its level is at
// least the context level, without asking the next handler; otherwise the next
// handler decides. Because slog.Logger asks only the outermost handler, a
// LevelHandler must be the outermost one for the context level to apply.
//
// The context level is available only to the logging methods that take a
// context, such as DebugContext and LogAttrs.
type LevelHandler struct{ next slog.Handler }

// NewLevelHandler returns a LevelHandler that passes records to next.
//
// Accepted options: none.
func NewLevelHandler(next slog.Handler, opts ...Option) (*LevelHandler, error) {
	if next == nil {
		return nil, errNilHandler("NewLevelHandler")
	}
	if _, err := newConfig("NewLevelHandler", 0, 0, opts); err != nil {
		return nil, err
	}
	return &LevelHandler{next: next}, nil
}

// Enabled compares level with the level attached to ctx, or asks the next
// handler when ctx carries none.
func (h *LevelHandler) Enabled(ctx context.Context, level slog.Level) bool {
	if threshold, ok := levelFrom(ctx); ok {
		return level >= threshold
	}
	return h.next.Enabled(ctx, level)
}

// Handle passes r to the next handler.
func (h *LevelHandler) Handle(ctx context.Context, r slog.Record) error {
	return handle(ctx, h.next, r)
}

// WithAttrs returns a LevelHandler whose next handler has the attributes.
func (h *LevelHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	if len(attrs) == 0 {
		return h
	}
	return &LevelHandler{next: h.next.WithAttrs(attrs)}
}

// WithGroup returns a LevelHandler whose next handler has the group open.
func (h *LevelHandler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	return &LevelHandler{next: h.next.WithGroup(name)}
}

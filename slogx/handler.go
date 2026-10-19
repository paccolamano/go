package slogx

import (
	"context"
	"log/slog"
)

// Handler is the pipeline built by New. It passes each record through, in
// order:
//
//  1. LevelHandler, outermost, so that the level attached with WithLevel is
//     honored before the output handler's own threshold;
//  2. SamplingHandler, when WithSampling is given, before the others so that a
//     dropped record costs nothing more and summaries carry no request
//     attribute;
//  3. ContextHandler, which adds the context attributes;
//  4. RedactHandler, when WithRedactKeys is given, after ContextHandler so that
//     context attributes are redacted too;
//  5. the output handler given to New.
type Handler struct {
	head    slog.Handler
	sampler *sampler // nil without WithSampling
}

// New returns a Handler that composes the handlers of this package, in the
// correct order, over next, typically a slog.JSONHandler or slog.TextHandler.
// The context attributes and the per-request level are always active.
//
// Accepted options: all of them.
//
// New(next, opts...) behaves exactly like this manual composition:
//
//	r, err := slogx.NewRedactHandler(next, slogx.WithRedactKeys(keys...))           // only with WithRedactKeys
//	c, err := slogx.NewContextHandler(r, slogx.WithContextFunc(fn))
//	s, err := slogx.NewSamplingHandler(c, slogx.WithSampling(first, thereafter, i)) // only with WithSampling
//	l, err := slogx.NewLevelHandler(s)
func New(next slog.Handler, opts ...Option) (*Handler, error) {
	if next == nil {
		return nil, errNilHandler("New")
	}
	cfg, err := newConfig("New", optContextFunc|optRedactKeys|optSampling|optSampleErrors, 0, opts)
	if err != nil {
		return nil, err
	}
	h := next
	if len(cfg.keys) > 0 {
		h = &RedactHandler{next: h, keys: cfg.keys}
	}
	h = &ContextHandler{root: h, next: h, fns: cfg.fns}
	var s *sampler
	if cfg.set&optSampling != 0 {
		sh := newSamplingHandler(h, cfg)
		s, h = sh.s, sh
	}
	return &Handler{head: &LevelHandler{next: h}, sampler: s}, nil
}

// Enabled reports whether the pipeline handles records at level for ctx.
func (h *Handler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.head.Enabled(ctx, level)
}

// Handle passes r through the pipeline.
func (h *Handler) Handle(ctx context.Context, r slog.Record) error {
	return handle(ctx, h.head, r)
}

// WithAttrs returns a Handler whose pipeline has the attributes.
func (h *Handler) WithAttrs(attrs []slog.Attr) slog.Handler {
	if len(attrs) == 0 {
		return h
	}
	return &Handler{head: h.head.WithAttrs(attrs), sampler: h.sampler}
}

// WithGroup returns a Handler whose pipeline has the group open.
func (h *Handler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	return &Handler{head: h.head.WithGroup(name), sampler: h.sampler}
}

// Flush emits the summaries of the current sampling interval, with ctx, and
// starts a new interval; see SamplingHandler.Flush. It returns nil when
// WithSampling was not given. Call it at shutdown with a context that carries
// no request attribute.
func (h *Handler) Flush(ctx context.Context) error {
	if h.sampler == nil {
		return nil
	}
	return h.sampler.flush(ctx)
}

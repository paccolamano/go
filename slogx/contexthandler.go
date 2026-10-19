package slogx

import (
	"context"
	"fmt"
	"log/slog"
)

// ContextHandler adds context attributes to every record: the attributes
// attached to the record's context with With, then those computed by the
// functions given with WithContextFunc.
//
// Context attributes are always written at the top level of the record, also
// when the logger has open groups, so that they are found in the same place in
// every record. They come after the logger's top-level attributes and before
// the record's own attributes. When the record, or the logger before any group,
// already has an attribute with the same key at the top level, that attribute
// wins and the context attribute is skipped.
//
// Context attributes are available only to the logging methods that take a
// context, such as InfoContext and LogAttrs; Info and the other methods without
// a context use context.Background().
type ContextHandler struct {
	root    slog.Handler // next, with only the attributes added before the first group
	next    slog.Handler // root, with every later WithGroup and WithAttrs applied
	topKeys []string     // keys of the logger attributes added before the first group
	ops     []op         // operations applied after the first group
	fns     []func(context.Context) []slog.Attr
}

// op is a WithGroup (group != "") or a WithAttrs operation.
type op struct {
	group string
	attrs []slog.Attr
}

// NewContextHandler returns a ContextHandler that passes records to next.
//
// Accepted options: WithContextFunc.
func NewContextHandler(next slog.Handler, opts ...Option) (*ContextHandler, error) {
	if next == nil {
		return nil, errNilHandler("NewContextHandler")
	}
	cfg, err := newConfig("NewContextHandler", optContextFunc, 0, opts)
	if err != nil {
		return nil, err
	}
	return &ContextHandler{root: next, next: next, fns: cfg.fns}, nil
}

// Enabled reports whether the next handler handles records at level.
func (h *ContextHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.next.Enabled(ctx, level)
}

// Handle adds the context attributes to r and passes it to the next handler. A
// nil ctx carries no attribute, and the extraction functions are not called.
func (h *ContextHandler) Handle(ctx context.Context, r slog.Record) error {
	if ctx == nil || (len(h.fns) == 0 && len(attrsFrom(ctx)) == 0) {
		return handle(ctx, h.next, r)
	}
	out := slog.NewRecord(r.Time, r.Level, r.Message, r.PC)
	if len(h.ops) == 0 {
		skip := func(key string) bool { return containsString(h.topKeys, key) || recordHasKey(&r, key) }
		if h.addContext(ctx, &out, skip) == 0 {
			return handle(ctx, h.next, r)
		}
		r.Attrs(func(a slog.Attr) bool {
			out.AddAttrs(a)
			return true
		})
		return handle(ctx, h.next, out)
	}
	skip := func(key string) bool { return containsString(h.topKeys, key) }
	if h.addContext(ctx, &out, skip) == 0 {
		return handle(ctx, h.next, r)
	}
	out.AddAttrs(h.fold(r)...)
	return handle(ctx, h.root, out)
}

// addContext adds to out the context attributes whose key skip rejects not,
// and returns how many it added.
func (h *ContextHandler) addContext(ctx context.Context, out *slog.Record, skip func(string) bool) int {
	n := 0
	attached := attrsFrom(ctx)
	for _, a := range attached {
		if !skip(a.Key) {
			out.AddAttrs(a)
			n++
		}
	}
	for _, fn := range h.fns {
		for _, a := range flattenAttrs(extract(ctx, fn)) {
			if !skip(a.Key) && !hasKey(attached, a.Key) && !recordHasKey(out, a.Key) {
				out.AddAttrs(a)
				n++
			}
		}
	}
	return n
}

// fold returns the record's attributes wrapped in the groups and attributes
// that the logger added after its first group.
func (h *ContextHandler) fold(r slog.Record) []slog.Attr {
	inner := make([]slog.Attr, 0, r.NumAttrs())
	r.Attrs(func(a slog.Attr) bool {
		inner = append(inner, a)
		return true
	})
	for i := len(h.ops) - 1; i >= 0; i-- {
		o := h.ops[i]
		if o.group != "" {
			inner = []slog.Attr{{Key: o.group, Value: slog.GroupValue(inner...)}}
			continue
		}
		inner = append(o.attrs[:len(o.attrs):len(o.attrs)], inner...)
	}
	return inner
}

// extract calls fn. When fn panics, the result is a single attribute that
// reports the panic, so that the record is still written.
func extract(ctx context.Context, fn func(context.Context) []slog.Attr) (attrs []slog.Attr) {
	defer func() {
		if v := recover(); v != nil {
			attrs = []slog.Attr{slog.String("!CONTEXTFUNC", fmt.Sprintf("panic: %v", v))}
		}
	}()
	return fn(ctx)
}

// WithAttrs returns a ContextHandler whose next handler has the attributes.
func (h *ContextHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	if len(attrs) == 0 {
		return h
	}
	c := *h
	if len(h.ops) == 0 {
		c.root = h.root.WithAttrs(attrs)
		c.next = c.root
		keys := make([]string, len(h.topKeys), len(h.topKeys)+len(attrs))
		copy(keys, h.topKeys)
		for _, a := range flattenAttrs(attrs) {
			keys = append(keys, a.Key)
		}
		c.topKeys = keys
		return &c
	}
	c.ops = append(h.ops[:len(h.ops):len(h.ops)], op{attrs: attrs})
	c.next = h.next.WithAttrs(attrs)
	return &c
}

// WithGroup returns a ContextHandler whose next handler has the group open.
// Context attributes stay outside it.
func (h *ContextHandler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	c := *h
	c.ops = append(h.ops[:len(h.ops):len(h.ops)], op{group: name})
	c.next = h.next.WithGroup(name)
	return &c
}

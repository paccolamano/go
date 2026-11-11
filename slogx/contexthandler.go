package slogx

import (
	"context"
	"fmt"
	"log/slog"
)

// contextHandler adds context attributes to every record: the attributes
// attached to the record's context with With, then those of the extraction
// functions and context values given with WithContextFunc and
// WithContextValue, in configuration order.
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
type contextHandler struct {
	root    slog.Handler // next, with only the attributes added before the first group
	next    slog.Handler // root, with every later WithGroup and WithAttrs applied
	topKeys []string     // keys of the logger attributes added before the first group
	ops     []op         // operations applied after the first group
	sources []source     // extraction functions and context values, in configuration order
}

// op is a WithGroup (group != "") or a WithAttrs operation.
type op struct {
	group string
	attrs []slog.Attr
}

// Enabled reports whether the next handler handles records at level.
func (h *contextHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.next.Enabled(ctx, level)
}

// Handle adds the context attributes to r and passes it to the next handler. A
// nil ctx carries no attribute, and the extraction functions are not called.
func (h *contextHandler) Handle(ctx context.Context, r slog.Record) error {
	if ctx == nil || (len(h.sources) == 0 && len(attrsFrom(ctx)) == 0) {
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
func (h *contextHandler) addContext(ctx context.Context, out *slog.Record, skip func(string) bool) int {
	n := 0
	attached := attrsFrom(ctx)
	for _, a := range attached {
		if !skip(a.Key) {
			out.AddAttrs(a)
			n++
		}
	}
	free := func(key string) bool {
		return !skip(key) && !hasKey(attached, key) && !recordHasKey(out, key)
	}
	add := func(a slog.Attr) {
		if free(a.Key) {
			out.AddAttrs(a)
			n++
		}
	}
	for _, s := range h.sources {
		switch {
		case s.fn != nil:
			for _, a := range flattenAttrs(extract(ctx, s.fn)) {
				add(a)
			}
		case free(s.key): // a context value whose key is taken is not computed
			if a, ok := s.value(ctx); ok {
				add(a)
			}
		}
	}
	return n
}

// fold returns the record's attributes wrapped in the groups and attributes
// that the logger added after its first group.
func (h *contextHandler) fold(r slog.Record) []slog.Attr {
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

// source computes context attributes: an extraction function (fn), or a
// context value (key and get).
type source struct {
	fn  func(context.Context) []slog.Attr
	key string
	get func(context.Context) (slog.Value, bool)
}

// value calls get. When get panics, the result is the attribute that reports
// the panic, as for an extraction function.
func (s source) value(ctx context.Context) (a slog.Attr, ok bool) {
	defer func() {
		if v := recover(); v != nil {
			a, ok = panicAttr(v), true
		}
	}()
	v, ok := s.get(ctx)
	return slog.Attr{Key: s.key, Value: v}, ok
}

// panicAttr reports a panic of an extraction function or a context value.
func panicAttr(v any) slog.Attr {
	return slog.String("!CONTEXTFUNC", fmt.Sprintf("panic: %v", v))
}

// extract calls fn. When fn panics, the result is a single attribute that
// reports the panic, so that the record is still written.
func extract(ctx context.Context, fn func(context.Context) []slog.Attr) (attrs []slog.Attr) {
	defer func() {
		if v := recover(); v != nil {
			attrs = []slog.Attr{panicAttr(v)}
		}
	}()
	return fn(ctx)
}

// WithAttrs returns a contextHandler whose next handler has the attributes.
func (h *contextHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
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

// WithGroup returns a contextHandler whose next handler has the group open.
// Context attributes stay outside it.
func (h *contextHandler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	c := *h
	c.ops = append(h.ops[:len(h.ops):len(h.ops)], op{group: name})
	c.next = h.next.WithGroup(name)
	return &c
}

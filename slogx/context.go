package slogx

import (
	"context"
	"log/slog"

	"github.com/paccolamano/go/slogx/internal/attrs"
)

type attrsKey struct{}

type levelKey struct{}

// ctxAttrs holds the attributes attached to a context and its ancestors. The
// slice has no empty key and no duplicate key, and is never modified once
// stored.
type ctxAttrs struct{ attrs []slog.Attr }

// With returns a context that carries attrs in addition to the attributes of
// ctx. ContextHandler adds them to every record logged with that context.
//
// Empty attributes are dropped and groups with an empty key are inlined. When a
// key is attached again, its latest value replaces the earlier one, in the
// position of the new attribute. ctx itself is not modified, and is returned
// unchanged when attrs adds nothing. A nil ctx is treated as
// context.Background().
func With(ctx context.Context, attrs ...slog.Attr) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	added := flattenAttrs(attrs)
	if len(added) == 0 {
		return ctx
	}
	prev := attrsFrom(ctx)
	merged := make([]slog.Attr, 0, len(prev)+len(added))
	for _, a := range prev {
		if !hasKey(added, a.Key) {
			merged = append(merged, a)
		}
	}
	for i, a := range added {
		if !hasKey(added[i+1:], a.Key) {
			merged = append(merged, a)
		}
	}
	return context.WithValue(ctx, attrsKey{}, &ctxAttrs{attrs: merged})
}

// WithLevel returns a context that carries the minimum level of the records
// logged with it. LevelHandler compares the level of those records with it
// alone, ignoring the threshold of the next handler in both directions: it can
// enable debug records for one request, or silence a request. The innermost
// WithLevel along a chain of contexts applies. A nil ctx is treated as
// context.Background().
func WithLevel(ctx context.Context, level slog.Level) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, levelKey{}, level)
}

// levelFrom returns the level attached to ctx with WithLevel.
func levelFrom(ctx context.Context) (slog.Level, bool) {
	if ctx == nil {
		return 0, false
	}
	level, ok := ctx.Value(levelKey{}).(slog.Level)
	return level, ok
}

// attrsFrom returns the attributes attached to ctx, or nil.
func attrsFrom(ctx context.Context) []slog.Attr {
	if ctx == nil {
		return nil
	}
	if v, ok := ctx.Value(attrsKey{}).(*ctxAttrs); ok {
		return v.attrs
	}
	return nil
}

// flattenAttrs applies attrs.Flatten, without copying when there is nothing to
// drop or inline.
func flattenAttrs(as []slog.Attr) []slog.Attr {
	for _, a := range as {
		if a.Key == "" {
			return attrs.Flatten(nil, as)
		}
	}
	return as
}

func hasKey(as []slog.Attr, key string) bool {
	for _, a := range as {
		if a.Key == key {
			return true
		}
	}
	return false
}

func containsString(ss []string, s string) bool {
	for _, v := range ss {
		if v == s {
			return true
		}
	}
	return false
}

// recordHasKey reports whether r has a top-level attribute with the key.
func recordHasKey(r *slog.Record, key string) bool {
	found := false
	r.Attrs(func(a slog.Attr) bool {
		found = a.Key == key
		return !found
	})
	return found
}

// Package attrs holds attribute-tree helpers shared by slogx and slogxtest.
package attrs

import "log/slog"

// MaxDepth is the number of nested group levels that redaction and the test
// recorder follow. A group value nested deeper is replaced by
// DepthPlaceholder, so that a LogValuer that returns itself cannot recurse
// until the stack overflows.
const MaxDepth = 64

// DepthPlaceholder replaces a group value nested deeper than MaxDepth.
const DepthPlaceholder = "!DEPTH"

// Flatten appends attrs to dst, following the rules of slog.Handler: it drops
// attributes with an empty key, except groups, and inlines the attributes of
// groups with an empty key, recursively. Groups with a key are kept unchanged.
func Flatten(dst, attrs []slog.Attr) []slog.Attr {
	for _, a := range attrs {
		if a.Key != "" {
			dst = append(dst, a)
			continue
		}
		if a.Value.Kind() == slog.KindGroup {
			dst = Flatten(dst, a.Value.Group())
		}
	}
	return dst
}

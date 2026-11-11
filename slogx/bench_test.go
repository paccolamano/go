package slogx_test

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/paccolamano/go/slogx"
)

// benchAttrs are the attributes of a typical access log record.
func benchAttrs() []slog.Attr {
	return []slog.Attr{
		slog.String("method", "GET"),
		slog.String("path", "/orders/42"),
		slog.Int("status", 200),
		slog.Duration("duration", 1234*time.Microsecond),
		slog.String("client", "203.0.113.7"),
	}
}

// benchContext carries tenant (attached) and request_id (for ridAttrs).
func benchContext() context.Context {
	return slogx.With(withRID(context.Background(), "abc"), slog.String("tenant", "t1"))
}

// benchHandlers returns the handlers that the benchmarks compare, over next.
func benchHandlers(next slog.Handler) map[string]slog.Handler {
	keys := slogx.WithRedactKeys(sensitiveKeys...)
	info := slogx.WithDefaultLevel(slog.LevelInfo)
	composed := must(slogx.New(next, slogx.WithContextFunc(ridAttrs), keys))
	return map[string]slog.Handler{
		"composed":       composed,
		"composed-group": composed.WithGroup("db"),
		"value":          must(slogx.New(next, slogx.WithContextValue("request_id", ridFrom), keys, info)),
		"fanout": must(slogx.New(slogx.Fanout(next, next),
			slogx.WithContextValue("request_id", ridFrom), slogx.WithContextValue("trace_id", ridFrom), keys, info)),
		"multi": must(slogx.New(slog.NewMultiHandler(next, next), slogx.WithContextValue("request_id", ridFrom), keys)),
	}
}

// BenchmarkDisabled measures records below the threshold: nothing may
// allocate, also with a default level, context values and a fanout (FR-021,
// SC-006 of spec 013).
func BenchmarkDisabled(b *testing.B) {
	ctx := benchContext()
	attrs := benchAttrs()
	for name, h := range benchHandlers(slog.NewJSONHandler(io.Discard, nil)) {
		logger := slog.New(h)
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				logger.LogAttrs(ctx, slog.LevelDebug, "http request", attrs...)
			}
		})
	}
}

// BenchmarkEnabled measures enabled records. value, the handler with redaction
// and a context value, must take at most twice the time of bare-same-attrs, the
// output handler writing the same 7 attributes (SC-007 of spec 013).
func BenchmarkEnabled(b *testing.B) {
	ctx := benchContext()
	attrs := benchAttrs()
	next := slog.NewJSONHandler(io.Discard, nil)
	handlers := benchHandlers(next)
	handlers["bare"] = next
	for name, h := range handlers {
		logger := slog.New(h)
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				logger.LogAttrs(ctx, slog.LevelInfo, "http request", attrs...)
			}
		})
	}
	sameAttrs := append(benchAttrs(), slog.String("tenant", "t1"), slog.String("request_id", "abc"))
	bare := slog.New(next)
	b.Run("bare-same-attrs", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			bare.LogAttrs(ctx, slog.LevelInfo, "http request", sameAttrs...)
		}
	})
}

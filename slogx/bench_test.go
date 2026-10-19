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

// benchHandlers returns every handler of the module over next. Sampling lets
// every record pass, so that the enabled benchmarks measure the full path.
func benchHandlers(next slog.Handler) map[string]slog.Handler {
	passAll := slogx.WithSampling(1<<62, 1, time.Second)
	keys := slogx.WithRedactKeys(sensitiveKeys...)
	composed := must(slogx.New(next, slogx.WithContextFunc(ridAttrs), keys, passAll))
	return map[string]slog.Handler{
		"level":          must(slogx.NewLevelHandler(next)),
		"context":        must(slogx.NewContextHandler(next, slogx.WithContextFunc(ridAttrs))),
		"redact":         must(slogx.NewRedactHandler(next, keys)),
		"sampling":       must(slogx.NewSamplingHandler(next, passAll)),
		"composed":       composed,
		"composed-group": composed.WithGroup("db"),
	}
}

// BenchmarkDisabled measures records below the threshold: no handler may
// allocate (FR-004, SC-003).
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

// BenchmarkEnabled measures enabled records. composed must take at most twice
// the time of bare-same-attrs, the output handler writing the same 7 attributes
// (SC-008).
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

// BenchmarkParallelSampling measures the sampler under contention, with most
// records dropped.
func BenchmarkParallelSampling(b *testing.B) {
	h := must(slogx.NewSamplingHandler(slog.NewJSONHandler(io.Discard, nil), slogx.WithSampling(10, 100, time.Second)))
	logger := slog.New(h)
	ctx := context.Background()
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			logger.LogAttrs(ctx, slog.LevelWarn, "db query failed")
		}
	})
}

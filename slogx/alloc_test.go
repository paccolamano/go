package slogx_test

import (
	"bytes"
	"context"
	"log/slog"
	"testing"

	"github.com/paccolamano/go/slogx"
	"github.com/paccolamano/go/slogx/slogxtest"
)

// assertNoAllocsWhenDisabled checks that logging a Debug record through h,
// which must disable Debug for each of ctxs, allocates nothing (FR-004).
func assertNoAllocsWhenDisabled(tb testing.TB, h slog.Handler, ctxs ...context.Context) {
	tb.Helper()
	logger := slog.New(h)
	for i, ctx := range ctxs {
		if logger.Enabled(ctx, slog.LevelDebug) {
			tb.Fatalf("context %d: Debug is enabled", i)
		}
		allocs := testing.AllocsPerRun(1000, func() {
			logger.LogAttrs(ctx, slog.LevelDebug, "m", slog.String("a", "b"))
		})
		if allocs != 0 {
			tb.Errorf("context %d: %v allocations per disabled record, want 0", i, allocs)
		}
	}
}

// TestAlloc_AllOptionsDisabled checks that a disabled record allocates nothing
// with a default level, two context values, sensitive keys and a fanout of two
// outputs (SC-006).
func TestAlloc_AllOptionsDisabled(t *testing.T) {
	h := must(slogx.New(slogx.Fanout(newJSON(&bytes.Buffer{}, nil), slogxtest.NewRecorder()),
		slogx.WithDefaultLevel(slog.LevelInfo),
		slogx.WithContextValue("request_id", ridFrom),
		slogx.WithContextValue("trace_id", ridFrom),
		slogx.WithRedactKeys(sensitiveKeys...),
	))
	assertNoAllocsWhenDisabled(t, h, requestContext(), slogx.WithLevel(requestContext(), slog.LevelWarn))
	assertNoAllocsWhenDisabled(t, h.WithGroup("g").WithAttrs([]slog.Attr{slog.Int("a", 1)}), requestContext())
}

package slogx_test

import (
	"context"
	"log/slog"
	"testing"
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

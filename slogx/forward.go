package slogx

import (
	"context"
	"log/slog"
)

// handle passes r to h. Every handler of this package forwards records through
// it, so that the next handler's error is returned unchanged from one place.
func handle(ctx context.Context, h slog.Handler, r slog.Record) error {
	return h.Handle(ctx, r) //nolint:wrapcheck // handlers return the next handler's error unchanged (FR-005)
}

package slogx

import (
	"io"
	"log/slog"
	"testing"
)

// TestRedact_SealedReturnsItself checks that a group opened under a sensitive
// key seals the handler: later groups and attributes are dropped without
// building new handlers.
func TestRedact_SealedReturnsItself(t *testing.T) {
	h := &redactHandler{next: slog.NewJSONHandler(io.Discard, nil), keys: []string{"token"}}
	sealed := h.WithGroup("token")
	if sealed.WithGroup("x") != sealed || sealed.WithAttrs([]slog.Attr{slog.Int("a", 1)}) != sealed {
		t.Error("a sealed handler does not return itself")
	}
}

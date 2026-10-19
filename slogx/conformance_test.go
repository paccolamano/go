package slogx_test

import (
	"bytes"
	"log/slog"
	"testing"
	"testing/slogtest"
)

// testConformance runs the standard handler conformance suite on the handler
// returned by wrap, around a JSON handler.
func testConformance(t *testing.T, wrap func(next slog.Handler) slog.Handler) {
	t.Helper()
	var buf *bytes.Buffer
	slogtest.Run(t, func(*testing.T) slog.Handler {
		buf = &bytes.Buffer{}
		return wrap(slog.NewJSONHandler(buf, nil))
	}, func(t *testing.T) map[string]any {
		t.Helper()
		ls := lines(t, buf)
		if len(ls) == 0 {
			t.Fatal("no output")
		}
		return decode(t, ls[len(ls)-1])
	})
}

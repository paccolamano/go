package slogx_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"
)

// must returns h, and panics, which fails the test, when the constructor
// returned an error: must(slogx.New(...)).
func must[H any](h H, err error) H {
	if err != nil {
		panic(err)
	}
	return h
}

// dropTime removes the top-level time, so that outputs can be compared byte for byte.
func dropTime(groups []string, a slog.Attr) slog.Attr {
	if len(groups) == 0 && a.Key == slog.TimeKey {
		return slog.Attr{}
	}
	return a
}

// newJSON returns a JSON handler without time, enabled from level (Info when nil).
func newJSON(w io.Writer, level slog.Leveler) slog.Handler {
	return slog.NewJSONHandler(w, &slog.HandlerOptions{Level: level, ReplaceAttr: dropTime})
}

// lines returns the non-empty lines written to buf.
func lines(tb testing.TB, buf *bytes.Buffer) []string {
	tb.Helper()
	var out []string
	for line := range strings.SplitSeq(buf.String(), "\n") {
		if line != "" {
			out = append(out, line)
		}
	}
	return out
}

// onlyLine returns the single line written to buf.
func onlyLine(tb testing.TB, buf *bytes.Buffer) string {
	tb.Helper()
	ls := lines(tb, buf)
	if len(ls) != 1 {
		tb.Fatalf("got %d lines, want 1: %q", len(ls), buf.String())
	}
	return ls[0]
}

// decode decodes one JSON line.
func decode(tb testing.TB, line string) map[string]any {
	tb.Helper()
	m := map[string]any{}
	if err := json.Unmarshal([]byte(line), &m); err != nil {
		tb.Fatalf("decoding %q: %v", line, err)
	}
	return m
}

type ridKey struct{}

// withRID stores a request identifier the way another package would.
func withRID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, ridKey{}, id)
}

// ridAttrs is an extraction function for the identifier stored by withRID.
func ridAttrs(ctx context.Context) []slog.Attr {
	if id, ok := ctx.Value(ridKey{}).(string); ok {
		return []slog.Attr{slog.String("request_id", id)}
	}
	return nil
}

// recordWith returns an Info record with message "m" and attrs.
func recordWith(attrs ...slog.Attr) slog.Record {
	r := slog.NewRecord(time.Time{}, slog.LevelInfo, "m", 0)
	r.AddAttrs(attrs...)
	return r
}

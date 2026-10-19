package slogx_test

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/paccolamano/go/slogx"
)

// contextLogger returns a logger that writes context attributes as JSON into buf.
func contextLogger(tb testing.TB, buf *bytes.Buffer, opts ...slogx.Option) *slog.Logger {
	tb.Helper()
	return slog.New(must(slogx.NewContextHandler(newJSON(buf, nil), opts...)))
}

func TestWith_DerivedContexts(t *testing.T) {
	var buf bytes.Buffer
	logger := contextLogger(t, &buf)
	parent := slogx.With(context.Background(), slog.String("user", "u1"))
	child := slogx.With(parent, slog.String("tenant", "t1"))

	logger.InfoContext(child, "child")
	if got, want := onlyLine(t, &buf), `{"level":"INFO","msg":"child","user":"u1","tenant":"t1"}`; got != want {
		t.Errorf("child record = %s, want %s", got, want)
	}
	buf.Reset()
	logger.InfoContext(parent, "parent")
	if got, want := onlyLine(t, &buf), `{"level":"INFO","msg":"parent","user":"u1"}`; got != want {
		t.Errorf("parent record = %s, want %s", got, want)
	}
}

func TestWith_LatestWins(t *testing.T) {
	tests := []struct {
		name string
		ctx  context.Context
	}{
		{"two calls", slogx.With(slogx.With(context.Background(), slog.String("tenant", "t1")), slog.String("tenant", "t2"))},
		{"one call", slogx.With(context.Background(), slog.String("tenant", "t1"), slog.String("tenant", "t2"))},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			contextLogger(t, &buf).InfoContext(tt.ctx, "m")
			line := onlyLine(t, &buf)
			if strings.Count(line, `"tenant"`) != 1 || !strings.Contains(line, `"tenant":"t2"`) {
				t.Errorf("record = %s, want tenant once with t2", line)
			}
		})
	}
}

func TestWith_KeyReplacedKeepsNewPosition(t *testing.T) {
	var buf bytes.Buffer
	ctx := slogx.With(context.Background(), slog.String("a", "1"), slog.String("b", "2"))
	ctx = slogx.With(ctx, slog.String("a", "3"))
	contextLogger(t, &buf).InfoContext(ctx, "m")
	if got, want := onlyLine(t, &buf), `{"level":"INFO","msg":"m","b":"2","a":"3"}`; got != want {
		t.Errorf("record = %s, want %s", got, want)
	}
}

func TestWith_Normalization(t *testing.T) {
	var buf bytes.Buffer
	ctx := slogx.With(context.Background(),
		slog.Attr{},
		slog.String("", "dropped"),
		slog.Group("", slog.String("a", "1"), slog.Group("", slog.Int("b", 2))),
		slog.Group("g", slog.String("c", "3")),
	)
	contextLogger(t, &buf).InfoContext(ctx, "m")
	if got, want := onlyLine(t, &buf), `{"level":"INFO","msg":"m","a":"1","b":2,"g":{"c":"3"}}`; got != want {
		t.Errorf("record = %s, want %s", got, want)
	}
}

func TestWith_NoAttrs(t *testing.T) {
	ctx := context.WithValue(context.Background(), ridKey{}, "x")
	if got := slogx.With(ctx); got != ctx {
		t.Error("With(ctx) did not return ctx")
	}
	if got := slogx.With(ctx, slog.Attr{}, slog.String("", "v")); got != ctx {
		t.Error("With(ctx, empty attributes) did not return ctx")
	}
}

func TestNil_ContextWith(t *testing.T) {
	var nilCtx context.Context
	ctx := slogx.With(nilCtx, slog.String("k", "v"))
	if ctx == nil {
		t.Fatal("With(nil, ...) returned nil")
	}
	var buf bytes.Buffer
	contextLogger(t, &buf).InfoContext(ctx, "m")
	if !strings.Contains(onlyLine(t, &buf), `"k":"v"`) {
		t.Errorf("record = %s, want k=v", buf.String())
	}
	if got := slogx.With(nilCtx); got == nil {
		t.Error("With(nil) returned nil")
	}
}

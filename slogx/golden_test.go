package slogx_test

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"testing"

	"github.com/paccolamano/go/slogx"
)

// goldenWrite rewrites testdata/v0.1.0.golden. Run it only on a checkout of
// slogx/v0.1.0, with this file copied there; never on the current code.
var goldenWrite = flag.Bool("golden.write", false, "write testdata/v0.1.0.golden; run only on a checkout of slogx/v0.1.0")

// This file must compile, unchanged, against slogx/v0.1.0: it uses only the
// identifiers that both versions export with the same signatures, and declares
// its own helpers.

type goldenRIDKey struct{}

// goldenUser resolves to a group that contains a token.
type goldenUser struct{ name, token string }

func (u goldenUser) LogValue() slog.Value {
	return slog.GroupValue(slog.String("name", u.name), slog.String("token", u.token))
}

// goldenRID is an extraction function for the identifier stored under
// goldenRIDKey.
func goldenRID(ctx context.Context) []slog.Attr {
	if id, ok := ctx.Value(goldenRIDKey{}).(string); ok {
		return []slog.Attr{slog.String("request_id", id)}
	}
	return nil
}

// goldenTenant returns keys that compete with other context attributes, an
// empty attribute and an inline group.
func goldenTenant(context.Context) []slog.Attr {
	return []slog.Attr{
		slog.String("tenant", "from-func"),
		slog.String("request_id", "from-func"),
		{},
		slog.Group("", slog.Int("inl", 1)),
	}
}

func goldenPanicFunc(context.Context) []slog.Attr { panic("extract") }

// goldenOutput logs every case through every configuration and output.
func goldenOutput() (string, error) {
	keys := []string{"authorization", "password", "token", "cookie"}
	configs := []struct {
		name string
		opts []slogx.Option
	}{
		{"none", nil},
		{"context", []slogx.Option{slogx.WithContextFunc(goldenRID)}},
		{"redact", []slogx.Option{slogx.WithRedactKeys(keys...)}},
		{"both", []slogx.Option{slogx.WithContextFunc(goldenRID), slogx.WithRedactKeys(keys...)}},
		{"many", []slogx.Option{
			slogx.WithContextFunc(goldenRID),
			slogx.WithContextFunc(goldenTenant),
			slogx.WithContextFunc(goldenPanicFunc),
			slogx.WithRedactKeys("token"),
			slogx.WithRedactKeys("Cookie", "secret"),
		}},
	}
	dropTime := func(groups []string, a slog.Attr) slog.Attr {
		if len(groups) == 0 && a.Key == slog.TimeKey {
			return slog.Attr{}
		}
		return a
	}
	outputs := []struct {
		name string
		new  func(*bytes.Buffer) slog.Handler
	}{
		{"json-info", func(b *bytes.Buffer) slog.Handler {
			return slog.NewJSONHandler(b, &slog.HandlerOptions{ReplaceAttr: dropTime})
		}},
		{"json-warn", func(b *bytes.Buffer) slog.Handler {
			return slog.NewJSONHandler(b, &slog.HandlerOptions{Level: slog.LevelWarn, ReplaceAttr: dropTime})
		}},
		{"text-debug", func(b *bytes.Buffer) slog.Handler {
			return slog.NewTextHandler(b, &slog.HandlerOptions{Level: slog.LevelDebug, ReplaceAttr: dropTime})
		}},
	}
	var nilCtx context.Context
	req := slogx.With(context.WithValue(context.Background(), goldenRIDKey{}, "abc"),
		slog.String("token", "s3cr3t"), slog.String("tenant", "t1"))
	cases := []struct {
		name string
		log  func(*slog.Logger)
	}{
		{"plain", func(l *slog.Logger) { l.InfoContext(req, "plain", "password", "hunter2", "n", 1) }},
		{"background", func(l *slog.Logger) { l.Info("background", "a", 1) }},
		{"nil context", func(l *slog.Logger) { l.Log(nilCtx, slog.LevelWarn, "nil context") }},
		{"levels", func(l *slog.Logger) {
			for _, level := range []slog.Level{slog.LevelDebug, slog.LevelInfo, slog.LevelWarn, slog.LevelError} {
				l.Log(req, level, "level")
			}
		}},
		{"context level debug", func(l *slog.Logger) {
			l.DebugContext(slogx.WithLevel(req, slog.LevelDebug), "debug for one request")
		}},
		{"context level error", func(l *slog.Logger) {
			ctx := slogx.WithLevel(req, slog.LevelError)
			l.WarnContext(ctx, "silenced")
			l.ErrorContext(ctx, "kept")
		}},
		{"groups", func(l *slog.Logger) {
			l.WithGroup("db").With("table", "orders", "Authorization", "x").WithGroup("q").InfoContext(req, "grouped", "rows", 3)
		}},
		{"logger attrs", func(l *slog.Logger) {
			l.With("service", "api", "cookie", "c", "tenant", "logger").InfoContext(req, "logger")
		}},
		{"duplicates", func(l *slog.Logger) { l.InfoContext(req, "dup", "request_id", "own", "tenant", "own") }},
		{"sealed group", func(l *slog.Logger) { l.WithGroup("Token").InfoContext(req, "sealed", "a", 1) }},
		{"valuers", func(l *slog.Logger) {
			l.InfoContext(req, "valuers", "user", goldenUser{"bob", "t0k"}, "pin", slogx.NewSecret(1234))
		}},
		{"nested", func(l *slog.Logger) {
			l.InfoContext(req, "nested", slog.Group("a", slog.Group("b",
				slog.String("password", "p"),
				slog.Group("", slog.Int("inline", 1)),
				slog.Group("empty"),
			)))
		}},
		{"error", func(l *slog.Logger) { l.ErrorContext(req, "failed", "err", errors.New("token=abc")) }},
	}
	var out strings.Builder
	for _, c := range configs {
		for _, o := range outputs {
			var buf bytes.Buffer
			h, err := slogx.New(o.new(&buf), c.opts...)
			if err != nil {
				return "", err
			}
			for _, cs := range cases {
				buf.Reset()
				cs.log(slog.New(h))
				fmt.Fprintf(&out, "## %s / %s / %s\n%s", c.name, o.name, cs.name, buf.String())
			}
		}
	}
	return out.String(), nil
}

// TestNew_SameOutputAsV010 compares the output of configurations without the
// options added in 0.2.0 with the output of slogx/v0.1.0 (FR-020, SC-005).
func TestNew_SameOutputAsV010(t *testing.T) {
	got, err := goldenOutput()
	if err != nil {
		t.Fatal(err)
	}
	const path = "testdata/v0.1.0.golden"
	if *goldenWrite {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got == string(want) {
		return
	}
	gotLines, wantLines := strings.Split(got, "\n"), strings.Split(string(want), "\n")
	for i := range min(len(gotLines), len(wantLines)) {
		if gotLines[i] != wantLines[i] {
			t.Fatalf("line %d differs:\ngot:  %s\nwant: %s", i+1, gotLines[i], wantLines[i])
		}
	}
	t.Fatalf("got %d lines, want %d", len(gotLines), len(wantLines))
}

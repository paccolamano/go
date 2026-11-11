package slogx_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/paccolamano/go/slogx"
)

// composedOptions are the options of the composed example.
func composedOptions() []slogx.Option {
	return []slogx.Option{slogx.WithContextFunc(ridAttrs), slogx.WithRedactKeys(sensitiveKeys...)}
}

func TestNew_Conformance(t *testing.T) {
	testConformance(t, func(next slog.Handler) slog.Handler {
		return must(slogx.New(next, slogx.WithContextFunc(alwaysAttr), slogx.WithRedactKeys("password")))
	})
}

func TestNew_ComposedScenario(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(must(slogx.New(newJSON(&buf, slog.LevelInfo), composedOptions()...)))
	ctx := slogx.WithLevel(slogx.With(withRID(context.Background(), "abc"), slog.String("token", "s3cr3t")), slog.LevelDebug)
	logger.DebugContext(ctx, "cache miss")
	if got, want := onlyLine(t, &buf), `{"level":"DEBUG","msg":"cache miss","token":"[REDACTED]","request_id":"abc"}`; got != want {
		t.Errorf("record = %s\nwant      %s", got, want)
	}
	if strings.Contains(buf.String(), "s3cr3t") {
		t.Errorf("secret leaked: %s", buf.String())
	}
}

func TestNew_NoOptions(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(must(slogx.New(newJSON(&buf, slog.LevelInfo))))
	ctx := slogx.WithLevel(slogx.With(context.Background(), slog.String("tenant", "t1")), slog.LevelDebug)
	logger.DebugContext(ctx, "m", "pin", slogx.NewSecret(1234))
	if got, want := onlyLine(t, &buf), `{"level":"DEBUG","msg":"m","tenant":"t1","pin":"[REDACTED]"}`; got != want {
		t.Errorf("record = %s, want %s", got, want)
	}
	buf.Reset()
	for range 1000 {
		logger.Warn("repeated")
	}
	if n := len(lines(t, &buf)); n != 1000 {
		t.Errorf("got %d records, want all 1000", n)
	}
}

func TestNew_InvalidOptions(t *testing.T) {
	next := newJSON(&bytes.Buffer{}, nil)
	tests := []struct {
		name string
		next slog.Handler
		opts []slogx.Option
	}{
		{"nil next", nil, nil},
		{"empty key", next, []slogx.Option{slogx.WithRedactKeys("")}},
		{"nil function", next, []slogx.Option{slogx.WithContextFunc(nil)}},
		{"nil option", next, []slogx.Option{nil}},
		{"nil default level", next, []slogx.Option{slogx.WithDefaultLevel(nil)}},
		{"empty context value key", next, []slogx.Option{slogx.WithContextValue("", ridFrom)}},
		{"fanout without default level", slogx.Fanout(next), nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h, err := slogx.New(tt.next, tt.opts...)
			if !errors.Is(err, slogx.ErrInvalidConfig) || h != nil {
				t.Errorf("New = %v, %v; want nil, ErrInvalidConfig", h, err)
			}
			// FR-017: no option is refused as inapplicable or as required.
			if err != nil && (strings.HasSuffix(err.Error(), ": does not apply") || strings.HasSuffix(err.Error(), ": required")) {
				t.Errorf("err = %q, an applicability error", err)
			}
		})
	}
}

// TestNew_AnyCombination checks that New accepts every option, in any order
// and any number of times (FR-017).
func TestNew_AnyCombination(t *testing.T) {
	get := func(context.Context) (string, bool) { return "v", true }
	every := []slogx.Option{
		slogx.WithDefaultLevel(slog.LevelInfo),
		slogx.WithContextFunc(ridAttrs),
		slogx.WithContextValue("k", get),
		slogx.WithRedactKeys("token"),
	}
	reversed := []slogx.Option{every[3], every[2], every[1], every[0]}
	twice := append(every[:len(every):len(every)],
		slogx.WithDefaultLevel(slog.LevelDebug),
		slogx.WithContextFunc(alwaysAttr),
		slogx.WithContextValue("k2", get),
		slogx.WithRedactKeys("password"),
	)
	out := newJSON(&bytes.Buffer{}, nil)
	tests := []struct {
		name string
		next slog.Handler
		opts []slogx.Option
	}{
		{"every option", out, every},
		{"reverse order", out, reversed},
		{"every option twice", out, twice},
		{"over a fanout", slogx.Fanout(out), every},
		{"no option", out, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if h, err := slogx.New(tt.next, tt.opts...); h == nil || err != nil {
				t.Errorf("New = %v, %v; want a handler", h, err)
			}
		})
	}
}

func TestNew_DerivedHandlers(t *testing.T) {
	h := must(slogx.New(newJSON(&bytes.Buffer{}, nil), composedOptions()...))
	if _, ok := h.WithAttrs([]slog.Attr{slog.Int("a", 1)}).(*slogx.Handler); !ok {
		t.Error("WithAttrs does not return a *Handler")
	}
	if _, ok := h.WithGroup("g").(*slogx.Handler); !ok {
		t.Error("WithGroup does not return a *Handler")
	}
	if h.WithGroup("") != slog.Handler(h) || h.WithAttrs(nil) != slog.Handler(h) {
		t.Error(`WithGroup("") or WithAttrs(nil) did not return the receiver`)
	}
}

func TestNew_ReturnsNextError(t *testing.T) {
	errNext := errors.New("next failed")
	h := must(slogx.New(&recordingHandler{err: errNext}, composedOptions()...))
	if err := h.Handle(context.Background(), recordWith(slog.String("a", "b"))); err != errNext { //nolint:errorlint // identity check (FR-005)
		t.Errorf("err = %v, want the next handler's error unchanged", err)
	}
}

func TestAlloc_NewDisabled(t *testing.T) {
	h := must(slogx.New(newJSON(&bytes.Buffer{}, nil), slogx.WithContextFunc(ridAttrs), slogx.WithRedactKeys("token")))
	assertNoAllocsWhenDisabled(t, h,
		requestContext(),
		slogx.WithLevel(requestContext(), slog.LevelWarn),
	)
}

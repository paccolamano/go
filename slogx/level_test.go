package slogx_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/paccolamano/go/slogx"
)

// levelLogger returns a logger whose output handler, writing into buf, is at
// threshold.
func levelLogger(buf *bytes.Buffer, threshold slog.Level) *slog.Logger {
	return slog.New(must(slogx.NewLevelHandler(newJSON(buf, threshold))))
}

func TestLevelHandler_Conformance(t *testing.T) {
	testConformance(t, func(next slog.Handler) slog.Handler {
		return must(slogx.NewLevelHandler(next))
	})
}

func TestLevelHandler_DebugForOneRequest(t *testing.T) {
	var buf bytes.Buffer
	logger := levelLogger(&buf, slog.LevelInfo)
	logger.DebugContext(context.Background(), "hidden")
	logger.DebugContext(slogx.WithLevel(context.Background(), slog.LevelDebug), "shown")
	if got, want := onlyLine(t, &buf), `{"level":"DEBUG","msg":"shown"}`; got != want {
		t.Errorf("records = %s, want only %s", buf.String(), want)
	}
}

func TestWithLevel_Nested(t *testing.T) {
	var buf bytes.Buffer
	logger := levelLogger(&buf, slog.LevelInfo)
	outer := slogx.WithLevel(context.Background(), slog.LevelDebug)
	inner := slogx.WithLevel(outer, slog.LevelWarn)
	logger.InfoContext(inner, "suppressed")
	logger.InfoContext(outer, "written")
	logger.WarnContext(inner, "warned")
	got := strings.Join(lines(t, &buf), "\n")
	want := `{"level":"INFO","msg":"written"}` + "\n" + `{"level":"WARN","msg":"warned"}`
	if got != want {
		t.Errorf("records = %q, want %q", got, want)
	}
}

func TestLevelHandler_NoContextMethod(t *testing.T) {
	var buf bytes.Buffer
	levelLogger(&buf, slog.LevelInfo).Debug("m")
	if buf.Len() != 0 {
		t.Errorf("record written without a context: %s", buf.String())
	}
}

func TestLevelHandler_Silence(t *testing.T) {
	var buf bytes.Buffer
	ctx := slogx.WithLevel(context.Background(), slog.LevelError)
	logger := levelLogger(&buf, slog.LevelInfo)
	logger.WarnContext(ctx, "silenced")
	logger.ErrorContext(ctx, "written")
	if got, want := onlyLine(t, &buf), `{"level":"ERROR","msg":"written"}`; got != want {
		t.Errorf("records = %s, want only %s", buf.String(), want)
	}
}

func TestLevelHandler_DefersWithoutLevel(t *testing.T) {
	h := must(slogx.NewLevelHandler(newJSON(&bytes.Buffer{}, slog.LevelWarn)))
	ctx := context.Background()
	if h.Enabled(ctx, slog.LevelInfo) || !h.Enabled(ctx, slog.LevelError) {
		t.Error("Enabled does not defer to the next handler without a context level")
	}
}

func TestLevelHandler_DerivedHandlersKeepTheLevel(t *testing.T) {
	var buf bytes.Buffer
	ctx := slogx.WithLevel(context.Background(), slog.LevelDebug)
	levelLogger(&buf, slog.LevelInfo).With("a", 1).WithGroup("g").DebugContext(ctx, "m", "b", 2)
	if got, want := onlyLine(t, &buf), `{"level":"DEBUG","msg":"m","a":1,"g":{"b":2}}`; got != want {
		t.Errorf("record = %s, want %s", got, want)
	}
}

func TestLevelHandler_ReturnsNextError(t *testing.T) {
	errNext := errors.New("next failed")
	h := must(slogx.NewLevelHandler(&recordingHandler{err: errNext}))
	if err := h.Handle(context.Background(), slog.NewRecord(time.Time{}, slog.LevelInfo, "m", 0)); err != errNext { //nolint:errorlint // identity check (FR-005)
		t.Errorf("err = %v, want the next handler's error unchanged", err)
	}
}

func TestNil_Level(t *testing.T) {
	var nilCtx context.Context
	if ctx := slogx.WithLevel(nilCtx, slog.LevelDebug); ctx == nil {
		t.Fatal("WithLevel(nil, ...) returned nil")
	}
	h := must(slogx.NewLevelHandler(newJSON(&bytes.Buffer{}, slog.LevelWarn)))
	if h.Enabled(nilCtx, slog.LevelInfo) || !h.Enabled(nilCtx, slog.LevelWarn) {
		t.Error("Enabled with a nil context does not defer to the next handler")
	}
}

func TestLevelHandler_Options(t *testing.T) {
	next := newJSON(&bytes.Buffer{}, nil)
	for name, build := range map[string]func() (*slogx.LevelHandler, error){
		"nil next": func() (*slogx.LevelHandler, error) { return slogx.NewLevelHandler(nil) },
		"inapplicable option": func() (*slogx.LevelHandler, error) {
			return slogx.NewLevelHandler(next, slogx.WithContextFunc(ridAttrs))
		},
	} {
		t.Run(name, func(t *testing.T) {
			h, err := build()
			if !errors.Is(err, slogx.ErrInvalidConfig) || h != nil {
				t.Errorf("NewLevelHandler = %v, %v; want nil, ErrInvalidConfig", h, err)
			}
		})
	}
	h := must(slogx.NewLevelHandler(next))
	if h.WithGroup("") != slog.Handler(h) {
		t.Error(`WithGroup("") did not return the receiver`)
	}
}

func TestAlloc_LevelHandlerDisabled(t *testing.T) {
	h := must(slogx.NewLevelHandler(newJSON(&bytes.Buffer{}, nil)))
	assertNoAllocsWhenDisabled(t, h, context.Background(), slogx.WithLevel(context.Background(), slog.LevelWarn))
}

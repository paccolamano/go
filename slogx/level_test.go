package slogx_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/paccolamano/go/slogx"
	"github.com/paccolamano/go/slogx/slogxtest"
)

// levelLogger returns a logger whose output handler, writing into buf, is at
// threshold.
func levelLogger(buf *bytes.Buffer, threshold slog.Level) *slog.Logger {
	return slog.New(must(slogx.New(newJSON(buf, threshold))))
}

func TestLevel_Conformance(t *testing.T) {
	testConformance(t, func(next slog.Handler) slog.Handler {
		return must(slogx.New(next))
	})
}

func TestLevel_DebugForOneRequest(t *testing.T) {
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

func TestLevel_NoContextMethod(t *testing.T) {
	var buf bytes.Buffer
	levelLogger(&buf, slog.LevelInfo).Debug("m")
	if buf.Len() != 0 {
		t.Errorf("record written without a context: %s", buf.String())
	}
}

func TestLevel_Silence(t *testing.T) {
	var buf bytes.Buffer
	ctx := slogx.WithLevel(context.Background(), slog.LevelError)
	logger := levelLogger(&buf, slog.LevelInfo)
	logger.WarnContext(ctx, "silenced")
	logger.ErrorContext(ctx, "written")
	if got, want := onlyLine(t, &buf), `{"level":"ERROR","msg":"written"}`; got != want {
		t.Errorf("records = %s, want only %s", buf.String(), want)
	}
}

func TestLevel_DefersWithoutLevel(t *testing.T) {
	h := must(slogx.New(newJSON(&bytes.Buffer{}, slog.LevelWarn)))
	ctx := context.Background()
	if h.Enabled(ctx, slog.LevelInfo) || !h.Enabled(ctx, slog.LevelError) {
		t.Error("Enabled does not defer to the next handler without a context level")
	}
}

func TestLevel_DerivedHandlersKeepTheLevel(t *testing.T) {
	var buf bytes.Buffer
	ctx := slogx.WithLevel(context.Background(), slog.LevelDebug)
	levelLogger(&buf, slog.LevelInfo).With("a", 1).WithGroup("g").DebugContext(ctx, "m", "b", 2)
	if got, want := onlyLine(t, &buf), `{"level":"DEBUG","msg":"m","a":1,"g":{"b":2}}`; got != want {
		t.Errorf("record = %s, want %s", got, want)
	}
}

func TestLevel_ReturnsNextError(t *testing.T) {
	errNext := errors.New("next failed")
	h := must(slogx.New(&recordingHandler{err: errNext}))
	if err := h.Handle(context.Background(), slog.NewRecord(time.Time{}, slog.LevelInfo, "m", 0)); err != errNext { //nolint:errorlint // identity check (FR-005)
		t.Errorf("err = %v, want the next handler's error unchanged", err)
	}
}

func TestNil_Level(t *testing.T) {
	var nilCtx context.Context
	if ctx := slogx.WithLevel(nilCtx, slog.LevelDebug); ctx == nil {
		t.Fatal("WithLevel(nil, ...) returned nil")
	}
	h := must(slogx.New(newJSON(&bytes.Buffer{}, slog.LevelWarn)))
	if h.Enabled(nilCtx, slog.LevelInfo) || !h.Enabled(nilCtx, slog.LevelWarn) {
		t.Error("Enabled with a nil context does not defer to the next handler")
	}
}

func TestLevel_Options(t *testing.T) {
	next := newJSON(&bytes.Buffer{}, nil)
	for name, build := range map[string]func() (*slogx.Handler, error){
		"nil next": func() (*slogx.Handler, error) { return slogx.New(nil) },
	} {
		t.Run(name, func(t *testing.T) {
			h, err := build()
			if !errors.Is(err, slogx.ErrInvalidConfig) || h != nil {
				t.Errorf("New = %v, %v; want nil, ErrInvalidConfig", h, err)
			}
		})
	}
	h := must(slogx.New(next))
	if h.WithGroup("") != slog.Handler(h) {
		t.Error(`WithGroup("") did not return the receiver`)
	}
}

func TestAlloc_LevelDisabled(t *testing.T) {
	h := must(slogx.New(newJSON(&bytes.Buffer{}, nil)))
	assertNoAllocsWhenDisabled(t, h, context.Background(), slogx.WithLevel(context.Background(), slog.LevelWarn))
}

// defaultLogger returns a logger over next with the default level.
func defaultLogger(next slog.Handler, level slog.Leveler) *slog.Logger {
	return slog.New(must(slogx.New(next, slogx.WithDefaultLevel(level))))
}

func TestDefaultLevel_OverAllLevelsOutput(t *testing.T) {
	ctx := context.Background()
	rec := slogxtest.NewRecorder()
	if !rec.Enabled(ctx, slog.LevelDebug) {
		t.Fatal("the recorder does not enable Debug")
	}
	logger := defaultLogger(rec, slog.LevelInfo)
	logger.DebugContext(ctx, "debug")
	logger.InfoContext(ctx, "info")
	if r, _ := rec.Last(); rec.Len() != 1 || r.Message != "info" {
		t.Errorf("records = %+v, want only info", rec.Records())
	}
}

func TestDefaultLevel_ContextLevelWins(t *testing.T) {
	tests := []struct {
		name        string
		ctxLevel    slog.Level
		recordLevel slog.Level
		written     bool
	}{
		{"debug request, debug record", slog.LevelDebug, slog.LevelDebug, true},
		{"warn request, info record", slog.LevelWarn, slog.LevelInfo, false},
		{"warn request, warn record", slog.LevelWarn, slog.LevelWarn, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := slogxtest.NewRecorder()
			ctx := slogx.WithLevel(context.Background(), tt.ctxLevel)
			defaultLogger(rec, slog.LevelInfo).Log(ctx, tt.recordLevel, "m")
			if got := rec.Len() == 1; got != tt.written {
				t.Errorf("written = %v, want %v", got, tt.written)
			}
		})
	}
}

func TestDefaultLevel_OutputThresholdIgnored(t *testing.T) {
	var buf bytes.Buffer
	defaultLogger(newJSON(&buf, nil), slog.LevelDebug).DebugContext(context.Background(), "m")
	if got, want := onlyLine(t, &buf), `{"level":"DEBUG","msg":"m"}`; got != want {
		t.Errorf("record = %s, want %s", got, want)
	}
}

func TestDefaultLevel_LevelVar(t *testing.T) {
	var level slog.LevelVar
	rec := slogxtest.NewRecorder()
	logger := defaultLogger(rec, &level)
	logger.Debug("hidden")
	if rec.Len() != 0 {
		t.Fatalf("Debug written at Info: %+v", rec.Records())
	}
	level.Set(slog.LevelDebug)
	logger.Debug("shown")
	if r, _ := rec.Last(); rec.Len() != 1 || r.Message != "shown" {
		t.Errorf("records = %+v, want only shown", rec.Records())
	}
}

func TestDefaultLevel_None(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(must(slogx.New(newJSON(&buf, slog.LevelWarn))))
	logger.Info("hidden")
	logger.Warn("shown")
	if got, want := onlyLine(t, &buf), `{"level":"WARN","msg":"shown"}`; got != want {
		t.Errorf("record = %s, want %s", got, want)
	}
	rec := slogxtest.NewRecorder()
	slog.New(must(slogx.New(rec))).Debug("m")
	if rec.Len() != 1 {
		t.Error("the next handler does not decide without a default level")
	}
}

func TestDefaultLevel_Invalid(t *testing.T) {
	var nilVar *slog.LevelVar
	tests := []struct {
		name string
		opts []slogx.Option
	}{
		{"nil", []slogx.Option{slogx.WithDefaultLevel(nil)}},
		{"nil level variable", []slogx.Option{slogx.WithDefaultLevel(nilVar)}},
		{"nil, then valid", []slogx.Option{slogx.WithDefaultLevel(nil), slogx.WithDefaultLevel(slog.LevelInfo)}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h, err := slogx.New(newJSON(&bytes.Buffer{}, nil), tt.opts...)
			if h != nil || !errors.Is(err, slogx.ErrInvalidConfig) || !strings.HasSuffix(err.Error(), "WithDefaultLevel: must not be nil") {
				t.Errorf("New = %v, %v; want nil, ErrInvalidConfig for WithDefaultLevel", h, err)
			}
		})
	}
}

func TestDefaultLevel_LastWins(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(must(slogx.New(newJSON(&buf, nil),
		slogx.WithDefaultLevel(slog.LevelWarn),
		slogx.WithDefaultLevel(slog.LevelDebug),
	)))
	logger.Debug("m")
	if got, want := onlyLine(t, &buf), `{"level":"DEBUG","msg":"m"}`; got != want {
		t.Errorf("record = %s, want %s", got, want)
	}
}

func TestDefaultLevel_NilContext(t *testing.T) {
	var nilCtx context.Context
	h := must(slogx.New(slogxtest.NewRecorder(), slogx.WithDefaultLevel(slog.LevelInfo)))
	if h.Enabled(nilCtx, slog.LevelDebug) || !h.Enabled(nilCtx, slog.LevelInfo) {
		t.Error("Enabled with a nil context does not apply the default level")
	}
}

func TestDefaultLevel_Derived(t *testing.T) {
	rec := slogxtest.NewRecorder()
	logger := defaultLogger(rec, slog.LevelInfo).With("a", 1).WithGroup("g")
	logger.Debug("hidden")
	logger.Info("shown")
	if r, _ := rec.Last(); rec.Len() != 1 || r.Message != "shown" {
		t.Errorf("records = %+v, want only shown", rec.Records())
	}
}

// TestDefaultLevel_Concurrent changes a level variable while 50 goroutines log:
// every record logged after the change follows the new level (SC-002).
func TestDefaultLevel_Concurrent(t *testing.T) {
	var level slog.LevelVar
	rec := slogxtest.NewRecorder()
	logger := defaultLogger(rec, &level)
	const workers, perWorker = 50, 20
	phase := func(msg string) {
		var wg sync.WaitGroup
		for range workers {
			wg.Go(func() {
				for range perWorker {
					logger.Debug(msg)
				}
			})
		}
		wg.Wait()
	}
	phase("before")
	if rec.Len() != 0 {
		t.Fatalf("got %d Debug records at Info", rec.Len())
	}
	var wg sync.WaitGroup
	wg.Go(func() { level.Set(slog.LevelDebug) })
	for range workers {
		wg.Go(func() { logger.Debug("during") })
	}
	wg.Wait()
	before := rec.Len()
	phase("after")
	if got := rec.Len() - before; got != workers*perWorker {
		t.Errorf("got %d records after the change, want %d", got, workers*perWorker)
	}
}

func TestDefaultLevel_Conformance(t *testing.T) {
	testConformance(t, func(next slog.Handler) slog.Handler {
		return must(slogx.New(next, slogx.WithDefaultLevel(slog.LevelDebug)))
	})
}

func TestAlloc_DefaultLevelDisabled(t *testing.T) {
	var level slog.LevelVar
	for name, lv := range map[string]slog.Leveler{"fixed": slog.LevelInfo, "variable": &level} {
		t.Run(name, func(t *testing.T) {
			h := must(slogx.New(slogxtest.NewRecorder(),
				slogx.WithDefaultLevel(lv),
				slogx.WithContextFunc(ridAttrs),
				slogx.WithRedactKeys("token"),
			))
			assertNoAllocsWhenDisabled(t, h, requestContext(), slogx.WithLevel(requestContext(), slog.LevelWarn))
		})
	}
}

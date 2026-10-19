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

// composedOptions are the options of the request's composed example, without
// sampling.
func composedOptions() []slogx.Option {
	return []slogx.Option{slogx.WithContextFunc(ridAttrs), slogx.WithRedactKeys(sensitiveKeys...)}
}

// manualChain builds, with the standalone constructors, the chain that New
// builds for composedOptions.
func manualChain(next slog.Handler) slog.Handler {
	r := must(slogx.NewRedactHandler(next, slogx.WithRedactKeys(sensitiveKeys...)))
	c := must(slogx.NewContextHandler(r, slogx.WithContextFunc(ridAttrs)))
	return must(slogx.NewLevelHandler(c))
}

// composedCases log the same records through any handler.
func composedCases() []func(*slog.Logger) {
	request := slogx.With(withRID(context.Background(), "abc"), slog.String("token", "s3cr3t"), slog.String("tenant", "t1"))
	debug := slogx.WithLevel(request, slog.LevelDebug)
	return []func(*slog.Logger){
		func(l *slog.Logger) { l.InfoContext(request, "plain", "password", "hunter2") },
		func(l *slog.Logger) {
			l.WithGroup("db").With("table", "orders").InfoContext(request, "grouped", "rows", 3)
		},
		func(l *slog.Logger) {
			l.With("service", "api", "cookie", "c").InfoContext(request, "logger attributes")
		},
		func(l *slog.Logger) { l.DebugContext(debug, "context level") },
		func(l *slog.Logger) { l.DebugContext(request, "not written") },
		func(l *slog.Logger) { l.InfoContext(request, "duplicate", "request_id", "explicit") },
		func(l *slog.Logger) { l.WithGroup("Authorization").InfoContext(request, "sealed", "a", 1) },
		func(l *slog.Logger) { l.Info("no context", "user", userValue{name: "bob", token: "s3cr3t"}) },
	}
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

func TestCompose_EquivalentToManual(t *testing.T) {
	var composed, manual bytes.Buffer
	composedLogger := slog.New(must(slogx.New(newJSON(&composed, slog.LevelInfo), composedOptions()...)))
	manualLogger := slog.New(manualChain(newJSON(&manual, slog.LevelInfo)))
	for _, log := range composedCases() {
		log(composedLogger)
		log(manualLogger)
	}
	if composed.String() != manual.String() {
		t.Errorf("composed output:\n%s\nmanual output:\n%s", composed.String(), manual.String())
	}
	if n := len(lines(t, &composed)); n != 7 {
		t.Errorf("got %d records, want 7", n)
	}
	if strings.Contains(composed.String(), "s3cr3t") || strings.Contains(composed.String(), "hunter2") {
		t.Errorf("secret leaked: %s", composed.String())
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
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h, err := slogx.New(tt.next, tt.opts...)
			if !errors.Is(err, slogx.ErrInvalidConfig) || h != nil {
				t.Errorf("New = %v, %v; want nil, ErrInvalidConfig", h, err)
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

package slogx_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/paccolamano/go/slogx"
)

var sensitiveKeys = []string{"authorization", "password", "token", "cookie"}

// redactLogger returns a logger that writes into buf through a RedactHandler
// with the sensitive keys.
func redactLogger(buf *bytes.Buffer) *slog.Logger {
	return slog.New(must(slogx.NewRedactHandler(newJSON(buf, nil), slogx.WithRedactKeys(sensitiveKeys...))))
}

// userValue resolves to a group that contains a token.
type userValue struct{ name, token string }

func (u userValue) LogValue() slog.Value {
	return slog.GroupValue(slog.String("name", u.name), slog.String("token", u.token))
}

// countingValuer counts how many times its LogValue method runs.
type countingValuer struct{ calls *atomic.Int32 }

func (v countingValuer) LogValue() slog.Value {
	v.calls.Add(1)
	return slog.StringValue("hunter2")
}

func TestRedactHandler_Conformance(t *testing.T) {
	testConformance(t, func(next slog.Handler) slog.Handler {
		return must(slogx.NewRedactHandler(next, slogx.WithRedactKeys("password")))
	})
}

func TestRedactHandler_Keys(t *testing.T) {
	var buf bytes.Buffer
	redactLogger(&buf).Info("login", "user", "bob", "password", "hunter2")
	if got, want := onlyLine(t, &buf), `{"level":"INFO","msg":"login","user":"bob","password":"[REDACTED]"}`; got != want {
		t.Errorf("record = %s, want %s", got, want)
	}
}

func TestRedactHandler_CaseInsensitiveInGroups(t *testing.T) {
	tests := []struct {
		key  string
		want string
	}{
		{"Authorization", `{"level":"INFO","msg":"http request","header":{"Authorization":"[REDACTED]","Accept":"*/*"}}`},
		{"AUTHORIZATION", `{"level":"INFO","msg":"http request","header":{"AUTHORIZATION":"[REDACTED]","Accept":"*/*"}}`},
		{"authorization", `{"level":"INFO","msg":"http request","header":{"authorization":"[REDACTED]","Accept":"*/*"}}`},
	}
	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			var buf bytes.Buffer
			redactLogger(&buf).Info("http request", slog.Group("header", tt.key, "Bearer x", "Accept", "*/*"))
			if got := onlyLine(t, &buf); got != tt.want {
				t.Errorf("record = %s\nwant      %s", got, tt.want)
			}
		})
	}
}

func TestRedactHandler_LogValuerGroup(t *testing.T) {
	var buf bytes.Buffer
	var calls atomic.Int32
	redactLogger(&buf).Info("u", "user", userValue{name: "bob", token: "s3cr3t"}, "token", countingValuer{&calls})
	if got, want := onlyLine(t, &buf), `{"level":"INFO","msg":"u","user":{"name":"bob","token":"[REDACTED]"},"token":"[REDACTED]"}`; got != want {
		t.Errorf("record = %s\nwant      %s", got, want)
	}
	if n := calls.Load(); n != 0 {
		t.Errorf("LogValue of a sensitive attribute called %d times, want 0", n)
	}
}

func TestRedactHandler_LogValuerResolvedOnce(t *testing.T) {
	var buf bytes.Buffer
	var calls atomic.Int32
	redactLogger(&buf).Info("m", "v", countingValuer{&calls})
	if n := calls.Load(); n != 1 {
		t.Errorf("LogValue called %d times, want 1", n)
	}
}

func TestRedactHandler_WithAttrs(t *testing.T) {
	var buf bytes.Buffer
	logger := redactLogger(&buf).With("token", "s3cr3t")
	logger.Info("first")
	logger.WithGroup("g").Info("second", "a", 1)
	logger.With(slog.Group("creds", "Cookie", "s3cr3t")).Info("third")
	want := []string{
		`{"level":"INFO","msg":"first","token":"[REDACTED]"}`,
		`{"level":"INFO","msg":"second","token":"[REDACTED]","g":{"a":1}}`,
		`{"level":"INFO","msg":"third","token":"[REDACTED]","creds":{"Cookie":"[REDACTED]"}}`,
	}
	if got := lines(t, &buf); strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("records = %q\nwant      %q", got, want)
	}
	if strings.Contains(buf.String(), "s3cr3t") {
		t.Errorf("secret leaked: %s", buf.String())
	}
}

func TestRedactHandler_WholeKeyOnly(t *testing.T) {
	var buf bytes.Buffer
	redactLogger(&buf).Info("m", "password_hint", "x", "my_token", "y")
	if got, want := onlyLine(t, &buf), `{"level":"INFO","msg":"m","password_hint":"x","my_token":"y"}`; got != want {
		t.Errorf("record = %s, want %s", got, want)
	}
}

func TestRedactHandler_GroupKey(t *testing.T) {
	tests := []struct {
		name string
		log  func(*slog.Logger)
		want string
	}{
		{
			name: "group attribute",
			log:  func(l *slog.Logger) { l.Info("m", slog.Group("password", "a", 1)) },
			want: `{"level":"INFO","msg":"m","password":"[REDACTED]"}`,
		},
		{
			name: "group opened on the logger",
			log:  func(l *slog.Logger) { l.WithGroup("Password").With("x", 1).Info("m", "a", 2) },
			want: `{"level":"INFO","msg":"m","Password":"[REDACTED]"}`,
		},
		{
			name: "nested group opened on the logger",
			log:  func(l *slog.Logger) { l.WithGroup("g").WithGroup("TOKEN").WithGroup("h").Info("m", "a", 1) },
			want: `{"level":"INFO","msg":"m","g":{"TOKEN":"[REDACTED]"}}`,
		},
		{
			name: "empty group opened on the logger",
			log:  func(l *slog.Logger) { l.WithGroup("token").Info("m") },
			want: `{"level":"INFO","msg":"m","token":"[REDACTED]"}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			tt.log(redactLogger(&buf))
			if got := onlyLine(t, &buf); got != tt.want {
				t.Errorf("record = %s\nwant      %s", got, tt.want)
			}
		})
	}
}

func TestRedactHandler_Untouched(t *testing.T) {
	log := func(l *slog.Logger) {
		l.With("service", "api").WithGroup("req").Info("token hunter2",
			"method", "GET",
			slog.Group("header", "Accept", "*/*", slog.Group("x", "y", 1)),
			"user", slog.GroupValue(slog.String("name", "bob")),
		)
	}
	var plain, redacted bytes.Buffer
	log(slog.New(newJSON(&plain, nil)))
	log(redactLogger(&redacted))
	if plain.String() != redacted.String() {
		t.Errorf("output changed:\n%s\nwant\n%s", redacted.String(), plain.String())
	}
}

func TestRedactHandler_ReturnsNextError(t *testing.T) {
	errNext := errors.New("next failed")
	h := must(slogx.NewRedactHandler(&recordingHandler{err: errNext}, slogx.WithRedactKeys("token")))
	logger := slog.New(h)
	for _, l := range []*slog.Logger{logger, logger.WithGroup("token")} {
		err := l.Handler().Handle(context.Background(), recordWith(slog.String("token", "x")))
		if err != errNext { //nolint:errorlint // identity check (FR-005)
			t.Errorf("err = %v, want the next handler's error unchanged", err)
		}
	}
}

func TestRedactHandler_Options(t *testing.T) {
	next := newJSON(&bytes.Buffer{}, nil)
	tests := []struct {
		name string
		next slog.Handler
		opts []slogx.Option
	}{
		{"nil next", nil, []slogx.Option{slogx.WithRedactKeys("x")}},
		{"missing required option", next, nil},
		{"no key", next, []slogx.Option{slogx.WithRedactKeys()}},
		{"blank key", next, []slogx.Option{slogx.WithRedactKeys(" ")}},
		{"inapplicable option", next, []slogx.Option{slogx.WithRedactKeys("x"), slogx.WithContextFunc(ridAttrs)}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h, err := slogx.NewRedactHandler(tt.next, tt.opts...)
			if !errors.Is(err, slogx.ErrInvalidConfig) || h != nil {
				t.Errorf("NewRedactHandler = %v, %v; want nil, ErrInvalidConfig", h, err)
			}
		})
	}
}

func TestRedactHandler_Derived(t *testing.T) {
	h := must(slogx.NewRedactHandler(newJSON(&bytes.Buffer{}, nil), slogx.WithRedactKeys("token")))
	if h.WithGroup("") != slog.Handler(h) || h.WithAttrs(nil) != slog.Handler(h) {
		t.Error(`WithGroup("") or WithAttrs(nil) did not return the receiver`)
	}
	if !h.Enabled(context.Background(), slog.LevelInfo) || h.Enabled(context.Background(), slog.LevelDebug) {
		t.Error("Enabled does not forward to the next handler")
	}
	sealed := h.WithGroup("token")
	if sealed.WithGroup("x") != sealed || sealed.WithAttrs([]slog.Attr{slog.Int("a", 1)}) != sealed {
		t.Error("a sealed handler does not return itself")
	}
}

func TestAlloc_RedactHandlerDisabled(t *testing.T) {
	h := must(slogx.NewRedactHandler(newJSON(&bytes.Buffer{}, nil), slogx.WithRedactKeys(sensitiveKeys...)))
	assertNoAllocsWhenDisabled(t, h, context.Background())
}

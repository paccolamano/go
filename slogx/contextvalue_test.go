package slogx_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/paccolamano/go/slogx"
	"github.com/paccolamano/go/slogx/slogxtest"
)

// valueLine logs through a handler with opts, writing JSON, and returns the
// only line written.
func valueLine(tb testing.TB, log func(*slog.Logger), opts ...slogx.Option) string {
	tb.Helper()
	var buf bytes.Buffer
	log(slog.New(must(slogx.New(newJSON(&buf, nil), opts...))))
	return onlyLine(tb, &buf)
}

func TestContextValue_PresentAndAbsent(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(must(slogx.New(newJSON(&buf, nil), slogx.WithContextValue("request_id", ridFrom))))
	logger.InfoContext(withRID(context.Background(), "abc"), "with id")
	logger.InfoContext(context.Background(), "without id")
	want := `{"level":"INFO","msg":"with id","request_id":"abc"}` + "\n" + `{"level":"INFO","msg":"without id"}`
	if got := strings.Join(lines(t, &buf), "\n"); got != want {
		t.Errorf("records = %q, want %q", got, want)
	}
}

func TestContextValue_TakenKey(t *testing.T) {
	ctx := withRID(context.Background(), "abc")
	fromFunc := func(context.Context) []slog.Attr { return []slog.Attr{slog.String("request_id", "func")} }
	first := func(context.Context) (string, bool) { return "first", true }
	tests := []struct {
		name      string
		opts      []slogx.Option
		log       func(*slog.Logger)
		want      string
		wantCalls int
	}{
		{
			name:      "record attribute",
			log:       func(l *slog.Logger) { l.InfoContext(ctx, "m", "request_id", "own") },
			want:      `{"level":"INFO","msg":"m","request_id":"own"}`,
			wantCalls: 0,
		},
		{
			name:      "attached attribute",
			log:       func(l *slog.Logger) { l.InfoContext(slogx.With(ctx, slog.String("request_id", "att")), "m") },
			want:      `{"level":"INFO","msg":"m","request_id":"att"}`,
			wantCalls: 0,
		},
		{
			name:      "logger attribute",
			log:       func(l *slog.Logger) { l.With("request_id", "logger").InfoContext(ctx, "m") },
			want:      `{"level":"INFO","msg":"m","request_id":"logger"}`,
			wantCalls: 0,
		},
		{
			name:      "earlier extraction function",
			opts:      []slogx.Option{slogx.WithContextFunc(fromFunc)},
			log:       func(l *slog.Logger) { l.InfoContext(ctx, "m") },
			want:      `{"level":"INFO","msg":"m","request_id":"func"}`,
			wantCalls: 0,
		},
		{
			name:      "earlier context value",
			opts:      []slogx.Option{slogx.WithContextValue("request_id", first)},
			log:       func(l *slog.Logger) { l.InfoContext(ctx, "m") },
			want:      `{"level":"INFO","msg":"m","request_id":"first"}`,
			wantCalls: 0,
		},
		{
			name:      "record attribute inside a group",
			log:       func(l *slog.Logger) { l.WithGroup("g").InfoContext(ctx, "m", "request_id", "own") },
			want:      `{"level":"INFO","msg":"m","request_id":"abc","g":{"request_id":"own"}}`,
			wantCalls: 1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			calls := 0
			get := func(ctx context.Context) (string, bool) {
				calls++
				return ridFrom(ctx)
			}
			opts := append(tt.opts[:len(tt.opts):len(tt.opts)], slogx.WithContextValue("request_id", get))
			if got := valueLine(t, tt.log, opts...); got != tt.want {
				t.Errorf("record = %s, want %s", got, tt.want)
			}
			if calls != tt.wantCalls {
				t.Errorf("get called %d times, want %d", calls, tt.wantCalls)
			}
		})
	}
}

func TestContextValue_FirstConfiguredWins(t *testing.T) {
	ctx := withRID(context.Background(), "abc")
	fromFunc := func(context.Context) []slog.Attr { return []slog.Attr{slog.String("request_id", "func")} }
	log := func(l *slog.Logger) { l.InfoContext(ctx, "m") }
	if got, want := valueLine(t, log, slogx.WithContextValue("request_id", ridFrom), slogx.WithContextFunc(fromFunc)),
		`{"level":"INFO","msg":"m","request_id":"abc"}`; got != want {
		t.Errorf("context value first: record = %s, want %s", got, want)
	}
	if got, want := valueLine(t, log, slogx.WithContextFunc(fromFunc), slogx.WithContextValue("request_id", ridFrom)),
		`{"level":"INFO","msg":"m","request_id":"func"}`; got != want {
		t.Errorf("extraction function first: record = %s, want %s", got, want)
	}
}

func TestContextValue_Redaction(t *testing.T) {
	log := func(l *slog.Logger) { l.InfoContext(context.Background(), "m") }
	pin := slogx.WithContextValue("pin", func(context.Context) (slogx.Secret[int], bool) { return slogx.NewSecret(1234), true })
	token := slogx.WithContextValue("token", func(context.Context) (string, bool) { return "s3cr3t", true })
	if got, want := valueLine(t, log, pin, token, slogx.WithRedactKeys("token")),
		`{"level":"INFO","msg":"m","pin":"[REDACTED]","token":"[REDACTED]"}`; got != want {
		t.Errorf("with the sensitive key: record = %s, want %s", got, want)
	}
	if got, want := valueLine(t, log, pin, token),
		`{"level":"INFO","msg":"m","pin":"[REDACTED]","token":"s3cr3t"}`; got != want {
		t.Errorf("without sensitive keys: record = %s, want %s", got, want)
	}
}

func TestContextValue_LogValuer(t *testing.T) {
	log := func(l *slog.Logger) { l.InfoContext(context.Background(), "m") }
	user := slogx.WithContextValue("user", func(context.Context) (userValue, bool) {
		return userValue{name: "bob", token: "t0k"}, true
	})
	if got, want := valueLine(t, log, user, slogx.WithRedactKeys(sensitiveKeys...)),
		`{"level":"INFO","msg":"m","user":{"name":"bob","token":"[REDACTED]"}}`; got != want {
		t.Errorf("with sensitive keys: record = %s, want %s", got, want)
	}
	if got, want := valueLine(t, log, user),
		`{"level":"INFO","msg":"m","user":{"name":"bob","token":"t0k"}}`; got != want {
		t.Errorf("without sensitive keys: record = %s, want %s", got, want)
	}
}

func TestContextValue_Panic(t *testing.T) {
	log := func(l *slog.Logger) { l.InfoContext(withRID(context.Background(), "abc"), "m") }
	got := valueLine(t, log,
		slogx.WithContextValue("boom", func(context.Context) (int, bool) { panic("bad") }),
		slogx.WithContextValue("request_id", ridFrom),
	)
	if want := `{"level":"INFO","msg":"m","!CONTEXTFUNC":"panic: bad","request_id":"abc"}`; got != want {
		t.Errorf("record = %s, want %s", got, want)
	}
}

func TestContextValue_Invalid(t *testing.T) {
	tests := []struct {
		name string
		opt  slogx.Option
		want string
	}{
		{"empty key", slogx.WithContextValue("", ridFrom), "WithContextValue: empty key"},
		{"blank key", slogx.WithContextValue(" \t", ridFrom), "WithContextValue: empty key"},
		{"nil function", slogx.WithContextValue[string]("request_id", nil), "WithContextValue: function must not be nil"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h, err := slogx.New(newJSON(&bytes.Buffer{}, nil), tt.opt)
			if h != nil || !errors.Is(err, slogx.ErrInvalidConfig) || !strings.HasSuffix(err.Error(), tt.want) {
				t.Errorf("New = %v, %v; want nil, ErrInvalidConfig ending with %q", h, err, tt.want)
			}
		})
	}
}

func TestContextValue_Disabled(t *testing.T) {
	calls := 0
	get := func(ctx context.Context) (string, bool) {
		calls++
		return ridFrom(ctx)
	}
	var buf bytes.Buffer
	h := must(slogx.New(newJSON(&buf, nil), slogx.WithContextValue("request_id", get)))
	slog.New(h).DebugContext(withRID(context.Background(), "abc"), "m")
	if buf.Len() != 0 || calls != 0 {
		t.Fatalf("disabled record: output %q, %d calls; want nothing", buf.String(), calls)
	}
	assertNoAllocsWhenDisabled(t, h, requestContext(), withRID(context.Background(), "abc"))
	if calls != 0 {
		t.Errorf("get called %d times for disabled records", calls)
	}
}

func TestContextValue_ZeroValue(t *testing.T) {
	got := valueLine(t, func(l *slog.Logger) { l.InfoContext(context.Background(), "m") },
		slogx.WithContextValue("count", func(context.Context) (int, bool) { return 0, true }),
		slogx.WithContextValue("name", func(context.Context) (string, bool) { return "", true }),
	)
	if want := `{"level":"INFO","msg":"m","count":0,"name":""}`; got != want {
		t.Errorf("record = %s, want %s", got, want)
	}
}

func TestContextValue_Group(t *testing.T) {
	got := valueLine(t, func(l *slog.Logger) {
		l.WithGroup("db").InfoContext(withRID(context.Background(), "abc"), "q", "rows", 3)
	}, slogx.WithContextValue("request_id", ridFrom))
	if want := `{"level":"INFO","msg":"q","request_id":"abc","db":{"rows":3}}`; got != want {
		t.Errorf("record = %s, want %s", got, want)
	}
}

// TestContextValue_RequestMiddleware checks SC-003: with one option and no
// extraction function, every record logged with a request's context carries
// the identifier that a middleware stored.
func TestContextValue_RequestMiddleware(t *testing.T) {
	rec := slogxtest.NewRecorder()
	logger := slog.New(must(slogx.New(rec,
		slogx.WithDefaultLevel(slog.LevelInfo),
		slogx.WithContextValue("request_id", ridFrom),
	)))
	// storeID stores the request identifier in the context, as the requestid
	// middleware does.
	storeID := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r.WithContext(withRID(r.Context(), r.Header.Get("X-Request-Id"))))
		})
	}
	app := storeID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		logger.InfoContext(ctx, "order received")
		logger.WarnContext(ctx, "stock low", "sku", "a1")
		logger.WithGroup("db").ErrorContext(ctx, "insert failed")
		w.WriteHeader(http.StatusNoContent)
	}))
	for i := range 5 {
		req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/orders", nil)
		req.Header.Set("X-Request-Id", fmt.Sprintf("req-%d", i))
		app.ServeHTTP(httptest.NewRecorder(), req)
	}
	records := rec.Records()
	if len(records) != 15 {
		t.Fatalf("got %d records, want 15", len(records))
	}
	for j, r := range records {
		want := fmt.Sprintf("req-%d", j/3)
		if v, ok := r.Attr("request_id"); !ok || v.String() != want {
			t.Errorf("record %d (%s): request_id = %v, %v; want %s", j, r.Message, v, ok, want)
		}
	}
}

func TestContextValue_Conformance(t *testing.T) {
	testConformance(t, func(next slog.Handler) slog.Handler {
		return must(slogx.New(next, slogx.WithContextValue("ctxvalue", func(context.Context) (string, bool) { return "x", true })))
	})
}

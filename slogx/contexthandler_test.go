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

func alwaysAttr(context.Context) []slog.Attr { return []slog.Attr{slog.String("ctxattr", "x")} }

// requestContext carries tenant=t1 (attached) and the identifier abc (for ridAttrs).
func requestContext() context.Context {
	return slogx.With(withRID(context.Background(), "abc"), slog.String("tenant", "t1"))
}

func TestContextHandler_Conformance(t *testing.T) {
	t.Run("no context attributes", func(t *testing.T) {
		testConformance(t, func(next slog.Handler) slog.Handler {
			return must(slogx.NewContextHandler(next))
		})
	})
	t.Run("context attributes on every record", func(t *testing.T) {
		testConformance(t, func(next slog.Handler) slog.Handler {
			return must(slogx.NewContextHandler(next, slogx.WithContextFunc(alwaysAttr)))
		})
	})
}

func TestContextHandler_TopLevel(t *testing.T) {
	var buf bytes.Buffer
	ctx := slogx.With(context.Background(), slog.String("tenant", "t1"))
	contextLogger(t, &buf).InfoContext(ctx, "order created")
	if got, want := onlyLine(t, &buf), `{"level":"INFO","msg":"order created","tenant":"t1"}`; got != want {
		t.Errorf("record = %s, want %s", got, want)
	}
}

func TestContextHandler_ExtractionFunc(t *testing.T) {
	var buf bytes.Buffer
	logger := contextLogger(t, &buf, slogx.WithContextFunc(ridAttrs))
	logger.InfoContext(withRID(context.Background(), "abc"), "with id")
	logger.InfoContext(context.Background(), "without id")
	ls := lines(t, &buf)
	want := []string{
		`{"level":"INFO","msg":"with id","request_id":"abc"}`,
		`{"level":"INFO","msg":"without id"}`,
	}
	if strings.Join(ls, "\n") != strings.Join(want, "\n") {
		t.Errorf("records = %q, want %q", ls, want)
	}
}

func TestContextHandler_GroupsKeepContextAtTop(t *testing.T) {
	tests := []struct {
		name string
		log  func(*slog.Logger, context.Context)
		want string
	}{
		{
			name: "group with logger attributes",
			log: func(l *slog.Logger, ctx context.Context) {
				l.WithGroup("db").With("table", "orders").InfoContext(ctx, "q", "rows", 3)
			},
			want: `{"level":"INFO","msg":"q","tenant":"t1","request_id":"abc","db":{"table":"orders","rows":3}}`,
		},
		{
			name: "nested groups",
			log: func(l *slog.Logger, ctx context.Context) {
				l.WithGroup("a").With("x", 1).WithGroup("b").InfoContext(ctx, "m", "y", 2)
			},
			want: `{"level":"INFO","msg":"m","tenant":"t1","request_id":"abc","a":{"x":1,"b":{"y":2}}}`,
		},
		{
			name: "empty group omitted",
			log: func(l *slog.Logger, ctx context.Context) {
				l.WithGroup("a").InfoContext(ctx, "m")
			},
			want: `{"level":"INFO","msg":"m","tenant":"t1","request_id":"abc"}`,
		},
		{
			name: "top-level logger attributes before the group",
			log: func(l *slog.Logger, ctx context.Context) {
				l.With("service", "api").WithGroup("db").InfoContext(ctx, "m", "rows", 1)
			},
			want: `{"level":"INFO","msg":"m","service":"api","tenant":"t1","request_id":"abc","db":{"rows":1}}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			tt.log(contextLogger(t, &buf, slogx.WithContextFunc(ridAttrs)), requestContext())
			if got := onlyLine(t, &buf); got != tt.want {
				t.Errorf("record = %s\nwant      %s", got, tt.want)
			}
		})
	}
}

func TestContextHandler_RecordWins(t *testing.T) {
	ctx := withRID(context.Background(), "xyz")
	tests := []struct {
		name string
		log  func(*slog.Logger)
		want string
	}{
		{
			name: "record attribute",
			log:  func(l *slog.Logger) { l.InfoContext(ctx, "m", "request_id", "abc") },
			want: `{"level":"INFO","msg":"m","request_id":"abc"}`,
		},
		{
			name: "logger attribute",
			log:  func(l *slog.Logger) { l.With("request_id", "w").InfoContext(ctx, "m") },
			want: `{"level":"INFO","msg":"m","request_id":"w"}`,
		},
		{
			name: "same key inside a group is a different path",
			log:  func(l *slog.Logger) { l.WithGroup("db").InfoContext(ctx, "m", "request_id", "x") },
			want: `{"level":"INFO","msg":"m","request_id":"xyz","db":{"request_id":"x"}}`,
		},
		{
			name: "logger attribute before a group",
			log:  func(l *slog.Logger) { l.With("request_id", "w").WithGroup("db").InfoContext(ctx, "m", "a", 1) },
			want: `{"level":"INFO","msg":"m","request_id":"w","db":{"a":1}}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			tt.log(contextLogger(t, &buf, slogx.WithContextFunc(ridAttrs)))
			if got := onlyLine(t, &buf); got != tt.want {
				t.Errorf("record = %s\nwant      %s", got, tt.want)
			}
		})
	}
	t.Run("duplicates among the record's own attributes are kept", func(t *testing.T) {
		var buf bytes.Buffer
		contextLogger(t, &buf).InfoContext(context.Background(), "m", "request_id", "a", "request_id", "b")
		if n := strings.Count(onlyLine(t, &buf), `"request_id"`); n != 2 {
			t.Errorf("request_id appears %d times, want 2", n)
		}
	})
}

func TestContextHandler_Precedence(t *testing.T) {
	extracted := func(ctx context.Context) []slog.Attr {
		return []slog.Attr{
			slog.String("k", "extracted"),
			slog.Group("", slog.String("g", "1")),
			slog.String("", "dropped"),
		}
	}
	second := func(context.Context) []slog.Attr {
		return []slog.Attr{slog.String("g", "second"), slog.String("only2", "v")}
	}
	var buf bytes.Buffer
	logger := contextLogger(t, &buf, slogx.WithContextFunc(extracted), slogx.WithContextFunc(second))
	logger.InfoContext(slogx.With(context.Background(), slog.String("k", "attached")), "m")
	if got, want := onlyLine(t, &buf), `{"level":"INFO","msg":"m","k":"attached","g":"1","only2":"v"}`; got != want {
		t.Errorf("record = %s\nwant      %s", got, want)
	}
}

func TestContextHandler_OrderStable(t *testing.T) {
	ctx := slogx.With(context.Background(), slog.String("c", "ctx"))
	var buf bytes.Buffer
	logger := contextLogger(t, &buf).With("l", "logger")
	logger.InfoContext(ctx, "m", "r", "record")
	logger.WithGroup("g").InfoContext(ctx, "m", "r", "record")
	ls := lines(t, &buf)
	want := []string{
		`{"level":"INFO","msg":"m","l":"logger","c":"ctx","r":"record"}`,
		`{"level":"INFO","msg":"m","l":"logger","c":"ctx","g":{"r":"record"}}`,
	}
	if strings.Join(ls, "\n") != strings.Join(want, "\n") {
		t.Errorf("records = %q\nwant      %q", ls, want)
	}
}

func TestContextHandler_NoContextMethod(t *testing.T) {
	static := func(context.Context) []slog.Attr { return []slog.Attr{slog.String("service", "api")} }
	var buf bytes.Buffer
	contextLogger(t, &buf, slogx.WithContextFunc(ridAttrs), slogx.WithContextFunc(static)).Info("m")
	if got, want := onlyLine(t, &buf), `{"level":"INFO","msg":"m","service":"api"}`; got != want {
		t.Errorf("record = %s, want %s", got, want)
	}
}

// recordingHandler stores the records it receives, as they are.
type recordingHandler struct {
	records []slog.Record
	err     error
}

func (h *recordingHandler) Enabled(context.Context, slog.Level) bool { return true }

func (h *recordingHandler) Handle(_ context.Context, r slog.Record) error {
	h.records = append(h.records, r.Clone())
	return h.err
}

func (h *recordingHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *recordingHandler) WithGroup(string) slog.Handler      { return h }

func TestContextHandler_Unchanged(t *testing.T) {
	next := &recordingHandler{}
	h := must(slogx.NewContextHandler(next))
	r := slog.NewRecord(time.Unix(1, 0), slog.LevelWarn, "m", 0)
	r.AddAttrs(slog.String("a", "1"), slog.Int("b", 2))
	if err := h.Handle(context.Background(), r); err != nil {
		t.Fatal(err)
	}
	got := next.records[0]
	if !got.Time.Equal(r.Time) || got.Level != r.Level || got.Message != r.Message || got.NumAttrs() != 2 {
		t.Errorf("record = %v, want %v", got, r)
	}
}

func TestContextHandler_ReturnsNextError(t *testing.T) {
	errNext := errors.New("next failed")
	h := must(slogx.NewContextHandler(&recordingHandler{err: errNext}, slogx.WithContextFunc(alwaysAttr)))
	for _, hh := range []slog.Handler{h, h.WithGroup("g")} {
		if err := hh.Handle(context.Background(), slog.NewRecord(time.Time{}, slog.LevelInfo, "m", 0)); err != errNext { //nolint:errorlint // identity check (FR-005)
			t.Errorf("err = %v, want the next handler's error unchanged", err)
		}
	}
}

func TestContextHandler_Options(t *testing.T) {
	next := newJSON(&bytes.Buffer{}, nil)
	tests := []struct {
		name string
		next slog.Handler
		opts []slogx.Option
	}{
		{"nil next", nil, nil},
		{"nil function", next, []slogx.Option{slogx.WithContextFunc(nil)}},
		{"inapplicable option", next, []slogx.Option{slogx.WithRedactKeys("x")}},
		{"nil option", next, []slogx.Option{nil}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h, err := slogx.NewContextHandler(tt.next, tt.opts...)
			if !errors.Is(err, slogx.ErrInvalidConfig) || h != nil {
				t.Errorf("NewContextHandler = %v, %v; want nil, ErrInvalidConfig", h, err)
			}
		})
	}
}

func TestContextHandler_Derived(t *testing.T) {
	h := must(slogx.NewContextHandler(newJSON(&bytes.Buffer{}, nil)))
	if h.WithGroup("") != slog.Handler(h) {
		t.Error(`WithGroup("") did not return the receiver`)
	}
	if h.WithAttrs(nil) != slog.Handler(h) {
		t.Error("WithAttrs(nil) did not return the receiver")
	}
	if !h.Enabled(context.Background(), slog.LevelInfo) || h.Enabled(context.Background(), slog.LevelDebug) {
		t.Error("Enabled does not forward to the next handler")
	}
}

func TestContextHandler_DerivedHandlersIndependent(t *testing.T) {
	var buf bytes.Buffer
	base := contextLogger(t, &buf).WithGroup("g").With("a", 1)
	left := base.With("l", 1)
	right := base.With("r", 2)
	ctx := slogx.With(context.Background(), slog.String("c", "x"))
	left.InfoContext(ctx, "left")
	right.InfoContext(ctx, "right")
	ls := lines(t, &buf)
	want := []string{
		`{"level":"INFO","msg":"left","c":"x","g":{"a":1,"l":1}}`,
		`{"level":"INFO","msg":"right","c":"x","g":{"a":1,"r":2}}`,
	}
	if strings.Join(ls, "\n") != strings.Join(want, "\n") {
		t.Errorf("records = %q\nwant      %q", ls, want)
	}
}

func TestAlloc_ContextHandlerDisabled(t *testing.T) {
	h := must(slogx.NewContextHandler(newJSON(&bytes.Buffer{}, nil), slogx.WithContextFunc(ridAttrs)))
	assertNoAllocsWhenDisabled(t, h, requestContext(), context.Background())
	assertNoAllocsWhenDisabled(t, h.WithGroup("g").WithAttrs([]slog.Attr{slog.Int("a", 1)}), requestContext())
}

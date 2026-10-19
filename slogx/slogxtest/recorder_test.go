package slogxtest_test

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/slogtest"
	"time"

	"github.com/paccolamano/go/slogx/slogxtest"
)

// toMap converts a captured record into the shape that slogtest expects.
func toMap(r slogxtest.Record) map[string]any {
	m := map[string]any{slog.LevelKey: r.Level, slog.MessageKey: r.Message}
	if !r.Time.IsZero() {
		m[slog.TimeKey] = r.Time
	}
	addAttrs(m, r.Attrs)
	return m
}

func addAttrs(m map[string]any, attrs []slog.Attr) {
	for _, a := range attrs {
		if a.Value.Kind() == slog.KindGroup {
			g := map[string]any{}
			addAttrs(g, a.Value.Group())
			m[a.Key] = g
			continue
		}
		m[a.Key] = a.Value.Any()
	}
}

// lastRecord returns the last captured record, failing the test when there is none.
func lastRecord(tb testing.TB, rec *slogxtest.Recorder) slogxtest.Record {
	tb.Helper()
	r, ok := rec.Last()
	if !ok {
		tb.Fatal("no record captured")
	}
	return r
}

func TestRecorder_Conformance(t *testing.T) {
	var rec *slogxtest.Recorder
	slogtest.Run(t, func(*testing.T) slog.Handler {
		rec = slogxtest.NewRecorder()
		return rec
	}, func(t *testing.T) map[string]any {
		t.Helper()
		return toMap(lastRecord(t, rec))
	})
}

func TestRecorder_Last(t *testing.T) {
	rec := slogxtest.NewRecorder()
	slog.New(rec).InfoContext(context.Background(), "panic recovered", "request_id", "abc")
	r := lastRecord(t, rec)
	if r.Message != "panic recovered" || r.Level != slog.LevelInfo || r.Time.IsZero() {
		t.Errorf("record = %+v", r)
	}
	if v, ok := r.Attr("request_id"); !ok || v.String() != "abc" {
		t.Errorf(`Attr("request_id") = %v, %v; want abc, true`, v, ok)
	}
}

func TestRecorder_GroupsAndAttrs(t *testing.T) {
	rec := slogxtest.NewRecorder()
	slog.New(rec).With("component", "db").WithGroup("query").Info("slow", "ms", 12)
	r := lastRecord(t, rec)
	if v, ok := r.Attr("component"); !ok || v.String() != "db" {
		t.Errorf(`Attr("component") = %v, %v; want db, true`, v, ok)
	}
	if v, ok := r.Attr("query", "ms"); !ok || v.Int64() != 12 {
		t.Errorf(`Attr("query", "ms") = %v, %v; want 12, true`, v, ok)
	}
	if _, ok := r.Attr("ms"); ok {
		t.Error(`Attr("ms") found an attribute that is inside the group`)
	}
}

func TestRecorder_MinLevel(t *testing.T) {
	ctx := context.Background()
	warn := slogxtest.NewRecorder(slogxtest.WithLevel(slog.LevelWarn))
	if warn.Enabled(ctx, slog.LevelInfo) || !warn.Enabled(ctx, slog.LevelWarn) {
		t.Error("WithLevel(Warn): Enabled is wrong")
	}
	slog.New(warn).Info("ignored")
	if warn.Len() != 0 {
		t.Errorf("Len = %d, want 0", warn.Len())
	}
	tests := []struct {
		name string
		rec  slog.Handler
		want bool
	}{
		{"default enables every level", slogxtest.NewRecorder(), true},
		{"nil leveler enables every level", slogxtest.NewRecorder(slogxtest.WithLevel(nil)), true},
		{"derived recorder keeps the default", slogxtest.NewRecorder().WithGroup("g"), true},
		{"derived recorder keeps the level", slogxtest.NewRecorder(slogxtest.WithLevel(slog.LevelError)).WithAttrs([]slog.Attr{slog.Int("a", 1)}), false},
		{"dynamic leveler", slogxtest.NewRecorder(slogxtest.WithLevel(new(slog.LevelVar))), false},
	}
	for _, tt := range tests {
		if got := tt.rec.Enabled(ctx, slog.Level(-100)); got != tt.want {
			t.Errorf("%s: Enabled(-100) = %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestRecorder_Concurrent(t *testing.T) {
	rec := slogxtest.NewRecorder()
	logger := slog.New(rec)
	var wg sync.WaitGroup
	for i := range 100 {
		wg.Go(func() {
			if i%2 == 0 {
				logger.Info("m", "i", i)
				return
			}
			logger.With("odd", true).WithGroup("g").Info("m", "i", i)
		})
	}
	wg.Wait()
	if rec.Len() != 100 || len(rec.Records()) != 100 {
		t.Errorf("Len = %d, Records = %d; want 100", rec.Len(), len(rec.Records()))
	}
}

func TestRecorder_Empty(t *testing.T) {
	rec := slogxtest.NewRecorder()
	if r, ok := rec.Last(); ok {
		t.Errorf("Last = %+v, true; want false", r)
	}
	if rec.Len() != 0 || len(rec.Records()) != 0 {
		t.Error("an empty recorder reports records")
	}
}

func TestRecord_Attr(t *testing.T) {
	r := slogxtest.Record{Attrs: []slog.Attr{
		slog.String("k", "first"),
		slog.Group("g", slog.Int("n", 1), slog.Group("h", slog.Bool("b", true))),
		slog.String("k", "last"),
		slog.String("leaf", "v"),
	}}
	tests := []struct {
		path []string
		want string
		ok   bool
	}{
		{[]string{"k"}, "last", true},
		{[]string{"g", "n"}, "1", true},
		{[]string{"g", "h", "b"}, "true", true},
		{[]string{"missing"}, "", false},
		{[]string{"g", "missing"}, "", false},
		{[]string{"leaf", "x"}, "", false},
		{nil, "", false},
	}
	for _, tt := range tests {
		t.Run(strings.Join(tt.path, "."), func(t *testing.T) {
			v, ok := r.Attr(tt.path...)
			if ok != tt.ok || (ok && v.String() != tt.want) {
				t.Errorf("Attr(%q) = %v, %v; want %q, %v", tt.path, v, ok, tt.want, tt.ok)
			}
		})
	}
	if v, ok := r.Attr("g"); !ok || v.Kind() != slog.KindGroup {
		t.Errorf(`Attr("g") = %v, %v; want the group`, v, ok)
	}
}

// changingValuer returns the current value of n when it is resolved.
type changingValuer struct{ n *atomic.Int64 }

func (v changingValuer) LogValue() slog.Value { return slog.Int64Value(v.n.Load()) }

// selfValuer resolves to a group that contains itself.
type selfValuer struct{}

func (s selfValuer) LogValue() slog.Value { return slog.GroupValue(slog.Any("x", s)) }

func TestRecorder_Normalization(t *testing.T) {
	rec := slogxtest.NewRecorder()
	var n atomic.Int64
	n.Store(1)
	logger := slog.New(rec).With("w", changingValuer{&n})
	n.Store(2)
	logger.Info("m",
		slog.Attr{},
		slog.String("", "dropped"),
		slog.Group("", slog.String("inlined", "yes")),
		slog.Group("empty"),
		slog.Group("outer", slog.Group("inner")),
		"v", changingValuer{&n},
		"deep", selfValuer{},
	)
	n.Store(3)
	r := lastRecord(t, rec)
	keys := make([]string, 0, len(r.Attrs))
	for _, a := range r.Attrs {
		keys = append(keys, a.Key)
	}
	if got, want := strings.Join(keys, ","), "w,inlined,v,deep"; got != want {
		t.Errorf("keys = %s, want %s", got, want)
	}
	if v, _ := r.Attr("w"); v.Int64() != 1 {
		t.Errorf("logger attribute = %v, want 1 (resolved by WithAttrs)", v)
	}
	if v, _ := r.Attr("v"); v.Int64() != 2 {
		t.Errorf("record attribute = %v, want 2 (resolved when captured)", v)
	}
	path := []string{"deep"}
	for range 63 {
		path = append(path, "x")
	}
	if v, ok := r.Attr(path...); !ok || v.Kind() != slog.KindGroup {
		t.Fatalf("group level 64 = %v, %v; want a group", v, ok)
	}
	if v, ok := r.Attr(append(path, "x")...); !ok || v.String() != "!DEPTH" {
		t.Errorf("group level 65 = %v, %v; want !DEPTH", v, ok)
	}
}

func TestRecorder_RecordsCopy(t *testing.T) {
	rec := slogxtest.NewRecorder()
	slog.New(rec).Info("original")
	records := rec.Records()
	records[0].Message = "changed"
	if r := lastRecord(t, rec); r.Message != "original" {
		t.Errorf("Message = %q, want original", r.Message)
	}
}

func TestRecorder_HandleReturnsNil(t *testing.T) {
	rec := slogxtest.NewRecorder()
	if err := rec.Handle(context.Background(), slog.NewRecord(time.Time{}, slog.LevelInfo, "m", 0)); err != nil {
		t.Errorf("Handle = %v, want nil", err)
	}
	if rec.WithGroup("") != slog.Handler(rec) || rec.WithAttrs(nil) != slog.Handler(rec) {
		t.Error(`WithGroup("") or WithAttrs(nil) did not return the receiver`)
	}
}

// TestRecorder_MiddlewarePatterns replays what the accesslog and recovery tests
// assert with their own capture handlers (FR-041).
func TestRecorder_MiddlewarePatterns(t *testing.T) {
	rec := slogxtest.NewRecorder()
	logger := slog.New(rec)
	ctx := context.Background()
	logger.LogAttrs(ctx, slog.LevelInfo, "http request",
		slog.String("method", "GET"),
		slog.Int("status", 200),
		slog.Attr{Key: "header", Value: slog.GroupValue(slog.String("X-Test", "a, b"))},
	)
	logger.LogAttrs(ctx, slog.LevelError, "panic recovered",
		slog.String("request_id", "abc"),
		slog.Bool("response_started", true),
	)

	records := rec.Records()
	if len(records) != 2 || rec.Len() != 2 {
		t.Fatalf("records = %d, want 2", len(records))
	}
	access, recovered := records[0], records[1]
	if access.Message != "http request" || access.Level != slog.LevelInfo {
		t.Errorf("access record = %q at %v", access.Message, access.Level)
	}
	if v, _ := access.Attr("status"); v.Int64() != 200 {
		t.Errorf("status = %v, want 200", v)
	}
	if v, _ := access.Attr("header", "X-Test"); v.String() != "a, b" {
		t.Errorf("header.X-Test = %v, want %q", v, "a, b")
	}
	if recovered.Message != "panic recovered" || recovered.Level != slog.LevelError {
		t.Errorf("recovery record = %q at %v", recovered.Message, recovered.Level)
	}
	if v, ok := recovered.Attr("response_started"); !ok || !v.Bool() {
		t.Errorf("response_started = %v, %v; want true", v, ok)
	}

	warn := slogxtest.NewRecorder(slogxtest.WithLevel(slog.LevelWarn))
	slog.New(warn).LogAttrs(ctx, slog.LevelInfo, "http request")
	if warn.Len() != 0 {
		t.Error("a record below the minimum level was captured")
	}
}

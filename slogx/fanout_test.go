package slogx_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/paccolamano/go/slogx"
	"github.com/paccolamano/go/slogx/slogxtest"
)

type traceKey struct{}

// withTrace stores a trace identifier the way a tracing library would.
func withTrace(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, traceKey{}, id)
}

// traceAttrs returns trace_id and span_id, as the README recipe does with
// OpenTelemetry.
func traceAttrs(ctx context.Context) []slog.Attr {
	if id, ok := ctx.Value(traceKey{}).(string); ok {
		return []slog.Attr{slog.String("trace_id", id), slog.String("span_id", "s-"+id)}
	}
	return nil
}

// appendingHandler adds added=value to every record it receives, without
// cloning it, and keeps the record.
type appendingHandler struct {
	value   string
	records []slog.Record
}

func (h *appendingHandler) Enabled(context.Context, slog.Level) bool { return true }

func (h *appendingHandler) Handle(_ context.Context, r slog.Record) error {
	r.AddAttrs(slog.String("added", h.value))
	h.records = append(h.records, r)
	return nil
}

func (h *appendingHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *appendingHandler) WithGroup(string) slog.Handler      { return h }

// defaultInfo is the default level of most fanout tests.
func defaultInfo() slogx.Option { return slogx.WithDefaultLevel(slog.LevelInfo) }

func TestFanout_SameRecords(t *testing.T) {
	var stdout bytes.Buffer
	otel := slogxtest.NewRecorder()
	logger := slog.New(must(slogx.New(slogx.Fanout(newJSON(&stdout, nil), otel), defaultInfo())))
	ctx := context.Background()
	logger.InfoContext(ctx, "info")
	logger.DebugContext(ctx, "debug")
	logger.DebugContext(slogx.WithLevel(ctx, slog.LevelDebug), "request debug")

	want := `{"level":"INFO","msg":"info"}` + "\n" + `{"level":"DEBUG","msg":"request debug"}`
	if got := strings.Join(lines(t, &stdout), "\n"); got != want {
		t.Errorf("stdout = %q, want %q", got, want)
	}
	records := otel.Records()
	if len(records) != 2 ||
		records[0].Message != "info" || records[0].Level != slog.LevelInfo ||
		records[1].Message != "request debug" || records[1].Level != slog.LevelDebug {
		t.Errorf("otel = %+v, want info and request debug", records)
	}
}

func TestFanout_IgnoresOutputLevels(t *testing.T) {
	var buf bytes.Buffer
	slog.New(must(slogx.New(slogx.Fanout(newJSON(&buf, slog.LevelError)), defaultInfo()))).Info("m")
	if got, want := onlyLine(t, &buf), `{"level":"INFO","msg":"m"}`; got != want {
		t.Errorf("record = %s, want %s", got, want)
	}
}

func TestFanout_RequiresDefaultLevel(t *testing.T) {
	out := newJSON(&bytes.Buffer{}, nil)
	tests := []struct {
		name string
		next slog.Handler
	}{
		{"fanout", slogx.Fanout(out)},
		{"with attributes", slogx.Fanout(out).WithAttrs([]slog.Attr{slog.Int("a", 1)})},
		{"with a group", slogx.Fanout(out).WithGroup("g")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h, err := slogx.New(tt.next)
			if h != nil || !errors.Is(err, slogx.ErrInvalidConfig) || !strings.HasSuffix(err.Error(), "Fanout: requires WithDefaultLevel") {
				t.Errorf("New = %v, %v; want nil, ErrInvalidConfig requiring WithDefaultLevel", h, err)
			}
		})
	}
}

func TestFanout_GroupsRedactionContext(t *testing.T) {
	var a, b bytes.Buffer
	logger := slog.New(must(slogx.New(slogx.Fanout(newJSON(&a, nil), newJSON(&b, slog.LevelError)),
		defaultInfo(),
		slogx.WithContextFunc(ridAttrs),
		slogx.WithRedactKeys(sensitiveKeys...),
	)))
	ctx := slogx.With(requestContext(), slog.String("token", "s3cr3t"))
	logger.With("service", "api").WithGroup("db").With("table", "orders").InfoContext(ctx, "grouped", "password", "p")
	logger.InfoContext(ctx, "plain")

	if a.String() != b.String() {
		t.Errorf("outputs differ:\n%s\n%s", a.String(), b.String())
	}
	want := []string{
		`{"level":"INFO","msg":"grouped","service":"api","tenant":"t1","token":"[REDACTED]","request_id":"abc","db":{"table":"orders","password":"[REDACTED]"}}`,
		`{"level":"INFO","msg":"plain","tenant":"t1","token":"[REDACTED]","request_id":"abc"}`,
	}
	if got := lines(t, &a); strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("records = %q\nwant      %q", got, want)
	}
}

func TestFanout_NestedNew(t *testing.T) {
	var so, ot bytes.Buffer
	nested := must(slogx.New(newJSON(&so, nil), slogx.WithContextFunc(traceAttrs)))
	logger := slog.New(must(slogx.New(slogx.Fanout(nested, newJSON(&ot, nil)),
		defaultInfo(),
		slogx.WithContextFunc(ridAttrs),
		slogx.WithRedactKeys(sensitiveKeys...),
	)))
	ctx := withTrace(requestContext(), "T")
	bare := withTrace(context.Background(), "T")
	logger.InfoContext(ctx, "plain")
	logger.WithGroup("db").InfoContext(ctx, "grouped", "rows", 3)
	logger.InfoContext(bare, "no outer attributes")
	logger.WithGroup("g").InfoContext(bare, "grouped, no outer attributes", "a", 1)

	stdout, otel := lines(t, &so), lines(t, &ot)
	if len(stdout) != 4 || len(otel) != 4 {
		t.Fatalf("got %d and %d lines, want 4 each:\n%s\n%s", len(stdout), len(otel), so.String(), ot.String())
	}
	withoutTrace := strings.NewReplacer(`"trace_id":"T","span_id":"s-T",`, "", `,"trace_id":"T","span_id":"s-T"`, "")
	for i := range stdout {
		if rec := decode(t, stdout[i]); rec["trace_id"] != "T" || rec["span_id"] != "s-T" {
			t.Errorf("stdout %d has no trace IDs: %s", i, stdout[i])
		}
		if strings.Contains(otel[i], `"trace_id"`) || strings.Contains(otel[i], `"span_id"`) {
			t.Errorf("otel %d has trace IDs: %s", i, otel[i])
		}
		for _, key := range []string{"trace_id", "span_id", "tenant", "request_id"} {
			if n := strings.Count(stdout[i], `"`+key+`":`); n > 1 {
				t.Errorf("stdout %d has %s %d times: %s", i, key, n, stdout[i])
			}
		}
		if got := withoutTrace.Replace(stdout[i]); got != otel[i] {
			t.Errorf("record %d differs beyond the trace IDs:\n%s\n%s", i, stdout[i], otel[i])
		}
	}
}

func TestFanout_Errors(t *testing.T) {
	ctx := context.Background()
	t.Run("one fails", func(t *testing.T) {
		errSecond := errors.New("second failed")
		first, second, third := &recordingHandler{}, &recordingHandler{err: errSecond}, &recordingHandler{}
		h := must(slogx.New(slogx.Fanout(first, second, third), defaultInfo()))
		if err := h.Handle(ctx, recordWith(slog.String("a", "b"))); err != errSecond { //nolint:errorlint // identity check (FR-009)
			t.Errorf("err = %v, want the failing output's error unchanged", err)
		}
		if len(first.records) != 1 || len(second.records) != 1 || len(third.records) != 1 {
			t.Errorf("records = %d, %d, %d; want 1 each", len(first.records), len(second.records), len(third.records))
		}
	})
	t.Run("several fail", func(t *testing.T) {
		e1, e3 := errors.New("first failed"), errors.New("third failed")
		ok := &recordingHandler{}
		h := must(slogx.New(slogx.Fanout(&recordingHandler{err: e1}, ok, &recordingHandler{err: e3}), defaultInfo()))
		err := h.Handle(ctx, recordWith(slog.String("a", "b")))
		if !errors.Is(err, e1) || !errors.Is(err, e3) || err == e1 || err == e3 { //nolint:errorlint // identity check (FR-009)
			t.Errorf("err = %v, want both errors combined", err)
		}
		if len(ok.records) != 1 {
			t.Errorf("the output between the failing ones got %d records, want 1", len(ok.records))
		}
	})
	t.Run("none fails", func(t *testing.T) {
		h := must(slogx.New(slogx.Fanout(&recordingHandler{}, &recordingHandler{}), defaultInfo()))
		if err := h.Handle(ctx, recordWith(slog.String("a", "b"))); err != nil {
			t.Errorf("err = %v, want nil", err)
		}
	})
}

func TestFanout_InvalidOutputs(t *testing.T) {
	out := newJSON(&bytes.Buffer{}, nil)
	info := []slogx.Option{defaultInfo()}
	tests := []struct {
		name string
		next slog.Handler
		opts []slogx.Option
		want string
	}{
		{"no output", slogx.Fanout(), info, "Fanout: no output"},
		{"nil output", slogx.Fanout(out, nil), info, "Fanout: outputs[1]: must not be nil"},
		{"nil first output", slogx.Fanout(nil, out), info, "Fanout: outputs[0]: must not be nil"},
		{"nested without output", slogx.Fanout(out, slogx.Fanout()), info, "Fanout: outputs[1]: no output"},
		{"nested nil output", slogx.Fanout(slogx.Fanout(nil)), info, "Fanout: outputs[0]: outputs[0]: must not be nil"},
		{"invalid, with a group", slogx.Fanout().WithGroup("g"), info, "Fanout: no output"},
		{"invalid before missing level", slogx.Fanout(), nil, "Fanout: no output"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h, err := slogx.New(tt.next, tt.opts...)
			if h != nil || !errors.Is(err, slogx.ErrInvalidConfig) || !strings.HasSuffix(err.Error(), tt.want) {
				t.Errorf("New = %v, %v; want nil, ErrInvalidConfig ending with %q", h, err, tt.want)
			}
		})
	}
}

func TestFanout_OneOutput(t *testing.T) {
	var got, want bytes.Buffer
	slog.New(must(slogx.New(slogx.Fanout(newJSON(&got, slog.LevelError)), defaultInfo()))).Info("m", "a", 1)
	slog.New(newJSON(&want, nil)).Info("m", "a", 1)
	if got.String() != want.String() {
		t.Errorf("output = %q, want %q", got.String(), want.String())
	}
}

func TestFanout_SameOutputTwice(t *testing.T) {
	rec := slogxtest.NewRecorder()
	slog.New(must(slogx.New(slogx.Fanout(rec, rec), defaultInfo()))).Info("m")
	if rec.Len() != 2 {
		t.Errorf("got %d records, want 2", rec.Len())
	}
}

func TestFanout_Nested(t *testing.T) {
	rec := slogxtest.NewRecorder()
	slog.New(must(slogx.New(slogx.Fanout(rec, slogx.Fanout(rec, rec)), defaultInfo()))).Info("m")
	if rec.Len() != 3 {
		t.Errorf("got %d records, want 3", rec.Len())
	}
}

func TestFanout_OnItsOwn(t *testing.T) {
	ctx := context.Background()
	rec := slogxtest.NewRecorder()
	var buf bytes.Buffer
	f := slogx.Fanout(rec, newJSON(&buf, slog.LevelError))
	for _, level := range []slog.Level{-100, slog.LevelDebug, slog.LevelError, 100} {
		if !f.Enabled(ctx, level) {
			t.Errorf("Enabled(%v) = false, want true", level)
		}
	}
	slog.New(f).Debug("m")
	if got, want := onlyLine(t, &buf), `{"level":"DEBUG","msg":"m"}`; rec.Len() != 1 || got != want {
		t.Errorf("recorder has %d records, JSON has %s; want 1 and %s", rec.Len(), got, want)
	}

	rec2 := slogxtest.NewRecorder()
	invalid := slogx.Fanout(nil, rec2).WithGroup("g").WithAttrs([]slog.Attr{slog.Int("a", 1)})
	if err := invalid.Handle(ctx, recordWith()); err != nil || rec2.Len() != 1 {
		t.Errorf("invalid fanout on its own: err = %v, %d records; want nil and 1", err, rec2.Len())
	}
}

func TestFanout_Hidden(t *testing.T) {
	inner := must(slogx.New(slogx.Fanout(slogxtest.NewRecorder()), defaultInfo()))
	if h, err := slogx.New(inner); h == nil || err != nil {
		t.Errorf("New over a handler that wraps a fanout = %v, %v; want a handler", h, err)
	}
}

func TestFanout_OwnCopy(t *testing.T) {
	// Added one at a time, the attributes leave spare capacity in the record's
	// overflow slice. Without a copy per output, the second output would write
	// into the slice of the first, which slog reports with a !BUG attribute.
	r := slog.NewRecord(time.Time{}, slog.LevelInfo, "m", 0)
	for i := range 8 {
		r.AddAttrs(slog.Int(fmt.Sprintf("a%d", i), i))
	}
	a, b := &appendingHandler{value: "a"}, &appendingHandler{value: "b"}
	if err := slogx.Fanout(a, b).Handle(context.Background(), r); err != nil {
		t.Fatal(err)
	}
	for _, h := range []*appendingHandler{a, b} {
		var keys []string
		var last slog.Attr
		h.records[0].Attrs(func(attr slog.Attr) bool {
			keys = append(keys, attr.Key)
			last = attr
			return true
		})
		if len(keys) != 9 || last.Key != "added" || last.Value.String() != h.value || strings.Contains(strings.Join(keys, ","), "!BUG") {
			t.Errorf("output %s: keys %v, last %v; want 9 attributes ending with added=%s", h.value, keys, last, h.value)
		}
	}
}

func TestFanout_Derived(t *testing.T) {
	f := slogx.Fanout(slogxtest.NewRecorder())
	if f.WithGroup("") != f || f.WithAttrs(nil) != f {
		t.Error(`WithGroup("") or WithAttrs(nil) did not return the receiver`)
	}
}

// TestFanout_LevelMatrix checks SC-001: both outputs receive exactly the records
// whose level is at least the per-request level, or the default level when the
// context carries none.
func TestFanout_LevelMatrix(t *testing.T) {
	for _, def := range []slog.Level{slog.LevelDebug, slog.LevelInfo, slog.LevelWarn, slog.LevelError} {
		for _, req := range []string{"none", "below", "above"} {
			t.Run(fmt.Sprintf("%v/%s", def, req), func(t *testing.T) {
				var buf bytes.Buffer
				rec := slogxtest.NewRecorder()
				logger := slog.New(must(slogx.New(slogx.Fanout(newJSON(&buf, nil), rec), slogx.WithDefaultLevel(def))))
				ctx, threshold := context.Background(), def
				switch req {
				case "below":
					threshold = def - 4
					ctx = slogx.WithLevel(ctx, threshold)
				case "above":
					threshold = def + 4
					ctx = slogx.WithLevel(ctx, threshold)
				}
				var want []slog.Level
				for _, level := range []slog.Level{-8, -4, 0, 4, 8, 12} {
					logger.Log(ctx, level, "m")
					if level >= threshold {
						want = append(want, level)
					}
				}
				var jsonLevels, wantStrings []string
				for _, line := range lines(t, &buf) {
					jsonLevels = append(jsonLevels, fmt.Sprint(decode(t, line)["level"]))
				}
				for _, level := range want {
					wantStrings = append(wantStrings, level.String())
				}
				if strings.Join(jsonLevels, ",") != strings.Join(wantStrings, ",") {
					t.Errorf("JSON levels = %v, want %v", jsonLevels, wantStrings)
				}
				var recLevels []slog.Level
				for _, r := range rec.Records() {
					recLevels = append(recLevels, r.Level)
				}
				if fmt.Sprint(recLevels) != fmt.Sprint(want) {
					t.Errorf("recorder levels = %v, want %v", recLevels, want)
				}
			})
		}
	}
}

func TestFanout_Conformance(t *testing.T) {
	t.Run("alone", func(t *testing.T) {
		testConformance(t, func(next slog.Handler) slog.Handler { return slogx.Fanout(next) })
	})
	t.Run("under New", func(t *testing.T) {
		testConformance(t, func(next slog.Handler) slog.Handler {
			return must(slogx.New(slogx.Fanout(next),
				slogx.WithDefaultLevel(slog.LevelDebug),
				slogx.WithContextFunc(alwaysAttr),
				slogx.WithRedactKeys("password"),
			))
		})
	})
}

// TestMultiHandler_AppliesOutputLevels pins the behavior that the documentation
// recommends slog.NewMultiHandler for, and the reason Fanout exists: each output
// applies its own level, even to a record that a request enabled.
func TestMultiHandler_AppliesOutputLevels(t *testing.T) {
	var buf bytes.Buffer
	rec := slogxtest.NewRecorder()
	logger := slog.New(must(slogx.New(slog.NewMultiHandler(newJSON(&buf, nil), rec), defaultInfo())))
	logger.DebugContext(slogx.WithLevel(context.Background(), slog.LevelDebug), "m")
	if buf.Len() != 0 || rec.Len() != 1 {
		t.Errorf("JSON output %q, recorder %d records; want nothing and 1", buf.String(), rec.Len())
	}
}

func TestAlloc_FanoutDisabled(t *testing.T) {
	h := must(slogx.New(slogx.Fanout(newJSON(&bytes.Buffer{}, nil), slogxtest.NewRecorder()),
		defaultInfo(),
		slogx.WithContextFunc(ridAttrs),
		slogx.WithRedactKeys("token"),
	))
	assertNoAllocsWhenDisabled(t, h, requestContext(), slogx.WithLevel(requestContext(), slog.LevelWarn))
}

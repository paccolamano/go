package slogx_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/paccolamano/go/slogx"
	"github.com/paccolamano/go/slogx/slogxtest"
)

const summaryMessage = "records dropped by sampling"

// samplingLogger returns a recorder and a logger that samples into it.
func samplingLogger(opts ...slogx.Option) (*slogxtest.Recorder, *slog.Logger, *slogx.SamplingHandler) {
	rec := slogxtest.NewRecorder()
	h := must(slogx.NewSamplingHandler(rec, opts...))
	return rec, slog.New(h), h
}

// summaries returns the summary records among records.
func summaries(records []slogxtest.Record) []slogxtest.Record {
	var out []slogxtest.Record
	for _, r := range records {
		if r.Message == summaryMessage {
			out = append(out, r)
		}
	}
	return out
}

// checkSummary checks the content of a summary record.
func checkSummary(t *testing.T, r slogxtest.Record, level slog.Level, msg string, dropped int64, start, end time.Time) {
	t.Helper()
	if r.Message != summaryMessage || r.Level != level {
		t.Errorf("summary = %q at %v, want %q at %v", r.Message, r.Level, summaryMessage, level)
	}
	if v, _ := r.Attr("sampling", "msg"); v.String() != msg {
		t.Errorf("sampling.msg = %v, want %q", v, msg)
	}
	if v, _ := r.Attr("sampling", "dropped"); v.Int64() != dropped {
		t.Errorf("sampling.dropped = %v, want %d", v, dropped)
	}
	if v, _ := r.Attr("sampling", "start"); !v.Time().Equal(start) {
		t.Errorf("sampling.start = %v, want %v", v, start)
	}
	if v, _ := r.Attr("sampling", "end"); !v.Time().Equal(end) {
		t.Errorf("sampling.end = %v, want %v", v, end)
	}
}

func TestSamplingHandler_Conformance(t *testing.T) {
	testConformance(t, func(next slog.Handler) slog.Handler {
		return must(slogx.NewSamplingHandler(next, slogx.WithSampling(1000, 1, time.Hour)))
	})
}

func TestSamplingHandler_Burst(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		rec, logger, _ := samplingLogger(slogx.WithSampling(10, 100, time.Second))
		ctx := context.Background()
		start := time.Now()
		for range 10_000 {
			logger.WarnContext(ctx, "db query failed")
		}
		if n := rec.Len(); n != 109 {
			t.Fatalf("written = %d, want 109", n)
		}
		time.Sleep(time.Second)
		logger.WarnContext(ctx, "db query failed")
		records := rec.Records()
		if len(records) != 111 {
			t.Fatalf("records = %d, want 109 + summary + 1", len(records))
		}
		checkSummary(t, records[109], slog.LevelWarn, "db query failed", 9891, start, start.Add(time.Second))
		if !records[109].Time.Equal(start.Add(time.Second)) {
			t.Errorf("summary time = %v, want the emission time", records[109].Time)
		}
		if records[110].Message != "db query failed" {
			t.Errorf("record after the summary = %q", records[110].Message)
		}
	})
}

func TestSamplingHandler_Concurrent(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		rec, logger, _ := samplingLogger(slogx.WithSampling(10, 100, time.Second))
		start := time.Now()
		var wg sync.WaitGroup
		for range 50 {
			wg.Go(func() {
				for range 200 {
					logger.Warn("db query failed")
				}
			})
		}
		wg.Wait()
		if n := rec.Len(); n != 109 {
			t.Fatalf("written = %d, want 109", n)
		}
		time.Sleep(time.Second)
		logger.Info("next")
		sums := summaries(rec.Records())
		if len(sums) != 1 {
			t.Fatalf("summaries = %d, want 1", len(sums))
		}
		checkSummary(t, sums[0], slog.LevelWarn, "db query failed", 9891, start, start.Add(time.Second))
	})
}

func TestSamplingHandler_Errors(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		rec, logger, _ := samplingLogger(slogx.WithSampling(10, 100, time.Second))
		for range 500 {
			logger.Error("failed")
		}
		time.Sleep(time.Second)
		logger.Error("failed")
		if n, s := rec.Len(), len(summaries(rec.Records())); n != 501 || s != 0 {
			t.Errorf("written = %d with %d summaries, want 501 and none", n, s)
		}

		rec, logger, _ = samplingLogger(slogx.WithSampling(10, 100, time.Second), slogx.WithSampleErrors())
		for range 500 {
			logger.Error("failed")
		}
		if n := rec.Len(); n != 14 {
			t.Errorf("with WithSampleErrors: written = %d, want 14", n)
		}
	})
}

func TestSamplingHandler_NoDrops(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		rec, logger, _ := samplingLogger(slogx.WithSampling(10, 100, time.Second))
		for range 10 {
			logger.Warn("m")
		}
		time.Sleep(time.Second)
		logger.Warn("m")
		if n, s := rec.Len(), len(summaries(rec.Records())); n != 11 || s != 0 {
			t.Errorf("written = %d with %d summaries, want 11 and none", n, s)
		}
	})
}

func TestSamplingHandler_GroupKey(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		rec, logger, _ := samplingLogger(slogx.WithSampling(1, 1000, time.Second))
		for i := range 5 {
			logger.Warn("m", "i", i)
			logger.Info("m", "i", i)
		}
		logger.With("k", "v").Warn("m")
		logger.WithGroup("g").Warn("m", "a", 1)
		if n := rec.Len(); n != 2 {
			t.Fatalf("written = %d, want one per level", n)
		}
		start := time.Now()
		time.Sleep(time.Second)
		logger.Debug("next")
		sums := summaries(rec.Records())
		if len(sums) != 2 {
			t.Fatalf("summaries = %d, want 2", len(sums))
		}
		checkSummary(t, sums[0], slog.LevelInfo, "m", 4, start, start.Add(time.Second))
		checkSummary(t, sums[1], slog.LevelWarn, "m", 6, start, start.Add(time.Second))
	})
}

func TestSamplingHandler_SummaryOrder(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		rec, logger, _ := samplingLogger(slogx.WithSampling(1, 1000, time.Second))
		for range 3 {
			logger.Warn("b")
			logger.Warn("a")
			logger.Info("a")
		}
		time.Sleep(time.Second)
		logger.Info("next")
		var got []string
		for _, r := range summaries(rec.Records()) {
			msg, _ := r.Attr("sampling", "msg")
			got = append(got, msg.String()+"/"+r.Level.String())
		}
		if strings.Join(got, ",") != "a/INFO,a/WARN,b/WARN" {
			t.Errorf("summary order = %v, want a/INFO, a/WARN, b/WARN", got)
		}
	})
}

func TestFlush_EmitsPending(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		rec, logger, h := samplingLogger(slogx.WithSampling(1, 1000, time.Second))
		start := time.Now()
		for range 5 {
			logger.Warn("m")
		}
		time.Sleep(300 * time.Millisecond)
		if err := h.Flush(context.Background()); err != nil {
			t.Fatal(err)
		}
		sums := summaries(rec.Records())
		if len(sums) != 1 {
			t.Fatalf("summaries = %d, want 1", len(sums))
		}
		checkSummary(t, sums[0], slog.LevelWarn, "m", 4, start, start.Add(300*time.Millisecond))
		if err := h.Flush(context.Background()); err != nil {
			t.Fatal(err)
		}
		if n := len(summaries(rec.Records())); n != 1 {
			t.Errorf("a second Flush emitted %d more summaries", n-1)
		}
		logger.Warn("m")
		if n := rec.Len(); n != 3 {
			t.Errorf("after Flush a new interval starts: records = %d, want 3", n)
		}
	})
	rec, _, h := samplingLogger(slogx.WithSampling(1, 1000, time.Second))
	if err := h.Flush(context.Background()); err != nil || rec.Len() != 0 {
		t.Errorf("Flush before any record = %v with %d records, want nil and none", err, rec.Len())
	}
}

func TestSamplingHandler_SummaryNoRequestAttrs(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		rec := slogxtest.NewRecorder()
		h := must(slogx.New(rec, slogx.WithContextFunc(ridAttrs), slogx.WithSampling(10, 100, time.Second)))
		logger := slog.New(h).WithGroup("g").With("k", "v")
		ctx := slogx.With(withRID(context.Background(), "abc"), slog.String("tenant", "t1"))
		for range 200 {
			logger.WarnContext(ctx, "db query failed")
		}
		time.Sleep(time.Second)
		logger.WarnContext(ctx, "db query failed")
		sums := summaries(rec.Records())
		if len(sums) != 1 {
			t.Fatalf("summaries = %d, want 1", len(sums))
		}
		for _, path := range [][]string{{"request_id"}, {"tenant"}, {"k"}, {"g"}} {
			if v, ok := sums[0].Attr(path...); ok {
				t.Errorf("summary has %v = %v", path, v)
			}
		}
		if v, ok := sums[0].Attr("sampling", "dropped"); !ok || v.Int64() != 190-1 {
			t.Errorf("sampling.dropped = %v, %v; want 189 at the top level", v, ok)
		}
	})
}

func TestSamplingHandler_ErrorsJoined(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		errFake := errors.New("next failed")
		h := must(slogx.NewSamplingHandler(&recordingHandler{err: errFake}, slogx.WithSampling(1, 1000, time.Second)))
		ctx := context.Background()
		if err := h.Handle(ctx, recordWith()); !errors.Is(err, errFake) {
			t.Errorf("written record: err = %v, want errFake", err)
		}
		if err := h.Handle(ctx, recordWith()); err != nil {
			t.Errorf("dropped record: err = %v, want nil", err)
		}
		time.Sleep(time.Second)
		if err := h.Handle(ctx, recordWith(slog.String("other", "record"))); !errors.Is(err, errFake) {
			t.Errorf("summary emission: err = %v, want errFake", err)
		}
		if err := h.Handle(ctx, recordWith()); err != nil {
			t.Errorf("dropped record: err = %v, want nil", err)
		}
		if err := h.Flush(ctx); !errors.Is(err, errFake) {
			t.Errorf("Flush: err = %v, want errFake", err)
		}
	})
}

func TestSamplingHandler_Options(t *testing.T) {
	next := newJSON(&bytes.Buffer{}, nil)
	sampling := slogx.WithSampling(1, 1, time.Second)
	tests := []struct {
		name  string
		build func() (slog.Handler, error)
	}{
		{"missing required option", func() (slog.Handler, error) { return slogx.NewSamplingHandler(next) }},
		{"first below 1", func() (slog.Handler, error) {
			return slogx.NewSamplingHandler(next, slogx.WithSampling(0, 1, time.Second))
		}},
		{"thereafter below 1", func() (slog.Handler, error) {
			return slogx.NewSamplingHandler(next, slogx.WithSampling(1, 0, time.Second))
		}},
		{"zero interval", func() (slog.Handler, error) { return slogx.NewSamplingHandler(next, slogx.WithSampling(1, 1, 0)) }},
		{"sample errors alone", func() (slog.Handler, error) { return slogx.NewSamplingHandler(next, slogx.WithSampleErrors()) }},
		{"New with sample errors alone", func() (slog.Handler, error) { return slogx.New(next, slogx.WithSampleErrors()) }},
		{"inapplicable option", func() (slog.Handler, error) {
			return slogx.NewSamplingHandler(next, sampling, slogx.WithRedactKeys("x"))
		}},
		{"nil next", func() (slog.Handler, error) { return slogx.NewSamplingHandler(nil, sampling) }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := tt.build(); !errors.Is(err, slogx.ErrInvalidConfig) {
				t.Errorf("err = %v, want ErrInvalidConfig", err)
			}
		})
	}
	h := must(slogx.NewSamplingHandler(next, sampling))
	if h.WithGroup("") != slog.Handler(h) || h.WithAttrs(nil) != slog.Handler(h) {
		t.Error(`WithGroup("") or WithAttrs(nil) did not return the receiver`)
	}
	if !h.Enabled(context.Background(), slog.LevelInfo) || h.Enabled(context.Background(), slog.LevelDebug) {
		t.Error("Enabled does not forward to the next handler")
	}
}

func TestNew_WithSampling(t *testing.T) {
	var buf bytes.Buffer
	opts := append(composedOptions(), slogx.WithSampling(10, 100, time.Second))
	logger := slog.New(must(slogx.New(newJSON(&buf, slog.LevelInfo), opts...)))
	ctx := slogx.WithLevel(slogx.With(withRID(context.Background(), "abc"), slog.String("token", "s3cr3t")), slog.LevelDebug)
	logger.DebugContext(ctx, "cache miss")
	if got, want := onlyLine(t, &buf), `{"level":"DEBUG","msg":"cache miss","token":"[REDACTED]","request_id":"abc"}`; got != want {
		t.Errorf("record = %s\nwant      %s", got, want)
	}

	synctest.Test(t, func(t *testing.T) {
		rec := slogxtest.NewRecorder()
		h := must(slogx.New(rec, slogx.WithSampling(1, 1000, time.Second)))
		logger := slog.New(h).With("a", 1)
		for range 3 {
			logger.Warn("m")
		}
		if err := h.Flush(context.Background()); err != nil {
			t.Fatal(err)
		}
		if n := len(summaries(rec.Records())); n != 1 {
			t.Errorf("Handler.Flush emitted %d summaries, want 1", n)
		}
	})

	plain := must(slogx.New(newJSON(&bytes.Buffer{}, nil)))
	if err := plain.Flush(context.Background()); err != nil {
		t.Errorf("Flush without sampling = %v, want nil", err)
	}
}

func TestCompose_EquivalentToManualWithSampling(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		sampling := slogx.WithSampling(2, 5, time.Second)
		var composed, manual bytes.Buffer
		composedHandler := must(slogx.New(newJSON(&composed, slog.LevelInfo), append(composedOptions(), sampling)...))
		r := must(slogx.NewRedactHandler(newJSON(&manual, slog.LevelInfo), slogx.WithRedactKeys(sensitiveKeys...)))
		c := must(slogx.NewContextHandler(r, slogx.WithContextFunc(ridAttrs)))
		s := must(slogx.NewSamplingHandler(c, sampling))
		manualHandler := must(slogx.NewLevelHandler(s))
		composedLogger, manualLogger := slog.New(composedHandler), slog.New(manualHandler)
		for range 12 {
			for _, log := range composedCases() {
				log(composedLogger)
				log(manualLogger)
			}
		}
		time.Sleep(time.Second)
		composedLogger.Info("after")
		manualLogger.Info("after")
		for range 12 {
			composedLogger.Warn("again")
			manualLogger.Warn("again")
		}
		if err := composedHandler.Flush(context.Background()); err != nil {
			t.Fatal(err)
		}
		if err := s.Flush(context.Background()); err != nil {
			t.Fatal(err)
		}
		if composed.String() != manual.String() {
			t.Errorf("composed output:\n%s\nmanual output:\n%s", composed.String(), manual.String())
		}
		if n := strings.Count(composed.String(), summaryMessage); n < 8 {
			t.Errorf("only %d summaries; the cases do not exercise sampling", n)
		}
	})
}

func TestAlloc_SamplingHandlerDisabled(t *testing.T) {
	h := must(slogx.NewSamplingHandler(newJSON(&bytes.Buffer{}, nil), slogx.WithSampling(10, 100, time.Second)))
	assertNoAllocsWhenDisabled(t, h, context.Background())
	full := must(slogx.New(newJSON(&bytes.Buffer{}, nil), slogx.WithContextFunc(ridAttrs), slogx.WithRedactKeys("token"), slogx.WithSampling(10, 100, time.Second)))
	assertNoAllocsWhenDisabled(t, full, requestContext(), slogx.WithLevel(requestContext(), slog.LevelWarn))
}

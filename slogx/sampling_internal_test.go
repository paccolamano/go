package slogx

import (
	"context"
	"log/slog"
	"strconv"
	"sync/atomic"
	"testing"
	"time"
)

// countingHandler counts the records it receives.
type countingHandler struct{ n atomic.Int64 }

func (h *countingHandler) Enabled(context.Context, slog.Level) bool { return true }

func (h *countingHandler) Handle(context.Context, slog.Record) error {
	h.n.Add(1)
	return nil
}

func (h *countingHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *countingHandler) WithGroup(string) slog.Handler      { return h }

func TestSampler_Bound(t *testing.T) {
	next := &countingHandler{}
	h, err := NewSamplingHandler(next, WithSampling(1, 1000, time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	for i := range 1_000_000 {
		h.s.decide(slog.LevelInfo, strconv.Itoa(i), now)
		if i%10_000 == 0 && h.s.trackedGroups() > maxGroups {
			t.Fatalf("after %d messages: %d groups tracked, want at most %d", i+1, h.s.trackedGroups(), maxGroups)
		}
	}
	if n := h.s.trackedGroups(); n != maxGroups {
		t.Fatalf("groups tracked = %d, want %d", n, maxGroups)
	}

	ctx := context.Background()
	logger := slog.New(h)
	for range 50 {
		logger.InfoContext(ctx, "untracked")
	}
	if n := next.n.Load(); n != 50 {
		t.Errorf("untracked group: %d of 50 records written, want all", n)
	}
	for range 50 {
		logger.InfoContext(ctx, "0") // tracked by the loop above
	}
	if n := next.n.Load(); n != 50 {
		t.Errorf("tracked group: %d records written, want none beyond its first", n-50)
	}
}

func TestSampler_Rule(t *testing.T) {
	s := newSampler(&countingHandler{}, 3, 4, time.Hour, false)
	now := time.Now()
	var passed []int
	for n := 1; n <= 16; n++ {
		if pass, _ := s.decide(slog.LevelWarn, "m", now); pass {
			passed = append(passed, n)
		}
	}
	if got, want := passed, []int{1, 2, 3, 7, 11, 15}; len(got) != len(want) || got[3] != 7 || got[5] != 15 {
		t.Errorf("passed = %v, want %v", got, want)
	}
	for _, total := range []int{4, 7, 100, 1001} {
		s := newSampler(&countingHandler{}, 3, 4, time.Hour, false)
		written := 0
		for range total {
			if pass, _ := s.decide(slog.LevelWarn, "m", now); pass {
				written++
			}
		}
		if want := 3 + (total-3)/4; written != want {
			t.Errorf("T=%d: written = %d, want %d", total, written, want)
		}
	}
}

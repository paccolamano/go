package sse_test

import (
	"errors"
	"strconv"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/paccolamano/go/sse"
)

// TestServe_StalledSubscribers checks that publishing never waits for
// subscribers, and that subscribers who stop reading are disconnected while an
// active one receives everything (SC-005).
func TestServe_StalledSubscribers(t *testing.T) {
	e2e(t, func(e *env) {
		t := e.T
		const stalled, total = 999, 10_000
		b := newBroker(t)
		results := serveBroker(e, b)
		for range stalled {
			e.Get(t, t.Context(), "/?topics=t", nil) // never read
		}
		active, _ := subscribe(e, t.Context(), "t", nil)

		data := strings.Repeat("x", 256)
		start := time.Now()
		for i := range total {
			mustPublish(t, b, sse.Event{ID: strconv.Itoa(i), Data: data}, "t")
			if i%32 == 31 {
				synctest.Wait() // let the active subscriber drain its queue
			}
		}
		if waited := time.Since(start); waited != 0 {
			t.Fatalf("publishing waited %v for subscribers", waited)
		}
		for i, ev := range receive(t, active, total) {
			if ev.ID != strconv.Itoa(i) {
				t.Fatalf("event %d has ID %q", i, ev.ID)
			}
		}

		time.Sleep(11 * time.Second) // stalled writes reach their deadline
		for range stalled {
			if err := <-results; !errors.Is(err, sse.ErrSlowConsumer) {
				t.Fatalf("stalled Serve = %v, want ErrSlowConsumer", err)
			}
		}
	})
}

package sse_test

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"testing"
	"testing/synctest"
	"time"

	"github.com/paccolamano/go/sse"
)

func lastEventID(id string) http.Header {
	return http.Header{"Last-Event-Id": {id}}
}

// connection is an open subscription whose events are decoded.
type connection struct {
	events <-chan sse.Event
	cancel context.CancelFunc
}

func connect(e *env, topics string, header http.Header) connection {
	e.T.Helper()
	ctx, cancel := context.WithCancel(e.T.Context())
	events, _ := subscribe(e, ctx, topics, header)
	return connection{events: events, cancel: cancel}
}

// disconnect closes the connection and returns the events it had not consumed.
func (c connection) disconnect() []sse.Event {
	c.cancel()
	var rest []sse.Event
	for ev := range c.events {
		rest = append(rest, ev)
	}
	return rest
}

// firstBlock returns the raw lines up to the first empty line.
func firstBlock(tb testing.TB, lines <-chan string) []string {
	tb.Helper()
	timeout := time.NewTimer(virtualTimeout)
	defer timeout.Stop()
	var out []string
	for {
		select {
		case line, ok := <-lines:
			if !ok {
				tb.Fatal("stream ended before the end of the block")
			}
			if line == "" {
				return out
			}
			out = append(out, line)
		case <-timeout.C:
			tb.Fatalf("no complete block within %v, got %q", virtualTimeout, out)
		}
	}
}

func publishIDs(tb testing.TB, b *sse.Broker, topic string, ids ...string) {
	tb.Helper()
	for _, id := range ids {
		mustPublish(tb, b, sse.Event{ID: id, Data: "d"}, topic)
	}
}

func eIDs(from, to int) []string {
	out := make([]string, 0, to-from+1)
	for i := from; i <= to; i++ {
		out = append(out, fmt.Sprintf("e%d", i))
	}
	return out
}

func TestResume_FromHistory(t *testing.T) {
	e2e(t, func(e *env) {
		t := e.T
		b := newBroker(t)
		serveBroker(e, b)
		c := connect(e, "t", nil)
		publishIDs(t, b, "t", eIDs(1, 5)...)
		receive(t, c.events, 5)
		c.disconnect()
		publishIDs(t, b, "t", eIDs(6, 9)...)

		events, _ := subscribe(e, t.Context(), "t", lastEventID("e5"))
		if got := ids(receive(t, events, 4)); !slices.Equal(got, eIDs(6, 9)) {
			t.Fatalf("resumed with %v", got)
		}
		publishIDs(t, b, "t", "e10")
		if got := receive(t, events, 1)[0].ID; got != "e10" {
			t.Fatalf("live event %q", got)
		}
		assertNone(t, events)
	})
}

func TestResume_TopicsOnly(t *testing.T) {
	e2e(t, func(e *env) {
		t := e.T
		b := newBroker(t)
		serveBroker(e, b)
		publishIDs(t, b, "t", eIDs(1, 6)...)
		publishIDs(t, b, "other", "e7")
		publishIDs(t, b, "t", "e8", "e9")
		events, _ := subscribe(e, t.Context(), "t", lastEventID("e5"))
		if got := ids(receive(t, events, 3)); !slices.Equal(got, []string{"e6", "e8", "e9"}) {
			t.Fatalf("resumed with %v", got)
		}
		assertNone(t, events)
	})
}

func TestPosition_NewSubscription(t *testing.T) {
	e2e(t, func(e *env) {
		t := e.T
		b := newBroker(t)
		serveBroker(e, b)
		publishIDs(t, b, "t", eIDs(1, 9)...)
		ctx, cancel := context.WithCancel(t.Context())
		lines := readLines(t, e.Get(t, ctx, "/?topics=t", nil).Body)
		if got := firstBlock(t, lines); !slices.Equal(got, []string{"id: e9"}) {
			t.Fatalf("first block %q, want the position", got)
		}
		cancel() // gone before any event
		publishIDs(t, b, "t", "e10")
		events, _ := subscribe(e, t.Context(), "t", lastEventID("e9"))
		if got := receive(t, events, 1)[0].ID; got != "e10" {
			t.Fatalf("resumed with %q", got)
		}
	})
	e2e(t, func(e *env) {
		t := e.T
		b := newBroker(t, sse.WithKeepAlive(5*time.Second))
		serveBroker(e, b)
		lines := readLines(t, e.Get(t, t.Context(), "/?topics=t", nil).Body)
		time.Sleep(5 * time.Second)
		if line := <-lines; line != ": keep-alive" {
			t.Fatalf("empty history: first line %q, want no position", line)
		}
	})
}

func TestResume_Unresolvable(t *testing.T) {
	e2e(t, func(e *env) {
		t := e.T
		b := newBroker(t)
		serveBroker(e, b)
		publishIDs(t, b, "t", eIDs(1, 5)...)
		lines := readLines(t, e.Get(t, t.Context(), "/?topics=t", lastEventID("gone")).Body)
		if got := firstBlock(t, lines); !slices.Equal(got, []string{"id: e5"}) {
			t.Fatalf("first block %q, want the position", got)
		}
		publishIDs(t, b, "t", "e6")
		if got := firstBlock(t, lines); !slices.Equal(got, []string{"id: e6", "data: d"}) {
			t.Fatalf("next block %q, want only the new event", got)
		}
	})
}

func TestReset_Sent(t *testing.T) {
	e2e(t, func(e *env) {
		t := e.T
		b := newBroker(t, sse.WithResetEvent(sse.Event{Type: "reset", ID: "ignored"}))
		serveBroker(e, b)
		publishIDs(t, b, "t", eIDs(1, 5)...)
		c := connect(e, "t", lastEventID("gone"))
		if ev := receive(t, c.events, 1)[0]; ev.Type != "reset" || ev.ID != "e5" {
			t.Fatalf("first event %+v, want the reset with ID e5", ev)
		}
		publishIDs(t, b, "t", "e6")
		if got := receive(t, c.events, 1)[0].ID; got != "e6" {
			t.Fatalf("live event %q", got)
		}
		c.disconnect()

		events, _ := subscribe(e, t.Context(), "t", lastEventID("e5"))
		if got := receive(t, events, 1)[0]; got.Type == "reset" || got.ID != "e6" {
			t.Fatalf("reconnecting with the reset's ID gave %+v", got)
		}
	})
}

func TestReset_EmptyHistory(t *testing.T) {
	e2e(t, func(e *env) {
		t := e.T
		b := newBroker(t, sse.WithResetEvent(sse.Event{Type: "reset"}))
		serveBroker(e, b)
		lines := readLines(t, e.Get(t, t.Context(), "/?topics=t", lastEventID("gone")).Body)
		if got := firstBlock(t, lines); !slices.Equal(got, []string{"event: reset", "id:", "data:"}) {
			t.Fatalf("reset block %q", got)
		}
	})
}

func TestResume_Restart(t *testing.T) {
	e2e(t, func(e *env) {
		t := e.T
		old, oldEnv := newBroker(t), newEnv(t, e.H2) // the previous process
		serveBroker(oldEnv, old)
		events, _ := subscribe(oldEnv, t.Context(), "t", nil)
		mustPublish(t, old, sse.Event{Data: "before the restart"}, "t")
		oldID := receive(t, events, 1)[0].ID

		b := newBroker(t)
		serveBroker(e, b)
		publishIDs(t, b, "t", eIDs(1, 3)...)
		lines := readLines(t, e.Get(t, t.Context(), "/?topics=t", lastEventID(oldID)).Body)
		if got := firstBlock(t, lines); !slices.Equal(got, []string{"id: e3"}) {
			t.Fatalf("first block %q, want the position", got)
		}
	})
}

func TestResume_HistoryBound(t *testing.T) {
	e2e(t, func(e *env) {
		t := e.T
		b := newBroker(t, sse.WithHistory(3))
		serveBroker(e, b)
		publishIDs(t, b, "t", eIDs(1, 10)...)
		events, _ := subscribe(e, t.Context(), "t", lastEventID("e8"))
		if got := ids(receive(t, events, 2)); !slices.Equal(got, []string{"e9", "e10"}) {
			t.Fatalf("resumed with %v", got)
		}
		lines := readLines(t, e.Get(t, t.Context(), "/?topics=t", lastEventID("e5")).Body)
		if got := firstBlock(t, lines); !slices.Equal(got, []string{"id: e10"}) {
			t.Fatalf("evicted ID: first block %q, want the position", got)
		}
	})
}

func TestResume_EmptyHeader(t *testing.T) {
	e2e(t, func(e *env) {
		t := e.T
		b := newBroker(t)
		serveBroker(e, b)
		publishIDs(t, b, "t", eIDs(1, 3)...)
		lines := readLines(t, e.Get(t, t.Context(), "/?topics=t", lastEventID("")).Body)
		if got := firstBlock(t, lines); !slices.Equal(got, []string{"id: e3"}) {
			t.Fatalf("first block %q, want the position of a new subscription", got)
		}
	})
}

func TestResume_OtherTopicID(t *testing.T) {
	e2e(t, func(e *env) {
		t := e.T
		b := newBroker(t)
		serveBroker(e, b)
		publishIDs(t, b, "t", "e1")
		publishIDs(t, b, "other", "e2")
		publishIDs(t, b, "t", "e3")
		publishIDs(t, b, "other", "e4")
		events, _ := subscribe(e, t.Context(), "t", lastEventID("e2"))
		if got := ids(receive(t, events, 1)); !slices.Equal(got, []string{"e3"}) {
			t.Fatalf("resumed with %v", got)
		}
		assertNone(t, events)
	})
}

// seeded is a small deterministic generator (SplitMix64). The randomized tests
// need sequences that a seed reproduces, which crypto/rand cannot give, and
// the security scanners forbid math/rand.
type seeded struct{ state uint64 }

func newSeeded(seed uint64) *seeded { return &seeded{state: seed} }

// IntN returns a number in [0, n).
func (s *seeded) IntN(n int) int {
	s.state += 0x9e3779b97f4a7c15
	z := s.state
	z = (z ^ (z >> 30)) * 0xbf58476d1ce4e5b9
	z = (z ^ (z >> 27)) * 0x94d049bb133111eb
	return int((z ^ (z >> 31)) % uint64(n))
}

// anyPoint runs the scenario of SC-003 for one seed: 10,000 events with
// IDs bus-0…bus-9999 on random topics among a, b and c, a client following a
// and b that disconnects after a random event, and a reconnection with its last
// identifier. publish sends an event to every broker involved; first opens the
// first connection; second reconnects with the last identifier.
func anyPoint(tb testing.TB, seed uint64, publish func(sse.Event, string), first func() connection, second func(lastID string) <-chan sse.Event) {
	tb.Helper()
	const n = 10_000
	rng := newSeeded(seed)
	topics := make([]string, n)
	for i := range topics {
		topics[i] = [...]string{"a", "b", "c"}[rng.IntN(3)]
	}
	followed := func(i int) bool { return topics[i] != "c" }

	// Published before the first connection, the warm-up event is not
	// delivered: it becomes the connection's position, so the client has an
	// identifier to resume from even if it leaves before its first event.
	publish(sse.Event{ID: "warm-up"}, "a")
	c := first()
	cut := rng.IntN(n)
	var want []string
	for i := range n {
		id := fmt.Sprintf("bus-%d", i)
		if i == cut {
			synctest.Wait()
			got := ids(c.disconnect())
			if !slices.Equal(got, want) {
				tb.Fatalf("seed %d: before the disconnection got %d events, want %d", seed, len(got), len(want))
			}
			want = nil
		}
		publish(sse.Event{ID: id, Data: "d"}, topics[i])
		if followed(i) {
			want = append(want, id)
		}
	}
	last := "warm-up"
	for i := cut - 1; i >= 0; i-- {
		if followed(i) {
			last = fmt.Sprintf("bus-%d", i)
			break
		}
	}
	events := second(last)
	if got := ids(receive(tb, events, len(want))); !slices.Equal(got, want) {
		tb.Fatalf("seed %d: resumed events differ from the missed ones", seed)
	}
	assertNone(tb, events)
}

func TestResume_AnyPoint(t *testing.T) {
	for seed := range uint64(10) {
		t.Run(fmt.Sprint("seed", seed), func(t *testing.T) {
			e2e(t, func(e *env) {
				t := e.T
				b := newBroker(t, sse.WithHistory(10_001), sse.WithBuffer(10_001))
				serveBroker(e, b)
				anyPoint(t, seed,
					func(ev sse.Event, topic string) { mustPublish(t, b, ev, topic) },
					func() connection { return connect(e, "a,b", nil) },
					func(lastID string) <-chan sse.Event {
						events, _ := subscribe(e, t.Context(), "a,b", lastEventID(lastID))
						return events
					})
			})
		})
	}
}

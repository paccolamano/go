package sse_test

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/paccolamano/go/sse"
)

// source stands in for a durable message bus: events with their topics, in
// publication order.
type source struct {
	mu      sync.Mutex
	entries []sourceEntry
	lastIDs []string // lastID of every replay call
}

type sourceEntry struct {
	ev     sse.Event
	topics []string
}

func (s *source) add(ev sse.Event, topics ...string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries = append(s.entries, sourceEntry{ev: ev, topics: topics})
}

// from returns the index of the entry after lastID, reading the current
// length at every call, or false when lastID is unknown. It records the call.
func (s *source) from(lastID string) (int, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastIDs = append(s.lastIDs, lastID)
	for i, e := range s.entries {
		if e.ev.ID == lastID {
			return i + 1, true
		}
	}
	return 0, false
}

// at returns entry i, or false past the end.
func (s *source) at(i int) (sourceEntry, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if i >= len(s.entries) {
		return sourceEntry{}, false
	}
	return s.entries[i], true
}

func (s *source) calls() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.lastIDs)
}

// replay is a ReplayFunc over the source. It reads up to the current end, as a
// bus read would.
func (s *source) replay(_ context.Context, lastID string, _ []string, send func(sse.Event, ...string) error) error {
	i, ok := s.from(lastID)
	if !ok {
		return sse.ErrUnknownID
	}
	for ; ; i++ {
		e, ok := s.at(i)
		if !ok {
			return nil
		}
		if err := send(e.ev, e.topics...); err != nil {
			return err
		}
	}
}

// both publishes to the source, then to the broker, as a bus and its feed do.
func both(tb testing.TB, src *source, b *sse.Broker, ev sse.Event, topics ...string) {
	tb.Helper()
	src.add(ev, topics...)
	mustPublish(tb, b, ev, topics...)
}

func TestReplay_FromSource(t *testing.T) {
	e2e(t, func(e *env) {
		t := e.T
		src := &source{}
		b := newBroker(t, sse.WithHistory(1000), sse.WithReplay(src.replay))
		serveBroker(e, b)
		for _, id := range eIDs(1, 5000) {
			both(t, src, b, sse.Event{ID: id}, "t")
		}
		events, _ := subscribe(e, t.Context(), "t", lastEventID("e100"))
		if got := ids(receive(t, events, 4900)); !slices.Equal(got, eIDs(101, 5000)) {
			t.Fatalf("replayed %d events, not e101…e5000", len(got))
		}
		both(t, src, b, sse.Event{ID: "e5001"}, "t")
		if got := receive(t, events, 1)[0].ID; got != "e5001" {
			t.Fatalf("live event %q", got)
		}
		assertNone(t, events)
	})
}

func TestReplay_ConcurrentPublish(t *testing.T) {
	e2e(t, func(e *env) {
		t := e.T
		src := &source{}
		var b *sse.Broker
		replay := func(ctx context.Context, lastID string, topics []string, send func(sse.Event, ...string) error) error {
			i, ok := src.from(lastID)
			if !ok {
				return sse.ErrUnknownID
			}
			for ; i < 6; i++ { // e4…e6
				en, _ := src.at(i)
				if err := send(en.ev, en.topics...); err != nil {
					return err
				}
			}
			for _, id := range []string{"n1", "n2", "n3"} { // published meanwhile
				both(t, src, b, sse.Event{ID: id}, "t")
			}
			return src.replay(ctx, "e6", topics, send) // the rest, including n1…n3
		}
		b = newBroker(t, sse.WithHistory(2), sse.WithReplay(replay))
		serveBroker(e, b)
		for _, id := range eIDs(1, 10) {
			both(t, src, b, sse.Event{ID: id}, "t")
		}
		events, _ := subscribe(e, t.Context(), "t", lastEventID("e3"))
		want := append(eIDs(4, 10), "n1", "n2", "n3")
		if got := ids(receive(t, events, len(want))); !slices.Equal(got, want) {
			t.Fatalf("got %v, want %v", got, want)
		}
		both(t, src, b, sse.Event{ID: "after"}, "t")
		if got := receive(t, events, 1)[0].ID; got != "after" {
			t.Fatalf("live event %q", got)
		}
		assertNone(t, events)
	})
}

func TestReplay_NotCalledWhenInHistory(t *testing.T) {
	e2e(t, func(e *env) {
		t := e.T
		src := &source{}
		b := newBroker(t, sse.WithReplay(src.replay))
		serveBroker(e, b)
		for _, id := range eIDs(1, 5) {
			both(t, src, b, sse.Event{ID: id}, "t")
		}
		events, _ := subscribe(e, t.Context(), "t", lastEventID("e3"))
		if got := ids(receive(t, events, 2)); !slices.Equal(got, []string{"e4", "e5"}) {
			t.Fatalf("got %v", got)
		}
		if calls := src.calls(); len(calls) != 0 {
			t.Fatalf("replay called with %v", calls)
		}
	})
}

func TestReplay_UnknownID(t *testing.T) {
	e2e(t, func(e *env) {
		t := e.T
		src := &source{}
		b := newBroker(t, sse.WithReplay(src.replay), sse.WithResetEvent(sse.Event{Type: "reset"}))
		serveBroker(e, b)
		for _, id := range eIDs(1, 3) {
			both(t, src, b, sse.Event{ID: id}, "t")
		}
		events, _ := subscribe(e, t.Context(), "t", lastEventID("unknown"))
		if ev := receive(t, events, 1)[0]; ev.Type != "reset" || ev.ID != "e3" {
			t.Fatalf("first event %+v, want the reset", ev)
		}
		both(t, src, b, sse.Event{ID: "e4"}, "t")
		if got := receive(t, events, 1)[0].ID; got != "e4" {
			t.Fatalf("live event %q", got)
		}
	})
	e2e(t, func(e *env) {
		t := e.T
		src := &source{}
		b := newBroker(t, sse.WithReplay(src.replay))
		serveBroker(e, b)
		for _, id := range eIDs(1, 3) {
			both(t, src, b, sse.Event{ID: id}, "t")
		}
		lines := readLines(t, e.Get(t, t.Context(), "/?topics=t", lastEventID("unknown")).Body)
		if got := firstBlock(t, lines); !slices.Equal(got, []string{"id: e3"}) {
			t.Fatalf("first block %q, want the position", got)
		}
	})
}

var errBusDown = errors.New("bus down")

func TestReplay_Failure(t *testing.T) {
	e2e(t, func(e *env) {
		t := e.T
		src := &source{}
		var b *sse.Broker
		var once sync.Once
		replay := func(_ context.Context, lastID string, _ []string, send func(sse.Event, ...string) error) error {
			i, ok := src.from(lastID)
			if !ok {
				return sse.ErrUnknownID
			}
			for n := 0; n < 50; n, i = n+1, i+1 {
				en, _ := src.at(i)
				if err := send(en.ev, en.topics...); err != nil {
					return err
				}
			}
			once.Do(func() { mustPublish(t, b, sse.Event{ID: "live-1"}, "t") }) // must not be delivered
			return errBusDown
		}
		b = newBroker(t, sse.WithHistory(10), sse.WithReplay(replay), sse.WithResetEvent(sse.Event{Type: "reset"}))
		results := serveBroker(e, b)
		for _, id := range eIDs(1, 200) {
			both(t, src, b, sse.Event{ID: id}, "t")
		}
		events, end := subscribe(e, t.Context(), "t", lastEventID("e100"))
		if got := ids(receive(t, events, 50)); !slices.Equal(got, eIDs(101, 150)) {
			t.Fatalf("got %v", got)
		}
		if err := <-end; err != nil {
			t.Fatalf("the response did not end cleanly: %v", err)
		}
		for ev := range events {
			t.Fatalf("event after the failure: %+v", ev)
		}
		if err := <-results; !errors.Is(err, errBusDown) {
			t.Fatalf("Serve = %v, want the replay failure", err)
		}
		events, _ = subscribe(e, t.Context(), "t", lastEventID("e150"))
		receive(t, events, 50)
		if calls := src.calls(); calls[len(calls)-1] != "e150" {
			t.Fatalf("replay calls %v, want the last one from e150", calls)
		}
	})
}

func TestReplay_ClientGone(t *testing.T) {
	e2e(t, func(e *env) {
		t := e.T
		sendAfter := make(chan error, 1)
		replay := func(ctx context.Context, _ string, _ []string, send func(sse.Event, ...string) error) error {
			if err := send(sse.Event{ID: "r1"}, "t"); err != nil {
				return err
			}
			<-ctx.Done()
			err := send(sse.Event{ID: "r2"}, "t")
			sendAfter <- err
			return err
		}
		b := newBroker(t, sse.WithReplay(replay))
		results := serveBroker(e, b)
		c := connect(e, "t", lastEventID("old"))
		receive(t, c.events, 1)
		c.disconnect()
		if err := <-sendAfter; err == nil {
			t.Fatal("send succeeded after the client left")
		}
		if err := <-results; err != nil {
			t.Fatalf("Serve = %v, want nil", err)
		}
	})
}

func TestReplay_TopicFilter(t *testing.T) {
	e2e(t, func(e *env) {
		t := e.T
		src := &source{}
		b := newBroker(t, sse.WithHistory(1), sse.WithReplay(src.replay))
		serveBroker(e, b)
		both(t, src, b, sse.Event{ID: "start"}, "a")
		both(t, src, b, sse.Event{ID: "x1"}, "a")
		both(t, src, b, sse.Event{ID: "x2"}, "c")
		both(t, src, b, sse.Event{ID: "x3"}, "a", "b")
		both(t, src, b, sse.Event{ID: "x4"}, "c")
		both(t, src, b, sse.Event{ID: "x5"}, "b")
		events, _ := subscribe(e, t.Context(), "a,b", lastEventID("start"))
		if got := ids(receive(t, events, 3)); !slices.Equal(got, []string{"x1", "x3", "x5"}) {
			t.Fatalf("got %v", got)
		}
		assertNone(t, events)
	})
}

func TestReplay_Overflow(t *testing.T) {
	e2e(t, func(e *env) {
		t := e.T
		src := &source{}
		replay := func(_ context.Context, lastID string, _ []string, send func(sse.Event, ...string) error) error {
			i, ok := src.from(lastID)
			if !ok {
				return sse.ErrUnknownID
			}
			for ; ; i++ {
				en, ok := src.at(i)
				if !ok {
					return nil
				}
				if err := send(en.ev, en.topics...); err != nil {
					return err
				}
				time.Sleep(time.Second) // a slow bus
			}
		}
		b := newBroker(t, sse.WithHistory(1), sse.WithBuffer(4), sse.WithReplay(replay))
		results := serveBroker(e, b)
		for _, id := range eIDs(1, 20) {
			src.add(sse.Event{ID: id}, "t")
		}
		c := connect(e, "t", lastEventID("e1"))
		receive(t, c.events, 1) // e2, then the replay sleeps
		for i := range 10 {
			mustPublish(t, b, sse.Event{ID: fmt.Sprintf("live-%d", i)}, "t")
		}
		if err := <-results; !errors.Is(err, sse.ErrSlowConsumer) {
			t.Fatalf("Serve = %v, want ErrSlowConsumer", err)
		}
		got := append([]string{"e2"}, ids(c.disconnect())...)
		last := got[len(got)-1]
		again := connect(e, "t", lastEventID(last))
		receive(t, again.events, 1)
		if calls := src.calls(); calls[len(calls)-1] != last {
			t.Fatalf("replay calls %v, want the last one from %s", calls, last)
		}
		again.disconnect() // the slow replay is still running: end it before the bubble
		if err := <-results; err != nil {
			t.Fatalf("second Serve = %v", err)
		}
	})
}

func TestReplay_InvalidEvent(t *testing.T) {
	tests := []struct {
		name   string
		ev     sse.Event
		topics []string
	}{
		{"invalid ID", sse.Event{ID: "a\nb"}, []string{"t"}},
		{"no ID", sse.Event{Data: "x"}, []string{"t"}},
		{"no topic", sse.Event{ID: "x"}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e2e(t, func(e *env) {
				t := e.T
				sendErr := make(chan error, 1)
				replay := func(_ context.Context, _ string, _ []string, send func(sse.Event, ...string) error) error {
					err := send(tt.ev, tt.topics...)
					sendErr <- err
					return err
				}
				b := newBroker(t, sse.WithReplay(replay))
				results := serveBroker(e, b)
				subscribe(e, t.Context(), "t", lastEventID("old"))
				if err := <-sendErr; !errors.Is(err, sse.ErrInvalidEvent) {
					t.Fatalf("send = %v, want ErrInvalidEvent", err)
				}
				if err := <-results; !errors.Is(err, sse.ErrInvalidEvent) {
					t.Fatalf("Serve = %v, want a replay failure wrapping ErrInvalidEvent", err)
				}
			})
		})
	}
}

func TestReplay_SkipsLastID(t *testing.T) {
	e2e(t, func(e *env) {
		t := e.T
		replay := func(_ context.Context, lastID string, _ []string, send func(sse.Event, ...string) error) error {
			for _, id := range []string{lastID, "r1", "r2"} { // a source that includes lastID
				if err := send(sse.Event{ID: id}, "t"); err != nil {
					return err
				}
			}
			return nil
		}
		b := newBroker(t, sse.WithReplay(replay))
		serveBroker(e, b)
		events, _ := subscribe(e, t.Context(), "t", lastEventID("old"))
		if got := ids(receive(t, events, 2)); !slices.Equal(got, []string{"r1", "r2"}) {
			t.Fatalf("got %v", got)
		}
		assertNone(t, events)
	})
}

func TestReplay_Empty(t *testing.T) {
	e2e(t, func(e *env) {
		t := e.T
		replay := func(context.Context, string, []string, func(sse.Event, ...string) error) error { return nil }
		b := newBroker(t, sse.WithReplay(replay))
		serveBroker(e, b)
		events, _ := subscribe(e, t.Context(), "t", lastEventID("old"))
		mustPublish(t, b, sse.Event{ID: "live"}, "t")
		if got := receive(t, events, 1)[0].ID; got != "live" {
			t.Fatalf("got %q", got)
		}
	})
}

func TestReplay_Panic(t *testing.T) {
	e2e(t, func(e *env) {
		t := e.T
		replay := func(_ context.Context, _ string, _ []string, send func(sse.Event, ...string) error) error {
			if err := send(sse.Event{ID: "r1"}, "t"); err != nil {
				return err
			}
			panic("bug in the replay function")
		}
		b := newBroker(t, sse.WithReplay(replay))
		results := serveBroker(e, b)
		events, end := subscribe(e, t.Context(), "t", lastEventID("old"))
		if got := receive(t, events, 1)[0].ID; got != "r1" {
			t.Fatalf("got %q", got)
		}
		if err := <-end; err != nil {
			t.Fatalf("the response did not end cleanly: %v", err)
		}
		if err := <-results; err == nil || !strings.Contains(err.Error(), "replay panicked") {
			t.Fatalf("Serve = %v, want the recovered panic", err)
		}
	})
}

func TestReplay_SendAfterReturn(t *testing.T) {
	e2e(t, func(e *env) {
		t := e.T
		stored := make(chan func(sse.Event, ...string) error, 1)
		replay := func(_ context.Context, _ string, _ []string, send func(sse.Event, ...string) error) error {
			stored <- send
			return nil
		}
		b := newBroker(t, sse.WithReplay(replay))
		serveBroker(e, b)
		events, _ := subscribe(e, t.Context(), "t", lastEventID("old"))
		send := <-stored
		synctest.Wait()
		if err := send(sse.Event{ID: "late"}, "t"); err == nil {
			t.Fatal("send succeeded after the replay function returned")
		}
		assertNone(t, events)
	})
}

func TestReplay_AnyPoint(t *testing.T) {
	for seed := range uint64(10) {
		t.Run(fmt.Sprint("seed", seed), func(t *testing.T) {
			e2e(t, func(e *env) {
				t := e.T
				src := &source{}
				b := newBroker(t, sse.WithHistory(100), sse.WithBuffer(10_001), sse.WithReplay(src.replay))
				serveBroker(e, b)
				anyPoint(t, seed,
					func(ev sse.Event, topic string) { both(t, src, b, ev, topic) },
					func() connection { return connect(e, "a,b", nil) },
					func(lastID string) <-chan sse.Event {
						events, _ := subscribe(e, t.Context(), "a,b", lastEventID(lastID))
						return events
					})
			})
		})
	}
}

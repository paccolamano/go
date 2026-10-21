package sse_test

import (
	"context"
	"fmt"
	"slices"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/paccolamano/go/sse"
)

// TestInterleave_ReplayAndPublish races a replay against publishers in 1,000
// seeded runs and checks that the client receives every event after its last
// identifier exactly once, in order (SC-004).
func TestInterleave_ReplayAndPublish(t *testing.T) {
	if testing.Short() {
		t.Skip("1,000 runs")
	}
	for seed := range uint64(1000) {
		synctest.Test(t, func(t *testing.T) { interleave(t, seed) })
	}
}

func interleave(t *testing.T, seed uint64) {
	t.Helper()
	var mu sync.Mutex
	rng := newSeeded(seed)
	randN := func(n int) int { // rng is shared by the goroutines below
		mu.Lock()
		defer mu.Unlock()
		return rng.IntN(n)
	}
	pause := func() {
		if randN(2) == 0 {
			time.Sleep(time.Duration(randN(5)) * time.Millisecond)
		}
	}

	src := &source{}
	replay := func(_ context.Context, lastID string, _ []string, send func(sse.Event, ...string) error) error {
		i, ok := src.from(lastID)
		if !ok {
			return sse.ErrUnknownID
		}
		for ; ; i++ {
			pause()
			e, ok := src.at(i) // reads up to the current end, which keeps growing
			if !ok {
				return nil
			}
			if err := send(e.ev, e.topics...); err != nil {
				return err
			}
		}
	}
	b := newBroker(t, sse.WithHistory(5), sse.WithBuffer(1000), sse.WithReplay(replay))
	e := newEnv(t, false)
	results := serveBroker(e, b)

	prefix := 10 + randN(40)
	var all []string
	for i := range prefix {
		id := fmt.Sprintf("p%d", i)
		both(t, src, b, sse.Event{ID: id}, "t")
		all = append(all, id)
	}
	last := randN(prefix - 6) // older than the history of 5
	want := slices.Clone(all[last+1:])

	extra := randN(20)
	for i := range extra {
		want = append(want, fmt.Sprintf("n%d", i))
	}
	c := connect(e, "t", lastEventID(all[last]))
	var wg sync.WaitGroup
	wg.Go(func() {
		for i := range extra {
			pause()
			both(t, src, b, sse.Event{ID: fmt.Sprintf("n%d", i)}, "t")
		}
	})
	wg.Wait()
	if got := ids(receive(t, c.events, len(want))); !slices.Equal(got, want) {
		t.Fatalf("seed %d: got %v, want %v", seed, got, want)
	}
	assertNone(t, c.events)
	// The replay function may still be pausing: end the subscription and wait
	// for Serve, so that no goroutine outlives the bubble.
	c.disconnect()
	if err := <-results; err != nil {
		t.Fatalf("seed %d: Serve = %v", seed, err)
	}
}

package sse

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
)

// ErrUnknownID is returned by a ReplayFunc whose source does not know the
// identifier it was given. The broker then treats the identifier as
// unresolvable: it sends the reset event, if configured, or a position.
var ErrUnknownID = errors.New("sse: unknown event identifier")

var (
	errReplayEnded = errors.New("sse: send called after the replay function returned")
	errReplayPanic = errors.New("sse: replay panicked")
)

// ReplayFunc provides, in order, the events that follow lastID in a durable
// source, by calling send with each event and its topics. topics are those of
// the subscription and may narrow the query: the broker filters the events
// itself, so an event on other topics never reaches the client.
//
// When send returns an error, the function should stop and return it. It
// returns an error matching ErrUnknownID when the source does not know lastID,
// and any other error on failure: the stream then ends without live events and
// the client retries the replay when it reconnects. Returning nil without
// calling send means that lastID is known and nothing follows it.
//
// The broker calls it only when lastID is not in its history, with the
// request's context. No keep-alive is written while it runs, so it should
// return within the idle timeout of proxies. A panic is recovered and treated
// as a failure.
type ReplayFunc func(ctx context.Context, lastID string, topics []string,
	send func(ev Event, topics ...string) error) error

// replayer delivers the events of a replay function to one subscription
// (research R-12).
type replayer struct {
	ctx    context.Context // the request's
	s      *Stream
	sub    *subscriber
	lastID string

	mu      sync.Mutex
	ended   bool  // the replay function has returned
	stop    error // why send refuses: the client left, the queue overflowed, a write failed
	invalid error // an invalid replayed event: the replay failed
	seen    map[string]struct{}
	order   []string // the remembered identifiers, at most limit
	head    int      // index of the oldest remembered identifier once order is full
	limit   int
}

// send writes one replayed event, unless it is lastID itself or on none of the
// subscription's topics, and remembers its identifier to skip its live copy.
func (rp *replayer) send(ev Event, topics ...string) error {
	rp.mu.Lock()
	defer rp.mu.Unlock()
	if rp.ended {
		return errReplayEnded
	}
	if rp.stop != nil {
		return rp.stop
	}
	if err := checkReplayed(ev, topics); err != nil {
		rp.invalid, rp.stop = err, err
		return err
	}
	if ev.ID == rp.lastID || !sharesTopic(topics, rp.sub.topics) {
		return nil
	}
	select {
	case <-rp.sub.overflow:
		rp.stop = ErrSlowConsumer
		return rp.stop
	default:
	}
	if err := rp.s.sendValid(ev); err != nil {
		rp.stop = err
		return err
	}
	rp.remember(ev.ID)
	return nil
}

// checkReplayed reports why a replayed event cannot be sent: it must be valid,
// have an identifier and at least one topic.
func checkReplayed(ev Event, topics []string) error {
	if err := ev.validate(); err != nil {
		return err
	}
	if ev.ID == "" {
		return fmt.Errorf("%w: replayed event without an ID", ErrInvalidEvent)
	}
	if _, err := normalizeTopics(topics); err != nil {
		return fmt.Errorf("%w: replayed event %q: %w", ErrInvalidEvent, ev.ID, err)
	}
	return nil
}

// remember records id, forgetting the oldest identifier beyond limit. The
// overlap between replayed and live events is at most the queue's capacity:
// a larger one overflows the queue first.
func (rp *replayer) remember(id string) {
	if len(rp.order) < rp.limit {
		rp.order = append(rp.order, id)
	} else {
		delete(rp.seen, rp.order[rp.head])
		rp.order[rp.head] = id
		rp.head = (rp.head + 1) % rp.limit
	}
	rp.seen[id] = struct{}{}
}

// replayed reports whether id was sent by the replay.
func (rp *replayer) replayed(id string) bool {
	rp.mu.Lock()
	defer rp.mu.Unlock()
	_, ok := rp.seen[id]
	return ok
}

// call runs fn, recovering a panic, and makes later calls to send fail.
func (rp *replayer) call(fn ReplayFunc) (err error) {
	defer func() {
		if v := recover(); v != nil {
			err = fmt.Errorf("%w: %v", errReplayPanic, v)
		}
		rp.mu.Lock()
		rp.ended = true
		rp.mu.Unlock()
	}()
	return fn(rp.ctx, rp.lastID, slices.Clone(rp.sub.topics), rp.send)
}

// replay runs the replay function for a subscription whose last event
// identifier is not in the history. It returns the filter of live events that
// the replay already sent, or done with the outcome of Serve.
func (b *Broker) replay(ctx context.Context, s *Stream, sub *subscriber, res resolution) (skip func(string) bool, done bool, err error) {
	rp := &replayer{ctx: ctx, s: s, sub: sub, lastID: res.lastID, seen: make(map[string]struct{}), limit: b.cfg.buffer}
	callErr := rp.call(b.cfg.replay)
	rp.mu.Lock()
	stop, invalid := rp.stop, rp.invalid
	rp.mu.Unlock()

	select {
	case <-sub.overflow:
		return nil, true, ErrSlowConsumer
	default:
	}
	switch {
	case ctx.Err() != nil:
		return nil, true, nil
	case invalid != nil:
		return nil, true, fmt.Errorf("sse: replay: %w", invalid)
	case stop != nil:
		return nil, true, b.writeOutcome(ctx, sub, stop)
	case callErr == nil:
		return rp.replayed, false, nil
	case errors.Is(callErr, ErrUnknownID):
		if err := s.write(b.unresolvedBlock(res.latest)); err != nil {
			return nil, true, b.writeOutcome(ctx, sub, err)
		}
		return nil, false, nil
	case errors.Is(callErr, errReplayPanic):
		return nil, true, callErr
	}
	return nil, true, fmt.Errorf("sse: replay: %w", callErr)
}

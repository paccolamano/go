package sse

import (
	"context"
	"errors"
	"net/http"
	"os"
	"time"
)

// ErrSlowConsumer is returned by Serve when the subscriber's queue overflowed
// because the client did not read fast enough. The client resumes from the
// history when it reconnects.
var ErrSlowConsumer = errors.New("sse: subscriber too slow")

// subscriber is one subscription. Every field except the channels is guarded
// by the broker's mutex.
type subscriber struct {
	topics     []string
	queue      chan entry
	overflow   chan struct{} // closed once when queue was full
	lastSeq    uint64        // last sequence offered, for exactly-once delivery across topics
	removed    bool
	overflowed bool
}

// Serve subscribes the request to topics and streams their events until the
// client disconnects (nil), the broker is closed (ErrClosed), the subscriber
// falls behind (ErrSlowConsumer), or a write fails. The response always
// completes with status 200, so that browsers reconnect.
//
// The subscription is registered before the response headers are sent: every
// event published once the client has received them is delivered. When the
// request carries a Last-Event-ID, the missed events are sent first, from the
// history or from the replay function.
//
// Choose topics from the authenticated identity of the request, never from
// unchecked client input: the broker does not decide who may follow what.
// Without topics, or when the writer cannot flush, Serve returns an error and
// writes nothing, so that the handler can answer it.
func (b *Broker) Serve(w http.ResponseWriter, r *http.Request, topics ...string) error {
	ts, err := normalizeTopics(topics)
	if err != nil {
		return err
	}
	if !canFlush(w) {
		return ErrStreamingUnsupported
	}
	sub := &subscriber{topics: ts, queue: make(chan entry, b.cfg.buffer), overflow: make(chan struct{})}

	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		// An empty stream, never another status: browsers give up for good on
		// anything but 200, while a normal end makes them reconnect elsewhere.
		_, _ = newStream(w, r, b.cfg, b.firstBlock(nil))
		return ErrClosed
	}
	// Registering and resolving under one lock makes the snapshot and the
	// queue contiguous: no event is lost or repeated between them.
	b.addLocked(sub)
	res := b.resolveLocked(r.Header.Get("Last-Event-ID"), sub)
	b.mu.Unlock()
	defer b.unregister(sub)

	s, err := newStream(w, r, b.cfg, b.firstBlock(res.block))
	if err != nil {
		return b.writeOutcome(r.Context(), sub, err)
	}
	for _, e := range res.snapshot {
		if err := s.sendValid(e.ev); err != nil {
			return b.writeOutcome(r.Context(), sub, err)
		}
	}
	var skip func(string) bool
	if res.replay {
		var done bool
		if skip, done, err = b.replay(r.Context(), s, sub, res); done {
			return err
		}
	}
	return b.live(r.Context(), s, sub, skip)
}

// resolution is what a subscription sends before its live events.
type resolution struct {
	block    []byte  // position or reset event, written with the headers
	snapshot []entry // missed events from the history
	replay   bool    // the missed events come from the replay function
	lastID   string  // the client's last event identifier, for the replay
	latest   string  // the newest history identifier at registration
}

// resolveLocked resolves the client's last event identifier against the
// history (research R-11).
func (b *Broker) resolveLocked(lastID string, sub *subscriber) resolution {
	latest := b.hist.latestID()
	switch seq, found := b.hist.lookup(lastID); {
	case lastID == "":
		// A new subscription: give the client a position to resume from, even
		// if it leaves before its first event.
		if latest != "" {
			return resolution{block: appendPosition(nil, latest)}
		}
		return resolution{}
	case found:
		return resolution{snapshot: b.hist.after(seq, sub.topics)}
	case b.cfg.replay != nil:
		return resolution{replay: true, lastID: lastID, latest: latest}
	default:
		return resolution{block: b.unresolvedBlock(latest)}
	}
}

// unresolvedBlock returns what tells a client that its last event identifier
// cannot be resolved: the reset event when configured, otherwise a position.
// Both carry the newest identifier, so the client stops sending the old one.
func (b *Broker) unresolvedBlock(latest string) []byte {
	if b.cfg.reset == nil {
		return appendPosition(nil, latest)
	}
	ev := *b.cfg.reset
	ev.ID = latest
	return appendResetEvent(nil, ev)
}

// firstBlock prefixes block with the reconnection time, when configured, in a
// single block.
func (b *Broker) firstBlock(block []byte) []byte {
	if b.cfg.retry <= 0 {
		return block
	}
	first := appendRetry(nil, b.cfg.retry)
	if len(block) == 0 {
		return append(first, '\n')
	}
	return append(first, block...)
}

// live writes the subscriber's events, and a keep-alive comment after each
// silent interval, until the subscription ends. Events for which skip, if not
// nil, reports true were already sent by a replay.
func (b *Broker) live(ctx context.Context, s *Stream, sub *subscriber, skip func(string) bool) error {
	keepAlive := time.NewTimer(b.cfg.keepAlive)
	defer keepAlive.Stop()
	for {
		var err error
		select {
		case <-ctx.Done():
			return nil
		case <-b.done:
			return ErrClosed
		case <-sub.overflow:
			return ErrSlowConsumer
		case e := <-sub.queue:
			if skip != nil && skip(e.ev.ID) {
				continue
			}
			err = s.sendValid(e.ev)
		case <-keepAlive.C:
			err = s.KeepAlive()
		}
		if err != nil {
			return b.writeOutcome(ctx, sub, err)
		}
		keepAlive.Reset(b.cfg.keepAlive)
	}
}

// writeOutcome maps a failed write to the outcome of Serve. An overflow takes
// precedence, because a client stalled in a write is also what fills its
// queue. A client that has gone is not an error: when a write fails on a
// closed connection, net/http ends the request's context, possibly a moment
// later, so writeOutcome waits for it for up to one write timeout. A write
// that timed out means that the client stopped reading, and is reported.
func (b *Broker) writeOutcome(ctx context.Context, sub *subscriber, err error) error {
	b.mu.Lock()
	overflowed := sub.overflowed
	b.mu.Unlock()
	switch {
	case overflowed:
		return ErrSlowConsumer
	case ctx.Err() != nil:
		return nil
	case errors.Is(err, os.ErrDeadlineExceeded):
		return err
	}
	wait := time.NewTimer(b.cfg.writeTimeout)
	defer wait.Stop()
	select {
	case <-ctx.Done():
		return nil
	case <-wait.C:
		return err
	}
}

func (b *Broker) unregister(sub *subscriber) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.removeLocked(sub)
}

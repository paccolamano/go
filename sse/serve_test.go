package sse_test

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/paccolamano/go/sse"
)

// serveBroker starts e with a handler that subscribes each request to the
// comma-separated topics of its "topics" query parameter. The returned channel
// receives what each Serve call returned.
func serveBroker(e *env, b *sse.Broker) <-chan error {
	results := make(chan error, 2048)
	e.Start(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var topics []string
		if q := r.URL.Query().Get("topics"); q != "" {
			topics = strings.Split(q, ",")
		}
		results <- b.Serve(w, r, topics...)
	}), 0)
	return results
}

// subscribe opens a subscription to topics and returns its decoded events.
func subscribe(e *env, ctx context.Context, topics string, header http.Header) (<-chan sse.Event, <-chan error) {
	e.T.Helper()
	return readEvents(e.T, e.Get(e.T, ctx, "/?topics="+topics, header).Body)
}

// virtualTimeout bounds the waits of the helpers. Inside a bubble it costs no
// real time, and it turns a missing event into a failure instead of a hang:
// keep-alive comments would otherwise advance virtual time forever.
const virtualTimeout = time.Hour

// receive returns the next n events, failing the test if the stream ends first
// or an event does not arrive within virtualTimeout.
func receive(tb testing.TB, events <-chan sse.Event, n int) []sse.Event {
	tb.Helper()
	timeout := time.NewTimer(virtualTimeout)
	defer timeout.Stop()
	out := make([]sse.Event, 0, n)
	for range n {
		select {
		case ev, ok := <-events:
			if !ok {
				tb.Fatalf("stream ended after %d of %d events", len(out), n)
			}
			out = append(out, ev)
		case <-timeout.C:
			tb.Fatalf("received %d of %d events within %v", len(out), n, virtualTimeout)
		}
	}
	return out
}

// assertNone fails if events holds an event once every goroutine is idle.
func assertNone(tb testing.TB, events <-chan sse.Event) {
	tb.Helper()
	synctest.Wait()
	select {
	case ev, ok := <-events:
		if ok {
			tb.Fatalf("unexpected event %+v", ev)
		}
	default:
	}
}

func ids(events []sse.Event) []string {
	out := make([]string, 0, len(events))
	for _, ev := range events {
		out = append(out, ev.ID)
	}
	return out
}

func busIDs(from, to int) []string {
	out := make([]string, 0, to-from+1)
	for i := from; i <= to; i++ {
		out = append(out, "bus-"+strconv.Itoa(i))
	}
	return out
}

func mustPublish(tb testing.TB, b *sse.Broker, ev sse.Event, topics ...string) {
	tb.Helper()
	if err := b.Publish(ev, topics...); err != nil {
		tb.Fatalf("Publish(%+v, %v): %v", ev, topics, err)
	}
}

func TestServe_Topics(t *testing.T) {
	e2e(t, func(e *env) {
		t := e.T
		b := newBroker(t)
		serveBroker(e, b)
		alice, _ := subscribe(e, t.Context(), "user:alice", nil)
		bob, _ := subscribe(e, t.Context(), "user:bob", nil)
		mustPublish(t, b, sse.Event{Data: "for alice"}, "user:alice")
		if ev := receive(t, alice, 1)[0]; ev.Data != "for alice" {
			t.Fatalf("alice got %+v", ev)
		}
		assertNone(t, bob)
	})
}

func TestServe_OncePerSubscriber(t *testing.T) {
	e2e(t, func(e *env) {
		t := e.T
		b := newBroker(t)
		serveBroker(e, b)
		events, _ := subscribe(e, t.Context(), "user:alice,broadcast", nil)
		mustPublish(t, b, sse.Event{ID: "1"}, "user:alice", "broadcast")
		mustPublish(t, b, sse.Event{ID: "2"}, "broadcast", "broadcast")
		if got := ids(receive(t, events, 2)); !slices.Equal(got, []string{"1", "2"}) {
			t.Fatalf("got %v", got)
		}
		assertNone(t, events)
	})
}

func TestServe_PublishAfterHeaders(t *testing.T) {
	e2e(t, func(e *env) {
		t := e.T
		b := newBroker(t)
		serveBroker(e, b)
		events, _ := subscribe(e, t.Context(), "t", nil)
		mustPublish(t, b, sse.Event{Data: "right away"}, "t")
		if ev := receive(t, events, 1)[0]; ev.Data != "right away" {
			t.Fatalf("got %+v", ev)
		}
	})
}

func TestServe_GeneratedIDs(t *testing.T) {
	e2e(t, func(e *env) {
		t := e.T
		pattern := regexp.MustCompile(`^([A-Z2-7]{12})-(\d+)$`)
		prefixOf := func(b *sse.Broker, e *env) string {
			serveBroker(e, b)
			events, _ := subscribe(e, t.Context(), "t", nil)
			for range 3 {
				mustPublish(t, b, sse.Event{Data: "x"}, "t")
			}
			var prefix string
			for i, ev := range receive(t, events, 3) {
				m := pattern.FindStringSubmatch(ev.ID)
				if m == nil || m[2] != strconv.Itoa(i+1) {
					t.Fatalf("ID %q does not match <prefix>-%d", ev.ID, i+1)
				}
				if prefix != "" && m[1] != prefix {
					t.Fatalf("prefix changed within a broker: %q", ev.ID)
				}
				prefix = m[1]
			}
			return prefix
		}
		first := prefixOf(newBroker(t), e)
		second := prefixOf(newBroker(t), newEnv(t, e.H2))
		if first == second {
			t.Fatalf("two brokers share the prefix %q", first)
		}
	})
}

func TestServe_KeepAlive(t *testing.T) {
	e2e(t, func(e *env) {
		t := e.T
		b := newBroker(t)
		serveBroker(e, b)
		lines := readLines(t, e.Get(t, t.Context(), "/?topics=t", nil).Body)
		time.Sleep(15 * time.Second)
		synctest.Wait()
		select {
		case line := <-lines:
			if line != ": keep-alive" {
				t.Fatalf("first line %q, want the keep-alive comment", line)
			}
		default:
			t.Fatal("no keep-alive after 15 s of silence")
		}
	})
	e2e(t, func(e *env) {
		t := e.T
		b := newBroker(t, sse.WithKeepAlive(5*time.Second))
		serveBroker(e, b)
		lines := readLines(t, e.Get(t, t.Context(), "/?topics=t", nil).Body)
		for range 5 {
			time.Sleep(4 * time.Second)
			mustPublish(t, b, sse.Event{Data: "tick"}, "t")
		}
		time.Sleep(4 * time.Second)
		synctest.Wait()
		for len(lines) > 0 {
			if line := <-lines; strings.HasPrefix(line, ":") {
				t.Fatalf("keep-alive written while events kept flowing: %q", line)
			}
		}
	})
}

func TestServe_Disconnect(t *testing.T) {
	e2e(t, func(e *env) {
		t := e.T
		b := newBroker(t)
		results := serveBroker(e, b)
		ctx, cancel := context.WithCancel(t.Context())
		resp := e.Get(t, ctx, "/?topics=t", nil)
		cancel()
		_ = resp.Body.Close()
		if err := <-results; err != nil {
			t.Fatalf("Serve = %v, want nil after the client left", err)
		}
		for range 100 {
			mustPublish(t, b, sse.Event{Data: "nobody listens"}, "t")
		}
	})
}

// failingFlush is a response writer whose flushes fail with err. The first
// failure runs onFail.
type failingFlush struct {
	*httptest.ResponseRecorder
	err    error
	once   sync.Once
	onFail func()
}

func (w *failingFlush) FlushError() error {
	w.once.Do(w.onFail)
	return w.err
}

// TestServe_ClosedConnection is the regression test of a race: a write fails on
// a closed connection a moment before net/http ends the request's context.
// Serve must report the client's departure as nil, but a write that timed out
// as an error, at once.
func TestServe_ClosedConnection(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		b := newBroker(t)
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		w := &failingFlush{
			ResponseRecorder: httptest.NewRecorder(),
			err:              fmt.Errorf("write: %w", net.ErrClosed),
			onFail:           func() { time.AfterFunc(time.Millisecond, cancel) },
		}
		req := httptest.NewRequestWithContext(ctx, http.MethodGet, "/", nil)
		if err := b.Serve(w, req, "t"); err != nil {
			t.Fatalf("Serve = %v, want nil when the client has gone", err)
		}
	})
	synctest.Test(t, func(t *testing.T) {
		b := newBroker(t)
		w := &failingFlush{
			ResponseRecorder: httptest.NewRecorder(),
			err:              fmt.Errorf("write: %w", os.ErrDeadlineExceeded),
			onFail:           func() {},
		}
		start := time.Now()
		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil)
		if err := b.Serve(w, req, "t"); !errors.Is(err, os.ErrDeadlineExceeded) {
			t.Fatalf("Serve = %v, want the timeout", err)
		}
		if waited := time.Since(start); waited != 0 {
			t.Fatalf("a timed-out write waited %v", waited)
		}
	})
	synctest.Test(t, func(t *testing.T) {
		b := newBroker(t, sse.WithWriteTimeout(2*time.Second))
		w := &failingFlush{
			ResponseRecorder: httptest.NewRecorder(),
			err:              fmt.Errorf("write: %w", net.ErrClosed),
			onFail:           func() {}, // the request's context never ends
		}
		start := time.Now()
		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil)
		if err := b.Serve(w, req, "t"); !errors.Is(err, net.ErrClosed) {
			t.Fatalf("Serve = %v, want the write error", err)
		}
		if waited := time.Since(start); waited != 2*time.Second {
			t.Fatalf("waited %v for the request's context, want one write timeout", waited)
		}
	})
}

func TestServe_Order(t *testing.T) {
	e2e(t, func(e *env) {
		t := e.T
		b := newBroker(t, sse.WithBuffer(1000))
		serveBroker(e, b)
		var subs [3]<-chan sse.Event
		for i := range subs {
			subs[i], _ = subscribe(e, t.Context(), "t", nil)
		}
		var wg sync.WaitGroup
		for g := range 50 {
			wg.Go(func() {
				for i := range 20 {
					mustPublish(t, b, sse.Event{ID: fmt.Sprintf("g%d-%d", g, i)}, "t")
				}
			})
		}
		wg.Wait()
		first := ids(receive(t, subs[0], 1000))
		for _, sub := range subs[1:] {
			if got := ids(receive(t, sub, 1000)); !slices.Equal(got, first) {
				t.Fatal("subscribers received events in different orders")
			}
		}
	})
}

func TestServe_Unsupported(t *testing.T) {
	b := newBroker(t)
	var serveErr error
	h := http.TimeoutHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		serveErr = b.Serve(w, r, "t")
	}), time.Minute, "timeout")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil))
	if !errors.Is(serveErr, sse.ErrStreamingUnsupported) {
		t.Fatalf("Serve = %v, want ErrStreamingUnsupported", serveErr)
	}
	if rec.Header().Get("Content-Type") == "text/event-stream" || rec.Body.Len() != 0 {
		t.Fatal("Serve wrote to a response it cannot stream")
	}
}

func TestServe_NoTopics(t *testing.T) {
	b := newBroker(t)
	rec := httptest.NewRecorder()
	if err := b.Serve(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil)); err == nil {
		t.Fatal("Serve without topics returned nil")
	}
	if rec.Flushed || rec.Body.Len() != 0 || rec.Header().Get("Content-Type") != "" {
		t.Fatal("Serve without topics wrote to the response")
	}
}

func TestServe_Retry(t *testing.T) {
	e2e(t, func(e *env) {
		t := e.T
		b := newBroker(t, sse.WithRetry(3*time.Second))
		serveBroker(e, b)
		lines := readLines(t, e.Get(t, t.Context(), "/?topics=t", nil).Body)
		if line := <-lines; line != "retry: 3000" {
			t.Fatalf("first line %q", line)
		}
	})
}

func TestServe_RetryWithPosition(t *testing.T) {
	e2e(t, func(e *env) {
		t := e.T
		b := newBroker(t, sse.WithRetry(3*time.Second))
		serveBroker(e, b)
		mustPublish(t, b, sse.Event{ID: "e1"}, "t")
		lines := readLines(t, e.Get(t, t.Context(), "/?topics=t", nil).Body)
		// One block: the reconnection time and the position (wire-format.md).
		if got := firstBlock(t, lines); !slices.Equal(got, []string{"retry: 3000", "id: e1"}) {
			t.Fatalf("first block %q", got)
		}
	})
}

func TestServe_DuplicateNotDelivered(t *testing.T) {
	e2e(t, func(e *env) {
		t := e.T
		b := newBroker(t)
		serveBroker(e, b)
		events, _ := subscribe(e, t.Context(), "t", nil)
		for _, id := range busIDs(1, 10) {
			mustPublish(t, b, sse.Event{ID: id}, "t")
		}
		for _, id := range busIDs(5, 8) { // re-delivered after a feed reconnection
			if err := b.Publish(sse.Event{ID: id}, "t"); !errors.Is(err, sse.ErrDuplicateID) {
				t.Fatalf("re-publishing %s = %v, want ErrDuplicateID", id, err)
			}
		}
		if got := ids(receive(t, events, 10)); !slices.Equal(got, busIDs(1, 10)) {
			t.Fatalf("got %v", got)
		}
		assertNone(t, events)
	})
}

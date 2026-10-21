package sse_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"

	"github.com/paccolamano/go/sse"
)

// memoryBus stands in for a durable message bus, such as Redis Streams, Kafka
// or RabbitMQ Streams.
type memoryBus struct{ messages []busMessage }

type busMessage struct {
	id, data string
	topics   []string
}

// replay is the ReplayFunc over the bus: it sends every message after lastID,
// with its topics. The broker filters them by the subscription's topics.
func (m *memoryBus) replay(_ context.Context, lastID string, _ []string, send func(sse.Event, ...string) error) error {
	for i, msg := range m.messages {
		if msg.id != lastID {
			continue
		}
		for _, next := range m.messages[i+1:] {
			if err := send(sse.Event{ID: next.id, Data: next.data}, next.topics...); err != nil {
				return err
			}
		}
		return nil
	}
	return sse.ErrUnknownID
}

// leaveAfterFlushes records a response and ends its request after n flushes,
// like a client that disconnects.
type leaveAfterFlushes struct {
	*httptest.ResponseRecorder
	n      int
	cancel context.CancelFunc
}

func (w *leaveAfterFlushes) Flush() {
	w.ResponseRecorder.Flush()
	if w.n--; w.n == 0 {
		w.cancel()
	}
}

func ExampleWithReplay() {
	bus := &memoryBus{}
	b, err := sse.NewBroker(sse.WithHistory(1), sse.WithReplay(bus.replay))
	if err != nil {
		fmt.Println(err)
		return
	}
	for _, id := range []string{"1", "2", "3"} {
		msg := busMessage{id: id, data: "update " + id, topics: []string{"orders"}}
		bus.messages = append(bus.messages, msg) // the domain code writes to the bus
		// The feed loop of this replica publishes it, with the bus identifier.
		if err := b.Publish(sse.Event{ID: msg.id, Data: msg.data}, msg.topics...); err != nil {
			fmt.Println(err)
		}
	}

	// The history keeps only event 3: a client that last saw event 1 needs the bus.
	ctx, cancel := context.WithCancel(context.Background())
	r := httptest.NewRequestWithContext(ctx, http.MethodGet, "/events", nil)
	r.Header.Set("Last-Event-ID", "1")
	w := &leaveAfterFlushes{ResponseRecorder: httptest.NewRecorder(), n: 3, cancel: cancel}

	fmt.Println("Serve:", b.Serve(w, r, "orders"))
	fmt.Print(w.Body.String())
	// Output:
	// Serve: <nil>
	// id: 2
	// data: update 2
	//
	// id: 3
	// data: update 3
}

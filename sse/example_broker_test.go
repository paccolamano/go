package sse_test

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"sync"

	"github.com/paccolamano/go/sse"
)

// userKey stands in for the context key of your authentication middleware.
type userKey struct{}

// eventsHandler subscribes each request to the notifications of its
// authenticated user and to broadcasts.
func eventsHandler(b *sse.Broker) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, _ := r.Context().Value(userKey{}).(string)
		if err := b.Serve(w, r, "user:"+user, "broadcast"); err != nil {
			fmt.Println("stream ended:", err) // log it in a real server
		}
	}
}

// flushRecorder records a streamed response and signals each flush.
type flushRecorder struct {
	mu      sync.Mutex
	header  http.Header
	body    bytes.Buffer
	flushed chan struct{}
}

func newFlushRecorder() *flushRecorder {
	return &flushRecorder{header: http.Header{}, flushed: make(chan struct{}, 16)}
}

func (f *flushRecorder) Header() http.Header { return f.header }

func (f *flushRecorder) WriteHeader(int) {}

func (f *flushRecorder) Write(p []byte) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.body.Write(p)
}

func (f *flushRecorder) Flush() { f.flushed <- struct{}{} }

func (f *flushRecorder) String() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.body.String()
}

func ExampleBroker() {
	b, err := sse.NewBroker()
	if err != nil {
		fmt.Println(err)
		return
	}

	// A request from alice, as the authentication middleware leaves it.
	ctx, cancel := context.WithCancel(context.WithValue(context.Background(), userKey{}, "alice"))
	r := httptest.NewRequestWithContext(ctx, http.MethodGet, "/events", nil)
	w := newFlushRecorder()
	done := make(chan struct{})
	go func() {
		eventsHandler(b)(w, r)
		close(done)
	}()
	<-w.flushed // headers sent: the subscription is registered

	// Anywhere in the service:
	if err := b.Publish(sse.Event{Type: "order.created", Data: `{"id":1}`}, "user:alice"); err != nil {
		fmt.Println(err)
	}
	<-w.flushed // the event was written

	cancel() // the client goes away: Serve returns nil
	<-done
	fmt.Print(regexp.MustCompile(`id: \S+`).ReplaceAllString(w.String(), "id: <generated>"))
	// Output:
	// event: order.created
	// id: <generated>
	// data: {"id":1}
}

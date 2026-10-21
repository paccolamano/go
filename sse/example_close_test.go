package sse_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"time"

	"github.com/paccolamano/go/sse"
)

// firstFlush records a response and signals its first flush: by then the
// subscription is registered.
type firstFlush struct {
	*httptest.ResponseRecorder
	once sync.Once
	done chan struct{}
}

func (w *firstFlush) Flush() {
	w.ResponseRecorder.Flush()
	w.once.Do(func() { close(w.done) })
}

func ExampleBroker_Close() {
	b, err := sse.NewBroker()
	if err != nil {
		fmt.Println(err)
		return
	}
	srv := &http.Server{Addr: ":8080", ReadHeaderTimeout: 10 * time.Second}
	srv.RegisterOnShutdown(b.Close) // without it, Shutdown waits for open streams

	// An open stream.
	w := &firstFlush{ResponseRecorder: httptest.NewRecorder(), done: make(chan struct{})}
	r := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/events", nil)
	served := make(chan error, 1)
	go func() { served <- b.Serve(w, r, "news") }()
	<-w.done

	// The deploy stops this replica.
	if err := srv.Shutdown(context.Background()); err != nil {
		fmt.Println(err)
	}
	fmt.Println(<-served) // the stream ended normally: the browser reconnects elsewhere
	fmt.Println(b.Publish(sse.Event{Data: "too late"}, "news"))
	// Output:
	// sse: broker closed
	// sse: broker closed
}

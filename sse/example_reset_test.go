package sse_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"

	"github.com/paccolamano/go/sse"
)

// leaveAfterFirstBlock records a response and ends its request after the first
// flush, like a client that reads the first block and disconnects.
type leaveAfterFirstBlock struct {
	*httptest.ResponseRecorder
	cancel context.CancelFunc
}

func (w leaveAfterFirstBlock) Flush() {
	w.ResponseRecorder.Flush()
	w.cancel()
}

func ExampleWithResetEvent() {
	b, err := sse.NewBroker(sse.WithResetEvent(sse.Event{Type: "reset"}))
	if err != nil {
		fmt.Println(err)
		return
	}
	for _, id := range []string{"1", "2"} {
		if err := b.Publish(sse.Event{ID: id, Data: "news"}, "news"); err != nil {
			fmt.Println(err)
		}
	}

	// A client comes back with an identifier the broker no longer knows: it was
	// evicted from the history, or issued by a previous process.
	ctx, cancel := context.WithCancel(context.Background())
	r := httptest.NewRequestWithContext(ctx, http.MethodGet, "/events", nil)
	r.Header.Set("Last-Event-ID", "gone")
	w := leaveAfterFirstBlock{httptest.NewRecorder(), cancel}

	fmt.Println("Serve:", b.Serve(w, r, "news"))
	fmt.Print(w.Body.String())
	// The reset event carries the newest identifier, so that the client, after
	// reloading its state, resumes from there.

	// Output:
	// Serve: <nil>
	// event: reset
	// id: 2
	// data:
}

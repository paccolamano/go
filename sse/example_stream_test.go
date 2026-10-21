package sse_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"time"

	"github.com/paccolamano/go/sse"
)

// progressHandler streams the progress of a job to the client that asked for
// it.
func progressHandler(w http.ResponseWriter, r *http.Request) {
	s, err := sse.NewStream(w, r, sse.WithRetry(5*time.Second))
	if err != nil {
		// ErrStreamingUnsupported: nothing was written, so answer an error.
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	for _, pct := range []string{"10%", "50%", "100%"} {
		if err := s.Send(sse.Event{Type: "progress", Data: pct}); err != nil {
			return // the client has gone
		}
	}
}

func ExampleStream() {
	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/jobs/42/progress", nil)
	progressHandler(rec, req)

	fmt.Println(rec.Header().Get("Content-Type"))
	fmt.Print(rec.Body.String())
	// Output:
	// text/event-stream
	// retry: 5000
	//
	// event: progress
	// data: 10%
	//
	// event: progress
	// data: 50%
	//
	// event: progress
	// data: 100%
}

package sse_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/paccolamano/go/sse"
)

// cannedTransport answers in memory, in place of a server: the first
// connection gets a stream that ends, the second must resume after event 1,
// and the third is told to stop with 204 No Content.
type cannedTransport struct{ calls int }

func (t *cannedTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	t.calls++
	switch {
	case t.calls == 1:
		return eventStream(r, "retry: 1\nid: 1\ndata: first\n\n"), nil
	case t.calls == 2 && r.Header.Get("Last-Event-ID") == "1":
		return eventStream(r, "id: 2\ndata: second\n\n"), nil
	}
	return &http.Response{StatusCode: http.StatusNoContent, Body: http.NoBody, Request: r}, nil
}

func eventStream(r *http.Request, body string) *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": {"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    r,
	}
}

func ExampleClient() {
	c, err := sse.NewClient(sse.WithHTTPClient(&http.Client{Transport: &cannedTransport{}}))
	if err != nil {
		fmt.Println(err)
		return
	}
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "https://api.example.com/events", nil)
	if err != nil {
		fmt.Println(err)
		return
	}
	// Headers and body are sent again on every reconnection, for example an
	// Authorization header.
	for ev, err := range c.Events(req) {
		if err != nil {
			fmt.Println(err) // *ResponseError, ErrEventTooLarge or the context's error
			return
		}
		fmt.Println(ev.Data)
	}
	// Output:
	// first
	// second
}

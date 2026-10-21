package sse_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/paccolamano/go/sse"
)

// discardWriter is a response writer that discards the body and signals its
// first flush. It does not support write deadlines, which a stream tolerates.
type discardWriter struct {
	header  http.Header
	once    sync.Once
	flushed chan struct{}
}

func (w *discardWriter) Header() http.Header         { return w.header }
func (w *discardWriter) WriteHeader(int)             {}
func (w *discardWriter) Write(p []byte) (int, error) { return len(p), nil }
func (w *discardWriter) Flush()                      { w.once.Do(func() { close(w.flushed) }) }

// BenchmarkPublish measures publishing one event to 1,000 subscribers that
// keep up.
func BenchmarkPublish(b *testing.B) {
	const subscribers = 1000
	br, err := sse.NewBroker(sse.WithBuffer(1 << 16))
	if err != nil {
		b.Fatal(err)
	}
	ctx, cancel := context.WithCancel(b.Context())
	var wg sync.WaitGroup
	for range subscribers {
		w := &discardWriter{header: http.Header{}, flushed: make(chan struct{})}
		r := httptest.NewRequestWithContext(ctx, http.MethodGet, "/", nil)
		wg.Go(func() { _ = br.Serve(w, r, "bench") })
		<-w.flushed // registered
	}
	ev := sse.Event{Data: strings.Repeat("x", 100)}
	b.ReportAllocs()
	for b.Loop() {
		if err := br.Publish(ev, "bench"); err != nil {
			b.Fatal(err)
		}
	}
	b.StopTimer()
	cancel()
	wg.Wait()
}

// BenchmarkEncode measures writing a 1 KiB event of three lines to a stream.
func BenchmarkEncode(b *testing.B) {
	w := &discardWriter{header: http.Header{}, flushed: make(chan struct{})}
	s, err := sse.NewStream(w, httptest.NewRequestWithContext(b.Context(), http.MethodGet, "/", nil))
	if err != nil {
		b.Fatal(err)
	}
	line := strings.Repeat("x", 340)
	ev := sse.Event{ID: "42", Type: "update", Data: line + "\n" + line + "\n" + line}
	b.ReportAllocs()
	for b.Loop() {
		if err := s.Send(ev); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkDecode measures decoding a stream of 100 events.
func BenchmarkDecode(b *testing.B) {
	var sb strings.Builder
	for range 100 {
		sb.WriteString("event: update\nid: 42\ndata: " + strings.Repeat("x", 100) + "\n\n")
	}
	stream := sb.String()
	b.ReportAllocs()
	b.SetBytes(int64(len(stream)))
	for b.Loop() {
		for _, err := range sse.Decode(io.NopCloser(strings.NewReader(stream))) {
			if err != nil {
				b.Fatal(err)
			}
		}
	}
}

package sse_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/paccolamano/go/sse"
)

func newRecorderStream(tb testing.TB, opts ...sse.Option) (*httptest.ResponseRecorder, *sse.Stream) {
	tb.Helper()
	rec := httptest.NewRecorder()
	s, err := sse.NewStream(rec, httptest.NewRequestWithContext(tb.Context(), http.MethodGet, "/", nil), opts...)
	if err != nil {
		tb.Fatal(err)
	}
	return rec, s
}

func TestStream_Headers(t *testing.T) {
	rec := httptest.NewRecorder()
	rec.Header().Set("Access-Control-Allow-Origin", "https://app.example")
	if _, err := sse.NewStream(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil)); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"Content-Type":                "text/event-stream",
		"Cache-Control":               "no-store",
		"X-Accel-Buffering":           "no",
		"Access-Control-Allow-Origin": "https://app.example",
	}
	for k, v := range want {
		if got := rec.Header().Get(k); got != v {
			t.Errorf("%s = %q, want %q", k, got, v)
		}
	}
	if rec.Code != http.StatusOK || !rec.Flushed {
		t.Fatalf("code %d, flushed %v", rec.Code, rec.Flushed)
	}
	if rec.Body.Len() != 0 {
		t.Fatalf("body %q, want empty", rec.Body)
	}
}

func TestStream_Encoding(t *testing.T) {
	tests := []struct {
		ev   sse.Event
		want string
	}{
		{sse.Event{Data: "hello"}, "data: hello\n\n"},
		{sse.Event{Type: "progress", ID: "7", Data: "50%"}, "event: progress\nid: 7\ndata: 50%\n\n"},
		{sse.Event{Data: "a\r\nb\rc\n"}, "data: a\ndata: b\ndata: c\ndata:\n\n"},
		{sse.Event{Data: ""}, "data:\n\n"},
		{sse.Event{Data: "  x"}, "data:   x\n\n"},
	}
	for _, tt := range tests {
		rec, s := newRecorderStream(t)
		if err := s.Send(tt.ev); err != nil {
			t.Fatal(err)
		}
		if got := rec.Body.String(); got != tt.want {
			t.Errorf("Send(%+v) wrote %q, want %q", tt.ev, got, tt.want)
		}
	}
}

func TestStream_InvalidEvent(t *testing.T) {
	rec, s := newRecorderStream(t)
	for _, ev := range []sse.Event{{ID: "a\nb"}, {Data: "\xff"}} {
		if err := s.Send(ev); !errors.Is(err, sse.ErrInvalidEvent) {
			t.Fatalf("Send(%+v) = %v, want ErrInvalidEvent", ev, err)
		}
	}
	if rec.Body.Len() != 0 {
		t.Fatalf("invalid events wrote %q", rec.Body)
	}
	if err := s.Send(sse.Event{Data: "ok"}); err != nil {
		t.Fatal(err)
	}
	if got := rec.Body.String(); got != "data: ok\n\n" {
		t.Fatalf("body %q", got)
	}
}

func TestStream_Unsupported(t *testing.T) {
	var streamErr error
	h := http.TimeoutHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, streamErr = sse.NewStream(w, r)
		_, _ = w.Write([]byte("fallback"))
	}), time.Minute, "timeout")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil))
	if !errors.Is(streamErr, sse.ErrStreamingUnsupported) {
		t.Fatalf("NewStream = %v, want ErrStreamingUnsupported", streamErr)
	}
	if rec.Body.String() != "fallback" || rec.Header().Get("Content-Type") == "text/event-stream" {
		t.Fatalf("body %q, content type %q", rec.Body, rec.Header().Get("Content-Type"))
	}
}

func TestStream_Retry(t *testing.T) {
	rec, _ := newRecorderStream(t, sse.WithRetry(5*time.Second))
	if got := rec.Body.String(); got != "retry: 5000\n\n" {
		t.Fatalf("body %q, want the retry block", got)
	}
}

func TestStream_KeepAlive(t *testing.T) {
	rec, s := newRecorderStream(t)
	if err := s.KeepAlive(); err != nil {
		t.Fatal(err)
	}
	if got := rec.Body.String(); got != ": keep-alive\n" {
		t.Fatalf("body %q", got)
	}
}

func TestStream_Concurrent(t *testing.T) {
	rec, s := newRecorderStream(t)
	data := strings.Repeat("x", 1024)
	var wg sync.WaitGroup
	for range 50 {
		wg.Go(func() {
			if err := s.Send(sse.Event{Data: data}); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	events, err := decodeString(t, rec.Body.String())
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 50 {
		t.Fatalf("decoded %d events, want 50", len(events))
	}
	for _, ev := range events {
		if ev.Data != data {
			t.Fatalf("corrupted event of %d bytes", len(ev.Data))
		}
	}
}

// closingWriter delivers writes, then fails to clear the write deadline, as a
// connection that closes right after a flush.
type closingWriter struct {
	*httptest.ResponseRecorder
}

var errConnClosed = errors.New("connection closed")

func (w closingWriter) SetWriteDeadline(t time.Time) error {
	if t.IsZero() {
		return errConnClosed
	}
	return nil
}

// TestStream_ConnectionClosedAfterFlush is the regression test of a race: an
// event was delivered, then the connection closed before the deadline was
// cleared. Send must report success, and the next write the failure.
func TestStream_ConnectionClosedAfterFlush(t *testing.T) {
	w := closingWriter{httptest.NewRecorder()}
	s, err := sse.NewStream(w, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil))
	if err != nil {
		t.Fatalf("NewStream = %v, want success: the headers were delivered", err)
	}
	if err := s.Send(sse.Event{Data: "delivered"}); !errors.Is(err, errConnClosed) {
		t.Fatalf("Send after a failed clear = %v, want the recorded failure", err)
	}
	if got := w.Body.String(); got != "" {
		t.Fatalf("a write followed a recorded failure: %q", got)
	}

	w = closingWriter{httptest.NewRecorder()}
	s, err = sse.NewStream(w, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil), sse.WithRetry(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if got := w.Body.String(); got != "retry: 1000\n\n" {
		t.Fatalf("body %q, want the delivered first block", got)
	}
	if err := s.KeepAlive(); err == nil {
		t.Fatal("the write after the connection closed succeeded")
	}
}

// wrapper hides the recorder's methods behind Unwrap, as middleware writers do.
type wrapper struct{ w http.ResponseWriter }

func (w wrapper) Header() http.Header         { return w.w.Header() }
func (w wrapper) Write(p []byte) (int, error) { return w.w.Write(p) }
func (w wrapper) WriteHeader(code int)        { w.w.WriteHeader(code) }
func (w wrapper) Unwrap() http.ResponseWriter { return w.w }

func TestStream_Unwrap(t *testing.T) {
	rec := httptest.NewRecorder()
	s, err := sse.NewStream(wrapper{rec}, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil))
	if err != nil {
		t.Fatalf("NewStream through Unwrap = %v", err)
	}
	if err := s.Send(sse.Event{Data: "x"}); err != nil {
		t.Fatal(err)
	}
	if !rec.Flushed || rec.Body.String() != "data: x\n\n" {
		t.Fatalf("flushed %v, body %q", rec.Flushed, rec.Body)
	}
}

// brokenWriter fails every write after the headers.
type brokenWriter struct{ *httptest.ResponseRecorder }

var errBroken = errors.New("broken pipe")

func (brokenWriter) Write([]byte) (int, error) { return 0, errBroken }

func TestStream_WriteError(t *testing.T) {
	s, err := sse.NewStream(brokenWriter{httptest.NewRecorder()}, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil))
	if err != nil {
		t.Fatal(err)
	}
	first := s.Send(sse.Event{Data: "x"})
	if !errors.Is(first, errBroken) {
		t.Fatalf("Send = %v, want the write error", first)
	}
	if again := s.KeepAlive(); again == nil || again.Error() != first.Error() {
		t.Fatalf("KeepAlive after a failure = %v, want %v", again, first)
	}
}

func TestStream_Options(t *testing.T) {
	rec := httptest.NewRecorder()
	_, err := sse.NewStream(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil), sse.WithHistory(1))
	if !errors.Is(err, sse.ErrInvalidConfig) {
		t.Fatalf("err = %v, want ErrInvalidConfig", err)
	}
	if rec.Flushed || rec.Body.Len() != 0 || rec.Header().Get("Content-Type") != "" {
		t.Fatal("an invalid configuration wrote to the response")
	}
}

package compress_test

import (
	"bufio"
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestCompress_StreamingFlush(t *testing.T) {
	next := make(chan struct{})
	deadlineErr := make(chan error, 1)
	srv := httptest.NewServer(mustNew(t)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		deadlineErr <- http.NewResponseController(w).SetWriteDeadline(time.Now().Add(time.Minute))
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: one\n\n")
		_ = http.NewResponseController(w).Flush()
		<-next
		_, _ = io.WriteString(w, "data: two\n\n")
	})))
	t.Cleanup(srv.Close)

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, srv.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Accept-Encoding", "gzip") // explicit: the client does not decompress
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if err := <-deadlineErr; err != nil {
		t.Errorf("SetWriteDeadline through the wrapper: %v", err)
	}
	if resp.Header.Get("Content-Encoding") != "gzip" {
		t.Fatalf("Content-Encoding = %q, want gzip", resp.Header.Get("Content-Encoding"))
	}
	zr, err := gzip.NewReader(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	r := bufio.NewReader(zr)
	if line, err := r.ReadString('\n'); err != nil || line != "data: one\n" {
		t.Fatalf("first event = %q, %v; it was not delivered before the handler continued", line, err)
	}
	close(next)
	rest, err := io.ReadAll(r)
	if err != nil || string(rest) != "\ndata: two\n\n" {
		t.Errorf("rest = %q, %v", rest, err)
	}
}

func TestCompress_HijackDetaches(t *testing.T) {
	srv := httptest.NewServer(mustNew(t)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		conn, brw, err := http.NewResponseController(w).Hijack()
		if err != nil {
			t.Errorf("Hijack: %v", err)
			return
		}
		defer func() { _ = conn.Close() }()
		_, _ = brw.WriteString("HTTP/1.1 200 OK\r\nContent-Length: 2\r\nConnection: close\r\n\r\nok")
		_ = brw.Flush()
	})))
	t.Cleanup(srv.Close)
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, srv.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Accept-Encoding", "gzip")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if body, _ := io.ReadAll(resp.Body); string(body) != "ok" {
		t.Errorf("body = %q, want the raw hijacked response", body)
	}
}

func TestCompress_FlusherInterface(t *testing.T) {
	h := mustNew(t)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: tiny\n\n")
		w.(http.Flusher).Flush() // the plain interface, as older code uses it
	}))
	rec := do(t, h, http.MethodGet, "gzip")
	if !rec.Flushed || rec.Header().Get("Content-Encoding") != "gzip" {
		t.Errorf("flushed=%v encoding=%q, want a flushed gzip stream although below the minimum size",
			rec.Flushed, rec.Header().Get("Content-Encoding"))
	}
}

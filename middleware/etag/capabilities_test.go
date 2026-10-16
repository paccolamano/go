package etag_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestETag_PreservesCapabilities(t *testing.T) {
	deadlineErr := make(chan error, 1)
	srv := httptest.NewServer(mustNew(t)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/hijack" {
			conn, brw, err := http.NewResponseController(w).Hijack()
			if err != nil {
				t.Errorf("Hijack: %v", err)
				return
			}
			defer func() { _ = conn.Close() }()
			_, _ = brw.WriteString("HTTP/1.1 200 OK\r\nContent-Length: 2\r\nConnection: close\r\n\r\nok")
			_ = brw.Flush()
			return
		}
		deadlineErr <- http.NewResponseController(w).SetWriteDeadline(time.Now().Add(time.Minute))
		_, _ = io.WriteString(w, "streamed")
		w.(http.Flusher).Flush() // the plain interface, as older code uses it
	})))
	t.Cleanup(srv.Close)

	for path, want := range map[string]string{"/stream": "streamed", "/hijack": "ok"} {
		req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, srv.URL+path, nil)
		if err != nil {
			t.Fatal(err)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if string(body) != want {
			t.Errorf("%s: body = %q, want %q", path, body, want)
		}
		if path == "/stream" && resp.Header.Get("ETag") != "" {
			t.Error("a flushed response got an ETag")
		}
	}
	if err := <-deadlineErr; err != nil {
		t.Errorf("SetWriteDeadline through the wrapper: %v", err)
	}
}

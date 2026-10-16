package middleware_test

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/paccolamano/go/middleware"
	"github.com/paccolamano/go/middleware/accesslog"
	"github.com/paccolamano/go/middleware/realip"
	"github.com/paccolamano/go/middleware/recovery"
	"github.com/paccolamano/go/middleware/requestid"
)

// logCapture is a slog.Handler that keeps every record.
type logCapture struct {
	mu      sync.Mutex
	records []slog.Record
}

func (c *logCapture) Enabled(context.Context, slog.Level) bool { return true }
func (c *logCapture) WithAttrs([]slog.Attr) slog.Handler       { return c }
func (c *logCapture) WithGroup(string) slog.Handler            { return c }

func (c *logCapture) Handle(_ context.Context, r slog.Record) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.records = append(c.records, r.Clone())
	return nil
}

// take returns the records captured so far and resets the capture.
func (c *logCapture) take() []slog.Record {
	c.mu.Lock()
	defer c.mu.Unlock()
	records := c.records
	c.records = nil
	return records
}

func recordAttrs(r slog.Record) map[string]slog.Value {
	m := map[string]slog.Value{}
	r.Attrs(func(a slog.Attr) bool {
		m[a.Key] = a.Value
		return true
	})
	return m
}

// observabilityChain builds realip → requestid → accesslog → recovery.
func observabilityChain(t *testing.T, logs *logCapture) middleware.Chain {
	t.Helper()
	logger := slog.New(logs)
	rip, err := realip.New(realip.TrustRanges("10.0.0.0/8"))
	if err != nil {
		t.Fatal(err)
	}
	rid, err := requestid.New()
	if err != nil {
		t.Fatal(err)
	}
	alog, err := accesslog.New(logger)
	if err != nil {
		t.Fatal(err)
	}
	rec, err := recovery.New(logger)
	if err != nil {
		t.Fatal(err)
	}
	return middleware.New(rip, rid, alog, rec)
}

func appHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/ok", func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "ok") })
	mux.HandleFunc("/panic", func(http.ResponseWriter, *http.Request) { panic("handler bug") })
	mux.HandleFunc("/late", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "partial")
		_ = http.NewResponseController(w).Flush()
		panic("late handler bug")
	})
	return mux
}

func doRequest(t *testing.T, h http.Handler, path, peer string, header http.Header) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, path, nil)
	req.RemoteAddr = peer
	for k, v := range header {
		req.Header[k] = v
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestIntegration_PanicIsLoggedAndServed(t *testing.T) {
	logs := &logCapture{}
	h := observabilityChain(t, logs).Then(appHandler())

	rec := doRequest(t, h, "/panic", "10.0.0.2:5000", http.Header{"X-Forwarded-For": {"198.51.100.7"}})
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	id := rec.Header().Get("X-Request-ID")
	records := logs.take()
	if len(records) != 2 {
		t.Fatalf("got %d records, want a recovery and an access record", len(records))
	}
	recovered, access := recordAttrs(records[0]), recordAttrs(records[1])
	if records[0].Message != "panic recovered" || records[1].Message != "http request" {
		t.Fatalf("messages = %q, %q", records[0].Message, records[1].Message)
	}
	if recovered["request_id"].String() != id || access["request_id"].String() != id {
		t.Errorf("request_id = %q / %q, want %q in both records", recovered["request_id"], access["request_id"], id)
	}
	if access["status"].Int64() != http.StatusInternalServerError {
		t.Errorf("access status = %v, want 500", access["status"])
	}
	if access["client"].String() != "198.51.100.7" {
		t.Errorf("access client = %q, want the resolved address", access["client"])
	}

	if rec := doRequest(t, h, "/ok", "10.0.0.2:5000", nil); rec.Code != http.StatusOK || rec.Body.String() != "ok" {
		t.Errorf("next request = %d %q, want 200 ok", rec.Code, rec.Body.String())
	}
}

func TestIntegration_PanicAfterFirstByte(t *testing.T) {
	logs := &logCapture{}
	srv := httptest.NewServer(observabilityChain(t, logs).Then(appHandler()))
	t.Cleanup(srv.Close)

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, srv.URL+"/late", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if body, err := io.ReadAll(resp.Body); err == nil {
		t.Errorf("an aborted response was read completely: %q", body)
	}
}

func TestIntegration_RequestIDs(t *testing.T) {
	logs := &logCapture{}
	h := observabilityChain(t, logs).Then(appHandler())

	generated := doRequest(t, h, "/ok", "10.0.0.2:5000", nil).Header().Get("X-Request-ID")
	reused := doRequest(t, h, "/ok", "10.0.0.2:5000", http.Header{"X-Request-Id": {"trace-42"}}).Header().Get("X-Request-ID")
	replaced := doRequest(t, h, "/ok", "10.0.0.2:5000", http.Header{"X-Request-Id": {"bad id"}}).Header().Get("X-Request-ID")
	if generated == "" || reused != "trace-42" || replaced == "" || replaced == "bad id" {
		t.Errorf("ids = %q, %q, %q", generated, reused, replaced)
	}
}

func TestIntegration_ForgedForwardingHeader(t *testing.T) {
	logs := &logCapture{}
	h := observabilityChain(t, logs).Then(appHandler())

	doRequest(t, h, "/ok", "203.0.113.5:6000", http.Header{"X-Forwarded-For": {"1.2.3.4"}})
	records := logs.take()
	if len(records) != 1 {
		t.Fatalf("got %d records, want 1", len(records))
	}
	if got := recordAttrs(records[0])["client"].String(); got != "203.0.113.5" {
		t.Errorf("client = %q, want the untrusted peer, not the forged address", got)
	}
}

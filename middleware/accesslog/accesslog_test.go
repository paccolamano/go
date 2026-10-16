package accesslog_test

import (
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/paccolamano/go/middleware"
	"github.com/paccolamano/go/middleware/accesslog"
	"github.com/paccolamano/go/middleware/requestid"
)

func mustNew(t *testing.T, logger *slog.Logger, opts ...accesslog.Option) middleware.Middleware {
	t.Helper()
	mw, err := accesslog.New(logger, opts...)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return mw
}

func newRequest(t *testing.T, method, target string) *http.Request {
	t.Helper()
	req := httptest.NewRequestWithContext(t.Context(), method, target, nil)
	req.RemoteAddr = "198.51.100.7:52000"
	return req
}

func serveOne(t *testing.T, h http.Handler, req *http.Request) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func onlyRecord(t *testing.T, logs *capture) slog.Record {
	t.Helper()
	records := logs.all()
	if len(records) != 1 {
		t.Fatalf("got %d records, want 1", len(records))
	}
	return records[0]
}

func TestAccessLog_InvalidConfig(t *testing.T) {
	logger := slog.New(&capture{})
	tests := []struct {
		name   string
		logger *slog.Logger
		opts   []accesslog.Option
	}{
		{"nil logger", nil, nil},
		{"nil level func", logger, []accesslog.Option{accesslog.WithLevel(nil)}},
		{"nil attrs func", logger, []accesslog.Option{accesslog.WithAttrs(nil)}},
		{"nil skip func", logger, []accesslog.Option{accesslog.WithSkip(nil)}},
		{"no header names", logger, []accesslog.Option{accesslog.WithHeaders()}},
		{"invalid header name", logger, []accesslog.Option{accesslog.WithHeaders("X Bad")}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mw, err := accesslog.New(tt.logger, tt.opts...)
			if !errors.Is(err, middleware.ErrInvalidConfig) || mw != nil {
				t.Fatalf("New = %v, %v; want nil, ErrInvalidConfig", mw, err)
			}
			if !strings.HasPrefix(err.Error(), "middleware: invalid configuration: accesslog: ") {
				t.Errorf("error message = %q", err)
			}
		})
	}
}

func TestAccessLog_Record(t *testing.T) {
	logs := &capture{}
	rid, err := requestid.New()
	if err != nil {
		t.Fatal(err)
	}
	h := middleware.New(rid, mustNew(t, slog.New(logs))).Then(http.HandlerFunc(
		func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusCreated)
			_, _ = io.WriteString(w, "hello")
		}))
	req := newRequest(t, http.MethodPost, "/items?token=secret")
	req.Header.Set("User-Agent", "test-agent/1.0")
	req.Header.Set("Authorization", "Bearer secret")
	rec := serveOne(t, h, req)

	r := onlyRecord(t, logs)
	if r.Message != "http request" || r.Level != slog.LevelInfo {
		t.Errorf("record = %q at %v", r.Message, r.Level)
	}
	a := attrs(r)
	checks := map[string]string{
		"method":     http.MethodPost,
		"path":       "/items",
		"proto":      "HTTP/1.1",
		"client":     "198.51.100.7",
		"user_agent": "test-agent/1.0",
		"request_id": rec.Header().Get("X-Request-ID"),
	}
	for k, want := range checks {
		if got := a[k].String(); got != want {
			t.Errorf("%s = %q, want %q", k, got, want)
		}
	}
	if a["status"].Int64() != http.StatusCreated || a["bytes"].Int64() != 5 {
		t.Errorf("status/bytes = %v/%v, want 201/5", a["status"], a["bytes"])
	}
	if a["duration"].Kind() != slog.KindDuration {
		t.Errorf("duration kind = %v", a["duration"].Kind())
	}
	for _, absent := range []string{"query", "panic", "header"} {
		if _, ok := a[absent]; ok {
			t.Errorf("unexpected key %q", absent)
		}
	}
}

func TestAccessLog_OptionalKeys(t *testing.T) {
	logs := &capture{}
	h := mustNew(t, slog.New(logs), accesslog.WithQuery(), accesslog.WithHeaders("accept", "X-Multi"))(
		http.NotFoundHandler())
	req := newRequest(t, http.MethodGet, "/search?q=go")
	req.Header.Set("Accept", "text/html")
	req.Header.Add("X-Multi", "a")
	req.Header.Add("X-Multi", "b")
	req.RemoteAddr = "not-an-address"
	serveOne(t, h, req)

	a := attrs(onlyRecord(t, logs))
	if a["query"].String() != "q=go" {
		t.Errorf("query = %q", a["query"])
	}
	if a["client"].String() != "not-an-address" {
		t.Errorf("client = %q, want the raw RemoteAddr", a["client"])
	}
	if _, ok := a["user_agent"]; ok {
		t.Error("user_agent present without a User-Agent header")
	}
	group := map[string]string{}
	for _, ga := range a["header"].Group() {
		group[ga.Key] = ga.Value.String()
	}
	if group["Accept"] != "text/html" || group["X-Multi"] != "a, b" || len(group) != 2 {
		t.Errorf("header group = %v", group)
	}
}

func TestAccessLog_Status(t *testing.T) {
	tests := []struct {
		name       string
		handler    http.HandlerFunc
		wantStatus int64
		wantBytes  int64
	}{
		{"write without status", func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "abc") }, 200, 3},
		{"nothing written", func(http.ResponseWriter, *http.Request) {}, 200, 0},
		{"early hints then final", func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusEarlyHints)
			w.WriteHeader(http.StatusAccepted)
		}, 202, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logs := &capture{}
			serveOne(t, mustNew(t, slog.New(logs))(tt.handler), newRequest(t, http.MethodGet, "/"))
			a := attrs(onlyRecord(t, logs))
			if a["status"].Int64() != tt.wantStatus || a["bytes"].Int64() != tt.wantBytes {
				t.Errorf("status/bytes = %v/%v, want %d/%d", a["status"], a["bytes"], tt.wantStatus, tt.wantBytes)
			}
		})
	}
}

func TestAccessLog_Panic(t *testing.T) {
	logs := &capture{}
	h := mustNew(t, slog.New(logs))(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("boom")
	}))
	func() {
		defer func() {
			if v := recover(); v != "boom" {
				t.Errorf("recovered %v, want the original panic to propagate", v)
			}
		}()
		serveOne(t, h, newRequest(t, http.MethodGet, "/"))
	}()
	a := attrs(onlyRecord(t, logs))
	if !a["panic"].Bool() || a["status"].Int64() != http.StatusInternalServerError {
		t.Errorf("panic/status = %v/%v, want true/500", a["panic"], a["status"])
	}
}

func TestAccessLog_Hijack(t *testing.T) {
	logs := &capture{}
	inner := mustNew(t, slog.New(logs))(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		conn, brw, err := http.NewResponseController(w).Hijack()
		if err != nil {
			t.Errorf("Hijack: %v", err)
			return
		}
		defer func() { _ = conn.Close() }()
		_, _ = brw.WriteString("HTTP/1.1 200 OK\r\nContent-Length: 0\r\nConnection: close\r\n\r\n")
		_ = brw.Flush()
	}))
	done := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer close(done) // the access log record is emitted before this runs
		inner.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, srv.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	<-done
	if got := attrs(onlyRecord(t, logs))["status"].Int64(); got != http.StatusSwitchingProtocols {
		t.Errorf("status = %d, want 101", got)
	}
}

func TestAccessLog_Callbacks(t *testing.T) {
	logs := &capture{}
	var gotResult accesslog.Result
	h := mustNew(t, slog.New(logs),
		accesslog.WithLevel(func(_ *http.Request, res accesslog.Result) slog.Level {
			gotResult = res
			if res.Status >= 500 {
				return slog.LevelError
			}
			return slog.LevelInfo
		}),
		accesslog.WithAttrs(func(*http.Request, accesslog.Result) []slog.Attr {
			return []slog.Attr{slog.String("user", "alice")}
		}),
	)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = io.WriteString(w, "upstream")
	}))
	serveOne(t, h, newRequest(t, http.MethodGet, "/"))

	r := onlyRecord(t, logs)
	if r.Level != slog.LevelError {
		t.Errorf("level = %v, want ERROR", r.Level)
	}
	if gotResult.Status != http.StatusBadGateway || gotResult.Bytes != 8 || gotResult.Panicked {
		t.Errorf("Result = %+v", gotResult)
	}
	var last slog.Attr
	r.Attrs(func(a slog.Attr) bool { last = a; return true })
	if last.Key != "user" || last.Value.String() != "alice" {
		t.Errorf("last attribute = %v, want the custom one", last)
	}
}

func TestAccessLog_Skip(t *testing.T) {
	logs := &capture{}
	h := mustNew(t, slog.New(logs), accesslog.WithSkip(func(r *http.Request) bool {
		return r.URL.Path == "/healthz"
	}))(http.NotFoundHandler())
	serveOne(t, h, newRequest(t, http.MethodGet, "/healthz"))
	if n := len(logs.all()); n != 0 {
		t.Errorf("got %d records for a skipped request, want 0", n)
	}
	serveOne(t, h, newRequest(t, http.MethodGet, "/other"))
	if n := len(logs.all()); n != 1 {
		t.Errorf("got %d records, want 1", n)
	}
}

func TestAccessLog_DisabledLevel(t *testing.T) {
	logs := &capture{minLevel: slog.LevelWarn}
	serveOne(t, mustNew(t, slog.New(logs))(http.NotFoundHandler()), newRequest(t, http.MethodGet, "/"))
	if n := len(logs.all()); n != 0 {
		t.Errorf("got %d records with INFO disabled, want 0", n)
	}
}

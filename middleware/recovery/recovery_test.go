package recovery_test

import (
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/paccolamano/go/middleware"
	"github.com/paccolamano/go/middleware/recovery"
	"github.com/paccolamano/go/middleware/requestid"
)

func mustNew(t *testing.T, logger *slog.Logger, opts ...recovery.Option) middleware.Middleware {
	t.Helper()
	mw, err := recovery.New(logger, opts...)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return mw
}

func panickingHandler(v any) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		h := w.Header()
		for _, k := range []string{"Content-Length", "Content-Type", "Content-Encoding", "Content-Range",
			"ETag", "Last-Modified", "Content-Disposition", "Trailer"} {
			h.Set(k, "set-by-handler")
		}
		h.Set("Cache-Control", "max-age=3600")
		panicNow(v)
	})
}

// panicNow is a named function so that its frame can be found in the stack.
func panicNow(v any) { panic(v) }

func TestRecovery_InvalidConfig(t *testing.T) {
	logger := slog.New(&capture{})
	tests := []struct {
		name   string
		logger *slog.Logger
		opts   []recovery.Option
	}{
		{"nil logger", nil, nil},
		{"nil reporter", logger, []recovery.Option{recovery.WithReporter(nil)}},
		{"nil reject func", logger, []recovery.Option{recovery.WithRejectFunc(nil)}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mw, err := recovery.New(tt.logger, tt.opts...)
			if !errors.Is(err, middleware.ErrInvalidConfig) || mw != nil {
				t.Fatalf("New = %v, %v; want nil, ErrInvalidConfig", mw, err)
			}
			if !strings.HasPrefix(err.Error(), "middleware: invalid configuration: recovery: ") {
				t.Errorf("error message = %q", err)
			}
		})
	}
}

func TestRecovery_PanicBeforeResponse(t *testing.T) {
	logs := &capture{}
	rid, err := requestid.New()
	if err != nil {
		t.Fatal(err)
	}
	h := middleware.New(rid, mustNew(t, slog.New(logs))).Then(panickingHandler("secret value"))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/orders", nil))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "secret value") {
		t.Error("the response body discloses the panic value")
	}
	for _, k := range []string{"Content-Length", "Content-Encoding", "Content-Range", "ETag", "Last-Modified",
		"Content-Disposition", "Trailer"} {
		if rec.Header().Get(k) == "set-by-handler" {
			t.Errorf("representation header %s was not removed", k)
		}
	}
	if rec.Header().Get("Content-Type") == "set-by-handler" {
		t.Error("Content-Type set by the handler was not removed")
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", got)
	}
	id := rec.Header().Get("X-Request-ID")
	if id == "" {
		t.Error("a header set by an outer middleware was lost")
	}

	records := logs.all()
	if len(records) != 1 {
		t.Fatalf("got %d log records, want 1", len(records))
	}
	r := records[0]
	if r.Message != "panic recovered" || r.Level != slog.LevelError {
		t.Errorf("record = %q at %v, want \"panic recovered\" at ERROR", r.Message, r.Level)
	}
	a := attrs(r)
	if a["panic"].String() != "secret value" {
		t.Errorf("panic = %q", a["panic"])
	}
	if !strings.Contains(a["stack"].String(), "panicNow") {
		t.Error("stack does not contain the panicking frame")
	}
	if a["method"].String() != http.MethodPost || a["path"].String() != "/orders" {
		t.Errorf("method/path = %v %v", a["method"], a["path"])
	}
	if a["request_id"].String() != id {
		t.Errorf("request_id = %q, want %q", a["request_id"], id)
	}
	if a["response_started"].Bool() {
		t.Error("response_started = true, want false")
	}
}

func TestRecovery_ReporterAndRejectFunc(t *testing.T) {
	var reported any
	var stack []byte
	var rejection middleware.Rejection
	mw := mustNew(t, slog.New(&capture{}),
		recovery.WithReporter(func(_ *http.Request, v any, s []byte) { reported, stack = v, s }),
		recovery.WithRejectFunc(func(w http.ResponseWriter, _ *http.Request, rej middleware.Rejection) {
			rejection = rej
			w.WriteHeader(rej.Status)
			_, _ = io.WriteString(w, `{"error":"internal"}`)
		}),
	)
	rec := httptest.NewRecorder()
	mw(panickingHandler(42)).ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil))

	if reported != 42 || len(stack) == 0 {
		t.Errorf("reporter got %v and %d stack bytes", reported, len(stack))
	}
	if rejection.Status != http.StatusInternalServerError || !errors.Is(rejection.Err, recovery.ErrPanic) {
		t.Errorf("rejection = %+v, want 500 with ErrPanic", rejection)
	}
	if rec.Body.String() != `{"error":"internal"}` {
		t.Errorf("body = %q, want the custom one", rec.Body.String())
	}
}

func TestRecovery_AbortHandlerIsPropagated(t *testing.T) {
	logs := &capture{}
	mw := mustNew(t, slog.New(logs))
	defer func() {
		v := recover()
		if err, _ := v.(error); !errors.Is(err, http.ErrAbortHandler) {
			t.Errorf("recovered %v, want http.ErrAbortHandler", v)
		}
		if n := len(logs.all()); n != 0 {
			t.Errorf("got %d log records, want 0", n)
		}
	}()
	mw(panickingHandler(http.ErrAbortHandler)).ServeHTTP(httptest.NewRecorder(),
		httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil))
}

func TestRecovery_PanicNil(t *testing.T) {
	rec := httptest.NewRecorder()
	mustNew(t, slog.New(&capture{}))(panickingHandler(nil)).ServeHTTP(rec,
		httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil))
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
}

func TestRecovery_PanicAfterResponseStarted(t *testing.T) {
	logs := &capture{}
	mw := mustNew(t, slog.New(logs))
	srv := httptest.NewServer(mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/ok" {
			_, _ = io.WriteString(w, "fine")
			return
		}
		_, _ = io.WriteString(w, "partial")
		_ = http.NewResponseController(w).Flush()
		panicNow("late failure")
	})))
	t.Cleanup(srv.Close)

	resp := get(t, srv.URL+"/late")
	body, err := io.ReadAll(resp.Body)
	if err == nil {
		t.Errorf("reading a response aborted mid-stream succeeded with %q, want an error", body)
	}
	records := logs.all()
	if len(records) != 1 || !attrs(records[0])["response_started"].Bool() {
		t.Fatalf("records = %v, want one with response_started=true", records)
	}

	resp = get(t, srv.URL+"/ok")
	if body, _ := io.ReadAll(resp.Body); string(body) != "fine" {
		t.Errorf("next request body = %q, want fine", body)
	}
}

func get(t *testing.T, url string) *http.Response {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	return resp
}

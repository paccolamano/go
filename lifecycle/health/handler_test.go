package health_test

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"
	"unicode"

	"github.com/paccolamano/go/lifecycle/health"
)

// record is one log record kept by logRecorder.
type record struct {
	level slog.Level
	msg   string
	attrs map[string]slog.Value
}

// logRecorder is a concurrency-safe slog.Handler that keeps every record.
type logRecorder struct {
	mu   *sync.Mutex
	recs *[]record
}

func newLogRecorder() *logRecorder { return &logRecorder{mu: &sync.Mutex{}, recs: &[]record{}} }

func (h *logRecorder) Enabled(context.Context, slog.Level) bool { return true }

func (h *logRecorder) Handle(_ context.Context, r slog.Record) error {
	rec := record{level: r.Level, msg: r.Message, attrs: map[string]slog.Value{}}
	r.Attrs(func(a slog.Attr) bool {
		rec.attrs[a.Key] = a.Value.Resolve()
		return true
	})
	h.mu.Lock()
	defer h.mu.Unlock()
	*h.recs = append(*h.recs, rec)
	return nil
}

func (h *logRecorder) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *logRecorder) WithGroup(string) slog.Handler      { return h }

func (h *logRecorder) all() []record {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]record(nil), *h.recs...)
}

func mustNew(tb testing.TB, checks []health.Check, opts ...health.Option) *health.Handler {
	tb.Helper()
	h, err := health.New(checks, opts...)
	if err != nil {
		tb.Fatalf("health.New: %v", err)
	}
	return h
}

func serve(ctx context.Context, h http.Handler, method, target string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequestWithContext(ctx, method, target, nil))
	return rec
}

func failing(msg string) func(context.Context) error {
	return func(context.Context) error { return errors.New(msg) }
}

// counted returns a check that counts its calls.
func counted(n *atomic.Int32, fn func(context.Context) error) func(context.Context) error {
	return func(ctx context.Context) error {
		n.Add(1)
		return fn(ctx)
	}
}

func TestHandler_AllPass(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		h := mustNew(t, []health.Check{{Name: "lifecycle", Func: ok}, {Name: "postgres", Func: ok}, {Name: "redis", Func: ok}})
		rec := serve(t.Context(), h, http.MethodGet, "/readyz")
		if rec.Code != http.StatusOK || rec.Body.String() != "ok\n" {
			t.Fatalf("response = %d %q, want 200 ok", rec.Code, rec.Body.String())
		}
		want := map[string]string{
			"Content-Type":           "text/plain; charset=utf-8",
			"Cache-Control":          "no-store",
			"X-Content-Type-Options": "nosniff",
			"Content-Length":         "3",
		}
		if len(rec.Header()) != len(want) {
			t.Errorf("headers = %v, want exactly %v", rec.Header(), want)
		}
		for k, v := range want {
			if got := rec.Header().Get(k); got != v {
				t.Errorf("%s = %q, want %q", k, got, v)
			}
		}
	})
}

func TestHandler_Failure(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		logs := newLogRecorder()
		h := mustNew(t, []health.Check{
			{Name: "lifecycle", Func: ok},
			{Name: "postgres", Func: ok},
			{Name: "redis", Func: failing("dial tcp db.internal:6379: connection refused")},
		}, health.WithLogger(slog.New(logs)))

		rec := serve(t.Context(), h, http.MethodGet, "/readyz")
		want := "[+]lifecycle ok\n[+]postgres ok\n[-]redis failed: reason withheld\nfailed\n"
		if rec.Code != http.StatusServiceUnavailable || rec.Body.String() != want {
			t.Fatalf("response = %d %q, want 503 %q", rec.Code, rec.Body.String(), want)
		}
		if strings.Contains(rec.Body.String(), "db.internal") {
			t.Fatal("the response discloses the error")
		}
		recs := logs.all()
		if len(recs) != 1 {
			t.Fatalf("logged %d records, want 1", len(recs))
		}
		r := recs[0]
		if r.msg != "health: check failed" || r.level != slog.LevelWarn || r.attrs["check"].String() != "redis" ||
			r.attrs["reason"].String() != "error" || !strings.Contains(r.attrs["error"].String(), "connection refused") {
			t.Fatalf("record = %+v, want health: check failed at WARN with check=redis reason=error and the error", r)
		}
	})
}

func TestHandler_Verbose(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		h := mustNew(t, []health.Check{{Name: "lifecycle", Func: ok}, {Name: "postgres", Func: ok}})
		for _, target := range []string{"/readyz?verbose", "/readyz?verbose=0"} {
			rec := serve(t.Context(), h, http.MethodGet, target)
			want := "[+]lifecycle ok\n[+]postgres ok\nok\n"
			if rec.Code != http.StatusOK || rec.Body.String() != want {
				t.Errorf("%s = %d %q, want 200 %q", target, rec.Code, rec.Body.String(), want)
			}
		}
	})
}

func TestHandler_Head(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		h := mustNew(t, []health.Check{{Name: "redis", Func: failing("down")}})
		get := serve(t.Context(), h, http.MethodGet, "/readyz")
		head := serve(t.Context(), h, http.MethodHead, "/readyz")
		if head.Code != get.Code || head.Body.Len() != 0 {
			t.Fatalf("HEAD = %d with %d body bytes, want %d and no body", head.Code, head.Body.Len(), get.Code)
		}
		for _, k := range []string{"Content-Type", "Cache-Control", "X-Content-Type-Options", "Content-Length"} {
			if head.Header().Get(k) != get.Header().Get(k) {
				t.Errorf("HEAD %s = %q, want %q", k, head.Header().Get(k), get.Header().Get(k))
			}
		}
	})
}

func TestHandler_MethodNotAllowed(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var calls atomic.Int32
		h := mustNew(t, []health.Check{{Name: "db", Func: counted(&calls, ok)}})
		rec := serve(t.Context(), h, http.MethodPost, "/readyz")
		if rec.Code != http.StatusMethodNotAllowed || rec.Header().Get("Allow") != "GET, HEAD" || rec.Body.String() != "method not allowed\n" {
			t.Fatalf("POST = %d %q Allow=%q, want 405 with Allow: GET, HEAD", rec.Code, rec.Body.String(), rec.Header().Get("Allow"))
		}
		if calls.Load() != 0 {
			t.Fatal("a check ran for a POST")
		}
	})
}

func TestHandler_NoChecks(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		h := mustNew(t, nil)
		if rec := serve(t.Context(), h, http.MethodGet, "/livez"); rec.Code != http.StatusOK || rec.Body.String() != "ok\n" {
			t.Fatalf("response = %d %q, want 200 ok", rec.Code, rec.Body.String())
		}
	})
}

func TestHandler_NeverLeaks(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		secrets := []string{"postgres://app@db.internal:5432/app", "10.0.0.7:6379", "secret-panic-value", "deadline"}
		var checks []health.Check
		for i := range 20 {
			name := "check-" + string(rune('a'+i))
			var fn func(context.Context) error
			switch i % 4 {
			case 0:
				fn = ok
			case 1:
				fn = failing("connect " + secrets[0] + " via " + secrets[1])
			case 2:
				fn = func(context.Context) error { panic(secrets[2]) }
			case 3:
				fn = func(ctx context.Context) error { <-ctx.Done(); return fmt.Errorf("%s: %w", secrets[3], ctx.Err()) }
			}
			checks = append(checks, health.Check{Name: name, Func: fn, Timeout: 100 * time.Millisecond})
		}
		h := mustNew(t, checks)
		for _, target := range []string{"/readyz", "/readyz?verbose"} {
			body := serve(t.Context(), h, http.MethodGet, target).Body.String()
			for _, s := range secrets {
				if strings.Contains(body, s) {
					t.Errorf("%s discloses %q:\n%s", target, s, body)
				}
			}
			if strings.ContainsFunc(body, unicode.IsDigit) {
				t.Errorf("%s contains digits, such as a duration:\n%s", target, body)
			}
		}
	})
}

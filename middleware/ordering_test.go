package middleware_test

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/paccolamano/go/middleware"
	"github.com/paccolamano/go/middleware/accesslog"
	"github.com/paccolamano/go/middleware/cors"
	"github.com/paccolamano/go/middleware/ratelimit"
	"github.com/paccolamano/go/middleware/realip"
	"github.com/paccolamano/go/middleware/recovery"
)

// The tests in this file document the ordering pitfalls listed in the root
// package documentation, and show that the recommended order avoids them.

func clientLogged(t *testing.T, accessFirst bool) string {
	t.Helper()
	logs := &logCapture{}
	alog := must(t)(accesslog.New(slog.New(logs)))
	rip := must(t)(realip.New(realip.TrustRanges("10.0.0.0/8")))
	chain := middleware.New(rip, alog)
	if accessFirst {
		chain = middleware.New(alog, rip)
	}
	doRequest(t, chain.Then(http.NotFoundHandler()), "/", "10.0.0.2:5000",
		http.Header{"X-Forwarded-For": {"198.51.100.7"}})
	records := logs.take()
	if len(records) != 1 {
		t.Fatalf("got %d records, want 1", len(records))
	}
	return recordAttrs(records[0])["client"].String()
}

func TestOrdering_AccessLogBeforeRealIP(t *testing.T) {
	if got := clientLogged(t, true); got != "10.0.0.2" {
		t.Errorf("accesslog before realip logged %q, want the proxy address 10.0.0.2", got)
	}
	if got := clientLogged(t, false); got != "198.51.100.7" {
		t.Errorf("recommended order logged %q, want the client 198.51.100.7", got)
	}
}

func TestOrdering_RecoveryOnlyCatchesInnerPanics(t *testing.T) {
	rec := must(t)(recovery.New(slog.New(slog.DiscardHandler)))
	panicky := func(http.Handler) http.Handler {
		return http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic("middleware bug") })
	}

	h := middleware.New(panicky, rec).Then(http.NotFoundHandler())
	func() {
		defer func() {
			if recover() == nil {
				t.Error("recovery placed after the panicking middleware caught its panic")
			}
		}()
		doRequest(t, h, "/", "192.0.2.1:1", nil)
	}()

	h = middleware.New(rec, panicky).Then(http.NotFoundHandler())
	if code := doRequest(t, h, "/", "192.0.2.1:1", nil).Code; code != http.StatusInternalServerError {
		t.Errorf("recommended order: status = %d, want 500", code)
	}
}

func TestOrdering_CORSAfterRateLimit(t *testing.T) {
	origin := http.Header{"Origin": {"https://app.example.com"}}
	refusal := func(chain middleware.Chain) *httptest.ResponseRecorder {
		h := chain.Then(http.NotFoundHandler())
		doRequest(t, h, "/", "192.0.2.1:1", origin)
		return doRequest(t, h, "/", "192.0.2.1:1", origin)
	}
	newCORS := func() middleware.Middleware { return must(t)(cors.New([]string{"https://app.example.com"})) }
	newLimit := func() middleware.Middleware { return must(t)(ratelimit.New(1, time.Hour)) }

	rec := refusal(middleware.New(newLimit(), newCORS()))
	if rec.Code != http.StatusTooManyRequests || rec.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Errorf("cors after ratelimit: %d with ACAO %q, want 429 without CORS headers",
			rec.Code, rec.Header().Get("Access-Control-Allow-Origin"))
	}
	rec = refusal(middleware.New(newCORS(), newLimit()))
	if rec.Code != http.StatusTooManyRequests || rec.Header().Get("Access-Control-Allow-Origin") != "https://app.example.com" {
		t.Errorf("recommended order: %d with ACAO %q, want 429 readable by the browser",
			rec.Code, rec.Header().Get("Access-Control-Allow-Origin"))
	}
}

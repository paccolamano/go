package middleware_test

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/paccolamano/go/middleware"
	"github.com/paccolamano/go/middleware/accesslog"
	"github.com/paccolamano/go/middleware/bodylimit"
	"github.com/paccolamano/go/middleware/contenttype"
	"github.com/paccolamano/go/middleware/cors"
	"github.com/paccolamano/go/middleware/decompress"
	"github.com/paccolamano/go/middleware/maxconcurrency"
	"github.com/paccolamano/go/middleware/ratelimit"
	"github.com/paccolamano/go/middleware/realip"
	"github.com/paccolamano/go/middleware/recovery"
	"github.com/paccolamano/go/middleware/requestid"
	"github.com/paccolamano/go/middleware/secureheaders"
)

// BenchmarkRecommendedChain measures every non-buffering middleware with its
// defaults, in the recommended order (SC-005: under 100µs per request).
func BenchmarkRecommendedChain(b *testing.B) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	chain := middleware.New(
		must(b)(realip.New(realip.TrustRanges("10.0.0.0/8"))),
		must(b)(requestid.New()),
		must(b)(accesslog.New(logger)),
		must(b)(recovery.New(logger)),
		must(b)(secureheaders.New()),
		must(b)(cors.New([]string{"https://app.example.com"})),
		must(b)(ratelimit.New(1_000_000_000, time.Second)),
		must(b)(maxconcurrency.New(1024)),
		must(b)(bodylimit.New(1<<20)),
		must(b)(decompress.New()),
		must(b)(contenttype.New([]string{"application/json"})),
	)
	h := chain.Then(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"ok":true}`)
	}))
	req := httptest.NewRequestWithContext(b.Context(), http.MethodGet, "/api/items", nil)
	req.RemoteAddr = "10.0.0.2:5000"
	req.Header.Set("X-Forwarded-For", "198.51.100.7")
	req.Header.Set("Origin", "https://app.example.com")
	req.Header.Set("User-Agent", "bench")
	b.ReportAllocs()
	for b.Loop() {
		h.ServeHTTP(httptest.NewRecorder(), req)
	}
}

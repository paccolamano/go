package accesslog_test

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/paccolamano/go/middleware/accesslog"
)

func BenchmarkAccessLog(b *testing.B) {
	mw, err := accesslog.New(slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		b.Fatal(err)
	}
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "ok")
	}))
	req := httptest.NewRequestWithContext(b.Context(), http.MethodGet, "/bench?x=1", nil)
	req.Header.Set("User-Agent", "bench")
	b.ReportAllocs()
	for b.Loop() {
		h.ServeHTTP(httptest.NewRecorder(), req)
	}
}

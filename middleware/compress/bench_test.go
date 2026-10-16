package compress_test

import (
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/paccolamano/go/middleware/compress"
)

func BenchmarkCompress(b *testing.B) {
	body, err := os.ReadFile("testdata/fixture.json")
	if err != nil {
		b.Fatal(err)
	}
	mw, err := compress.New()
	if err != nil {
		b.Fatal(err)
	}
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	}))
	req := httptest.NewRequestWithContext(b.Context(), http.MethodGet, "/", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	b.SetBytes(int64(len(body)))
	b.ReportAllocs()
	for b.Loop() {
		h.ServeHTTP(httptest.NewRecorder(), req)
	}
}

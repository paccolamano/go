package etag_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/paccolamano/go/middleware/etag"
)

func BenchmarkETag(b *testing.B) {
	body := []byte(strings.Repeat(`{"name":"item","price":10},`, 400))
	mw, err := etag.New()
	if err != nil {
		b.Fatal(err)
	}
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(body)
	}))
	req := httptest.NewRequestWithContext(b.Context(), http.MethodGet, "/", nil)
	b.SetBytes(int64(len(body)))
	b.ReportAllocs()
	for b.Loop() {
		h.ServeHTTP(httptest.NewRecorder(), req)
	}
}

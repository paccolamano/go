package compress_test

import (
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"

	"github.com/paccolamano/go/middleware/compress"
)

func ExampleNew() {
	mw, err := compress.New()
	if err != nil {
		panic(err)
	}
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"items":[`+strings.Repeat(`"item",`, 500)+`"last"]}`)
	}))
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/", nil)
	req.Header.Set("Accept-Encoding", "gzip, deflate")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	fmt.Println(rec.Header().Get("Content-Encoding"), rec.Header().Get("Vary"))
	// Output: gzip Accept-Encoding
}

func ExampleWithCoding() {
	// A coding implemented by another library plugs in through a Writer. Here
	// gzip at its fastest level stands in for, e.g., a brotli encoder.
	fast := func(w io.Writer) (compress.Writer, error) {
		return gzip.NewWriterLevel(w, gzip.BestSpeed)
	}
	mw, err := compress.New(compress.WithCoding("x-fast", fast))
	if err != nil {
		panic(err)
	}
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = io.WriteString(w, strings.Repeat("compress me ", 200))
	}))
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/", nil)
	req.Header.Set("Accept-Encoding", "gzip, x-fast")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	fmt.Println(rec.Header().Get("Content-Encoding"))
	// Output: x-fast
}

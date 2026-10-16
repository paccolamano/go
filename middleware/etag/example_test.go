package etag_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"

	"github.com/paccolamano/go/middleware/etag"
)

func ExampleNew() {
	mw, err := etag.New()
	if err != nil {
		panic(err)
	}
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "unchanged content")
	}))

	first := httptest.NewRecorder()
	h.ServeHTTP(first, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/", nil))
	tag := first.Header().Get("ETag")

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/", nil)
	req.Header.Set("If-None-Match", tag)
	second := httptest.NewRecorder()
	h.ServeHTTP(second, req)
	fmt.Println(first.Code, second.Code, second.Body.Len())
	// Output: 200 304 0
}

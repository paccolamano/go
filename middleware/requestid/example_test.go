package requestid_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"

	"github.com/paccolamano/go/middleware/requestid"
)

func ExampleNew() {
	mw, err := requestid.New()
	if err != nil {
		panic(err)
	}
	h := mw(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/", nil)
	req.Header.Set("X-Request-ID", "abc-123")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	fmt.Println(rec.Header().Get("X-Request-ID"))
	// Output: abc-123
}

func ExampleFromContext() {
	mw, err := requestid.New(requestid.WithGenerator(func() string { return "generated-1" }))
	if err != nil {
		panic(err)
	}
	h := mw(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		id, _ := requestid.FromContext(r.Context())
		fmt.Println("handling request", id)
	}))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/", nil))
	// Output: handling request generated-1
}

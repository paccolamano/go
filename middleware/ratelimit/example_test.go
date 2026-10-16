package ratelimit_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"time"

	"github.com/paccolamano/go/middleware/ratelimit"
)

func ExampleNew() {
	mw, err := ratelimit.New(2, time.Minute) // 2 requests per minute per client
	if err != nil {
		panic(err)
	}
	h := mw(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	for range 3 {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/", nil))
		fmt.Printf("%d Retry-After=%q\n", rec.Code, rec.Header().Get("Retry-After"))
	}
	// Output:
	// 200 Retry-After=""
	// 200 Retry-After=""
	// 429 Retry-After="30"
}

func ExampleWithKey() {
	mw, err := ratelimit.New(1, time.Hour, ratelimit.WithKey(func(r *http.Request) string {
		return r.Header.Get("X-Api-Key")
	}))
	if err != nil {
		panic(err)
	}
	h := mw(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	for _, key := range []string{"alpha", "beta", "alpha"} {
		req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/", nil)
		req.Header.Set("X-Api-Key", key)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		fmt.Println(key, rec.Code)
	}
	// Output:
	// alpha 200
	// beta 200
	// alpha 429
}

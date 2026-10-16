package maxconcurrency_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"time"

	"github.com/paccolamano/go/middleware/maxconcurrency"
)

func ExampleNew() {
	mw, err := maxconcurrency.New(1, maxconcurrency.WithRetryAfter(5*time.Second))
	if err != nil {
		panic(err)
	}
	release := make(chan struct{})
	started := make(chan struct{})
	h := mw(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		close(started)
		<-release // a slow request holds the only slot
	}))
	done := make(chan struct{})
	go func() {
		defer close(done)
		h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/", nil))
	}()
	<-started

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/", nil))
	fmt.Println(rec.Code, rec.Header().Get("Retry-After"))
	close(release)
	<-done
	// Output: 503 5
}

func ExampleWithQueue() {
	// Up to 8 requests may wait at most 2 seconds for one of the 64 slots.
	mw, err := maxconcurrency.New(64, maxconcurrency.WithQueue(8, 2*time.Second))
	if err != nil {
		panic(err)
	}
	rec := httptest.NewRecorder()
	mw(http.NotFoundHandler()).ServeHTTP(rec, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/", nil))
	fmt.Println(rec.Code)
	// Output: 404
}

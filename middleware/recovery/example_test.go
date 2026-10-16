package recovery_test

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"

	"github.com/paccolamano/go/middleware"
	"github.com/paccolamano/go/middleware/recovery"
)

func ExampleNew() {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	mw, err := recovery.New(logger)
	if err != nil {
		panic(err)
	}
	h := mw(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("unexpected nil map")
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/", nil))
	fmt.Println(rec.Code, rec.Body.String())
	// Output: 500 Internal Server Error
}

func ExampleWithRejectFunc() {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	mw, err := recovery.New(logger, recovery.WithRejectFunc(
		func(w http.ResponseWriter, _ *http.Request, rej middleware.Rejection) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(rej.Status)
			_, _ = io.WriteString(w, `{"error":"internal"}`)
		}))
	if err != nil {
		panic(err)
	}
	h := mw(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic("boom") }))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/", nil))
	fmt.Println(rec.Code, rec.Body.String())
	// Output: 500 {"error":"internal"}
}

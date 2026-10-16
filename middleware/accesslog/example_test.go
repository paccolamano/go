package accesslog_test

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"

	"github.com/paccolamano/go/middleware/accesslog"
)

// exampleLogger writes text records to stdout without the time and duration,
// so that the example output is deterministic.
func exampleLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		ReplaceAttr: func(_ []string, a slog.Attr) slog.Attr {
			if a.Key == slog.TimeKey || a.Key == "duration" {
				return slog.Attr{}
			}
			return a
		},
	}))
}

func ExampleNew() {
	mw, err := accesslog.New(exampleLogger())
	if err != nil {
		panic(err)
	}
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "hello")
	}))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/hello", nil))
	// Output: level=INFO msg="http request" method=GET path=/hello proto=HTTP/1.1 status=200 bytes=5 client=192.0.2.1
}

func ExampleWithSkip() {
	mw, err := accesslog.New(exampleLogger(), accesslog.WithSkip(func(r *http.Request) bool {
		return r.URL.Path == "/healthz"
	}))
	if err != nil {
		panic(err)
	}
	h := mw(http.NotFoundHandler())
	for _, path := range []string{"/healthz", "/missing"} {
		h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequestWithContext(context.Background(), http.MethodGet, path, nil))
	}
	// Output: level=INFO msg="http request" method=GET path=/missing proto=HTTP/1.1 status=404 bytes=19 client=192.0.2.1
}

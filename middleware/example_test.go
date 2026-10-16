package middleware_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"

	"github.com/paccolamano/go/middleware"
)

// tag returns a middleware that prints its name before and after the next
// handler runs.
func tag(name string) middleware.Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			fmt.Println("enter", name)
			next.ServeHTTP(w, r)
			fmt.Println("leave", name)
		})
	}
}

func hello() http.Handler {
	return http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		fmt.Println("handler")
	})
}

func serveOnce(h http.Handler) {
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/", nil)
	h.ServeHTTP(httptest.NewRecorder(), req)
}

func ExampleNew() {
	chain := middleware.New(tag("outer"), tag("inner"))
	serveOnce(chain.Then(hello()))
	// Output:
	// enter outer
	// enter inner
	// handler
	// leave inner
	// leave outer
}

func ExampleChain_Append() {
	base := middleware.New(tag("base"))
	admin := base.Append(tag("admin"))

	serveOnce(admin.Then(hello()))
	fmt.Println("---")
	serveOnce(base.Then(hello())) // base is unchanged
	// Output:
	// enter base
	// enter admin
	// handler
	// leave admin
	// leave base
	// ---
	// enter base
	// handler
	// leave base
}

func ExampleChain_Then() {
	mux := http.NewServeMux()
	mux.Handle("/", middleware.New(tag("logging")).Then(hello()))
	serveOnce(mux)
	// Output:
	// enter logging
	// handler
	// leave logging
}

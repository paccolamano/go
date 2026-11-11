package middleware_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"

	"github.com/paccolamano/go/middleware"
	"github.com/paccolamano/go/middleware/bodylimit"
	"github.com/paccolamano/go/middleware/cors"
	"github.com/paccolamano/go/middleware/requestid"
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

func ExampleChain_Use() {
	base := middleware.New().
		Use(requestid.New()).
		Append(tag("logging"))
	api := base.
		Use(cors.New([]string{"https://app.example.com"})).
		Use(bodylimit.New(1 << 20))
	if err := api.Err(); err != nil { // covers base too
		fmt.Println(err)
		return
	}
	serveOnce(api.Then(hello()))
	// Output:
	// enter logging
	// handler
	// leave logging
}

func ExampleChain_Err() {
	base := middleware.New().Use(requestid.New())
	api := base.
		Use(cors.New([]string{"app.example.com"})). // the scheme is missing
		Use(bodylimit.New(0))
	fmt.Println(base.Err())
	fmt.Println(api.Err())
	fmt.Println(errors.Is(api.Err(), middleware.ErrInvalidConfig))
	// Output:
	// <nil>
	// middleware: invalid configuration: cors: origins: "app.example.com" is not an origin (scheme://host[:port])
	// middleware: invalid configuration: bodylimit: maxBytes: must be greater than 0
	// true
}

func ExampleChain_AppendChain() {
	common := middleware.New(tag("common"))
	api := middleware.New(tag("api"))
	serveOnce(common.AppendChain(api).Then(hello()))

	broken := api.Use(bodylimit.New(0))
	fmt.Println(common.AppendChain(broken).Err())
	// Output:
	// enter common
	// enter api
	// handler
	// leave api
	// leave common
	// middleware: invalid configuration: bodylimit: maxBytes: must be greater than 0
}

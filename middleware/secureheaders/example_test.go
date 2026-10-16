package secureheaders_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"time"

	"github.com/paccolamano/go/middleware/secureheaders"
)

func ExampleNew() {
	mw, err := secureheaders.New()
	if err != nil {
		panic(err)
	}
	rec := httptest.NewRecorder()
	mw(http.NotFoundHandler()).ServeHTTP(rec, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/", nil))
	fmt.Println(rec.Header().Get("Content-Security-Policy"))
	fmt.Println(rec.Header().Get("X-Content-Type-Options"))
	// Output:
	// default-src 'none'; frame-ancestors 'none'
	// nosniff
}

func ExampleWithCSPNonce() {
	mw, err := secureheaders.New(
		secureheaders.WithCSP("script-src 'nonce-{nonce}' 'strict-dynamic'; object-src 'none'; base-uri 'none'"),
		secureheaders.WithCSPNonce(),
	)
	if err != nil {
		panic(err)
	}
	page := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nonce, _ := secureheaders.NonceFromContext(r.Context())
		// A template would render <script nonce="{{.Nonce}}">…</script>.
		fmt.Println("nonce matches header:",
			w.Header().Get("Content-Security-Policy") ==
				"script-src 'nonce-"+nonce+"' 'strict-dynamic'; object-src 'none'; base-uri 'none'")
	})
	mw(page).ServeHTTP(httptest.NewRecorder(), httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/", nil))
	// Output: nonce matches header: true
}

func ExampleWithHSTS() {
	mw, err := secureheaders.New(secureheaders.WithHSTS(secureheaders.HSTS{
		MaxAge:            365 * 24 * time.Hour,
		IncludeSubDomains: true,
	}))
	if err != nil {
		panic(err)
	}
	rec := httptest.NewRecorder()
	mw(http.NotFoundHandler()).ServeHTTP(rec, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/", nil))
	fmt.Println(rec.Header().Get("Strict-Transport-Security"))
	// Output: max-age=31536000; includeSubDomains
}

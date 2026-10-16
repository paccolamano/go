package realip_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"

	"github.com/paccolamano/go/middleware/realip"
)

func ExampleNew() {
	// Requests come through proxies in 10.0.0.0/8.
	mw, err := realip.New(realip.TrustRanges("10.0.0.0/8"))
	if err != nil {
		panic(err)
	}
	h := mw(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		peer, _ := realip.PeerFromContext(r.Context())
		fmt.Println("client:", r.RemoteAddr, "via:", peer)
	}))
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/", nil)
	req.RemoteAddr = "10.0.0.5:41000"
	req.Header.Set("X-Forwarded-For", "203.0.113.66, 198.51.100.7, 10.0.0.9")
	h.ServeHTTP(httptest.NewRecorder(), req)
	// Output: client: 198.51.100.7:0 via: 10.0.0.5:41000
}

func ExampleTrustHops() {
	// Every request passes through exactly one load balancer.
	mw, err := realip.New(realip.TrustHops(1))
	if err != nil {
		panic(err)
	}
	h := mw(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		fmt.Println("client:", r.RemoteAddr)
	}))
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/", nil)
	req.Header.Set("X-Forwarded-For", "203.0.113.66, 198.51.100.7")
	h.ServeHTTP(httptest.NewRecorder(), req)
	// Output: client: 198.51.100.7:0
}

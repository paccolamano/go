package ratelimit_test

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/paccolamano/go/middleware/ratelimit"
)

func BenchmarkRateLimit(b *testing.B) {
	mw, err := ratelimit.New(1_000_000, time.Second)
	if err != nil {
		b.Fatal(err)
	}
	h := mw(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	var next atomic.Int64
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		req := httptest.NewRequestWithContext(b.Context(), http.MethodGet, "/", nil)
		for pb.Next() {
			n := next.Add(1) % 10_000
			req.RemoteAddr = "10." + strconv.FormatInt(n/256%256, 10) + "." + strconv.FormatInt(n%256, 10) + ".1:1"
			h.ServeHTTP(httptest.NewRecorder(), req)
		}
	})
}

package realip_test

import (
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"

	"github.com/paccolamano/go/middleware"
	"github.com/paccolamano/go/middleware/realip"
)

const (
	trustedPeer   = "10.0.0.2:4000"
	untrustedPeer = "203.0.113.5:4000"
)

func fuzzHeader(f *testing.F, mw middleware.Middleware, header string, seeds ...string) {
	f.Helper()
	for _, s := range seeds {
		f.Add(s, true)
		f.Add(s, false)
	}
	f.Fuzz(func(t *testing.T, value string, trusted bool) {
		peer := untrustedPeer
		if trusted {
			peer = trustedPeer
		}
		var got string
		h := mw(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { got = r.RemoteAddr }))
		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil)
		req.RemoteAddr = peer
		req.Header[header] = []string{value}
		h.ServeHTTP(httptest.NewRecorder(), req)
		if !trusted && got != peer {
			t.Fatalf("untrusted peer rewritten to %q by %q", got, value)
		}
		if got != peer {
			ap, err := netip.ParseAddrPort(got)
			if err != nil || ap.Port() != 0 {
				t.Fatalf("rewritten RemoteAddr %q is not a valid address with port 0 (value %q)", got, value)
			}
		}
	})
}

func FuzzForwardedFor(f *testing.F) {
	mw, err := realip.New(realip.TrustRanges("10.0.0.0/8"))
	if err != nil {
		f.Fatal(err)
	}
	fuzzHeader(f, mw, "X-Forwarded-For", "198.51.100.1, 10.1.1.1", "garbage", "[::1]:80, 10.0.0.1", "")
}

func FuzzForwarded(f *testing.F) {
	mw, err := realip.New(realip.TrustRanges("10.0.0.0/8"), realip.WithHeader("Forwarded"))
	if err != nil {
		f.Fatal(err)
	}
	fuzzHeader(f, mw, "Forwarded", `for="[2001:db8::1]:4711"`, `for=198.51.100.1;proto=https, for=10.0.0.9`,
		`for="\"quoted\""`, `for=unknown`, `;;,,=`)
}

package cors_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/paccolamano/go/middleware/cors"
)

func FuzzOrigin(f *testing.F) {
	for _, s := range []string{"https://api.example.com", "https://example.com", "http://api.example.com",
		"https://api.example.com:8443", "null", "HTTPS://API.EXAMPLE.COM", "https://evil.com/.example.com", ""} {
		f.Add(s)
	}
	mw, err := cors.New([]string{"https://*.example.com", "https://exact.example.net"})
	if err != nil {
		f.Fatal(err)
	}
	f.Fuzz(func(t *testing.T, origin string) {
		res := do(t, mw, http.MethodGet, origin, nil)
		acao := res.header.Get("Access-Control-Allow-Origin")
		if acao == "" {
			return
		}
		if acao != origin {
			t.Fatalf("Access-Control-Allow-Origin = %q for origin %q", acao, origin)
		}
		lower := strings.ToLower(origin)
		if lower == "https://exact.example.net" {
			return
		}
		host, ok := strings.CutPrefix(lower, "https://")
		if !ok || !strings.HasSuffix(host, ".example.com") || strings.ContainsAny(host, "/:?#@") {
			t.Fatalf("origin %q was allowed by the pattern", origin)
		}
	})
}

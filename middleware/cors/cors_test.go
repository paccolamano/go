package cors_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/paccolamano/go/middleware"
	"github.com/paccolamano/go/middleware/cors"
)

func mustNew(t *testing.T, origins []string, opts ...cors.Option) middleware.Middleware {
	t.Helper()
	mw, err := cors.New(origins, opts...)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return mw
}

type result struct {
	code    int
	header  http.Header
	invoked bool
}

func do(t *testing.T, mw middleware.Middleware, method, origin string, header http.Header) result {
	t.Helper()
	var res result
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		res.invoked = true
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequestWithContext(t.Context(), method, "/resource", nil)
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	for k, v := range header {
		req.Header[k] = v
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	res.code, res.header = rec.Code, rec.Header()
	return res
}

func preflight(method string, headers ...string) http.Header {
	h := http.Header{"Access-Control-Request-Method": {method}}
	if len(headers) > 0 {
		h["Access-Control-Request-Headers"] = []string{strings.Join(headers, ", ")}
	}
	return h
}

func TestCORS_InvalidConfig(t *testing.T) {
	allow := func(string) bool { return true }
	tests := []struct {
		name    string
		origins []string
		opts    []cors.Option
	}{
		{"no origins", nil, nil},
		{"star not alone", []string{"*", "https://a.example"}, nil},
		{"star with credentials", []string{"*"}, []cors.Option{cors.WithCredentials()}},
		{"star with origin func", []string{"*"}, []cors.Option{cors.WithOriginFunc(allow)}},
		{"nil origin func", []string{"https://a.example"}, []cors.Option{cors.WithOriginFunc(nil)}},
		{"origin with path", []string{"https://a.example/app"}, nil},
		{"origin with query", []string{"https://a.example?x=1"}, nil},
		{"origin with fragment", []string{"https://a.example#top"}, nil},
		{"origin without scheme", []string{"a.example"}, nil},
		{"origin with empty port", []string{"https://a.example:"}, nil},
		{"bad pattern", []string{"https://*"}, nil},
		{"pattern wildcard in the middle", []string{"https://a.*.example"}, nil},
		{"no methods", []string{"https://a.example"}, []cors.Option{cors.WithMethods()}},
		{"bad method", []string{"https://a.example"}, []cors.Option{cors.WithMethods("GET POST")}},
		{"bad header", []string{"https://a.example"}, []cors.Option{cors.WithHeaders("X Bad")}},
		{"star header not alone", []string{"https://a.example"}, []cors.Option{cors.WithHeaders("*", "X-A")}},
		{"bad exposed header", []string{"https://a.example"}, []cors.Option{cors.WithExposedHeaders("X Bad")}},
		{"negative max age", []string{"https://a.example"}, []cors.Option{cors.WithMaxAge(-time.Second)}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mw, err := cors.New(tt.origins, tt.opts...)
			if !errors.Is(err, middleware.ErrInvalidConfig) || mw != nil {
				t.Fatalf("New = %v, %v; want nil, ErrInvalidConfig", mw, err)
			}
			if !strings.HasPrefix(err.Error(), "middleware: invalid configuration: cors: ") {
				t.Errorf("error message = %q", err)
			}
		})
	}
	if _, err := cors.New(nil, cors.WithOriginFunc(allow)); err != nil {
		t.Errorf("an origin function without a list must be valid: %v", err)
	}
}

func TestCORS_ActualRequests(t *testing.T) {
	mw := mustNew(t, []string{"https://App.Example.com", "https://*.example.org", "null"},
		cors.WithCredentials(), cors.WithExposedHeaders("X-Total-Count"))
	tests := []struct {
		name    string
		origin  string
		allowed bool
	}{
		{"exact, case-normalized", "https://app.example.com", true},
		{"subdomain pattern", "https://api.eu.example.org", true},
		{"pattern does not match apex", "https://example.org", false},
		{"pattern does not match other scheme", "http://api.example.org", false},
		{"explicit null", "null", true},
		{"other origin", "https://evil.example", false},
		{"no origin", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := do(t, mw, http.MethodGet, tt.origin, nil)
			if !res.invoked {
				t.Fatal("next was not invoked")
			}
			if !strings.Contains(strings.Join(res.header.Values("Vary"), ","), "Origin") {
				t.Error("missing Vary: Origin")
			}
			acao := res.header.Get("Access-Control-Allow-Origin")
			if tt.allowed {
				if acao != tt.origin || res.header.Get("Access-Control-Allow-Credentials") != "true" ||
					res.header.Get("Access-Control-Expose-Headers") != "X-Total-Count" {
					t.Errorf("grant headers = %v", res.header)
				}
				return
			}
			for k := range res.header {
				if strings.HasPrefix(k, "Access-Control-") {
					t.Errorf("unexpected %s on a disallowed request", k)
				}
			}
		})
	}
}

func TestCORS_NullNotMatchedByFunc(t *testing.T) {
	mw := mustNew(t, nil, cors.WithOriginFunc(func(string) bool { return true }))
	if res := do(t, mw, http.MethodGet, "null", nil); res.header.Get("Access-Control-Allow-Origin") != "" {
		t.Error("the null origin was allowed by the decision function")
	}
	if res := do(t, mw, http.MethodGet, "https://any.example", nil); res.header.Get("Access-Control-Allow-Origin") != "https://any.example" {
		t.Error("the decision function was not consulted")
	}
}

func TestCORS_AnyOrigin(t *testing.T) {
	mw := mustNew(t, []string{"*"})
	for _, origin := range []string{"https://a.example", ""} {
		res := do(t, mw, http.MethodGet, origin, nil)
		if res.header.Get("Access-Control-Allow-Origin") != "*" {
			t.Errorf("origin %q: Access-Control-Allow-Origin = %q, want *", origin, res.header.Get("Access-Control-Allow-Origin"))
		}
		if len(res.header.Values("Vary")) != 0 {
			t.Errorf("origin %q: Vary = %q, want none", origin, res.header.Values("Vary"))
		}
	}
	if do(t, mw, http.MethodGet, "null", nil).header.Get("Access-Control-Allow-Origin") != "*" {
		t.Error("any-origin mode must answer * regardless of the origin")
	}
}

func TestCORS_Preflight(t *testing.T) {
	mw := mustNew(t, []string{"https://app.example.com"},
		cors.WithMethods("PUT", "DELETE"), cors.WithHeaders("Content-Type", "X-Token"), cors.WithCredentials())
	const vary = "Origin, Access-Control-Request-Method, Access-Control-Request-Headers"

	t.Run("allowed", func(t *testing.T) {
		res := do(t, mw, http.MethodOptions, "https://app.example.com", preflight("PUT", "x-token", "content-type"))
		if res.invoked || res.code != http.StatusNoContent {
			t.Fatalf("invoked=%v code=%d, want a 204 answered by the middleware", res.invoked, res.code)
		}
		checks := map[string]string{
			"Access-Control-Allow-Origin":      "https://app.example.com",
			"Access-Control-Allow-Methods":     "PUT, DELETE",
			"Access-Control-Allow-Headers":     "Content-Type, X-Token",
			"Access-Control-Allow-Credentials": "true",
			"Access-Control-Max-Age":           "600",
		}
		for k, want := range checks {
			if got := res.header.Get(k); got != want {
				t.Errorf("%s = %q, want %q", k, got, want)
			}
		}
		if got := strings.Join(res.header.Values("Vary"), ", "); got != vary {
			t.Errorf("Vary = %q, want %q", got, vary)
		}
	})
	t.Run("safelisted method always passes", func(t *testing.T) {
		res := do(t, mw, http.MethodOptions, "https://app.example.com", preflight("POST"))
		if res.header.Get("Access-Control-Allow-Origin") == "" {
			t.Error("a preflight for POST was refused")
		}
		if res.header.Get("Access-Control-Allow-Headers") != "" {
			t.Error("Access-Control-Allow-Headers sent although no header was requested")
		}
	})
	for name, h := range map[string]http.Header{
		"disallowed method": preflight("PATCH"),
		"disallowed header": preflight("PUT", "X-Other"),
	} {
		t.Run(name, func(t *testing.T) {
			res := do(t, mw, http.MethodOptions, "https://app.example.com", h)
			if res.invoked || res.code != http.StatusNoContent {
				t.Fatalf("invoked=%v code=%d, want 204", res.invoked, res.code)
			}
			for k := range res.header {
				if strings.HasPrefix(k, "Access-Control-") {
					t.Errorf("unexpected %s", k)
				}
			}
			if got := strings.Join(res.header.Values("Vary"), ", "); got != vary {
				t.Errorf("Vary = %q, want %q", got, vary)
			}
		})
	}
	t.Run("disallowed origin", func(t *testing.T) {
		res := do(t, mw, http.MethodOptions, "https://evil.example", preflight("PUT"))
		if res.invoked || res.header.Get("Access-Control-Allow-Origin") != "" {
			t.Errorf("invoked=%v headers=%v", res.invoked, res.header)
		}
	})
	t.Run("options without request method is ordinary", func(t *testing.T) {
		res := do(t, mw, http.MethodOptions, "https://app.example.com", nil)
		if !res.invoked || res.header.Get("Access-Control-Allow-Origin") != "https://app.example.com" {
			t.Errorf("invoked=%v headers=%v", res.invoked, res.header)
		}
	})
}

func TestCORS_AnyRequestedHeader(t *testing.T) {
	mw := mustNew(t, []string{"https://app.example.com"}, cors.WithHeaders("*"))
	res := do(t, mw, http.MethodOptions, "https://app.example.com", preflight("GET", "X-One", "x-two"))
	if got := res.header.Get("Access-Control-Allow-Headers"); got != "X-One, x-two" {
		t.Errorf("Access-Control-Allow-Headers = %q, want the requested list echoed", got)
	}
}

func TestCORS_MaxAge(t *testing.T) {
	res := do(t, mustNew(t, []string{"https://a.example"}, cors.WithMaxAge(0)),
		http.MethodOptions, "https://a.example", preflight("GET"))
	if _, ok := res.header["Access-Control-Max-Age"]; ok {
		t.Error("Access-Control-Max-Age sent with WithMaxAge(0)")
	}
	res = do(t, mustNew(t, []string{"https://a.example"}, cors.WithMaxAge(90*time.Second+500*time.Millisecond)),
		http.MethodOptions, "https://a.example", preflight("GET"))
	if got := res.header.Get("Access-Control-Max-Age"); got != "90" {
		t.Errorf("Access-Control-Max-Age = %q, want 90", got)
	}
}

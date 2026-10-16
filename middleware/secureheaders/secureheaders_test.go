package secureheaders_test

import (
	"encoding/base64"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/paccolamano/go/middleware"
	"github.com/paccolamano/go/middleware/secureheaders"
)

func defaultHeaders() map[string]string {
	return map[string]string{
		"Content-Security-Policy":           "default-src 'none'; frame-ancestors 'none'",
		"Cross-Origin-Embedder-Policy":      "require-corp",
		"Cross-Origin-Opener-Policy":        "same-origin",
		"Cross-Origin-Resource-Policy":      "same-origin",
		"Permissions-Policy":                "accelerometer=(), camera=(), display-capture=(), geolocation=(), gyroscope=(), hid=(), magnetometer=(), microphone=(), midi=(), payment=(), serial=(), usb=(), xr-spatial-tracking=()",
		"Referrer-Policy":                   "no-referrer",
		"X-Content-Type-Options":            "nosniff",
		"X-Frame-Options":                   "DENY",
		"X-Permitted-Cross-Domain-Policies": "none",
	}
}

func mustNew(t *testing.T, opts ...secureheaders.Option) middleware.Middleware {
	t.Helper()
	mw, err := secureheaders.New(opts...)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return mw
}

func serve(t *testing.T, h http.Handler) http.Header {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil))
	return rec.Header()
}

var noop = http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})

func TestSecureHeaders_Defaults(t *testing.T) {
	h := serve(t, mustNew(t)(noop))
	want := defaultHeaders()
	for k, v := range want {
		if got := h.Get(k); got != v {
			t.Errorf("%s = %q, want %q", k, got, v)
		}
	}
	for _, k := range []string{"Strict-Transport-Security", "Content-Security-Policy-Report-Only"} {
		if h.Get(k) != "" {
			t.Errorf("%s is set by default", k)
		}
	}
	if len(h) != len(want) {
		t.Errorf("got %d headers, want exactly %d: %v", len(h), len(want), h)
	}
}

func TestSecureHeaders_InvalidConfig(t *testing.T) {
	tests := []struct {
		name string
		opts []secureheaders.Option
	}{
		{"header name not a token", []secureheaders.Option{secureheaders.WithHeader("X Bad", "v")}},
		{"header value with CR", []secureheaders.Option{secureheaders.WithHeader("X-Test", "a\rb")}},
		{"header value with LF", []secureheaders.Option{secureheaders.WithHeader("X-Test", "a\nb")}},
		{"header value with NUL", []secureheaders.Option{secureheaders.WithHeader("X-Test", "a\x00b")}},
		{"WithHeader on CSP", []secureheaders.Option{secureheaders.WithHeader("content-security-policy", "default-src 'self'")}},
		{"WithHeader on CSP report-only", []secureheaders.Option{secureheaders.WithHeader("Content-Security-Policy-Report-Only", "default-src 'self'")}},
		{"WithHeader on HSTS", []secureheaders.Option{secureheaders.WithHeader("Strict-Transport-Security", "max-age=1")}},
		{"WithoutHeader name not a token", []secureheaders.Option{secureheaders.WithoutHeader("X Bad")}},
		{"HSTS max age below 1s", []secureheaders.Option{secureheaders.WithHSTS(secureheaders.HSTS{MaxAge: time.Millisecond})}},
		{"HSTS preload without subdomains", []secureheaders.Option{secureheaders.WithHSTS(secureheaders.HSTS{MaxAge: 2 * 365 * 24 * time.Hour, Preload: true})}},
		{"HSTS preload below one year", []secureheaders.Option{secureheaders.WithHSTS(secureheaders.HSTS{MaxAge: 30 * 24 * time.Hour, IncludeSubDomains: true, Preload: true})}},
		{"empty CSP", []secureheaders.Option{secureheaders.WithCSP("")}},
		{"CSP with LF", []secureheaders.Option{secureheaders.WithCSP("default-src 'self'\nscript-src 'self'")}},
		{"CSP directive without name", []secureheaders.Option{secureheaders.WithCSP("default-src 'self'; 'unsafe-inline'")}},
		{"CSP directive name uppercase", []secureheaders.Option{secureheaders.WithCSP("Default-Src 'self'")}},
		{"empty report-only CSP", []secureheaders.Option{secureheaders.WithCSPReportOnly(" ; ")}},
		{"nonce without placeholder", []secureheaders.Option{secureheaders.WithCSPNonce()}},
		{"placeholder without nonce", []secureheaders.Option{secureheaders.WithCSP("script-src 'nonce-{nonce}'")}},
		{"placeholder in report-only without nonce", []secureheaders.Option{secureheaders.WithCSPReportOnly("script-src 'nonce-{nonce}'")}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mw, err := secureheaders.New(tt.opts...)
			if !errors.Is(err, middleware.ErrInvalidConfig) || mw != nil {
				t.Fatalf("New = %v, %v; want nil, ErrInvalidConfig", mw, err)
			}
			if !strings.HasPrefix(err.Error(), "middleware: invalid configuration: secureheaders: ") {
				t.Errorf("error message = %q", err)
			}
		})
	}
}

func TestSecureHeaders_OverrideAndRemove(t *testing.T) {
	h := serve(t, mustNew(t,
		secureheaders.WithHeader("x-frame-options", "SAMEORIGIN"),
		secureheaders.WithHeader("Cross-Origin-Embedder-Policy", "credentialless"),
		secureheaders.WithHeader("X-Custom", "1"),
		secureheaders.WithoutHeader("Content-Security-Policy"),
		secureheaders.WithoutHeader("Not-There"),
	)(noop))
	checks := map[string]string{
		"X-Frame-Options":              "SAMEORIGIN",
		"Cross-Origin-Embedder-Policy": "credentialless",
		"X-Custom":                     "1",
		"Content-Security-Policy":      "",
		"Referrer-Policy":              "no-referrer",
	}
	for k, want := range checks {
		if got := h.Get(k); got != want {
			t.Errorf("%s = %q, want %q", k, got, want)
		}
	}
}

func TestSecureHeaders_HSTS(t *testing.T) {
	tests := []struct {
		hsts secureheaders.HSTS
		want string
	}{
		{secureheaders.HSTS{MaxAge: time.Hour}, "max-age=3600"},
		{secureheaders.HSTS{MaxAge: 2 * time.Second, IncludeSubDomains: true}, "max-age=2; includeSubDomains"},
		{secureheaders.HSTS{MaxAge: 365 * 24 * time.Hour, IncludeSubDomains: true, Preload: true}, "max-age=31536000; includeSubDomains; preload"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if got := serve(t, mustNew(t, secureheaders.WithHSTS(tt.hsts))(noop)).Get("Strict-Transport-Security"); got != tt.want {
				t.Errorf("Strict-Transport-Security = %q, want %q", got, tt.want)
			}
		})
	}
	h := serve(t, mustNew(t, secureheaders.WithHSTS(secureheaders.HSTS{MaxAge: time.Hour}),
		secureheaders.WithoutHeader("Strict-Transport-Security"))(noop))
	if h.Get("Strict-Transport-Security") != "" {
		t.Error("WithoutHeader did not remove HSTS")
	}
}

func TestSecureHeaders_CSPAndReportOnly(t *testing.T) {
	h := serve(t, mustNew(t,
		secureheaders.WithCSP("default-src 'self'; img-src 'self' data:;"),
		secureheaders.WithCSPReportOnly("default-src 'none'; report-to csp"),
	)(noop))
	if got := h.Get("Content-Security-Policy"); got != "default-src 'self'; img-src 'self' data:;" {
		t.Errorf("Content-Security-Policy = %q", got)
	}
	if got := h.Get("Content-Security-Policy-Report-Only"); got != "default-src 'none'; report-to csp" {
		t.Errorf("Content-Security-Policy-Report-Only = %q", got)
	}
}

func TestSecureHeaders_Nonce(t *testing.T) {
	var seen []string
	mw := mustNew(t,
		secureheaders.WithCSP("script-src 'nonce-{nonce}' 'strict-dynamic'; style-src 'nonce-{nonce}'"),
		secureheaders.WithCSPReportOnly("script-src 'nonce-{nonce}'"),
		secureheaders.WithCSPNonce(),
	)
	h := mw(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		n, ok := secureheaders.NonceFromContext(r.Context())
		if !ok {
			t.Error("NonceFromContext reported false")
		}
		seen = append(seen, n)
	}))
	var nonces []string
	for i := range 2 {
		hdr := serve(t, h)
		n := seen[i]
		if len(n) != 24 {
			t.Fatalf("nonce %q has length %d, want 24", n, len(n))
		}
		if raw, err := base64.StdEncoding.DecodeString(n); err != nil || len(raw) != 16 {
			t.Errorf("nonce %q is not 16 bytes of standard base64: %v", n, err)
		}
		wantCSP := "script-src 'nonce-" + n + "' 'strict-dynamic'; style-src 'nonce-" + n + "'"
		if got := hdr.Get("Content-Security-Policy"); got != wantCSP {
			t.Errorf("Content-Security-Policy = %q, want %q", got, wantCSP)
		}
		if got := hdr.Get("Content-Security-Policy-Report-Only"); got != "script-src 'nonce-"+n+"'" {
			t.Errorf("Content-Security-Policy-Report-Only = %q", got)
		}
		nonces = append(nonces, n)
	}
	if nonces[0] == nonces[1] {
		t.Error("two requests got the same nonce")
	}
}

func TestSecureHeaders_NonceAbsentWithoutOption(t *testing.T) {
	h := mustNew(t)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		if n, ok := secureheaders.NonceFromContext(r.Context()); ok || n != "" {
			t.Errorf("NonceFromContext = %q, %v; want \"\", false", n, ok)
		}
	}))
	serve(t, h)
}

func TestSecureHeaders_HandlerWins(t *testing.T) {
	h := serve(t, mustNew(t)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-Frame-Options", "SAMEORIGIN")
		w.Header().Del("Cross-Origin-Embedder-Policy")
	})))
	if h.Get("X-Frame-Options") != "SAMEORIGIN" || h.Get("Cross-Origin-Embedder-Policy") != "" {
		t.Errorf("handler changes were overridden: %v", h)
	}
}

func TestSecureHeaders_NoSharedValues(t *testing.T) {
	mw := mustNew(t)
	first := serve(t, mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Add("Referrer-Policy", "strict-origin")
	})))
	if got := first.Values("Referrer-Policy"); len(got) != 2 {
		t.Fatalf("Referrer-Policy = %q, want two values", got)
	}
	if got := serve(t, mw(noop)).Values("Referrer-Policy"); len(got) != 1 || got[0] != "no-referrer" {
		t.Errorf("next response Referrer-Policy = %q, want [no-referrer]", got)
	}
}

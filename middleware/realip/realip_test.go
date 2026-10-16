package realip_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/paccolamano/go/middleware"
	"github.com/paccolamano/go/middleware/realip"
)

type seen struct {
	remoteAddr string
	peer       string
	peerOK     bool
}

// run serves one request with the given peer and header lines, and returns
// what the next handler saw.
func run(t *testing.T, mw middleware.Middleware, peer, header string, values ...string) seen {
	t.Helper()
	var s seen
	h := mw(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		s.remoteAddr = r.RemoteAddr
		s.peer, s.peerOK = realip.PeerFromContext(r.Context())
	}))
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil)
	req.RemoteAddr = peer
	for _, v := range values {
		req.Header.Add(header, v)
	}
	h.ServeHTTP(httptest.NewRecorder(), req)
	return s
}

func mustNew(t *testing.T, trust realip.Trust, opts ...realip.Option) middleware.Middleware {
	t.Helper()
	mw, err := realip.New(trust, opts...)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return mw
}

func TestRealIP_InvalidConfig(t *testing.T) {
	tests := []struct {
		name  string
		trust realip.Trust
		opts  []realip.Option
	}{
		{"zero trust", realip.Trust{}, nil},
		{"no ranges", realip.TrustRanges(), nil},
		{"bad cidr", realip.TrustRanges("10.0.0.0/33"), nil},
		{"not an address", realip.TrustRanges("proxy.internal"), nil},
		{"zero hops", realip.TrustHops(0), nil},
		{"negative hops", realip.TrustHops(-1), nil},
		{"bad header", realip.TrustHops(1), []realip.Option{realip.WithHeader("X Forwarded")}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mw, err := realip.New(tt.trust, tt.opts...)
			if !errors.Is(err, middleware.ErrInvalidConfig) || mw != nil {
				t.Fatalf("New = %v, %v; want nil, ErrInvalidConfig", mw, err)
			}
			if !strings.HasPrefix(err.Error(), "middleware: invalid configuration: realip: ") {
				t.Errorf("error message = %q", err)
			}
		})
	}
}

func TestRealIP_Ranges(t *testing.T) {
	mw := mustNew(t, realip.TrustRanges("10.0.0.0/8", "192.0.2.1"))
	const xff = "X-Forwarded-For"
	tests := []struct {
		name   string
		peer   string
		values []string
		want   string // "" means unchanged
	}{
		{"untrusted peer ignores header", "203.0.113.5:4000", []string{"198.51.100.1"}, ""},
		{"rightmost untrusted", "10.0.0.2:4000", []string{"6.6.6.6, 198.51.100.1, 10.1.1.1"}, "198.51.100.1:0"},
		{"single ip trust entry", "192.0.2.1:4000", []string{"198.51.100.1"}, "198.51.100.1:0"},
		{"all trusted uses leftmost", "10.0.0.2:4000", []string{"10.9.9.9, 10.1.1.1"}, "10.9.9.9:0"},
		{"several header lines", "10.0.0.2:4000", []string{"6.6.6.6", "198.51.100.1, 10.1.1.1"}, "198.51.100.1:0"},
		{"malformed entry on the walk", "10.0.0.2:4000", []string{"198.51.100.1, garbage, 10.1.1.1"}, ""},
		{"entry with port", "10.0.0.2:4000", []string{"198.51.100.1:5555"}, "198.51.100.1:0"},
		{"ipv6 entry", "10.0.0.2:4000", []string{"2001:db8::7"}, "[2001:db8::7]:0"},
		{"ipv4-mapped entry", "10.0.0.2:4000", []string{"::ffff:198.51.100.1"}, "198.51.100.1:0"},
		{"no header", "10.0.0.2:4000", nil, ""},
		{"unparseable peer", "@", []string{"198.51.100.1"}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := run(t, mw, tt.peer, xff, tt.values...)
			if tt.want == "" {
				if s.remoteAddr != tt.peer || s.peerOK {
					t.Errorf("RemoteAddr = %q (peer %q, %v), want unchanged %q", s.remoteAddr, s.peer, s.peerOK, tt.peer)
				}
				return
			}
			if s.remoteAddr != tt.want {
				t.Errorf("RemoteAddr = %q, want %q", s.remoteAddr, tt.want)
			}
			if !s.peerOK || s.peer != tt.peer {
				t.Errorf("PeerFromContext = %q, %v; want %q, true", s.peer, s.peerOK, tt.peer)
			}
		})
	}
}

func TestRealIP_Hops(t *testing.T) {
	mw := mustNew(t, realip.TrustHops(2))
	tests := []struct {
		name  string
		value string
		want  string
	}{
		{"second from right", "6.6.6.6, 198.51.100.1, 203.0.113.9", "198.51.100.1:0"},
		{"exactly two entries", "198.51.100.1, 203.0.113.9", "198.51.100.1:0"},
		{"fewer entries than hops", "203.0.113.9", ""},
		{"malformed entry on the walk", "198.51.100.1, nope", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			const peer = "10.0.0.2:4000"
			s := run(t, mw, peer, "X-Forwarded-For", tt.value)
			want := tt.want
			if want == "" {
				want = peer
			}
			if s.remoteAddr != want {
				t.Errorf("RemoteAddr = %q, want %q", s.remoteAddr, want)
			}
		})
	}
}

func TestRealIP_Forwarded(t *testing.T) {
	mw := mustNew(t, realip.TrustRanges("10.0.0.0/8"), realip.WithHeader("forwarded"))
	tests := []struct {
		name  string
		value string
		want  string
	}{
		{"quoted ipv6 with port", `for="[2001:db8::1]:4711"`, "[2001:db8::1]:0"},
		{"plain ipv4", `for=198.51.100.1;proto=https`, "198.51.100.1:0"},
		{"case-insensitive parameter", `proto=https;FOR=198.51.100.1`, "198.51.100.1:0"},
		{"several elements", `for=6.6.6.6, for=198.51.100.1, for=10.1.1.1`, "198.51.100.1:0"},
		{"unknown", `for=unknown`, ""},
		{"obfuscated", `for=_hidden`, ""},
		{"element without for", `proto=https`, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			const peer = "10.0.0.2:4000"
			s := run(t, mw, peer, "Forwarded", tt.value)
			want := tt.want
			if want == "" {
				want = peer
			}
			if s.remoteAddr != want {
				t.Errorf("RemoteAddr = %q, want %q", s.remoteAddr, want)
			}
		})
	}
}

func TestRealIP_SingleAddressHeader(t *testing.T) {
	mw := mustNew(t, realip.TrustRanges("10.0.0.0/8"), realip.WithHeader("X-Real-IP"))
	s := run(t, mw, "10.0.0.2:4000", "X-Real-Ip", "198.51.100.1")
	if s.remoteAddr != "198.51.100.1:0" {
		t.Errorf("RemoteAddr = %q, want 198.51.100.1:0", s.remoteAddr)
	}
	// The default header is no longer consulted.
	s = run(t, mw, "10.0.0.2:4000", "X-Forwarded-For", "198.51.100.1")
	if s.remoteAddr != "10.0.0.2:4000" {
		t.Errorf("RemoteAddr = %q, want unchanged", s.remoteAddr)
	}
}

func TestRealIP_PeerFromContextWithoutMiddleware(t *testing.T) {
	if p, ok := realip.PeerFromContext(t.Context()); ok || p != "" {
		t.Errorf("PeerFromContext = %q, %v; want \"\", false", p, ok)
	}
}

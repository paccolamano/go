// Package realip resolves the real client address of requests that reach the
// service through proxies or load balancers.
//
// Forwarding headers can be forged by clients, so they are trusted only as
// far as the configuration allows:
//   - with [TrustRanges], the header is used only when the immediate peer is a
//     trusted proxy, and the client is the rightmost address in the list that
//     is not a trusted proxy (the leftmost one if every address is trusted);
//   - with [TrustHops], the client is the n-th address from the right.
//
// A malformed entry never becomes the client address: when one is found, the
// request is left unchanged. Ports, brackets and zones are removed, and
// IPv4-mapped IPv6 addresses are unmapped.
//
// The resolved address replaces r.RemoteAddr, as "ip:0", so that every
// downstream middleware and handler uses it without extra configuration. The
// original value is available through [PeerFromContext].
//
// Mandatory settings: the [Trust] built with TrustRanges or TrustHops.
//
// Options and defaults:
//   - [WithHeader]: X-Forwarded-For. "Forwarded" selects RFC 7239 parsing.
//
// Recommended position: first in the chain, so that every other middleware,
// including accesslog and ratelimit, sees the resolved client address.
package realip

import (
	"context"
	"net/http"
	"net/netip"

	"github.com/paccolamano/go/middleware"
	"github.com/paccolamano/go/middleware/internal/httpheader"
	"github.com/paccolamano/go/middleware/internal/netaddr"
)

type ctxKey struct{}

// New returns a middleware that resolves the client address according to
// trust.
func New(trust Trust, opts ...Option) (middleware.Middleware, error) {
	cfg := config{header: "X-Forwarded-For"}
	for _, opt := range opts {
		opt(&cfg)
	}
	res, err := newResolver(trust, cfg)
	if err != nil {
		return nil, err
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			client, ok := res.resolve(r)
			if !ok {
				next.ServeHTTP(w, r)
				return
			}
			r2 := r.WithContext(context.WithValue(r.Context(), ctxKey{}, r.RemoteAddr))
			r2.RemoteAddr = netip.AddrPortFrom(client, 0).String()
			next.ServeHTTP(w, r2)
		})
	}, nil
}

// PeerFromContext returns the original r.RemoteAddr when realip replaced it
// with the resolved client address. It reports false otherwise.
func PeerFromContext(ctx context.Context) (string, bool) {
	peer, ok := ctx.Value(ctxKey{}).(string)
	return peer, ok
}

func (res *resolver) resolve(r *http.Request) (netip.Addr, bool) {
	if res.hops == 0 {
		peer, ok := netaddr.ParseRemoteAddr(r.RemoteAddr)
		if !ok || !res.trusted(peer) {
			return netip.Addr{}, false
		}
	}
	values := r.Header.Values(res.header)
	if len(values) == 0 {
		return netip.Addr{}, false
	}
	var entries []string
	if res.forwarded {
		entries = forwardedFor(values)
	} else {
		entries = httpheader.SplitList(values)
	}
	if res.hops > 0 {
		return res.byHops(entries)
	}
	return res.byRanges(entries)
}

func (res *resolver) byRanges(entries []string) (netip.Addr, bool) {
	var leftmost netip.Addr
	for i := len(entries) - 1; i >= 0; i-- {
		a, ok := parseEntry(entries[i])
		if !ok {
			return netip.Addr{}, false
		}
		if !res.trusted(a) {
			return a, true
		}
		leftmost = a
	}
	return leftmost, leftmost.IsValid()
}

func (res *resolver) byHops(entries []string) (netip.Addr, bool) {
	idx := len(entries) - res.hops
	if idx < 0 {
		return netip.Addr{}, false
	}
	var a netip.Addr
	for i := len(entries) - 1; i >= idx; i-- {
		var ok bool
		if a, ok = parseEntry(entries[i]); !ok {
			return netip.Addr{}, false
		}
	}
	return a, true
}

func (res *resolver) trusted(a netip.Addr) bool {
	for _, p := range res.prefixes {
		if p.Contains(a) {
			return true
		}
	}
	return false
}

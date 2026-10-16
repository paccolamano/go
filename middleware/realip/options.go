package realip

import (
	"errors"
	"fmt"
	"net/http"
	"net/netip"
	"slices"

	"github.com/paccolamano/go/middleware"
	"github.com/paccolamano/go/middleware/internal/httpheader"
)

type trustMode int

const (
	modeUnset trustMode = iota
	modeRanges
	modeHops
)

// Trust declares which proxies in front of the service are trusted. Build it
// with TrustRanges or TrustHops; the zero value is invalid.
type Trust struct {
	mode  trustMode
	cidrs []string
	hops  int
}

// TrustRanges trusts the proxies whose addresses fall within the given CIDR
// ranges or single IP addresses. Forwarding headers are used only when the
// immediate peer is trusted, and the client is the rightmost untrusted
// address. The values are validated by New.
func TrustRanges(cidrsOrIPs ...string) Trust {
	return Trust{mode: modeRanges, cidrs: slices.Clone(cidrsOrIPs)}
}

// TrustHops declares that every request passes through exactly n proxies,
// each appending the address it received the request from. The client is the
// n-th address from the right. Use it when the proxy addresses are not known
// in advance, for example behind a cloud load balancer.
func TrustHops(n int) Trust {
	return Trust{mode: modeHops, hops: n}
}

// Option configures the middleware returned by New.
type Option func(*config)

type config struct {
	header string
}

// WithHeader sets the header that carries the forwarding information. The
// default is X-Forwarded-For. "Forwarded" selects RFC 7239 parsing; any other
// header is read as a comma-separated list of addresses, so a single-address
// header such as X-Real-IP is supported too.
func WithHeader(name string) Option {
	return func(c *config) { c.header = name }
}

var errNoRanges = errors.New("at least one range or address is required")

func (t Trust) prefixes() ([]netip.Prefix, error) {
	if len(t.cidrs) == 0 {
		return nil, errNoRanges
	}
	prefixes := make([]netip.Prefix, 0, len(t.cidrs))
	for _, s := range t.cidrs {
		if p, err := netip.ParsePrefix(s); err == nil {
			prefixes = append(prefixes, p.Masked())
			continue
		}
		a, err := netip.ParseAddr(s)
		if err != nil {
			return nil, fmt.Errorf("%q is neither a CIDR range nor an IP address", s)
		}
		a = a.WithZone("").Unmap()
		prefixes = append(prefixes, netip.PrefixFrom(a, a.BitLen()))
	}
	return prefixes, nil
}

type resolver struct {
	prefixes  []netip.Prefix
	hops      int
	header    string
	forwarded bool
}

func newResolver(trust Trust, cfg config) (*resolver, error) {
	res := &resolver{}
	switch trust.mode {
	case modeRanges:
		prefixes, err := trust.prefixes()
		if err != nil {
			return nil, fmt.Errorf("%w: realip: TrustRanges: %w", middleware.ErrInvalidConfig, err)
		}
		res.prefixes = prefixes
	case modeHops:
		if trust.hops < 1 {
			return nil, fmt.Errorf("%w: realip: TrustHops: n must be at least 1", middleware.ErrInvalidConfig)
		}
		res.hops = trust.hops
	default:
		return nil, fmt.Errorf("%w: realip: trust: must be built with TrustRanges or TrustHops", middleware.ErrInvalidConfig)
	}
	if !httpheader.ValidToken(cfg.header) {
		return nil, fmt.Errorf("%w: realip: WithHeader: %q is not a valid header name", middleware.ErrInvalidConfig, cfg.header)
	}
	res.header = http.CanonicalHeaderKey(cfg.header)
	res.forwarded = res.header == "Forwarded"
	return res, nil
}

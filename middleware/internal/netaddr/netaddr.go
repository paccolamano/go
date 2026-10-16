// Package netaddr parses client addresses from http.Request.RemoteAddr and
// derives rate-limiting keys from them.
package netaddr

import "net/netip"

// ParseRemoteAddr parses an "ip:port" or bare IP address, as found in
// http.Request.RemoteAddr. The zone is removed and IPv4-mapped IPv6 addresses
// are unmapped. It reports false when s holds no IP address.
func ParseRemoteAddr(s string) (netip.Addr, bool) {
	var addr netip.Addr
	if ap, err := netip.ParseAddrPort(s); err == nil {
		addr = ap.Addr()
	} else if a, err := netip.ParseAddr(s); err == nil {
		addr = a
	} else {
		return netip.Addr{}, false
	}
	return addr.WithZone("").Unmap(), true
}

// Key returns the rate-limiting key of a: the address itself for IPv4, and
// its /64 prefix for IPv6, since a single subscriber usually controls a whole
// /64.
func Key(a netip.Addr) string {
	if a.Is6() {
		return netip.PrefixFrom(a, 64).Masked().String()
	}
	return a.String()
}

package realip

import (
	"net/netip"
	"strings"

	"github.com/paccolamano/go/middleware/internal/netaddr"
)

// forwardedFor extracts the for= value of every element of RFC 7239 Forwarded
// header values, in order. An element without a for= parameter yields an
// empty entry, which is malformed.
func forwardedFor(values []string) []string {
	var entries []string
	for _, v := range values {
		for _, elem := range splitQuoted(v, ',') {
			if elem = strings.Trim(elem, " \t"); elem == "" {
				continue
			}
			entry := ""
			for _, pair := range splitQuoted(elem, ';') {
				name, value, ok := strings.Cut(strings.Trim(pair, " \t"), "=")
				if ok && strings.EqualFold(strings.Trim(name, " \t"), "for") {
					entry = unquote(strings.Trim(value, " \t"))
					break
				}
			}
			entries = append(entries, entry)
		}
	}
	return entries
}

// splitQuoted splits s on sep, ignoring separators inside quoted strings.
func splitQuoted(s string, sep byte) []string {
	var parts []string
	inQuote, escaped := false, false
	start := 0
	for i := range len(s) {
		c := s[i]
		switch {
		case escaped:
			escaped = false
		case inQuote && c == '\\':
			escaped = true
		case c == '"':
			inQuote = !inQuote
		case !inQuote && c == sep:
			parts = append(parts, s[start:i])
			start = i + 1
		}
	}
	return append(parts, s[start:])
}

// unquote removes the quotes and backslash escapes of a quoted string; other
// values are returned unchanged.
func unquote(v string) string {
	if len(v) < 2 || v[0] != '"' || v[len(v)-1] != '"' {
		return v
	}
	inner := v[1 : len(v)-1]
	var b strings.Builder
	for i := 0; i < len(inner); i++ {
		if inner[i] == '\\' && i+1 < len(inner) {
			i++
		}
		b.WriteByte(inner[i])
	}
	return b.String()
}

// parseEntry parses one forwarding entry: an IP address, optionally with a
// port, optionally in brackets. Identifiers such as "unknown" or "_hidden"
// are malformed.
func parseEntry(s string) (netip.Addr, bool) {
	if a, ok := netaddr.ParseRemoteAddr(s); ok {
		return a, true
	}
	if inner, ok := strings.CutPrefix(s, "["); ok {
		if inner, ok = strings.CutSuffix(inner, "]"); ok {
			return netaddr.ParseRemoteAddr(inner)
		}
	}
	return netip.Addr{}, false
}

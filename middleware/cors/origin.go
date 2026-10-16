package cors

import (
	"fmt"
	"net/url"
	"strings"
)

// originPattern matches the origins scheme://<one or more labels>.suffix:port.
type originPattern struct {
	scheme string
	suffix string // ".example.com"
	port   string
}

type originMatcher struct {
	any      bool
	exact    map[string]struct{}
	patterns []originPattern
	fn       func(origin string) bool
}

func parseOrigins(origins []string) (originMatcher, error) {
	m := originMatcher{exact: map[string]struct{}{}}
	for _, o := range origins {
		switch {
		case o == "*":
			if len(origins) != 1 {
				return originMatcher{}, fmt.Errorf(`"*" must be the only origin`)
			}
			m.any = true
		case o == "null":
			m.exact["null"] = struct{}{}
		case strings.Contains(o, "*"):
			p, err := parsePattern(o)
			if err != nil {
				return originMatcher{}, err
			}
			m.patterns = append(m.patterns, p)
		default:
			normalized, err := normalizeOrigin(o)
			if err != nil {
				return originMatcher{}, err
			}
			m.exact[normalized] = struct{}{}
		}
	}
	return m, nil
}

// normalizeOrigin validates scheme://host[:port] and returns it in lowercase.
func normalizeOrigin(o string) (string, error) {
	lower := strings.ToLower(o)
	u, err := url.Parse(lower)
	if err != nil || u.Scheme == "" || u.Host == "" || u.Opaque != "" || u.User != nil ||
		u.Path != "" || u.RawQuery != "" || u.Fragment != "" || u.ForceQuery ||
		strings.HasSuffix(lower, "#") || strings.HasSuffix(u.Host, ":") || strings.ContainsAny(u.Host, "*/") {
		return "", fmt.Errorf("%q is not an origin (scheme://host[:port])", o)
	}
	return lower, nil
}

// parsePattern validates scheme://*.domain[:port].
func parsePattern(o string) (originPattern, error) {
	lower := strings.ToLower(o)
	scheme, rest, ok := strings.Cut(lower, "://*.")
	if !ok || scheme == "" || strings.Contains(rest, "*") {
		return originPattern{}, fmt.Errorf("%q is not an origin pattern (scheme://*.domain[:port])", o)
	}
	normalized, err := normalizeOrigin(scheme + "://" + rest)
	if err != nil {
		return originPattern{}, fmt.Errorf("%q is not an origin pattern (scheme://*.domain[:port])", o)
	}
	u, _ := url.Parse(normalized) // already validated
	return originPattern{scheme: u.Scheme, suffix: "." + u.Hostname(), port: u.Port()}, nil
}

// allowed reports whether origin, the value of the Origin header, is allowed.
// The "null" origin is allowed only when listed explicitly.
func (m originMatcher) allowed(origin string) bool {
	if m.any {
		return true
	}
	lower := strings.ToLower(origin)
	if _, ok := m.exact[lower]; ok {
		return true
	}
	if lower == "null" {
		return false
	}
	if len(m.patterns) > 0 {
		if u, err := url.Parse(lower); err == nil && u.Path == "" && u.RawQuery == "" && u.Fragment == "" &&
			u.User == nil && u.Opaque == "" && !u.ForceQuery && !strings.ContainsAny(lower, "#?") &&
			!strings.HasSuffix(u.Host, ":") {
			host := u.Hostname()
			for _, p := range m.patterns {
				if u.Scheme == p.scheme && u.Port() == p.port && len(host) > len(p.suffix) && strings.HasSuffix(host, p.suffix) {
					return true
				}
			}
		}
	}
	return m.fn != nil && m.fn(origin)
}

// Package mediatype matches Content-Type values against media-type patterns:
// "type/subtype", "type/*", "type/*+suffix" and "*/*".
package mediatype

import (
	"errors"
	"fmt"
	"mime"
	"strings"

	"github.com/paccolamano/go/middleware/internal/httpheader"
)

var errNoPatterns = errors.New("at least one media type is required")

type pattern struct {
	typ    string // "*" only for "*/*"
	sub    string // "*" for wildcards
	suffix string // structured syntax suffix of "type/*+suffix", without "+"
}

// Matcher matches media types against a set of patterns.
type Matcher struct {
	patterns []pattern
}

// ParsePatterns validates patterns and returns a Matcher. Every pattern must
// be lowercase and of the form "type/subtype", "type/*", "type/*+suffix" or
// "*/*"; the list must not be empty.
func ParsePatterns(patterns []string) (Matcher, error) {
	if len(patterns) == 0 {
		return Matcher{}, errNoPatterns
	}
	m := Matcher{patterns: make([]pattern, 0, len(patterns))}
	for _, s := range patterns {
		p, err := parsePattern(s)
		if err != nil {
			return Matcher{}, err
		}
		m.patterns = append(m.patterns, p)
	}
	return m, nil
}

func parsePattern(s string) (pattern, error) {
	typ, sub, ok := strings.Cut(s, "/")
	if !ok || s != strings.ToLower(s) {
		return pattern{}, fmt.Errorf("invalid media type pattern %q", s)
	}
	if typ == "*" {
		if sub != "*" {
			return pattern{}, fmt.Errorf("invalid media type pattern %q: only */* may use a wildcard type", s)
		}
		return pattern{typ: "*", sub: "*"}, nil
	}
	if !httpheader.ValidToken(typ) {
		return pattern{}, fmt.Errorf("invalid media type pattern %q", s)
	}
	if sub == "*" {
		return pattern{typ: typ, sub: "*"}, nil
	}
	if suffix, ok := strings.CutPrefix(sub, "*+"); ok {
		if !httpheader.ValidToken(suffix) || strings.Contains(suffix, "*") {
			return pattern{}, fmt.Errorf("invalid media type pattern %q", s)
		}
		return pattern{typ: typ, sub: "*", suffix: suffix}, nil
	}
	if !httpheader.ValidToken(sub) || strings.Contains(sub, "*") {
		return pattern{}, fmt.Errorf("invalid media type pattern %q", s)
	}
	return pattern{typ: typ, sub: sub}, nil
}

// Match reports whether the media type of contentType, a Content-Type header
// value, matches one of the patterns. Parameters are ignored and the
// comparison is case-insensitive. Empty or unparseable values never match.
func (m Matcher) Match(contentType string) bool {
	mt, _, err := mime.ParseMediaType(contentType)
	if err != nil && !errors.Is(err, mime.ErrInvalidMediaParameter) {
		return false
	}
	typ, sub, ok := strings.Cut(mt, "/")
	if !ok || typ == "" || sub == "" {
		return false
	}
	for _, p := range m.patterns {
		switch {
		case p.typ == "*":
			return true
		case p.typ != typ:
			continue
		case p.suffix != "":
			if strings.HasSuffix(sub, "+"+p.suffix) {
				return true
			}
		case p.sub == "*" || p.sub == sub:
			return true
		}
	}
	return false
}

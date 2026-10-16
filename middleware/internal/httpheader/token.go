// Package httpheader implements the HTTP header field syntax shared by the
// middleware of this module: tokens, comma-separated lists, Vary handling,
// content-coding negotiation and entity-tag lists.
package httpheader

import (
	"net/http"
	"strings"
)

// ValidToken reports whether s is a non-empty RFC 9110 token.
func ValidToken(s string) bool {
	if s == "" {
		return false
	}
	for i := range len(s) {
		if !isTokenChar(s[i]) {
			return false
		}
	}
	return true
}

func isTokenChar(c byte) bool {
	switch {
	case 'a' <= c && c <= 'z', 'A' <= c && c <= 'Z', '0' <= c && c <= '9':
		return true
	}
	return strings.IndexByte("!#$%&'*+-.^_`|~", c) >= 0
}

// ValidValue reports whether s can be used as a header field value: it must
// not contain CR, LF or NUL.
func ValidValue(s string) bool {
	return !strings.ContainsAny(s, "\r\n\x00")
}

// SplitList splits comma-separated header field values into their items, in
// order. Optional whitespace around items is removed and empty items are
// dropped. It does not handle quoted strings.
func SplitList(values []string) []string {
	var items []string
	for _, v := range values {
		for item := range strings.SplitSeq(v, ",") {
			if item = strings.Trim(item, " \t"); item != "" {
				items = append(items, item)
			}
		}
	}
	return items
}

// HasToken reports whether the comma-separated header field name contains
// token, compared case-insensitively.
func HasToken(h http.Header, name, token string) bool {
	if token == "" {
		return false
	}
	for _, item := range SplitList(h.Values(name)) {
		if strings.EqualFold(item, token) {
			return true
		}
	}
	return false
}

// AddVary adds token to the Vary header unless it, or "*", is already present.
func AddVary(h http.Header, token string) {
	if HasToken(h, "Vary", token) || HasToken(h, "Vary", "*") {
		return
	}
	h.Add("Vary", token)
}

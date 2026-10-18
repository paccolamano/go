package problem

import (
	"net/url"
	"strings"
)

// uriPunctuation lists the RFC 3986 characters, other than letters and digits,
// that may appear in a URI reference: the unreserved marks and the reserved
// delimiters. Percent signs are checked separately.
const uriPunctuation = "-._~:/?#[]@!$&'()*+,;="

// validURIReference reports whether s is a URI reference: not empty, made only
// of RFC 3986 characters and percent-encodings, and accepted by url.Parse.
// url.Parse alone accepts spaces and non-ASCII characters, which are not URIs.
func validURIReference(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case isLetter(c), isDigit(c), strings.IndexByte(uriPunctuation, c) >= 0:
		case c == '%':
			if i+2 >= len(s) || !isHex(s[i+1]) || !isHex(s[i+2]) {
				return false
			}
		default:
			return false
		}
	}
	_, err := url.Parse(s)
	return err == nil
}

// isTypeURI reports whether s can identify a problem type: a URI reference that
// is absolute or begins with "/" (RFC 9457 §3.1.1).
func isTypeURI(s string) bool {
	if !validURIReference(s) {
		return false
	}
	if strings.HasPrefix(s, "/") {
		return true
	}
	u, err := url.Parse(s)
	return err == nil && u.IsAbs()
}

// validExtensionName reports whether s follows RFC 9457 §3.2: a letter first,
// then letters, digits or '_', for three characters or more. The standard
// member names are refused in any capitalization, because some clients match
// member names without regard to case.
func validExtensionName(s string) bool {
	if len(s) < 3 || !isLetter(s[0]) {
		return false
	}
	for i := 1; i < len(s); i++ {
		if c := s[i]; !isLetter(c) && !isDigit(c) && c != '_' {
			return false
		}
	}
	switch strings.ToLower(s) {
	case "type", "title", "status", "detail", "instance":
		return false
	}
	return true
}

// resolve resolves ref against base (RFC 3986 §5). It returns ref unchanged
// when it is empty, absolute or not a URI reference.
func resolve(base *url.URL, ref string) string {
	if ref == "" {
		return ref
	}
	u, err := url.Parse(ref)
	if err != nil || u.IsAbs() {
		return ref
	}
	return base.ResolveReference(u).String()
}

func isLetter(c byte) bool { return 'a' <= c && c <= 'z' || 'A' <= c && c <= 'Z' }

func isDigit(c byte) bool { return '0' <= c && c <= '9' }

func isHex(c byte) bool { return isDigit(c) || 'a' <= c && c <= 'f' || 'A' <= c && c <= 'F' }

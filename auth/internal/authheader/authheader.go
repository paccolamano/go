// Package authheader parses Authorization credentials and formats challenge
// parameters (RFC 9110 §11).
package authheader

import (
	"net/http"
	"strings"
)

// Credentials returns the token that follows scheme in the request's
// Authorization header. present is true only when the header's scheme equals
// scheme, compared without regard to case; token may then be empty.
func Credentials(r *http.Request, scheme string) (token string, present bool) {
	h := r.Header.Get("Authorization")
	s, rest, _ := strings.Cut(h, " ")
	if h == "" || !strings.EqualFold(s, scheme) {
		return "", false
	}
	return strings.TrimLeft(rest, " "), true
}

// Quote returns v as a quoted string, escaping '"' and '\'.
func Quote(v string) string {
	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`)
	return `"` + r.Replace(v) + `"`
}

// Scheme returns the authentication scheme of a challenge, lower-cased.
func Scheme(challenge string) string {
	s, _, _ := strings.Cut(challenge, " ")
	return strings.ToLower(s)
}

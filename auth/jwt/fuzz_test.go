package jwt_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	jose "github.com/go-jose/go-jose/v4"
)

func FuzzToken(f *testing.F) {
	k := testKeys()
	v := staticVerifier(f)
	valid := map[string]bool{}
	for _, tok := range []string{
		sign(f, jose.ES256, k.ec, "es", nil, validClaims()),
		sign(f, jose.RS256, k.rsa, "rs", nil, validClaims()),
	} {
		valid[tok] = true
		f.Add("Bearer " + tok)
	}
	f.Add("Bearer a.b.c")
	f.Add("Bearer " + unsigned(`{"alg":"none"}`, `{}`))
	f.Add("Basic x")
	f.Fuzz(func(t *testing.T, header string) {
		r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil)
		r.Header.Set("Authorization", header)
		if _, err := v.Authenticate(r); err == nil {
			_, tok, _ := strings.Cut(header, " ")
			if !valid[strings.TrimLeft(tok, " ")] {
				t.Fatalf("%q authenticated", header)
			}
		}
	})
}

package jwt_test

import (
	"testing"

	jose "github.com/go-jose/go-jose/v4"
)

func BenchmarkJWTAuthenticate(b *testing.B) {
	k := testKeys()
	v := staticVerifier(b)
	for _, c := range []struct {
		name string
		alg  jose.SignatureAlgorithm
		key  any
		kid  string
	}{{"RS256", jose.RS256, k.rsa, "rs"}, {"ES256", jose.ES256, k.ec, "es"}, {"EdDSA", jose.EdDSA, k.ed, "ed"}} {
		r := bearer(b, sign(b, c.alg, c.key, c.kid, nil, validClaims()))
		b.Run(c.name, func(b *testing.B) {
			for b.Loop() {
				if _, err := v.Authenticate(r); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

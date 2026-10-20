package jwt

import (
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rsa"
	"fmt"

	jose "github.com/go-jose/go-jose/v4"

	"github.com/paccolamano/go/auth"
)

const (
	minRSABits = 2048
	minHMACKey = 32
)

// boundKey is a verification key bound to exactly one algorithm (RFC 8725).
type boundKey struct {
	kid string
	alg string
	key any
}

// algorithms returns the accepted signature algorithms, always passed to the
// token parser.
func algorithms() []jose.SignatureAlgorithm {
	return []jose.SignatureAlgorithm{jose.RS256, jose.PS256, jose.ES256, jose.EdDSA, jose.HS256}
}

// staticKey validates a key passed to WithKey.
func staticKey(kid, alg string, key any) (boundKey, error) {
	ok := false
	switch alg {
	case "RS256", "PS256":
		k, isRSA := key.(*rsa.PublicKey)
		ok = isRSA && k != nil && k.N != nil && k.N.BitLen() >= minRSABits
	case "ES256":
		k, isEC := key.(*ecdsa.PublicKey)
		ok = isEC && k != nil && k.Curve == elliptic.P256()
	case "EdDSA":
		k, isEd := key.(ed25519.PublicKey)
		ok = isEd && len(k) == ed25519.PublicKeySize
	case "HS256":
		k, isBytes := key.([]byte)
		ok = isBytes && len(k) >= minHMACKey
	default:
		return boundKey{}, fmt.Errorf("%w: jwt.New: WithKey: algorithm must be RS256, PS256, ES256, EdDSA or HS256", auth.ErrInvalidConfig)
	}
	if !ok {
		return boundKey{}, fmt.Errorf("%w: jwt.New: WithKey: the key does not fit %s (RSA ≥ 2048 bits, ECDSA P-256, Ed25519, or a shared HS256 key of ≥ 32 bytes)", auth.ErrInvalidConfig, alg)
	}
	return boundKey{kid: kid, alg: alg, key: key}, nil
}

// candidates returns the keys bound to alg, restricted to kid when the token
// names one.
func candidates(keys []boundKey, kid, alg string) []boundKey {
	var out []boundKey
	for _, k := range keys {
		if k.alg == alg && (kid == "" || k.kid == kid) {
			out = append(out, k)
		}
	}
	return out
}

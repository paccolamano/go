package jwt

import (
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rsa"
	"encoding/json"
	"errors"
	"fmt"

	jose "github.com/go-jose/go-jose/v4"
)

var errNoUsableKey = errors.New("jwt: the key set has no usable key")

// parseKeySet parses a JWK Set key by key, so that one unsupported key does
// not make the whole set unusable. It skips keys that fail to parse, are not
// public (symmetric or private keys), are not for signatures, are too weak or
// on another curve, or declare an algorithm their type does not allow.
func parseKeySet(doc []byte) ([]boundKey, error) {
	var raw struct {
		Keys []json.RawMessage `json:"keys"`
	}
	if err := json.Unmarshal(doc, &raw); err != nil {
		return nil, fmt.Errorf("jwt: decode key set: %w", err)
	}
	var out []boundKey
	for _, r := range raw.Keys {
		var k jose.JSONWebKey
		if err := k.UnmarshalJSON(r); err != nil {
			continue
		}
		if !k.Valid() || !k.IsPublic() || (k.Use != "" && k.Use != "sig") {
			continue
		}
		if alg, ok := boundAlgorithm(k); ok {
			out = append(out, boundKey{kid: k.KeyID, alg: alg, key: k.Key})
		}
	}
	if len(out) == 0 {
		return nil, errNoUsableKey
	}
	return out, nil
}

// boundAlgorithm returns the one algorithm k may verify: the one it declares,
// or the only one its type allows, with RS256 for RSA keys that declare none.
func boundAlgorithm(k jose.JSONWebKey) (string, bool) {
	declared := k.Algorithm
	switch key := k.Key.(type) {
	case *rsa.PublicKey:
		if key.N.BitLen() < minRSABits {
			return "", false
		}
		switch declared {
		case "":
			return "RS256", true
		case "RS256", "PS256":
			return declared, true
		}
	case *ecdsa.PublicKey:
		if key.Curve == elliptic.P256() && (declared == "" || declared == "ES256") {
			return "ES256", true
		}
	case ed25519.PublicKey:
		if declared == "" || declared == "EdDSA" {
			return "EdDSA", true
		}
	}
	return "", false
}

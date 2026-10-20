package jwt

import (
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"math/big"
	"testing"

	jose "github.com/go-jose/go-jose/v4"
)

func doc(tb testing.TB, keys ...any) []byte {
	tb.Helper()
	b, err := json.Marshal(map[string]any{"keys": keys})
	if err != nil {
		tb.Fatal(err)
	}
	return b
}

func TestParseKeySet_Skips(t *testing.T) {
	r2048, _ := rsa.GenerateKey(rand.Reader, 2048)
	r1024 := &rsa.PrivateKey{PublicKey: rsa.PublicKey{N: new(big.Int).SetBit(new(big.Int), 1023, 1), E: 65537}} // 1,024-bit modulus, not a usable key
	p256, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	p384, _ := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	good := jose.JSONWebKey{Key: &p256.PublicKey, KeyID: "good", Algorithm: "ES256", Use: "sig"}
	skipped := []any{
		map[string]any{"kty": "OKP", "crv": "X25519", "x": "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"},
		map[string]any{"kty": "unknown"},
		map[string]any{"kty": "oct", "k": "c2VjcmV0c2VjcmV0c2VjcmV0c2VjcmV0c2VjcmV0"},
		jose.JSONWebKey{Key: r2048, KeyID: "private"},
		jose.JSONWebKey{Key: &r2048.PublicKey, KeyID: "enc", Use: "enc"},
		jose.JSONWebKey{Key: &r1024.PublicKey, KeyID: "small"},
		jose.JSONWebKey{Key: &p384.PublicKey, KeyID: "p384"},
		jose.JSONWebKey{Key: &p256.PublicKey, KeyID: "wrong-alg", Algorithm: "RS256"},
		jose.JSONWebKey{Key: &p256.PublicKey, KeyID: "hs", Algorithm: "HS256"},
	}
	keys, err := parseKeySet(doc(t, append(skipped, good)...))
	if err != nil || len(keys) != 1 || keys[0].kid != "good" {
		t.Fatalf("parseKeySet = %+v, %v; want only the good key", keys, err)
	}
}

func TestParseKeySet_ImpliedAlg(t *testing.T) {
	r, _ := rsa.GenerateKey(rand.Reader, 2048)
	e, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	_, d, _ := ed25519.GenerateKey(rand.Reader)
	keys, err := parseKeySet(doc(t,
		jose.JSONWebKey{Key: &r.PublicKey, KeyID: "r"},
		jose.JSONWebKey{Key: &r.PublicKey, KeyID: "p", Algorithm: "PS256"},
		jose.JSONWebKey{Key: &e.PublicKey, KeyID: "e"},
		jose.JSONWebKey{Key: d.Public(), KeyID: "d"},
	))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"r": "RS256", "p": "PS256", "e": "ES256", "d": "EdDSA"}
	for _, k := range keys {
		if want[k.kid] != k.alg {
			t.Errorf("key %s bound to %s, want %s", k.kid, k.alg, want[k.kid])
		}
	}
	if len(keys) != 4 {
		t.Fatalf("%d keys, want 4", len(keys))
	}
}

func TestParseKeySet_NoUsable(t *testing.T) {
	for _, b := range []string{`{"keys":[]}`, `{}`, `not json`, `{"keys":[{"kty":"oct","k":"AA"}]}`} {
		if _, err := parseKeySet([]byte(b)); err == nil {
			t.Errorf("parseKeySet(%s) succeeded", b)
		}
	}
}

func FuzzKeySet(f *testing.F) {
	e, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	f.Add(doc(f, jose.JSONWebKey{Key: &e.PublicKey, KeyID: "k"}))
	f.Add([]byte(`{"keys":[{"kty":"RSA","n":"AQAB","e":"AQAB"}]}`))
	f.Add([]byte(`{"keys":null}`))
	f.Fuzz(func(t *testing.T, b []byte) {
		keys, err := parseKeySet(b)
		if err != nil {
			return
		}
		for _, k := range keys {
			switch k.alg {
			case "RS256", "PS256":
				if _, ok := k.key.(*rsa.PublicKey); !ok {
					t.Fatalf("%s bound to %T", k.alg, k.key)
				}
			case "ES256":
				if _, ok := k.key.(*ecdsa.PublicKey); !ok {
					t.Fatalf("%s bound to %T", k.alg, k.key)
				}
			case "EdDSA":
				if _, ok := k.key.(ed25519.PublicKey); !ok {
					t.Fatalf("%s bound to %T", k.alg, k.key)
				}
			default:
				t.Fatalf("key bound to %q", k.alg)
			}
		}
	})
}

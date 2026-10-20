package jwt_test

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	jose "github.com/go-jose/go-jose/v4"
	josejwt "github.com/go-jose/go-jose/v4/jwt"

	"github.com/paccolamano/go/auth/jwt"
)

const (
	issuer   = "https://id.example.test"
	audience = "orders-api"
	jwksURL  = "https://id.example.test/jwks"
)

type keyPairs struct {
	rsa     *rsa.PrivateKey
	ec      *ecdsa.PrivateKey
	ed      ed25519.PrivateKey
	hmac    []byte
	another *ecdsa.PrivateKey
}

// testKeys generates the test keys once: RSA 2048 is slow to generate.
var testKeys = sync.OnceValue(func() keyPairs {
	r, _ := rsa.GenerateKey(rand.Reader, 2048)
	e, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	_, d, _ := ed25519.GenerateKey(rand.Reader)
	a, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	h := make([]byte, 32)
	_, _ = rand.Read(h)
	return keyPairs{rsa: r, ec: e, ed: d, hmac: h, another: a}
})

// sign returns a compact token signed with key.
func sign(tb testing.TB, alg jose.SignatureAlgorithm, key any, kid string, headers map[jose.HeaderKey]any, claims map[string]any) string {
	tb.Helper()
	opts := &jose.SignerOptions{ExtraHeaders: map[jose.HeaderKey]any{}}
	for k, v := range headers {
		opts.ExtraHeaders[k] = v
	}
	signingKey := key
	if kid != "" {
		signingKey = jose.JSONWebKey{Key: key, KeyID: kid}
	}
	s, err := jose.NewSigner(jose.SigningKey{Algorithm: alg, Key: signingKey}, opts)
	if err != nil {
		tb.Fatalf("NewSigner: %v", err)
	}
	tok, err := josejwt.Signed(s).Claims(claims).Serialize()
	if err != nil {
		tb.Fatalf("Serialize: %v", err)
	}
	return tok
}

// validClaims returns claims that pass every check, expiring in an hour.
func validClaims() map[string]any {
	return map[string]any{
		"iss": issuer, "aud": audience, "sub": "u1",
		"exp": time.Now().Add(time.Hour).Unix(), "amr": []string{"pwd"},
	}
}

func with(claims map[string]any, kv ...any) map[string]any {
	for i := 0; i < len(kv); i += 2 {
		if kv[i+1] == nil {
			delete(claims, kv[i].(string))
			continue
		}
		claims[kv[i].(string)] = kv[i+1]
	}
	return claims
}

func unsigned(header, payload string) string {
	enc := base64.RawURLEncoding.EncodeToString
	return enc([]byte(header)) + "." + enc([]byte(payload)) + "."
}

// fakeIdP serves a key set from memory.
type fakeIdP struct {
	mu           sync.Mutex
	status       int
	body         []byte
	cacheControl string
	err          error
	block        chan struct{}
	urls         []string
	accept       string
	requests     atomic.Int32
}

func newIdP(tb testing.TB, keys ...jose.JSONWebKey) *fakeIdP {
	tb.Helper()
	f := &fakeIdP{status: http.StatusOK}
	f.setKeys(tb, keys...)
	return f
}

func (f *fakeIdP) setKeys(tb testing.TB, keys ...jose.JSONWebKey) {
	tb.Helper()
	b, err := json.Marshal(jose.JSONWebKeySet{Keys: keys})
	if err != nil {
		tb.Fatal(err)
	}
	f.mu.Lock()
	f.body = b
	f.mu.Unlock()
}

func (f *fakeIdP) set(fn func(f *fakeIdP)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	fn(f)
}

func (f *fakeIdP) RoundTrip(r *http.Request) (*http.Response, error) {
	f.requests.Add(1)
	f.mu.Lock()
	f.urls = append(f.urls, r.URL.String())
	f.accept = r.Header.Get("Accept")
	block, status, body, cc, err := f.block, f.status, f.body, f.cacheControl, f.err
	f.mu.Unlock()
	if block != nil {
		select {
		case <-block:
		case <-r.Context().Done():
			return nil, r.Context().Err()
		}
	}
	if err != nil {
		return nil, err
	}
	h := http.Header{}
	if cc != "" {
		h.Set("Cache-Control", cc)
	}
	return &http.Response{StatusCode: status, Header: h, Body: io.NopCloser(bytes.NewReader(body)), Request: r}, nil
}

func jwk(key any, kid, alg string) jose.JSONWebKey {
	return jose.JSONWebKey{Key: key, KeyID: kid, Algorithm: alg, Use: "sig"}
}

func newVerifier(tb testing.TB, idp *fakeIdP, opts ...jwt.Option) *jwt.Verifier {
	tb.Helper()
	base := []jwt.Option{jwt.WithIssuer(issuer), jwt.WithAudience(audience)}
	if idp != nil {
		base = append(base, jwt.WithJWKS(jwksURL), jwt.WithHTTPClient(&http.Client{Transport: idp}))
	}
	v, err := jwt.New(append(base, opts...)...)
	if err != nil {
		tb.Fatalf("jwt.New: %v", err)
	}
	return v
}

func bearer(tb testing.TB, token string) *http.Request {
	tb.Helper()
	r := httptest.NewRequestWithContext(tb.Context(), http.MethodGet, "/", nil)
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	return r
}

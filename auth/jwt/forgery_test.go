package jwt_test

import (
	"crypto/x509"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	jose "github.com/go-jose/go-jose/v4"
	josejwt "github.com/go-jose/go-jose/v4/jwt"

	"github.com/paccolamano/go/auth"
	"github.com/paccolamano/go/auth/jwt"
)

func TestForgeryCorpus(t *testing.T) {
	k := testKeys()
	v := newVerifier(t, nil,
		jwt.WithKey("rs", "RS256", &k.rsa.PublicKey),
		jwt.WithKey("es", "ES256", &k.ec.PublicKey),
		jwt.WithType("at+jwt"))
	typ := map[jose.HeaderKey]any{jose.HeaderType: "at+jwt"}
	der, _ := x509.MarshalPKIXPublicKey(&k.rsa.PublicKey)
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der})
	withTyp := func(extra map[jose.HeaderKey]any) map[jose.HeaderKey]any {
		h := map[jose.HeaderKey]any{jose.HeaderType: "at+jwt"}
		for key, val := range extra {
			h[key] = val
		}
		return h
	}
	embedded := func() string {
		s, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.ES256, Key: k.another},
			(&jose.SignerOptions{EmbedJWK: true}).WithType("at+jwt"))
		if err != nil {
			t.Fatal(err)
		}
		tok, err := josejwt.Signed(s).Claims(validClaims()).Serialize()
		if err != nil {
			t.Fatal(err)
		}
		return tok
	}

	forged := map[string]string{
		"alg none":               unsigned(`{"alg":"none","typ":"at+jwt"}`, `{"iss":"`+issuer+`","aud":"`+audience+`","sub":"u1","exp":9999999999}`),
		"HS256 with RSA DER":     sign(t, jose.HS256, der, "rs", typ, validClaims()),
		"HS256 with RSA PEM":     sign(t, jose.HS256, pemBytes, "rs", typ, validClaims()),
		"PS256 for an RS256 key": sign(t, jose.PS256, k.rsa, "rs", typ, validClaims()),
		"missing exp":            sign(t, jose.ES256, k.ec, "es", typ, with(validClaims(), "exp", nil)),
		"expired":                sign(t, jose.ES256, k.ec, "es", typ, with(validClaims(), "exp", time.Now().Add(-time.Hour).Unix())),
		"wrong issuer":           sign(t, jose.ES256, k.ec, "es", typ, with(validClaims(), "iss", "https://evil.test")),
		"audience missing":       sign(t, jose.ES256, k.ec, "es", typ, with(validClaims(), "aud", nil)),
		"wrong type":             sign(t, jose.ES256, k.ec, "es", map[jose.HeaderKey]any{jose.HeaderType: "JWT"}, validClaims()),
		"crit b64":               sign(t, jose.ES256, k.ec, "es", withTyp(map[jose.HeaderKey]any{"crit": []string{"b64"}, "b64": true}), validClaims()),
		"crit exp":               sign(t, jose.ES256, k.ec, "es", withTyp(map[jose.HeaderKey]any{"crit": []string{"exp"}, "exp": 1}), validClaims()),
		"embedded jwk":           embedded(),
		"encrypted form":         "a.b.c.d.e",
	}
	mw, _ := auth.New(v)
	h := mw(http.NotFoundHandler())
	for name, tok := range forged {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, bearer(t, tok))
		if rec.Code != 401 {
			t.Errorf("%s: status %d, want 401", name, rec.Code)
		}
	}
	for name, tok := range map[string]string{
		"RS256": sign(t, jose.RS256, k.rsa, "rs", typ, validClaims()),
		"ES256": sign(t, jose.ES256, k.ec, "es", typ, validClaims()),
	} {
		if _, err := v.Authenticate(bearer(t, tok)); err != nil {
			t.Errorf("control %s: %v", name, err)
		}
	}
}

func TestForgeryCorpus_ReferencedKey(t *testing.T) {
	k := testKeys()
	idp := newIdP(t, jwk(&k.ec.PublicKey, "es", "ES256"))
	v := newVerifier(t, idp)
	tok := sign(t, jose.ES256, k.another, "es", map[jose.HeaderKey]any{"jku": "https://attacker.test/jwks", "x5u": "https://attacker.test/cert"}, validClaims())
	if _, err := v.Authenticate(bearer(t, tok)); err == nil {
		t.Fatal("a token signed with a referenced key was accepted")
	}
	for _, u := range idp.urls {
		if strings.Contains(u, "attacker") {
			t.Fatalf("the verifier fetched %s", u)
		}
	}
}

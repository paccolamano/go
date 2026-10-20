package jwt_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"fmt"
	"net/http"
	"net/http/httptest"
	"time"

	jose "github.com/go-jose/go-jose/v4"
	josejwt "github.com/go-jose/go-jose/v4/jwt"

	"github.com/paccolamano/go/auth"
	"github.com/paccolamano/go/auth/jwt"
)

// issue plays the identity provider: it signs an access token.
func issue(key *ecdsa.PrivateKey, claims map[string]any) string {
	s, _ := jose.NewSigner(jose.SigningKey{Algorithm: jose.ES256, Key: jose.JSONWebKey{Key: key, KeyID: "k1"}},
		(&jose.SignerOptions{}).WithType("at+jwt"))
	tok, _ := josejwt.Signed(s).Claims(claims).Serialize()
	return tok
}

func ExampleNew() {
	idpKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)

	// In production, use jwt.WithJWKS("https://id.example.com/.well-known/jwks.json").
	tokens, err := jwt.New(
		jwt.WithIssuer("https://id.example.com"),
		jwt.WithAudience("orders-api"),
		jwt.WithKey("k1", "ES256", &idpKey.PublicKey),
		jwt.WithType("at+jwt"),
	)
	if err != nil {
		panic(err)
	}
	mw, _ := auth.New(tokens)
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p, _ := auth.FromContext(r.Context())
		_, _ = fmt.Fprint(w, p.Subject, " ", p.Methods)
	}))

	tok := issue(idpKey, map[string]any{
		"iss": "https://id.example.com", "aud": "orders-api", "sub": "user-42",
		"exp": time.Now().Add(time.Hour).Unix(), "amr": []string{"pwd", "otp", "mfa"},
	})
	r := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/orders", nil)
	r.Header.Set("Authorization", "Bearer "+tok)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	fmt.Println(rec.Code, rec.Body.String())

	r.Header.Set("Authorization", "Bearer "+tok[:len(tok)-4]+"AAAA")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	fmt.Println(rec.Code, rec.Header().Get("WWW-Authenticate"))
	// Output:
	// 200 user-42 [pwd otp mfa]
	// 401 Bearer error="invalid_token"
}

func ExampleClaims() {
	idpKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	tokens, _ := jwt.New(
		jwt.WithIssuer("https://id.example.com"),
		jwt.WithAudience("orders-api"),
		jwt.WithKey("k1", "ES256", &idpKey.PublicKey),
	)
	r := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/", nil)
	r.Header.Set("Authorization", "Bearer "+issue(idpKey, map[string]any{
		"iss": "https://id.example.com", "aud": "orders-api", "sub": "user-42",
		"exp": time.Now().Add(time.Hour).Unix(), "roles": []string{"admin"},
	}))
	p, _ := tokens.Authenticate(r)

	claims, ok := jwt.Claims(p)
	fmt.Println(ok, claims["roles"])
	// Output: true [admin]
}

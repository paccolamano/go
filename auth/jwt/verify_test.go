package jwt_test

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"errors"
	"math/big"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	jose "github.com/go-jose/go-jose/v4"

	"github.com/paccolamano/go/auth"
	"github.com/paccolamano/go/auth/jwt"
)

func staticVerifier(tb testing.TB, opts ...jwt.Option) *jwt.Verifier {
	tb.Helper()
	k := testKeys()
	return newVerifier(tb, nil, append([]jwt.Option{
		jwt.WithKey("rs", "RS256", &k.rsa.PublicKey),
		jwt.WithKey("ps", "PS256", &k.rsa.PublicKey),
		jwt.WithKey("es", "ES256", &k.ec.PublicKey),
		jwt.WithKey("ed", "EdDSA", k.ed.Public()),
		jwt.WithKey("hs", "HS256", k.hmac),
	}, opts...)...)
}

func TestVerify_Success(t *testing.T) {
	k := testKeys()
	v := staticVerifier(t)
	for _, c := range []struct {
		alg jose.SignatureAlgorithm
		key any
		kid string
	}{
		{jose.RS256, k.rsa, "rs"}, {jose.PS256, k.rsa, "ps"}, {jose.ES256, k.ec, "es"},
		{jose.EdDSA, k.ed, "ed"}, {jose.HS256, k.hmac, "hs"},
	} {
		p, err := v.Authenticate(bearer(t, sign(t, c.alg, c.key, c.kid, nil, validClaims())))
		if err != nil || p.Subject != "u1" || !slices.Equal(p.Methods, []string{"pwd"}) {
			t.Errorf("%s: %+v, %v", c.alg, p, err)
		}
	}

	idp := newIdP(t, jwk(&k.ec.PublicKey, "k1", "ES256"))
	if _, err := newVerifier(t, idp).Authenticate(bearer(t, sign(t, jose.ES256, k.ec, "k1", nil, validClaims()))); err != nil {
		t.Errorf("JWKS: %v", err)
	}
}

func TestVerify_Shape(t *testing.T) {
	v := staticVerifier(t)
	for _, c := range []struct {
		header string
		want   error
	}{
		{"", auth.ErrNoCredentials},
		{"Basic YTpi", auth.ErrNoCredentials},
		{"Bearer acme_0123456789abcdef_secret", auth.ErrNoCredentials},
		{"Bearer a.b", auth.ErrNoCredentials},
		{"Bearer a.b.c.d.e", auth.ErrInvalidCredentials},
		{"Bearer a.b.c", auth.ErrInvalidCredentials},
	} {
		r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil)
		if c.header != "" {
			r.Header.Set("Authorization", c.header)
		}
		if _, err := v.Authenticate(r); !errors.Is(err, c.want) {
			t.Errorf("%q: %v, want %v", c.header, err, c.want)
		}
	}
}

func TestVerify_Claims(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		k := testKeys()
		v := staticVerifier(t)
		now := time.Now()
		for name, c := range map[string]struct {
			claims map[string]any
			ok     bool
		}{
			"missing exp":          {with(validClaims(), "exp", nil), false},
			"expired 61s ago":      {with(validClaims(), "exp", now.Add(-61*time.Second).Unix()), false},
			"expired 59s ago":      {with(validClaims(), "exp", now.Add(-59*time.Second).Unix()), true},
			"nbf 2 minutes ahead":  {with(validClaims(), "nbf", now.Add(2*time.Minute).Unix()), false},
			"iat 2 minutes ahead":  {with(validClaims(), "iat", now.Add(2*time.Minute).Unix()), false},
			"wrong issuer":         {with(validClaims(), "iss", "https://evil.test"), false},
			"audience among other": {with(validClaims(), "aud", []string{"x", audience}), true},
			"wrong audience":       {with(validClaims(), "aud", []string{"x"}), false},
			"empty sub":            {with(validClaims(), "sub", ""), false},
			"amr string":           {with(validClaims(), "amr", "pwd"), false},
			"amr numbers":          {with(validClaims(), "amr", []int{1}), false},
			"no amr":               {with(validClaims(), "amr", nil), true},
		} {
			_, err := v.Authenticate(bearer(t, sign(t, jose.ES256, k.ec, "es", nil, c.claims)))
			if c.ok != (err == nil) || (!c.ok && !errors.Is(err, auth.ErrInvalidCredentials)) {
				t.Errorf("%s: %v, want ok %v", name, err, c.ok)
			}
		}
	})
}

func TestVerify_DuplicateMethods(t *testing.T) {
	k := testKeys()
	p, err := staticVerifier(t).Authenticate(bearer(t, sign(t, jose.ES256, k.ec, "es", nil, with(validClaims(), "amr", []string{"pwd", "mfa", "pwd"}))))
	if err != nil || !slices.Equal(p.Methods, []string{"pwd", "mfa"}) {
		t.Fatalf("Methods = %v, %v; want duplicates removed", p, err)
	}
}

func TestType_Required(t *testing.T) {
	k := testKeys()
	v := staticVerifier(t, jwt.WithType("at+jwt"))
	for typ, ok := range map[string]bool{"JWT": false, "": false, "application/at+jwt": true, "AT+JWT": true, "at+jwt": true} {
		var headers map[jose.HeaderKey]any
		if typ != "" {
			headers = map[jose.HeaderKey]any{jose.HeaderType: typ}
		}
		_, err := v.Authenticate(bearer(t, sign(t, jose.ES256, k.ec, "es", headers, validClaims())))
		if (err == nil) != ok {
			t.Errorf("typ %q: %v, want ok %v", typ, err, ok)
		}
	}
}

func TestVerify_KidSelection(t *testing.T) {
	k := testKeys()
	v := newVerifier(t, nil, jwt.WithKey("", "ES256", &k.ec.PublicKey), jwt.WithKey("", "EdDSA", k.ed.Public()))
	if _, err := v.Authenticate(bearer(t, sign(t, jose.ES256, k.ec, "", nil, validClaims()))); err != nil {
		t.Errorf("unique match without kid: %v", err)
	}
	two := newVerifier(t, nil, jwt.WithKey("a", "ES256", &k.ec.PublicKey), jwt.WithKey("b", "ES256", &k.another.PublicKey))
	if _, err := two.Authenticate(bearer(t, sign(t, jose.ES256, k.ec, "", nil, validClaims()))); !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Errorf("ambiguous match without kid: %v", err)
	}
	if _, err := two.Authenticate(bearer(t, sign(t, jose.ES256, k.ec, "a", nil, validClaims()))); err != nil {
		t.Errorf("kid a: %v", err)
	}
}

func TestVerify_Challenge(t *testing.T) {
	v := staticVerifier(t)
	mw, _ := auth.New(v)
	rec := httptest.NewRecorder()
	mw(http.NotFoundHandler()).ServeHTTP(rec, bearer(t, "a.b.c"))
	if got := rec.Result().Header.Values("WWW-Authenticate"); rec.Code != 401 || !slices.Equal(got, []string{`Bearer error="invalid_token"`}) {
		t.Fatalf("got %d %q", rec.Code, got)
	}
}

func TestClaims_Read(t *testing.T) {
	k := testKeys()
	p, err := staticVerifier(t).Authenticate(bearer(t, sign(t, jose.ES256, k.ec, "es", nil, with(validClaims(), "roles", []string{"admin"}))))
	if err != nil {
		t.Fatal(err)
	}
	claims, ok := jwt.Claims(p)
	if !ok || claims["sub"] != "u1" || claims["roles"].([]any)[0] != "admin" {
		t.Fatalf("Claims = %v, %v", claims, ok)
	}
	if c, ok := jwt.Claims(&auth.Principal{Subject: "x"}); ok || c != nil {
		t.Fatalf("Claims of another principal = %v, %v", c, ok)
	}
	if c, ok := jwt.Claims(nil); ok || c != nil {
		t.Fatalf("Claims(nil) = %v, %v", c, ok)
	}
}

func TestVerify_StaticNoNetwork(t *testing.T) {
	k := testKeys()
	idp := newIdP(t)
	v := newVerifier(t, nil, jwt.WithKey("es", "ES256", &k.ec.PublicKey), jwt.WithHTTPClient(&http.Client{Transport: idp}))
	if _, err := v.Authenticate(bearer(t, sign(t, jose.ES256, k.ec, "es", nil, validClaims()))); err != nil {
		t.Fatal(err)
	}
	if n := idp.requests.Load(); n != 0 {
		t.Fatalf("%d network requests with static keys", n)
	}
}

func TestVerifier_Config(t *testing.T) {
	k := testKeys()
	small := &rsa.PrivateKey{PublicKey: rsa.PublicKey{N: new(big.Int).Lsh(big.NewInt(1), 1023), E: 65537}} // 1,024-bit modulus, not a usable key
	p384, _ := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	es := jwt.WithKey("es", "ES256", &k.ec.PublicKey)
	base := []jwt.Option{jwt.WithIssuer(issuer), jwt.WithAudience(audience)}
	for name, opts := range map[string][]jwt.Option{
		"no issuer":         {jwt.WithAudience(audience), es},
		"no audience":       {jwt.WithIssuer(issuer), es},
		"no keys":           base,
		"keys and JWKS":     append(slices.Clone(base), es, jwt.WithJWKS(jwksURL)),
		"http JWKS":         append(slices.Clone(base), jwt.WithJWKS("http://id.example.test/jwks")),
		"JWKS without host": append(slices.Clone(base), jwt.WithJWKS("https:///jwks")),
		"unknown alg":       append(slices.Clone(base), jwt.WithKey("", "HS512", k.hmac)),
		"none alg":          append(slices.Clone(base), jwt.WithKey("", "none", k.hmac)),
		"type mismatch":     append(slices.Clone(base), jwt.WithKey("", "RS256", &k.ec.PublicKey)),
		"RSA 1024":          append(slices.Clone(base), jwt.WithKey("", "RS256", &small.PublicKey)),
		"P-384":             append(slices.Clone(base), jwt.WithKey("", "ES256", &p384.PublicKey)),
		"HS256 31 bytes":    append(slices.Clone(base), jwt.WithKey("", "HS256", k.hmac[:31])),
		"leeway negative":   append(slices.Clone(base), es, jwt.WithLeeway(-time.Second)),
		"leeway over 5m":    append(slices.Clone(base), es, jwt.WithLeeway(5*time.Minute+time.Second)),
		"refresh under 1s":  append(slices.Clone(base), es, jwt.WithRefreshInterval(time.Millisecond)),
		"refresh over 1h":   append(slices.Clone(base), es, jwt.WithRefreshInterval(2*time.Hour)),
		"nil client":        append(slices.Clone(base), es, jwt.WithHTTPClient(nil)),
		"empty type":        append(slices.Clone(base), es, jwt.WithType("")),
		"nil option":        append(slices.Clone(base), es, nil),
	} {
		if _, err := jwt.New(opts...); !errors.Is(err, auth.ErrInvalidConfig) {
			t.Errorf("%s: %v, want ErrInvalidConfig", name, err)
		}
	}
	if _, err := jwt.New(append(slices.Clone(base), es)...); err != nil {
		t.Errorf("valid configuration: %v", err)
	}
	_, err := jwt.New(append(slices.Clone(base), jwt.WithKey("", "HS256", []byte("short-secret")))...)
	if err == nil || strings.Contains(err.Error(), "short-secret") {
		t.Errorf("short HS256 key: %v; the error must exist and not contain the key", err)
	}
}

package jwt_test

import (
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	jose "github.com/go-jose/go-jose/v4"

	"github.com/paccolamano/go/auth"
)

func TestStepUp(t *testing.T) {
	k := testKeys()
	v := staticVerifier(t)
	mw, _ := auth.New(v)
	h := mw(auth.RequireMethods("mfa")(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, bearer(t, sign(t, jose.ES256, k.ec, "es", nil, validClaims())))
	want := []string{`Bearer error="insufficient_user_authentication", error_description="A different authentication level is required"`}
	if got := rec.Result().Header.Values("WWW-Authenticate"); rec.Code != 401 || !slices.Equal(got, want) {
		t.Fatalf("amr [pwd]: %d %q", rec.Code, got)
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, bearer(t, sign(t, jose.ES256, k.ec, "es", nil, with(validClaims(), "amr", []string{"pwd", "mfa"}))))
	if rec.Code != 200 {
		t.Fatalf("amr [pwd mfa]: %d", rec.Code)
	}
}

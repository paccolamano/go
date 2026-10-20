package auth_test

import (
	"errors"
	"net/http"
	"slices"
	"testing"

	"github.com/paccolamano/go/auth"
)

// headerToken is an authentication method defined outside the module.
type headerToken struct{}

func (headerToken) Authenticate(r *http.Request) (*auth.Principal, error) {
	switch v := r.Header.Get("X-Test-Token"); v {
	case "":
		return nil, auth.ErrNoCredentials
	case "good":
		return &auth.Principal{Subject: "svc"}, nil
	default:
		return nil, auth.ErrInvalidCredentials
	}
}

func (headerToken) Challenge(cause error) string {
	switch {
	case errors.Is(cause, auth.ErrNoCredentials):
		return "TestToken"
	case errors.Is(cause, auth.ErrInvalidCredentials):
		return `TestToken error="bad"`
	}
	return ""
}

func TestThirdPartyMethod(t *testing.T) {
	bearer := stub{err: auth.ErrNoCredentials, challenges: map[error]string{auth.ErrNoCredentials: "Bearer"}}
	mw := mustNew(t, auth.Any(headerToken{}, bearer))

	r := newRequest(t)
	r.Header.Set("X-Test-Token", "good")
	if rec, p, _ := serve(t, mw, r); rec.Code != 200 || p.Subject != "svc" {
		t.Fatalf("good token: %d %v", rec.Code, p)
	}

	rec, _, _ := serve(t, mw, newRequest(t))
	if got := challenges(rec); rec.Code != 401 || !slices.Equal(got, []string{"TestToken", "Bearer"}) {
		t.Fatalf("no token: %d %q", rec.Code, got)
	}

	r = newRequest(t)
	r.Header.Set("X-Test-Token", "bad")
	rec, _, _ = serve(t, mw, r)
	if got := challenges(rec); rec.Code != 401 || !slices.Equal(got, []string{`TestToken error="bad"`, "Bearer"}) {
		t.Fatalf("bad token: %d %q", rec.Code, got)
	}
}

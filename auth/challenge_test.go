package auth_test

import (
	"slices"
	"testing"

	"github.com/paccolamano/go/auth"
)

const (
	basicChallenge  = `Basic realm="api", charset="UTF-8"`
	cookieChallenge = `Cookie cookie-name="__Host-session"`
	invalidToken    = `Bearer error="invalid_token"`
)

func TestChallenges_Plain(t *testing.T) {
	basic := stub{err: auth.ErrNoCredentials, challenges: map[error]string{auth.ErrNoCredentials: basicChallenge}}
	bearer := stub{err: auth.ErrNoCredentials, challenges: map[error]string{auth.ErrNoCredentials: "Bearer"}}
	rec, _, _ := serve(t, mustNew(t, auth.Any(basic, bearer)), newRequest(t))
	if got := challenges(rec); !slices.Equal(got, []string{basicChallenge, "Bearer"}) {
		t.Fatalf("challenges = %q", got)
	}
}

func TestChallenges_Dedup(t *testing.T) {
	bearer := stub{err: auth.ErrNoCredentials, challenges: map[error]string{auth.ErrNoCredentials: "Bearer"}}
	rec, _, _ := serve(t, mustNew(t, auth.Any(bearer, bearer)), newRequest(t))
	if got := challenges(rec); !slices.Equal(got, []string{"Bearer"}) {
		t.Fatalf("challenges = %q, want one Bearer", got)
	}
}

func TestChallenges_InvalidReplacesScheme(t *testing.T) {
	none := plain{err: auth.ErrNoCredentials}
	session := stub{err: auth.ErrNoCredentials, challenges: map[error]string{
		auth.ErrNoCredentials: cookieChallenge, auth.ErrInvalidCredentials: cookieChallenge,
	}}
	keys := stub{err: auth.ErrNoCredentials, challenges: map[error]string{
		auth.ErrNoCredentials: "Bearer", auth.ErrInvalidCredentials: invalidToken,
	}}
	tokens := stub{err: auth.ErrInvalidCredentials, challenges: map[error]string{
		auth.ErrNoCredentials: "Bearer", auth.ErrInvalidCredentials: invalidToken,
	}}
	rec, _, _ := serve(t, mustNew(t, auth.Any(none, session, keys, tokens)), newRequest(t))
	if got := challenges(rec); !slices.Equal(got, []string{cookieChallenge, invalidToken}) {
		t.Fatalf("challenges = %q", got)
	}
}

func TestChallenges_None(t *testing.T) {
	rec, _, _ := serve(t, mustNew(t, auth.Any(plain{err: auth.ErrNoCredentials})), newRequest(t))
	if rec.Code != 401 || len(challenges(rec)) != 0 {
		t.Fatalf("got %d %q, want 401 without WWW-Authenticate", rec.Code, challenges(rec))
	}
}

func TestChallenges_NotOnOtherStatuses(t *testing.T) {
	for _, err := range []error{auth.ErrTooManyAttempts, errDBDown} {
		m := stub{err: err, challenges: map[error]string{auth.ErrNoCredentials: "Bearer"}}
		rec, _, _ := serve(t, mustNew(t, m), newRequest(t))
		if len(challenges(rec)) != 0 {
			t.Errorf("%d: WWW-Authenticate = %q", rec.Code, challenges(rec))
		}
	}
}

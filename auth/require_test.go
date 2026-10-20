package auth_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/paccolamano/go/auth"
)

const stepUp = `Bearer error="insufficient_user_authentication", error_description="A different authentication level is required"`

// guarded serves r through New(a, opts...) and then RequireMethods(methods...).
func guarded(tb testing.TB, a auth.Authenticator, opts []auth.Option, methods ...string) *httptest.ResponseRecorder {
	tb.Helper()
	mw := mustNew(tb, a, opts...)
	rm := auth.RequireMethods(methods...)
	rec, _, _ := serve(tb, func(h http.Handler) http.Handler { return mw(rm(h)) }, newRequest(tb))
	return rec
}

func TestRequireMethods_NoState(t *testing.T) {
	rec, _, _ := serve(t, auth.RequireMethods("mfa"), newRequest(t))
	if rec.Code != 500 {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
}

func TestRequireMethods_Anonymous(t *testing.T) {
	var got auth.Rejection
	fn := func(w http.ResponseWriter, _ *http.Request, rej auth.Rejection) { got = rej }
	m := stub{err: auth.ErrNoCredentials, challenges: map[error]string{auth.ErrNoCredentials: "Bearer"}}
	rec := guarded(t, m, []auth.Option{auth.WithAnonymous(), auth.WithRejectFunc(fn)}, "mfa")
	if rec.Code != 401 || !slices.Equal(challenges(rec), []string{"Bearer"}) {
		t.Fatalf("got %d %q, want 401 with Bearer", rec.Code, challenges(rec))
	}
	if got.Status != 401 || !errors.Is(got.Err, auth.ErrNoCredentials) {
		t.Fatalf("Rejection = %+v", got)
	}
}

func TestRequireMethods_Satisfied(t *testing.T) {
	m := stub{p: principal("u", "pwd", "otp", "mfa")}
	for _, methods := range [][]string{{"mfa"}, {"pwd", "mfa"}, {}} {
		if rec := guarded(t, m, nil, methods...); rec.Code != 200 {
			t.Errorf("RequireMethods(%q) = %d, want 200", methods, rec.Code)
		}
	}
}

func TestRequireMethods_StepUp(t *testing.T) {
	var got auth.Rejection
	fn := func(w http.ResponseWriter, _ *http.Request, rej auth.Rejection) { got = rej }
	tokens := stub{p: principal("u", "pwd"), challenges: map[error]string{
		auth.ErrNoCredentials: "Bearer", auth.ErrInsufficientMethods: stepUp,
	}}
	other := stub{err: auth.ErrNoCredentials, challenges: map[error]string{auth.ErrNoCredentials: "Other"}}
	rec := guarded(t, auth.Any(other, auth.Any(tokens)), []auth.Option{auth.WithRejectFunc(fn)}, "mfa")
	if rec.Code != 401 || !slices.Equal(challenges(rec), []string{stepUp}) {
		t.Fatalf("got %d %q, want 401 with the step-up challenge only", rec.Code, challenges(rec))
	}
	if !errors.Is(got.Err, auth.ErrInsufficientMethods) {
		t.Fatalf("Rejection.Err = %v", got.Err)
	}
}

func TestRequireMethods_Forbidden(t *testing.T) {
	m := stub{p: principal("u", "pwd"), challenges: map[error]string{auth.ErrNoCredentials: cookieChallenge}}
	rec := guarded(t, m, nil, "mfa")
	if rec.Code != 403 || len(challenges(rec)) != 0 {
		t.Fatalf("got %d %q, want 403 without challenges", rec.Code, challenges(rec))
	}
}

func TestRequireMethods_EmptyValue(t *testing.T) {
	if rec := guarded(t, stub{p: principal("u", "pwd")}, nil, ""); rec.Code != 403 {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
}

func TestRequireMethods_CopiesList(t *testing.T) {
	list := []string{"mfa"}
	rm := auth.RequireMethods(list...)
	list[0] = "pwd"
	mw := mustNew(t, stub{p: principal("u", "pwd")})
	rec, _, _ := serve(t, func(h http.Handler) http.Handler { return mw(rm(h)) }, newRequest(t))
	if rec.Code != 403 {
		t.Fatalf("status = %d, want 403: the list was not copied", rec.Code)
	}
}

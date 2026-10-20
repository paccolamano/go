package auth_test

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/paccolamano/go/auth"
)

var errDBDown = errors.New("db down")

func TestNew_Outcomes(t *testing.T) {
	for _, c := range []struct {
		name      string
		p         *auth.Principal
		err       error
		anonymous bool
		status    int
		principal bool
		challenge bool
	}{
		{"principal", principal("u1", "pwd"), nil, false, 200, true, false},
		{"no credentials", nil, auth.ErrNoCredentials, false, 401, false, true},
		{"no credentials anonymous", nil, auth.ErrNoCredentials, true, 200, false, false},
		{"invalid", nil, auth.ErrInvalidCredentials, false, 401, false, true},
		{"invalid anonymous", nil, auth.ErrInvalidCredentials, true, 401, false, true},
		{"too many attempts", nil, auth.ErrTooManyAttempts, false, 429, false, false},
		{"too many and invalid", nil, fmt.Errorf("%w: %w", auth.ErrTooManyAttempts, auth.ErrInvalidCredentials), false, 429, false, false},
		{"failure", nil, errDBDown, false, 500, false, false},
		{"nil nil", nil, nil, false, 500, false, false},
		{"empty subject", principal(""), nil, false, 500, false, false},
		{"wrapped no credentials", nil, fmt.Errorf("x: %w", auth.ErrNoCredentials), false, 401, false, true},
		{"wrapped invalid", nil, fmt.Errorf("x: %w", auth.ErrInvalidCredentials), false, 401, false, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			var opts []auth.Option
			if c.anonymous {
				opts = append(opts, auth.WithAnonymous())
			}
			m := stub{p: c.p, err: c.err, challenges: map[error]string{
				auth.ErrNoCredentials: "Test", auth.ErrInvalidCredentials: "Test error=1",
			}}
			rec, p, ok := serve(t, mustNew(t, m, opts...), newRequest(t))
			if rec.Code != c.status {
				t.Fatalf("status = %d, want %d", rec.Code, c.status)
			}
			if ok != c.principal || (ok && p != c.p) {
				t.Fatalf("FromContext = %v, %v; want principal %v", p, ok, c.principal)
			}
			if got := len(challenges(rec)) > 0; got != c.challenge {
				t.Fatalf("WWW-Authenticate = %q, want present %v", challenges(rec), c.challenge)
			}
		})
	}
}

func TestNew_RejectFunc(t *testing.T) {
	for _, c := range []struct {
		err    error
		status int
		is     error
	}{
		{auth.ErrNoCredentials, 401, auth.ErrNoCredentials},
		{auth.ErrInvalidCredentials, 401, auth.ErrInvalidCredentials},
		{auth.ErrTooManyAttempts, 429, auth.ErrTooManyAttempts},
		{errDBDown, 500, errDBDown},
	} {
		t.Run(fmt.Sprint(c.status, c.is), func(t *testing.T) {
			var got auth.Rejection
			var header []string
			fn := func(w http.ResponseWriter, _ *http.Request, rej auth.Rejection) {
				got = rej
				header = w.Header().Values("WWW-Authenticate")
				w.WriteHeader(418)
			}
			m := stub{err: c.err, challenges: map[error]string{auth.ErrNoCredentials: "Test", auth.ErrInvalidCredentials: "Test"}}
			rec, _, _ := serve(t, mustNew(t, m, auth.WithRejectFunc(fn)), newRequest(t))
			if rec.Code != 418 {
				t.Fatalf("status = %d, want the RejectFunc's 418", rec.Code)
			}
			if got.Status != c.status || !errors.Is(got.Err, c.is) {
				t.Fatalf("Rejection = %+v, want status %d and %v", got, c.status, c.is)
			}
			if c.status == 401 && len(header) == 0 {
				t.Fatal("WWW-Authenticate was not set before the RejectFunc ran")
			}
		})
	}

	t.Run("writes nothing", func(t *testing.T) {
		fn := func(http.ResponseWriter, *http.Request, auth.Rejection) {}
		rec, _, _ := serve(t, mustNew(t, stub{err: auth.ErrInvalidCredentials}, auth.WithRejectFunc(fn)), newRequest(t))
		if rec.Code != 401 || rec.Body.Len() != 0 {
			t.Fatalf("got %d %q, want 401 with an empty body", rec.Code, rec.Body.String())
		}
	})
}

func TestNew_DefaultResponse(t *testing.T) {
	for _, c := range []struct {
		err    error
		status int
	}{{auth.ErrNoCredentials, 401}, {auth.ErrTooManyAttempts, 429}, {errDBDown, 500}} {
		rec, _, _ := serve(t, mustNew(t, stub{err: c.err}), newRequest(t))
		if ct := rec.Header().Get("Content-Type"); ct != "text/plain; charset=utf-8" {
			t.Errorf("%d: Content-Type = %q", c.status, ct)
		}
		if body := strings.TrimSpace(rec.Body.String()); body != http.StatusText(c.status) {
			t.Errorf("%d: body = %q, want %q", c.status, body, http.StatusText(c.status))
		}
		if strings.Contains(rec.Body.String(), "db down") {
			t.Error("the 500 body exposes the error")
		}
	}
}

func TestNew_Config(t *testing.T) {
	ok := stub{p: principal("u")}
	for name, build := range map[string]func() (func(http.Handler) http.Handler, error){
		"nil authenticator": func() (func(http.Handler) http.Handler, error) { return auth.New(nil) },
		"empty Any":         func() (func(http.Handler) http.Handler, error) { return auth.New(auth.Any()) },
		"nil in Any":        func() (func(http.Handler) http.Handler, error) { return auth.New(auth.Any(nil)) },
		"nested empty Any": func() (func(http.Handler) http.Handler, error) {
			return auth.New(auth.Any(ok, auth.Any()))
		},
		"nil RejectFunc": func() (func(http.Handler) http.Handler, error) {
			return auth.New(ok, auth.WithRejectFunc(nil))
		},
		"nil Option": func() (func(http.Handler) http.Handler, error) { return auth.New(ok, nil) },
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := build(); !errors.Is(err, auth.ErrInvalidConfig) {
				t.Fatalf("error = %v, want ErrInvalidConfig", err)
			}
		})
	}
}

func TestFromContext_NoState(t *testing.T) {
	if p, ok := auth.FromContext(t.Context()); p != nil || ok {
		t.Fatalf("FromContext = %v, %v; want nil, false", p, ok)
	}
}

func TestNew_RejectFuncResponseController(t *testing.T) {
	fn := func(w http.ResponseWriter, _ *http.Request, rej auth.Rejection) {
		w.WriteHeader(rej.Status)
		if err := http.NewResponseController(w).Flush(); err != nil {
			t.Errorf("Flush through the rejection writer: %v", err)
		}
	}
	rec, _, _ := serve(t, mustNew(t, stub{err: auth.ErrNoCredentials}, auth.WithRejectFunc(fn)), newRequest(t))
	if !rec.Flushed || rec.Code != 401 {
		t.Fatalf("flushed %v, status %d", rec.Flushed, rec.Code)
	}
}

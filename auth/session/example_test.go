package session_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"

	"github.com/paccolamano/go/auth"
	"github.com/paccolamano/go/auth/memstore"
	"github.com/paccolamano/go/auth/session"
)

func ExampleManager() {
	sessions, err := session.New(memstore.New())
	if err != nil {
		panic(err)
	}
	ctx := context.Background()

	// POST /login, after the password was checked.
	login := httptest.NewRecorder()
	p := &auth.Principal{Subject: "user-42", Methods: []string{"pwd"}}
	if err := sessions.Issue(login, httptest.NewRequestWithContext(ctx, http.MethodPost, "/login", nil), p); err != nil {
		panic(err)
	}
	c := login.Result().Cookies()[0]
	fmt.Println(c.Name, c.Secure, c.HttpOnly, c.SameSite == http.SameSiteLaxMode)

	// A later request carries the cookie.
	r := httptest.NewRequestWithContext(ctx, http.MethodGet, "/", nil)
	r.AddCookie(c)
	got, err := sessions.Authenticate(r)
	fmt.Println(got.Subject, got.Methods, err)

	// POST /logout.
	_ = sessions.Revoke(httptest.NewRecorder(), r)
	_, err = sessions.Authenticate(r)
	fmt.Println(err)
	// Output:
	// __Host-session true true true
	// user-42 [pwd] <nil>
	// auth: no credentials
}

func ExampleManager_Pending() {
	sessions, _ := session.New(memstore.New())
	ctx := context.Background()

	// POST /login for a user enrolled in a second factor.
	login := httptest.NewRecorder()
	_ = sessions.IssuePending(login, httptest.NewRequestWithContext(ctx, http.MethodPost, "/login", nil),
		&auth.Principal{Subject: "user-42", Methods: []string{"pwd"}})

	// POST /login/otp carries the pending cookie.
	r := httptest.NewRequestWithContext(ctx, http.MethodPost, "/login/otp", nil)
	for _, c := range login.Result().Cookies() {
		r.AddCookie(c)
	}
	_, err := sessions.Authenticate(r)
	fmt.Println(err) // a pending login authenticates nothing

	pending, _ := sessions.Pending(r)
	err = pending.Attempt(ctx, func(context.Context) error {
		return nil // here totp.Verifier or recovery.Verifier checks the code
	})
	fmt.Println(pending.Principal().Subject, err)

	done := httptest.NewRecorder()
	_ = sessions.Issue(done, r, &auth.Principal{Subject: "user-42", Methods: []string{"pwd", "otp", "mfa"}})
	_, err = sessions.Pending(r)
	fmt.Println(err)
	// Output:
	// auth: no credentials
	// user-42 <nil>
	// auth: no credentials
}

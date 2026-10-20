package totp_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"time"

	"github.com/paccolamano/go/auth"
	"github.com/paccolamano/go/auth/internal/hotp"
	"github.com/paccolamano/go/auth/memstore"
	"github.com/paccolamano/go/auth/session"
	"github.com/paccolamano/go/auth/totp"
)

// active stands in for the developer's table of confirmed secrets.
var active = map[string][]byte{}

func lookupSecret(_ context.Context, subject string) ([]byte, error) {
	s, ok := active[subject]
	if !ok {
		return nil, totp.ErrNotFound
	}
	return s, nil
}

// authenticatorApp computes the code an authenticator app would show.
func authenticatorApp(secret []byte) string {
	return hotp.Value(hotp.SHA1, secret, uint64(time.Now().Unix()/30), 6)
}

func ExampleNewEnrollment() {
	e, err := totp.NewEnrollment("Acme", "alice@example.com")
	if err != nil {
		panic(err)
	}
	// Show e.URI() as a QR code; keep e.Secret() as a candidate.
	fmt.Println(strings.HasPrefix(e.URI(), "otpauth://totp/Acme:alice@example.com?secret="))
	fmt.Println(len(e.Secret()), e)
	// Output:
	// true
	// 20 [REDACTED]
}

func ExampleVerifier_Confirm() {
	v, _ := totp.New(lookupSecret, memstore.New())
	ctx := context.Background()

	e, _ := totp.NewEnrollment("Acme", "bob@example.com")
	candidate := e.Secret()

	// The user types the first code shown by the app.
	if err := v.Confirm(ctx, "user-7", candidate, authenticatorApp(candidate)); err == nil {
		active["user-7"] = candidate // now the second factor is active
	}
	enrolled, _ := v.Enrolled(ctx, "user-7")
	fmt.Println(enrolled)
	// Output: true
}

func ExampleVerifier_Enrolled() {
	v, _ := totp.New(lookupSecret, memstore.New())
	enrolled, err := v.Enrolled(context.Background(), "nobody")
	fmt.Println(enrolled, err)
	// Output: false <nil>
}

func ExampleVerifier_Verify() {
	store := memstore.New()
	sessions, _ := session.New(store)
	v, _ := totp.New(lookupSecret, store)
	ctx := context.Background()
	active["user-42"] = []byte("an example secret, 20")

	// POST /login: the password was checked; the user is enrolled.
	login := httptest.NewRecorder()
	_ = sessions.IssuePending(login, httptest.NewRequestWithContext(ctx, http.MethodPost, "/login", nil),
		&auth.Principal{Subject: "user-42", Methods: []string{"pwd"}})

	// POST /login/otp
	r := httptest.NewRequestWithContext(ctx, http.MethodPost, "/login/otp", nil)
	for _, c := range login.Result().Cookies() {
		r.AddCookie(c)
	}
	pending, _ := sessions.Pending(r)
	p, err := v.Verify(ctx, pending, authenticatorApp(active["user-42"]))
	fmt.Println(p.Methods, err)
	_ = sessions.Issue(httptest.NewRecorder(), r, p) // new session; the pending login is deleted
	// Output: [pwd otp mfa] <nil>
}

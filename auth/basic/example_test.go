package basic_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"

	"github.com/paccolamano/go/auth"
	"github.com/paccolamano/go/auth/basic"
)

// demoChecker accepts one fixed account. In production, pass a
// *password.Checker.
type demoChecker struct{}

func (demoChecker) Check(_ context.Context, username, password string) (*auth.Principal, error) {
	if username == "ci" && password == "correct horse" {
		return &auth.Principal{Subject: "ci-bot", Methods: []string{"pwd"}}, nil
	}
	return nil, auth.ErrInvalidCredentials
}

func ExampleNew() {
	b, err := basic.New(demoChecker{}, basic.WithRealm("api"))
	if err != nil {
		panic(err)
	}
	mw, _ := auth.New(b)
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p, _ := auth.FromContext(r.Context())
		_, _ = fmt.Fprint(w, p.Subject)
	}))

	r := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/", nil)
	r.SetBasicAuth("ci", "correct horse")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	fmt.Println(rec.Code, rec.Body.String())

	r.SetBasicAuth("ci", "wrong")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	fmt.Println(rec.Code, rec.Header().Get("WWW-Authenticate"))
	// Output:
	// 200 ci-bot
	// 401 Basic realm="api", charset="UTF-8"
}

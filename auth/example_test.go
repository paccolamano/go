package auth_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"

	"github.com/paccolamano/go/auth"
)

// userHeader is a toy method that trusts the X-User header. Never use it in
// production: it only illustrates the Authenticator contract.
type userHeader struct{}

func (userHeader) Authenticate(r *http.Request) (*auth.Principal, error) {
	u := r.Header.Get("X-User")
	if u == "" {
		return nil, auth.ErrNoCredentials
	}
	return &auth.Principal{Subject: u, Methods: []string{"pwd"}}, nil
}

func (userHeader) Challenge(cause error) string {
	if errors.Is(cause, auth.ErrNoCredentials) {
		return "XUser"
	}
	return ""
}

// rejectAll is a toy method that rejects every Bearer credential.
type rejectAll struct{}

func (rejectAll) Authenticate(r *http.Request) (*auth.Principal, error) {
	if r.Header.Get("Authorization") == "" {
		return nil, auth.ErrNoCredentials
	}
	return nil, auth.ErrInvalidCredentials
}

func ExampleAny() {
	mw, err := auth.New(auth.Any(rejectAll{}, userHeader{}))
	if err != nil {
		panic(err)
	}
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p, _ := auth.FromContext(r.Context())
		_, _ = fmt.Fprint(w, p.Subject)
	}))

	r := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/", nil)
	r.Header.Set("X-User", "alice")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	fmt.Println(rec.Code, rec.Body.String())

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/", nil))
	fmt.Println(rec.Code, rec.Header().Get("WWW-Authenticate"))
	// Output:
	// 200 alice
	// 401 XUser
}

func ExampleFromContext() {
	mw, _ := auth.New(userHeader{}, auth.WithAnonymous())
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if p, ok := auth.FromContext(r.Context()); ok {
			fmt.Println("hello", p.Subject, p.Methods)
			return
		}
		fmt.Println("hello, anonymous")
	}))

	r := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/", nil)
	r.Header.Set("X-User", "bob")
	h.ServeHTTP(httptest.NewRecorder(), r)
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/", nil))
	// Output:
	// hello bob [pwd]
	// hello, anonymous
}

func ExampleWithRejectFunc() {
	// An RFC 9457 problem document, written without any dependency.
	problem := func(w http.ResponseWriter, _ *http.Request, rej auth.Rejection) {
		detail := "authentication required"
		if errors.Is(rej.Err, auth.ErrInsufficientMethods) {
			detail = "a second factor is required"
		}
		w.Header().Set("Content-Type", "application/problem+json")
		w.WriteHeader(rej.Status)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"title":  http.StatusText(rej.Status),
			"status": rej.Status,
			"detail": detail,
		})
	}
	mw, _ := auth.New(userHeader{}, auth.WithRejectFunc(problem))
	h := mw(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/", nil))
	fmt.Println(rec.Code, rec.Header().Get("Content-Type"))
	fmt.Print(rec.Body.String())
	// Output:
	// 401 application/problem+json
	// {"detail":"authentication required","status":401,"title":"Unauthorized"}
}

package recovery_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"

	"github.com/paccolamano/go/auth"
	"github.com/paccolamano/go/auth/memstore"
	"github.com/paccolamano/go/auth/recovery"
	"github.com/paccolamano/go/auth/session"
)

func ExampleGenerate() {
	codes, err := recovery.Generate(recovery.DefaultCount)
	if err != nil {
		panic(err)
	}
	// Show codes.Plain() once; store only codes.Hashes().
	fmt.Println(len(codes.Plain()), len(codes.Plain()[0]), len(codes.Hashes()[0]), codes)
	// Output: 10 19 64 [REDACTED]
}

func ExampleVerifier_Verify() {
	codes, _ := recovery.Generate(recovery.DefaultCount)
	unused := codes.Hashes() // the developer's table

	store := memstore.New()
	sessions, _ := session.New(store)
	v, _ := recovery.New(
		func(context.Context, string) ([]string, error) { return slices.Clone(unused), nil },
		func(_ context.Context, _, hash string) error {
			i := slices.Index(unused, hash)
			if i < 0 {
				return recovery.ErrNotFound
			}
			unused = slices.Delete(unused, i, i+1)
			return nil
		},
		store)
	ctx := context.Background()

	login := httptest.NewRecorder()
	_ = sessions.IssuePending(login, httptest.NewRequestWithContext(ctx, http.MethodPost, "/login", nil),
		&auth.Principal{Subject: "user-42", Methods: []string{"pwd"}})
	r := httptest.NewRequestWithContext(ctx, http.MethodPost, "/login/recovery", nil)
	for _, c := range login.Result().Cookies() {
		r.AddCookie(c)
	}
	pending, _ := sessions.Pending(r)

	p, err := v.Verify(ctx, pending, codes.Plain()[0])
	fmt.Println(p.Methods, err, len(unused))
	// Output: [pwd mfa] <nil> 9
}

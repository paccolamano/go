package apikey_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"

	"github.com/paccolamano/go/auth"
	"github.com/paccolamano/go/auth/apikey"
)

// keyTable stands in for the developer's table: identifier → account.
var keyTable = map[string]apikey.Account{}

func lookupKey(_ context.Context, id string) (apikey.Account, error) {
	a, ok := keyTable[id]
	if !ok {
		return apikey.Account{}, apikey.ErrNotFound
	}
	return a, nil
}

func ExampleKeys_Generate() {
	keys, err := apikey.New("acme", lookupKey)
	if err != nil {
		panic(err)
	}
	key, stored := keys.Generate()
	// Show key once; save stored.ID, stored.Hash and the owner.
	keyTable[stored.ID] = apikey.Account{Subject: "client-9", Hash: stored.Hash}

	fmt.Println(strings.HasPrefix(string(key), "acme_"), len(stored.Hash))
	// Output: true 32
}

func ExampleNew() {
	keys, _ := apikey.New("acme", lookupKey)
	key, stored := keys.Generate()
	keyTable[stored.ID] = apikey.Account{Subject: "client-9", Hash: stored.Hash}

	mw, _ := auth.New(keys)
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p, _ := auth.FromContext(r.Context())
		_, _ = fmt.Fprint(w, p.Subject)
	}))

	r := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/", nil)
	r.Header.Set("Authorization", "Bearer "+string(key))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	fmt.Println(rec.Code, rec.Body.String())
	// Output: 200 client-9
}

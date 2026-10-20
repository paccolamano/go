package apikey_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/paccolamano/go/auth/apikey"
)

// table stands in for the developer's API key table.
type table struct {
	mu   sync.Mutex
	m    map[string]apikey.Account
	err  error
	ids  []string
	ctxs []context.Context
}

func (t *table) lookup(ctx context.Context, id string) (apikey.Account, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.ids = append(t.ids, id)
	t.ctxs = append(t.ctxs, ctx)
	if t.err != nil {
		return apikey.Account{}, t.err
	}
	a, ok := t.m[id]
	if !ok {
		return apikey.Account{}, apikey.ErrNotFound
	}
	return a, nil
}

func newKeys(tb testing.TB, opts ...apikey.Option) (*table, *apikey.Keys) {
	tb.Helper()
	tbl := &table{m: map[string]apikey.Account{}}
	k, err := apikey.New("acme", tbl.lookup, opts...)
	if err != nil {
		tb.Fatal(err)
	}
	return tbl, k
}

// store generates a key owned by subject and stores it in tbl.
func store(tbl *table, k *apikey.Keys, subject string) apikey.Key {
	key, s := k.Generate()
	tbl.mu.Lock()
	tbl.m[s.ID] = apikey.Account{Subject: subject, Hash: s.Hash}
	tbl.mu.Unlock()
	return key
}

func bearer(tb testing.TB, value string) *http.Request {
	tb.Helper()
	r := httptest.NewRequestWithContext(tb.Context(), http.MethodGet, "/", nil)
	if value != "" {
		r.Header.Set("Authorization", value)
	}
	return r
}

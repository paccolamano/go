package apikey_test

import (
	"strings"
	"testing"
)

func FuzzAPIKey(f *testing.F) {
	tbl, k := newKeys(f)
	key := string(store(tbl, k, "c"))
	f.Add("Bearer " + key)
	f.Add("Bearer acme_")
	f.Add("bearer " + key[:20])
	f.Add("Basic " + key)
	f.Add("")
	f.Fuzz(func(t *testing.T, v string) {
		_, err := k.Authenticate(bearer(t, v))
		if err != nil {
			return
		}
		scheme, rest, _ := strings.Cut(v, " ")
		if !strings.EqualFold(scheme, "Bearer") || strings.TrimLeft(rest, " ") != key {
			t.Fatalf("%q authenticated", v)
		}
	})
}

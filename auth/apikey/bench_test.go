package apikey_test

import "testing"

func BenchmarkAPIKeyAuthenticate(b *testing.B) {
	tbl, k := newKeys(b)
	r := bearer(b, "Bearer "+string(store(tbl, k, "c")))
	for b.Loop() {
		if _, err := k.Authenticate(r); err != nil {
			b.Fatal(err)
		}
	}
}

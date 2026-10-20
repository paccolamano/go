package session_test

import (
	"testing"

	"github.com/paccolamano/go/auth/memstore"
)

func BenchmarkSessionAuthenticate(b *testing.B) {
	m := mustManager(b, memstore.New())
	r := request(b, issue(b, m, alice()))
	for b.Loop() {
		if _, err := m.Authenticate(r); err != nil {
			b.Fatal(err)
		}
	}
}

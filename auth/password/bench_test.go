package password_test

import (
	"context"
	"testing"

	"github.com/paccolamano/go/auth/password"
)

func BenchmarkCheck(b *testing.B) {
	hash, err := password.Hash("correct horse")
	if err != nil {
		b.Fatal(err)
	}
	acc := &accounts{m: map[string]password.Account{"alice": {Subject: "user-1", Hash: hash}}}
	c, err := password.NewChecker(acc.lookup)
	if err != nil {
		b.Fatal(err)
	}
	ctx := context.Background()
	for _, bc := range []struct{ name, user, pw string }{
		{"existing-user", "alice", "correct horse"},
		{"missing-user", "bob", "correct horse"},
		{"wrong-password", "alice", "wrong"},
	} {
		b.Run(bc.name, func(b *testing.B) {
			for b.Loop() {
				_, _ = c.Check(ctx, bc.user, bc.pw)
			}
		})
	}
}

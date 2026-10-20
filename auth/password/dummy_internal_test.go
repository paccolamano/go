package password

import (
	"context"
	"testing"
)

func noAccount(context.Context, string) (Account, error) { return Account{}, ErrNotFound }

func TestDummyHashParams(t *testing.T) {
	for _, opts := range [][]Option{nil, {WithParams(Params{Memory: 7168, Iterations: 5, Parallelism: 1})}} {
		c, err := NewChecker(noAccount, opts...)
		if err != nil {
			t.Fatal(err)
		}
		if c.dummy.params != c.params || len(c.dummy.salt) != saltLen || len(c.dummy.key) != keyLen {
			t.Fatalf("dummy = %+v with %d-byte salt and %d-byte key; checker params %+v", c.dummy.params, len(c.dummy.salt), len(c.dummy.key), c.params)
		}
	}
}

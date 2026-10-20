package recovery_test

import (
	"strings"
	"testing"

	"github.com/paccolamano/go/auth/memstore"
	"github.com/paccolamano/go/auth/recovery"
)

func FuzzRecoveryCode(f *testing.F) {
	generated, err := recovery.Generate(1)
	if err != nil {
		f.Fatal(err)
	}
	stored := generated.Plain()[0]
	f.Add(stored)
	f.Add(strings.ToUpper(stored))
	f.Add("")
	f.Add("abcd-efgh")
	f.Add("\x00\xff")
	canonical := strings.ReplaceAll(stored, "-", "")
	f.Fuzz(func(t *testing.T, input string) {
		store := memstore.New()
		db := &codes{unused: map[string][]string{"user-1": generated.Hashes()}}
		v, err := recovery.New(db.lookup, db.consume, store)
		if err != nil {
			t.Fatal(err)
		}
		_, err = v.Verify(t.Context(), newPending(t, mustSessions(t, store), "user-1"), input)
		norm := strings.NewReplacer(" ", "", "-", "", "i", "1", "l", "1", "o", "0").Replace(strings.ToLower(input))
		if err == nil && norm != canonical {
			t.Fatalf("input %q accepted", input)
		}
	})
}

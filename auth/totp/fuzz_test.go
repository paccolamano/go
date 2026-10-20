package totp_test

import (
	"testing"
	"testing/synctest"

	"github.com/paccolamano/go/auth/memstore"
)

func FuzzTOTPCode(f *testing.F) {
	for _, s := range []string{"123456", "000000", "", "12345a", "１２３４５６", "1234567"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, input string) {
		synctest.Test(t, func(t *testing.T) {
			store := memstore.New()
			s := &secrets{m: map[string][]byte{"user-1": secret}}
			v := mustVerifier(t, s.lookup, store)
			valid := map[string]bool{code(-1): true, code(0): true, code(1): true}
			_, err := v.Verify(t.Context(), newPending(t, mustSessions(t, store), "user-1"), input)
			if err == nil && !valid[input] {
				t.Fatalf("code %q accepted", input)
			}
		})
	})
}

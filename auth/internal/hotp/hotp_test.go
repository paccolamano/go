package hotp_test

import (
	"testing"

	"github.com/paccolamano/go/auth/internal/hotp"
)

func TestRFC4226Vectors(t *testing.T) {
	secret := []byte("12345678901234567890")
	want := []string{"755224", "287082", "359152", "969429", "338314", "254676", "287922", "162583", "399871", "520489"}
	for counter, w := range want {
		if got := hotp.Value(hotp.SHA1, secret, uint64(counter), 6); got != w {
			t.Errorf("counter %d: %s, want %s", counter, got, w)
		}
	}
}

func TestRFC6238Vectors(t *testing.T) {
	secrets := map[hotp.Algorithm][]byte{
		hotp.SHA1:   []byte("12345678901234567890"),
		hotp.SHA256: []byte("12345678901234567890123456789012"),
		hotp.SHA512: []byte("1234567890123456789012345678901234567890123456789012345678901234"),
	}
	for _, c := range []struct {
		time                 uint64
		sha1, sha256, sha512 string
	}{
		{59, "94287082", "46119246", "90693936"},
		{1111111109, "07081804", "68084774", "25091201"},
		{1111111111, "14050471", "67062674", "99943326"},
		{1234567890, "89005924", "91819424", "93441116"},
		{2000000000, "69279037", "90698825", "38618901"},
		{20000000000, "65353130", "77737706", "47863826"},
	} {
		for alg, want := range map[hotp.Algorithm]string{hotp.SHA1: c.sha1, hotp.SHA256: c.sha256, hotp.SHA512: c.sha512} {
			if got := hotp.Value(alg, secrets[alg], c.time/30, 8); got != want {
				t.Errorf("time %d alg %d: %s, want %s", c.time, alg, got, want)
			}
		}
	}
	if got := hotp.Value(hotp.SHA1, secrets[hotp.SHA1], 1111111109/30, 8); len(got) != 8 || got[0] != '0' {
		t.Errorf("leading zero lost: %q", got)
	}
}

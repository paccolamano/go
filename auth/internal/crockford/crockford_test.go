package crockford_test

import (
	"bytes"
	"crypto/rand"
	"testing"

	"github.com/paccolamano/go/auth/internal/crockford"
)

func TestEncode_Lengths(t *testing.T) {
	for _, c := range []struct{ bytes, chars int }{{10, 16}, {32, 52}} {
		if got := len(crockford.Encode(make([]byte, c.bytes))); got != c.chars {
			t.Errorf("len(Encode(%d bytes)) = %d, want %d", c.bytes, got, c.chars)
		}
	}
	if got := crockford.Encode([]byte{0xff, 0xff, 0xff, 0xff, 0xff}); got != "zzzzzzzz" {
		t.Errorf("Encode(ff*5) = %q, want zzzzzzzz", got)
	}
}

func TestDecode_RoundTrip(t *testing.T) {
	for n := 0; n <= 40; n++ {
		b := make([]byte, n)
		_, _ = rand.Read(b)
		s := crockford.Encode(b)
		got, err := crockford.Decode(s)
		if err != nil || !bytes.Equal(got, b) {
			t.Fatalf("Decode(Encode(%x)) = %x, %v", b, got, err)
		}
	}
}

func TestDecode_Strict(t *testing.T) {
	valid := crockford.Encode([]byte("0123456789"))
	for _, s := range []string{
		valid[:15] + "i", valid[:15] + "l", valid[:15] + "o", valid[:15] + "u",
		valid[:15] + "A", valid[:15] + "-", valid[:15] + "\n", valid + "\n", "0",
	} {
		if _, err := crockford.Decode(s); err == nil {
			t.Errorf("Decode(%q) succeeded, want an error", s)
		}
	}
	// A non-canonical last character, with non-zero padding bits, is rejected.
	enc := crockford.Encode(make([]byte, 32))
	if _, err := crockford.Decode(enc[:51] + "1"); err == nil {
		t.Error("Decode accepted a non-canonical encoding")
	}
}

func TestNormalize(t *testing.T) {
	for in, want := range map[string]string{
		"ABCD-EFGH IJKL-MNOP": "abcdefgh1jk1mn0p",
		"abcd efgh":           "abcdefgh",
		"O0 Il":               "0011",
		"":                    "",
	} {
		if got := crockford.Normalize(in); got != want {
			t.Errorf("Normalize(%q) = %q, want %q", in, got, want)
		}
	}
}

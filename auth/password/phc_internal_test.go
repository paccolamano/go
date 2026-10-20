package password

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"strings"
	"testing"
)

func b64(n int) string { return base64.RawStdEncoding.EncodeToString(bytes.Repeat([]byte{7}, n)) }

func phcString(m, t, p uint64, saltLen, keyLen int) string {
	return fmt.Sprintf("$argon2id$v=19$m=%d,t=%d,p=%d$%s$%s", m, t, p, b64(saltLen), b64(keyLen))
}

func TestPHC_RoundTrip(t *testing.T) {
	h := phc{params: Params{Memory: 19456, Iterations: 2, Parallelism: 1}, salt: bytes.Repeat([]byte{1}, 16), key: bytes.Repeat([]byte{2}, 32)}
	s := encodePHC(h)
	want := "$argon2id$v=19$m=19456,t=2,p=1$" + base64.RawStdEncoding.EncodeToString(h.salt) + "$" + base64.RawStdEncoding.EncodeToString(h.key)
	if s != want {
		t.Fatalf("encodePHC = %q, want %q", s, want)
	}
	got, err := parsePHC(s)
	if err != nil || got.params != h.params || !bytes.Equal(got.salt, h.salt) || !bytes.Equal(got.key, h.key) {
		t.Fatalf("parsePHC = %+v, %v", got, err)
	}
}

func TestPHC_Bounds(t *testing.T) {
	for _, c := range []struct {
		name      string
		m, t, p   uint64
		salt, key int
		ok        bool
	}{
		{"memory min", 1, 1, 1, 16, 32, true},
		{"memory zero", 0, 1, 1, 16, 32, false},
		{"memory max", 2097152, 1, 1, 16, 32, true},
		{"memory over", 2097153, 1, 1, 16, 32, false},
		{"iterations zero", 19456, 0, 1, 16, 32, false},
		{"iterations max", 19456, 100, 1, 16, 32, true},
		{"iterations over", 19456, 101, 1, 16, 32, false},
		{"parallelism zero", 19456, 2, 0, 16, 32, false},
		{"parallelism max", 19456, 2, 64, 16, 32, true},
		{"parallelism over", 19456, 2, 65, 16, 32, false},
		{"salt min", 19456, 2, 1, 8, 32, true},
		{"salt under", 19456, 2, 1, 7, 32, false},
		{"salt max", 19456, 2, 1, 64, 32, true},
		{"salt over", 19456, 2, 1, 65, 32, false},
		{"key min", 19456, 2, 1, 16, 16, true},
		{"key under", 19456, 2, 1, 16, 15, false},
		{"key max", 19456, 2, 1, 16, 64, true},
		{"key over", 19456, 2, 1, 16, 65, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			_, err := parsePHC(phcString(c.m, c.t, c.p, c.salt, c.key))
			if (err == nil) != c.ok {
				t.Fatalf("parsePHC error = %v, want ok %v", err, c.ok)
			}
		})
	}
	if _, err := parsePHC(strings.Replace(phcString(19456, 2, 1, 16, 32), "v=19", "v=16", 1)); err == nil {
		t.Error("version 16 accepted")
	}
}

func TestPHC_Malformed(t *testing.T) {
	valid := phcString(19456, 2, 1, 16, 32)
	for _, s := range []string{
		"",
		"$argon2id$v=19$m=19456,t=2,p=1$" + b64(16),
		valid + "$extra",
		strings.Replace(valid, "$argon2id$", "$argon2i$", 1),
		strings.Replace(valid, "$argon2id$", "$argon2d$", 1),
		"$argon2id$v=19$m=19456,t=2,p=1$" + b64(16) + "=$" + b64(32),
		"$argon2id$v=19$m=19456,t=2,p=1$!!!!$" + b64(32),
		"$argon2id$v=19$m=abc,t=2,p=1$" + b64(16) + "$" + b64(32),
		"$argon2id$v=19$m=19456,t=2$" + b64(16) + "$" + b64(32),
		"$argon2id$v=19$m=19456,m=1,t=2,p=1$" + b64(16) + "$" + b64(32),
		"$argon2id$v=19$t=2,m=19456,p=1$" + b64(16) + "$" + b64(32),
		"$argon2id$v=19$m=019456,t=2,p=1$" + b64(16) + "$" + b64(32),
		"$argon2id$v=19$m=+19456,t=2,p=1$" + b64(16) + "$" + b64(32),
		"$argon2id$v=19$m=19456,t=2,p=1$" + b64(16) + "\n$" + b64(32),
	} {
		if _, err := parsePHC(s); err == nil {
			t.Errorf("parsePHC(%q) succeeded", s)
		}
	}
}

func TestBcryptPrefix(t *testing.T) {
	for s, want := range map[string]bool{
		"$2a$10$abc": true, "$2b$10$abc": true, "$2y$10$abc": true,
		"$2x$10$abc": false, "$1$abc": false, "$argon2id$v=19": false,
	} {
		if got := isBcrypt(s); got != want {
			t.Errorf("isBcrypt(%q) = %v, want %v", s, got, want)
		}
	}
}

func TestParams_Floor(t *testing.T) {
	for _, c := range []struct {
		p  Params
		ok bool
	}{
		{Params{7168, 5, 1}, true},
		{Params{19456, 2, 1}, true},
		{Params{47104, 1, 1}, true},
		{Params{7168, 4, 1}, false},
		{Params{7167, 10, 1}, false},
		{Params{19456, 2, 0}, false},
		{Params{19456, 2, 65}, false},
	} {
		if err := c.p.validate(); (err == nil) != c.ok {
			t.Errorf("%+v.validate() = %v, want ok %v", c.p, err, c.ok)
		}
	}
}

func TestOutdated(t *testing.T) {
	cur := Params{19456, 2, 1}
	base := phc{params: cur, salt: make([]byte, 16), key: make([]byte, 32)}
	if outdated(base, cur) {
		t.Error("current hash reported outdated")
	}
	for name, h := range map[string]phc{
		"memory":      {params: Params{7168, 2, 1}, salt: base.salt, key: base.key},
		"iterations":  {params: Params{19456, 3, 1}, salt: base.salt, key: base.key},
		"parallelism": {params: Params{19456, 2, 2}, salt: base.salt, key: base.key},
		"salt":        {params: cur, salt: make([]byte, 8), key: base.key},
		"key":         {params: cur, salt: base.salt, key: make([]byte, 16)},
	} {
		if !outdated(h, cur) {
			t.Errorf("%s change not reported outdated", name)
		}
	}
}

func FuzzPHC(f *testing.F) {
	f.Add(phcString(19456, 2, 1, 16, 32))
	f.Add(phcString(1, 1, 1, 8, 16))
	f.Add("$argon2id$v=19$m=0,t=0,p=0$$")
	f.Add("$2b$10$abcdefghijklmnopqrstuuabcdefghijklmnopqrstuvwxyz01234")
	f.Fuzz(func(t *testing.T, s string) {
		h, err := parsePHC(s)
		if err != nil {
			return
		}
		if got := encodePHC(h); got != s {
			t.Fatalf("accepted %q but re-encodes to %q", s, got)
		}
	})
}

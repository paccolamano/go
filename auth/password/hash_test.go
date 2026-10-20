package password_test

import (
	"encoding/base64"
	"errors"
	"strings"
	"testing"

	"github.com/paccolamano/go/auth/password"
)

func TestHash_Format(t *testing.T) {
	h, err := password.Hash("hunter2")
	if err != nil {
		t.Fatal(err)
	}
	const prefix = "$argon2id$v=19$m=19456,t=2,p=1$"
	if !strings.HasPrefix(h, prefix) {
		t.Fatalf("Hash = %q, want prefix %q", h, prefix)
	}
	parts := strings.Split(strings.TrimPrefix(h, prefix), "$")
	salt, err1 := base64.RawStdEncoding.DecodeString(parts[0])
	key, err2 := base64.RawStdEncoding.DecodeString(parts[1])
	if err1 != nil || err2 != nil || len(salt) != 16 || len(key) != 32 {
		t.Fatalf("salt %d bytes, key %d bytes (%v, %v); want 16 and 32", len(salt), len(key), err1, err2)
	}
}

func TestHash_Random(t *testing.T) {
	a, _ := password.Hash("hunter2")
	b, _ := password.Hash("hunter2")
	if a == b {
		t.Fatal("two hashes of the same password are equal")
	}
	acc := &accounts{m: map[string]password.Account{"a": {Subject: "a", Hash: a}, "b": {Subject: "b", Hash: b}}}
	c := mustChecker(t, acc.lookup)
	for _, u := range []string{"a", "b"} {
		if _, err := c.Check(t.Context(), u, "hunter2"); err != nil {
			t.Fatalf("hash %s does not verify: %v", u, err)
		}
	}
}

func TestHash_Errors(t *testing.T) {
	if _, err := password.Hash(""); !errors.Is(err, password.ErrEmpty) {
		t.Errorf("Hash(\"\") = %v, want ErrEmpty", err)
	}
	if _, err := password.Hash(strings.Repeat("x", 1025)); !errors.Is(err, password.ErrTooLong) {
		t.Errorf("Hash(1025 bytes) = %v, want ErrTooLong", err)
	}
	if _, err := password.Hash(strings.Repeat("x", 1024)); err != nil {
		t.Errorf("Hash(1024 bytes) = %v, want nil", err)
	}
}

func TestHash_CheckerParams(t *testing.T) {
	c := mustChecker(t, (&accounts{}).lookup, floor())
	if h := mustHash(t, c, "hunter2"); !strings.HasPrefix(h, "$argon2id$v=19$m=7168,t=5,p=1$") {
		t.Fatalf("Checker.Hash = %q", h)
	}
}

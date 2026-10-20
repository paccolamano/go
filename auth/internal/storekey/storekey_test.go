package storekey_test

import (
	"crypto/sha256"
	"encoding/base64"
	"regexp"
	"strings"
	"testing"

	"github.com/paccolamano/go/auth/internal/storekey"
)

func TestKey_Format(t *testing.T) {
	for _, kind := range []string{"session", "pending", "pending-attempts", "password-attempts", "totp-used", "recovery-used"} {
		t.Run(kind, func(t *testing.T) {
			k := storekey.Key(kind, "secret-input")
			re := regexp.MustCompile(`^auth:` + regexp.QuoteMeta(kind) + `:[A-Za-z0-9_-]{43}$`)
			if !re.MatchString(k) {
				t.Fatalf("Key = %q, want auth:%s:<43 base64url chars>", k, kind)
			}
			if n := len(k + ":123456789012"); n > 80 {
				t.Fatalf("key with step suffix is %d bytes, want <= 80", n)
			}
		})
	}
}

func TestKey_Digest(t *testing.T) {
	sum := sha256.Sum256([]byte("a\x00bc"))
	want := "auth:k:" + base64.RawURLEncoding.EncodeToString(sum[:])
	if got := storekey.Key("k", "a", "bc"); got != want {
		t.Fatalf("Key = %q, want %q", got, want)
	}
}

func TestKey_Properties(t *testing.T) {
	first, second := storekey.Key("k", "x"), storekey.Key("k", "x")
	if first != second {
		t.Fatal("Key is not deterministic")
	}
	if storekey.Key("k", "a", "bc") == storekey.Key("k", "ab", "c") {
		t.Fatal("parts are not separated")
	}
	if storekey.Key("k", "x") == storekey.Key("k", "y") {
		t.Fatal("different inputs give the same key")
	}
	secret := "hunter2-session-identifier"
	if k := storekey.Key("session", secret); strings.Contains(k, secret) {
		t.Fatalf("key %q contains its input", k)
	}
}

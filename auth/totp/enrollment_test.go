package totp_test

import (
	"bytes"
	"encoding/base32"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"testing"

	"github.com/paccolamano/go/auth/totp"
)

func TestEnrollment_Secret(t *testing.T) {
	e, err := totp.NewEnrollment("Acme", "alice@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if n := len(e.Secret()); n != 20 {
		t.Fatalf("secret is %d bytes, want 20", n)
	}
	e.Secret()[0] ^= 0xff
	other, _ := totp.NewEnrollment("Acme", "alice@example.com")
	if bytes.Equal(e.Secret(), other.Secret()) {
		t.Fatal("two enrollments share a secret")
	}
}

func TestEnrollment_URI(t *testing.T) {
	e, _ := totp.NewEnrollment("Acme Corp", "alice@example.com")
	u, err := url.Parse(e.URI())
	if err != nil {
		t.Fatal(err)
	}
	if u.Scheme != "otpauth" || u.Host != "totp" || u.Path != "/Acme Corp:alice@example.com" {
		t.Fatalf("URI = %s", e.URI())
	}
	if !strings.Contains(e.URI(), "/Acme%20Corp:") {
		t.Fatalf("label not escaped: %s", e.URI())
	}
	q := u.Query()
	want := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(e.Secret())
	if q.Get("secret") != want || q.Get("issuer") != "Acme Corp" || q.Get("algorithm") != "SHA1" || q.Get("digits") != "6" || q.Get("period") != "30" {
		t.Fatalf("query = %v", q)
	}
}

func TestEnrollment_Invalid(t *testing.T) {
	for _, c := range [][2]string{{"", "a"}, {"a", ""}, {"a:b", "c"}, {"a", "b:c"}} {
		if _, err := totp.NewEnrollment(c[0], c[1]); err == nil {
			t.Errorf("NewEnrollment(%q, %q) succeeded", c[0], c[1])
		}
	}
}

func TestEnrollment_Redacted(t *testing.T) {
	e, _ := totp.NewEnrollment("Acme", "alice")
	b32 := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(e.Secret())
	var buf bytes.Buffer
	slog.New(slog.NewJSONHandler(&buf, nil)).Info("enrolled", "e", e)
	for name, out := range map[string]string{"slog": buf.String(), "fmt": fmt.Sprint(e), "fmt %v": fmt.Sprintf("%v", e)} {
		if !strings.Contains(out, "[REDACTED]") || strings.Contains(out, b32) {
			t.Errorf("%s output %q leaks the secret", name, out)
		}
	}
}

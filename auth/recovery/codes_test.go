package recovery_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"testing"

	"github.com/paccolamano/go/auth/recovery"
)

func TestGenerate_Count(t *testing.T) {
	for _, n := range []int{0, 101, -1} {
		if _, err := recovery.Generate(n); err == nil {
			t.Errorf("Generate(%d) succeeded", n)
		}
	}
	if recovery.DefaultCount != 10 {
		t.Fatalf("DefaultCount = %d", recovery.DefaultCount)
	}
	if n := len(mustGenerate(t, recovery.DefaultCount).Plain()); n != 10 {
		t.Fatalf("Generate(DefaultCount) gave %d codes", n)
	}
}

func TestGenerate_Format(t *testing.T) {
	re := regexp.MustCompile(`^[0-9a-hjkmnp-tv-z]{4}(-[0-9a-hjkmnp-tv-z]{4}){3}$`)
	seen := map[string]bool{}
	for _, c := range mustGenerate(t, 100).Plain() {
		if !re.MatchString(c) {
			t.Fatalf("code %q has the wrong format", c)
		}
		if seen[c] {
			t.Fatalf("duplicate code %q", c)
		}
		seen[c] = true
	}
}

func TestGenerate_Hashes(t *testing.T) {
	c := mustGenerate(t, 10)
	for i, code := range c.Plain() {
		sum := sha256.Sum256([]byte(strings.ReplaceAll(code, "-", "")))
		if c.Hashes()[i] != hex.EncodeToString(sum[:]) {
			t.Fatalf("hash %d = %s", i, c.Hashes()[i])
		}
	}
}

func TestCodes_Redacted(t *testing.T) {
	c := mustGenerate(t, 3)
	var buf bytes.Buffer
	slog.New(slog.NewJSONHandler(&buf, nil)).Info("codes", "c", c)
	for name, out := range map[string]string{"slog": buf.String(), "fmt": fmt.Sprint(c)} {
		if !strings.Contains(out, "[REDACTED]") {
			t.Errorf("%s output %q is not redacted", name, out)
		}
		for _, code := range c.Plain() {
			if strings.Contains(out, code) {
				t.Errorf("%s output leaks %q", name, code)
			}
		}
	}
}

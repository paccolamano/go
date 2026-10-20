// Package recovery generates and verifies single-use recovery codes as a
// second factor, for users who lost their authenticator.
//
// Each code has 80 bits of entropy and is shown to the user once; the
// developer stores only its SHA-256 hash. A code is accepted at most once, even
// under parallel requests, and attempts count against the pending login's
// budget, shared with time-based codes. RFC 8176 has no value for recovery
// codes, so a successful code adds only "mfa" to the principal's methods.
package recovery

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"slices"

	"github.com/paccolamano/go/auth/internal/crockford"
)

// DefaultCount is the usual number of codes in a set.
const DefaultCount = 10

const (
	maxCount  = 100
	codeBytes = 10
	codeChars = 16
)

// Codes is a generated set of recovery codes. Show Plain once and store only
// Hashes. It renders as [REDACTED] in logs and with fmt.
type Codes struct {
	plain  []string
	hashes []string
}

// Generate returns n codes, 1 ≤ n ≤ 100, formatted as xxxx-xxxx-xxxx-xxxx with
// Crockford's base32 alphabet.
func Generate(n int) (*Codes, error) {
	if n < 1 || n > maxCount {
		return nil, fmt.Errorf("recovery: generate: n must be between 1 and %d", maxCount)
	}
	c := &Codes{}
	seen := make(map[string]bool, n)
	for len(c.plain) < n {
		b := make([]byte, codeBytes)
		_, _ = rand.Read(b) // never fails since Go 1.24
		s := crockford.Encode(b)
		if seen[s] {
			continue
		}
		seen[s] = true
		c.plain = append(c.plain, s[0:4]+"-"+s[4:8]+"-"+s[8:12]+"-"+s[12:16])
		c.hashes = append(c.hashes, hashCode(s))
	}
	return c, nil
}

// Plain returns the codes in clear, to show to the user once.
func (c *Codes) Plain() []string { return slices.Clone(c.plain) }

// Hashes returns the lowercase hex SHA-256 hashes of the codes, in the order
// of Plain: the only form to store.
func (c *Codes) Hashes() []string { return slices.Clone(c.hashes) }

// LogValue implements slog.LogValuer: a set of codes is logged as [REDACTED].
func (c *Codes) LogValue() slog.Value { return slog.StringValue("[REDACTED]") }

// String returns [REDACTED].
func (c *Codes) String() string { return "[REDACTED]" }

// normalize turns a code typed by a person into its 16-character canonical
// form.
func normalize(code string) (string, bool) {
	s := crockford.Normalize(code)
	if len(s) != codeChars {
		return "", false
	}
	if _, err := crockford.Decode(s); err != nil {
		return "", false
	}
	return s, true
}

func hashCode(canonical string) string {
	sum := sha256.Sum256([]byte(canonical))
	return hex.EncodeToString(sum[:])
}

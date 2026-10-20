// Package apikey generates API keys and authenticates requests that carry
// them, storing only a hash of the secret.
//
// A key has the form <prefix>_<id>_<secret>: the application's prefix, which
// makes keys recognizable to secret scanners, a public random identifier used
// by the lookup, and a 256-bit secret. The developer stores only the
// identifier and the SHA-256 of the secret, so a database backup cannot be
// used to call the service. With 256 bits of entropy a slow hash is not
// needed, and verification at every request stays cheap.
package apikey

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"log/slog"
	"strings"
	"time"

	"github.com/paccolamano/go/auth"
	"github.com/paccolamano/go/auth/internal/crockford"
)

const (
	idBytes     = 10
	secretBytes = 32
	idChars     = 16
	secretChars = 52
)

// ErrNotFound is returned by a LookupFunc when no key has the identifier, for
// example because it was deleted. It is auth.ErrNotFound.
var ErrNotFound = auth.ErrNotFound

// Key is a generated key in clear. Show it to its owner once and never store
// it. LogValue redacts it in structured logs; fmt and templates print it.
type Key string

// LogValue implements slog.LogValuer: a key is logged as [REDACTED].
func (Key) LogValue() slog.Value { return slog.StringValue("[REDACTED]") }

// Stored is what the developer saves for a generated key, together with its
// owner and an optional expiry.
type Stored struct {
	// ID is the public identifier, passed to the LookupFunc.
	ID string
	// Hash is the SHA-256 of the secret.
	Hash []byte
}

// Account is what a LookupFunc returns for an existing key.
type Account struct {
	// Subject is the key's owner. It must not be empty.
	Subject string
	// Hash is the stored Stored.Hash.
	Hash []byte
	// ExpiresAt is the instant the key stops working; zero means never.
	ExpiresAt time.Time
}

// LookupFunc reads a key by identifier from the developer's database. It
// returns ErrNotFound when no key has the identifier; any other error makes
// authentication fail with 500.
type LookupFunc func(ctx context.Context, id string) (Account, error)

func generate(prefix string) (Key, Stored) {
	id := make([]byte, idBytes)
	secret := make([]byte, secretBytes)
	_, _ = rand.Read(id) // never fails since Go 1.24
	_, _ = rand.Read(secret)
	sum := sha256.Sum256(secret)
	encodedID := crockford.Encode(id)
	return Key(prefix + "_" + encodedID + "_" + crockford.Encode(secret)), Stored{ID: encodedID, Hash: sum[:]}
}

// parse splits a credential that starts with "<prefix>_". ok is false when the
// rest is not a well-formed identifier and secret.
func parse(prefix, cred string) (id string, secret []byte, ok bool) {
	rest := strings.TrimPrefix(cred, prefix+"_")
	id, enc, found := strings.Cut(rest, "_")
	if !found || len(id) != idChars || len(enc) != secretChars {
		return "", nil, false
	}
	if _, err := crockford.Decode(id); err != nil {
		return "", nil, false
	}
	secret, err := crockford.Decode(enc)
	if err != nil || len(secret) != secretBytes {
		return "", nil, false
	}
	return id, secret, true
}

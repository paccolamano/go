// Package storekey derives store keys that never contain the secrets they are
// derived from.
package storekey

import (
	"crypto/sha256"
	"encoding/base64"
)

// Key returns "auth:<kind>:<digest>", where digest is the unpadded base64url
// encoding of the SHA-256 of parts joined by NUL bytes.
func Key(kind string, parts ...string) string {
	h := sha256.New()
	for i, p := range parts {
		if i > 0 {
			h.Write([]byte{0})
		}
		h.Write([]byte(p))
	}
	return "auth:" + kind + ":" + base64.RawURLEncoding.EncodeToString(h.Sum(nil))
}

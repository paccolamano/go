// Package hotp computes HOTP values (RFC 4226).
package hotp

import (
	"crypto/hmac"
	"crypto/sha1" // RFC 6238 requires HMAC-SHA-1; HMAC does not rely on SHA-1 collision resistance (RFC 6194)
	"crypto/sha256"
	"crypto/sha512"
	"encoding/binary"
	"fmt"
	"hash"
)

// Algorithm selects the HMAC hash function.
type Algorithm int

// The hash functions of RFC 6238.
const (
	SHA1 Algorithm = iota + 1
	SHA256
	SHA512
)

// Value returns the HOTP value of counter for secret, with digits decimal
// digits and leading zeros kept.
func Value(alg Algorithm, secret []byte, counter uint64, digits int) string {
	var h func() hash.Hash
	switch alg {
	case SHA256:
		h = sha256.New
	case SHA512:
		h = sha512.New
	default:
		h = sha1.New
	}
	var msg [8]byte
	binary.BigEndian.PutUint64(msg[:], counter)
	mac := hmac.New(h, secret)
	mac.Write(msg[:])
	sum := mac.Sum(nil)
	off := sum[len(sum)-1] & 0x0f
	v := uint64(binary.BigEndian.Uint32(sum[off:off+4]) & 0x7fffffff)
	mod := uint64(1)
	for range digits {
		mod *= 10
	}
	return fmt.Sprintf("%0*d", digits, v%mod)
}

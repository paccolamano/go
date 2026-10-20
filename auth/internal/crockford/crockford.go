// Package crockford encodes bytes with Crockford's base32 alphabet, in lower
// case and without padding, for API keys and recovery codes.
package crockford

import (
	"encoding/base32"
	"errors"
	"strings"
)

// alphabet omits i, l, o and u, which are easy to misread.
const alphabet = "0123456789abcdefghjkmnpqrstvwxyz"

var errInvalid = errors.New("crockford: invalid encoding")

func encoding() *base32.Encoding {
	return base32.NewEncoding(alphabet).WithPadding(base32.NoPadding)
}

// Encode returns the encoding of b.
func Encode(b []byte) string {
	return encoding().EncodeToString(b)
}

// Decode returns the bytes encoded by s. It accepts only the canonical
// encoding: lower-case alphabet characters, and zero padding bits.
func Decode(s string) ([]byte, error) {
	for i := range len(s) {
		if strings.IndexByte(alphabet, s[i]) < 0 {
			return nil, errInvalid
		}
	}
	b, err := encoding().DecodeString(s)
	if err != nil || Encode(b) != s {
		return nil, errInvalid
	}
	return b, nil
}

// Normalize prepares a code typed by a person for Decode: it lower-cases it,
// removes spaces and hyphens, and maps i and l to 1 and o to 0.
func Normalize(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range strings.ToLower(s) {
		switch r {
		case ' ', '-':
		case 'i', 'l':
			b.WriteByte('1')
		case 'o':
			b.WriteByte('0')
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

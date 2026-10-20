package password

import (
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

var errMalformed = errors.New("password: malformed or out-of-bounds stored hash")

// phc is a parsed argon2id hash; the version is always 19.
type phc struct {
	params Params
	salt   []byte
	key    []byte
}

func encodePHC(h phc) string {
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s", argonVersion,
		h.params.Memory, h.params.Iterations, h.params.Parallelism,
		base64.RawStdEncoding.EncodeToString(h.salt), base64.RawStdEncoding.EncodeToString(h.key))
}

// parsePHC parses an argon2id PHC string. It accepts only the canonical
// encoding and parameters within the stored-hash bounds.
func parsePHC(s string) (phc, error) {
	parts := strings.Split(s, "$")
	if len(parts) != 6 || parts[0] != "" || parts[1] != "argon2id" || parts[2] != "v=19" {
		return phc{}, errMalformed
	}
	fields := strings.Split(parts[3], ",")
	if len(fields) != 3 {
		return phc{}, errMalformed
	}
	var vals [3]uint64
	for i, name := range []string{"m=", "t=", "p="} {
		v, ok := strings.CutPrefix(fields[i], name)
		if !ok {
			return phc{}, errMalformed
		}
		n, err := strconv.ParseUint(v, 10, 32)
		if err != nil {
			return phc{}, errMalformed
		}
		vals[i] = n
	}
	if vals[2] > maxParallelism {
		return phc{}, errMalformed
	}
	salt, err1 := base64.RawStdEncoding.Strict().DecodeString(parts[4])
	key, err2 := base64.RawStdEncoding.Strict().DecodeString(parts[5])
	if err1 != nil || err2 != nil {
		return phc{}, errMalformed
	}
	h := phc{
		params: Params{Memory: uint32(vals[0]), Iterations: uint32(vals[1]), Parallelism: uint8(vals[2])},
		salt:   salt,
		key:    key,
	}
	if !h.params.stored() || len(salt) < minSalt || len(salt) > maxSalt || len(key) < minKey || len(key) > maxKey {
		return phc{}, errMalformed
	}
	// Reject non-canonical forms, such as leading zeros or line breaks, which
	// the decoders above tolerate.
	if encodePHC(h) != s {
		return phc{}, errMalformed
	}
	return h, nil
}

// isBcrypt reports whether s has a bcrypt prefix.
func isBcrypt(s string) bool {
	for _, p := range []string{"$2a$", "$2b$", "$2y$"} {
		if strings.HasPrefix(s, p) {
			return true
		}
	}
	return false
}

// outdated reports whether an argon2id hash differs from the current
// parameters, salt length or key length.
func outdated(h phc, cur Params) bool {
	return h.params != cur || len(h.salt) != saltLen || len(h.key) != keyLen
}

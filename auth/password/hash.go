package password

import (
	"crypto/rand"
	"errors"

	"golang.org/x/crypto/argon2"

	"github.com/paccolamano/go/auth"
)

var (
	// ErrNotFound is returned by a LookupFunc when the account does not exist.
	// It is auth.ErrNotFound.
	ErrNotFound = auth.ErrNotFound
	// ErrEmpty is returned by Hash for an empty password.
	ErrEmpty = errors.New("password: empty")
	// ErrTooLong is returned by Hash for a password over the maximum length.
	ErrTooLong = errors.New("password: too long")
)

// defaultMaxLength is the default maximum password length in bytes.
const defaultMaxLength = 1024

// Hash returns an argon2id hash of plain in PHC string format, with a random
// salt and DefaultParams. It returns ErrEmpty for an empty password and
// ErrTooLong for a password over 1,024 bytes. Use Checker.Hash when the
// checker is configured with other parameters.
func Hash(plain string) (string, error) {
	return hashWith(plain, DefaultParams(), defaultMaxLength)
}

func hashWith(plain string, p Params, maxLength int) (string, error) {
	switch {
	case plain == "":
		return "", ErrEmpty
	case len(plain) > maxLength:
		return "", ErrTooLong
	}
	salt := make([]byte, saltLen)
	_, _ = rand.Read(salt) // never fails since Go 1.24
	key := argon2.IDKey([]byte(plain), salt, p.Iterations, p.Memory, p.Parallelism, keyLen)
	return encodePHC(phc{params: p, salt: salt, key: key}), nil
}

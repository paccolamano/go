package password

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/bcrypt"

	"github.com/paccolamano/go/auth"
	"github.com/paccolamano/go/auth/internal/storekey"
)

const methodPassword = "pwd"

// bcryptMaxLength is the number of password bytes bcrypt uses.
const bcryptMaxLength = 72

// Checker verifies usernames and passwords. It is safe for concurrent use,
// and it satisfies basic.Checker.
type Checker struct {
	lookup    LookupFunc
	params    Params
	maxLength int
	rehash    RehashFunc
	limit     *AttemptLimit
	dummy     phc
}

// NewChecker returns a Checker that reads accounts through lookup. It computes
// a dummy hash with the checker's parameters, verified for nonexistent
// usernames so that they cost the same as a wrong password.
func NewChecker(lookup LookupFunc, opts ...Option) (*Checker, error) {
	if lookup == nil {
		return nil, fmt.Errorf("%w: password.NewChecker: lookup: must not be nil", auth.ErrInvalidConfig)
	}
	cfg := config{params: DefaultParams(), maxLength: defaultMaxLength}
	for _, o := range opts {
		if o == nil {
			return nil, fmt.Errorf("%w: password.NewChecker: options: must not be nil", auth.ErrInvalidConfig)
		}
		o(&cfg)
	}
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	c := &Checker{lookup: lookup, params: cfg.params, maxLength: cfg.maxLength, rehash: cfg.rehash, limit: cfg.limit}
	salt := make([]byte, saltLen)
	_, _ = rand.Read(salt) // never fails since Go 1.24
	c.dummy = phc{params: c.params, salt: salt, key: argon2.IDKey(salt, salt, c.params.Iterations, c.params.Memory, c.params.Parallelism, keyLen)}
	return c, nil
}

// Hash returns an argon2id hash of plain with the checker's parameters and
// maximum length.
func (c *Checker) Hash(plain string) (string, error) {
	return hashWith(plain, c.params, c.maxLength)
}

// Check verifies username and plain. On success it returns a principal with
// the account's subject and the methods ["pwd"]. It returns an error matching
// auth.ErrInvalidCredentials for a wrong password, an unknown username, or a
// password that is empty or over the maximum length; auth.ErrTooManyAttempts
// when the attempt limit blocks the username; and any other error when the
// lookup or the store fails or the stored hash is malformed.
func (c *Checker) Check(ctx context.Context, username, plain string) (*auth.Principal, error) {
	if username == "" || plain == "" || len(plain) > c.maxLength {
		return nil, auth.ErrInvalidCredentials
	}
	acct, err := c.lookup(ctx, username)
	found := true
	switch {
	case errors.Is(err, auth.ErrNotFound):
		found = false
	case err != nil:
		return nil, fmt.Errorf("password: lookup: %w", err)
	case acct.Subject == "":
		return nil, errors.New("password: lookup returned an account without a subject")
	}

	var counter string
	if c.limit != nil {
		counter = storekey.Key("password-attempts", username)
		n, err := c.limit.Store.Incr(ctx, counter, c.limit.Window)
		if err != nil {
			return nil, fmt.Errorf("password: count attempt: %w", err)
		}
		if n > int64(c.limit.Max) {
			return nil, auth.ErrTooManyAttempts
		}
	}

	if !found {
		verifyArgon(c.dummy, plain)
		return nil, auth.ErrInvalidCredentials
	}
	ok, stale, err := c.verify(acct.Hash, plain)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, auth.ErrInvalidCredentials
	}

	if c.limit != nil {
		if err := c.limit.Store.Delete(ctx, counter); err != nil {
			return nil, fmt.Errorf("password: clear attempts: %w", err)
		}
	}
	if stale && c.rehash != nil {
		h, err := c.Hash(plain)
		if err != nil {
			return nil, err
		}
		c.rehash(ctx, acct.Subject, h)
	}
	return &auth.Principal{Subject: acct.Subject, Methods: []string{methodPassword}}, nil
}

// verify checks plain against a stored hash. stale reports a hash that should
// be replaced with one using the current parameters.
func (c *Checker) verify(stored, plain string) (ok, stale bool, err error) {
	if isBcrypt(stored) {
		err := bcrypt.CompareHashAndPassword([]byte(stored), []byte(plain))
		switch {
		case err == nil:
			// bcrypt ignores everything after the 72nd byte: never match a
			// longer password, after the same work as any other comparison.
			return len(plain) <= bcryptMaxLength, true, nil
		case errors.Is(err, bcrypt.ErrMismatchedHashAndPassword):
			return false, true, nil
		default:
			return false, false, errMalformed
		}
	}
	if !strings.HasPrefix(stored, "$argon2id$") {
		return false, false, errMalformed
	}
	h, err := parsePHC(stored)
	if err != nil {
		return false, false, err
	}
	return verifyArgon(h, plain), outdated(h, c.params), nil
}

// verifyArgon derives the key of plain with h's parameters and salt, and
// compares it with h's key in constant time.
func verifyArgon(h phc, plain string) bool {
	key := argon2.IDKey([]byte(plain), h.salt, h.params.Iterations, h.params.Memory, h.params.Parallelism, uint32(len(h.key)))
	return subtle.ConstantTimeCompare(key, h.key) == 1
}

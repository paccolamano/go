package password

import (
	"context"
	"fmt"
	"time"

	"github.com/paccolamano/go/auth"
)

// Account is what a LookupFunc returns for an existing account.
type Account struct {
	// Subject is the stable identifier of the account. It must not be empty.
	Subject string
	// Hash is the stored hash: an argon2id PHC string or a bcrypt hash.
	Hash string
}

// LookupFunc reads an account from the developer's database. It returns
// ErrNotFound when the account does not exist; any other error makes Check
// fail with that error, answered with 500 and never with 401.
type LookupFunc func(ctx context.Context, username string) (Account, error)

// RehashFunc stores a new hash computed after a successful check of an
// outdated hash. It cannot fail the login: it handles its own errors.
type RehashFunc func(ctx context.Context, subject, hash string)

// AttemptLimit configures the per-username limit on failed attempts.
type AttemptLimit struct {
	// Store holds the counters. It is required.
	Store auth.Store
	// Max is the number of failures allowed per window; 0 means 10.
	Max int
	// Window is the time after which the count starts again; 0 means 15
	// minutes.
	Window time.Duration
}

const (
	defaultMaxAttempts = 10
	defaultWindow      = 15 * time.Minute
	minMaxLength       = 256
	maxMaxLength       = 4096
)

// Option configures NewChecker.
type Option func(*config)

type config struct {
	params    Params
	maxLength int
	rehash    RehashFunc
	rehashSet bool
	limit     *AttemptLimit
}

// WithParams sets the argon2id parameters of new hashes and of the dummy hash.
// Parameters below the floor are rejected: memory of at least 7,168 KiB, and
// memory × iterations of at least 35,840 KiB.
func WithParams(p Params) Option {
	return func(c *config) { c.params = p }
}

// WithMaxLength sets the maximum password length in bytes, from 256 to 4,096;
// the default is 1,024. Longer passwords are rejected, never truncated. Every
// allowed value accepts 64 characters in any script.
func WithMaxLength(n int) Option {
	return func(c *config) { c.maxLength = n }
}

// WithRehash sets the function that stores a new hash after a successful check
// of a hash that uses bcrypt or outdated argon2id parameters.
func WithRehash(fn RehashFunc) Option {
	return func(c *config) {
		c.rehash = fn
		c.rehashSet = true
	}
}

// WithAttemptLimit enables a limit on failed attempts per username, counted in
// l.Store. Once the limit is reached within the window, Check rejects every
// attempt for that username with auth.ErrTooManyAttempts, without verifying
// the password, until the window ends. Nonexistent usernames are counted too,
// so the limit reveals nothing about which accounts exist. A successful check
// clears the count. Pass usernames in their canonical form, for example
// lower-cased, when the lookup ignores case.
func WithAttemptLimit(l AttemptLimit) Option {
	return func(c *config) { c.limit = &l }
}

func (c *config) validate() error {
	if err := c.params.validate(); err != nil {
		return err
	}
	if c.maxLength < minMaxLength || c.maxLength > maxMaxLength {
		return fmt.Errorf("%w: password.NewChecker: WithMaxLength: must be between %d and %d bytes", auth.ErrInvalidConfig, minMaxLength, maxMaxLength)
	}
	if c.rehashSet && c.rehash == nil {
		return fmt.Errorf("%w: password.NewChecker: WithRehash: the function must not be nil", auth.ErrInvalidConfig)
	}
	if l := c.limit; l != nil {
		switch {
		case l.Store == nil:
			return fmt.Errorf("%w: password.NewChecker: WithAttemptLimit: Store must not be nil", auth.ErrInvalidConfig)
		case l.Max < 0 || l.Window < 0:
			return fmt.Errorf("%w: password.NewChecker: WithAttemptLimit: Max and Window must not be negative", auth.ErrInvalidConfig)
		}
		if l.Max == 0 {
			l.Max = defaultMaxAttempts
		}
		if l.Window == 0 {
			l.Window = defaultWindow
		}
	}
	return nil
}

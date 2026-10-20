package totp

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"time"

	"github.com/paccolamano/go/auth"
	"github.com/paccolamano/go/auth/internal/hotp"
	"github.com/paccolamano/go/auth/internal/storekey"
)

const (
	digits    = 6
	period    = 30
	replayTTL = 2 * time.Minute
	minSecret = 16
)

// ErrNotFound is returned by a LookupFunc when the subject has no active
// secret, that is, the second factor is not enabled. It is auth.ErrNotFound.
var ErrNotFound = auth.ErrNotFound

// LookupFunc reads the active secret of a subject from the developer's
// database. It returns ErrNotFound when the subject is not enrolled.
type LookupFunc func(ctx context.Context, subject string) ([]byte, error)

// Verifier verifies time-based codes. It is safe for concurrent use.
type Verifier struct {
	lookup LookupFunc
	store  auth.Store
}

// New returns a Verifier that reads secrets through lookup and records used
// codes in store.
func New(lookup LookupFunc, store auth.Store) (*Verifier, error) {
	switch {
	case lookup == nil:
		return nil, fmt.Errorf("%w: totp.New: lookup: must not be nil", auth.ErrInvalidConfig)
	case store == nil:
		return nil, fmt.Errorf("%w: totp.New: store: must not be nil", auth.ErrInvalidConfig)
	}
	return &Verifier{lookup: lookup, store: store}, nil
}

// Enrolled reports whether subject has an active secret.
func (v *Verifier) Enrolled(ctx context.Context, subject string) (bool, error) {
	_, err := v.lookup(ctx, subject)
	switch {
	case errors.Is(err, auth.ErrNotFound):
		return false, nil
	case err != nil:
		return false, fmt.Errorf("totp: lookup: %w", err)
	}
	return true, nil
}

// Verify checks code as the second factor of pending. On success it returns a
// principal with the pending methods plus "otp" and "mfa". A wrong, malformed
// or replayed code returns an error matching auth.ErrInvalidCredentials and
// counts as an attempt; the fifth failure also matches
// auth.ErrTooManyAttempts and cancels the pending login. A subject without a
// secret returns an error matching auth.ErrNotFound, not counted.
func (v *Verifier) Verify(ctx context.Context, pending auth.Pending, code string) (*auth.Principal, error) {
	pp := pending.Principal()
	secret, err := v.lookup(ctx, pp.Subject)
	switch {
	case errors.Is(err, auth.ErrNotFound):
		return nil, fmt.Errorf("totp: subject is not enrolled: %w", err)
	case err != nil:
		return nil, fmt.Errorf("totp: lookup: %w", err)
	}
	if err := pending.Attempt(ctx, func(ctx context.Context) error {
		return v.check(ctx, pp.Subject, secret, code)
	}); err != nil {
		return nil, fmt.Errorf("totp: %w", err)
	}
	return &auth.Principal{Subject: pp.Subject, Methods: addMethods(pp.Methods, "otp", "mfa")}, nil
}

// Confirm checks the first code of a candidate secret during enrollment, with
// replay protection and no attempt budget. Store the secret as active only
// when Confirm returns nil.
func (v *Verifier) Confirm(ctx context.Context, subject string, secret []byte, code string) error {
	if len(secret) < minSecret {
		return errors.New("totp: confirm: the secret is shorter than 128 bits")
	}
	return v.check(ctx, subject, secret, code)
}

// check accepts code when it matches the step before, at or after the current
// one, and that step was not used by subject before.
func (v *Verifier) check(ctx context.Context, subject string, secret []byte, code string) error {
	if !sixDigits(code) {
		return auth.ErrInvalidCredentials
	}
	now := time.Now().Unix() / period
	matched := int64(-1)
	for step := now - 1; step <= now+1; step++ {
		want := hotp.Value(hotp.SHA1, secret, uint64(step), digits)
		if subtle.ConstantTimeCompare([]byte(want), []byte(code)) == 1 && matched < 0 {
			matched = step
		}
	}
	if matched < 0 {
		return auth.ErrInvalidCredentials
	}
	n, err := v.store.Incr(ctx, storekey.Key("totp-used", subject)+":"+strconv.FormatInt(matched, 10), replayTTL)
	if err != nil {
		return fmt.Errorf("totp: record code: %w", err)
	}
	if n > 1 {
		return auth.ErrInvalidCredentials
	}
	return nil
}

func sixDigits(s string) bool {
	if len(s) != digits {
		return false
	}
	for i := range len(s) {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// addMethods returns base followed by the methods it lacks.
func addMethods(base []string, add ...string) []string {
	out := slices.Clone(base)
	for _, m := range add {
		if !slices.Contains(out, m) {
			out = append(out, m)
		}
	}
	return out
}

package recovery

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/paccolamano/go/auth"
	"github.com/paccolamano/go/auth/internal/storekey"
)

// guardTTL bounds the window in which parallel requests could both see a code
// as unused.
const guardTTL = 10 * time.Minute

// ErrNotFound is returned by a ConsumeFunc when the code was already used, and
// by a LookupFunc when the subject has no codes. It is auth.ErrNotFound.
var ErrNotFound = auth.ErrNotFound

// LookupFunc returns the hashes of the subject's unused codes.
type LookupFunc func(ctx context.Context, subject string) ([]string, error)

// ConsumeFunc marks a code, identified by its hash, as used. It returns
// ErrNotFound when the code was already used.
type ConsumeFunc func(ctx context.Context, subject, hash string) error

// Verifier verifies recovery codes. It is safe for concurrent use.
type Verifier struct {
	lookup  LookupFunc
	consume ConsumeFunc
	store   auth.Store
}

// New returns a Verifier that reads hashes through lookup, marks codes as used
// through consume, and guards against parallel use in store.
func New(lookup LookupFunc, consume ConsumeFunc, store auth.Store) (*Verifier, error) {
	switch {
	case lookup == nil:
		return nil, fmt.Errorf("%w: recovery.New: lookup: must not be nil", auth.ErrInvalidConfig)
	case consume == nil:
		return nil, fmt.Errorf("%w: recovery.New: consume: must not be nil", auth.ErrInvalidConfig)
	case store == nil:
		return nil, fmt.Errorf("%w: recovery.New: store: must not be nil", auth.ErrInvalidConfig)
	}
	return &Verifier{lookup: lookup, consume: consume, store: store}, nil
}

// Verify checks code as the second factor of pending. Codes are compared
// without regard to case, spaces and hyphens. On success it returns a
// principal with the pending methods plus "mfa". A wrong, malformed or used
// code returns an error matching auth.ErrInvalidCredentials and counts as an
// attempt; the fifth failure also matches auth.ErrTooManyAttempts.
func (v *Verifier) Verify(ctx context.Context, pending auth.Pending, code string) (*auth.Principal, error) {
	pp := pending.Principal()
	hashes, err := v.lookup(ctx, pp.Subject)
	if err != nil && !errors.Is(err, auth.ErrNotFound) {
		return nil, fmt.Errorf("recovery: lookup: %w", err)
	}
	if err := pending.Attempt(ctx, func(ctx context.Context) error {
		return v.check(ctx, pp.Subject, hashes, code)
	}); err != nil {
		return nil, fmt.Errorf("recovery: %w", err)
	}
	methods := slices.Clone(pp.Methods)
	if !slices.Contains(methods, "mfa") {
		methods = append(methods, "mfa")
	}
	return &auth.Principal{Subject: pp.Subject, Methods: methods}, nil
}

func (v *Verifier) check(ctx context.Context, subject string, hashes []string, code string) error {
	canonical, ok := normalize(code)
	if !ok {
		return auth.ErrInvalidCredentials
	}
	h := hashCode(canonical)
	found := false
	for _, stored := range hashes {
		if subtle.ConstantTimeCompare([]byte(stored), []byte(h)) == 1 {
			found = true
		}
	}
	if !found {
		return auth.ErrInvalidCredentials
	}
	n, err := v.store.Incr(ctx, storekey.Key("recovery-used", subject, h), guardTTL)
	if err != nil {
		return fmt.Errorf("recovery: guard: %w", err)
	}
	if n > 1 {
		return auth.ErrInvalidCredentials
	}
	err = v.consume(ctx, subject, h)
	switch {
	case errors.Is(err, auth.ErrNotFound):
		return auth.ErrInvalidCredentials
	case err != nil:
		return fmt.Errorf("recovery: consume: %w", err)
	}
	return nil
}

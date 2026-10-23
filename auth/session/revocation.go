package session

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/paccolamano/go/auth"
	"github.com/paccolamano/go/auth/internal/storekey"
)

// revocationMargin is how much longer than the longest session or pending
// login a revocation state is kept: it covers an issuance in flight during
// the revocation, and clock differences between instances.
const revocationMargin = time.Hour

// RevokeSubject revokes every session and pending login of subject, on every
// instance that shares the store: for example after a password change, when
// an account is disabled, or to log out of every device. Once it returns,
// requests that carry them get auth.ErrNoCredentials. Sessions and pending
// logins issued afterwards are not affected, so a handler that changes a
// password can call RevokeSubject and then Issue to keep its user signed in.
// RevokeSubject clears no cookie.
//
// A session issued after RevokeSubject returns is valid even when its
// credentials were checked before: call Issue right after checking them.
//
// It returns an error for an empty subject and for a store failure. Calling
// it again is harmless.
func (m *Manager) RevokeSubject(ctx context.Context, subject string) error {
	if subject == "" {
		return errors.New("session: revoke subject: the subject is empty")
	}
	if err := m.store.Set(ctx, revocationKey(subject), []byte(newID()), m.revocationTTL); err != nil {
		return fmt.Errorf("session: revoke subject: %w", err)
	}
	return nil
}

// generation returns the revocation state of subject: the value written by its
// last revocation, or "" when it was never revoked or that state expired.
func (m *Manager) generation(ctx context.Context, subject string) (string, error) {
	b, err := m.store.Get(ctx, revocationKey(subject))
	switch {
	case errors.Is(err, auth.ErrNotFound):
		return "", nil
	case err != nil:
		return "", fmt.Errorf("read revocation state: %w", err)
	}
	return string(b), nil
}

// revoked reports whether subject was revoked after a record observed gen.
func (m *Manager) revoked(ctx context.Context, subject, gen string) (bool, error) {
	cur, err := m.generation(ctx, subject)
	if err != nil {
		return false, err
	}
	return cur != "" && cur != gen, nil
}

func revocationKey(subject string) string { return storekey.Key("session-gen", subject) }

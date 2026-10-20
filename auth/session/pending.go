package session

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"time"

	"github.com/paccolamano/go/auth"
	"github.com/paccolamano/go/auth/internal/storekey"
)

// maxAttempts is the second-factor budget of a pending login, shared by
// time-based and recovery codes.
const maxAttempts = 5

// IssuePending starts a pending login for p, a principal that passed its first
// factor and must pass a second one. The pending login has its own cookie,
// lives for the pending lifetime and never authenticates a request. It
// replaces any pending login the request carried.
func (m *Manager) IssuePending(w http.ResponseWriter, r *http.Request, p *auth.Principal) error {
	if p == nil || p.Subject == "" {
		return errors.New("session: issue pending: the principal has no subject")
	}
	ctx := r.Context()
	if id, ok := readID(r, m.pendingCookie); ok {
		if err := m.store.Delete(ctx, storekey.Key("pending", id)); err != nil {
			return fmt.Errorf("session: issue pending: %w", err)
		}
	}
	id := newID()
	b, err := encodeRecord(pendingRecord{V: recordVersion, Sub: p.Subject, AMR: slices.Clone(p.Methods), IAT: time.Now().Unix()})
	if err != nil {
		return err
	}
	if err := m.store.Set(ctx, storekey.Key("pending", id), b, m.pendingLifetime); err != nil {
		return fmt.Errorf("session: issue pending: %w", err)
	}
	setCookie(w, m.pendingCookie, id, m.pendingLifetime)
	return nil
}

// Pending returns the pending login of r. It returns auth.ErrNoCredentials
// when the request carries none, or when it expired or was cancelled.
func (m *Manager) Pending(r *http.Request) (auth.Pending, error) {
	id, ok := readID(r, m.pendingCookie)
	if !ok {
		return nil, auth.ErrNoCredentials
	}
	b, err := m.store.Get(r.Context(), storekey.Key("pending", id))
	switch {
	case errors.Is(err, auth.ErrNotFound):
		return nil, auth.ErrNoCredentials
	case err != nil:
		return nil, fmt.Errorf("session: read pending login: %w", err)
	}
	var rec pendingRecord
	if err := decodeRecord(b, &rec); err != nil {
		return nil, err
	}
	if !time.Now().Before(time.Unix(rec.IAT, 0).Add(m.pendingLifetime)) {
		return nil, auth.ErrNoCredentials
	}
	return &pending{m: m, id: id, p: &auth.Principal{Subject: rec.Sub, Methods: rec.AMR}}, nil
}

// pending implements auth.Pending.
type pending struct {
	m  *Manager
	id string
	p  *auth.Principal
}

// Principal returns the principal of the first factor.
func (p *pending) Principal() *auth.Principal { return p.p }

// Attempt runs verify as one of the five second-factor attempts of the pending
// login (auth.Pending).
func (p *pending) Attempt(ctx context.Context, verify func(context.Context) error) error {
	n, err := p.m.store.Incr(ctx, storekey.Key("pending-attempts", p.id), p.m.pendingLifetime)
	if err != nil {
		return fmt.Errorf("session: count attempt: %w", err)
	}
	if n > maxAttempts {
		return errors.Join(auth.ErrTooManyAttempts, p.cancel(ctx))
	}
	err = verify(ctx)
	if errors.Is(err, auth.ErrInvalidCredentials) && n == maxAttempts {
		return errors.Join(fmt.Errorf("%w: %w", auth.ErrTooManyAttempts, err), p.cancel(ctx))
	}
	return err
}

// cancel deletes the pending login.
func (p *pending) cancel(ctx context.Context) error {
	if err := p.m.store.Delete(ctx, storekey.Key("pending", p.id)); err != nil {
		return fmt.Errorf("session: cancel pending login: %w", err)
	}
	return nil
}

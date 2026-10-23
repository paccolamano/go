package session

import (
	"errors"
	"fmt"
	"net/http"
	"slices"
	"time"

	"github.com/paccolamano/go/auth"
	"github.com/paccolamano/go/auth/internal/storekey"
)

// Issue starts a session for p. It deletes the session and the pending login
// the request carried, writes a new session under a new identifier, sets the
// session cookie and clears the pending cookie. The session records the
// revocation state of p.Subject: a RevokeSubject that runs while Issue writes
// it revokes it. On a store failure it returns an error and sets no cookie.
func (m *Manager) Issue(w http.ResponseWriter, r *http.Request, p *auth.Principal) error {
	if p == nil || p.Subject == "" {
		return errors.New("session: issue: the principal has no subject")
	}
	if err := m.deleteCarried(r); err != nil {
		return err
	}
	gen, err := m.generation(r.Context(), p.Subject)
	if err != nil {
		return fmt.Errorf("session: issue: %w", err)
	}
	id := newID()
	now := time.Now()
	rec := sessionRecord{
		V:   recordVersion,
		Sub: p.Subject,
		AMR: slices.Clone(p.Methods),
		IAT: now.Unix(),
		EXP: now.Add(m.lifetime).Unix(),
		RW:  now.Unix(),
		Gen: gen,
	}
	b, err := encodeRecord(rec)
	if err != nil {
		return err
	}
	if err := m.store.Set(r.Context(), storekey.Key("session", id), b, m.ttl(rec, now)); err != nil {
		return fmt.Errorf("session: issue: %w", err)
	}
	setCookie(w, m.cookie, id, m.lifetime)
	clearCookie(w, m.pendingCookie)
	return nil
}

// Revoke deletes the session and the pending login of r and clears both
// cookies. The cookies are cleared even when a delete fails; the error is then
// returned. Revoking a request without a session is not an error.
func (m *Manager) Revoke(w http.ResponseWriter, r *http.Request) error {
	err := m.deleteCarried(r)
	clearCookie(w, m.cookie)
	clearCookie(w, m.pendingCookie)
	if err != nil {
		return fmt.Errorf("session: revoke: %w", err)
	}
	return nil
}

// deleteCarried deletes the session and the pending login identified by the
// request's cookies.
func (m *Manager) deleteCarried(r *http.Request) error {
	var errs []error
	if id, ok := readID(r, m.cookie); ok {
		if err := m.store.Delete(r.Context(), storekey.Key("session", id)); err != nil {
			errs = append(errs, fmt.Errorf("session: delete session: %w", err))
		}
	}
	if id, ok := readID(r, m.pendingCookie); ok {
		if err := m.store.Delete(r.Context(), storekey.Key("pending", id)); err != nil {
			errs = append(errs, fmt.Errorf("session: delete pending login: %w", err))
		}
	}
	return errors.Join(errs...)
}

func setCookie(w http.ResponseWriter, name, value string, maxAge time.Duration) {
	http.SetCookie(w, &http.Cookie{
		Name:     name,
		Value:    value,
		Path:     "/",
		MaxAge:   int(maxAge / time.Second),
		Secure:   true,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
}

func clearCookie(w http.ResponseWriter, name string) {
	http.SetCookie(w, &http.Cookie{
		Name:     name,
		Path:     "/",
		MaxAge:   -1,
		Secure:   true,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
}

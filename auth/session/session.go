// Package session keeps signed-in users in server-side sessions carried by a
// __Host- cookie.
//
// Session identifiers have 256 bits from crypto/rand; the store holds only
// their SHA-256 hash. A session expires after an absolute lifetime and after an
// idle timeout; activity renews it at most once every quarter of the idle
// timeout, so most requests cost two store reads, the session and the
// revocation state of its subject, and no write. Issue always creates a new
// identifier and deletes the session and pending login the request carried,
// which prevents session fixation. RevokeSubject ends every session and
// pending login of a subject, for example after a password change.
//
// Cookie sessions need protection against cross-site request forgery: wrap the
// routes that change state with http.CrossOriginProtection.
package session

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/paccolamano/go/auth"
	"github.com/paccolamano/go/auth/internal/authheader"
	"github.com/paccolamano/go/auth/internal/storekey"
)

const (
	defaultCookieName      = "session"
	defaultLifetime        = 8 * time.Hour
	defaultIdleTimeout     = 30 * time.Minute
	defaultPendingLifetime = 5 * time.Minute
	maxPendingLifetime     = time.Hour
	maxCookieName          = 64
	cookiePrefix           = "__Host-"
	idBytes                = 32
)

// Option configures New.
type Option func(*config)

type config struct {
	name            string
	lifetime        time.Duration
	idle            time.Duration
	pendingLifetime time.Duration
}

// WithCookieName sets the base name of the cookies; the default is "session".
// The session cookie is "__Host-<name>" and the pending login cookie
// "__Host-<name>-pending". name must be an HTTP token of at most 64 bytes.
func WithCookieName(name string) Option {
	return func(c *config) { c.name = name }
}

// WithLifetime sets the absolute lifetime of a session; the default is 8
// hours.
func WithLifetime(d time.Duration) Option {
	return func(c *config) { c.lifetime = d }
}

// WithIdleTimeout sets the time after which an unused session expires; the
// default is 30 minutes. It must not exceed the lifetime.
func WithIdleTimeout(d time.Duration) Option {
	return func(c *config) { c.idle = d }
}

// WithPendingLifetime sets the lifetime of a pending login; the default is 5
// minutes, and the maximum one hour.
func WithPendingLifetime(d time.Duration) Option {
	return func(c *config) { c.pendingLifetime = d }
}

// Manager issues, authenticates and revokes sessions and pending logins. It
// implements auth.Authenticator and auth.Challenger, and is safe for
// concurrent use. Its methods take their context from the request.
type Manager struct {
	store           auth.Store
	cookie          string
	pendingCookie   string
	lifetime        time.Duration
	idle            time.Duration
	pendingLifetime time.Duration
	revocationTTL   time.Duration
}

// New returns a Manager that keeps sessions in store.
func New(store auth.Store, opts ...Option) (*Manager, error) {
	if store == nil {
		return nil, fmt.Errorf("%w: session.New: store: must not be nil", auth.ErrInvalidConfig)
	}
	cfg := config{name: defaultCookieName, lifetime: defaultLifetime, idle: defaultIdleTimeout, pendingLifetime: defaultPendingLifetime}
	for _, o := range opts {
		if o == nil {
			return nil, fmt.Errorf("%w: session.New: options: must not be nil", auth.ErrInvalidConfig)
		}
		o(&cfg)
	}
	switch {
	case !isToken(cfg.name) || len(cfg.name) > maxCookieName:
		return nil, fmt.Errorf("%w: session.New: WithCookieName: must be an HTTP token of at most %d bytes", auth.ErrInvalidConfig, maxCookieName)
	case cfg.lifetime <= 0:
		return nil, fmt.Errorf("%w: session.New: WithLifetime: must be > 0", auth.ErrInvalidConfig)
	case cfg.idle <= 0 || cfg.idle > cfg.lifetime:
		return nil, fmt.Errorf("%w: session.New: WithIdleTimeout: must be > 0 and ≤ lifetime", auth.ErrInvalidConfig)
	case cfg.pendingLifetime <= 0 || cfg.pendingLifetime > maxPendingLifetime:
		return nil, fmt.Errorf("%w: session.New: WithPendingLifetime: must be > 0 and ≤ 1 h", auth.ErrInvalidConfig)
	}
	return &Manager{
		store:           store,
		cookie:          cookiePrefix + cfg.name,
		pendingCookie:   cookiePrefix + cfg.name + "-pending",
		lifetime:        cfg.lifetime,
		idle:            cfg.idle,
		pendingLifetime: cfg.pendingLifetime,
		revocationTTL:   max(cfg.lifetime, cfg.pendingLifetime) + revocationMargin,
	}, nil
}

// Authenticate authenticates r with its session cookie, reading only the
// store: the session, and the revocation state of its subject. A missing
// cookie, or one whose session is malformed, unknown, expired or revoked, by
// Revoke, Issue or RevokeSubject, returns auth.ErrNoCredentials, also when the
// session is deleted while this request renews it. A store failure returns an
// error.
func (m *Manager) Authenticate(r *http.Request) (*auth.Principal, error) {
	id, ok := readID(r, m.cookie)
	if !ok {
		return nil, auth.ErrNoCredentials
	}
	ctx := r.Context()
	key := storekey.Key("session", id)
	b, err := m.store.Get(ctx, key)
	switch {
	case errors.Is(err, auth.ErrNotFound):
		return nil, auth.ErrNoCredentials
	case err != nil:
		return nil, fmt.Errorf("session: read: %w", err)
	}
	var rec sessionRecord
	if err := decodeRecord(b, &rec); err != nil {
		return nil, err
	}
	now := time.Now()
	exp := time.Unix(rec.EXP, 0)
	lastWrite := time.Unix(rec.RW, 0)
	if !now.Before(exp) || !now.Before(lastWrite.Add(m.idle)) {
		return nil, auth.ErrNoCredentials
	}
	switch revoked, err := m.revoked(ctx, rec.Sub, rec.Gen); {
	case err != nil:
		return nil, fmt.Errorf("session: %w", err)
	case revoked:
		return nil, auth.ErrNoCredentials
	}
	if now.Sub(lastWrite) >= m.idle/4 {
		rec.RW = now.Unix()
		b, err := encodeRecord(rec)
		if err != nil {
			return nil, err
		}
		// Replace, never Set: a session deleted since the Get, by a logout or
		// a new login, must stay deleted.
		ok, err := m.store.Replace(ctx, key, b, m.ttl(rec, now))
		switch {
		case err != nil:
			return nil, fmt.Errorf("session: renew: %w", err)
		case !ok:
			return nil, auth.ErrNoCredentials
		}
	}
	return &auth.Principal{Subject: rec.Sub, Methods: rec.AMR}, nil
}

// ttl returns the store TTL of a session record: min(rw + idle, exp) − now.
func (m *Manager) ttl(rec sessionRecord, now time.Time) time.Duration {
	end := time.Unix(rec.RW, 0).Add(m.idle)
	if exp := time.Unix(rec.EXP, 0); exp.Before(end) {
		end = exp
	}
	if d := end.Sub(now); d > 0 {
		return d
	}
	return time.Second
}

// Challenge returns the session's challenge, Cookie cookie-name="<name>", for
// missing and invalid credentials. No registered scheme exists for cookie
// sessions; the parameter name follows draft-broyer-http-cookie-auth.
func (m *Manager) Challenge(cause error) string {
	if errors.Is(cause, auth.ErrNoCredentials) || errors.Is(cause, auth.ErrInvalidCredentials) {
		return "Cookie cookie-name=" + authheader.Quote(m.cookie)
	}
	return ""
}

// newID returns a new identifier: 32 random bytes in unpadded base64url.
func newID() string {
	b := make([]byte, idBytes)
	_, _ = rand.Read(b) // never fails since Go 1.24
	return base64.RawURLEncoding.EncodeToString(b)
}

// readID returns the identifier in cookie name, if it has the shape of one.
func readID(r *http.Request, name string) (string, bool) {
	c, err := r.Cookie(name)
	if err != nil {
		return "", false
	}
	b, err := base64.RawURLEncoding.Strict().DecodeString(c.Value)
	if err != nil || len(b) != idBytes || base64.RawURLEncoding.EncodeToString(b) != c.Value {
		return "", false
	}
	return c.Value, true
}

// isToken reports whether s is a non-empty HTTP token (RFC 9110 §5.6.2).
func isToken(s string) bool {
	if s == "" {
		return false
	}
	for i := range len(s) {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		case c < 0x80 && c != 0 && (c == '!' || c == '#' || c == '$' || c == '%' || c == '&' || c == '\'' ||
			c == '*' || c == '+' || c == '-' || c == '.' || c == '^' || c == '_' || c == '`' || c == '|' || c == '~'):
		default:
			return false
		}
	}
	return true
}

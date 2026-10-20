// Package basic implements HTTP Basic authentication (RFC 7617) on a password
// checker. It offers no second factor: with Basic there is no second step in
// which to ask for one, so require "mfa" with auth.RequireMethods on sensitive
// routes. Every request verifies a password, which costs the CPU and memory of
// one argon2id computation.
package basic

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/paccolamano/go/auth"
	"github.com/paccolamano/go/auth/internal/authheader"
)

const (
	defaultRealm = "restricted"
	maxRealm     = 128
)

// Checker verifies a username and a password. *password.Checker satisfies it.
type Checker interface {
	Check(ctx context.Context, username, password string) (*auth.Principal, error)
}

// Option configures New.
type Option func(*config)

type config struct {
	realm string
}

// WithRealm sets the realm of the challenge, 1 to 128 printable ASCII
// characters; the default is "restricted".
func WithRealm(realm string) Option {
	return func(c *config) { c.realm = realm }
}

// Authenticator authenticates requests with HTTP Basic credentials. It
// implements auth.Authenticator and auth.Challenger, and is safe for
// concurrent use.
type Authenticator struct {
	checker   Checker
	challenge string
}

// New returns an Authenticator that verifies credentials with checker.
func New(checker Checker, opts ...Option) (*Authenticator, error) {
	if checker == nil {
		return nil, fmt.Errorf("%w: basic.New: checker: must not be nil", auth.ErrInvalidConfig)
	}
	cfg := config{realm: defaultRealm}
	for _, o := range opts {
		if o == nil {
			return nil, fmt.Errorf("%w: basic.New: options: must not be nil", auth.ErrInvalidConfig)
		}
		o(&cfg)
	}
	if !validRealm(cfg.realm) {
		return nil, fmt.Errorf("%w: basic.New: WithRealm: must be 1 to %d printable ASCII characters", auth.ErrInvalidConfig, maxRealm)
	}
	return &Authenticator{
		checker:   checker,
		challenge: "Basic realm=" + authheader.Quote(cfg.realm) + `, charset="UTF-8"`,
	}, nil
}

// Authenticate verifies the Basic credentials of r with the checker. A request
// without Basic credentials returns auth.ErrNoCredentials. A header that is not
// valid base64, has no colon, has an empty username or is not UTF-8 returns
// auth.ErrInvalidCredentials. The password may contain colons.
func (a *Authenticator) Authenticate(r *http.Request) (*auth.Principal, error) {
	cred, ok := authheader.Credentials(r, "Basic")
	if !ok {
		return nil, auth.ErrNoCredentials
	}
	b, err := base64.StdEncoding.Strict().DecodeString(cred)
	if err != nil {
		return nil, auth.ErrInvalidCredentials
	}
	user, pass, found := strings.Cut(string(b), ":")
	if !found || user == "" || !utf8.ValidString(user) || !utf8.ValidString(pass) {
		return nil, auth.ErrInvalidCredentials
	}
	p, err := a.checker.Check(r.Context(), user, pass)
	if err != nil {
		return nil, fmt.Errorf("basic: %w", err)
	}
	return p, nil
}

// Challenge returns Basic realm="<realm>", charset="UTF-8" for missing and
// invalid credentials. There is no step-up challenge.
func (a *Authenticator) Challenge(cause error) string {
	if errors.Is(cause, auth.ErrNoCredentials) || errors.Is(cause, auth.ErrInvalidCredentials) {
		return a.challenge
	}
	return ""
}

func validRealm(s string) bool {
	if s == "" || len(s) > maxRealm {
		return false
	}
	for i := range len(s) {
		if s[i] < 0x20 || s[i] > 0x7e {
			return false
		}
	}
	return true
}

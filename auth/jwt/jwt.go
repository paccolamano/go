// Package jwt verifies JSON Web Tokens issued by an identity provider. It only
// verifies; it never issues.
//
// It relies on github.com/go-jose/go-jose/v4 for parsing, signatures and key
// sets, and adds what go-jose does not do:
//   - each key is bound to exactly one algorithm, so the token's alg never
//     chooses it (RFC 8725): alg none, an alg different from the key's, or an
//     RSA public key used as an HMAC secret are all rejected;
//   - exp is required, iss must equal the configured issuer and aud must
//     contain the configured audience;
//   - tokens with a crit header and encrypted tokens are rejected;
//   - a remote key set is downloaded at the first verification, cached
//     according to Cache-Control, and downloaded again for an unknown key
//     identifier at most once per refresh interval, without background
//     goroutines.
//
// A key server that cannot be reached is an outage, answered with 500, not a
// reason to reject users with 401.
package jwt

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/paccolamano/go/auth"
)

const (
	defaultLeeway  = time.Minute
	maxLeeway      = 5 * time.Minute
	defaultRefresh = 30 * time.Second
	minRefresh     = time.Second
	maxRefresh     = time.Hour
	clientTimeout  = 10 * time.Second
)

// Option configures New.
type Option func(*config)

type staticSpec struct {
	kid, alg string
	key      any
}

type config struct {
	issuer, audience string
	jwks             string
	jwksSet          bool
	keys             []staticSpec
	client           *http.Client
	clientSet        bool
	leeway           time.Duration
	refresh          time.Duration
	typ              string
	typSet           bool
}

// WithIssuer sets the required issuer, compared exactly with iss. It is
// required.
func WithIssuer(iss string) Option { return func(c *config) { c.issuer = iss } }

// WithAudience sets the audience that aud must contain. It is required.
func WithAudience(aud string) Option { return func(c *config) { c.audience = aud } }

// WithJWKS sets the HTTPS URL of the identity provider's key set. It excludes
// WithKey.
func WithJWKS(url string) Option {
	return func(c *config) {
		c.jwks = url
		c.jwksSet = true
	}
}

// WithKey adds a static key, bound to alg: RS256 or PS256 with an
// *rsa.PublicKey of at least 2,048 bits, ES256 with an *ecdsa.PublicKey on
// P-256, EdDSA with an ed25519.PublicKey, or HS256 with a shared []byte of at
// least 32 bytes. kid may be empty. It excludes WithJWKS.
func WithKey(kid, alg string, key any) Option {
	return func(c *config) { c.keys = append(c.keys, staticSpec{kid: kid, alg: alg, key: key}) }
}

// WithHTTPClient sets the client that downloads the key set. The default has a
// 10-second timeout; every download is also bounded to 10 seconds.
func WithHTTPClient(client *http.Client) Option {
	return func(c *config) {
		c.client = client
		c.clientSet = true
	}
}

// WithLeeway sets the clock leeway for exp, nbf and iat, from 0 to 5 minutes;
// the default is one minute.
func WithLeeway(d time.Duration) Option { return func(c *config) { c.leeway = d } }

// WithRefreshInterval sets the minimum time between two key set downloads,
// from one second to one hour; the default is 30 seconds. It bounds the load
// that tokens with invented key identifiers can put on the key server, and the
// delay before a newly rotated key is accepted.
func WithRefreshInterval(d time.Duration) Option { return func(c *config) { c.refresh = d } }

// WithType requires the token type, for example "at+jwt" (RFC 9068), so that
// an ID token is not accepted as an access token. The comparison ignores case
// and an "application/" prefix.
func WithType(typ string) Option {
	return func(c *config) {
		c.typ = typ
		c.typSet = true
	}
}

// Verifier verifies tokens. It implements auth.Authenticator and
// auth.Challenger, and is safe for concurrent use.
type Verifier struct {
	issuer, audience string
	typ              string
	leeway           time.Duration
	static           []boundKey
	remote           *keySet
}

// New returns a Verifier. WithIssuer, WithAudience, and either WithJWKS or at
// least one WithKey, are required.
func New(opts ...Option) (*Verifier, error) {
	cfg := config{leeway: defaultLeeway, refresh: defaultRefresh}
	for _, o := range opts {
		if o == nil {
			return nil, fmt.Errorf("%w: jwt.New: options: must not be nil", auth.ErrInvalidConfig)
		}
		o(&cfg)
	}
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	v := &Verifier{issuer: cfg.issuer, audience: cfg.audience, typ: cfg.typ, leeway: cfg.leeway}
	for _, s := range cfg.keys {
		k, err := staticKey(s.kid, s.alg, s.key)
		if err != nil {
			return nil, err
		}
		v.static = append(v.static, k)
	}
	if cfg.jwksSet {
		client := cfg.client
		if client == nil {
			client = &http.Client{Timeout: clientTimeout}
		}
		v.remote = &keySet{url: cfg.jwks, client: client, refresh: cfg.refresh}
	}
	return v, nil
}

func (c *config) validate() error {
	invalid := func(setting, reason string) error {
		return fmt.Errorf("%w: jwt.New: %s: %s", auth.ErrInvalidConfig, setting, reason)
	}
	switch {
	case c.issuer == "":
		return invalid("WithIssuer", "is required")
	case c.audience == "":
		return invalid("WithAudience", "is required")
	case c.jwksSet && len(c.keys) > 0:
		return invalid("WithJWKS", "excludes WithKey")
	case !c.jwksSet && len(c.keys) == 0:
		return invalid("keys", "WithJWKS or WithKey is required")
	case c.clientSet && c.client == nil:
		return invalid("WithHTTPClient", "must not be nil")
	case c.leeway < 0 || c.leeway > maxLeeway:
		return invalid("WithLeeway", "must be between 0 and 5 minutes")
	case c.refresh < minRefresh || c.refresh > maxRefresh:
		return invalid("WithRefreshInterval", "must be between 1 second and 1 hour")
	case c.typSet && c.typ == "":
		return invalid("WithType", "must not be empty")
	}
	if c.jwksSet {
		u, err := url.Parse(c.jwks)
		if err != nil || u.Scheme != "https" || u.Host == "" {
			return invalid("WithJWKS", "must be an https URL with a host")
		}
	}
	return nil
}

// Challenge returns Bearer, Bearer error="invalid_token" for a rejected token
// (RFC 6750 §3), and the step-up challenge of RFC 9470 for a principal that
// lacks a method required by auth.RequireMethods.
func (v *Verifier) Challenge(cause error) string {
	switch {
	case errors.Is(cause, auth.ErrInsufficientMethods):
		return `Bearer error="insufficient_user_authentication", error_description="A different authentication level is required"`
	case errors.Is(cause, auth.ErrInvalidCredentials):
		return `Bearer error="invalid_token"`
	case errors.Is(cause, auth.ErrNoCredentials):
		return "Bearer"
	}
	return ""
}

// Claims returns all the claims of the token that authenticated p, including
// custom ones such as roles. It reports false for a principal authenticated by
// another method. The map must not be modified.
func Claims(p *auth.Principal) (map[string]any, bool) {
	if p == nil {
		return nil, false
	}
	c, ok := p.Extra.(claims)
	if !ok {
		return nil, false
	}
	return c, true
}

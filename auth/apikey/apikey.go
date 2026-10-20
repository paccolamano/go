package apikey

import (
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/paccolamano/go/auth"
	"github.com/paccolamano/go/auth/internal/authheader"
)

const maxPrefix = 32

// Option configures New.
type Option func(*config)

type config struct {
	header    string
	headerSet bool
}

// WithHeader reads the whole value of the named header as the key, instead of
// the token of Authorization: Bearer. The name must be a valid header name
// other than Authorization.
func WithHeader(name string) Option {
	return func(c *config) {
		c.header = name
		c.headerSet = true
	}
}

// Keys generates and authenticates API keys. It implements
// auth.Authenticator and auth.Challenger, and is safe for concurrent use.
type Keys struct {
	prefix string
	header string
	lookup LookupFunc
}

// New returns Keys for prefix, 1 to 32 ASCII letters and digits starting with
// a letter, that reads keys through lookup.
func New(prefix string, lookup LookupFunc, opts ...Option) (*Keys, error) {
	if !validPrefix(prefix) {
		return nil, fmt.Errorf("%w: apikey.New: prefix: must be 1 to %d ASCII letters and digits, starting with a letter", auth.ErrInvalidConfig, maxPrefix)
	}
	if lookup == nil {
		return nil, fmt.Errorf("%w: apikey.New: lookup: must not be nil", auth.ErrInvalidConfig)
	}
	var cfg config
	for _, o := range opts {
		if o == nil {
			return nil, fmt.Errorf("%w: apikey.New: options: must not be nil", auth.ErrInvalidConfig)
		}
		o(&cfg)
	}
	if cfg.headerSet && (!validHeader(cfg.header) || strings.EqualFold(cfg.header, "Authorization")) {
		return nil, fmt.Errorf("%w: apikey.New: WithHeader: must be a valid header name other than Authorization", auth.ErrInvalidConfig)
	}
	return &Keys{prefix: prefix, header: cfg.header, lookup: lookup}, nil
}

// Generate returns a new key in clear, to show once, and the record to store.
func (k *Keys) Generate() (Key, Stored) {
	return generate(k.prefix)
}

// Authenticate authenticates r with the key it carries. A request without a
// credential, or with one that lacks the prefix, returns auth.ErrNoCredentials,
// so that another Bearer method can try. A malformed, unknown, wrong or
// expired key returns auth.ErrInvalidCredentials. The principal has the key's
// owner as subject and no methods: RFC 8176 has no value for API keys.
func (k *Keys) Authenticate(r *http.Request) (*auth.Principal, error) {
	var cred string
	if k.header == "" {
		token, ok := authheader.Credentials(r, "Bearer")
		if !ok {
			return nil, auth.ErrNoCredentials
		}
		cred = token
	} else {
		cred = strings.TrimSpace(r.Header.Get(k.header))
	}
	if !strings.HasPrefix(cred, k.prefix+"_") {
		return nil, auth.ErrNoCredentials
	}
	id, secret, ok := parse(k.prefix, cred)
	if !ok {
		return nil, auth.ErrInvalidCredentials
	}
	acct, err := k.lookup(r.Context(), id)
	switch {
	case errors.Is(err, auth.ErrNotFound):
		return nil, auth.ErrInvalidCredentials
	case err != nil:
		return nil, fmt.Errorf("apikey: lookup: %w", err)
	case acct.Subject == "":
		return nil, errors.New("apikey: lookup returned a key without an owner")
	}
	sum := sha256.Sum256(secret)
	if subtle.ConstantTimeCompare(sum[:], acct.Hash) != 1 {
		return nil, auth.ErrInvalidCredentials
	}
	if !acct.ExpiresAt.IsZero() && !time.Now().Before(acct.ExpiresAt) {
		return nil, auth.ErrInvalidCredentials
	}
	return &auth.Principal{Subject: acct.Subject}, nil
}

// Challenge returns Bearer, or Bearer error="invalid_token" for a rejected
// key (RFC 6750). With WithHeader it returns ApiKey header="<name>". There is
// no step-up challenge.
func (k *Keys) Challenge(cause error) string {
	switch {
	case errors.Is(cause, auth.ErrInsufficientMethods):
		return ""
	case k.header != "":
		return "ApiKey header=" + authheader.Quote(k.header)
	case errors.Is(cause, auth.ErrInvalidCredentials):
		return `Bearer error="invalid_token"`
	case errors.Is(cause, auth.ErrNoCredentials):
		return "Bearer"
	}
	return ""
}

func validPrefix(p string) bool {
	if p == "" || len(p) > maxPrefix {
		return false
	}
	for i := range len(p) {
		c := p[i]
		letter := c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
		if !letter && (i == 0 || c < '0' || c > '9') {
			return false
		}
	}
	return true
}

func validHeader(name string) bool {
	if name == "" {
		return false
	}
	for i := range len(name) {
		switch c := name[i]; {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '-', c == '_':
		default:
			return false
		}
	}
	return true
}

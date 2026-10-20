# Contract: Public Go API

**Module**: `github.com/paccolamano/go/auth` (minimum Go 1.26) | **Research**: [../research.md](../research.md)

This is the complete exported surface of `v0.1.0`. Anything not listed here is unexported or
lives under `internal/`. The doc comments are abbreviated, and behavior is specified in
[http-semantics.md](http-semantics.md) and [store.md](store.md).

Shared conventions:

- Every constructor validates its configuration. An invalid one returns an error wrapping
  `auth.ErrInvalidConfig`.
- Every package with options has `type Option func(*config)`.
- Lookups report a missing record with `ErrNotFound`, which is `auth.ErrNotFound` in every
  package.

## Package `auth`

```go
package auth

// Sentinel errors.
var (
    ErrNoCredentials       = errors.New("auth: no credentials")
    ErrInvalidCredentials  = errors.New("auth: invalid credentials")
    ErrTooManyAttempts     = errors.New("auth: too many attempts")
    ErrInsufficientMethods = errors.New("auth: insufficient authentication methods")
    ErrNotFound            = errors.New("auth: not found")
    ErrInvalidConfig       = errors.New("auth: invalid configuration")
)

// Authenticator is an authentication method.
type Authenticator interface {
    Authenticate(r *http.Request) (*Principal, error)
}

// Challenger is implemented by an Authenticator that contributes a
// WWW-Authenticate challenge. cause is ErrNoCredentials, ErrInvalidCredentials
// or ErrInsufficientMethods; "" means none for that cause.
type Challenger interface {
    Challenge(cause error) string
}

// Principal is an authenticated user or client. Handlers must not modify it.
type Principal struct {
    Subject string   // stable identifier, never empty
    Methods []string // RFC 8176 values, e.g. "pwd", "otp", "mfa"
    Extra   any      // method-specific data, read through the method's package
}

// Pending is a login that passed its first factor and awaits a second one.
type Pending interface {
    // Principal returns the principal of the first factor.
    Principal() *Principal
    // Attempt runs verify as one second-factor attempt (see http-semantics.md §6).
    Attempt(ctx context.Context, verify func(ctx context.Context) error) error
}

// Store is a key-value store with expiry for the library's short-lived state.
type Store interface {
    Get(ctx context.Context, key string) ([]byte, error) // ErrNotFound if absent or expired
    Set(ctx context.Context, key string, value []byte, ttl time.Duration) error
    Delete(ctx context.Context, key string) error
    Incr(ctx context.Context, key string, ttl time.Duration) (int64, error) // atomic
}

// Rejection describes a refused request.
type Rejection struct {
    Status int   // 401, 403, 429 or 500
    Err    error // compare with errors.Is; never write it to clients
}

// RejectFunc writes the response for a refused request.
type RejectFunc func(w http.ResponseWriter, r *http.Request, rej Rejection)

// Option configures New.
type Option func(*config)

func WithAnonymous() Option                // let requests without credentials through
func WithRejectFunc(fn RejectFunc) Option  // replace the plain-text response

// New returns the authentication middleware.
func New(a Authenticator, opts ...Option) (func(http.Handler) http.Handler, error)

// Any combines methods, tried in order.
func Any(methods ...Authenticator) Authenticator

// RequireMethods admits only principals whose Methods include every value.
func RequireMethods(methods ...string) func(http.Handler) http.Handler

// FromContext returns the request's principal.
func FromContext(ctx context.Context) (*Principal, bool)
```

## Package `auth/memstore`

```go
package memstore

// Store is an in-memory auth.Store, safe for concurrent use.
type Store struct{ /* unexported */ }

func New() *Store
func (s *Store) Get(ctx context.Context, key string) ([]byte, error)
func (s *Store) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error
func (s *Store) Delete(ctx context.Context, key string) error
func (s *Store) Incr(ctx context.Context, key string, ttl time.Duration) (int64, error)
```

## Package `auth/password`

```go
package password

var (
    ErrNotFound = auth.ErrNotFound
    ErrEmpty    = errors.New("password: empty")
    ErrTooLong  = errors.New("password: too long")
)

// Params are argon2id parameters.
type Params struct {
    Memory      uint32 // KiB
    Iterations  uint32
    Parallelism uint8
}

func DefaultParams() Params // 19 MiB, t=2, p=1

// Hash returns an argon2id PHC string with the default parameters.
func Hash(plain string) (string, error)

// Account is what the lookup returns.
type Account struct {
    Subject string
    Hash    string // PHC argon2id or bcrypt
}

type LookupFunc func(ctx context.Context, username string) (Account, error)

// RehashFunc stores a hash computed after an outdated hash verified. It
// cannot fail the login: it handles its own errors.
type RehashFunc func(ctx context.Context, subject, hash string)

// AttemptLimit configures the per-username failed-attempt limit.
type AttemptLimit struct {
    Store  auth.Store
    Max    int           // 0: 10
    Window time.Duration // 0: 15 minutes
}

type Option func(*config)

func WithParams(p Params) Option
func WithMaxLength(n int) Option // bytes; default 1,024
func WithRehash(fn RehashFunc) Option
func WithAttemptLimit(l AttemptLimit) Option

// Checker verifies passwords; safe for concurrent use.
type Checker struct{ /* unexported */ }

func NewChecker(lookup LookupFunc, opts ...Option) (*Checker, error)
func (c *Checker) Check(ctx context.Context, username, plain string) (*auth.Principal, error)
func (c *Checker) Hash(plain string) (string, error) // with the checker's parameters
```

## Package `auth/session`

```go
package session

type Option func(*config)

func WithCookieName(name string) Option          // base name; default "session"
func WithLifetime(d time.Duration) Option        // absolute; default 8h
func WithIdleTimeout(d time.Duration) Option     // default 30m
func WithPendingLifetime(d time.Duration) Option // default 5m

// Manager issues, authenticates and revokes sessions. It implements
// auth.Authenticator and auth.Challenger.
type Manager struct{ /* unexported */ }

func New(store auth.Store, opts ...Option) (*Manager, error)
func (m *Manager) Authenticate(r *http.Request) (*auth.Principal, error)
func (m *Manager) Challenge(cause error) string
func (m *Manager) Issue(w http.ResponseWriter, r *http.Request, p *auth.Principal) error
func (m *Manager) IssuePending(w http.ResponseWriter, r *http.Request, p *auth.Principal) error
func (m *Manager) Pending(r *http.Request) (auth.Pending, error)
func (m *Manager) Revoke(w http.ResponseWriter, r *http.Request) error
```

## Package `auth/totp`

```go
package totp

var ErrNotFound = auth.ErrNotFound

type LookupFunc func(ctx context.Context, subject string) ([]byte, error)

// Verifier verifies time-based codes.
type Verifier struct{ /* unexported */ }

func New(lookup LookupFunc, store auth.Store) (*Verifier, error)
func (v *Verifier) Enrolled(ctx context.Context, subject string) (bool, error)
func (v *Verifier) Verify(ctx context.Context, pending auth.Pending, code string) (*auth.Principal, error)
func (v *Verifier) Confirm(ctx context.Context, subject string, secret []byte, code string) error

// Enrollment is a candidate secret and its otpauth:// URI.
type Enrollment struct{ /* unexported */ }

func NewEnrollment(issuer, account string) (*Enrollment, error)
func (e *Enrollment) Secret() []byte
func (e *Enrollment) URI() string
func (e *Enrollment) LogValue() slog.Value // [REDACTED]
func (e *Enrollment) String() string       // [REDACTED]
```

## Package `auth/recovery`

```go
package recovery

var ErrNotFound = auth.ErrNotFound

const DefaultCount = 10

// Codes is a generated set: show Plain once, store Hashes.
type Codes struct{ /* unexported */ }

func Generate(n int) (*Codes, error) // 1 <= n <= 100
func (c *Codes) Plain() []string
func (c *Codes) Hashes() []string
func (c *Codes) LogValue() slog.Value // [REDACTED]
func (c *Codes) String() string       // [REDACTED]

type LookupFunc func(ctx context.Context, subject string) ([]string, error)   // unused hashes
type ConsumeFunc func(ctx context.Context, subject, hash string) error        // ErrNotFound if already used

type Verifier struct{ /* unexported */ }

func New(lookup LookupFunc, consume ConsumeFunc, store auth.Store) (*Verifier, error)
func (v *Verifier) Verify(ctx context.Context, pending auth.Pending, code string) (*auth.Principal, error)
```

## Package `auth/basic`

```go
package basic

// Checker verifies a username and password; *password.Checker satisfies it.
type Checker interface {
    Check(ctx context.Context, username, password string) (*auth.Principal, error)
}

type Option func(*config)

func WithRealm(realm string) Option // default "restricted"

// Authenticator implements auth.Authenticator and auth.Challenger.
type Authenticator struct{ /* unexported */ }

func New(checker Checker, opts ...Option) (*Authenticator, error)
func (a *Authenticator) Authenticate(r *http.Request) (*auth.Principal, error)
func (a *Authenticator) Challenge(cause error) string
```

## Package `auth/apikey`

```go
package apikey

var ErrNotFound = auth.ErrNotFound

// Key is a generated key in clear. LogValue redacts it; fmt prints it, so it
// can be shown once.
type Key string

func (k Key) LogValue() slog.Value // [REDACTED]

// Stored is what the developer saves.
type Stored struct {
    ID   string
    Hash []byte // SHA-256 of the secret
}

// Account is what the lookup returns.
type Account struct {
    Subject   string
    Hash      []byte
    ExpiresAt time.Time // zero: never
}

type LookupFunc func(ctx context.Context, id string) (Account, error)

type Option func(*config)

func WithHeader(name string) Option // default: Authorization: Bearer

// Keys generates and authenticates keys; implements auth.Authenticator and
// auth.Challenger.
type Keys struct{ /* unexported */ }

func New(prefix string, lookup LookupFunc, opts ...Option) (*Keys, error)
func (k *Keys) Generate() (Key, Stored)
func (k *Keys) Authenticate(r *http.Request) (*auth.Principal, error)
func (k *Keys) Challenge(cause error) string
```

## Package `auth/jwt`

```go
package jwt

type Option func(*config)

func WithIssuer(iss string) Option                   // required
func WithAudience(aud string) Option                 // required
func WithJWKS(url string) Option                     // https; exclusive with WithKey
func WithKey(kid, alg string, key any) Option        // repeatable; alg in RS256, PS256, ES256, EdDSA, HS256
func WithHTTPClient(c *http.Client) Option           // default: 10s timeout
func WithLeeway(d time.Duration) Option              // default 1m
func WithRefreshInterval(d time.Duration) Option     // default 30s
func WithType(typ string) Option                     // e.g. "at+jwt"

// Verifier implements auth.Authenticator and auth.Challenger.
type Verifier struct{ /* unexported */ }

func New(opts ...Option) (*Verifier, error)
func (v *Verifier) Authenticate(r *http.Request) (*auth.Principal, error)
func (v *Verifier) Challenge(cause error) string

// Claims returns the claims of the token that authenticated p.
func Claims(p *auth.Principal) (map[string]any, bool)
```

## Size of the surface

| Package | Types | Functions and methods | Variables and constants | Options |
|---------|------:|----------------------:|------------------------:|--------:|
| `auth` | 8 | 4 + 0 | 6 | 2 |
| `memstore` | 1 | 1 + 4 | 0 | 0 |
| `password` | 7 | 3 + 2 | 3 | 4 |
| `session` | 2 | 1 + 6 | 0 | 4 |
| `totp` | 3 | 2 + 7 | 1 | 0 |
| `recovery` | 4 | 2 + 5 | 2 | 0 |
| `basic` | 3 | 1 + 2 | 0 | 1 |
| `apikey` | 6 | 1 + 4 | 1 | 1 |
| `jwt` | 2 | 2 + 2 | 0 | 8 |
| **Total** | **36** | **17 + 32** | **13** | **20** |

The total is 118 exported identifiers, counting types, functions, methods, options, variables
and constants, but not struct fields or interface methods. The functions column does not count
the options.

## Departures from the request's sketch

| Request | Contract | Reason |
|---------|----------|--------|
| `key, stored, err := keys.Generate()` | `key, stored := keys.Generate()` | `crypto/rand.Read` never fails since Go 1.24, so there is no error to return (R-16) |
| `RejectFunc` from `middleware` (decision 4) | `auth.RejectFunc`, same shape | no third dependency (R-06) |
| Lookup sentinels per package | the same value, `auth.ErrNotFound`, re-exported | `errors.Is` across packages (R-03) |
| (not in the request) `auth.Pending` | the interface between `session` and the second factors | shared 5-attempt budget (R-12) |
| (not in the request) `auth.Challenger` | optional interface | per-method challenges (FR-015, R-05) |
| (not in the request) `Principal.Extra` | one slot for method data | `jwt.Claims(p)` without JWT in the core (R-03) |

## Traceability

| Requirement | Exported API |
|-------------|--------------|
| FR-001 | package layout above |
| FR-002 | `go.mod` (plan) |
| FR-003 | `auth.ErrInvalidConfig`, every `New`/`NewChecker` and every `Option` |
| FR-008 | `apikey.Key.LogValue`, `Enrollment.LogValue`/`String`, `Codes.LogValue`/`String` |
| FR-011, FR-012 | `Authenticator`, `Principal`, sentinel errors |
| FR-013 | `Any` |
| FR-014, FR-016 | `New`, `WithAnonymous`, `WithRejectFunc`, `Rejection`, `RejectFunc` |
| FR-015, FR-017 | `Challenger`, and every method's `Challenge` |
| FR-018 | `FromContext` |
| FR-019 | `RequireMethods`, `ErrInsufficientMethods` |
| FR-020 to FR-022 | `Store` |
| FR-023 | `memstore` |
| FR-024 | `password.Hash`, `Checker.Hash`, `Params`, `DefaultParams`, `WithParams`, `ErrEmpty`, `ErrTooLong` |
| FR-025 to FR-028 | `NewChecker`, `Check`, `Account`, `LookupFunc`, `WithMaxLength` |
| FR-029 | `WithRehash`, `RehashFunc` |
| FR-030 | `WithAttemptLimit`, `AttemptLimit`, `auth.ErrTooManyAttempts` |
| FR-031 | `basic.Checker`, satisfied by `*password.Checker` |
| FR-032 to FR-037, FR-041 | `session.New`, `Issue`, `Authenticate`, `Revoke`, options |
| FR-038 to FR-040 | `IssuePending`, `Pending`, `auth.Pending` |
| FR-042 to FR-047 | `totp` |
| FR-048 to FR-050 | `recovery` |
| FR-051 to FR-054 | `basic` |
| FR-055 to FR-058 | `apikey` |
| FR-059 to FR-069 | `jwt` |

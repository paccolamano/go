package auth

import (
	"context"
	"errors"
	"net/http"
)

// Sentinel errors. Compare them with errors.Is: methods may wrap them.
var (
	// ErrNoCredentials reports that a request carries no credentials for a
	// method. A combination moves on to its next method.
	ErrNoCredentials = errors.New("auth: no credentials")
	// ErrInvalidCredentials reports credentials that are present but not
	// valid. The request is rejected with 401, without trying other methods.
	ErrInvalidCredentials = errors.New("auth: invalid credentials")
	// ErrTooManyAttempts reports that an attempt limit was reached. The
	// middleware answers it with 429.
	ErrTooManyAttempts = errors.New("auth: too many attempts")
	// ErrInsufficientMethods reports a principal that lacks a method required
	// by RequireMethods.
	ErrInsufficientMethods = errors.New("auth: insufficient authentication methods")
	// ErrNotFound is returned by a Store for an absent or expired key, and by
	// the developer's lookup functions for a missing record.
	ErrNotFound = errors.New("auth: not found")
	// ErrInvalidConfig is wrapped by every configuration error returned by the
	// constructors of this module.
	ErrInvalidConfig = errors.New("auth: invalid configuration")
)

// Authenticator is an authentication method. Authenticate examines a request
// and returns exactly one of: a principal; an error matching ErrNoCredentials
// when the request carries no credentials for this method; an error matching
// ErrInvalidCredentials when it carries credentials that are not valid; or any
// other error when the method could not decide, for example because a database
// is unreachable. Authenticate uses r.Context() for any I/O.
type Authenticator interface {
	Authenticate(r *http.Request) (*Principal, error)
}

// Challenger is implemented by an Authenticator that contributes a
// WWW-Authenticate challenge (RFC 9110 §11.6.1). cause is ErrNoCredentials,
// ErrInvalidCredentials or ErrInsufficientMethods; "" means none for that
// cause.
type Challenger interface {
	Challenge(cause error) string
}

// Principal is an authenticated user or client. Handlers must not modify it.
type Principal struct {
	// Subject is the stable identifier of the user or client. It is never
	// empty.
	Subject string
	// Methods lists the authentication methods used, as RFC 8176 values such
	// as "pwd", "otp" and "mfa", in the order they were added, without
	// duplicates. It may be empty.
	Methods []string
	// Extra holds data specific to the method that produced the principal.
	// Read it through that method's package, for example jwt.Claims. It is
	// never persisted.
	Extra any
}

// Pending is a login that passed its first factor and awaits a second one.
// The session package implements it; the totp and recovery packages consume
// it.
type Pending interface {
	// Principal returns the principal authenticated by the first factor.
	Principal() *Principal
	// Attempt runs verify as one second-factor attempt. It counts the attempt
	// before calling verify, so that parallel requests never evaluate more
	// codes than the budget allows. Once the budget is exhausted it cancels the
	// pending login and returns an error matching ErrTooManyAttempts without
	// calling verify. When the last allowed attempt fails, the returned error
	// matches both ErrTooManyAttempts and ErrInvalidCredentials. A failure
	// while counting is returned and not counted.
	Attempt(ctx context.Context, verify func(ctx context.Context) error) error
}

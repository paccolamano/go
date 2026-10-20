// Package totp verifies time-based one-time codes (RFC 6238) as a second
// factor: HMAC-SHA-1, 6 digits, a 30-second step and a tolerance of one step
// on each side, as authenticator apps expect.
//
// A code is accepted at most once per subject and step, even under parallel
// requests, and attempts count against the pending login's budget. The secret
// lives in the developer's database and is read through a LookupFunc; it is
// never written to the store.
package totp

import (
	"crypto/rand"
	"encoding/base32"
	"errors"
	"log/slog"
	"net/url"
	"slices"
	"strings"
)

const secretBytes = 20

// Enrollment is a candidate secret and its otpauth:// URI. Show the URI, for
// example as a QR code, keep the secret as a candidate, and store it as
// active only after Verifier.Confirm accepts a first code. It renders as
// [REDACTED] in logs and with fmt.
type Enrollment struct {
	secret []byte
	uri    string
}

// NewEnrollment returns a new 160-bit secret and its URI, labeled with issuer
// and account. Neither may be empty or contain ':'.
func NewEnrollment(issuer, account string) (*Enrollment, error) {
	if issuer == "" || account == "" || strings.Contains(issuer, ":") || strings.Contains(account, ":") {
		return nil, errors.New("totp: issuer and account must be non-empty and contain no ':'")
	}
	secret := make([]byte, secretBytes)
	_, _ = rand.Read(secret) // never fails since Go 1.24
	q := func(s string) string { return strings.ReplaceAll(url.QueryEscape(s), "+", "%20") }
	uri := "otpauth://totp/" + url.PathEscape(issuer) + ":" + url.PathEscape(account) +
		"?secret=" + base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(secret) +
		"&issuer=" + q(issuer) + "&algorithm=SHA1&digits=6&period=30"
	return &Enrollment{secret: secret, uri: uri}, nil
}

// Secret returns a copy of the secret, to keep as a candidate and, after
// confirmation, to store as active.
func (e *Enrollment) Secret() []byte { return slices.Clone(e.secret) }

// URI returns the otpauth:// URI for authenticator apps.
func (e *Enrollment) URI() string { return e.uri }

// LogValue implements slog.LogValuer: an enrollment is logged as [REDACTED].
func (e *Enrollment) LogValue() slog.Value { return slog.StringValue("[REDACTED]") }

// String returns [REDACTED].
func (e *Enrollment) String() string { return "[REDACTED]" }

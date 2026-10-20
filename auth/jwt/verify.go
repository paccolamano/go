package jwt

import (
	"context"
	"net/http"
	"slices"
	"strings"
	"time"

	jose "github.com/go-jose/go-jose/v4"
	josejwt "github.com/go-jose/go-jose/v4/jwt"

	"github.com/paccolamano/go/auth"
	"github.com/paccolamano/go/auth/internal/authheader"
)

// claims is the type of Principal.Extra for principals authenticated with a
// token.
type claims map[string]any

// Authenticate verifies the token in Authorization: Bearer. A Bearer
// credential that does not have the shape of a compact token returns
// auth.ErrNoCredentials, so that another Bearer method, such as API keys, can
// try. Every rejected token returns auth.ErrInvalidCredentials. When the
// decision depends on a key set that cannot be downloaded, it returns another
// error, answered with 500.
func (v *Verifier) Authenticate(r *http.Request) (*auth.Principal, error) {
	token, ok := authheader.Credentials(r, "Bearer")
	if !ok {
		return nil, auth.ErrNoCredentials
	}
	switch strings.Count(token, ".") {
	case 2:
	case 4: // compact JWE: encrypted tokens are not supported
		return nil, auth.ErrInvalidCredentials
	default:
		return nil, auth.ErrNoCredentials
	}
	parsed, err := josejwt.ParseSigned(token, algorithms())
	if err != nil {
		return nil, auth.ErrInvalidCredentials
	}
	h := parsed.Headers[0]
	if _, ok := h.ExtraHeaders["crit"]; ok {
		return nil, auth.ErrInvalidCredentials
	}
	if v.typ != "" {
		typ, _ := h.ExtraHeaders[jose.HeaderType].(string)
		if !sameType(typ, v.typ) {
			return nil, auth.ErrInvalidCredentials
		}
	}
	key, err := v.key(r.Context(), h.KeyID, h.Algorithm)
	if err != nil {
		return nil, err
	}
	if key == nil {
		return nil, auth.ErrInvalidCredentials
	}

	var std josejwt.Claims
	all := claims{}
	if err := parsed.Claims(key.key, &std, &all); err != nil {
		return nil, auth.ErrInvalidCredentials
	}
	if std.Expiry == nil || std.Subject == "" {
		return nil, auth.ErrInvalidCredentials
	}
	expected := josejwt.Expected{Issuer: v.issuer, AnyAudience: josejwt.Audience{v.audience}, Time: time.Now()}
	if err := std.ValidateWithLeeway(expected, v.leeway); err != nil {
		return nil, auth.ErrInvalidCredentials
	}
	methods, ok := amr(all["amr"])
	if !ok {
		return nil, auth.ErrInvalidCredentials
	}
	return &auth.Principal{Subject: std.Subject, Methods: methods, Extra: all}, nil
}

// key selects the one key bound to alg, and to kid when the token names one.
// It returns nil when no key, or more than one, qualifies.
func (v *Verifier) key(ctx context.Context, kid, alg string) (*boundKey, error) {
	found := candidates(v.static, kid, alg)
	if v.remote != nil {
		var err error
		if found, err = v.remote.find(ctx, kid, alg); err != nil {
			return nil, err
		}
	}
	if len(found) != 1 {
		return nil, nil
	}
	return &found[0], nil
}

// amr converts the amr claim to methods, without duplicates. ok is false when
// it is present but not a list of strings.
func amr(v any) ([]string, bool) {
	if v == nil {
		return nil, true
	}
	list, ok := v.([]any)
	if !ok {
		return nil, false
	}
	var out []string
	for _, m := range list {
		s, ok := m.(string)
		if !ok {
			return nil, false
		}
		if !slices.Contains(out, s) {
			out = append(out, s)
		}
	}
	return out, true
}

// sameType compares two typ values without regard to case and to an optional
// "application/" prefix (RFC 7515 §4.1.9).
func sameType(a, b string) bool {
	trim := func(s string) string {
		if len(s) >= len("application/") && strings.EqualFold(s[:len("application/")], "application/") {
			return s[len("application/"):]
		}
		return s
	}
	return a != "" && strings.EqualFold(trim(a), trim(b))
}

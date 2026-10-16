package secureheaders

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/paccolamano/go/middleware"
	"github.com/paccolamano/go/middleware/internal/httpheader"
)

const (
	headerCSP           = "Content-Security-Policy"
	headerCSPReportOnly = "Content-Security-Policy-Report-Only"
	headerHSTS          = "Strict-Transport-Security"
	noncePlaceholder    = "{nonce}"
	preloadMinAge       = 365 * 24 * time.Hour
)

// HSTS configures the Strict-Transport-Security header.
type HSTS struct {
	// MaxAge is how long browsers remember to use HTTPS only. It must be at
	// least one second and is sent in whole seconds.
	MaxAge time.Duration
	// IncludeSubDomains applies the policy to every subdomain.
	IncludeSubDomains bool
	// Preload requests inclusion in the browsers' preload lists. It requires
	// IncludeSubDomains and a MaxAge of at least one year.
	Preload bool
}

// Option configures the middleware returned by New.
type Option func(*config)

type opKind int

const (
	opSet opKind = iota
	opRemove
	opCSP
	opHSTS
)

// op is one header change, applied in order on top of the defaults.
type op struct {
	kind   opKind
	option string // option name, for error messages
	name   string
	value  string
	hsts   HSTS
}

type config struct {
	ops   []op
	nonce bool
}

func defaultHeaders() map[string]string {
	return map[string]string{
		headerCSP:                           "default-src 'none'; frame-ancestors 'none'",
		"Cross-Origin-Embedder-Policy":      "require-corp",
		"Cross-Origin-Opener-Policy":        "same-origin",
		"Cross-Origin-Resource-Policy":      "same-origin",
		"Permissions-Policy":                "accelerometer=(), camera=(), display-capture=(), geolocation=(), gyroscope=(), hid=(), magnetometer=(), microphone=(), midi=(), payment=(), serial=(), usb=(), xr-spatial-tracking=()",
		"Referrer-Policy":                   "no-referrer",
		"X-Content-Type-Options":            "nosniff",
		"X-Frame-Options":                   "DENY",
		"X-Permitted-Cross-Domain-Policies": "none",
	}
}

// WithHeader sets a header to value, overriding a default or adding a new
// header. It cannot be used for Content-Security-Policy,
// Content-Security-Policy-Report-Only or Strict-Transport-Security, which have
// dedicated options.
func WithHeader(name, value string) Option {
	return func(c *config) {
		c.ops = append(c.ops, op{kind: opSet, option: "WithHeader", name: name, value: value})
	}
}

// WithoutHeader removes a header, including a default one such as
// Content-Security-Policy. Removing an absent header has no effect.
func WithoutHeader(name string) Option {
	return func(c *config) {
		c.ops = append(c.ops, op{kind: opRemove, option: "WithoutHeader", name: name})
	}
}

// WithHSTS enables Strict-Transport-Security, which is disabled by default
// because browsers remember it and it can lock users out of hosts that are not
// served over HTTPS.
func WithHSTS(h HSTS) Option {
	return func(c *config) {
		c.ops = append(c.ops, op{kind: opHSTS, option: "WithHSTS", name: headerHSTS, hsts: h})
	}
}

// WithCSP replaces the enforced Content-Security-Policy. The placeholder
// "{nonce}" is replaced by the per-request nonce enabled with WithCSPNonce.
func WithCSP(policy string) Option {
	return func(c *config) {
		c.ops = append(c.ops, op{kind: opCSP, option: "WithCSP", name: headerCSP, value: policy})
	}
}

// WithCSPReportOnly sets Content-Security-Policy-Report-Only, which browsers
// do not enforce but report violations of. It can be combined with the
// enforced policy to trial a stricter one. It may contain "{nonce}".
func WithCSPReportOnly(policy string) Option {
	return func(c *config) {
		c.ops = append(c.ops, op{kind: opCSP, option: "WithCSPReportOnly", name: headerCSPReportOnly, value: policy})
	}
}

// WithCSPNonce generates a fresh nonce for every request, replaces every
// "{nonce}" in the content security policies with it, and makes it available
// through NonceFromContext. At least one policy must contain "{nonce}".
func WithCSPNonce() Option {
	return func(c *config) { c.nonce = true }
}

// headers applies the options to the defaults and validates the result.
func (c *config) headers() (map[string]string, error) {
	headers := defaultHeaders()
	for _, o := range c.ops {
		if !httpheader.ValidToken(o.name) {
			return nil, invalid(o.option, "%q is not a valid header name", o.name)
		}
		name := http.CanonicalHeaderKey(o.name)
		switch o.kind {
		case opRemove:
			delete(headers, name)
		case opSet:
			if name == headerCSP || name == headerCSPReportOnly || name == headerHSTS {
				return nil, invalid(o.option, "%s has a dedicated option", name)
			}
			if !httpheader.ValidValue(o.value) {
				return nil, invalid(o.option, "the value of %s contains CR, LF or NUL", name)
			}
			headers[name] = o.value
		case opCSP:
			if err := validatePolicy(o.value); err != nil {
				return nil, invalid(o.option, "%s", err)
			}
			headers[name] = o.value
		case opHSTS:
			v, err := renderHSTS(o.hsts)
			if err != nil {
				return nil, invalid(o.option, "%s", err)
			}
			headers[name] = v
		}
	}
	referenced := strings.Contains(headers[headerCSP], noncePlaceholder) ||
		strings.Contains(headers[headerCSPReportOnly], noncePlaceholder)
	switch {
	case c.nonce && !referenced:
		return nil, invalid("WithCSPNonce", "no content security policy contains %s", noncePlaceholder)
	case !c.nonce && referenced:
		return nil, invalid("WithCSPNonce", "a policy contains %s but the option is not enabled", noncePlaceholder)
	}
	return headers, nil
}

type policyError string

func (e policyError) Error() string { return string(e) }

// validatePolicy checks that policy is a non-empty serialized CSP: no CR, LF or
// NUL, and every non-empty ";"-separated directive starts with a lowercase
// directive name.
func validatePolicy(policy string) error {
	if !httpheader.ValidValue(policy) {
		return policyError("the policy contains CR, LF or NUL")
	}
	directives := 0
	for d := range strings.SplitSeq(policy, ";") {
		d = strings.Trim(d, " \t")
		if d == "" {
			continue
		}
		name, _, _ := strings.Cut(d, " ")
		if name == "" || strings.Trim(name, "abcdefghijklmnopqrstuvwxyz-") != "" {
			return policyError(fmt.Sprintf("directive %q does not start with a directive name", d))
		}
		directives++
	}
	if directives == 0 {
		return policyError("the policy has no directive")
	}
	return nil
}

func renderHSTS(h HSTS) (string, error) {
	if h.MaxAge < time.Second {
		return "", policyError("MaxAge must be at least one second")
	}
	if h.Preload && (!h.IncludeSubDomains || h.MaxAge < preloadMinAge) {
		return "", policyError("Preload requires IncludeSubDomains and a MaxAge of at least one year")
	}
	v := "max-age=" + strconv.FormatInt(int64(h.MaxAge/time.Second), 10)
	if h.IncludeSubDomains {
		v += "; includeSubDomains"
	}
	if h.Preload {
		v += "; preload"
	}
	return v, nil
}

func invalid(option, format string, args ...any) error {
	return fmt.Errorf("%w: secureheaders: %s: %s", middleware.ErrInvalidConfig, option, fmt.Sprintf(format, args...))
}

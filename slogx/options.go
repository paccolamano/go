package slogx

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"
)

// ErrInvalidConfig is wrapped by every error a constructor of this package
// returns.
var ErrInvalidConfig = errors.New("slogx: invalid configuration")

// Option configures a handler. Each constructor accepts the options listed in
// its documentation; any other option makes it return an error wrapping
// ErrInvalidConfig.
type Option func(*config)

// optionSet records which options were given, for the applicability checks.
type optionSet uint8

const (
	optContextFunc optionSet = 1 << iota
	optRedactKeys
	optSampling
	optSampleErrors
)

// optionNames lists every option in the order the checks report them.
func optionNames() []struct {
	opt  optionSet
	name string
} {
	return []struct {
		opt  optionSet
		name string
	}{
		{optContextFunc, "WithContextFunc"},
		{optRedactKeys, "WithRedactKeys"},
		{optSampling, "WithSampling"},
		{optSampleErrors, "WithSampleErrors"},
	}
}

type config struct {
	fns          []func(context.Context) []slog.Attr
	keys         []string
	noKeys       bool // WithRedactKeys was called without keys
	first        int
	thereafter   int
	interval     time.Duration
	sampleErrors bool
	set          optionSet
}

// WithContextFunc adds a function that computes attributes from the context of
// every handled record, for example a request identifier stored by another
// package. It is called only for enabled records. It can be given several
// times: when two functions return the same key, the first one configured wins,
// and attributes attached with With win over both.
//
// Accepted by New and NewContextHandler.
func WithContextFunc(fn func(ctx context.Context) []slog.Attr) Option {
	return func(c *config) {
		c.fns = append(c.fns, fn)
		c.set |= optContextFunc
	}
}

// WithRedactKeys adds sensitive keys. The value of every attribute whose key
// equals one of them, compared without regard to case and at any depth, is
// written as [REDACTED]. It can be given several times: the keys accumulate.
//
// Accepted by New and NewRedactHandler, which requires it.
func WithRedactKeys(keys ...string) Option {
	return func(c *config) {
		if len(keys) == 0 {
			c.noKeys = true
		}
		c.keys = append(c.keys, keys...)
		c.set |= optRedactKeys
	}
}

// WithSampling samples repeated records: in each interval, the first records of
// each level and message pass, then one every thereafter. first and thereafter
// must be at least 1, and interval must be positive. When given several times,
// the last call wins.
//
// Accepted by New and NewSamplingHandler, which requires it.
func WithSampling(first, thereafter int, interval time.Duration) Option {
	return func(c *config) {
		c.first, c.thereafter, c.interval = first, thereafter, interval
		c.set |= optSampling
	}
}

// WithSampleErrors samples records at slog.LevelError and above too. Without
// it, they are always written. It requires WithSampling.
//
// Accepted by New and NewSamplingHandler.
func WithSampleErrors() Option {
	return func(c *config) {
		c.sampleErrors = true
		c.set |= optSampleErrors
	}
}

// newConfig applies opts for the constructor ctor, then checks that every
// option is in accepted, that every option in required was given, and that the
// values are valid.
func newConfig(ctor string, accepted, required optionSet, opts []Option) (config, error) {
	var c config
	for i, opt := range opts {
		if opt == nil {
			return config{}, invalid(ctor, fmt.Sprintf("options[%d]", i), "must not be nil")
		}
		opt(&c)
	}
	for _, o := range optionNames() {
		if c.set&o.opt != 0 && accepted&o.opt == 0 {
			return config{}, invalid(ctor, o.name, "does not apply")
		}
	}
	for _, o := range optionNames() {
		if required&o.opt != 0 && c.set&o.opt == 0 {
			return config{}, invalid(ctor, o.name, "required")
		}
	}
	if err := c.validate(ctor); err != nil {
		return config{}, err
	}
	return c, nil
}

func (c *config) validate(ctor string) error {
	for _, fn := range c.fns {
		if fn == nil {
			return invalid(ctor, "WithContextFunc", "function must not be nil")
		}
	}
	if c.noKeys {
		return invalid(ctor, "WithRedactKeys", "at least one key is required")
	}
	for _, k := range c.keys {
		if strings.TrimSpace(k) == "" {
			return invalid(ctor, "WithRedactKeys", "empty key")
		}
	}
	if c.set&optSampling != 0 {
		switch {
		case c.first < 1:
			return invalid(ctor, "WithSampling", "first must be at least 1")
		case c.thereafter < 1:
			return invalid(ctor, "WithSampling", "thereafter must be at least 1")
		case c.interval <= 0:
			return invalid(ctor, "WithSampling", "interval must be positive")
		}
	}
	if c.set&optSampleErrors != 0 && c.set&optSampling == 0 {
		return invalid(ctor, "WithSampleErrors", "requires WithSampling")
	}
	return nil
}

func invalid(ctor, option, reason string) error {
	return fmt.Errorf("%w: %s: %s: %s", ErrInvalidConfig, ctor, option, reason)
}

func errNilHandler(ctor string) error {
	return fmt.Errorf("%w: %s: next: must not be nil", ErrInvalidConfig, ctor)
}

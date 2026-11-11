package slogx

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
)

// ErrInvalidConfig is wrapped by every error New returns.
var ErrInvalidConfig = errors.New("slogx: invalid configuration")

// Option configures New. Every option applies, in any combination.
type Option func(*config)

type config struct {
	sources []source     // extraction functions and context values, in configuration order
	keys    []string     // sensitive keys
	level   slog.Leveler // nil without WithDefaultLevel
	err     error        // the first invalid option, which New reports
}

// WithDefaultLevel sets the minimum level of the records logged with a context
// that carries no level (WithLevel). For those records, the Handler compares
// the level with it alone and does not ask the next handler, so the threshold
// holds whatever the next handler enables. The level is read at every record:
// pass a *slog.LevelVar to change it at run time. When given several times, the
// last one applies. A nil level, or a nil *slog.LevelVar, is invalid.
//
// It is required when the next handler is a Fanout.
func WithDefaultLevel(level slog.Leveler) Option {
	return func(c *config) {
		if lv, ok := level.(*slog.LevelVar); level == nil || ok && lv == nil {
			c.fail("WithDefaultLevel", "must not be nil")
			return
		}
		c.level = level
	}
}

// fail records the first invalid option.
func (c *config) fail(option, reason string) {
	if c.err == nil {
		c.err = invalid(option, reason)
	}
}

// WithContextFunc adds a function that computes attributes from the context of
// every handled record, such as the trace and span identifiers of a tracing
// library. It is called only for enabled records. Use WithContextValue for a
// single value. It can be given several times: when extraction functions and
// context values produce the same key, the one configured first wins, and
// attributes attached with With win over all of them.
func WithContextFunc(fn func(ctx context.Context) []slog.Attr) Option {
	return func(c *config) {
		if fn == nil {
			c.fail("WithContextFunc", "function must not be nil")
			return
		}
		c.sources = append(c.sources, source{fn: fn})
	}
}

// WithContextValue adds the attribute key, with the value get returns, to every
// enabled record whose context carries it, that is, for which get returns true:
//
//	slogx.WithContextValue("request_id", requestid.FromContext)
//
// The attribute follows the rules of WithContextFunc: it is placed at the top
// level, the record's attributes and those attached with With win over it, and
// among context values and extraction functions the first configured wins. get
// is not called for disabled records, nor when the key is already taken. The
// value is logged as any attribute value: a Secret is written [REDACTED]. A get
// that panics is replaced by the placeholder of extraction functions. An empty
// key, or a nil get, is invalid. Use WithContextFunc for a function that
// returns several attributes.
func WithContextValue[T any](key string, get func(ctx context.Context) (T, bool)) Option {
	return func(c *config) {
		switch {
		case strings.TrimSpace(key) == "":
			c.fail("WithContextValue", "empty key")
			return
		case get == nil:
			c.fail("WithContextValue", "function must not be nil")
			return
		}
		c.sources = append(c.sources, source{key: key, get: func(ctx context.Context) (slog.Value, bool) {
			v, ok := get(ctx)
			if !ok {
				return slog.Value{}, false
			}
			return slog.AnyValue(v), true
		}})
	}
}

// WithRedactKeys adds sensitive keys. The value of every attribute whose key
// equals one of them, compared without regard to case and at any depth, is
// written as [REDACTED]. It can be given several times: the keys accumulate.
func WithRedactKeys(keys ...string) Option {
	return func(c *config) {
		if len(keys) == 0 {
			c.fail("WithRedactKeys", "at least one key is required")
			return
		}
		for _, k := range keys {
			if strings.TrimSpace(k) == "" {
				c.fail("WithRedactKeys", "empty key")
				return
			}
		}
		c.keys = append(c.keys, keys...)
	}
}

// newConfig applies opts in order, and returns the first invalid option.
func newConfig(opts []Option) (config, error) {
	var c config
	for i, opt := range opts {
		if opt == nil {
			return config{}, invalid(fmt.Sprintf("options[%d]", i), "must not be nil")
		}
		opt(&c)
	}
	if c.err != nil {
		return config{}, c.err
	}
	return c, nil
}

// invalid returns an error that wraps ErrInvalidConfig, about subject: an
// option, or a parameter of New.
func invalid(subject, reason string) error {
	return fmt.Errorf("%w: New: %s: %s", ErrInvalidConfig, subject, reason)
}

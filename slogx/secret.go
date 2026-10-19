package slogx

import (
	"fmt"
	"io"
	"log/slog"
)

// Secret holds a value that renders as [REDACTED] wherever it ends up:
//
//   - as a log value, through any handler, with or without this package;
//   - with every fmt verb, also as a field of a struct;
//   - in JSON and in every other encoding that uses encoding.TextMarshaler,
//     also as a field of an encoded struct.
//
// Reveal is the only way to read the value. Secret keeps the value behind a
// pointer, so that fmt, which cannot call methods on unexported struct fields,
// prints an address rather than the value. Reflection-based printers that read
// unexported fields and follow pointers can still reach it.
//
// The zero Secret holds the zero value of T.
type Secret[T any] struct{ p *T }

// NewSecret returns a Secret that holds v.
func NewSecret[T any](v T) Secret[T] { return Secret[T]{p: &v} }

// Reveal returns the value held by s.
func (s Secret[T]) Reveal() T {
	if s.p == nil {
		var zero T
		return zero
	}
	return *s.p
}

// LogValue returns [REDACTED], so that every slog handler logs the secret as
// such.
func (Secret[T]) LogValue() slog.Value { return slog.StringValue(redacted) }

// String returns [REDACTED].
func (Secret[T]) String() string { return redacted }

// Format writes [REDACTED] for every verb, ignoring flags and width.
func (Secret[T]) Format(f fmt.State, _ rune) { _, _ = io.WriteString(f, redacted) }

// MarshalText returns [REDACTED]; encoding/json uses it, so a Secret encodes as
// the string "[REDACTED]".
func (Secret[T]) MarshalText() ([]byte, error) { return []byte(redacted), nil }

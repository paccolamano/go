package problem

import (
	"encoding/json"
	"fmt"
)

// Key is the name of an extension member whose value has the Go type T (RFC
// 9457 §3.2). Declare keys once, typically as package-level values with [Must]:
//
//	var Balance = problem.Must(problem.NewKey[int]("balance"))
//
// The zero Key is undeclared.
type Key[T any] struct {
	name string
}

// NewKey declares the name of an extension member. It returns an error wrapping
// [ErrInvalid] unless name follows RFC 9457 §3.2: a letter first, then letters,
// digits or '_', for three characters or more. The standard member names
// "type", "title", "status", "detail" and "instance" are refused in any
// capitalization, because some clients match member names without regard to
// case.
//
// The name "errors" is accepted, but [WithErrors] owns that member (RFC 9457 §3
// validation errors): when both set it, the option applied last wins.
func NewKey[T any](name string) (Key[T], error) {
	if !validExtensionName(name) {
		return Key[T]{}, fmt.Errorf("%w: extension name %q breaks RFC 9457 §3.2 or is a standard member name", ErrInvalid, name)
	}
	return Key[T]{name: name}, nil
}

// WithExtension sets the extension member k to v. The value is converted to
// JSON with encoding/json when the occurrence is built, so later changes to v
// do not affect the occurrence. A value that has no JSON representation, such
// as an infinite number, makes Write send the generic server error instead.
func WithExtension[T any](k Key[T], v T) Option {
	return func(e *Error) {
		if k.name == "" {
			e.fail(fmt.Errorf("%w: undeclared extension key", ErrInvalid))
			return
		}
		raw, err := json.Marshal(v)
		if err != nil {
			e.fail(fmt.Errorf("%w: extension %q: %w", ErrInvalid, k.name, err))
			return
		}
		if e.ext == nil {
			e.ext = make(map[string]json.RawMessage)
		}
		e.ext[k.name] = raw
	}
}

// Lookup returns the value of the extension member k in e. It returns the zero
// value and false when e is nil, when the member is absent, or when its value
// does not decode into T.
func (k Key[T]) Lookup(e *Error) (T, bool) {
	var v T
	if e == nil {
		return v, false
	}
	raw, ok := e.ext[k.name]
	if !ok || k.name == "" {
		return v, false
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		var zero T
		return zero, false
	}
	return v, true
}

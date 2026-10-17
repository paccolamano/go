package errors

import (
	stderrors "errors"
)

// ErrUnsupported indicates that a requested operation cannot be performed,
// because it is unsupported. It is the standard library's own value, so it
// matches errors from code that uses the standard errors package.
var ErrUnsupported = stderrors.ErrUnsupported

// Is reports whether any error in err's tree matches target. It behaves
// exactly like the standard library's errors.Is.
func Is(err, target error) bool { return stderrors.Is(err, target) }

// As finds the first error in err's tree that matches target, and if one is
// found, sets target to that error value and returns true. It behaves exactly
// like the standard library's errors.As.
//
// Prefer AsType: go vet checks the target of the standard errors.As only, so a
// wrong target passed to this function is reported only at run time, by a panic.
func As(err error, target any) bool { return stderrors.As(err, target) }

// AsType finds the first error in err's tree that matches the type E, and if
// one is found, returns that error value and true. It behaves exactly like the
// standard library's errors.AsType.
func AsType[E error](err error) (E, bool) { return stderrors.AsType[E](err) }

// Unwrap returns the result of calling the Unwrap method on err, if err's type
// contains an Unwrap method returning error. Otherwise, Unwrap returns nil. It
// behaves exactly like the standard library's errors.Unwrap.
func Unwrap(err error) error { return stderrors.Unwrap(err) }

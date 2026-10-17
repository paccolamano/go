package errors

import (
	"fmt"
)

// Skip attributes the location, and the call stack, of the errors built by its
// methods to a caller further up. It is meant for helper functions that build
// errors on behalf of their caller:
//
//	func invalid(field string) error {
//		return errors.Skip(1).Errorf("invalid %s: %w", field, ErrValidation)
//	}
//
// Level 0 is the function that calls the library. Negative values count as 0.
// Skip(0).X(...) is identical to the package-level X(...).
type Skip int

func (s Skip) n() int { return max(int(s), 0) }

// New is like the package-level New, located s levels above its caller.
func (s Skip) New(text string) error { return newText(1+s.n(), text) }

// Errorf is like the package-level Errorf, located s levels above its caller.
func (s Skip) Errorf(format string, args ...any) error {
	return newErrorf(1+s.n(), format, args...)
}

// Wrap is like the package-level Wrap, located s levels above its caller.
func (s Skip) Wrap(err error, text string) error {
	if err == nil {
		return nil
	}
	return newWrap(1+s.n(), err, text)
}

// Wrapf is like the package-level Wrapf, located s levels above its caller.
func (s Skip) Wrapf(err error, format string, args ...any) error {
	if err == nil {
		return nil
	}
	return newWrap(1+s.n(), err, fmt.Sprintf(format, args...))
}

// WithStack is like the package-level WithStack, located s levels above its
// caller.
func (s Skip) WithStack(err error) error {
	if err == nil {
		return nil
	}
	return newWrap(1+s.n(), err, "")
}

// Join is like the package-level Join, located s levels above its caller.
func (s Skip) Join(errs ...error) error { return newJoin(1+s.n(), errs) }

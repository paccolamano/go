package errors

import (
	"fmt"
	"strings"
)

// wrapError is an error with at most one cause: it is built by New, Errorf
// with zero or one %w, Wrap, Wrapf and WithStack.
type wrapError struct {
	msg    string
	cause  error
	pc     uintptr
	origin *callStack
}

func (e *wrapError) Error() string                 { return e.msg }
func (e *wrapError) Unwrap() error                 { return e.cause }
func (e *wrapError) Format(s fmt.State, verb rune) { formatError(e, s, verb) }
func (e *wrapError) trace() (uintptr, *callStack)  { return e.pc, e.origin }

// joinError is an error with one or more causes: it is built by Join and by
// Errorf with two or more %w.
type joinError struct {
	msg    string
	errs   []error
	pc     uintptr
	origin *callStack
}

func (e *joinError) Error() string                 { return e.msg }
func (e *joinError) Unwrap() []error               { return e.errs }
func (e *joinError) Format(s fmt.State, verb rune) { formatError(e, s, verb) }
func (e *joinError) trace() (uintptr, *callStack)  { return e.pc, e.origin }

// New returns an error whose message is text. It records the location of its
// caller and the call stack, unless it is called while packages are being
// initialized, which is how sentinel errors are declared.
func New(text string) error { return newText(1, text) }

// Errorf formats according to a format specifier and returns the result as an
// error, exactly like fmt.Errorf: every operand of a %w verb becomes a cause of
// the returned error. It records the location of its caller, and the call stack
// when none of the causes carries one.
func Errorf(format string, args ...any) error { return newErrorf(1, format, args...) }

// Wrap returns an error whose message is text, a colon, a space and the message
// of err, or the message of err alone when text is empty. Its cause is err. It
// records the location of its caller, and the call stack when nothing in err's
// tree carries one. Wrap returns nil if err is nil.
func Wrap(err error, text string) error {
	if err == nil {
		return nil
	}
	return newWrap(1, err, text)
}

// Wrapf is like Wrap, with a context formatted by fmt.Sprintf. The %w verb is
// not supported in the context, and go vet reports it. Wrapf returns nil if err
// is nil.
func Wrapf(err error, format string, args ...any) error {
	if err == nil {
		return nil
	}
	return newWrap(1, err, fmt.Sprintf(format, args...))
}

// WithStack returns an error with the same message as err, whose cause is err.
// It records the location of its caller, and the call stack when nothing in
// err's tree carries one. WithStack returns nil if err is nil.
func WithStack(err error) error {
	if err == nil {
		return nil
	}
	return newWrap(1, err, "")
}

// Join returns an error that wraps the given errors, exactly like the standard
// library's errors.Join: nil errors are discarded, Join returns nil if every
// value in errs is nil, and the message is the messages of the errors, one per
// line. It records the location of its caller, and the call stack when none of
// the errors carries one.
func Join(errs ...error) error { return newJoin(1, errs) }

func newText(skip int, text string) error {
	pc, origin := capture(skip+1, true)
	return &wrapError{msg: text, pc: pc, origin: origin}
}

func newErrorf(skip int, format string, args ...any) error {
	err := fmt.Errorf(format, args...)
	msg := err.Error()
	if m, ok := err.(interface{ Unwrap() []error }); ok {
		if errs := nonNil(m.Unwrap()); len(errs) > 0 {
			pc, origin := capture(skip+1, !anyTraced(errs))
			return &joinError{msg: msg, errs: errs, pc: pc, origin: origin}
		}
		pc, origin := capture(skip+1, true)
		return &wrapError{msg: msg, pc: pc, origin: origin}
	}
	var cause error
	if s, ok := err.(interface{ Unwrap() error }); ok {
		cause = s.Unwrap()
	}
	pc, origin := capture(skip+1, !isTraced(cause))
	return &wrapError{msg: msg, cause: cause, pc: pc, origin: origin}
}

func newWrap(skip int, err error, text string) error {
	msg := messageOf(err)
	if text != "" {
		msg = text + ": " + msg
	}
	pc, origin := capture(skip+1, !isTraced(err))
	return &wrapError{msg: msg, cause: err, pc: pc, origin: origin}
}

func newJoin(skip int, errs []error) error {
	list := nonNil(errs)
	if len(list) == 0 {
		return nil
	}
	if len(list) == len(errs) {
		list = append([]error(nil), list...)
	}
	var b strings.Builder
	for i, err := range list {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(messageOf(err))
	}
	pc, origin := capture(skip+1, !anyTraced(list))
	return &joinError{msg: b.String(), errs: list, pc: pc, origin: origin}
}

// messageOf returns err.Error(). If the Error method panics, it returns the
// placeholder that fmt prints in that case, as fmt.Errorf does for %w.
func messageOf(err error) string {
	msg, _ := safeMessage(err)
	return msg
}

// nonNil returns errs without its nil entries. It returns errs itself when it
// has no nil entry.
func nonNil(errs []error) []error {
	n := 0
	for _, err := range errs {
		if err != nil {
			n++
		}
	}
	if n == len(errs) {
		return errs
	}
	list := make([]error, 0, n)
	for _, err := range errs {
		if err != nil {
			list = append(list, err)
		}
	}
	return list
}

func anyTraced(errs []error) bool {
	for _, err := range errs {
		if isTraced(err) {
			return true
		}
	}
	return false
}

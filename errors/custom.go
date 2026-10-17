package errors

import (
	"fmt"
)

// BaseError is the building block of custom error types. Embed it by value in
// a struct type, use that type through a pointer, and initialize it with Bind:
//
//	type ConnError struct {
//		errors.BaseError
//		Addr string
//	}
//
//	func NewConnError(addr string, cause error) *ConnError {
//		return errors.Bind(&ConnError{Addr: addr}, cause, "connect "+addr)
//	}
//
// The custom type then has a message, an optional cause, a location and a
// detail like any error of this package, and callers find it with AsType. A
// custom type may define its own Error method: formatting and logging use it.
//
// A BaseError that never went through Bind has no message and no location.
type BaseError struct {
	self   error
	msg    string
	cause  error
	pc     uintptr
	origin *callStack
}

// Error returns the message given to Bind, composed with the message of the
// cause as Wrap composes it.
func (b *BaseError) Error() string { return b.msg }

// Unwrap returns the cause given to Bind, which may be nil.
func (b *BaseError) Unwrap() error { return b.cause }

// Format formats the custom error that embeds b: %+v writes its detail, and
// every other verb formats its message like a standard library error.
func (b *BaseError) Format(s fmt.State, verb rune) { formatError(b.outer(), s, verb) }

func (b *BaseError) trace() (uintptr, *callStack) { return b.pc, b.origin }

func (b *BaseError) base() *BaseError { return b }

// outer returns the custom error bound to b, or b itself before Bind.
func (b *BaseError) outer() error {
	if b.self != nil {
		return b.self
	}
	return b
}

// Custom is satisfied by every pointer to a struct type that embeds BaseError.
type Custom interface {
	error
	base() *BaseError
}

// Bind initializes the BaseError embedded in e with cause and text, and
// returns e. The message is text, followed by a colon, a space and the message
// of cause when cause is not nil. The location is the caller of the function
// that calls Bind, which is usually the constructor of the custom type; the
// call stack is recorded too when nothing in cause's tree carries one.
func Bind[E Custom](e E, cause error, text string) E {
	bind(2, e, cause, text)
	return e
}

// Bind is like the package-level Bind, located s levels above its caller: the
// package-level Bind is equivalent to Skip(1).Bind.
func (s Skip) Bind(e Custom, cause error, text string) error {
	bind(1+s.n(), e, cause, text)
	return e
}

func bind(skip int, e Custom, cause error, text string) {
	msg := text
	if cause != nil {
		msg = messageOf(cause)
		if text != "" {
			msg = text + ": " + msg
		}
	}
	pc, origin := capture(skip+1, !isTraced(cause))
	*e.base() = BaseError{self: e, msg: msg, cause: cause, pc: pc, origin: origin}
}

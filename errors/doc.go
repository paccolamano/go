// Package errors is a drop-in replacement for the standard library's errors
// package. It records where errors are created and wrapped, and describes error
// trees as text and as structured data.
//
// # Drop-in replacement
//
// Is, As, AsType, Unwrap, Join, New and ErrUnsupported behave like their
// standard counterparts: switching an import from "errors" to this package
// changes no message and no inspection result. ErrUnsupported is the standard
// library's own value. Prefer AsType to As: go vet checks the target of the
// standard errors.As only. When a file needs both packages, import the standard
// one under another name, such as stderrors.
//
// # Creating and wrapping errors
//
//   - New creates an error from a message.
//   - Errorf creates an error from a format, exactly like fmt.Errorf: every
//     operand of a %w verb becomes a cause.
//   - Wrap and Wrapf add context to an error: the message becomes the context,
//     a colon, a space and the message of the cause, as with fmt.Errorf("...: %w").
//     Wrapf does not support %w, and go vet reports it.
//   - WithStack records a location without changing the message. Use it for
//     errors returned by other packages, which carry no location.
//   - Join combines errors, exactly like the standard errors.Join.
//
// Wrapping, annotating and combining nil returns nil, so these functions can be
// applied unconditionally to a function's result. Messages are composed when the
// error is created, as fmt.Errorf does. When the Error method of a cause panics,
// the message holds the placeholder that fmt prints in that case.
//
// # Locations
//
// Every error built by this package records the location of the code that built
// it: the function, the file and the line. Files are named by their package
// import path, such as example.com/app/api/handler.go, so details do not depend
// on the directory where the program was built and never disclose local paths.
//
// The first error of a chain built by this package, its origin, also records the
// call stack that led to it, up to 32 frames. Later wraps record their location
// only, so each chain carries its call stack once, and wrapping stays cheap.
//
// Errors created while packages are being initialized record no location and no
// call stack. This is how sentinel errors are declared:
//
//	var ErrNotFound = errors.New("not found")
//
// The first wrap of a sentinel at run time then becomes the origin. Two cases
// keep a location although they happen during initialization: errors created in
// a goroutine started during initialization, and errors created at the bottom of
// a call stack deeper than 32 frames.
//
// Helpers that build errors on behalf of their caller use Skip to attribute the
// location to that caller: errors.Skip(1).Errorf(...). Level 0 is the function
// that calls this package.
//
// # Error detail
//
// Describe returns the detail of any error, including errors built by other
// packages: its levels, from the outermost to the root cause, each with the text
// it adds to the message, its type, its location and, for the origin, its call
// stack. Errors with several causes are described as branches. The types of this
// package and the generic types of the standard library (errors.New, fmt.Errorf,
// errors.Join) are not shown.
//
// The detail is available as text, through Detail.String and the %+v verb, and as
// JSON, through encoding/json with stable field names. Every other verb formats
// the message exactly like a standard library error with the same message.
//
// Describe follows at most 64 levels along any path and describes at most 256
// levels in total; a level whose causes are cut is marked as truncated. It never
// panics, even on errors whose Error or Unwrap method does, and terminates on
// self-referencing errors. In the text, lines that could be mistaken for a
// location, a marker or another level are written Go-quoted.
//
// # Custom error types
//
// Embed BaseError in a struct type and build values with Bind, so that the type
// has a message, a cause, a location and a detail without hand-written methods.
// Bind locates the error at the caller of the constructor. See BaseError.
//
// # Logging
//
// Attr returns a slog attribute with the key "error", holding the message and
// the detail of an error; DetailValue returns the same value for another key.
// The detail is computed only when a handler processes the record.
//
// Errors of this package logged as plain values appear exactly like standard
// library errors with the same message, even with handlers that print values with
// %+v: they implement slog.LogValuer and resolve to a view of themselves that has
// no Format method. That view unwraps to the original error, so handlers that
// inspect attribute values must use errors.Is or errors.As, not a type assertion.
// Loggers outside the standard library that print %+v for errors show the detail.
package errors

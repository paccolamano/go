package middleware

import (
	"errors"
	"net/http"
)

// Middleware wraps an [http.Handler] with additional behavior.
//
// It is a type alias, so any func(http.Handler) http.Handler, including
// middleware from other libraries, can be used wherever a Middleware is
// expected.
type Middleware = func(http.Handler) http.Handler

// Rejection describes a request that a middleware refuses or fails to serve.
//
// Fields may be added in minor releases; code that reads a Rejection keeps
// compiling and working.
type Rejection struct {
	// Status is the HTTP status code the middleware would send.
	Status int
	// Err identifies the cause. Compare it with errors.Is against the sentinel
	// error of the middleware that produced the rejection. It must not be
	// written to clients verbatim.
	Err error
}

// RejectFunc writes the response for a rejected request.
//
// Every middleware that can refuse or fail a request accepts a RejectFunc
// through its WithRejectFunc option. The middleware sets the protocol headers
// that belong to the condition (for example Retry-After) before calling it.
// If the function writes neither a status nor a body, the middleware sends
// rej.Status with an empty body.
type RejectFunc func(w http.ResponseWriter, r *http.Request, rej Rejection)

// ErrInvalidConfig is wrapped by every configuration error returned by the
// constructors of this module.
var ErrInvalidConfig = errors.New("middleware: invalid configuration")

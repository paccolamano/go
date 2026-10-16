package middleware

import (
	"fmt"
	"net/http"
	"slices"
)

// Chain is an immutable, ordered list of middleware. The first middleware is
// the outermost: it receives the request first and the response last.
//
// The zero value is an empty chain. Append and Extend return new chains and
// never modify the receiver, so a base chain can be shared and specialized for
// different route groups.
type Chain struct {
	mws []Middleware
}

// New returns a chain of the given middleware, in order.
//
// It panics if a middleware is nil: that is a programming error, typically a
// constructor whose error was ignored.
func New(mws ...Middleware) Chain {
	checkNil(mws, 0)
	return Chain{mws: slices.Clone(mws)}
}

// Append returns a new chain with mws added after the middleware of c. It
// panics if a middleware is nil.
func (c Chain) Append(mws ...Middleware) Chain {
	checkNil(mws, len(c.mws))
	return Chain{mws: slices.Concat(c.mws, mws)}
}

// Extend returns a new chain with the middleware of c followed by those of
// other.
func (c Chain) Extend(other Chain) Chain {
	return Chain{mws: slices.Concat(c.mws, other.mws)}
}

// Then applies the chain to h and returns the resulting handler. Each
// middleware is called once, here, and not on every request. For an empty
// chain Then returns h itself.
//
// It panics if h is nil.
func (c Chain) Then(h http.Handler) http.Handler {
	if h == nil {
		panic("middleware: nil handler") //nolint:forbidigo // programming error; mirrors net/http.ServeMux.Handle (research R-03)
	}
	for _, m := range slices.Backward(c.mws) {
		h = m(h)
	}
	return h
}

func checkNil(mws []Middleware, offset int) {
	for i, m := range mws {
		if m == nil {
			panic(fmt.Sprintf("middleware: nil middleware at position %d", offset+i)) //nolint:forbidigo // programming error; mirrors net/http.ServeMux.Handle (research R-03)
		}
	}
}

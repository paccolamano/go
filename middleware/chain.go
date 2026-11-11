package middleware

import (
	"errors"
	"fmt"
	"net/http"
	"slices"
)

// Chain is an immutable, ordered list of middleware. The first middleware is
// the outermost: it receives the request first and the response last.
//
// The zero value is an empty chain. Append, Use and AppendChain return new
// chains and never modify the receiver, so a base chain can be shared and
// specialized for different route groups.
//
// A chain also records the errors of the constructors added with Use. Check
// them once, with Err, on the most derived chain before calling Then: Then
// panics on a chain with recorded errors.
type Chain struct {
	// mws and errs are never modified after the chain is built: every
	// derivation copies them into new arrays (slices.Concat), so chains
	// derived from the same base never share an array they write to.
	mws  []Middleware
	errs []error
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
	return Chain{mws: slices.Concat(c.mws, mws), errs: c.errs}
}

// Use returns a new chain with the result of one constructor added after the
// middleware of c. It takes the two results directly:
//
//	chain = chain.Use(requestid.New())
//
// When err is not nil, mw is not added and err is recorded in the new chain,
// to be reported by [Chain.Err]. When err is nil, Use panics if mw is nil: the
// constructor has a bug.
func (c Chain) Use(mw Middleware, err error) Chain {
	if err != nil {
		return Chain{mws: c.mws, errs: slices.Concat(c.errs, []error{err})}
	}
	return c.Append(mw)
}

// AppendChain returns a new chain with the middleware of c followed by those
// of other, so the middleware of c stay outermost. The errors recorded in
// other follow those of c.
func (c Chain) AppendChain(other Chain) Chain {
	return Chain{mws: slices.Concat(c.mws, other.mws), errs: slices.Concat(c.errs, other.errs)}
}

// Err returns the errors recorded by [Chain.Use] in c and in the chains it
// derives from, joined with [errors.Join] in the order they were recorded, or
// nil if there is none. Chains derived from a common base do not see each
// other's errors.
func (c Chain) Err() error {
	return errors.Join(c.errs...)
}

// Then applies the chain to h and returns the resulting handler. Each
// middleware is called once, here, and not on every request. For an empty
// chain Then returns h itself.
//
// It panics if c has recorded errors (see [Chain.Err]), listing them, and if
// h is nil.
func (c Chain) Then(h http.Handler) http.Handler {
	if len(c.errs) > 0 {
		panic(fmt.Errorf("middleware: chain has configuration errors:\n%w", errors.Join(c.errs...))) //nolint:forbidigo // programming error: Err was not checked (spec 014 research R-06)
	}
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

package problem

import "fmt"

// Type is a problem type: the type URI that identifies it, a title that is the
// same for every occurrence, and the status code of every occurrence (RFC 9457
// §4). Declare types once, typically as package-level values with [Must]:
//
//	var OutOfCredit = problem.Must(problem.NewType(
//		"https://example.com/probs/out-of-credit",
//		"You do not have enough credit.",
//		http.StatusForbidden,
//	))
//
// The zero Type is undeclared.
type Type struct {
	uri    string
	title  string
	status int
}

// NewType declares a problem type. It returns an error wrapping [ErrInvalid],
// and naming the invalid element, unless:
//
//   - uri is a URI reference that is absolute or begins with "/", as RFC 9457
//     §3.1.1 recommends, and is not "about:blank", which [Status] covers;
//   - title is not empty;
//   - status is between 400 and 599.
//
// Non-ASCII characters in uri must be percent-encoded.
func NewType(uri, title string, status int) (Type, error) {
	switch {
	case uri == blankType:
		return Type{}, fmt.Errorf("%w: type URI %q: use Status for about:blank", ErrInvalid, uri)
	case !isTypeURI(uri):
		return Type{}, fmt.Errorf("%w: type URI %q is neither an absolute URI nor a reference beginning with \"/\"", ErrInvalid, uri)
	case title == "":
		return Type{}, fmt.Errorf("%w: type %q: empty title", ErrInvalid, uri)
	case status < 400 || status > 599:
		return Type{}, fmt.Errorf("%w: type %q: status %d is outside 400-599", ErrInvalid, uri, status)
	}
	return Type{uri: uri, title: title, status: status}, nil
}

// New returns an occurrence of t, with the options applied in order. An
// occurrence of the zero Type cannot be written: Write sends the generic server
// error instead.
func (t Type) New(opts ...Option) *Error {
	e := &Error{typ: t.uri, title: t.title, status: t.status}
	if t.uri == "" {
		e.fail(fmt.Errorf("%w: undeclared type", ErrInvalid))
	}
	e.apply(opts)
	return e
}

// Match returns the first problem of type t in err's tree, and true. Unlike
// errors.As, it looks past problems of other types. The tree is walked in the
// order errors.As uses. Two problems are of the same type when their type URIs
// are equal; a problem read with [Read] has its type URI resolved first, so a
// type declared to match another API's problems must use that API's absolute
// URI.
func (t Type) Match(err error) (*Error, bool) {
	if t.uri == "" {
		return nil, false
	}
	return match(err, t.uri)
}

// match walks err's tree depth first, in the order errors.As uses.
func match(err error, uri string) (*Error, bool) {
	switch x := err.(type) { //nolint:errorlint // walks the tree by hand, in errors.As order, to look past problems of other types.
	case nil:
		return nil, false
	case *Error:
		switch {
		case x == nil:
			return nil, false
		case x.typ == uri:
			return x, true
		}
		return match(x.cause, uri)
	case interface{ Unwrap() error }:
		return match(x.Unwrap(), uri)
	case interface{ Unwrap() []error }:
		for _, c := range x.Unwrap() {
			if e, ok := match(c, uri); ok {
				return e, true
			}
		}
	}
	return nil, false
}

// Must returns v, and panics with err when err is not nil. It is meant for
// package-level declarations of problem types and extension names, whose
// arguments are fixed in the source code: an invalid declaration then stops the
// program when it starts, and any test of the declaring package reports it.
func Must[T any](v T, err error) T {
	if err != nil {
		panic(err) //nolint:forbidigo // Must is for static declarations; an invalid one is a programming error, as with regexp.MustCompile.
	}
	return v
}

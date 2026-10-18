package problem

import "fmt"

// Option sets one member of an occurrence while [Type.New] or [Status] builds
// it. Options apply in order: for the same member, the last one wins. Nil
// options are ignored.
type Option func(*Error)

// WithDetail sets the explanation of this occurrence. It should help the client
// correct the problem, not debug the server. An empty text clears the detail.
func WithDetail(text string) Option {
	return func(e *Error) { e.detail = text }
}

// WithTitle replaces the title of the problem type. RFC 9457 §3.1.3 allows the
// title to change between occurrences only for localization, so use it only to
// translate the title. An empty text keeps the current title.
func WithTitle(text string) Option {
	return func(e *Error) {
		if text != "" {
			e.title = text
		}
	}
}

// WithCause sets the internal cause of the occurrence: the error that led to
// it. The cause is part of the error tree, so errors.Is and errors.As find it
// and logs show it, but it is never written in a response.
func WithCause(err error) Option {
	return func(e *Error) { e.cause = err }
}

// WithInstance sets the URI reference that identifies this occurrence, such as
// "/account/12345/msgs/abc" or "urn:uuid:…". An empty uri clears the instance.
// A uri that is not a URI reference, for example because it contains spaces or
// non-ASCII characters that are not percent-encoded, makes Write send the
// generic server error instead.
func WithInstance(uri string) Option {
	return func(e *Error) {
		if uri != "" && !validURIReference(uri) {
			e.fail(fmt.Errorf("%w: instance %q is not a URI reference", ErrInvalid, uri))
			return
		}
		e.instance = uri
	}
}

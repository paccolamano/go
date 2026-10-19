package problem

import (
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strings"
)

// MediaType is the media type of a problem document (RFC 9457 §3).
const MediaType = "application/problem+json"

// blankType is the problem type of a problem that the status code describes
// entirely (RFC 9457 §4.2.1).
const blankType = "about:blank"

var (
	// ErrInvalid is wrapped by every error that reports a declaration or an
	// occurrence breaking the rules of RFC 9457 or of this package.
	ErrInvalid = errors.New("problem: invalid")
	// ErrNotProblem is wrapped by the error Read returns for a response that is
	// not a problem document. The response body is not read.
	ErrNotProblem = errors.New("problem: not a problem document")
	// ErrMalformed is wrapped by the error Read and Parse return for a document
	// that is too large, is not valid JSON or is not a JSON object.
	ErrMalformed = errors.New("problem: malformed problem document")
)

// Error is an occurrence of a problem: the problem details of one failed
// request. It is also a Go error, so it can be returned, wrapped and combined
// like any other error, and Write finds it in an error tree.
//
// An Error is immutable once built, and safe for concurrent use. Build one with
// [Type.New] or [Status], or read one with [Read] or [Parse].
type Error struct {
	typ      string
	title    string
	status   int
	detail   string
	instance string
	ext      map[string]json.RawMessage
	cause    error
	// invalid is the first failure recorded while the occurrence was built.
	// Write reports it and sends the generic server error instead.
	invalid error
}

// Status returns an occurrence of the "about:blank" problem type, which
// describes a problem entirely through its status code. Its title is the
// recommended phrase of code, such as "Not Found" for 404, or absent when code
// has none. Write accepts only codes from 400 to 599.
func Status(code int, opts ...Option) *Error {
	e := &Error{typ: blankType, title: http.StatusText(code), status: code}
	e.apply(opts)
	return e
}

// Type returns the problem type URI. A problem read from a document without a
// valid type has the type "about:blank".
func (e *Error) Type() string { return e.typ }

// Title returns the short summary of the problem type, or "" when absent.
func (e *Error) Title() string { return e.title }

// Status returns the status code of the problem, or 0 when absent.
func (e *Error) Status() int { return e.status }

// Detail returns the explanation of this occurrence, or "" when absent.
func (e *Error) Detail() string { return e.detail }

// Instance returns the URI reference of this occurrence, or "" when absent.
func (e *Error) Instance() string { return e.instance }

// Extensions returns the extension members of the problem as compact JSON
// values, keyed by member name. The map is a new copy on every call, and is
// never nil.
func (e *Error) Extensions() map[string]json.RawMessage {
	out := make(map[string]json.RawMessage, len(e.ext))
	for name, raw := range e.ext {
		out[name] = slices.Clone(raw)
	}
	return out
}

// Error returns the title and the detail that are not empty, joined by ": ",
// or the type URI when both are empty. When the occurrence has an internal
// cause, ": " and the cause's message follow, as with fmt.Errorf("...: %w").
// The message is meant for logs: Write never sends it.
func (e *Error) Error() string {
	parts := make([]string, 0, 3)
	for _, s := range []string{e.title, e.detail} {
		if s != "" {
			parts = append(parts, s)
		}
	}
	if len(parts) == 0 {
		parts = append(parts, e.typ)
	}
	if e.cause != nil {
		parts = append(parts, e.cause.Error())
	}
	return strings.Join(parts, ": ")
}

// Unwrap returns the internal cause set with [WithCause], or nil.
func (e *Error) Unwrap() error { return e.cause }

// With returns a copy of e with the options applied in order. e is not
// modified.
//
// The copy owns its extension values, so concurrent calls on the same problem
// are safe, and no change made through one problem reaches the other. The copy
// keeps the internal cause, so errors.Is, errors.As and [Type.Match] find the
// same errors in it, unless an option sets a new cause. A failure recorded while
// e was built carries over, and [Write] reports it. A failure caused by opts is
// recorded on the copy only. With on a nil *Error returns nil.
//
// With lets the HTTP layer add per-request members, such as a request ID, to a
// problem that another layer built.
func (e *Error) With(opts ...Option) *Error {
	if e == nil {
		return nil
	}
	c := *e
	if e.ext != nil {
		c.ext = make(map[string]json.RawMessage, len(e.ext))
		for name, raw := range e.ext {
			c.ext[name] = slices.Clone(raw)
		}
	}
	c.apply(opts)
	return &c
}

// apply applies the options in order, skipping nil ones.
func (e *Error) apply(opts []Option) {
	for _, opt := range opts {
		if opt != nil {
			opt(e)
		}
	}
}

// fail records err unless a failure is already recorded.
func (e *Error) fail(err error) {
	if e.invalid == nil {
		e.invalid = err
	}
}

// serverError returns the generic server error: the "about:blank" problem with
// status 500 and no other member.
func serverError() *Error {
	return &Error{
		typ:    blankType,
		title:  http.StatusText(http.StatusInternalServerError),
		status: http.StatusInternalServerError,
	}
}

# Contract: Public Go API

**Module**: `github.com/paccolamano/go/problem` | **Package**: `problem` | **Minimum Go**: 1.26

This file lists the complete exported surface of the module at its first release. Anything not
listed here is unexported. Later releases may only **add** identifiers, options, or methods
(Principle II). The exact doc-comment wording is decided at implementation time. The wire format
produced and read by these functions is in [document.md](document.md).

## Constants and sentinel errors

```go
// MediaType is the media type of a problem document (RFC 9457 §3).
const MediaType = "application/problem+json"

var (
    ErrInvalid    = errors.New("problem: invalid") // declaration or occurrence breaks RFC 9457 rules
    ErrNotProblem = errors.New("problem: not a problem document")
    ErrMalformed  = errors.New("problem: malformed problem document")
)
```

Every error the package returns wraps exactly one of these sentinels or an underlying I/O error
(see the sentinel table in [data-model.md](../data-model.md#sentinel-errors)).

## Declarations (FR-006, FR-008)

```go
// Type is a problem type: a type URI, a title and a status code. The zero Type is undeclared.
type Type struct{ /* unexported */ }

// NewType declares a problem type. uri must be an absolute URI or begin with "/", and must not
// be "about:blank"; title must not be empty; status must be between 400 and 599.
func NewType(uri, title string, status int) (Type, error)

// Key is the name of an extension member whose value has Go type T. The zero Key is undeclared.
type Key[T any] struct{ /* unexported */ }

// NewKey declares an extension member name: at least 3 characters, a letter first, then
// letters, digits or '_', and not a standard member name in any capitalization.
func NewKey[T any](name string) (Key[T], error)

// Must returns v, or panics with err. It is meant for package-level declarations.
func Must[T any](v T, err error) T
```

Typical declarations:

```go
var (
    OutOfCredit = problem.Must(problem.NewType(
        "https://example.com/probs/out-of-credit",
        "You do not have enough credit.",
        http.StatusForbidden,
    ))
    Balance  = problem.Must(problem.NewKey[int]("balance"))
    Accounts = problem.Must(problem.NewKey[[]string]("accounts"))
)
```

## Occurrences (FR-004, FR-005, FR-007, FR-009, FR-010, FR-018)

```go
// New returns an occurrence of t.
func (t Type) New(opts ...Option) *Error

// Status returns an occurrence of about:blank with the given status code. Its title is the
// recommended phrase of the code, absent when the code has none.
func Status(code int, opts ...Option) *Error

// Option sets one member of an occurrence while it is built. Options apply in order.
type Option func(*Error)

func WithDetail(text string) Option                // empty clears
func WithTitle(text string) Option                 // localization only; empty keeps the title
func WithInstance(uri string) Option               // must be a URI reference; empty clears
func WithCause(err error) Option                   // internal cause, never sent
func WithExtension[T any](k Key[T], v T) Option    // v is marshaled with encoding/json now
```

Typical occurrence:

```go
return OutOfCredit.New(
    problem.WithDetail("Your current balance is 30, but that costs 50."),
    problem.WithInstance("/account/12345/msgs/abc"),
    problem.WithExtension(Balance, 30),
    problem.WithExtension(Accounts, []string{"/account/12345", "/account/67890"}),
    problem.WithCause(err),
)
```

## The occurrence type (FR-017, FR-018, FR-024)

```go
// Error is an occurrence of a problem. It is immutable and safe for concurrent use.
type Error struct{ /* unexported */ }

func (e *Error) Type() string                             // type URI
func (e *Error) Title() string                            // "" when absent
func (e *Error) Status() int                              // 0 when absent
func (e *Error) Detail() string                           // "" when absent
func (e *Error) Instance() string                         // "" when absent
func (e *Error) Extensions() map[string]json.RawMessage   // fresh deep copy, never nil
func (e *Error) Error() string                            // "title: detail[: cause]"
func (e *Error) Unwrap() error                            // the internal cause

// Lookup returns the value of k in e, decoded into T. It returns the zero value and false
// when the member is absent or does not decode into T.
func (k Key[T]) Lookup(e *Error) (T, bool)

// Match returns the first problem of type t in err's tree, in errors.As order.
func (t Type) Match(err error) (*Error, bool)
```

`*Error` implements `error` only. It deliberately implements neither `json.Marshaler`,
`slog.LogValuer` nor `fmt.Formatter` (research R-08).

## Writing (FR-011 to FR-016, FR-019, FR-020)

```go
// Write answers the request with err as a problem document.
//
//   - The first *Error in err's tree (errors.As order) is written as is.
//   - Any other error, and a nil error, is answered with the generic server error
//     (500, about:blank, "Internal Server Error").
//   - A problem that cannot be written is replaced by the generic server error.
//
// Headers already set on w are kept, except Content-Type, which becomes MediaType. A HEAD
// request gets no body. The returned error reports a replaced problem (wrapping ErrInvalid)
// and any failure to write the body; it is nil when err was written as intended.
func Write(w http.ResponseWriter, r *http.Request, err error) error
```

Typical handler:

```go
func (h *Handler) send(w http.ResponseWriter, r *http.Request) {
    if err := h.svc.Send(r.Context(), msg); err != nil {
        h.log.ErrorContext(r.Context(), "send message", "err", err)
        _ = problem.Write(w, r, err)
        return
    }
    // ...
}
```

With the repository's `middleware` module (SC-007), no dependency in either direction:

```go
reject := func(w http.ResponseWriter, r *http.Request, rej middleware.Rejection) {
    _ = problem.Write(w, r, problem.Status(rej.Status))
}
```

## Reading (FR-022 to FR-028)

```go
// Read reads the problem document in resp's body. It returns ErrNotProblem, without reading
// the body, when the media type is not MediaType. Relative type and instance members are
// resolved against resp.Request.URL. Read does not close the body.
func Read(resp *http.Response) (*Error, error)

// Parse reads a problem document from data, without resolving relative URIs.
func Parse(data []byte) (*Error, error)
```

Typical client:

```go
resp, err := client.Do(req)
if err != nil { return err }
defer resp.Body.Close()
if resp.StatusCode >= 400 {
    p, err := problem.Read(resp)
    switch {
    case errors.Is(err, problem.ErrNotProblem):
        return fmt.Errorf("upstream: %s", resp.Status)
    case err != nil:
        return fmt.Errorf("upstream: %w", err)
    }
    if _, ok := OutOfCredit.Match(p); ok {
        balance, _ := Balance.Lookup(p)
        // ...
    }
    return p
}
```

## Compatibility notes

- New members of the problem representation arrive as new `With*` options and accessors.
- New sentinels may be added, and existing ones keep their identity.
- The document layout of [document.md](document.md), the limits of
  [research R-12](../research.md#r-12-limits-and-defaults) and the message format of `Error()`
  are documented behavior. Changing them after v1.0.0 is a major change.

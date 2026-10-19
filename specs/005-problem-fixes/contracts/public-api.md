# Contract: Public Go API Changes

**Module**: `github.com/paccolamano/go/problem` | **Package**: `problem` | **Minimum Go**: 1.26
| **From**: `problem/v0.1.0` | **To**: `problem/v0.2.0`

This contract lists the changes to the exported surface of the module, relative to
[004 contracts/public-api.md](../../004-problem-details/contracts/public-api.md):

- what the release adds, all additions with nothing removed or renamed;
- the documented behavior that changes.

The exact wording of the doc comments is decided at implementation time, but it must state
every rule below. The wire format is in [document.md](document.md).

## Added

### Deriving a problem (FR-005 to FR-010)

```go
// With returns a copy of e with the options applied in order. e is not modified.
//
// The copy owns its extension values, so concurrent calls on the same problem are safe, and
// no change made through one problem reaches the other. The copy keeps the internal cause,
// so errors.Is, errors.As and Type.Match find the same errors in it, unless an option sets
// a new cause. A failure recorded while e was built carries over, and Write reports it. A
// failure caused by opts is recorded on the copy only. With on a nil *Error returns nil.
func (e *Error) With(opts ...Option) *Error
```

Typical use: the HTTP layer adds per-request members to a problem that the service layer built.

```go
var RequestID = problem.Must(problem.NewKey[string]("request_id"))

func writeProblem(w http.ResponseWriter, r *http.Request, err error) {
    if p, ok := errors.AsType[*problem.Error](err); ok {
        if id, ok := requestIDFrom(r.Context()); ok {
            err = p.With(problem.WithExtension(RequestID, id))
        }
    }
    if werr := problem.Write(w, r, err); werr != nil {
        slog.ErrorContext(r.Context(), "write problem", "err", werr)
    }
}
```

### Validation errors (FR-011 to FR-018)

```go
// Item is one error of a validation problem (RFC 9457 §3): an explanation for the client
// and, optionally, a JSON Pointer to the member of the request that caused it.
type Item struct {
    Detail  string `json:"detail"`
    Pointer string `json:"pointer,omitempty"`
}

// WithErrors sets the "errors" extension member to items, in order. With no items, the
// member is an empty list. Pointers are written as given: build them with Pointer.
//
// WithErrors owns the "errors" member. A key named "errors", declared with NewKey, sets the
// same member: whichever option is applied last wins.
func WithErrors(items ...Item) Option

// Errors returns the items of the "errors" extension member of e, in order, and true. It
// returns nil and false when e is nil, when the member is absent, and when its value is not
// an array of objects whose "detail" and "pointer" members, when present, are strings.
// Members are matched by their exact names. Other members of the items are ignored, and
// Extensions still returns them.
func (e *Error) Errors() ([]Item, bool)

// Pointer returns the JSON Pointer (RFC 6901), in URI fragment form, to the member reached
// through the given reference tokens: Pointer("items", "0", "qty") is "#/items/0/qty", and
// Pointer() is "#", the whole document.
//
// In each token, "~" becomes "~0" and "/" becomes "~1" (RFC 6901 §3). Then every byte that
// a URI fragment does not allow, including "%" and the bytes of non-ASCII characters, is
// percent-encoded (RFC 6901 §6): Pointer("città") is "#/citt%C3%A0". Tokens are not
// interpreted: pass an array index as decimal text.
func Pointer(tokens ...string) string
```

Typical use: a validation function that answers with one 422 problem.

```go
var ValidationError = problem.Must(problem.NewType(
    "https://example.net/validation-error",
    "Your request is not valid.",
    http.StatusUnprocessableEntity,
))

func validate(req CreateUser) error {
    var items []problem.Item
    if req.Age <= 0 {
        items = append(items, problem.Item{Detail: "must be a positive integer", Pointer: problem.Pointer("age")})
    }
    if !slices.Contains([]string{"green", "red", "blue"}, req.Profile.Color) {
        items = append(items, problem.Item{
            Detail:  "must be 'green', 'red' or 'blue'",
            Pointer: problem.Pointer("profile", "color"),
        })
    }
    if len(items) > 0 {
        return ValidationError.New(problem.WithErrors(items...))
    }
    return nil
}
```

## Changed behavior

### `Write` (FR-001 to FR-003)

The paragraph about headers in the doc comment becomes:

```go
// The document is complete before anything is written. Write removes the headers that
// describe the body the handler meant to send: Content-Length, Content-Encoding,
// Content-Range, ETag, Last-Modified, Content-Disposition and Trailer. It sets Content-Type to
// [MediaType], and keeps every other header already set on w, such as Retry-After,
// Content-Language, Cache-Control and Vary. A HEAD request gets the status and the headers,
// and no body. Once the response has started, its status and headers can no longer change:
// call Write before writing anything else.
```

The signature and the returned error are unchanged. The package documentation states the same
rule.

### `Key.Lookup` (FR-004)

```go
// Lookup returns the value of the extension member k in e. It returns the zero value and
// false when e is nil, when the member is absent, or when its value does not decode into T.
func (k Key[T]) Lookup(e *Error) (T, bool)
```

Before this release, a nil `e` caused a panic.

### `NewKey` (FR-017)

The rules do not change: `errors` is still accepted. The doc comment adds:

```go
// The name "errors" is accepted, but WithErrors owns that member (RFC 9457 §3 validation
// errors): when both set it, the option applied last wins.
```

## Compatibility

- Every change is an addition, or a fix of documented behavior. `gorelease` against
  `problem/v0.1.0` must report only compatible changes, which gives the MINOR release
  `v0.2.0` (research R-01).
- The output of `Pointer` and the layout of the `errors` member are now documented behavior,
  and the reading rules of `Errors` too. After v1.0.0, changing them is a major change.
- `Item` has exported fields and no methods. Adding fields later is compatible for callers that
  use keyed literals, which is the form every example uses.

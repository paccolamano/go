# Contract: Public Go API

**Module**: `github.com/paccolamano/go/errors` | **Package**: `errors` | **Minimum Go**: 1.26

This file lists the complete exported surface of the module at its first release. Anything not
listed here is unexported. Later releases may only **add** identifiers, fields of the description
structs, or methods of `Custom` (Principle II). The exact doc-comment wording is decided at
implementation time.

## Drop-in parity with the standard package (FR-001)

Code that imports the standard `errors` package compiles unchanged after its import is switched
to this module, and behaves the same (User Story 1, scenario 8).

```go
// Forwarders: behavior identical to the standard functions.
func Is(err, target error) bool
func As(err error, target any) bool      // prefer AsType: vet's errorsas check covers only the standard As
func AsType[E error](err error) (E, bool)
func Unwrap(err error) error

// ErrUnsupported is the standard library's own value, not a copy.
var ErrUnsupported = stderrors.ErrUnsupported
```

## Creating, wrapping, annotating and combining

Every function below records a location (FR-015). It also records the call stack when nothing it
wraps already carries one (FR-016). An error created during package initialization records
neither (FR-008).

```go
func New(text string) error                          // message: text
func Errorf(format string, args ...any) error        // message and causes exactly as fmt.Errorf, %w included
func Wrap(err error, text string) error              // nil → nil; message: text + ": " + err.Error()
func Wrapf(err error, format string, args ...any) error // nil → nil; context via fmt.Sprintf; vet rejects %w
func WithStack(err error) error                      // nil → nil; message unchanged
func Join(errs ...error) error                       // standard Join semantics: nils dropped, all-nil → nil
```

| Call | Result type | `Unwrap` |
|------|-------------|----------|
| `New` | library node | `Unwrap() error` returns nil |
| `Errorf` without `%w` | library node | `Unwrap() error` returns nil |
| `Errorf` with one `%w` | library node | `Unwrap() error` returns the referenced error itself |
| `Errorf` with two or more `%w` | library node | `Unwrap() []error`, the referenced errors in order, as fmt returns them |
| `Wrap`, `Wrapf`, `WithStack` | library node | `Unwrap() error` returns `err` |
| `Join` | library node | `Unwrap() []error`, the non-nil arguments in order |

Every library node also implements:

- `fmt.Formatter` ([detail-format.md](detail-format.md#formatting-verbs));
- `slog.LogValuer`, which returns the plain view ([log-attributes.md](log-attributes.md#plain-values)).

## Caller skipping (FR-017)

```go
// Skip attributes the location (and call stack) to the n-th caller above the
// function that calls the method. Skip(0).X is identical to X. Negative values count as 0.
type Skip int

func (s Skip) New(text string) error
func (s Skip) Errorf(format string, args ...any) error
func (s Skip) Wrap(err error, text string) error
func (s Skip) Wrapf(err error, format string, args ...any) error
func (s Skip) WithStack(err error) error
func (s Skip) Join(errs ...error) error
func (s Skip) Bind(e Custom, cause error, text string) error // level s; package-level Bind == Skip(1).Bind
```

Typical helper:

```go
func invalid(field string) error {
    return errors.Skip(1).Errorf("invalid %s: %w", field, ErrValidation) // location: invalid's caller
}
```

## Custom error types (FR-026 to FR-029)

```go
// BaseError is embedded by value in developer-defined error types. Use the outer
// type through a pointer, and initialize it with Bind.
type BaseError struct{ /* unexported */ }

func (b *BaseError) Error() string                 // message composed as Wrap does
func (b *BaseError) Unwrap() error                 // the cause (may be nil)
func (b *BaseError) Format(s fmt.State, verb rune) // acts on the bound outer value
func (b *BaseError) LogValue() slog.Value          // plain view of the bound outer value

// Custom is satisfied by every pointer to a struct type that embeds BaseError.
type Custom interface {
    error
    // contains filtered or unexported methods
}

// Bind initializes the BaseError embedded in e and returns e. The location is the
// caller of the function that calls Bind (level 1).
func Bind[E Custom](e E, cause error, text string) E
```

Typical use (SC-007: one embedded field and one constructor, with no hand-written methods):

```go
type ConnError struct {
    errors.BaseError
    Addr string
}

func NewConnError(addr string, cause error) *ConnError {
    return errors.Bind(&ConnError{Addr: addr}, cause, "connect "+addr)
}

// Callers branch on it:
if ce, ok := errors.AsType[*ConnError](err); ok { retry(ce.Addr) }
```

A type may define its own `Error()`. Formatting and logging then use it, because `BaseError`
acts on the bound outer value (research R-13).

## Error detail (FR-019 to FR-025)

```go
// Describe returns the detail of any error, including errors from other packages.
// Describe(nil) returns a Detail with no levels.
func Describe(err error) Detail

type Detail struct {
    Levels []Level `json:"levels"`
}

func (d Detail) String() string // text layout of detail-format.md; "" for no levels

type Level struct {
    Text      string    `json:"text"`
    Type      string    `json:"type,omitempty"`
    Location  *Location `json:"location,omitempty"`
    Stack     *Stack    `json:"stack,omitempty"`
    Branches  [][]Level `json:"branches,omitempty"`
    Truncated bool      `json:"truncated,omitempty"`
}

type Location struct {
    Function string `json:"function"`
    File     string `json:"file"`
    Line     int    `json:"line"`
}

type Stack struct {
    Frames    []Location `json:"frames"`
    Truncated bool       `json:"truncated,omitempty"`
}
```

The field semantics are in [../data-model.md](../data-model.md#exported-description-values), and
the text and JSON layouts in [detail-format.md](detail-format.md).

## Logging with slog (FR-030)

```go
// Attr returns the attribute "error" holding the message and the structured detail
// of err, computed only when a handler processes the record. Attr(nil) adds nothing.
func Attr(err error) slog.Attr

// DetailValue returns the same value without a key, so the caller can choose it:
//     slog.Any("err", errors.DetailValue(err))
func DetailValue(err error) slog.Value
```

The shapes are in [log-attributes.md](log-attributes.md).

## Not exported

The following are deliberately unexported:

- the node types;
- the call-stack representation;
- the capture helpers;
- the limits: 32 frames, 64 levels of depth, 256 levels in total;
- the default slog key.

The limits and the key are documented in the package doc (FR-005) but are not part of the API,
so they can change in a minor release, with a CHANGELOG entry.

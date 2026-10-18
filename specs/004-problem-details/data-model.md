# Data Model: RFC 9457 Problem Details Module

**Feature**: [spec.md](spec.md) | **Plan**: [plan.md](plan.md) | **Date**: 2026-10-18

This module is a library and stores nothing persistently. Its data is:

- the declarations a developer makes once (problem types and extension names);
- the occurrences built for each failure or read from a response;
- the problem document, which is the wire form of an occurrence.

Every value is immutable once its constructor returns. The exported signatures are listed in
[contracts/public-api.md](contracts/public-api.md), and the wire format is in
[contracts/document.md](contracts/document.md).

## Declarations

### Type (problem type)

| Field (unexported) | Meaning | Rule, checked by `NewType` |
|--------------------|---------|----------------------------|
| `uri string` | the type URI, which identifies the type (RFC 9457 §3.1.1) | a valid URI reference ([research R-04](research.md#r-04-uri-reference-validation)); absolute or beginning with `/`; not `about:blank` |
| `title string` | a short summary that is the same for every occurrence | not empty |
| `status int` | the status code of every occurrence | between 400 and 599 |

- **Zero value**: `Type{}` is "undeclared". Its occurrences carry a recorded failure, and it
  never matches.
- **Identity**: two types are the same problem type when their URIs are equal. Declaring the
  same URI twice yields two equal values (spec, Edge Cases).
- **Comparability**: `Type` is a small comparable value, safe to copy and to share between
  goroutines.

### Key[T] (extension name)

| Field (unexported) | Meaning | Rule, checked by `NewKey` |
|--------------------|---------|---------------------------|
| `name string` | the member name | 3 or more characters, a letter first, then letters, digits or `_`; not `type`, `title`, `status`, `detail` or `instance` in any capitalization |

- **Value type**: the type parameter `T` is the Go type of the member's value, used to marshal it
  when building an occurrence and to unmarshal it in `Lookup`.
- **Zero value**: `Key[T]{}` is "undeclared". `WithExtension` with it records a failure, and
  `Lookup` with it returns the zero value and false.

## Occurrence: `Error`

`*Error` is both a problem and a Go `error`.

| Field (unexported) | Meaning | Produced occurrence | Read occurrence |
|--------------------|---------|---------------------|-----------------|
| `typ string` | the type URI | the type's URI, or `about:blank` for `Status` | the `type` member, resolved (R-10); `about:blank` when missing or invalid |
| `title string` | the title | the type's title, or `http.StatusText(code)` for `Status`; replaced by `WithTitle` | the `title` member; empty when missing or not a string |
| `status int` | the status code | the type's status, or `code` for `Status` | the `status` member; 0 when missing, not whole or outside 100–599 |
| `detail string` | the explanation of this occurrence | `WithDetail` | the `detail` member; empty when missing or not a string |
| `instance string` | the URI reference of this occurrence | `WithInstance` | the `instance` member, resolved; empty when missing or invalid |
| `ext map[string]json.RawMessage` | the extension members, as compact JSON | `WithExtension`, marshaled when built | every other member, compacted |
| `cause error` | the internal cause, never sent | `WithCause` | always nil |
| `invalid error` | the first failure recorded while building; it wraps `ErrInvalid` | see Validation | always nil |

### Accessors and error behavior

| Method | Returns |
|--------|---------|
| `Type()` | `typ` |
| `Title()`, `Detail()`, `Instance()` | the field, `""` when absent |
| `Status()` | `status`, 0 when absent |
| `Extensions()` | a new map with copies of every value; empty when there are none |
| `Error()` | the title and the detail that are not empty, joined by `": "`, or `typ` when both are empty; then `": "` and the cause's message when there is a cause |
| `Unwrap()` | `cause` |

### Construction (produced occurrences)

```text
Type.New(opts...) ─┐
                   ├─► apply options in order ─► *Error (immutable)
Status(code, opts...) ┘      │
                             └─ first failure kept in `invalid`
```

- **Options**: each option sets one member, and later options win. A nil option is ignored.
- **Empty arguments**:
  - `WithTitle("")` keeps the current title;
  - `WithDetail("")` and `WithInstance("")` clear their member;
  - `WithCause(nil)` sets no cause.
- **Extensions**: `WithExtension(k, v)` marshals `v` immediately, and a second call with the
  same key replaces the value.

### Validation

The validation rule of [research R-03](research.md#r-03-declaring-problem-types-and-extension-names-fr-006-fr-008)
applies: static values are checked when they are declared, and per-request values when the
occurrence is built or written.

| Condition | Checked when | Effect |
|-----------|--------------|--------|
| invalid type URI, empty title, status outside 400–599, `about:blank` | `NewType` | error wrapping `ErrInvalid`; `Must` panics with it |
| invalid extension name | `NewKey` | error wrapping `ErrInvalid`; `Must` panics with it |
| `New` on an undeclared `Type` | building | recorded in `invalid` |
| `WithInstance` with a string that is not a valid URI reference | building | recorded in `invalid` |
| `WithExtension` with an undeclared `Key`, or a value `encoding/json` cannot marshal | building | recorded in `invalid` |
| `status` outside 400–599 (for example `Status(200)`, or a read occurrence with status 0) | `Write` | generic server error sent; error wrapping `ErrInvalid` returned |
| `invalid` is set | `Write` | generic server error sent; `invalid` returned |

An occurrence with a recorded failure is still a valid `error`. Its message and its cause are
unaffected, so it can be logged and inspected normally.

## Generic server error

This is the occurrence `Write` sends for any error that is not a problem, for no error, and in
place of a problem that cannot be written. It is built fresh for each use, and is never
exported:

| Member | Value |
|--------|-------|
| `type` | `about:blank` |
| `title` | `Internal Server Error` |
| `status` | 500 |
| any other | absent |

## Selection: which problem a response carries

```text
Write(w, r, err)
  err == nil                      → generic server error, report ErrInvalid
  errors.As(err, &e) finds e      → e (nil *Error → generic server error, report ErrInvalid)
  otherwise                       → generic server error, no report
  e cannot be written             → generic server error, report the reason
```

The selected occurrence is the only source of data for the response (FR-020). Wrapping levels,
causes and other branches of the tree are never read while encoding.

## Type tests

`Type.Match(err)` walks the tree in the order `errors.As` uses, following `Unwrap() error` and
`Unwrap() []error`. It returns the first `*Error` whose `typ` equals the type's `uri`. For read
occurrences, `typ` is already resolved (R-10). A type declared to match another API's problems
must therefore use that API's absolute URI.

## Sentinel errors

| Sentinel | Returned by | Meaning |
|----------|-------------|---------|
| `ErrInvalid` | `NewType`, `NewKey`, `Write` | a declaration or an occurrence breaks the rules above |
| `ErrNotProblem` | `Read` | the response is not a problem document (media type, or nil response); its body was not read |
| `ErrMalformed` | `Read`, `Parse` | the document is too large, is not valid JSON, or is not a JSON object |

Other failures (network errors while reading a body, write errors) are returned wrapped with
`%w` and the `problem:` prefix, so the underlying error stays inspectable.

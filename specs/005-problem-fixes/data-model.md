# Data Model: Problem Module Fixes and Improvements

**Feature**: [spec.md](spec.md) | **Research**: [research.md](research.md)

This feature adds no new state to the module. It adds one value type (`Item`), and new ways to
build and read the existing occurrence type (`*Error`). The model of spec 004
([004 data-model.md](../004-problem-details/data-model.md)) still applies. This document
describes only what changes.

## Occurrence: `Error` (unchanged fields)

| Field | Meaning | Copied by `With` as |
|-------|---------|---------------------|
| `typ` | type URI (resolved, for a problem read with `Read`) | value |
| `title` | title | value |
| `status` | status code | value |
| `detail` | detail | value |
| `instance` | instance | value |
| `ext` | extension members: name → compact JSON value | **new map, and a new copy of every value** |
| `cause` | internal cause, never written | value (the same error) |
| `invalid` | first failure recorded while building | value (the failure carries over) |

### Derivation: `With` (FR-005 to FR-010)

```text
e.With(opts...):
  e == nil            → nil
  otherwise           → c := copy of e (table above); apply opts to c in order; return c
```

- The original is only read, so concurrent derivations from one `*Error` are safe.
- Options follow the rules of spec 004:
  - for the same member, the last option wins;
  - nil options are ignored;
  - `WithTitle("")` keeps the title;
  - `WithDetail("")` and `WithInstance("")` clear the member.
- The **failure state** has two values, valid and invalid with a reason, and changes only on
  the problem being built:

  | Original | Options of the derivation | Derived problem | Original afterwards |
  |----------|---------------------------|-----------------|---------------------|
  | valid | all valid | valid | valid |
  | valid | one invalid (reason R) | invalid, R | valid |
  | invalid (reason O) | all valid | invalid, O | invalid, O |
  | invalid (reason O) | one invalid (reason R) | invalid, O (the first is kept) | invalid, O |

- **Identity**: the derived problem is a new `*Error`. `errors.Is(derived, original)` is false.
  `errors.Is` and `errors.As` against the cause, and `Type.Match`, give the same results as on
  the original, unless an option replaced the cause.

## Validation item: `Item` (FR-011, FR-016)

| Field | JSON member | Written | Meaning |
|-------|-------------|---------|---------|
| `Detail` | `detail` | always, even when `""` | explanation of this error, for the client |
| `Pointer` | `pointer` | only when not `""` | JSON Pointer, in URI fragment form, to the offending member |

- `Item` is a plain value with exported fields and no methods. It has no invariant: the library
  writes the pointer as given (FR-015).
- **Stored as** the extension member `errors`: a JSON array of item objects, in the given order
  ([contracts/document.md](contracts/document.md#the-errors-member)).
- **Owner of `errors`**: `WithErrors` and a developer key named `errors` write the same
  extension slot, so the last option applied wins (FR-017).

### Reading the items: `Errors` (FR-016)

```text
e.Errors():
  e == nil                                      → nil, false
  no "errors" member                            → nil, false
  member is not a JSON array, or is null        → nil, false
  some element is not a JSON object (or null)   → nil, false
  some element's "detail" or "pointer" is present but not a JSON string
                                                → nil, false
  otherwise → one Item per element, in order, and true
              ("detail"/"pointer" matched by exact name, "" when absent;
               other members are ignored, and stay in Extensions())
```

An empty array gives an empty, non-nil list and true.

## JSON Pointer (FR-012 to FR-014)

A JSON Pointer is a string built from reference tokens by `Pointer`. It holds no state.

```text
pointer   = "#" *( "/" token-enc )              ; "#" alone for no tokens
token-enc = *( unescaped / "~0" / "~1" / pct-encoded )
unescaped = ALPHA / DIGIT / "-" / "." / "_"
          / "!" / "$" / "&" / "'" / "(" / ")" / "*" / "+" / "," / ";" / "="
          / ":" / "@" / "?"
pct-encoded = "%" HEXDIG-UPPER HEXDIG-UPPER     ; every other byte of the token
```

- `~0` stands for `~` in the token, and `~1` for `/` (RFC 6901 §3). Escaping happens before
  percent-encoding, so `%` in a token always becomes `%25`.
- **Invariants**, checked by fuzzing:
  - the pointer is ASCII only;
  - it is a valid URI reference;
  - decoding it with RFC 6901 §6 gives back the tokens byte for byte.
- **Tokens**: they are opaque text. Array indexes are passed as decimal text by the caller.

## Representation headers (FR-001, FR-002)

| Header | Why it is removed |
|--------|-------------------|
| `Content-Length` | it gives the length of the body the handler meant to send |
| `Content-Encoding` | it describes how that body was encoded (for example gzip) |
| `Content-Range` | it locates that body inside a larger representation |
| `ETag` | it is a validator of that representation |
| `Last-Modified` | it is a validator of that representation |
| `Content-Disposition` | it presents that body (for example as a download) |
| `Trailer` | it announces trailer fields of that body |

- `Write` deletes these headers, with all their values, before setting `Content-Type` and the
  status. This happens for every response it sends: a problem, the generic server error, and
  HEAD responses.
- Every other header is kept. `Content-Type` is always set to `application/problem+json`.
- The list matches `representationHeaders` in `middleware/recovery/recovery.go`. Recovery's list
  also includes `Content-Type`, which `Write` overwrites instead.

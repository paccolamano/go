# Research: RFC 9457 Problem Details Module

**Feature**: [spec.md](spec.md) | **Plan**: [plan.md](plan.md) | **Date**: 2026-10-18

Each section records a decision, its rationale and the alternatives rejected. RFC quotations come
from the RFC Editor's text of RFC 9457. Standard library behavior was checked against the local
Go 1.27.0 toolchain, and every API used exists in Go 1.26, the module's minimum version.

The risky decisions were validated by a throwaway spike, kept outside this repository. It is a
minimal module with this module's path that implements the core of the design. It was linted
with the repository's golangci-lint configuration (v2.14.0), and measured on Go 1.27.0,
linux/amd64 and an Intel i7-11800H, with 3 runs per benchmark.

## R-01 Module layout, versioning and Go version

- **Decision**:
  - One module, `github.com/paccolamano/go/problem`, in the top-level directory `problem/`, with
    one package named `problem`. There are no subpackages and no `internal/` tree.
  - The `go` directive is `go 1.26.0`. CI tests on 1.26 and 1.27.
  - Releases start at `problem/v0.1.0` and stay at 0.x until the API is declared stable.
  - The `require` list is empty (FR-001). `go.work` is regenerated with `make work`.
- **Rationale**: the module has one purpose and about 600 lines of library code. Producing and
  reading share one representation of a problem (R-06, R-09), so a single import serves both
  sides of an API.
- **Alternatives rejected**:
  - A `problem/client` subpackage for reading: it needs the same `Error` type and its unexported
    fields, so it would force either exported internals or a duplicate type.
  - A dependency on the `errors` or `middleware` modules: neither is needed (spec, Assumptions),
    and FR-001 forbids it.

## R-02 Names of the exported types

- **Decision**:

  | Concept (spec) | Go name | Notes |
  |----------------|---------|-------|
  | Problem type | `Type` | declared once, immutable |
  | Problem (occurrence) | `Error` | implements `error`; `(*Error).Type()` returns the type URI |
  | Extension name | `Key[T]` | the name of an extension member and the Go type of its value |
  | Construction option | `Option` | `With*` functions, as in the `middleware` module |

- **Rationale**:
  - The `errname` linter requires error types to end with `Error`. The spike confirmed that
    `problem.Error` passes it.
  - `problem.Problem` would fail `errname`, and revive's `exported` rule reports it as a stutter.
  - `problem.Error` follows the standard library (`url.Error`, `exec.Error`) and reads as "a
    problem, as an error": `var pe *problem.Error; errors.As(err, &pe)`.
- **Alternatives rejected**:
  - `Details`, the RFC's own term: it needs a `//nolint:errname` only to choose a name.
  - `ProblemError`: it stutters.

## R-03 Declaring problem types and extension names (FR-006, FR-008)

- **Decision**:
  - `NewType(uri, title string, status int) (Type, error)` and
    `NewKey[T any](name string) (Key[T], error)` validate their arguments and return an error
    that wraps `ErrInvalid` and names the invalid element.
  - A generic `Must[T any](v T, err error) T` returns `v`, or panics with `err`. It exists for
    package-level declarations:

    ```go
    var OutOfCredit = problem.Must(problem.NewType("https://example.com/probs/out-of-credit",
        "You do not have enough credit.", http.StatusForbidden))
    var Balance = problem.Must(problem.NewKey[int]("balance"))
    ```

  - **Validation rule**: values fixed in the source code are checked when they are declared:
    - the type URI, the title and the status of a problem type;
    - extension names.

    Values computed for each request are checked when the occurrence is built:
    - the instance;
    - extension values;
    - the status of a status-only problem.

    A failure in a per-request value is recorded in the occurrence. `Write` then sends the
    generic server error and returns the reason (FR-016, R-07).
  - **Type rules**:
    - the type URI is a valid URI reference (R-04);
    - it is absolute or begins with `/`, and is not `about:blank`;
    - the title is not empty;
    - the status is between 400 and 599.
- **Rationale**:
  - A package-level declaration through `Must` fails when the program starts, so any test of the
    declaring package catches the mistake. It is never discovered by a client (User Story 3).
  - `Must` is the standard idiom of `regexp.MustCompile` and `template.Must`. One generic `Must`
    covers both kinds of declaration, so the library has one panic site.
  - An invalid declaration written in the source code is a programming error, not a recoverable
    condition (Principle IV forbids panics only for the latter). The panic carries
    `//nolint:forbidigo` with its reason, and the spike showed that `nolintlint` accepts it.
  - `about:blank` is refused, because its title depends on the status (RFC 9457 §4.2.1). A
    declared `about:blank` type would fix one title for every status, and its type tests (R-08)
    would match every `about:blank` problem. `Status` covers `about:blank` (R-06).
  - Network-path references (`//host/path`) begin with `/` and resolve to absolute URIs, so they
    are accepted along with absolute paths.
- **Alternatives rejected**:
  - Only `NewType` and `NewKey`: every consumer would write their own `must` helper for
    package-level declarations.
  - Only `Must*` variants: types built at run time, for example from configuration, would have
    no path that does not panic.
  - Struct literals (`problem.Type{URI: …}`) checked when written: a mistake would surface only
    when that error path runs, which is often in production.
  - Extension names given as strings when an extension is added, and checked then: the same
    late discovery, on error paths that tests rarely cover. The spec's FR-006 is refined
    accordingly (R-15).

## R-04 URI reference validation

- **Decision**: a string is a valid URI reference when all of the following hold:
  1. it is not empty;
  2. every byte is an RFC 3986 unreserved or reserved character (`A–Z a–z 0–9 - . _ ~`, and
     `: / ? # [ ] @ ! $ & ' ( ) * + , ; =`), or `%` followed by two hexadecimal digits;
  3. `url.Parse` accepts it.

  The check applies to type URIs (when declared), instances (when an occurrence is built) and
  the `type` and `instance` members read from a document (FR-023).
- **Rationale**:
  - `url.Parse` alone is too lenient. The spike showed that it accepts `a b` and `/é`.
  - It does reject `%zz`, `http://[::1`, `http://h:port` and control characters.
  - Step 2 closes the gap, and `url.Parse` checks the structure: hosts, ports and escapes.
  - Non-ASCII characters must be percent-encoded, as RFC 3986 requires: an IRI is not a URI.
- **Alternatives rejected**:
  - A full RFC 3986 grammar: more code, with no observable gain over steps 2 and 3.
  - `url.Parse` alone: it accepts strings that are not URIs.

## R-05 Extension members (FR-004, FR-006, FR-024)

- **Decision**:
  - `Key[T]` carries the member name and the Go type of its value.
  - `WithExtension[T any](k Key[T], v T) Option` marshals `v` with `encoding/json` when the
    occurrence is built, and stores the compact bytes as a `json.RawMessage`.
  - `(Key[T]).Lookup(e *Error) (T, bool)` decodes the stored value. It returns the zero value
    and false when the member is absent or does not decode into `T`.
  - `(*Error).Extensions() map[string]json.RawMessage` returns a deep copy of every extension
    member, including those the developer never declared (FR-024).
  - **Name rule**: the name matches `^[A-Za-z][A-Za-z0-9_]{2,}$`, and is not a standard member
    name, ignoring case.
- **Rationale**:
  - Marshaling at construction keeps the occurrence immutable even if the developer later
    changes a slice or map they passed (FR-002). It also makes writing cheap and deterministic.
  - Marshaling failures (`+Inf`, channels, functions) are recorded and reported by `Write`. In the
    spike, `json: unsupported value: +Inf` produced the generic server error and a returned
    error.
  - Typed keys give consumers typed reading (User Story 4, scenario 1: the balance as a number,
    the accounts as a list of strings) without a reflection-based API.
  - Standard names are reserved regardless of case, because `encoding/json` matches struct fields
    without regard to case. The spike showed that `{"TYPE":"x"}` fills a field tagged
    `json:"type"`. An extension named `Status` would therefore overwrite the status for every Go
    client that decodes problems into a struct.
- **Alternatives rejected**:
  - `WithExtension(name string, v any)`: the name is checked only when an occurrence is built,
    and reading stays untyped.
  - Developer structs that embed the problem and use JSON tags: `encoding/json` promotes the
    embedded type's `MarshalJSON` and hides the outer fields, and names could only be checked by
    reflection at run time.
  - Storing values and marshaling them at write time: this breaks immutability.

## R-06 Building occurrences (FR-005, FR-009, FR-010, FR-018)

- **Decision**:
  - `(Type).New(opts ...Option) *Error` and `Status(code int, opts ...Option) *Error`.
  - `type Option func(*Error)`, with the `With` prefix used by the `middleware` module
    (`ratelimit.Option`): `WithDetail`, `WithTitle`, `WithInstance`, `WithCause` and
    `WithExtension`.
  - **Order**: options apply in order, and a later option wins for the same member. Nil options
    are ignored.
  - **Empty values**: `WithTitle("")` keeps the current title, and `WithInstance("")` clears the
    instance.
  - **Failures**: the first failure is recorded and reported by `Write` (R-07).
  - **Status**: `Status(code)` builds a problem with type `about:blank`, title
    `http.StatusText(code)` and status `code`.
    - The title is empty for codes without a phrase: the spike showed `""` for 499 and 599.
    - The range check (400–599) happens in `Write`, because the code is a per-request value
      (R-03).
  - **Zero values**: `New` on a zero `Type` records the failure "undeclared type".
  - Constructors return `*Error` (Principle V: return concrete types). Fields are unexported and
    read through accessors.
- **Rationale**:
  - Options follow the repository's convention, and later releases can add new ones without
    breaking callers (Principle II).
  - The concrete return type lets callers use the accessors without a type assertion.
- **Alternatives rejected**:
  - Builder methods that return copies (`e.WithDetail(…)`): they allocate at each step, and they
    look like mutation.
  - Exported struct fields: they break immutability (FR-002).

## R-07 Writing a problem (FR-011 to FR-016, FR-019, FR-020)

- **Decision**: `Write(w http.ResponseWriter, r *http.Request, err error) error` is the single
  entry point, for problems and for any other error.
  1. **Select**: `errors.As(err, &e)` finds the first `*Error`, in the order of the standard
     inspection (FR-019). When none is found, the generic server error is used. When `err` is
     nil, or the problem found is a nil `*Error`, the generic server error is used and an error
     wrapping `ErrInvalid` is returned.
  2. **Encode**: the whole document is encoded into a buffer before `w` is touched. A recorded
     failure, or a status outside 400–599, switches to the generic server error, and the reason
     is returned wrapped in `ErrInvalid`.
  3. **Headers**: `Content-Type` is set to `application/problem+json`, replacing any previous
     value. Every other header is left untouched. `Content-Length` is not set: `net/http`
     computes it for small bodies, and a compression middleware upstream may change the length.
  4. **Status**: `WriteHeader(status)`. For a HEAD request, `Write` stops there. A nil `r` is
     treated as a GET.
  5. **Body**: the body is written. A write error is wrapped (`problem: write response: %w`)
     and joined with any reason from steps 1 and 2.

  **Document layout**:
  - The members appear in this order, with no whitespace:
    `{"type":…,"title":…,"status":…,"detail":…,"instance":…,<extensions>}`.
  - The title is omitted when empty, and so are the detail and the instance.
  - Extensions are sorted by the byte order of their names.
  - Strings are encoded by `encoding/json`: `<`, `>` and `&` become `<`, `>` and
    `&`, and non-ASCII text stays UTF-8.

  The spike's output for User Story 1, scenario 1 matched the spec byte for byte.
- **Rationale**:
  - Because `*Error` is an `error`, one function serves both cases, and handlers need one call
    (SC-002).
  - Nothing can leak: the only data that reaches `w` is the selected problem's own members
    (FR-020). Wrapping context and causes live elsewhere in the tree, and the document never
    reads them.
  - Encoding first guarantees that no partial document is sent (FR-016).
  - Sorting the extensions makes the output byte-identical (FR-013, SC-006).
  - HTML escaping prevents a content-sniffing browser from running markup found in a document.
- **Alternatives rejected**:
  - A separate `WriteError`: it is redundant, because `*Error` is an `error`.
  - Encoding a map of all members: Go sorts map keys, so `detail` would come before `type`. A
    fixed order of standard members is easier for people to read.
  - `Encoder.SetEscapeHTML(false)`: rejected because of content sniffing.
  - Setting `X-Content-Type-Options`: that is the `secureheaders` middleware's job.
- **Response already started**: `net/http` ignores a second `WriteHeader` and logs it to
  `Server.ErrorLog`. The package documentation states this limit (spec, Edge Cases).

## R-08 Problems as errors (FR-017, FR-018, FR-020, FR-021)

- **Decision**:
  - **Message**: `Error()` joins the title and the detail that are not empty with `": "`, or
    uses the type URI when both are empty. When a cause is set, it then adds `": "` and the
    cause's message. Example:
    `You do not have enough credit.: Your current balance is 30, but that costs 50.: unexpected EOF`.
  - **Cause**: `Unwrap()` returns the cause, so `errors.Is` and `errors.As` find it (FR-018).
    The cause never enters the document (R-07).
  - **No other interfaces**: there is no `json.Marshaler`, no `slog.LogValuer` and no
    `fmt.Formatter`.
  - **Type test**: `(Type).Match(err error) (*Error, bool)` returns the first `*Error` in the tree
    whose type URI equals the type's. It walks `Unwrap() error` and `Unwrap() []error` in the
    order `errors.As` uses. The type switch of that walk carries the module's only
    `//nolint:errorlint`, with its reason.
- **Rationale**:
  - Adding the cause to the message mirrors `fmt.Errorf("…: %w")` and puts the internal cause in
    the logs, where it belongs. The message never reaches the client.
  - slog's JSON handler logs a `json.Marshaler` through `MarshalJSON` instead of `Error()`. A
    marshaler would make logs show the document without the cause, and a problem that cannot be
    represented would put a marshal error in the record. The spike showed that without it, the
    record carries the message.
  - `errors.As` stops at the first `*Error`, but `Match` must look past problems of other types,
    because FR-021 asks about any error in the tree. That requires a walk by hand.
    `errorlint` reports type assertions on errors, and the walk is the one place that needs them.
- **Alternatives rejected**:
  - `(*Error).Is(target error)` comparing type URIs: `errors.Is(err, OutOfCredit.New())` needs a
    throwaway occurrence, and it cannot return the problem that matched.
  - Making `Type` an `error`: a `Type` could then be returned by mistake, and it would be written
    as the generic server error.

## R-09 Reading responses (FR-022 to FR-024, FR-026, FR-027)

- **Decision**: `Read(resp *http.Response) (*Error, error)` and `Parse(data []byte) (*Error, error)`.
  - **`Read`**:
    - **Media type**: it parses the response's `Content-Type` with `mime.ParseMediaType`, which
      lowercases the type and separates the parameters. Anything other than
      `application/problem+json`, including a nil response, returns `ErrNotProblem`, and the
      body is left untouched (spike: the HTML body stayed unread).
    - **Body**: it reads through `io.LimitReader(body, max+1)`. More than the maximum is
      `ErrMalformed`, and a nil body is `ErrMalformed` as well.
    - Read errors are wrapped, and `Read` does not close the body.
    - Finally, it calls `Parse` and resolves the URIs (R-10).
  - **`Parse`**:
    - **Input**: more than the maximum is `ErrMalformed`. The document is decoded with
      `json.Unmarshal` into a `map[string]json.RawMessage`. A decoding error, or JSON `null`
      (which decodes to a nil map without error), is `ErrMalformed`.
    - **Standard members**, matched by exact name:

      | Member | Kept when | Otherwise |
      |--------|-----------|-----------|
      | `type` | a string that is a valid URI reference (R-04) | `about:blank` |
      | `title`, `detail` | a string | absent |
      | `instance` | a string that is a valid URI reference | absent |
      | `status` | a number that is whole and between 100 and 599 | 0 |

    - **Extensions**: every other member is an extension, with its value compacted by
      `json.Compact`. A mistyped standard member is dropped, and does not become an extension.
    - **Duplicate names**: the last occurrence wins, which is how `encoding/json` fills maps
      (spike).
    - **Nesting**: `encoding/json` stops at a depth of 10,000 with an error, which becomes
      `ErrMalformed` (spike: `exceeded max depth`).
  - A problem that was read has no cause and no recorded failure. It can be written again when
    its status is between 400 and 599.
  - **Maximum size**: 1 MiB (1,048,576 bytes), an unexported constant documented in the package
    documentation.
- **Rationale**:
  - Decoding into a map keeps member names case-sensitive, unlike decoding into a struct (R-05).
  - `json.RawMessage` keeps extension numbers exact, with no `float64` rounding. Compacting
    removes whitespace differences, so round trips compare byte for byte (FR-028).
  - `status` is decoded as a `float64` and then checked, so that `403.0` and `4.03e2` count as
    whole numbers, as JSON's number model says.
  - The spike parsed a 1 MiB validation document (about 15,000 entries) in about 5 ms, with about
    2.4 MB allocated. Typical problem documents are under 1 KiB.
- **Alternatives rejected**:
  - `encoding/json/v2`, which matches case-sensitively and rejects duplicates: in Go 1.27 it is
    still built only with `GOEXPERIMENT=jsonv2` (its sources carry
    `//go:build goexperiment.jsonv2`), so it cannot be used at Go 1.26.
  - Decoding into a struct: it is case-insensitive.
  - Rejecting duplicate names: that would need a token-level scan, while RFC 8259 leaves their
    meaning to each implementation. Last-wins is what both `encoding/json` and JavaScript's
    `JSON.parse` do.
  - Exporting the maximum as a constant or an option: no consumer needs to change it, and
    exporting it would make its value part of the API (Principles II and V).

## R-10 Resolving relative URIs (FR-025)

- **Decision**:
  - The base is `resp.Request.URL`. `http.Client` sets `resp.Request` to the last request of a
    redirect chain, so the base is the final URL.
  - A `type` or `instance` that parses as a relative reference is resolved with
    `URL.ResolveReference`.
  - Without a request, both are kept as written, and `Parse` never resolves.
- **Rationale**:
  - RFC 9457 §3.1.1 and §3.1.5 resolve both members against the document's base URI. For an
    HTTP response, that is the URI the document was retrieved from (RFC 3986 §5.1.3).
  - `Content-Location` is not used: RFC 7231 (Appendix B) removed its effect on the base URI, and
    RFC 9110 kept that change.
  - In the spike, `/probs/out-of-credit` from `https://api.example.com/v1/messages` became
    `https://api.example.com/probs/out-of-credit`.

## R-11 Context parameters (Principle IV)

- **Decision**: `Write` and `Read` take no `context.Context`.
- **Rationale**: both act inside an HTTP exchange whose context already governs cancellation.
  - `Write` writes to the `ResponseWriter` of request `r`, whose context is `r.Context()`, as
    `http.Error` and `http.ServeContent` do.
  - `Read` reads the body of a client response, and `net/http` aborts a blocked body read when
    the context of `resp.Request` is canceled.

  A second context would duplicate these. Honoring it in `Read` would also mean closing a body
  that the caller owns, from a `context.AfterFunc` callback.
- This is recorded as a deviation in the plan's Complexity Tracking table.

## R-12 Limits and defaults

| Limit | Value | Applies to |
|-------|-------|------------|
| Maximum document size read | 1 MiB (1,048,576 bytes) | `Read`, `Parse` |
| JSON nesting depth | 10,000 (`encoding/json`'s limit) | `Parse` |
| Status codes produced | 400–599 | `NewType`, `Write` |
| Status codes read | whole numbers 100–599 | `Parse` |
| Extension names | 3 or more characters, a letter first, then letters, digits or `_`; not a standard name in any case | `NewKey` |
| Media type | `application/problem+json`, written exactly, read ignoring case and parameters | `Write`, `Read` |

## R-13 Performance

The module is not declared performance-sensitive: it runs on error paths. The spike measured:

- `Write` of the "out of credit" occurrence: 2.8 to 4.1 µs per operation, 2.7 KB and 38
  allocations, including the `httptest` recorder;
- `Parse` of a 1 MiB document: 4.7 to 5.1 ms.

`BenchmarkWrite` and `BenchmarkParse` back SC-005, whose 100 ms bound leaves a margin of about
20 times. Tests do not assert wall-clock time (Principle III).

## R-14 Lint compliance

The spike ran with the repository's configuration.

- **Library code** had one finding, `staticcheck` QF1001 (De Morgan's law), which a rewrite of
  the condition fixes.
- **Accepted directives**: the `nolintlint` check accepted the two directives, `forbidigo` on
  `Must` (R-03) and `errorlint` on the tree walk (R-08). The spike had two `errorlint`
  directives; the final design merges them into one type switch.
- **Passing linters**: `errname`, revive's `exported` rule, `gochecknoglobals` (the only
  package-level variables are the `Err*` sentinels) and `wrapcheck` raised nothing.
- **Tests**:
  - `noctx` requires `httptest.NewRequestWithContext(t.Context(), …)`.
  - `staticcheck` ST1013 requires the `http.Status*` constants in tests. Numeric literals are
    allowed only for codes without a constant, such as 499.

## R-15 Refinements to the spec

The design changes four points of the spec. The spec and its checklist were updated (checklist,
Iteration 3):

- **FR-006, User Story 3, scenario 3**:
  - Extension names are checked when they are declared, before any occurrence uses them, rather
    than when an extension is added (R-03).
  - Standard names are reserved regardless of case (R-05).
- **FR-007, FR-016, User Story 3, scenario 4**: an invalid instance is a per-request value. It is
  reported by `Write`, which sends the generic server error, rather than refused when set (R-03).
- **FR-008**: a declared type cannot be `about:blank`, which status-only problems use (R-03).
- **FR-023**: an `instance` that is not a valid URI reference is treated as absent when read
  (R-09).

The spec's edge cases also gain two clarifications:

- answering with no error is reported to the developer (R-07);
- differently capitalized standard names in a document read are extensions (R-09).

## R-16 Testing approach (Principle III)

- **Packages**: the external test package `problem_test`, plus `uri_internal_test.go` for the
  validation tables of R-04 and the name rule of R-05.
- **Style**: table-driven tests with named subtests. Test names follow `Test<Area>_<Scenario>`,
  as fixed in `tasks.md`.
- **Writing**: `httptest.NewRecorder`, with requests built by
  `httptest.NewRequestWithContext(t.Context(), …)`.
- **Reading**: responses are built in memory as `http.Response` values, with:
  - `io.NopCloser` bodies;
  - a counting reader that proves no more than the maximum plus one byte is read;
  - a failing reader;
  - a `Request` set for resolution.

  There is no network access.
- **Schema conformance (SC-001)**: a helper checks the rules of the JSON schema in RFC 9457
  Appendix A, without a JSON Schema library (Principle V). The rules are:
  - the document is an object;
  - `type` and `instance` are strings in URI-reference format;
  - `title` and `detail` are strings;
  - `status` is an integer from 100 to 599.

  The helper also checks the extension name rule, and that `status` equals the recorded code.
- **Leak corpus (SC-003)**: errors whose messages hold credentials, file paths, SQL text and
  stack-like text. Neither the messages nor their distinctive tokens may appear in the body or
  in any header value.
- **Concurrency**: one occurrence of one declared type, written from 64 goroutines, gives
  byte-equal bodies under `-race`.
- **`FuzzParse`**: `Parse` never panics. Every result whose status is between 400 and 599
  round-trips: `Parse` of the written document equals the result. The seed corpus holds the
  RFC's examples and malformed documents.
- **Examples**: runnable `Example` functions cover every exported capability (FR-003).
- **Middleware integration (SC-007)**: the module cannot import `middleware` (FR-001).
  - The README shows the two-line refusal function.
  - A test runs the snippet's body against a local stand-in for `middleware.Rejection` (a
    status code and an error).
  - The quickstart checks the README snippet against the real module.

## R-17 Dependencies

The standard library only: `bytes`, `encoding/json`, `errors`, `fmt`, `io`, `maps`, `math`,
`mime`, `net/http`, `net/url`, `slices`, `strconv` and `strings`. Tests add `net/http/httptest`,
`log/slog` and `testing`. There is no third-party module, test-only ones included (FR-001,
SC-008).

## R-18 Documentation (FR-003, Principle I)

- **Package documentation** (`doc.go`):
  - declaring types and extension names, and when to use `Must`;
  - what RFC 9457 expects of type URIs, including documentation at the URI;
  - answering with a status code, with an occurrence and with any error;
  - what never reaches the client: wrapping context, internal causes, other errors;
  - reading responses, the limits of R-12, and the fact that `Read` does not close the body;
  - the limit when a response has already started (R-07);
  - the two-line refusal function for the `middleware` module.
- **README.md**: purpose, `go get`, minimal producing and reading examples, minimum Go version
  1.26, and the `middleware` snippet.
- **CHANGELOG.md**: Keep a Changelog, with an `[Unreleased]` section that becomes `[0.1.0]` at
  release.

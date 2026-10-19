# Research: Problem Module Fixes and Improvements

**Feature**: [spec.md](spec.md) | **Plan**: [plan.md](plan.md) | **Date**: 2026-10-19

The Technical Context had no open questions: the language, toolchain, module and dependencies
are those of the existing `problem` module (spec 004). This research records the design
decisions for each item of the spec. A spike validated them in a scratch copy of the module:

- the two header failures were reproduced and then fixed;
- the new API was implemented;
- the suite passed under `-race` with 99.6% statement coverage;
- the pointer builder was fuzzed for 45 seconds (2.5 million executions, no failure);
- `golangci-lint` with the repository configuration reported 0 issues.

Section numbers refer to RFC 9457 unless another RFC is named.

## R-01 Release level and API compatibility

- **Decision**: release `problem/v0.2.0`.
  - Every API change is an addition: one type (`Item`), two functions (`WithErrors`, `Pointer`)
    and two methods on `*Error` (`With`, `Errors`). Adding methods to a struct type is
    compatible.
  - `Write` changes its documented behavior: it now removes the representation headers. At 0.x
    a MINOR release may change behavior. The change is a fix, so the changelog records it under
    "Fixed" and says that the behavior changed.
  - `gorelease` in the pull request workflow confirms the level against `problem/v0.1.0`.
- **Changelog draft** (`problem/CHANGELOG.md`, section `[Unreleased]`):

  ```markdown
  ### Added

  - `(*Error).With`, which derives a problem with more members, for example a request ID added
    when the problem is written. The original is not modified.
  - `Item`, `WithErrors` and `(*Error).Errors`, which write and read the validation errors of
    RFC 9457 §3 in the `errors` extension member.
  - `Pointer`, which builds a JSON Pointer (RFC 6901) in URI fragment form, escaping the
    reference tokens.

  ### Fixed

  - `Write` removes the headers that describe the body the handler meant to send:
    `Content-Length`, `Content-Encoding`, `Content-Range`, `ETag`, `Last-Modified`,
    `Content-Disposition` and `Trailer`. **Behavior change**: `Write` used to keep these
    headers, and they contradicted the problem document. For example, a leftover
    `Content-Length` truncated the response, and a leftover `Content-Encoding: gzip` made
    clients fail to decompress plain JSON. Every other header is still kept.
  - `Key.Lookup` returns the zero value and false for a nil `*Error`, instead of panicking.
  ```

- **Rationale**: Principle II. Every user-visible change goes into the changelog in the same
  change, and the version follows the `gorelease` report.
- **Alternatives rejected**: a PATCH release (0.1.1). The release adds API, so it is not a patch.

## R-02 Removing representation headers (B1: FR-001 to FR-003)

- **Decision**: in `Write`, after selecting and encoding the document, and right before
  `Content-Type` is set and `WriteHeader` is called, delete each representation header with
  `w.Header().Del`:

  ```go
  // representationHeaders describe the body that the handler meant to send. They
  // contradict the problem document, so Write removes them. The list is the one the
  // recovery middleware of github.com/paccolamano/go/middleware removes.
  func representationHeaders() []string {
      return []string{"Content-Length", "Content-Encoding", "Content-Range",
          "ETag", "Last-Modified", "Content-Disposition", "Trailer"}
  }
  ```

  - The removal runs on the single path that every response takes after selection: an
    occurrence, a status-only problem, the generic server error (whatever the reason) and HEAD.
    No branch can skip it.
  - `Header().Del` canonicalizes the name, so it removes values set with `Set` or `Add` in any
    capitalization, with all their values. A key inserted into the map directly, in
    non-canonical form, is not removed, and `net/http` would not recognize such a key either.
  - The list is an unexported function, not a package-level variable, because
    `gochecknoglobals` refuses global variables. This matches
    `middleware/recovery/recovery.go`. The benchmark is unchanged: `BenchmarkWrite` has
    38 allocations either way, and about 4.7–5.0 µs per operation.
  - `Content-Type` is not in the list, because `Write` overwrites it with `Set`.
- **Spike evidence**: through an `httptest.NewServer`, before the fix:
  - `Content-Length: 5` gave the client status 404, an empty body and `unexpected EOF`, and
    `Write` returned `problem: write response: http: wrote more than the declared Content-Length`;
  - `Content-Encoding: gzip` gave the client `gzip: invalid header`.

  After the fix, both responses carry the complete 55-byte document with a server-computed
  `Content-Length: 55`, and there is no read error and no write error.
- **Rationale**: the headers describe a body that is never sent. Everything else, such as
  `Retry-After` set by a rate limiter, `Content-Language`, `Cache-Control`, `Vary` and security
  headers, applies to whatever response is sent, and must survive (FR-002).
- **Alternatives rejected**:
  - **Keep the headers and document the pitfall**: the response stays corrupted, and the
    developer often cannot know which middleware set the header.
  - **An allow-list that keeps only known headers**: this would drop `Retry-After`, security
    headers and custom headers that middlewares set before calling a refusal function.
  - **Set `Content-Length` to the document length**: this fixes the truncation only. It does
    not fix `Content-Encoding`, `ETag` or the others, and it conflicts with compression
    middlewares upstream (004 R-07).
  - **Share the list with the `middleware` module**: that creates a dependency between modules
    for 7 strings (Principle I). The code comment names the origin of the list instead.
  - **A configurable list**: no current use needs one (Principle V).

## R-03 Reading an extension of a nil problem (B2: FR-004)

- **Decision**: `Key[T].Lookup` returns the zero value and false when `e == nil`, before it
  touches `e.ext`. `(*Error).Errors` does the same (R-06). The doc comment of `Lookup` names the
  nil case.
- **Rationale**: `Type.Match` returns `nil, false` when it finds nothing. Writing
  `p, _ := T.Match(err); v, ok := K.Lookup(p)` must not crash: the second `ok` already carries
  the answer.
- **Scope**: the other accessors of `*Error` (`Type`, `Extensions`, and so on) keep their
  current behavior on nil. The spec covers only extension reading, and those accessors follow
  the usual Go rule that a nil pointer receiver is a programming error.
- **Alternatives rejected**: documenting "never pass nil". The spec requires no crash.

## R-04 Deriving a problem: `(*Error).With` (I1: FR-005 to FR-010)

- **Decision**:

  ```go
  // With returns a copy of e with the options applied in order. e is not modified.
  func (e *Error) With(opts ...Option) *Error {
      if e == nil {
          return nil
      }
      c := *e                       // type, title, status, detail, instance, cause, invalid
      if e.ext != nil {
          c.ext = make(map[string]json.RawMessage, len(e.ext))
          for name, raw := range e.ext {
              c.ext[name] = slices.Clone(raw)  // the copy owns its values
          }
      }
      c.apply(opts)
      return &c
  }
  ```

  It goes in `problem.go`, next to `apply` and `fail`.
- **Semantics**, each covered by a test:
  - **Members**: the struct copy carries every scalar member. The extension map and every raw
    value are copied, so no later change can reach the other problem (FR-007).
  - **Cause**: the cause is copied. `errors.Is`, `errors.As` and `Type.Match` walk the same
    chain, and the copy has the same type URI (FR-006). A `WithCause` among the options replaces
    it, because the last option wins.
  - **Failures**: `invalid` is copied, so a failure recorded on `e` carries over. `fail` keeps
    the first failure, so when both `e` and the options fail, the reason reported is `e`'s.
    New failures are recorded on the copy only, because `e` is never written to (FR-008).
  - **Nil**: a nil receiver returns nil (FR-009). Nil options are skipped by `apply`, as when a
    problem is built.
  - **Identity**: the copy is a new pointer, so `errors.Is(copy, e)` is false. The spec
    documents this as an edge case.
  - **Concurrency**: `With` only reads `e`, and `*Error` is immutable, so concurrent calls on
    one `*Error` are race-free. The spike ran 100 concurrent derivations under `-race`.
  - **Problems read from other APIs**: `Read` and `Parse` produce an ordinary `*Error`, so the
    resolved type and the extensions carry over (FR-010).
- **Maintenance hazard**: a future field of reference type, such as a map or a slice, must be
  deep-copied too. A test derives with every option and checks that the original's document is
  byte-identical before and after. That test fails if a shared field is ever modified.
- **Alternatives rejected**:
  - **`Clone()` followed by mutation**: `*Error` has no setters, and adding them would break
    immutability.
  - **Rebuilding with `Type.New(...)`**: the HTTP layer does not know the members the service
    set, and status-only and read problems have no `Type`.
  - **A `WithProblem(*Error)` option to merge**: it is harder to explain, and has the same copy
    semantics.
  - **A shallow copy that shares the raw values**: it would be safe today, because raw values
    are never modified in place. The request asks for a deep copy, which keeps that true even
    if a later change modifies values in place.

## R-05 Writing validation items: `Item` and `WithErrors` (I2: FR-011, FR-015, FR-017)

- **Decision**:

  ```go
  // Item is one error of a validation problem.
  type Item struct {
      Detail  string `json:"detail"`
      Pointer string `json:"pointer,omitempty"`
  }

  // errorsMember is the extension member of validation items (RFC 9457 §3).
  const errorsMember = "errors"

  func WithErrors(items ...Item) Option {
      if items == nil {
          items = []Item{} // "errors":[] rather than null
      }
      return WithExtension(Key[[]Item]{name: errorsMember}, items)
  }
  ```

  - `WithErrors` reuses `WithExtension`, through an unexported `Key` named `errors`, so the
    items are stored in the same `ext["errors"]` slot that a developer key named `errors`
    uses. "Last option wins" then holds with no extra code (FR-017), and both orders are
    tested.
  - The values are marshaled by `encoding/json` when the option applies, like every extension:
    the order of the struct fields gives `detail` before `pointer`, and `<`, `>` and `&` are
    escaped as in the rest of the document. The spike's output matched
    [contracts/document.md](contracts/document.md#the-errors-member) byte for byte.
  - Pointers are written exactly as given (FR-015). The library does not validate them:
    `Pointer` is the recommended way to build them.
  - `NewKey("errors")` stays accepted (spec 004, US3-3). The doc comments of `NewKey` and
    `WithErrors` say that `WithErrors` owns the member.
- **Alternatives rejected**:
  - **Refusing `NewKey("errors")`**: this breaks callers of v0.1.0, and the request asks to
    keep accepting it.
  - **Omitting the member when there are no items**: an explicit `WithErrors()` asks for the
    member, and spec 004 writes empty extension values as they are.
  - **Validating the pointers**: a pointer is free text in RFC 9457, and checking it would turn
    a cosmetic mistake into a 500.

## R-06 Reading validation items: `(*Error).Errors` (I2: FR-016, FR-018)

- **Decision**: decode exactly, not through `Key.Lookup`.
  1. If `e` is nil, or has no `errors` member, return `nil, false`.
  2. Unmarshal the member into `[]map[string]json.RawMessage`. If that fails, or the member is
     JSON `null`, return `nil, false`.
  3. For each element:
     - it must be an object (a `null` element fails the whole member);
     - `detail` and `pointer` are matched by their exact names;
     - each one, when present, must be a JSON string (as `stringMember` in `read.go` reads it),
       and is `""` when absent.

     Other members are ignored.
  4. Return the items, in order, and true. `[]` gives an empty, non-nil list and true.
- **Spike evidence**: reusing `Key[[]Item].Lookup` is too lenient:
  - `{"errors":null}` gave `nil, true`;
  - `[null]` gave one empty item;
  - `{"Detail":"x","POINTER":"#/a"}` filled both fields, because struct decoding in
    `encoding/json` ignores case (004 R-05).

  With exact decoding, these cases give "not found", "not found", and an item with empty
  fields, as FR-016 requires.
- **Rationale**: spec 004 reads standard members by exact name, and treats a value of the wrong
  type as absent. Validation items are the one extension the library defines, so they follow the
  same rules. Requiring the whole member to decode matches the all-or-nothing behavior of
  `Lookup`. The other members of an item stay available through `Extensions()`.
- **Alternatives rejected**:
  - **`Key.Lookup`**: the leniency shown above.
  - **Skipping only the bad elements**: the indexes would no longer match the document, and a
    partial list is easy to mistake for the whole list.

## R-07 Building JSON Pointers: `Pointer` (I2: FR-012 to FR-014)

- **Decision**: a byte-wise encoder in a new file, `pointer.go`.

  ```text
  Pointer(t1, …, tn) = "#" + "/" + enc(t1) + … + "/" + enc(tn)          ("#" when n = 0)
  enc(t), for each byte c of t:
    '~'                         → "~0"     (RFC 6901 §3, escaped first)
    '/'                         → "~1"
    ALPHA / DIGIT / - . _       → c        (RFC 3986 unreserved; '~' is handled above)
    ! $ & ' ( ) * + , ; =       → c        (sub-delims)
    : @ ?                       → c        (also allowed in a fragment, RFC 3986 §3.5)
    any other byte              → "%" + two uppercase hex digits (RFC 3986 §2.1)
  ```

  - Working byte by byte encodes each byte of a non-ASCII character in UTF-8, and makes invalid
    UTF-8 round-trip exactly. The output is always ASCII.
  - The output is checked against the 12 pointers of RFC 6901 §6, plus `città` →
    `#/citt%C3%A0`, `items,0,qty` → `#/items/0/qty` and `~1` → `#/~01`. The spike matched all
    of them.
  - Tokens are opaque. An array index is passed as text (`strconv.Itoa(i)`), and `-` is not
    interpreted.
  - The library exposes no decoder, because no requirement needs one. The tests use their own
    independent decoder (R-08).
- **Alternatives rejected**:
  - **`url.PathEscape`, or `url.URL{Fragment: …}.String()`**: both round-trip, but they also
    encode characters that a fragment allows (the spike showed `a?b` → `a%3Fb` and
    `a'b` → `a%27b`). Their exact output is chosen by the standard library for other contexts.
    The output of `Pointer` is documented behavior, and is specified completely by RFC 6901 and
    RFC 3986.
  - **Taking `...any`, to accept integer indexes**: the request fixes `...string`, which keeps
    the function total and simple.

## R-08 Fuzzing (FR-013, FR-016, SC-005)

- **Decision**:
  - **`FuzzPointer(t, a, b, c string)`**: for each prefix of `[a, b, c]` (no tokens, one, two,
    three), it checks that:
    - the pointer is ASCII only;
    - it passes the test helper `isURIReference`, which does not use the library;
    - an independent RFC 6901 decoder gives back the tokens exactly. The decoder strips `#`,
      calls `url.PathUnescape`, splits on `/`, then replaces `~1` with `/` and `~0` with `~`.

    Three free strings cover every byte value in every position, with up to three tokens and
    empty tokens. The seeds are the tokens of the RFC 6901 §6 examples, `città`, `~1`, `\xff`
    and `#?`.
  - **`FuzzParse`**, which already exists, also calls `p.Errors()`, to check that decoding never
    panics on any document. When items are found, it rebuilds them with `WithErrors(items...)`
    and checks that `Errors()` returns the same items. It gets seeds with `errors` arrays,
    including mistyped and `null` elements.
- **Spike evidence**: 45 seconds, about 2.5 million executions, no failure. The quickstart runs
  each target for 10 minutes (SC-005).
- **Rationale**: Principle III requires fuzz tests for code that decodes external input. That
  covers `Errors()`, which reads documents from other APIs. The request also asks to fuzz
  `Pointer`.

## R-09 Regression tests through a real server (FR-027, SC-001)

- **Decision**:
  - **`TestWrite_RealServer`**, table-driven over `Content-Length: 5` and
    `Content-Encoding: gzip`:
    - it starts `httptest.NewServer`, with a handler that sets the header and calls `Write`, and
      sends `Write`'s result on a buffered channel;
    - the client uses `srv.Client()`, with a request built by
      `http.NewRequestWithContext(t.Context(), …)` (`noctx`), and closes the body with
      `defer func() { _ = resp.Body.Close() }()` (`errcheck`);
    - the test checks status 404, the exact document, no read error, and a nil error on the
      channel.

    The default transport of `srv.Client()` asks for gzip and decompresses on its own, which
    is the path that failed in the report.
  - **`TestWrite_RepresentationHeaders`** (with a recorder) has one subtest per header of FR-001.
    In each, the header is set, with two values for `Trailer`. There are also subtests for the
    generic server error, an invalid instance, a plain error, and HEAD.
  - **`TestWrite_HeadersKept`** is adapted: it also sets `Cache-Control`, `Vary` and
    `X-Content-Type-Options`, together with every representation header, and checks that the
    kept ones keep their values.
- **Hermeticity**: `httptest.NewServer` listens on the loopback interface, inside the test
  process. It reaches nothing outside the machine, needs no external resource, and is
  deterministic, so it meets Principle III's "no real network access" (spec, Assumptions).
  Passing the result through a channel orders the handler's write before the test reads it,
  as `-race` requires.
- **Fails without the fix**: in the spike, the same test against the unmodified `Write`
  failed with `unexpected EOF` and `gzip: invalid header` (R-02).

## R-10 Documentation and examples (I3, I4: FR-019 to FR-025)

- **Decision, README** (`problem/README.md`). The sections, in order:
  1. **Declaring problem types**: unchanged.
  2. **Answering requests**: the handler handles `Write`'s error. A subsection, "When `Write`
     returns an error", says what the error means:
     - `errors.Is(werr, problem.ErrInvalid)` means that the problem was built incorrectly
       (for example an instance containing a space) and was replaced by the generic 500. This
       is a bug to fix, so log it as an error.
     - Any other error usually means that the client went away while the response was sent.

     A short helper shows both levels, using a `logger` that the application injects:

     ```go
     func writeProblem(w http.ResponseWriter, r *http.Request, err error) {
         werr := problem.Write(w, r, err)
         switch {
         case errors.Is(werr, problem.ErrInvalid): // a bug: fix how the problem is built
             logger.ErrorContext(r.Context(), "problem replaced by a generic 500", "err", werr)
         case werr != nil: // usually the client went away
             logger.DebugContext(r.Context(), "write problem", "err", werr)
         }
     }
     ```

  3. **Adding members when writing** (new): `With`, using the `request_id` example of the
     request.
  4. **Validation errors** (new): `Item`, `WithErrors` and `Pointer`, with the RFC 9457 §3
     document, and a note that `WithErrors` owns the `errors` member.
  5. **Translating domain errors** (new, I4):
     - a `toProblem(err error) error` function that recognizes a sentinel with `errors.Is` and
       a typed error with `errors.AsType`;
     - for each, it builds `T.New(problem.WithDetail(...), problem.WithExtension(...), problem.WithCause(err))`
       from the error's fields;
     - otherwise it returns `err` unchanged, which `Write` answers with the generic 500;
     - the rule "never `WithDetail(err.Error())`", with a wrong-and-right pair: the wrong
       version leaks `pq: duplicate key value violates unique constraint "users_email_key"`,
       and the right one says "An account with this email address already exists." and keeps
       the error as the cause.
  6. **Reading problem details**: unchanged, except that it mentions `Errors()`.
  7. **Using it with the middleware module**: the refusal function passes the cause and handles
     the error, in 5 lines:

     ```go
     reject := func(w http.ResponseWriter, r *http.Request, rej middleware.Rejection) {
         if err := problem.Write(w, r, problem.Status(rej.Status, problem.WithCause(rej.Err))); err != nil {
             logger.ErrorContext(r.Context(), "write problem", "err", err)
         }
     }
     ```

     The text changes from "in two lines" to "in a few lines".
- **Decision, package documentation** (`problem/doc.go`): the same changes. Its two snippets stop
  discarding the error. "Headers set before Write, such as Retry-After, are kept" becomes the
  rule of FR-001 and FR-002. New paragraphs cover `With`, and validation items with `Pointer`.
  This extends FR-019 to the package documentation (R-12).
- **Decision, runnable examples** (`problem/example*_test.go`):
  - **Whole-file examples**: the three examples that need package-level declarations (a method,
    a helper, domain error types) each go in a file of their own, with exactly one `Example`.
    `go doc` and pkg.go.dev then show the whole file, declarations included:
    - `example_with_test.go`: `ExampleError_With`, with `RequestID`, `requestIDFrom` and the
      request's `writeProblem`;
    - `example_validation_test.go`: `ExampleWithErrors`, with a `validate` function that
      returns a 422 problem with two items;
    - `example_translate_test.go`: `Example_translateDomainErrors`, with `ErrNotFound`,
      `ErrEmailTaken`, `*InsufficientFundsError` and `toProblem`. Its output shows the
      documents for the 404, the 403 with its fields, the 409 and the generic 500. The test
      also checks that `users_email_key` and `pq:` appear in no response (SC-008).
  - **Small examples** in `example_test.go`: `ExamplePointer` (the RFC 6901 cases) and
    `ExampleError_Errors` (reading the items back after `Parse`).
  - **Existing examples**: they stop discarding `Write`'s error.
    - When the example is about the error, it prints it, as `ExampleWithInstance` already does.
    - Otherwise it logs it with `slog.ErrorContext(ctx, "write problem", "err", err)`, the form
      of the request's own example. The default `slog` logger writes to stderr, which
      `// Output:` does not compare, and the branch never runs in a passing example.
  - **Names**: identifiers in the whole-file examples are capitalized (`OutOfCredit`,
    `Balance`), so they do not clash with the lowercase test fixtures in `helpers_test.go`.
- **Rationale**: Principle IV forbids discarding errors silently, and examples are copied.
  Whole-file examples make the documented pattern compile as shown (FR-024).
- **Alternatives rejected**:
  - **A package-level `logger` shared by all examples**: in the rendered documentation, an
    example would then use a variable that the reader cannot see.
  - **Keeping the middleware snippet without a cause**: the request asks to keep the internal
    cause, for logs.

## R-11 Lint compliance (Principle IV)

- **Decision**: the spike passes `golangci-lint` with the repository configuration and no new
  `//nolint` directive. Two findings were fixed in the spike:
  - `gochecknoglobals` on a package-level header array: it became a function (R-02);
  - `errcheck` on `defer resp.Body.Close()` in a test: it became
    `defer func() { _ = resp.Body.Close() }()`.

  The two existing `//nolint` directives of 004 R-14 are unchanged.

## R-12 Refinements to the spec

The research found three places where the spec should be more precise. The spec has been
updated:

- **FR-019 and SC-007**: the package documentation (`doc.go`) also discards `Write`'s error, in
  two snippets. It is documentation that developers copy, like the README, so FR-019 and SC-007
  now cover it.
- **FR-003**: the package documentation states the header rule too. Today it says "Headers set
  before Write, such as Retry-After, are kept", which would become wrong.
- **Edge cases**: reading validation items matches `detail` and `pointer` by their exact names,
  and an `errors` member that is `null`, or that contains a `null` element, gives "not found"
  (R-06).

## R-13 Testing approach (Principle III)

- **Test first**: `tasks.md` will order every test task before the implementation it covers.
  The spike showed that each regression test fails without its fix:
  - `TestWrite_RealServer` fails with `unexpected EOF` and `gzip: invalid header`;
  - the `Lookup(nil)` test fails with a nil pointer dereference.
- **New and changed test files**:
  - `write_test.go`: the header tests of R-09.
  - `extension_test.go`: `TestLookup_NilProblem`.
  - `with_test.go` (new): `TestWith_*`, one test per semantic of R-04, including a
    100-goroutine `TestWith_Concurrent`.
  - `validation_test.go` (new): `TestWithErrors_*`, `TestErrors_*` and `TestPointer_*`.
    `TestPointer_RFC6901` is a table of the 12 RFC cases plus the extra cases of R-07.
  - `fuzz_test.go`: `FuzzPointer`, and the `Errors()` additions to `FuzzParse` (R-08).
  - `example*_test.go`: the examples of R-10.
- **Style**: table-driven tests with named subtests, the standard `testing` package, and the
  existing helpers (`write`, `wantResponse`, `checkDocument`, `sameProblem`, `isURIReference`).
- **Coverage**: the spike measured 99.6%, against a gate of 80%.

## R-14 Dependencies

- **Decision**: no new dependency. The new code uses `encoding/json`, `slices` and `strings`,
  which the module already imports. The tests add `net/url` (already used in `helpers_test.go`)
  and `log/slog` (examples only). `go.mod` keeps no `require`.

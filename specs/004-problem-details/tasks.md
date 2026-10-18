---

description: "Task list for the RFC 9457 problem details module"
---

# Tasks: RFC 9457 Problem Details Module

**Input**: Design documents from `specs/004-problem-details/`

**Prerequisites**: [plan.md](plan.md), [spec.md](spec.md), [research.md](research.md),
[data-model.md](data-model.md), [contracts/](contracts/), [quickstart.md](quickstart.md)

**Tests**: REQUIRED. Constitution Principle III is non-negotiable:

- Tests are written first and observed failing. A compilation failure counts as red.
- Coverage must be ≥ 80%, with `-race -shuffle=on`.
- Code that decodes external input (`Parse`, `Read`) needs a fuzz test.
- SC-005 is backed by a benchmark.
- Every exported capability needs an `Example` (FR-003).

Every test task below precedes the implementation it covers.

**Organization**: tasks are grouped by user story (spec.md US1–US4). Every path is relative to the
repository root. All Go code lives in the single module `problem/`, with one package `problem`
and no subfolders except `testdata/fuzz/`.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: can run in parallel with other [P] tasks of the same phase: different files, and no
  dependency on an incomplete task.
- **[Story]**: US1–US4 from spec.md. Setup, Foundational and Polish tasks have no story label.

## Conventions for every task

- **Packages**:
  - Behavior tests use the external package `problem_test` and import
    `github.com/paccolamano/go/problem`.
  - Tests of unexported helpers use `package problem`, in `problem/uri_internal_test.go`.
- **Test names**: `Test<Area>_<Scenario>`, chosen so that the `-run` patterns of
  [quickstart.md §3](quickstart.md) select them:

  | Story | Areas |
  |-------|-------|
  | US1 | `Status`, `Occurrence`, `Extension`, `Write` |
  | US2 | `Error`, `WriteError`, `Match` |
  | US3 | `NewType`, `NewKey`, `Must`, `Instance`, `Concurrent` |
  | US4 | `Read`, `Parse` |

  Edge cases contain `Edge` in the scenario part (`TestWrite_EdgeInvalidStatus`). The fuzz target
  is `FuzzParse`, and benchmarks use `b.Loop()`.
- **Test style**:
  - table-driven, with `t.Run`;
  - no wall-clock assertions and no `time.Sleep`;
  - goroutines are joined with `sync.WaitGroup`;
  - requests come from the helper `newRequest(tb, method, target)` (T006), never from
    `httptest.NewRequest` (`noctx`).
  - Use the `http.Status*` constants (`staticcheck` ST1013). Numeric literals are allowed only
    for codes without a constant: 0, 499, 599, 600.
- **Expected documents**: compared byte for byte with the strings in
  [contracts/document.md](contracts/document.md#examples), never re-marshaled.
- **Lint**: golangci-lint must report zero findings. Library code may carry exactly these two
  directives, worded as shown (research R-14):
  - in `Must`:
    `//nolint:forbidigo // Must is for static declarations; an invalid one is a programming error, as with regexp.MustCompile.`
  - on the type switch of the tree walk in `Match`:
    `//nolint:errorlint // walks the tree by hand, in errors.As order, to look past problems of other types.`

  Tests compare errors with `errors.Is` and never with `==`. Test helpers take `testing.TB` and
  call `tb.Helper()` first (`thelper`).
- **Documentation**: every exported identifier has a doc comment, which states its rule as in
  [contracts/public-api.md](contracts/public-api.md). The package doc in `doc.go` is expanded in
  T040.

---

## Phase 1: Setup (Shared Infrastructure)

**Purpose**: create the module and register it with the repository tooling.

- [X] T001 Create `problem/go.mod` containing exactly `module github.com/paccolamano/go/problem` and `go 1.26.0`, with no `require` and no `replace`.
- [X] T002 [P] Create `problem/CHANGELOG.md` in Keep a Changelog format with an empty `## [Unreleased]` section. The entries are added in T042.
- [X] T003 [P] Create `problem/README.md` with:
  - the title `# problem`;
  - one paragraph of purpose ("produces and reads RFC 9457 problem details (`application/problem+json`) for HTTP APIs, with no dependency outside the standard library");
  - `go get github.com/paccolamano/go/problem`;
  - "Minimum Go version: 1.26";
  - placeholder headings "Declaring problem types", "Answering requests", "Reading problem details" and "Using it with the middleware module", which are completed in T041.
- [X] T004 Run `make work` from the repository root so that `go.work` lists `./problem`. Then confirm that `scripts/structure-check.sh problem` reports PASS (depends on T001–T003).

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: the validation primitives and the test helpers that every story uses.

**⚠️ CRITICAL**: no user story work can begin until this phase is complete.

- [X] T005 Create `problem/doc.go` with a short package doc comment for `package problem` ("Package problem produces and reads RFC 9457 problem details for HTTP APIs."). It is expanded in T040.
- [X] T006 [P] Create `problem/helpers_test.go` (`package problem_test`) with these helpers:
  - `newRequest(tb testing.TB, method, target string) *http.Request`, which returns `httptest.NewRequestWithContext(tb.Context(), method, target, nil)`.
  - `checkDocument(tb testing.TB, body []byte, wantStatus int) map[string]json.RawMessage`, the RFC 9457 Appendix A conformance check (SC-001). It decodes `body` into a `map[string]json.RawMessage` and fails unless:
    - the body is a JSON object;
    - `type` is present and is a string in URI-reference format;
    - `title` and `detail`, when present, are strings;
    - `status` is present, is an integer from 100 to 599, and equals `wantStatus`;
    - `instance`, when present, is a string in URI-reference format;
    - every other member name matches `^[A-Za-z][A-Za-z0-9_]{2,}$`.

    "URI-reference format" is checked independently of the library: every byte is in `A–Z a–z 0–9 - . _ ~ : / ? # [ ] @ ! $ & ' ( ) * + , ; = %`, and `url.Parse` succeeds. The helper returns the decoded members.
  - `failingWriter`, an `http.ResponseWriter` whose `Header()` returns a real map, whose `WriteHeader` records the code, and whose `Write` returns the sentinel `errWrite`.
  - `countingReader`, which wraps an `io.Reader` and counts the bytes read.
  - `failingReader`, whose `Read` returns the sentinel `errRead`.
- [X] T007 [P] Write failing internal tests in `problem/uri_internal_test.go` (`package problem`):
  1. `TestURI_ValidReference` (research R-04). These are accepted: `https://example.com/probs/out-of-credit`, `/probs/out-of-credit`, `//example.com/p`, `tag:example.com,2026:out-of-credit`, `urn:example:out-of-credit`, `out-of-credit`, `/a%20b`, `#frag` and `about:blank`. These are refused: `""`, `/a b`, `/é`, `%zz`, `%2`, `http://[::1`, `http://h:port`, `"/a\x7f"` and `/a"b`.
  2. `TestURI_AbsoluteOrFullPath`: `isTypeURI` accepts only valid references that are absolute or begin with `/`. It refuses `out-of-credit`, `probs/x`, `#x` and `?x`.
  3. `TestName_Valid` (research R-05). These are accepted: `balance`, `retry_after`, `errors`, `abc` and `X12`. These are refused: `""`, `ab`, `id`, `user-id`, `2fa`, `_meta`, `bal ance`, `bälance`, `type`, `title`, `status`, `detail`, `instance`, `Status`, `TYPE` and `Detail`.
- [X] T008 Implement `problem/uri.go` until T007 passes:
  - `func validURIReference(s string) bool`: it is not empty; every byte is an RFC 3986 unreserved or reserved character, or `%` followed by two hexadecimal digits; and `url.Parse` accepts it.
  - `func isTypeURI(s string) bool`: `validURIReference(s)`, and either absolute (`url.Parse(s).IsAbs()`) or beginning with `/`.
  - `func validExtensionName(s string) bool`: "3 or more characters, a letter first, then letters, digits or `_`; not `type`, `title`, `status`, `detail` or `instance` in any capitalization" (data-model.md). Use no package-level `regexp` (`gochecknoglobals`).

**Checkpoint**: the foundation is ready, and `GOWORK=off go test -race ./...` passes in
`problem/`. User stories can now begin.

---

## Phase 3: User Story 1 - Answer a failed request with a standard problem document (Priority: P1) 🎯 MVP

**Goal**: declare a problem type and extension names (accepted values only), build occurrences,
and write them as RFC 9457 responses: status, media type, document, headers, HEAD, determinism
and the generic fallback (FR-004, FR-005, FR-009 to FR-016).

**Independent Test**: serve requests in memory through handlers that answer with `Status(404)`
and with the "out of credit" occurrence. Check status codes, headers and exact documents, and
check every document with `checkDocument`.

### Tests for User Story 1 ⚠️ (write first, see them fail)

- [X] T009 [US1] Add the shared declarations to `problem/helpers_test.go`, which do not compile until T014–T016 exist:
  - `outOfCredit = problem.Must(problem.NewType("https://example.com/probs/out-of-credit", "You do not have enough credit.", http.StatusForbidden))`;
  - `validationError = problem.Must(problem.NewType("https://example.net/validation-error", "Your request is not valid.", http.StatusUnprocessableEntity))`;
  - `serviceUnavailable = problem.Must(problem.NewType("https://example.com/probs/service-unavailable", "The service is unavailable.", http.StatusServiceUnavailable))`;
  - `balance = problem.Must(problem.NewKey[int]("balance"))`;
  - `accounts = problem.Must(problem.NewKey[[]string]("accounts"))`;
  - `ratio = problem.Must(problem.NewKey[float64]("ratio"))`;
  - ``type fieldError struct { Detail string `json:"detail"`; Pointer string `json:"pointer"` }`` and `fieldErrors = problem.Must(problem.NewKey[[]fieldError]("errors"))`;
  - `func creditOccurrence() *problem.Error`, which returns the occurrence of User Story 1, scenario 2: detail "Your current balance is 30, but that costs 50.", instance `/account/12345/msgs/abc`, `balance` 30, and `accounts` `["/account/12345", "/account/67890"]`;
  - `const creditDocument`, exactly the second example document of contracts/document.md.
- [X] T010 [P] [US1] Write failing tests in `problem/problem_test.go`:
  1. `TestStatus_Members` (FR-005, FR-010):
     - `Status(http.StatusNotFound)` has type `about:blank`, title "Not Found", status 404, empty detail and instance, and empty `Extensions()`;
     - `Status(499)` has an empty title;
     - `Status(http.StatusServiceUnavailable, problem.WithTitle("Servizio non disponibile"))` has that title.
  2. `TestOccurrence_Options` (FR-009, data-model "Construction"):
     - `creditOccurrence()` carries the type's URI, title and status, plus the detail and instance it set;
     - two `WithDetail` options: the last wins;
     - `WithTitle("")` keeps the type's title;
     - `WithDetail("")` after `WithDetail("x")` clears it;
     - `WithInstance("")` clears;
     - a nil `Option` is ignored.
  3. `TestOccurrence_Message` (FR-017, without a cause):
     - `outOfCredit.New(problem.WithDetail("d")).Error()` is `"You do not have enough credit.: d"`;
     - `Status(http.StatusNotFound).Error()` is `"Not Found"`;
     - `Status(499).Error()` is `"about:blank"`.
  4. `TestOccurrence_Immutable` (FR-002):
     - mutating the map returned by `Extensions()`, or a slice inside it, does not change a second call's result;
     - mutating the `[]string` passed to `WithExtension` after `New` returns does not change the occurrence.
- [X] T011 [P] [US1] Write failing tests in `problem/type_test.go`:
  - `TestMust_Basic`: `problem.Must(7, nil) == 7`. `problem.Must(0, errX)` panics, and the recovered value is an `error` for which `errors.Is(v, errX)` holds.
- [X] T012 [P] [US1] Write failing tests in `problem/extension_test.go`:
  1. `TestExtension_Values` (FR-004). After `WithExtension`, `Extensions()` holds exactly these compact JSON bytes:
     - `balance` → `30`;
     - `accounts` → `["/account/12345","/account/67890"]`;
     - `fieldErrors` with two entries → `[{"detail":"must be a positive integer","pointer":"#/age"},{"detail":"must be 'green', 'red' or 'blue'","pointer":"#/profile/color"}]`;
     - a second `WithExtension(balance, 40)` replaces the value.
  2. `TestExtension_Lookup`:
     - `balance.Lookup(creditOccurrence())` returns `(30, true)`, and `accounts.Lookup` returns the two accounts;
     - `ratio.Lookup` on an occurrence without it returns `(0, false)`;
     - a `Key[string]` named `balance` looks up the number and returns `("", false)`.
- [X] T013 [P] [US1] Write failing tests in `problem/write_test.go`. Each test checks its documents with `checkDocument`:
  1. `TestWrite_StatusOnly` (US1-1): `Write(rec, newRequest(t, GET, "/"), problem.Status(http.StatusNotFound))` returns nil. The code is 404, `Content-Type` is exactly `application/problem+json`, and the body is exactly `{"type":"about:blank","title":"Not Found","status":404}`.
  2. `TestWrite_Occurrence` (US1-2, FR-012): `creditOccurrence()` gives 403 and a body equal to `creditDocument`.
  3. `TestWrite_ReplacedTitle` (US1-3): `Status(404, WithTitle("Risorsa non trovata"))` gives `{"type":"about:blank","title":"Risorsa non trovata","status":404}`.
  4. `TestWrite_HeadersKept` (US1-4, FR-014): before `Write` with a 503 problem, the handler sets `Retry-After: 120`, `Content-Language: it` and `Content-Type: text/plain`. The response keeps the first two, and its `Content-Type` is `application/problem+json`. `Content-Length` is not set by `Write`.
  5. `TestWrite_NestedExtension` (US1-5): a `validationError` occurrence with the two `fieldErrors` gives `{"type":"https://example.net/validation-error","title":"Your request is not valid.","status":422,"errors":[…]}`, with the list exactly as in T012.
  6. `TestWrite_Head` (US1-6, FR-015): a HEAD request with `Status(http.StatusServiceUnavailable)` gives code 503, the problem media type and an empty body. `Write` returns nil.
  7. `TestWrite_Deterministic` (US1-7, FR-013, SC-006):
     - two writes of `creditOccurrence()` give equal bodies;
     - 64 goroutines writing the **same** occurrence to their own recorders all give `creditDocument`;
     - the test passes under `-race`.
  8. `TestWrite_Schema` (SC-001): for every status from 400 to 599 and for every occurrence used in this file, `checkDocument` passes with the recorded code.
  9. `TestWrite_EdgeInvalidStatus` (FR-016): `Status(http.StatusOK)`, `Status(0)` and `Status(600)` each give the generic server error. That is code 500 and exactly `{"type":"about:blank","title":"Internal Server Error","status":500}`. `Write` returns an error for which `errors.Is(err, problem.ErrInvalid)` holds and whose message contains the status.
  10. `TestWrite_EdgeNoPhrase`: `Status(499)` gives exactly `{"type":"about:blank","status":499}`.
  11. `TestWrite_EdgeUnrepresentable` (FR-016): an occurrence with `WithExtension(ratio, math.Inf(1))` gives the generic server error, and `Write` returns `ErrInvalid` with a message naming `ratio`.
  12. `TestWrite_EdgeWriteFailure`: with `failingWriter`, writing `creditOccurrence()` returns an error for which `errors.Is(err, errWrite)` holds and whose message starts with `problem: write response`. Writing `Status(http.StatusOK)` returns an error matching both `ErrInvalid` and `errWrite`.
  13. `TestWrite_EdgeEmptyValues`: extensions of type `string` (`""`), `int` (`0`), `bool` (`false`), `any` (`nil`), `[]int{}` and `map[string]int{}` are written as `""`, `0`, `false`, `null`, `[]` and `{}`, in name order.
  14. `TestWrite_EdgeSpecialCharacters`: ``Status(404, WithDetail(`<script>alert("é😀")</script>`))`` writes the detail as `"<script>alert(\"é😀\")</script>"`, with `é` and `😀` as raw UTF-8.

### Implementation for User Story 1

- [X] T014 [US1] Implement `problem/problem.go`:
  - `const MediaType = "application/problem+json"`, plus the unexported `blankType = "about:blank"`;
  - the sentinels `ErrInvalid` (`"problem: invalid"`), `ErrNotProblem` (`"problem: not a problem document"`) and `ErrMalformed` (`"problem: malformed problem document"`);
  - `type Error struct`, with the unexported fields `typ`, `title`, `detail`, `instance string`, `status int`, `ext map[string]json.RawMessage`, `cause error` and `invalid error`, as in data-model.md;
  - the accessors `Type`, `Title`, `Status`, `Detail`, `Instance` and `Extensions` (`Extensions` returns a fresh map with `slices.Clone` of every value, never nil);
  - `Error()`: "the title and the detail that are not empty, joined by `": "`, or `typ` when both are empty". The cause is added in T023;
  - the unexported `apply(opts []Option)`, which skips nil options, and `fail(err error)`, which keeps only the first failure;
  - `func Status(code int, opts ...Option) *Error`: type `about:blank`, title `http.StatusText(code)`, status `code`, then the options;
  - the unexported `serverError() *Error`: "`about:blank`, `Internal Server Error`, 500, any other member absent" (data-model "Generic server error").
- [X] T015 [P] [US1] Implement `problem/option.go`:
  - `type Option func(*Error)`;
  - `WithDetail`, where an empty string clears;
  - `WithTitle`, where an empty string keeps the current title, and whose doc says "localization only" (RFC 9457 §3.1.3);
  - `WithInstance`, which only sets the value at this stage (validation comes in T032), and where an empty string clears.
- [X] T016 [P] [US1] Implement `problem/type.go` and `problem/extension.go` for accepted values:
  - `type Type struct{ uri, title string; status int }`;
  - `func NewType(uri, title string, status int) (Type, error)`, which stores its arguments at this stage (the rules come in T030);
  - `func (t Type) New(opts ...Option) *Error`;
  - `func Must[T any](v T, err error) T`, with the `forbidigo` directive of Conventions;
  - `type Key[T any] struct{ name string }`;
  - `func NewKey[T any](name string) (Key[T], error)`, which stores the name at this stage (the rule comes in T031);
  - `func WithExtension[T any](k Key[T], v T) Option`. It marshals `v` with `json.Marshal` **when the option is applied**, stores the bytes in `ext`, and on error calls `fail(fmt.Errorf("%w: extension %q: %w", ErrInvalid, name, err))`;
  - `func (k Key[T]) Lookup(e *Error) (T, bool)`: `json.Unmarshal` of the stored value into a `T`. It returns the zero value and false when the member is absent or the value does not decode.
- [X] T017 [US1] Implement `problem/write.go` until T013 passes (depends on T014–T016). `func Write(w http.ResponseWriter, r *http.Request, err error) error` follows research R-07:
  1. Select with `errors.As(err, &e)`. When nothing is found, use `serverError()`. Nil reporting comes in T025.
  2. Encode the whole document into a `bytes.Buffer` with the unexported `(*Error).document() ([]byte, error)`:
     - it returns `invalid` when set;
     - it returns `fmt.Errorf("%w: status %d is outside 400-599", ErrInvalid, status)` when out of range;
     - otherwise it writes `{"type":…` then `title` (when not empty), `status`, `detail` and `instance` (when not empty), then the extensions sorted with `slices.Sorted(maps.Keys(ext))`;
     - string values and names are encoded with `json.Marshal`.

     On error, switch to `serverError()` and keep the error as the report.
  3. `w.Header().Set("Content-Type", MediaType)`. Do not set `Content-Length`.
  4. `w.WriteHeader(status)`. Return the report when `r != nil && r.Method == http.MethodHead`.
  5. `w.Write(body)`. Wrap a failure as `fmt.Errorf("problem: write response: %w", werr)`, and return `errors.Join(report, wrapped)`.
- [X] T018 [US1] Write `ExampleStatus`, `ExampleNewType`, `ExampleNewKey`, `ExampleMust`, `ExampleType_New`, `ExampleWithDetail`, `ExampleWithTitle`, `ExampleWithExtension`, `ExampleKey_Lookup` and `ExampleWrite` in `problem/example_test.go`. `ExampleWrite` uses `httptest.NewRecorder` and prints the code, the `Content-Type` and the body. Every `// Output:` comment is deterministic (depends on T014–T017).

**Checkpoint**: `GOWORK=off go test -race -run 'Status|Occurrence|Extension|Write|Must' .` passes in
`problem/`. The MVP is deliverable: handlers can answer with standard problem documents.

---

## Phase 4: User Story 2 - Return problems through error handling without leaking internals (Priority: P1)

**Goal**: problems travel as errors with internal causes, any error is answered safely, and type
tests find problems anywhere in a tree (FR-017 to FR-021, SC-003).

**Independent Test**: have a stub service return problems, wrapped problems, joined errors and
plain errors. Answer each with `Write`, then inspect the responses and the errors.

### Tests for User Story 2 ⚠️

- [X] T019 [P] [US2] Add failing tests to `problem/problem_test.go`:
  1. `TestError_Cause` (US2-3, FR-018):
     - `serviceUnavailable.New(problem.WithCause(dbErr))`, where `dbErr` wraps `io.ErrUnexpectedEOF` and a `*fs.PathError`: `errors.Is(p, io.ErrUnexpectedEOF)` holds, `errors.As` finds the `*fs.PathError`, and `errors.Unwrap(p)` matches `dbErr` (checked with `errors.Is`);
     - `WithCause(nil)` gives `Unwrap() == nil`.
  2. `TestError_MessageWithCause` (US2-6, FR-017): `outOfCredit.New(WithDetail("Your current balance is 30, but that costs 50."), WithCause(io.ErrUnexpectedEOF)).Error()` is exactly `"You do not have enough credit.: Your current balance is 30, but that costs 50.: unexpected EOF"`.
- [X] T020 [P] [US2] Add failing tests to `problem/type_test.go`:
  - `TestMatch_Tree` (US2-5, FR-021). `outOfCredit.Match(err)` returns the occurrence and true for:
    - the occurrence itself;
    - the occurrence wrapped twice with `fmt.Errorf("…: %w")`;
    - `errors.Join(plain, occurrence)`;
    - `errors.Join(serviceUnavailable.New(), occurrence)`, which looks past another type;
    - `serviceUnavailable.New(WithCause(occurrence))`.

    It returns `(nil, false)` for:
    - nil;
    - a plain error;
    - `serviceUnavailable.New()`;
    - `Status(http.StatusForbidden)`;
    - a typed nil `(*problem.Error)(nil)` wrapped in `fmt.Errorf`.

    `problem.Type{}.Match(occurrence)` returns false.
- [X] T021 [P] [US2] Add failing tests to `problem/write_test.go`:
  1. `TestWriteError_Wrapped` (US2-1, FR-019, FR-020): `fmt.Errorf("send message: %w", fmt.Errorf("charge message: %w", creditOccurrence()))` gives 403 and exactly `creditDocument`. Neither "charge message" nor "send message" appears in the body or in any header value.
  2. `TestWriteError_Leak` (US2-2, SC-003): the corpus holds plain errors with these messages:
     - `pq: password authentication failed for user "admin"`;
     - `open /etc/app/secrets.yaml: permission denied`;
     - `ERROR: relation "users" does not exist (SQLSTATE 42P01): SELECT id, password_hash FROM users WHERE email = $1`;
     - `dial tcp 10.0.0.12:5432: connect: connection refused`;
     - `goroutine 1 [running]:\nmain.main()\n\t/home/dev/app/main.go:42 +0x1d`;
     - `config: password=hunter2`.

     Each gives code 500 and exactly the generic server error document, and `Write` returns nil. None of the tokens `pq:`, `admin`, `secrets.yaml`, `/etc/app`, `SQLSTATE`, `password_hash`, `10.0.0.12`, `goroutine`, `main.go` or `hunter2` appears in the body or in any header value.
  3. `TestWriteError_CauseNotSent` (US2-3): `serviceUnavailable.New(WithCause(errors.New("pq: connection reset; password=hunter2")))` gives 503 and `{"type":"https://example.com/probs/service-unavailable","title":"The service is unavailable.","status":503}`, without `hunter2` anywhere in the response.
  4. `TestWriteError_Join` (US2-4):
     - `errors.Join(errors.New("plain"), creditOccurrence())` gives `creditDocument`;
     - `errors.Join(serviceUnavailable.New(), creditOccurrence())` gives the service-unavailable document, the first in `errors.As` order.
  5. `TestWriteError_EdgeNil` (edge case "No error", FR-019):
     - `Write(rec, req, nil)` gives the generic server error, and returns an error matching `ErrInvalid`;
     - `fmt.Errorf("x: %w", (*problem.Error)(nil))` gives the same result.

### Implementation for User Story 2

- [X] T022 [P] [US2] Add `WithCause(err error) Option` to `problem/option.go`. Its doc says that the cause is internal and never sent.
- [X] T023 [US2] In `problem/problem.go`:
  - add `func (e *Error) Unwrap() error`, which returns `cause`;
  - extend `Error()` to append `": " + cause.Error()` when a cause is set (data-model "Accessors and error behavior").

  T019 must pass (depends on T022).
- [X] T024 [US2] Implement `func (t Type) Match(err error) (*Error, bool)` in `problem/type.go` until T020 passes:
  - return false when `t.uri == ""` or `err == nil`;
  - otherwise walk the tree recursively, in `errors.As` order: check the node, then follow `Unwrap() error`, or each element of `Unwrap() []error`;
  - return the first non-nil `*Error` whose `typ == t.uri`;
  - the node check and the `Unwrap` dispatch live in **one** type switch, which carries the `errorlint` directive of Conventions.
- [X] T025 [US2] In `problem/write.go`, report nil until T021 passes. When `err == nil`, or `errors.As` finds a nil `*Error`, send `serverError()` and set the report to `fmt.Errorf("%w: no problem to write", ErrInvalid)`. A non-problem error stays silent: `Write` sends the generic server error and returns nil.
- [X] T026 [US2] Add `ExampleWithCause`, `ExampleType_Match` and `ExampleWrite_error` (a plain error answered with the generic server error) to `problem/example_test.go` (depends on T023–T025).

**Checkpoint**: `GOWORK=off go test -race -run 'Error|Leak|Cause|Match' .` passes, and US1 still
passes.

---

## Phase 5: User Story 3 - Declare a catalog of problem types and catch mistakes early (Priority: P2)

**Goal**: refuse invalid declarations when they are made, and report invalid per-request values
when the occurrence is written (FR-006 to FR-008, FR-016).

**Independent Test**: declare valid and invalid types and extension names, and write occurrences
with valid and invalid instances. Then check the errors, the panics of `Must`, the responses, and
the concurrent use of one declaration.

### Tests for User Story 3 ⚠️

- [X] T027 [P] [US3] Add failing tests to `problem/type_test.go`:
  1. `TestNewType_Rules` (US3-2, FR-008):
     - these type URIs are accepted with title "t" and status 403: `https://example.com/probs/out-of-credit`, `/probs/out-of-credit`, `//example.com/probs/x`, `tag:example.com,2026:out-of-credit` and `urn:example:out-of-credit`;
     - these are refused: `out-of-credit`, `probs/x`, `#x`, `?x`, `http://[::1`, `/a b`, `/é`, `%zz`, `about:blank` and `""`;
     - an empty title is refused;
     - statuses 0, 200, 302, 399 and 600 are refused, while 400 and 599 are accepted.

     Every refusal satisfies `errors.Is(err, problem.ErrInvalid)`, and its message names the element: "type URI", "title" or "status".
  2. `TestMust_Refused`: `problem.Must(problem.NewType("out-of-credit", "t", http.StatusForbidden))` panics with an error that matches `ErrInvalid`.
  3. `TestNewType_Independent` (US3-1): two occurrences of `outOfCredit`, with details "a" and "b" and balances 1 and 2, write their own documents. A third, `outOfCredit.New()`, still has the declared URI, title and status.
  4. `TestNewType_Undeclared` (data-model "Type"): `problem.Type{}.New()` written gives the generic server error, and `Write` returns `ErrInvalid` with "undeclared type" in the message.
- [X] T028 [P] [US3] Add failing tests to `problem/extension_test.go`:
  1. `TestNewKey_Rules` (US3-3, FR-006):
     - refused: `type`, `title`, `status`, `detail`, `instance`, `Status`, `TYPE`, `Detail`, `id`, `ab`, `""`, `user-id`, `2fa` and `_meta`, each with an error matching `ErrInvalid` whose message quotes the name;
     - accepted: `balance`, `retry_after`, `errors`, `abc` and `X12`.
  2. `TestNewKey_Undeclared`: `WithExtension(problem.Key[int]{}, 1)` makes `Write` send the generic server error and return `ErrInvalid`. `problem.Key[int]{}.Lookup(creditOccurrence())` returns `(0, false)`.
- [X] T029 [P] [US3] Add failing tests to `problem/write_test.go`:
  1. `TestInstance_Invalid` (US3-4, FR-007):
     - the instances `/a b`, `/é`, `%zz` and `http://[::1` each give the generic server error, and `Write` returns `ErrInvalid` with "instance" in the message;
     - `/account/12345/msgs/abc`, `urn:uuid:3f1c2a1e-9b7d-4f5e-8a0b-1c2d3e4f5a6b` and `https://example.com/x` are written as given.
  2. `TestConcurrent_Occurrences` (US3-5, FR-002):
     - 64 goroutines each build `outOfCredit.New(WithDetail(fmt.Sprint(i)), WithExtension(balance, i))` and write it;
     - each body equals its own expected document, built from the index;
     - the test passes under `-race`.

### Implementation for User Story 3

- [X] T030 [US3] Add the rules of data-model "Type" to `NewType` in `problem/type.go`. Each refusal returns `fmt.Errorf("%w: …", ErrInvalid)` naming the element:
  - the type URI is "a valid URI reference; absolute or beginning with `/`; not `about:blank`", checked with `isTypeURI` and an explicit `about:blank` check;
  - the title is "not empty";
  - the status is "between 400 and 599".

  Also make `Type.New` on a zero `Type` call `fail(fmt.Errorf("%w: undeclared type", ErrInvalid))`. T027 must pass.
- [X] T031 [US3] In `problem/extension.go`, make `NewKey` refuse names that fail `validExtensionName`, with `fmt.Errorf("%w: extension name %q …", ErrInvalid, name)`. Make `WithExtension` with a zero `Key` call `fail(fmt.Errorf("%w: undeclared extension key", ErrInvalid))`, and make `Lookup` with a zero `Key` return false. T028 must pass.
- [X] T032 [US3] In `problem/option.go`, make `WithInstance` call `fail(fmt.Errorf("%w: instance %q is not a URI reference", ErrInvalid, uri))` when a non-empty `uri` fails `validURIReference`, and leave the instance unchanged in that case. T029 must pass.
- [X] T033 [US3] Add `ExampleNewType_refused` (it prints the error of `NewType("out-of-credit", …)`) and `ExampleWithInstance` to `problem/example_test.go` (depends on T030–T032).

**Checkpoint**: `GOWORK=off go test -race -run 'NewType|NewKey|Must|Instance|Concurrent' .` passes,
and US1 and US2 still pass.

---

## Phase 6: User Story 4 - Read problem details returned by other APIs (Priority: P2)

**Goal**: read problem responses and documents with the RFC's consumer rules, bounded and
crash-free, and resolve relative URIs (FR-022 to FR-028, SC-004, SC-005).

**Independent Test**: feed recorded `http.Response` values and documents to `Read` and `Parse`,
with no network access, and check the results, the errors and the bytes read.

### Tests for User Story 4 ⚠️

- [X] T034 [P] [US4] Write failing tests in `problem/read_test.go`. Responses are built in memory with `io.NopCloser` bodies and `Request: newRequest(t, GET, "https://api.example.com/v1/messages")`.
  1. `TestRead_OutOfCredit` (US4-1): a 403 response with `creditDocument` gives:
     - the type, title, status 403, detail and instance of the occurrence;
     - `balance.Lookup` = `(30, true)` and `accounts.Lookup` = the two accounts;
     - `outOfCredit.Match(p)` true.
  2. `TestRead_MistypedMembers` (US4-2, FR-023): `{"type":"https://example.com/probs/out-of-credit","status":"403","title":42,"detail":"d"}` gives:
     - `Status() == 0`, `Title() == ""` and `Detail() == "d"`;
     - no error;
     - `Extensions()` without `status` or `title`.
  3. `TestRead_MissingType` (US4-3): `{"title":"x"}` gives type `about:blank`.
  4. `TestRead_Resolve` (US4-4, FR-025):
     - `/probs/out-of-credit` and `/account/12345/msgs/abc` become `https://api.example.com/probs/out-of-credit` and `https://api.example.com/account/12345/msgs/abc`;
     - `about:blank` and `https://example.com/x` are unchanged;
     - a response whose `Request` is nil keeps the relative values;
     - `Parse` of the same document keeps them as well.
  5. `TestRead_UnknownExtensions` (US4-5, FR-024): `{"type":"about:blank","foo":{"a":[1,2]},"bar":null}` gives `Extensions()` equal to `foo` → `{"a":[1,2]}` and `bar` → `null`.
  6. `TestRead_NotProblem` (US4-6, FR-022): `Content-Type` values of `text/html`, `application/json`, missing, and `/` (unparsable), and a nil response, each return an error matching `ErrNotProblem`. For the non-nil responses, `countingReader` shows 0 bytes read, and the full body is still readable afterwards.
  7. `TestRead_Limits` (US4-7, FR-027):
     - a body of 1 MiB + 1 bytes returns `ErrMalformed`, and `countingReader` shows at most 1,048,577 bytes read;
     - a valid document of exactly 1,048,576 bytes is accepted;
     - a nil `Body` returns `ErrMalformed`;
     - `failingReader` returns an error matching `errRead` and not `ErrMalformed`.
  8. `TestRead_StatusDisagreement` (US4-8, FR-026): a 503 response whose document says `"status":500` gives `Status() == 500`, and `resp.StatusCode` is untouched.
  9. `TestRead_RoundTrip` (US4-9, FR-028, SC-004): every occurrence written in `write_test.go` is `Write`n to a recorder and read back. The table holds `creditOccurrence()`, the validation occurrence, `Status(404)`, `Status(499)`, the special-characters problem of T013 and the empty-values problem. The read result has the same type, title, status, detail, instance and byte-identical `Extensions()`.
  10. `TestParse_Edge` (spec Edge Cases, FR-023, FR-027):
      - `{"TYPE":"x","Status":1}` gives extensions `TYPE` and `Status`, and type `about:blank`;
      - `{"status":400,"status":404}` gives status 404;
      - `403`, `403.0` and `4.03e2` give 403, while `403.5`, `99`, `600`, `1e400`, `"403"`, `true` and `null` give 0;
      - an `instance` of `/a b` is absent, and a `type` of `/a b` gives `about:blank`;
      - `{}` gives `about:blank` with every other member absent;
      - `[]`, `null`, `"x"`, `{`, an empty input and `{"a":` followed by 10,001 `[` and the matching `]` each return `ErrMalformed`;
      - 1 MiB + 1 bytes returns `ErrMalformed`.
  11. `TestRead_EdgeMediaTypeVariants`: `Application/Problem+JSON; charset=utf-8` and `application/problem+json;charset=UTF-8` are recognized.
- [X] T035 [P] [US4] Write `FuzzParse` in `problem/fuzz_test.go`:
  - **Seeds**: `creditDocument`, the RFC 9457 §3 validation example, `{}`, `[]`, `null`, `{"status":"403"}`, `{"a":` with no end, and a document with duplicate names.
  - **Property**: `Parse` never panics. When it succeeds and 400 ≤ `Status()` ≤ 599, `Write` of the result returns nil, and `Parse` of the body equals the result: same type, title, status, detail, instance, and byte-identical `Extensions()`.
- [X] T036 [P] [US4] Write `problem/bench_test.go`:
  - `BenchmarkWrite` writes `creditOccurrence()` to a recorder;
  - `BenchmarkParse` parses a validation document just under 1 MiB: an `errors` list of `{"detail":"must be a positive integer","pointer":"#/items/<i>/age"}` entries, built once outside the loop, with `b.SetBytes`.

### Implementation for User Story 4

- [X] T037 [US4] Add `func resolve(base *url.URL, ref string) string` to `problem/uri.go`. It returns `ref` unchanged when `ref` is empty, does not parse, or is absolute, and `base.ResolveReference(u).String()` otherwise (research R-10).
- [X] T038 [US4] Implement `problem/read.go` until T034 and T035 pass (depends on T037).
  - Add `const maxDocumentSize = 1 << 20`.
  - **`func Parse(data []byte) (*Error, error)`**:
    - more than `maxDocumentSize` bytes is `ErrMalformed`;
    - decode with `json.Unmarshal` into a `map[string]json.RawMessage`; an error or a nil map is `fmt.Errorf("%w: %w", ErrMalformed, err)` (or "not a JSON object");
    - apply the member rules quoted from data-model.md:

      | Member | Read occurrence |
      |--------|-----------------|
      | `type` | "the `type` member … `about:blank` when missing or invalid" |
      | `title` | "empty when missing or not a string" |
      | `status` | "0 when missing, not whole or outside 100–599", decoded as `float64` and checked with `math.Trunc` |
      | `detail` | "empty when missing or not a string" |
      | `instance` | "empty when missing or invalid" |

    - delete the five standard names from the map;
    - store every remaining member compacted with `json.Compact`.
  - **`func Read(resp *http.Response) (*Error, error)`**:
    - a nil `resp`, or a media type that is not `MediaType` according to `mime.ParseMediaType`, is `ErrNotProblem`, and the body is not touched;
    - a nil `Body` is `ErrMalformed`;
    - read with `io.ReadAll(io.LimitReader(resp.Body, maxDocumentSize+1))`, and wrap a read error as `fmt.Errorf("problem: read body: %w", err)`;
    - call `Parse`;
    - when `resp.Request != nil && resp.Request.URL != nil`, apply `resolve` to `typ` and `instance`;
    - never close the body.
- [X] T039 [US4] Add `ExampleRead` (an in-memory 403 response with `creditDocument`, printing the type, `Balance.Lookup` and `Match`) and `ExampleParse` to `problem/example_test.go` (depends on T038).

**Checkpoint**: `GOWORK=off go test -race -run 'Read|Parse' .` passes, then
`GOWORK=off go test -run '^$' -fuzz '^FuzzParse$' -fuzztime 30s .` finds nothing. Every story passes
together.

---

## Phase 7: Polish & Cross-Cutting Concerns

**Purpose**: documentation, the middleware snippet, release notes and the quickstart validation.

- [X] T040 Expand `problem/doc.go` into the full package documentation (FR-003, research R-18). It covers:
  - declaring types and extension names, and when to use `Must`;
  - what RFC 9457 expects of type URIs, including human-readable documentation at the URI;
  - answering requests with `Status`, with an occurrence and with any error through `Write`;
  - what never reaches the client: wrapping context, internal causes and other errors;
  - the generic server error;
  - the limit when a response has already started;
  - reading with `Read` and `Parse`, the limits of research R-12, and the fact that `Read` does not close the body;
  - the two-line refusal function for the `middleware` module.
- [X] T041 [P] Complete `problem/README.md` under the placeholder headings of T003, with:
  - the declarations and handler of [contracts/public-api.md](contracts/public-api.md);
  - the client example;
  - a sample response;
  - the middleware snippet:

    ```go
    reject := func(w http.ResponseWriter, r *http.Request, rej middleware.Rejection) {
        _ = problem.Write(w, r, problem.Status(rej.Status))
    }
    ```

    with a sentence noting that the two modules do not depend on each other.
- [X] T042 [P] Fill `problem/CHANGELOG.md` `## [Unreleased]` → `### Added` with entries for:
  - `NewType`/`NewKey`/`Must`;
  - occurrences with `Type.New`, `Status` and the `With*` options;
  - `Write`, with the generic server error;
  - `*Error` as an error with an internal cause, and `Type.Match`;
  - `Read`/`Parse`, with relative URI resolution and `Key.Lookup`.
- [X] T043 Add `TestWrite_RejectFuncSnippet` to `problem/write_test.go` (SC-007, research R-16). It defines a local `type rejection struct{ Status int; Err error }` and a function with exactly the body of the README snippet. For statuses 413, 415, 429 (with `Retry-After: 30` set beforehand), 500 and 503, the response has:
  - that status;
  - the problem media type;
  - the body `{"type":"about:blank","title":<http.StatusText>,"status":<code>}`;
  - `Retry-After` kept.
- [X] T044 Run `cd problem && GOWORK=off go mod tidy -diff`, which must print nothing and leave `problem/go.mod` without `require`, and the cross-build check of [quickstart.md §2](quickstart.md): `go vet ./...` for linux, darwin and windows × amd64 and arm64. There must be no failures.
- [X] T045 Run `FuzzParse` for 10 minutes ([quickstart.md §4](quickstart.md), SC-005). Fix any failure, then commit the failing input under `problem/testdata/fuzz/FuzzParse/` as a regression seed.
- [X] T046 Run the benchmarks of `problem/bench_test.go` ([quickstart.md §5](quickstart.md)) with `-count 5`, and record the medians in the PR description. `BenchmarkParse` must be < 100 ms/op (SC-005). Optimize the implementation if the target is missed. Do not change the target.
- [X] T047 Verify the exported surface against [contracts/public-api.md](contracts/public-api.md). Run `GOWORK=off go doc -all .` in `problem/` and confirm that it lists exactly the 31 identifiers in the contract, each with a doc comment. Then run `GOWORK=off go test -coverprofile=c.out . && go tool cover -func=c.out` and confirm that no exported function or method is at 0% (Principle III).
- [X] T048 Run the manual middleware check of [quickstart.md §6](quickstart.md) in a scratch directory outside the repository, with `ratelimit` set to 1 request per minute and the snippet from `problem/README.md`. The second response is 429 with `Retry-After` and `{"type":"about:blank","title":"Too Many Requests","status":429}`. Never commit the scratch module.
- [X] T049 Run the local gates on `problem/` with `make check MODULE=problem`. golangci-lint must report zero findings, with only the two `//nolint` directives of Conventions, and `go test -race -shuffle=on` must pass with coverage ≥ 80%. If Gitleaks or Semgrep later flag the leak corpus of T021, add a narrow ignore entry with a justification (constitution, "Finding suppression") rather than weakening the test.

---

## Dependencies & Execution Order

### Phase dependencies

- **Setup (Phase 1)**: no dependencies.
- **Foundational (Phase 2)**: depends on Setup and blocks every user story.
- **US1 (Phase 3)**: depends on Foundational. Every later story builds on its `Error` type,
  declarations and `Write` (T014–T017).
- **US2 (Phase 4)**: depends on US1.
- **US3 (Phase 5)**: depends on US1. Its tests do not need US2, but it shares
  `problem/type_test.go`, `problem/write_test.go` and `problem/example_test.go` with US2, so it
  runs after US2.
- **US4 (Phase 6)**: depends on US1. Its test `TestRead_OutOfCredit` and `ExampleRead` use
  `Type.Match` (US2, T024).
- **Polish (Phase 7)**: depends on every story.

The stories therefore run in priority order: US1 → US2 → US3 → US4. Each one adds a complete,
testable increment, and each leaves the earlier stories' tests green. US3 and US4 touch disjoint
library files (`type.go`, `extension.go` and `option.go` versus `uri.go` and `read.go`). With two
developers, they can proceed in parallel, provided the edits to `problem/example_test.go` (T033,
T039) are serialized.

### Within each story

- Test tasks are written first and observed failing. A compilation failure counts.
- Implementation tasks follow their tests: problem → options → declarations → write (US1),
  options → message → `Match` → write (US2), and `resolve` → read (US4).
- Example tasks depend on the implementation they show.
- **Shared files**: tasks on the same file never run in parallel. Several files are edited in
  more than one phase:

  | File | Edited by |
  |------|-----------|
  | `problem/helpers_test.go` | T006, T009 |
  | `problem/problem_test.go` | T010, T019 |
  | `problem/type_test.go` | T011, T020, T027 |
  | `problem/write_test.go` | T013, T021, T029, T043 |
  | `problem/example_test.go` | T018, T026, T033, T039 |
  | `problem/problem.go` | T014, T023 |
  | `problem/option.go` | T015, T022, T032 |
  | `problem/type.go` | T016, T024, T030 |
  | `problem/write.go` | T017, T025 |
  | `problem/uri.go` | T008, T037 |

### Parallel opportunities

- **Setup**: T002 and T003.
- **Foundational**: T006 and T007, then T008.
- **US1**:
  - T009 first: it creates the shared declarations.
  - Then the test tasks T010, T011, T012 and T013 together.
  - Then T014, followed by T015 and T016 in parallel (different files), then T017, then T018.
- **US2**: T019, T020 and T021 together, then T022, then T023 → T024 → T025, then T026.
- **US3**: T027, T028 and T029 together. Then T030, T031 and T032 can run in parallel, since they
  touch different files, then T033.
- **US4**: T034, T035 and T036 together, then T037 → T038 → T039.
- **Polish**: T041 and T042 together, then T040 and T043. T044–T049 come last.

---

## Parallel Example: User Story 1

```bash
# After T009 (shared declarations), write all failing tests for US1 together:
Task: "T010 occurrence tests in problem/problem_test.go"
Task: "T011 Must tests in problem/type_test.go"
Task: "T012 extension tests in problem/extension_test.go"
Task: "T013 write tests in problem/write_test.go"

# Then implement:
"T014 problem.go" → ("T015 option.go" ∥ "T016 type.go + extension.go") → "T017 write.go" → "T018 examples"
```

## Parallel Example: User Story 4

```bash
Task: "T034 read tests in problem/read_test.go"
Task: "T035 FuzzParse in problem/fuzz_test.go"
Task: "T036 benchmarks in problem/bench_test.go"
# then T037 (uri.go resolve) → T038 (read.go) → T039 (examples)
```

---

## Requirement Coverage

| Requirement | Tasks |
|-------------|-------|
| FR-001, SC-008 | T001, T044 |
| FR-002 | T010 (case 4), T013 (case 7), T029 (case 2) |
| FR-003 | T018, T026, T033, T039, T040, T041, T047 |
| FR-004 | T012, T013 (cases 2, 5, 13), T016 |
| FR-005, FR-010 | T010 (case 1), T013 (cases 1, 3, 10), T014 |
| FR-006 | T007, T008, T028, T031 |
| FR-007 | T007, T008, T029 (case 1), T032 |
| FR-008 | T007, T008, T027, T030 |
| FR-009 | T010 (case 2), T015, T016, T027 (case 3) |
| FR-011 to FR-015 | T013, T017 |
| FR-016 | T013 (cases 9, 11, 12), T017, T027 (case 4), T028 (case 2), T029 (case 1) |
| FR-017, FR-018 | T010 (case 3), T019, T022, T023 |
| FR-019, FR-020, SC-003 | T021, T017, T025 |
| FR-021 | T020, T024 |
| FR-022 to FR-027 | T034, T035, T037, T038 |
| FR-028, SC-004 | T034 (case 9), T035 |
| SC-001 | T006, T013 (case 8) |
| SC-002 | T013, T021, T018, T026 |
| SC-005 | T035, T036, T045, T046 |
| SC-006 | T013 (case 7) |
| SC-007 | T041, T043, T048 |
| SC-009 | T047, T049 |

---

## Implementation Strategy

### MVP first (User Story 1)

1. Phase 1: Setup (T001–T004).
2. Phase 2: Foundational (T005–T008). This is critical: it blocks everything else.
3. Phase 3: US1 (T009–T018).
4. **Stop and validate**: run `GOWORK=off go test -race ./...` in `problem/`, then
   `make check MODULE=problem`. At this point handlers can already answer with standard,
   deterministic problem documents, although declarations are not yet checked (US3).

### Incremental delivery

1. US2 (P1): safe answers for any error, internal causes and type tests. This is the story that
   makes the module safe to use in real handlers.
2. US3 (P2): checks on declarations and on instances.
3. US4 (P2): reading other APIs' problem details, with fuzzing.
4. Polish: docs, the middleware snippet, quickstart validation, and the release of
   `problem/v0.1.0` after the PR is squash-merged.

Each step leaves `make check MODULE=problem` green, so the work can pause after any story.

---

## Notes

- Every task keeps to the single module `problem/`. `testdata/fuzz/` holds only fuzz regression
  seeds.
- Never weaken a test to make it pass. Fix the implementation, or update the spec and contracts
  first if the behavior itself must change.
- Commit after each task or logical group. The PR title is
  `feat(problem): add RFC 9457 problem details module`.
- The working branch is suggested as `004-problem-details`. The design documents currently sit
  uncommitted on `errors-release-0.1.0`.

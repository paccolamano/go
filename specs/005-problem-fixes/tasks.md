---

description: "Task list for the problem module fixes and improvements"
---

# Tasks: Problem Module Fixes and Improvements

**Input**: Design documents from `specs/005-problem-fixes/`

**Prerequisites**: [plan.md](plan.md), [spec.md](spec.md), [research.md](research.md),
[data-model.md](data-model.md), [contracts/](contracts/), [quickstart.md](quickstart.md)

**Tests**: REQUIRED. Constitution Principle III is non-negotiable, and FR-027 asks for them:

- Tests are written first and observed failing. A compilation failure or a panic counts as red.
- Every fix has a regression test that fails without the fix.
- Coverage must be ≥ 80%, with `-race -shuffle=on`.
- `Pointer` and `Errors()` have fuzz tests (research R-08).
- Every new exported capability has an `Example`.

Every test task below precedes the implementation it covers.

**Organization**: tasks are grouped by user story (spec.md US1–US6). All paths are relative to the
repository root. All code lives in the existing module `problem/`, which has a single package
`problem`. Nothing outside `problem/` changes.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: can run in parallel with other [P] tasks of the same phase: different files, and no
  dependency on an incomplete task.
- **[Story]**: US1–US6 from spec.md. Setup, Foundational and Polish tasks have no story label.

## Conventions for every task

- **Packages**: tests use the external package `problem_test` and import
  `github.com/paccolamano/go/problem`. Reuse the existing helpers:
  - from `problem/helpers_test.go`: `newRequest`, `checkDocument`, `sameProblem`,
    `isURIReference`, `failingWriter`, `creditOccurrence`, `outOfCredit`, `validationError`,
    `balance`, `accounts`, `ratio`, `fieldErrors`, `creditDocument`, `serverDocument`,
    `twoFieldErrors`, `twoFieldErrorsJSON`;
  - from `problem/write_test.go`: `write`, `wantResponse`, `wantAbsent`, and the `rejection`
    type;
  - from `problem/read_test.go`: `response`.
- **Test names**: `Test<Area>_<Scenario>`, chosen so that the `-run` patterns of
  [quickstart.md §3](quickstart.md#3-scenario-checks-per-user-story) select them:

  | Story | Names |
  |-------|-------|
  | US1 | `TestWrite_RealServer`, `TestWrite_RepresentationHeaders`, `TestWrite_HeadersKept` |
  | US2 | `TestLookup_NilProblem` (and `TestErrors_NilProblem`, written in US4) |
  | US3 | `TestWith_*` |
  | US4 | `TestWithErrors_*`, `TestErrors_*`, `TestPointer_*`, `FuzzPointer` |
  | US5 | `TestWrite_RejectFuncSnippet`, `TestWrite_ErrorLevelsSnippet` |
  | US6 | `Example_translateDomainErrors` |

  Edge cases have `Edge` in the scenario part, for example `TestWith_EdgeIdentity`.
- **Test style**:
  - table-driven, with `t.Run`;
  - no `time.Sleep` and no wall-clock assertions;
  - goroutines are joined with `sync.WaitGroup` (`wg.Go`);
  - requests come from `newRequest` or `http.NewRequestWithContext(t.Context(), …)`, never from
    `httptest.NewRequest` or `http.Get` (`noctx`);
  - errors are compared with `errors.Is`;
  - helpers take `testing.TB` and call `tb.Helper()` first;
  - use the `http.Status*` constants.
- **Expected documents** are compared byte for byte with the strings in
  [contracts/document.md](contracts/document.md), never re-marshaled.
- **Lint**: golangci-lint must report zero findings, and the change adds **no** `//nolint`
  directive (research R-11):
  - no package-level variables in library code (`gochecknoglobals`);
  - in tests, close response bodies with `defer func() { _ = resp.Body.Close() }()` (`errcheck`).
- **Documentation**: every new exported identifier has a doc comment that states every rule of
  [contracts/public-api.md](contracts/public-api.md). When the contract gives the comment's text,
  use it.
- **Examples**:
  - In examples, `Write`'s error is never discarded. It is logged with
    `slog.ErrorContext(ctx, "write problem", "err", err)`, or printed when the example is about
    the error.
  - Whole-file examples (one `Example` per file, plus its package-level declarations) use
    capitalized names that appear in no other test file: `OutOfCredit`, `Balance`, `RequestID`,
    `ValidationError`, and so on.
- **Commits**: do not commit, push or open a pull request unless the maintainer asks for it.

---

## Phase 1: Setup (Shared Infrastructure)

**Purpose**: work on a feature branch from a green baseline.

- [X] T001 From the repository root, on a clean `main`, create and switch to the branch `005-problem-fixes` (`git switch -c 005-problem-fixes`). Do not commit anything yet.

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: confirm the baseline. The module, its test helpers and its tooling already exist, so
no code blocks the stories.

**⚠️ CRITICAL**: no user story work begins until this check passes.

- [X] T002 Run `make check MODULE=problem` on the unmodified module `problem/`. It must report `PASS` for both lint and test. Record the coverage percentage, to compare in T040.

**Checkpoint**: the baseline is green, and every user story can begin.

---

## Phase 3: User Story 1 - The client can read a problem whatever the handler set before failing (Priority: P1) 🎯 MVP

**Goal**: `Write` removes the 7 representation headers and keeps every other header (FR-001 to
FR-003).

**Independent Test**: a real `httptest.NewServer` client receives the complete document after a
handler set `Content-Length` or `Content-Encoding`. With a recorder, each of the 7 headers is
gone, and the other headers are kept.

### Tests for User Story 1 ⚠️ (write first, see them fail)

- [X] T003 [US1] Add `TestWrite_RealServer` to `problem/write_test.go` (research R-09; SC-001). It is table-driven, over `{"Content-Length", "5"}` and `{"Content-Encoding", "gzip"}`. Each subtest does the following:
  1. Create `errc := make(chan error, 1)`.
  2. Start `srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Header().Set(name, value); errc <- problem.Write(w, r, problem.Status(http.StatusNotFound)) }))` and `defer srv.Close()`.
  3. Build `req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, srv.URL, nil)` and send it with `srv.Client().Do(req)`. Do not set `Accept-Encoding`: the client's default transport asks for gzip and decompresses on its own, which is the failing path.
  4. `defer func() { _ = resp.Body.Close() }()`, then `body, rerr := io.ReadAll(resp.Body)`.

  Assert all of the following:
  - `resp.StatusCode == http.StatusNotFound`;
  - `rerr == nil`;
  - the body is exactly `{"type":"about:blank","title":"Not Found","status":404}`;
  - `resp.Header.Get("Content-Encoding") == ""`;
  - `resp.Header.Get("Content-Type") == problem.MediaType`;
  - `<-errc` is nil.

  Run it and observe it failing:
  - the `Content-Length` subtest fails with `unexpected EOF`, and `Write` returns `http: wrote more than the declared Content-Length`;
  - the `Content-Encoding` subtest fails with `gzip: invalid header`.
- [X] T004 [US1] Add `TestWrite_RepresentationHeaders` to `problem/write_test.go` (FR-001; US1-3, US1-5 and US1-6). Use a recorder, and the header list `Content-Length: 5`, `Content-Encoding: gzip`, `Content-Range: bytes 0-4/10`, `ETag: "v1"`, `Last-Modified: Mon, 19 Oct 2026 07:00:00 GMT`, `Content-Disposition: attachment; filename="a.bin"`, and `Trailer` added twice (`X-Checksum`, then `X-Count`).
  - **One subtest per header**: set only that header, write `problem.Status(http.StatusServiceUnavailable)`, assert that `rec.Header().Values(name)` is empty, and call ``wantResponse(t, rec, http.StatusServiceUnavailable, `{"type":"about:blank","title":"Service Unavailable","status":503}`)``.
  - **Four subtests with all 7 headers set**, each asserting that every header is absent afterwards:
    - `invalid instance`: `problem.Status(http.StatusConflict, problem.WithInstance("/a b"))` gives `serverDocument` with status 500;
    - `plain error`: `errors.New("boom")` gives `serverDocument`;
    - `nil error`: `nil` gives `serverDocument`;
    - `HEAD`: `problem.Status(http.StatusServiceUnavailable)` on `newRequest(t, http.MethodHead, "/")` gives status 503 and an empty body.
- [X] T005 [US1] Adapt `TestWrite_HeadersKept` in `problem/write_test.go` (FR-002; US1-4).
  - Before writing `problem.Status(http.StatusServiceUnavailable)`, set:
    - `Retry-After: 120`, `Content-Language: it`, `Cache-Control: no-store`, `Vary: Accept-Language`, `X-Content-Type-Options: nosniff` and `Content-Type: text/plain`;
    - every representation header of T004.
  - Assert that each of the first five headers keeps exactly its value, that `Content-Type` is `problem.MediaType`, and that the 7 representation headers are absent.
  - Remove the old `Content-Length` assertion, which T004 now covers.

### Implementation for User Story 1

- [X] T006 [US1] Implement the removal in `problem/write.go` (research R-02):
  - Add the unexported function `representationHeaders() []string`, returning `[]string{"Content-Length", "Content-Encoding", "Content-Range", "ETag", "Last-Modified", "Content-Disposition", "Trailer"}`. Its doc comment says that these headers describe the body the handler meant to send, that they contradict the problem document, and that the list is the one the recovery middleware of `github.com/paccolamano/go/middleware` removes.
  - In `Write`, right before `w.Header().Set("Content-Type", MediaType)`, take `h := w.Header()` and call `h.Del(name)` for each name. This must happen on the single path that every response takes, after the body is chosen.
  - Replace the header paragraph of the `Write` doc comment with the text in [contracts/public-api.md § Write](contracts/public-api.md#write-fr-001-to-fr-003).

  T003, T004 and T005 must pass, and so must every existing test.
- [X] T007 [P] [US1] In `problem/doc.go`, section "Answering requests", replace "Headers set before Write, such as Retry-After, are kept." with the rule of FR-001 and FR-002:
  - `Write` removes `Content-Length`, `Content-Encoding`, `Content-Range`, `ETag`, `Last-Modified`, `Content-Disposition` and `Trailer`, which describe the body the handler meant to send;
  - it keeps every other header, such as `Retry-After`, `Content-Language`, `Cache-Control` and `Vary`;
  - it sets the media type.

  This covers FR-003.
- [X] T008 [P] [US1] In `problem/CHANGELOG.md`, under `## [Unreleased]`, add `### Fixed` with the first bullet of the draft in [research R-01](research.md#r-01-release-level-and-api-compatibility): the 7 headers, the explicit **Behavior change** note, and the truncation and gzip symptoms.

**Checkpoint**: `GOWORK=off go test -race -run 'Write_RepresentationHeaders|Write_HeadersKept|Write_RealServer' -v .` passes in `problem/`. The bug of the request is fixed.

---

## Phase 4: User Story 2 - Reading an extension never crashes on an absent problem (Priority: P1)

**Goal**: `Key.Lookup(nil)` returns the zero value and false (FR-004).

**Independent Test**: read an extension of a nil problem, directly and through the ignored result
of a failed `Match`. There is no panic.

### Tests for User Story 2 ⚠️

- [X] T009 [US2] Add `TestLookup_NilProblem` to `problem/extension_test.go` (US2-1, US2-2). Its table-driven cases:
  - `balance.Lookup(nil)` gives `0, false`;
  - for `p, _ := outOfCredit.Match(errors.New("plain"))`, `balance.Lookup(p)` gives `0, false`;
  - `accounts.Lookup((*problem.Error)(nil))` gives `nil, false`;
  - `(problem.Key[int]{}).Lookup(nil)` gives `0, false`.

  Run it and observe the nil pointer dereference panic.

### Implementation for User Story 2

- [X] T010 [US2] In `problem/extension.go`, make `Key[T].Lookup` return the zero value and false when `e == nil`, before reading `e.ext`. Update its doc comment to the text in [contracts/public-api.md § Key.Lookup](contracts/public-api.md#keylookup-fr-004), which names the nil case. T009 must pass.
- [X] T011 [US2] In `problem/CHANGELOG.md`, under `### Fixed`, add the second bullet of the [research R-01](research.md#r-01-release-level-and-api-compatibility) draft: `Key.Lookup` returns the zero value and false for a nil `*Error`, instead of panicking.

**Checkpoint**: `GOWORK=off go test -race -run 'Lookup_NilProblem' -v .` passes. Both bug fixes are
done.

---

## Phase 5: User Story 3 - Add per-request members to a problem built elsewhere (Priority: P2)

**Goal**: `(*Error).With(opts...)` derives a copy with more members. The original is unchanged,
and the cause and recorded failure are kept (FR-005 to FR-010).

**Independent Test**: derive from occurrences, from status-only problems, from problems read
from responses and from invalid problems. Write both problems and compare the documents and the
error inspection, including from 100 goroutines under `-race`.

### Tests for User Story 3 ⚠️

- [X] T012 [US3] Create `problem/with_test.go` (`package problem_test`) with the declaration `var requestID = problem.Must(problem.NewKey[string]("request_id"))` and these tests. They do not compile until T013. That is the red step.
  1. **`TestWith_AddsMembers`** (US3-1, FR-005): `d := creditOccurrence().With(problem.WithExtension(requestID, "req-42"))`.
     - Writing `d` gives exactly `{"type":"https://example.com/probs/out-of-credit","title":"You do not have enough credit.","status":403,"detail":"Your current balance is 30, but that costs 50.","instance":"/account/12345/msgs/abc","accounts":["/account/12345","/account/67890"],"balance":30,"request_id":"req-42"}`.
     - Writing the original afterwards still gives `creditDocument`.
     - A subtest checks the option rules: `With(problem.WithDetail(""), problem.WithTitle(""))` clears the detail and keeps the title, while the original keeps its detail.
  2. **`TestWith_KeepsCause`** (US3-2, FR-006):
     - Take `cause := &strconv.NumError{Func: "Atoi", Num: "x", Err: strconv.ErrSyntax}` and `orig := outOfCredit.New(problem.WithCause(cause))`, and derive `d := orig.With(problem.WithExtension(requestID, "r"))`.
     - Assert that `errors.Is(d, strconv.ErrSyntax)` is true.
     - Assert that `errors.As(d, &ne)` finds the same pointer `cause`.
     - Assert that `outOfCredit.Match(d)` returns `d, true`, and that `validationError.Match(d)` returns `nil, false`, as for `orig`.
  3. **`TestWith_Concurrent`** (US3-3, FR-007, SC-004):
     - Share `orig := creditOccurrence()`.
     - Start 100 goroutines with `wg.Go`. Goroutine `i` derives `orig.With(problem.WithExtension(requestID, "req-"+strconv.Itoa(i)))`, writes it with a recorder and `httptest.NewRequestWithContext(t.Context(), …)`, and stores the body in `bodies[i]`.
     - After `wg.Wait()`, each `bodies[i]` must equal ``strings.TrimSuffix(creditDocument, "}") + `,"request_id":"req-` + strconv.Itoa(i) + `"}` ``, and writing `orig` must still give `creditDocument`.
     - Run the test with `-race`.
  4. **`TestWith_ReplacesMember`** (US3-4): `d := creditOccurrence().With(problem.WithExtension(balance, 10))`. Then `balance.Lookup(d)` gives `10, true`, and `balance.Lookup(orig)` gives `30, true`.
  5. **`TestWith_CarriesFailure`** (US3-5, FR-008):
     - Take `bad := problem.Status(http.StatusConflict, problem.WithInstance("/a b"))`.
     - Writing `bad.With(problem.WithDetail("ok"))` gives `serverDocument` with status 500, and an error that matches `errors.Is(err, problem.ErrInvalid)` and contains `"/a b"`.
     - Writing `bad.With(problem.WithInstance("/c d"))` reports `"/a b"`, not `"/c d"`, because the first failure is kept.
  6. **`TestWith_NewFailure`** (US3-6, FR-008):
     - Writing `orig.With(problem.WithInstance("/c d"))` gives `serverDocument` and `ErrInvalid`.
     - Writing `orig.With(problem.WithExtension(ratio, math.Inf(1)))` gives `serverDocument` and `ErrInvalid`.
     - Afterwards, writing `orig` still gives `creditDocument`, with a nil error.
  7. **`TestWith_Nil`** (US3-7, FR-009):
     - `(*problem.Error)(nil).With(problem.WithDetail("x"))` is nil.
     - `creditOccurrence().With(nil)` writes `creditDocument`: a nil option is ignored.
  8. **`TestWith_ReadProblem`** (US3-8, FR-010):
     - Read ``p, err := problem.Read(response(t, http.StatusForbidden, problem.MediaType, strings.NewReader(`{"type":"/probs/out-of-credit","title":"You do not have enough credit.","status":403,"balance":30,"code":"E1"}`)))``.
     - Derive `d := p.With(problem.WithExtension(requestID, "r"))`.
     - Assert that `d.Type()` is `https://api.example.com/probs/out-of-credit`.
     - Assert that `d.Extensions()` has `balance`, `code` and `request_id`, with `balance` = `30` and `code` = `"E1"`.
  9. **`TestWith_NoOptions`** (US3-9): `d := creditOccurrence().With()`. `sameProblem(t, d, orig)` holds, and both write byte-identical bodies.
  10. **`TestWith_EdgeIdentity`**: `errors.Is(orig.With(), orig)` is false. The derived problem is a distinct occurrence.
  11. **`TestWith_EdgeReplaceCause`**: derive `orig := outOfCredit.New(problem.WithCause(io.ErrUnexpectedEOF))` into `d := orig.With(problem.WithCause(io.ErrClosedPipe))`. Then:
      - `errors.Is(d, io.ErrClosedPipe)` is true;
      - `errors.Is(d, io.ErrUnexpectedEOF)` is false;
      - `errors.Is(orig, io.ErrUnexpectedEOF)` is still true.

### Implementation for User Story 3

- [X] T013 [US3] Add `(*Error).With` to `problem/problem.go`, next to `apply` and `fail` (research R-04, data-model § Derivation):
  - a nil receiver returns nil;
  - otherwise `c := *e`;
  - when `e.ext != nil`, `c.ext` becomes a new map of the same length, where every value is `slices.Clone(raw)`;
  - then `c.apply(opts)`, and return `&c`.

  Use the doc comment of [contracts/public-api.md § Deriving a problem](contracts/public-api.md#deriving-a-problem-fr-005-to-fr-010). T012 must pass under `-race`.
- [X] T014 [P] [US3] Create the whole-file example `problem/example_with_test.go` (`package problem_test`). It holds exactly one example, and no other `Example`, `Test` or `Benchmark` in the file:
  - `var RequestID = problem.Must(problem.NewKey[string]("request_id"))`;
  - `type requestIDKey struct{}`;
  - `func withRequestID(ctx context.Context, id string) context.Context`;
  - `func requestIDFrom(ctx context.Context) (string, bool)`;
  - `func writeProblem(w http.ResponseWriter, r *http.Request, err error)`, exactly as in [contracts/public-api.md](contracts/public-api.md#deriving-a-problem-fr-005-to-fr-010), using `errors.AsType[*problem.Error]` and `slog.ErrorContext`;
  - `func ExampleError_With()`. It builds a request with `httptest.NewRequestWithContext(withRequestID(context.Background(), "req-42"), http.MethodPost, "/msgs", nil)` and calls `writeProblem(rec, req, fmt.Errorf("send message: %w", problem.Status(http.StatusConflict, problem.WithDetail("The message was already sent."))))`. It prints `rec.Body`, with `// Output:` `{"type":"about:blank","title":"Conflict","status":409,"detail":"The message was already sent.","request_id":"req-42"}`.
- [X] T015 [US3] Document deriving in `problem/README.md` and `problem/doc.go` (FR-005; research R-10):
  - In `problem/README.md`, add the section `## Adding members when writing` after "Answering requests". It has one paragraph explaining why `With` exists: a service layer builds the problem, and the HTTP layer adds per-request members such as a request ID. It shows the `RequestID` and `writeProblem` code of T014, and it states that the original is not modified and that the copy keeps the cause.
  - In `problem/doc.go`, add a paragraph to "Answering requests" introducing `[Error.With]`, with the same message.
- [X] T016 [US3] In `problem/CHANGELOG.md`, under `## [Unreleased]`, add `### Added`, placed before `### Fixed`, with the `With` bullet of the [research R-01](research.md#r-01-release-level-and-api-compatibility) draft.

**Checkpoint**: `GOWORK=off go test -race -run 'With_|ExampleError_With' -v .` passes.

---

## Phase 6: User Story 4 - Report validation errors with correct JSON Pointers (Priority: P2)

**Goal**: `Item`, `WithErrors`, `(*Error).Errors` and `Pointer`, with exact reading rules and
fuzzed pointers (FR-011 to FR-018).

**Independent Test**: write the RFC 9457 §3 validation problem, read it back, check every row of
the pointer and reading tables of [contracts/document.md](contracts/document.md), and fuzz.

### Tests for User Story 4 ⚠️

- [X] T017 [P] [US4] Create `problem/validation_test.go` (`package problem_test`) with these tests. They do not compile until T019 and T020. That is the red step.
  1. **`TestWithErrors_RFCExample`** (US4-1, FR-011): `validationError.New(problem.WithErrors(problem.Item{Detail: "must be a positive integer", Pointer: problem.Pointer("age")}, problem.Item{Detail: "must be 'green', 'red' or 'blue'", Pointer: problem.Pointer("profile", "color")}))` is written with status 422 and exactly the document of [contracts/document.md § The errors member](contracts/document.md#the-errors-member). Check it with `wantResponse`.
  2. **`TestWithErrors_Items`** (US4-5, US4-10, FR-011, FR-015), table-driven on the `errors` member, read with `string(e.Extensions()["errors"])`:
     - `problem.Item{Detail: "the body is empty"}` gives `[{"detail":"the body is empty"}]`;
     - `problem.Item{Pointer: "#/a"}` gives `[{"detail":"","pointer":"#/a"}]`;
     - `WithErrors()` and `WithErrors([]problem.Item(nil)...)` give `[]`;
     - `problem.Item{Detail: "x", Pointer: "not a pointer <&>"}` gives `[{"detail":"x","pointer":"not a pointer \u003c\u0026\u003e"}]`. The pointer is written as given, and JSON-escaped as everywhere.
  3. **`TestWithErrors_LastWins`** (US4-9, FR-017):
     - `WithErrors(a)` then `WithExtension(fieldErrors, twoFieldErrors())` gives `twoFieldErrorsJSON`;
     - in the reverse order, the result is the JSON of `a`;
     - also assert that `problem.NewKey[int]("errors")` is still accepted.
  4. **`TestErrors_RoundTrip`** (US4-6, FR-018, SC-006):
     - Write the T017.1 occurrence with a recorder.
     - `problem.Parse(rec.Body.Bytes())` and `problem.Read(response(t, http.StatusUnprocessableEntity, problem.MediaType, rec.Body))` both give `Errors()` equal to the two original items (`slices.Equal`), in order, with `true`.
     - Calling `Errors()` directly on the built occurrence gives the same.
  5. **`TestErrors_NotFound`** (US4-7, FR-016), table-driven over documents given to `problem.Parse`, where each must give `Errors()` = `nil, false`:
     - `{}`
     - `{"errors":null}`
     - `{"errors":"x"}`
     - `{"errors":1}`
     - `{"errors":true}`
     - `{"errors":{}}`
     - `{"errors":[null]}`
     - `{"errors":["a","b"]}`
     - `{"errors":[1]}`
     - `{"errors":[{"detail":42}]}`
     - `{"errors":[{"detail":"x","pointer":false}]}`
     - `{"errors":[{"detail":"ok"},{"detail":[]}]}`

     Also, `problem.Status(http.StatusBadRequest).Errors()` gives `nil, false`.
  6. **`TestErrors_Found`** (US4-8, FR-016), table-driven over documents given to `problem.Parse`:
     - `{"errors":[]}` gives an empty, non-nil list and `true`.
     - `{"errors":[{"detail":"x","pointer":"#/a","code":"E1"}]}` gives `[{x #/a}]`, and `Extensions()["errors"]` still contains `"code":"E1"`.
     - `{"errors":[{"pointer":"#/a"}]}` gives `[{"" #/a}]`.
     - `{"errors":[{"Detail":"x","POINTER":"#/a"}]}` gives `[{"" ""}]`, because names are exact.
     - `{"errors":[{"detail":"a","detail":"b"}]}` gives `[{b ""}]`, because the last duplicate wins.
  7. **`TestErrors_NilProblem`** (US2-3, FR-004): `(*problem.Error)(nil).Errors()` gives `nil, false`, without a panic.
  8. **`TestPointer_RFC6901`** (US4-2, US4-3, US4-4, FR-012, FR-014): a table with exactly the 16 rows of [contracts/document.md § JSON Pointers](contracts/document.md#json-pointers-built-by-pointer), from `(none)` → `#` to `a?b:c@d` → `#/a?b:c@d`. "One empty token" is `Pointer("")`, and "a space" is `Pointer(" ")`.
  9. **`TestPointer_EdgeBytes`** (FR-013):
     - `Pointer("\x00\x7f\xff")` gives `#/%00%7F%FF`;
     - ``Pointer("#[]{}`")`` gives `#/%23%5B%5D%7B%7D%60`;
     - for each, `isURIReference` holds.
- [X] T018 [P] [US4] In `problem/fuzz_test.go` (research R-08):
  - **Add `decodePointer(p string) ([]string, error)`**, an independent RFC 6901 decoder: require the `#` prefix, then `url.PathUnescape` the rest. An empty result gives no tokens. Otherwise require a leading `/`, then `strings.Split` on `/`, and in each part replace `~1` with `/`, then `~0` with `~`.
  - **Add `FuzzPointer(f *testing.F)`**, with signature `func(t *testing.T, a, b, c string)`, and seeds `("foo", "0", "")`, `("a/b", "m~n", "c%d")`, `("città", "~1", " ")` and `("\xff", "#?", "e^f")`. For each prefix `[]`, `[a]`, `[a b]` and `[a b c]`, `p := problem.Pointer(tokens...)` must:
    - contain only bytes below `0x80`;
    - satisfy `isURIReference(p)`;
    - give back `tokens` through `decodePointer`, with the same length and each token equal byte for byte.
  - **Extend `FuzzParse`**:
    - add the seeds `{"errors":[{"detail":"a","pointer":"#/a"},{"detail":"b"}]}`, `{"errors":[null]}` and `{"errors":[{"detail":1}]}`;
    - right after a successful `Parse`, call `items, ok := p.Errors()`;
    - when `ok`, `problem.Status(http.StatusBadRequest, problem.WithErrors(items...)).Errors()` must return `slices.Equal` items and `true`.

### Implementation for User Story 4

- [X] T019 [P] [US4] Create `problem/pointer.go` with `func Pointer(tokens ...string) string` (research R-07; data-model § JSON Pointer):
  - start with `#`, then add `/` before each token;
  - for each byte of the token:
    - `~` gives `~0`, and `/` gives `~1`;
    - ASCII letters, digits, `-`, `.`, `_`, `!`, `$`, `&`, `'`, `(`, `)`, `*`, `+`, `,`, `;`, `=`, `:`, `@` and `?` are written as they are;
    - every other byte gives `%` followed by two **uppercase** hexadecimal digits.
  - Use a `strings.Builder`, the existing `isLetter` and `isDigit` of `problem/uri.go`, and a `const` string of the allowed punctuation. No package-level variables.
  - Use the doc comment of [contracts/public-api.md](contracts/public-api.md#validation-errors-fr-011-to-fr-018).

  T017.8, T017.9 and `FuzzPointer` (seed corpus) must pass.
- [X] T020 [US4] Create `problem/validation.go` (research R-05, R-06; data-model § Validation item):
  - ``type Item struct { Detail string `json:"detail"`; Pointer string `json:"pointer,omitempty"` }``, with the contract's doc comment.
  - `const errorsMember = "errors"`.
  - `func WithErrors(items ...Item) Option`: when `items == nil`, set `items = []Item{}`. Then return `WithExtension(Key[[]Item]{name: errorsMember}, items)`, so that it shares the extension slot with a developer key named `errors`.
  - `func (e *Error) Errors() ([]Item, bool)`, following the decision table of [data-model.md § Reading the items](data-model.md#reading-the-items-errors-fr-016):
    1. Return `nil, false` when `e == nil` or the member is absent.
    2. Unmarshal into `[]map[string]json.RawMessage`. Return `nil, false` on error, or when the result is nil (JSON `null`).
    3. Return `nil, false` for a nil element.
    4. Read `detail` and `pointer` with a helper `optionalString(obj, name)`, which gives `"", true` when the member is absent and otherwise delegates to `stringMember` of `problem/read.go`. Return `nil, false` when a present member is not a string.
    5. Otherwise return the items and `true`.

  T017 (all tests) and the `FuzzParse` seeds must pass.
- [X] T021 [US4] In `problem/extension.go`, add the paragraph of [contracts/public-api.md § NewKey](contracts/public-api.md#newkey-fr-017) to the `NewKey` doc comment: `errors` is accepted, `WithErrors` owns that member, and the option applied last wins.
- [X] T022 [US4] Add the examples in `problem/example_validation_test.go` and `problem/example_test.go`:
  - **Create the whole-file example `problem/example_validation_test.go`**, with exactly one example:
    - `var ValidationError = problem.Must(problem.NewType("https://example.net/validation-error", "Your request is not valid.", http.StatusUnprocessableEntity))`;
    - `type Profile struct{ Color string }` and `type CreateUser struct { Age int; Profile Profile }`;
    - `func validate(req CreateUser) error`, as in [contracts/public-api.md](contracts/public-api.md#validation-errors-fr-011-to-fr-018);
    - `func ExampleWithErrors()`. It calls `validate(CreateUser{Age: -1, Profile: Profile{Color: "pink"}})`, writes the error to a recorder (logging `Write`'s error with `slog.ErrorContext`), and prints `rec.Code` and `rec.Body`. The `// Output:` is `422` followed by the RFC 9457 §3 document of [contracts/document.md](contracts/document.md#the-errors-member).
  - **In `problem/example_test.go`, add `ExamplePointer`**. It prints `problem.Pointer("items", "0", "qty")`, `problem.Pointer()`, `problem.Pointer("a/b", "m~n")` and `problem.Pointer("città")`, with `// Output:` `#/items/0/qty`, `#`, `#/a~1b/m~0n` and `#/citt%C3%A0`.
  - **In `problem/example_test.go`, add `ExampleError_Errors`**. It parses the compact RFC 9457 §3 document, and prints one `pointer: detail` line per item, `#/age: must be a positive integer` and `#/profile/color: must be 'green', 'red' or 'blue'`.
- [X] T023 [US4] Document validation errors in `problem/README.md` and `problem/doc.go`:
  - In `problem/README.md`, add `## Validation errors` after "Adding members when writing". It contains:
    - the `ValidationError` declaration and the `validate` function;
    - the resulting document;
    - one sentence on `Pointer`'s escaping, with the examples `a/b` → `#/a~1b` and `città` → `#/citt%C3%A0`;
    - one sentence saying that `WithErrors` owns the `errors` member, and that the last option wins.
  - In the "Reading problem details" section, mention `p.Errors()` for reading the validation items of another API.
  - In `problem/doc.go`, add a section `# Validation errors` with the same content in brief.
- [X] T024 [US4] In `problem/CHANGELOG.md`, under `### Added`, add the bullets of the [research R-01](research.md#r-01-release-level-and-api-compatibility) draft for `Item`/`WithErrors`/`(*Error).Errors` and for `Pointer`.

**Checkpoint**: `GOWORK=off go test -race -run 'WithErrors_|Errors_|Pointer_|ExampleWithErrors|ExamplePointer|ExampleError_Errors' -v .` passes, and `GOWORK=off go test -run '^$' -fuzz '^FuzzPointer$' -fuzztime 30s .` finds nothing.

---

## Phase 7: User Story 5 - The documentation never discards the write error (Priority: P3)

**Goal**: the README, `doc.go` and every example handle `Write`'s error. The README explains it,
and the middleware snippet keeps the cause (FR-019 to FR-021).

**Independent Test**: the snippet tests run the README code verbatim. The examples pass, and the
`grep` of [quickstart.md §5](quickstart.md#5-documentation) prints nothing.

### Tests for User Story 5 ⚠️

- [X] T025 [US5] In `problem/write_test.go`, the README snippets are copied verbatim into tests, so a test fails whenever a snippet is broken:
  1. **Update `TestWrite_RejectFuncSnippet`**:
     - Its function body becomes exactly the README snippet of [research R-10](research.md#r-10-documentation-and-examples-i3-i4-fr-019-to-fr-025): `if err := problem.Write(w, r, problem.Status(rej.Status, problem.WithCause(rej.Err))); err != nil { logger.ErrorContext(r.Context(), "write problem", "err", err) }`.
     - `logger` is `slog.New(slog.NewTextHandler(&logs, nil))`, where `logs` is a `bytes.Buffer`.
     - Keep the existing statuses and assertions, including `Retry-After` kept and `wantAbsent(t, rec, "refused")`.
     - Also assert that `logs.Len() == 0`.
     - The comment above the function says that the snippet is the one in README.md and doc.go.
  2. **Add `TestWrite_ErrorLevelsSnippet`**:
     - Copy the README `writeProblem` helper of [research R-10](research.md#r-10-documentation-and-examples-i3-i4-fr-019-to-fr-025) as a closure over `logger := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))`.
     - Cases:
       - an `invalid problem` (`problem.Status(http.StatusConflict, problem.WithInstance("/a b"))`, with a recorder): the logs contain `level=ERROR` and `msg="problem replaced by a generic 500"`;
       - a `client gone` case (`creditOccurrence()` written to a `&failingWriter{}`): the logs contain `level=DEBUG` and `msg="write problem"`, and no `level=ERROR`;
       - a `success` (`creditOccurrence()`, with a recorder): the logs are empty.

### Implementation for User Story 5

- [X] T026 [P] [US5] In `problem/example_test.go`, replace every `_ = problem.Write(...)` with handled code. Each `// Output:` stays unchanged.
  - **`ExampleWithCause` and `ExampleWrite_error`**: `if err := problem.Write(rec, req, e); err != nil { slog.ErrorContext(req.Context(), "write problem", "err", err) }`, with `req` hoisted into a variable.
  - **The handler in `ExampleWrite`**: `if werr := problem.Write(w, r, fmt.Errorf("send message: %w", err)); werr != nil { slog.ErrorContext(r.Context(), "write problem", "err", werr) }`.

  Then confirm that `grep -n '_ = problem.Write' problem/example*_test.go` prints nothing.
- [X] T027 [US5] Update `problem/README.md` (FR-019 to FR-021; research R-10):
  - **"Answering requests"**: the handler becomes `if werr := problem.Write(w, r, err); werr != nil { logger.ErrorContext(r.Context(), "write problem", "err", werr) }`.
  - **New subsection `### When Write returns an error`**:
    - an error that matches `errors.Is(werr, problem.ErrInvalid)` means that the problem was built incorrectly and was replaced by the generic 500, for example `problem.WithInstance("/msgs/a b")`. This is a bug to fix;
    - any other error usually means that the client went away while the response was being sent;
    - then the `writeProblem` helper of research R-10, which T025 tests.
  - **"Using it with the middleware module"**:
    - the snippet becomes the 5-line version of research R-10, with `problem.WithCause(rej.Err)` and the error handled;
    - "in two lines" becomes "in a few lines";
    - one sentence says that the cause is there for logs and never reaches the client.

  Confirm that `grep -n '_ = problem.Write' problem/README.md` prints nothing.
- [X] T028 [US5] Update `problem/doc.go`:
  - the handler snippet of "Answering requests" handles the error, as in the README;
  - a short paragraph says what the error means: `ErrInvalid` is a bug in how the problem was built, and other errors usually mean that the client went away;
  - in "Using it with the middleware module", the snippet becomes the 5-line version, and "in two lines" becomes "in a few lines".

  Confirm that `grep -n '_ = problem.Write' problem/doc.go` prints nothing.

**Checkpoint**: `GOWORK=off go test -race -run 'Snippet|Example' -v .` passes, and
`grep -n '_ = problem.Write' problem/README.md problem/doc.go problem/example*_test.go` prints
nothing (SC-007).

---

## Phase 8: User Story 6 - Translate domain errors into problems (Priority: P3)

**Goal**: the README section "Translating domain errors", with the same pattern as a compiled and
tested example (FR-022 to FR-024).

**Independent Test**: `Example_translateDomainErrors` passes, and its output shows that no domain
error message reaches a response (SC-008).

### Tests for User Story 6 ⚠️

- [X] T029 [US6] Create the whole-file example `problem/example_translate_test.go` (`package problem_test`), with exactly one example. Write it with its full `// Output:` first, and run it (FR-024).
  - **Declarations**:
    - `var OutOfCredit = problem.Must(problem.NewType("https://example.com/probs/out-of-credit", "You do not have enough credit.", http.StatusForbidden))`;
    - `var Balance = problem.Must(problem.NewKey[int]("balance"))`;
    - the sentinels `ErrNotFound = errors.New("store: not found")` and `ErrEmailTaken = errors.New("store: email already taken")`;
    - `type InsufficientFundsError struct { Balance, Price int }`, whose `Error()` returns `fmt.Sprintf("insufficient funds: balance %d, price %d", e.Balance, e.Price)`.
  - **`func toProblem(err error) error`**:
    - `errors.Is(err, ErrNotFound)` gives `problem.Status(http.StatusNotFound, problem.WithDetail("The message does not exist."), problem.WithCause(err))`;
    - `errors.Is(err, ErrEmailTaken)` gives `problem.Status(http.StatusConflict, problem.WithDetail("An account with this email address already exists."), problem.WithCause(err))`, with a comment showing the **wrong** `problem.WithDetail(err.Error())` and saying why it leaks;
    - `errors.AsType[*InsufficientFundsError](err)` gives `OutOfCredit.New(problem.WithDetail(fmt.Sprintf("Your current balance is %d, but that costs %d.", fe.Balance, fe.Price)), problem.WithExtension(Balance, fe.Balance), problem.WithCause(err))`;
    - otherwise `return err`.
  - **`func Example_translateDomainErrors()`**:
    - It builds four errors:
      - `fmt.Errorf("get message %q: %w", "abc", ErrNotFound)`;
      - `fmt.Errorf("send message: %w", &InsufficientFundsError{Balance: 30, Price: 50})`;
      - ``fmt.Errorf("create user: %w: %w", ErrEmailTaken, errors.New(`pq: duplicate key value violates unique constraint "users_email_key"`))``;
      - `errors.New("pq: connection refused")`.
    - It writes `toProblem(err)` for each with a recorder, handling `Write`'s error with `slog.ErrorContext`.
    - It prints `rec.Code` and `rec.Body` for each, and appends the bodies to a `strings.Builder`.
    - Finally it prints `fmt.Println("leaked:", strings.Contains(all, "users_email_key") || strings.Contains(all, "pq:") || strings.Contains(all, "insufficient funds"))`.
  - **The `// Output:` lines**:
    - `404` and `{"type":"about:blank","title":"Not Found","status":404,"detail":"The message does not exist."}`;
    - `403` and `{"type":"https://example.com/probs/out-of-credit","title":"You do not have enough credit.","status":403,"detail":"Your current balance is 30, but that costs 50.","balance":30}`;
    - `409` and `{"type":"about:blank","title":"Conflict","status":409,"detail":"An account with this email address already exists."}`;
    - `500` and `{"type":"about:blank","title":"Internal Server Error","status":500}`;
    - `leaked: false`.

### Implementation for User Story 6

- [X] T030 [US6] In `problem/README.md`, add `## Translating domain errors` after "Validation errors" (FR-022, FR-023). It contains:
  - one paragraph: mapping arbitrary errors is left to the application, and this is the recommended pattern;
  - the `toProblem` function of T029, with its domain error declarations;
  - one sentence: errors that `toProblem` does not recognize are returned unchanged, and `Write` answers them with the generic 500;
  - the rule, in bold: **never put `err.Error()` in `WithDetail`**, because the message can reveal internals such as a database constraint name;
  - a wrong and right pair:
    - wrong: `problem.WithDetail(err.Error())`, which sends `pq: duplicate key value violates unique constraint "users_email_key"`;
    - right: `problem.WithDetail("An account with this email address already exists."), problem.WithCause(err)`;
  - one sentence: the detail and the extensions come from the domain error's fields, as with `InsufficientFundsError`.

**Checkpoint**: `GOWORK=off go test -run 'Example_translateDomainErrors' -v .` passes, with
`leaked: false`.

---

## Phase 9: Polish & Cross-Cutting Concerns

**Purpose**: verify the whole change against the contracts and the quickstart, and run the gates.

- [X] T031 Verify the exported surface against [contracts/public-api.md](contracts/public-api.md). In `problem/`, run `GOWORK=off go doc -all .`. It must show exactly 5 new identifiers, each with a doc comment that states every rule of the contract: `Error.With`, `Item`, `WithErrors`, `Error.Errors` and `Pointer`. It must also show the updated doc comments of `Write` (the 7 headers), `Key.Lookup` (nil) and `NewKey` (`errors`). Then check `GOWORK=off go test -coverprofile=c.out . && go tool cover -func=c.out`: no function in `pointer.go`, `validation.go` or `problem.go` may be at 0%. Delete `c.out` afterwards.
- [X] T032 [P] Check `problem/README.md` against [quickstart.md §5](quickstart.md#5-documentation):
  - the `##` sections are, in order: "Declaring problem types", "Answering requests" (with "When Write returns an error"), "Adding members when writing", "Validation errors", "Translating domain errors", "Reading problem details", "Using it with the middleware module";
  - every Go snippet matches the code tested by T014, T022, T025 and T029.
- [X] T033 Run `cd problem && GOWORK=off go mod tidy -diff`, which must print nothing, and confirm that `problem/go.mod` still has no `require`. Then run the cross-build check of [quickstart.md §2](quickstart.md#2-tidy-and-api-compatibility) and of 004 quickstart §2: `GOWORK=off GOOS=$os GOARCH=$arch go vet ./...` for linux, darwin and windows × amd64 and arm64. There must be no failures.
- [X] T034 Run `FuzzPointer` and `FuzzParse` for 10 minutes each ([quickstart.md §4](quickstart.md#4-fuzzing-fr-013-fr-016-sc-005)). Fix any failure in the implementation, and keep the failing input that the fuzzer writes under `problem/testdata/fuzz/<Target>/` as a regression seed.
- [X] T035 Run the benchmarks of `problem/bench_test.go` with `cd problem && GOWORK=off go test -run '^$' -bench . -benchmem -count 5 .`. `BenchmarkWrite` must stay at 38 allocations per operation, with a time per operation close to the baseline of research R-02. `BenchmarkParse` must stay well under 100 ms per operation. Record the medians for the pull request description (T039).
- [X] T036 Run the manual middleware check of [quickstart.md §6](quickstart.md#6-integration-with-the-middleware-module-manual) in a scratch directory outside the repository, with the README's 5-line refusal function. The second response must be a 429 with `Retry-After` and the complete body. Never commit the scratch module.
- [X] T037 From `problem/`, run the scenario checks of [quickstart.md §3](quickstart.md#3-scenario-checks-per-user-story) one row at a time, and the documentation checks of §5. Every command must pass, and the SC-007 `grep` must print nothing.
- [X] T038 Run the local gates on `problem/` with `make check MODULE=problem`:
  - golangci-lint reports zero findings, and `git diff main -- problem | grep -c 'nolint'` is 0 (no new directive);
  - `go test -race -shuffle=on` passes, with coverage ≥ 80%. Compare it with the baseline of T002.

  If Gitleaks or Semgrep later flag the fake database message of T029, add a narrow ignore entry with a justification, as the constitution's "Finding suppression" rule requires, rather than changing the example.
- [X] T039 Draft the pull request for the changes in `problem/`, for the maintainer, and do not open it without their go-ahead:
  - **Title**: `fix(problem): remove representation headers and add derivation and validation errors`, a Conventional Commit with the `problem` scope.
  - **Description**:
    - a summary of B1, B2 and I1 to I4, with the behavior-change note of the header fix;
    - the benchmark medians of T035;
    - a "Possible follow-ups" list with exactly these three items from the spec's "Out of scope":
      - a mapper or `From` API in the library;
      - making `Type` match `about:blank` problems by status code;
      - reading `application/json` responses that contain problem details.
    - the attribution line required for pull request descriptions.

---

## Dependencies & Execution Order

### Phase dependencies

- **Setup (Phase 1)** and **Foundational (Phase 2)** run first: T001 → T002.
- **US1 (Phase 3)** and **US2 (Phase 4)**: both depend only on Phase 2. They touch different code
  files (`write.go` and `write_test.go`, versus `extension.go` and `extension_test.go`), and both
  edit `CHANGELOG.md` (T008, T011).
- **US3 (Phase 5)**: depends only on Phase 2. Its new code is in `problem.go` and its new tests in
  `with_test.go`.
- **US4 (Phase 6)**: depends on US2, because T021 edits `extension.go` after T010, and on US3,
  because T023 places its README section after the one of T015. Its library code
  (`pointer.go`, `validation.go`) is independent.
- **US5 (Phase 7)**: depends on US1 (both edit `write_test.go`) and on US4 (both edit
  `example_test.go`, and T027 completes the README structure).
- **US6 (Phase 8)**: depends on US5, because T030 places its README section within the final
  structure. Its example (T029) uses only the API of v0.1.0, so it can be written at any time.
- **Polish (Phase 9)**: depends on every story.

The recommended order is therefore the priority order: US1 → US2 → US3 → US4 → US5 → US6. Each
story leaves `make check MODULE=problem` green.

### Within each story

- Test tasks come first, and are observed failing: a compilation failure, a panic or a wrong
  result. Exception: the documentation guards T025 and T029 test verbatim snippets, and may pass
  as soon as they are written.
- The implementation follows: library code, then examples, then README, `doc.go` and changelog.
- **Shared files**: tasks on the same file never run in parallel.

  | File | Edited by |
  |------|-----------|
  | `problem/write_test.go` | T003, T004, T005, T025 |
  | `problem/write.go` | T006 |
  | `problem/extension.go` | T010, T021 |
  | `problem/extension_test.go` | T009 |
  | `problem/problem.go` | T013 |
  | `problem/example_test.go` | T022, T026 |
  | `problem/fuzz_test.go` | T018 |
  | `problem/doc.go` | T007, T015, T023, T028 |
  | `problem/README.md` | T015, T023, T027, T030 |
  | `problem/CHANGELOG.md` | T008, T011, T016, T024 |

### Parallel opportunities

- **US1**: T003 → T004 → T005 (same file), then T006. T007 and T008 can run in parallel with each
  other, and with T006.
- **US2**: T009 → T010 → T011. With two developers, US2 can run alongside US1, with T008 and T011
  serialized because both edit `CHANGELOG.md`.
- **US3**: T012 → T013. Then T014, which can run in parallel with T015 and T016, since it is a
  different file. T015 and T016 follow T013.
- **US4**: T017 and T018 together (different files). Then T019 and T020: T019 can run in parallel
  with T020, and T020 needs nothing from T019 except in the T017 tests. Then T021, T022, T023
  and T024 in sequence.
- **US5**: T025, then T026 in parallel with T027, then T028.
- **US6**: T029 → T030.
- **Polish**: T031, T032 and T033 can run in parallel. T034–T039 run last.

---

## Parallel Example: User Story 4

```bash
# Write the failing tests together (different files):
Task: "T017 validation tests in problem/validation_test.go"
Task: "T018 FuzzPointer, decodePointer and FuzzParse additions in problem/fuzz_test.go"

# Then implement (different files):
Task: "T019 Pointer in problem/pointer.go"
Task: "T020 Item, WithErrors, Errors in problem/validation.go"
# then T021 (extension.go doc) → T022 (examples) → T023 (README, doc.go) → T024 (CHANGELOG)
```

## Parallel Example: User Story 3

```bash
Task: "T012 TestWith_* in problem/with_test.go"
# then
Task: "T013 (*Error).With in problem/problem.go"
# then in parallel
Task: "T014 ExampleError_With in problem/example_with_test.go"
Task: "T015 README and doc.go section" → "T016 CHANGELOG entry"
```

---

## Requirement Coverage

| Requirement | Tasks |
|-------------|-------|
| FR-001 | T003, T004, T006 |
| FR-002 | T005, T006 |
| FR-003 | T006, T007, T031 |
| FR-004 | T009, T010, T017.7, T020 |
| FR-005 | T012.1, T013, T015 |
| FR-006 | T012.2, T012.10, T012.11, T013 |
| FR-007, SC-004 | T012.3, T013 |
| FR-008 | T012.5, T012.6, T013 |
| FR-009 | T012.7, T013 |
| FR-010 | T012.8, T013 |
| FR-011 | T017.1, T017.2, T020 |
| FR-012, FR-014 | T017.8, T019 |
| FR-013, SC-005 | T017.9, T018, T019, T034 |
| FR-015 | T017.2, T020 |
| FR-016 | T017.5, T017.6, T018, T020 |
| FR-017 | T017.3, T020, T021, T023 |
| FR-018, SC-006 | T017.4, T018, T020 |
| FR-019, SC-007 | T026, T027, T028, T037 |
| FR-020 | T025.2, T027, T028 |
| FR-021 | T025.1, T027, T028, T036 |
| FR-022, FR-023 | T029, T030 |
| FR-024, SC-008 | T029 |
| FR-025 | T014, T022 |
| FR-026 | T008, T011, T016, T024 |
| FR-027 | T003, T004, T005, T009, T012.3, T018 |
| SC-001, SC-002 | T003, T004, T005 |
| SC-003 | T009, T012.7, T017.7 |
| SC-009 | T031, T037, T038, and gorelease in CI |

---

## Implementation Strategy

### MVP first (User Story 1)

1. Phase 1 and Phase 2: T001–T002, the branch and a green baseline.
2. Phase 3, US1: T003–T008. This fixes the bug that corrupts responses.
3. **Stop and validate**: run `make check MODULE=problem`. This increment could ship alone as
   a fix release.

### Incremental delivery

1. US2 (P1): the `Lookup(nil)` crash. Both bugs are now fixed.
2. US3 (P2): `With`, so that the HTTP layer can add a request ID.
3. US4 (P2): validation errors and `Pointer`, with fuzzing.
4. US5 (P3): every example handles `Write`'s error, and the README explains it.
5. US6 (P3): translating domain errors.
6. Polish: contracts, fuzzing, benchmarks, gates, and the PR draft with the follow-ups.

Each step leaves `make check MODULE=problem` green, so the work can pause after any story. The
release `problem/v0.2.0` (research R-01) is prepared in a separate PR after the squash merge, as
for v0.1.0. The changelog entries stay under `[Unreleased]` until then.

---

## Notes

- Every task stays within the module `problem/`. `testdata/fuzz/` holds only regression seeds
  from the fuzzer.
- Never weaken a test to make it pass. Fix the implementation, or update the spec and the
  contracts first if the behavior itself must change.
- The research spike lives only in the session scratchpad. Re-implement from these tasks; do
  not copy files from it.

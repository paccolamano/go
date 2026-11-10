---

description: "Task list for the relative type match fix in the problem module"
---

# Tasks: Relative Problem Types Match Problems Read From a Response (problem)

**Input**: Design documents from `specs/012-fix-relative-type-match/`

**Prerequisites**: [plan.md](plan.md), [spec.md](spec.md), [research.md](research.md),
[data-model.md](data-model.md), [contracts/](contracts/), [quickstart.md](quickstart.md)

**Tests**: REQUIRED. Constitution Principle III is non-negotiable, and FR-014 and FR-015 ask for
them:

- Tests are written first and observed failing (T008).
- The fix has regression tests that fail without it: the in-memory table (T004) and the loopback
  server (T005). T017 checks again, with the fix stashed.
- `FuzzRead` covers the resolution of a declaration against an external URL (T007).
- `ExampleType_Match_shared` is the runnable example of FR-012 (T023).
- Coverage stays above 99% with `-race -shuffle=on`. It was 99.6% at `problem/v0.2.0`.

Every test task below precedes the implementation it covers.

**Organization**: tasks are grouped by user story (spec.md US1–US3). All paths are relative to the
repository root. All code lives in the existing module `problem/`, in its only package. Nothing
outside `problem/` changes, apart from ticking the boxes of this file.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: can run in parallel with other [P] tasks of the same phase: different files, and no
  dependency on an incomplete task.
- **[Story]**: US1–US3 from spec.md. Setup, Foundational and Polish tasks have no story label.

## Conventions for every task

- **Commands** run from `problem/` with `GOWORK=off`, unless a task says otherwise. `make` targets
  and `scripts/` run from the repository root.
- **Packages**: tests use the external test package `problem_test`. Reuse the existing helpers:
  - `problem/helpers_test.go`: `newRequest`, `sameProblem`, `fmtExtensions`, the declarations
    `outOfCredit` (absolute, `https://example.com/…`), `serviceUnavailable` and
    `validationError`, and the key `balance`;
  - `problem/read_test.go`: `response(tb, code, contentType, body)`, whose request is a GET of
    `https://api.example.com/v1/messages`;
  - `problem/with_test.go`: the key `requestID`;
  - the fixtures that T003 adds: `sharedCredit`, `apiCredit`, `apiTarget`, `creditDoc` and
    `readProblem`.
- **Test names**: those of [research R-06](research.md#r-06-testing-approach-fr-014-fr-015-sc-001-to-sc-005-principle-iii),
  chosen so that the `-run` patterns of [quickstart.md §4](quickstart.md#4-scenario-checks-per-user-story)
  select them: `TestMatch_ReadResponse`, `TestMatch_ReadFromServer`, `TestMatch_ReadTree`,
  `TestMatch_Allocs`, `TestRead_WriteAgain`, `FuzzRead` and `ExampleType_Match_shared`.
- **Test style**:
  - table-driven, with `t.Run`;
  - loopback servers from `httptest.NewServer`, closed with `defer srv.Close()`, as in
    `TestWrite_RealServer`. No real network and no `time.Sleep`;
  - requests from `newRequest`, `http.NewRequestWithContext(t.Context(), …)` or
    `httptest.NewRequestWithContext` (`noctx`, `usetesting`). Response bodies are closed with
    `_ = resp.Body.Close()`;
  - a server handler reports failures with `t.Errorf`, never `t.Fatal`;
  - helpers take `testing.TB` (or `*testing.T`) first and call `tb.Helper()` first (`thelper`).
- **Lint**: golangci-lint must report zero findings, and the change adds **no** `//nolint`
  directive. The existing `//nolint:errorlint` on the type switch of `match` stays as it is.
  Imports group `github.com/paccolamano/go` last (`goimports`).
- **Documentation**: when [contracts/public-api.md](contracts/public-api.md) or a task gives the
  text of a doc comment or of a paragraph, use it verbatim.
- **Commits**: do not commit, push or open a pull request unless the maintainer asks for it.

---

## Phase 1: Setup (Shared Infrastructure)

**Purpose**: work on a feature branch from a known state.

- [X] T001 From the repository root, confirm that `git status --short` lists only `go.work.sum` and `specs/012-fix-relative-type-match/` as untracked, with no change under `problem/`, and that `git tag --list 'problem/*'` ends with `problem/v0.2.0`. If the current branch is `main`, create the feature branch with `git switch -c 012-fix-relative-type-match` before editing any file. Creating the branch is not a commit.

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: record the baseline, and add the test fixtures that the tests of US1 and US2 share.

**⚠️ CRITICAL**: no user story work begins until this phase is complete.

- [X] T002 Record the baseline. From the repository root, run `make check MODULE=problem`, which must report `PASS` for lint and test. From `problem/`, run `GOWORK=off go test -cover .` and record the percentage: 99.6% at `problem/v0.2.0`.
- [X] T003 In `problem/helpers_test.go`, add the shared fixtures of this feature:
  1. In the `var` block "Shared declarations", after `serviceUnavailable`:
     - `sharedCredit = problem.Must(problem.NewType("/probs/out-of-credit", "You do not have enough credit.", http.StatusForbidden))`, with the comment `// sharedCredit is declared with a relative URI, as a service shares it with its clients.`;
     - `apiCredit = problem.Must(problem.NewType("https://api.example.com/probs/out-of-credit", "You do not have enough credit.", http.StatusForbidden))`, with the comment `// apiCredit is the absolute form of sharedCredit on the host of apiTarget.`. (Moved to T013 on 2026-11-10: nothing uses it before US2, and the `unused` linter failed the US1 checkpoint of T012.)
  2. A constant `apiTarget = "https://api.example.com/v1/messages"`, with the comment `// apiTarget is the request URL of the responses that response() builds.`.
  3. A function `creditDoc(typ string) string`, with the doc comment `// creditDoc returns a problem document of type typ, with a balance of 30.`. It returns `fmt.Sprintf(`{"type":%q,"title":"You do not have enough credit.","status":403,"balance":30}`, typ)`. `%q` gives valid JSON for the ASCII URIs that the tests use.
  4. A function `readProblem(tb testing.TB, target, doc string) *problem.Error`, with the doc comment `// readProblem reads doc with Read from a response to a GET of target, or from a response without a request when target is "".`. It calls `tb.Helper()` first, then:
     - `resp := response(tb, http.StatusForbidden, problem.MediaType, strings.NewReader(doc))`;
     - `resp.Request = nil`, then, when `target != ""`, `resp.Request = newRequest(tb, http.MethodGet, target)`;
     - `p, err := problem.Read(resp)`, calling `tb.Fatalf("Read: %v", err)` on an error, and returns `p`.

  Run `GOWORK=off go vet . && GOWORK=off go test .`: both pass, because nothing uses the fixtures yet.

**Checkpoint**: the fixtures exist, and every user story can begin.

---

## Phase 3: User Story 1 - A client recognizes the problems of a relative type it shares with the service (Priority: P1) 🎯 MVP

**Goal**: `Type.Match` resolves a relative declaration against the URL that `Read` resolved the
problem against, then compares (FR-001, FR-004, FR-005, FR-014, FR-015).

**Independent Test**: write a problem of a relative declared type in a response, read it back as a
client does, with the request URL set, and apply the type test with the same declaration.

### Tests for User Story 1 ⚠️ (write first, see them fail)

- [X] T004 [US1] In `problem/type_test.go`, add `TestMatch_ReadResponse`, after `TestMatch_Tree` (FR-007 rows 1–4, FR-014; US1 scenarios 1–3; spec edge cases "Network-path reference" and "Document without a type"). It is a table over `name`, `typ problem.Type`, `p *problem.Error`, `wantType string` and `want bool`. Every problem comes from `readProblem(t, apiTarget, creditDoc(…))`. Rows:
  - `"relative, same relative type"`: `sharedCredit`, document type `/probs/out-of-credit`, `wantType` `https://api.example.com/probs/out-of-credit`, `want: true`;
  - `"relative, same absolute type"`: `sharedCredit`, document type `https://api.example.com/probs/out-of-credit`, the same `wantType`, `want: true`;
  - `"relative, other host"`: `sharedCredit`, document type `https://other.example.com/probs/out-of-credit`, `wantType` as written, `want: false`;
  - `"relative, other path"`: `sharedCredit`, document type `/probs/other`, `wantType` `https://api.example.com/probs/other`, `want: false`;
  - `"network path"`: a declaration `problem.Must(problem.NewType("//cdn.example.com/probs/out-of-credit", "t", http.StatusForbidden))`, document type `//cdn.example.com/probs/out-of-credit`, `wantType` `https://cdn.example.com/probs/out-of-credit`, `want: true`;
  - `"document without a type"`: `sharedCredit`, the problem `readProblem(t, apiTarget, `{"status":403}`)`, `wantType` `about:blank`, `want: false`.

  Each subtest asserts:
  1. `tt.p.Type() == tt.wantType`;
  2. `got, ok := tt.typ.Match(tt.p)` gives `ok == tt.want`;
  3. when `tt.want`, `got == tt.p`, and `balance.Lookup(got)` returns `30, true`: every matching row has the balance of `creditDoc`;
  4. when not `tt.want`, `got == nil`.

  Leave room in the table for the rows of T013, which come from other sources.
- [X] T005 [US1] In `problem/type_test.go`, add `TestMatch_ReadFromServer` (FR-001, FR-004, FR-006, FR-014; SC-002; US1 scenario 5; spec edge case "Redirects"; research R-06). Add the import `net/http/httptest`.
  1. **Declarations**: a table of `name, uri`, each declared with `problem.Must(problem.NewType(uri, "You do not have enough credit.", http.StatusForbidden))` and kept in a `map[string]problem.Type` keyed by `uri`. Use exactly these 12 rows, which no two resolve alike:
     - `"full path"`, `/problems/user-not-registered`;
     - `"query"`, `/probs/out-of-credit?lang=en`;
     - `"fragment"`, `/probs#out-of-credit`;
     - `"percent-encoded space"`, `/probs/out%20of%20credit`;
     - `"percent-encoded UTF-8"`, `/probs/caf%C3%A9`;
     - `"percent-encoded slash"`, `/probs/a%2Fb`;
     - `"percent-encoded tilde"`, `/probs/%7Euser`;
     - `"dot segments"`, `/probs/./x/../out-of-credit`;
     - `"network path"`, `//cdn.example.com/probs/out-of-credit`;
     - `"root"`, `/`;
     - `"root with query"`, `/?type=credit`;
     - `"upper case"`, `/PROBS/Out-Of-Credit`.
  2. **Servers**: a handler `write` that calls `problem.Write(w, r, types[r.Header.Get("Test-Type")].New())` and reports a non-nil error with `t.Errorf`. Server `b := httptest.NewServer(http.HandlerFunc(write))`. Server `a` answers the path `/redirect` with `http.Redirect(w, r, b.URL+"/after/redirect?x=1", http.StatusTemporaryRedirect)`, and every other path with `write`. Close both with `defer`. `http.Client` forwards the `Test-Type` header across the redirect.
  3. **Request URLs**: `a.URL+"/api/v1/users/me"`, `a.URL+"/"`, `a.URL+"/deep/a/b/c/?q=1"`, `b.URL+"/other/port"`, `a.URL+"/redirect"` and `a.URL+"/x;p?q#frag"`.
  4. **A local helper** `get(t *testing.T, target, uri string) *problem.Error`. It calls `t.Helper()`, builds the request with `http.NewRequestWithContext(t.Context(), http.MethodGet, target, nil)`, sets `Test-Type` to `uri`, sends it with `a.Client().Do`, and closes the body with `defer func() { _ = resp.Body.Close() }()`. It asserts status 403, reads the problem with `problem.Read(resp)`, and calls `t.Fatalf` on any error.
  5. **One subtest per declaration**, named by its `name`. For each request URL, `p := get(t, target, uri)`, then:
     - `types[uri].Match(p)` returns `p, true`. On failure, report the declaration, the target and `p.Type()`;
     - `types[uri].Match(p.With(problem.WithDetail("derived")))` returns `true`;
     - for every other declaration `o`, `types[o].Match(p)` returns `false`.
  6. **Subtest `"redirect"`**: `p := get(t, a.URL+"/redirect", "/problems/user-not-registered")`. Then:
     - `p.Type()` is `b.URL+"/problems/user-not-registered"`;
     - a declaration of the absolute URI `a.URL+"/problems/user-not-registered"` does not match `p`;
     - a declaration of `b.URL+"/problems/user-not-registered"` matches it.

  These are 72 combinations: 12 declarations by 6 request URLs, a second port and a redirect among them.
- [X] T006 [US1] In `problem/type_test.go`, add `TestMatch_ReadTree` (FR-005; US1 scenario 4; spec edge cases "Problems from several services in one error" and "Absent problem, absent error or undeclared type"):
  1. **Found**: `p := readProblem(t, apiTarget, creditDoc("/probs/out-of-credit"))`, and a table over `name, err` that must give `sharedCredit.Match(err) == (p, true)`:
     - `"itself"`: `p`;
     - `"wrapped twice"`: `fmt.Errorf("send message: %w", fmt.Errorf("charge message: %w", p))`;
     - `"joined with a plain error"`: `errors.Join(errors.New("plain"), p)`;
     - `"after a problem of another type"`: `errors.Join(serviceUnavailable.New(), p)`;
     - `"as the cause of another problem"`: `serviceUnavailable.New(problem.WithCause(p))`.
  2. **Two services**: `pa := readProblem(t, "https://a.example.com/v1", creditDoc("/probs/out-of-credit"))`, `pb := readProblem(t, "https://b.example.com/v1", creditDoc("/probs/out-of-credit"))` and `pOther := readProblem(t, "https://a.example.com/v1", creditDoc("/probs/other"))`. Assert that:
     - `sharedCredit.Match(errors.Join(serviceUnavailable.New(), pa, pb))` returns `pa`, the first match;
     - `sharedCredit.Match(errors.Join(pOther, pb))` returns `pb`;
     - `problem.Must(problem.NewType("https://b.example.com/probs/out-of-credit", "t", http.StatusForbidden)).Match(errors.Join(pa, pb))` returns `pb`, because each problem is compared with its own URL.
  3. **Not found**: `(problem.Type{}).Match(p)` and `sharedCredit.Match(nil)` return `nil, false`.
- [X] T007 [P] [US1] In `problem/fuzz_test.go`, add `FuzzRead`, after `FuzzParse` (FR-015; SC-004). Add the imports `encoding/json` and `io`. Doc comment: `// FuzzRead checks that a type declared with the type URI of a document, as written, matches the problem that Read reads from it, whatever the request URL.`
  1. **Seeds** `f.Add(doc, target string)`:
     - with `apiTarget` and with `""`, the documents `creditDoc(t)` for each `t` in `/probs/out-of-credit`, `https://api.example.com/probs/out-of-credit`, `https://other.example.com/probs/out-of-credit`, `/probs/other`, `//cdn.example.com/probs/out-of-credit`, `/probs/./x/../out-of-credit` and `/probs/out-of-credit?lang=en#top`. These include the documents of every row of FR-007;
     - `{}` with `apiTarget`;
     - `{"type":"/x"}` with `/relative/base`, with `about:blank` and with `mailto:a@b`.
  2. **Body**:
     - build `resp := &http.Response{StatusCode: http.StatusForbidden, Header: http.Header{"Content-Type": {problem.MediaType}}, Body: io.NopCloser(strings.NewReader(doc))}`;
     - when `target != ""`, `u, err := url.Parse(target)`, returning on an error, then `req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil)`, `req.URL = u` and `resp.Request = req`;
     - `p, err := problem.Read(resp)`, returning on an error;
     - decode `doc` into `map[string]json.RawMessage`, then its `"type"` member into a string `typ`, returning on either error;
     - `decl, err := problem.NewType(typ, "t", http.StatusBadRequest)`, returning on an error, because `typ` cannot be declared;
     - `got, ok := decl.Match(p)` must be `p, true`. Otherwise call `t.Fatalf` with `typ`, `target` and `p.Type()`.
- [X] T008 [US1] From `problem/`, run `GOWORK=off go test -race -run 'TestMatch_Read|FuzzRead' -v .` and observe the expected failures:
  - `TestMatch_ReadResponse`: `relative,_same_relative_type`, `relative,_same_absolute_type` and `network_path` fail, with no match. The other rows pass;
  - `TestMatch_ReadFromServer`: every declaration subtest fails to match its own problem, with a type such as `http://127.0.0.1:<port>/problems/user-not-registered`. No declaration matches another's problem. In `redirect`, only the declaration of `b.URL` matches;
  - `TestMatch_ReadTree`: the found cases and the first two two-service cases fail. The absolute declaration of `b.example.com` and the not-found cases pass;
  - `FuzzRead`: the seeds with a relative type and the request URLs `apiTarget`, `about:blank` or `mailto:a@b` fail. The seeds with an absolute type, without a request, or with `/relative/base` pass.

  Do not continue until these failures are seen.

### Implementation for User Story 1

- [X] T009 [US1] In `problem/problem.go`, add the import `net/url`. In `Error`, between `cause` and the comment of `invalid`, add the field `base *url.URL` with the comment `// base is the URL that Read resolved typ and instance against, or nil. It is a copy that nothing modifies, and Write never writes it.`, wrapped at 80 columns like the other field comments (research R-02; data-model §2). Nothing else changes. `With` keeps `base` through `c := *e` (FR-006).
- [X] T010 [P] [US1] In `problem/read.go`, after T009, replace the body of `if resp.Request != nil && resp.Request.URL != nil { … }` in `Read` with the following, and leave the doc comment for T019 (research R-02):
  - `base := *resp.Request.URL`, with the comment `// A copy: the caller may change its request after Read returns.`;
  - `e.base = &base`;
  - `e.typ = resolve(e.base, e.typ)`;
  - `e.instance = resolve(e.base, e.instance)`.
- [X] T011 [P] [US1] In `problem/type.go`, after T009 (research R-03; data-model §3):
  1. Change `import "fmt"` to an import block with `fmt` and `strings`.
  2. In `match`, replace the case `x.typ == uri` with `x.hasType(uri)`. Leave the `//nolint:errorlint` directive and the rest of the walk unchanged.
  3. After `match`, add:

     ```go
     // hasType reports whether e is of the type declared with uri. A relative uri,
     // which begins with "/", is resolved against the URL that Read resolved e's
     // members against, so both sides of the comparison share one base. Otherwise
     // uri is compared as written.
     func (e *Error) hasType(uri string) bool {
     	if e.base != nil && strings.HasPrefix(uri, "/") {
     		return e.typ == resolve(e.base, uri)
     	}
     	return e.typ == uri
     }
     ```

     The guard order matters: an absolute declaration, or a problem without a base, never reaches `resolve`, so it allocates nothing (SC-003).

  Leave the `Match` doc comment for T018.
- [X] T012 [US1] Run `GOWORK=off go test -race -shuffle=on ./...`. Every test of T004–T007 must pass, and every existing test must still pass, `ExampleRead` included. Then run `GOWORK=off go test -run '^$' -fuzz '^FuzzRead$' -fuzztime 30s .`, which must find no failure. Discard any `testdata/fuzz/FuzzRead` directory it creates. Finally, from the repository root, run `make check MODULE=problem`, which must report `PASS`.

**Checkpoint**: a client recognizes the problems of a relative type it shares with the service.
This is the MVP.

---

## Phase 4: User Story 2 - What matched still matches, and nothing else starts matching (Priority: P1)

**Goal**: prove that every case outside US1 gives the results of `problem/v0.2.0`, with no new
allocation (FR-002, FR-003, FR-006, FR-008, FR-009; SC-003).

**Independent Test**: apply the table of FR-007 to problems built locally, read from a document
alone, read from a response without a request, and derived, and compare with `problem/v0.2.0`.

### Tests for User Story 2 ⚠️ (they guard unchanged behavior; T017 observes the regression)

- [X] T013 [US2] Add the remaining rows of FR-007 to the table of `TestMatch_ReadResponse` in `problem/type_test.go` (FR-002, FR-003, FR-007; US2 scenarios 1–3). For the `Parse` rows, add a local helper `parse(doc string) *problem.Error` that calls `t.Fatalf` on an error. Rows:
  - `"relative, read without a request"`: `sharedCredit`, `readProblem(t, "", creditDoc("/probs/out-of-credit"))`, `wantType` `/probs/out-of-credit`, `want: true`;
  - `"relative, parsed relative type"`: `sharedCredit`, `parse(creditDoc("/probs/out-of-credit"))`, `wantType` `/probs/out-of-credit`, `want: true`;
  - `"relative, parsed absolute type"`: `sharedCredit`, `parse(creditDoc("https://api.example.com/probs/out-of-credit"))`, `wantType` as written, `want: false`;
  - `"relative, built locally"`: `sharedCredit`, `sharedCredit.New(problem.WithExtension(balance, 30))`, `wantType` `/probs/out-of-credit`, `want: true`;
  - `"absolute, read relative type"`: `apiCredit`, `readProblem(t, apiTarget, creditDoc("/probs/out-of-credit"))`, `wantType` `https://api.example.com/probs/out-of-credit`, `want: true`;
  - `"absolute, parsed relative type"`: `apiCredit`, `parse(creditDoc("/probs/out-of-credit"))`, `wantType` `/probs/out-of-credit`, `want: false`.

  Add a comment above the table: `// The rows of FR-007 (spec 012). Only the first two changed from problem/v0.2.0.`
- [X] T014 [US2] In `problem/type_test.go`, add `TestMatch_Allocs` (SC-003; research R-05). Build every value before measuring, in a table over `name, typ problem.Type, err error, want bool`:
  - `"relative, built locally"`: `sharedCredit`, `sharedCredit.New()`, `true`;
  - `"relative, parsed"`: `sharedCredit`, the result of `problem.Parse([]byte(creditDoc("/probs/out-of-credit")))`, failing the test on an error, `true`;
  - `"relative, read without a request"`: `sharedCredit`, `readProblem(t, "", creditDoc("/probs/out-of-credit"))`, `true`;
  - `"relative, tree"`: `sharedCredit`, `fmt.Errorf("send: %w", errors.Join(errors.New("plain"), serviceUnavailable.New(), sharedCredit.New()))`, `true`;
  - `"absolute, read from a response"`: `apiCredit`, `readProblem(t, apiTarget, creditDoc("/probs/out-of-credit"))`, `true`;
  - `"absolute, read from a response, other type"`: `apiCredit`, `readProblem(t, apiTarget, creditDoc("/probs/other"))`, `false`;
  - `"absolute, built locally from the relative declaration"`: `apiCredit`, `sharedCredit.New()`, `false`.

  Each subtest first asserts that `_, ok := tt.typ.Match(tt.err)` gives `tt.want`. It then asserts that `testing.AllocsPerRun(100, func() { _, _ = tt.typ.Match(tt.err) })` is 0, and reports the count otherwise. A relative declaration against a problem read from a response is deliberately absent: that path allocates.
- [X] T015 [P] [US2] In `problem/with_test.go`, extend `TestWith_ReadProblem` (FR-006; US2 scenario 4). Keep its existing assertions, then add a table over `name, p *problem.Error, wantShared, wantAPI bool`, built from the document of the test:
  - `"read from a response"`: `p`, the problem the test already reads, `true`, `true`;
  - `"read without a request"`: `readProblem(t, "", doc)`, `true`, `false`.

  For each row, with `d := tt.p.With(problem.WithExtension(requestID, "r"))`, assert that:
  - `sharedCredit.Match(tt.p)` and `sharedCredit.Match(d)` both give `tt.wantShared`;
  - `apiCredit.Match(tt.p)` and `apiCredit.Match(d)` both give `tt.wantAPI`;
  - a match on `d` returns `d`, not `tt.p`.
- [X] T016 [P] [US2] In `problem/read_test.go`, add `TestRead_WriteAgain`, after `TestRead_RoundTrip` (FR-009; US2 scenario 5):
  - read the document `{"type":"/probs/out-of-credit","title":"You do not have enough credit.","status":403,"detail":"Your current balance is 30, but that costs 50.","instance":"/account/12345/msgs/abc","balance":30}` with `problem.Read(response(t, http.StatusForbidden, problem.MediaType, strings.NewReader(doc)))`;
  - write it with `problem.Write(rec, newRequest(t, http.MethodGet, "/"), p)` to `rec := httptest.NewRecorder()`, and assert a nil error;
  - assert that `rec.Body.String()` equals exactly `{"type":"https://api.example.com/probs/out-of-credit","title":"You do not have enough credit.","status":403,"detail":"Your current balance is 30, but that costs 50.","instance":"https://api.example.com/account/12345/msgs/abc","balance":30}`. This is the document that `problem/v0.2.0` writes: the resolved members, and no trace of the request URL beyond them.
- [X] T017 [US2] Run `GOWORK=off go test -race -run 'TestMatch_|TestWith_ReadProblem|TestRead_(Resolve|WriteAgain|RoundTrip)|TestNewType_Rules' -v .`. Every test passes. `TestNewType_Rules` already accepts `/probs/out-of-credit` and refuses `out-of-credit` (FR-008; US2 scenario 6), and `TestRead_Resolve` already checks the resolved type and instance (FR-009). Then observe the regression with the fix removed, from the repository root:
  1. run `git stash push -- problem/problem.go problem/read.go problem/type.go`;
  2. run `(cd problem && GOWORK=off go test -run 'TestMatch_ReadResponse|TestMatch_Allocs|TestWith_ReadProblem|TestRead_WriteAgain' -v .)`. See only the two changed rows of FR-007, the network-path row and the `read from a response` row of `TestWith_ReadProblem` fail. The rows of T013, `TestMatch_Allocs` and `TestRead_WriteAgain` pass, so the guards hold against `problem/v0.2.0`;
  3. run `git stash pop`, then the same command, and see every test pass.

  Never leave the stash applied.

**Checkpoint**: only the case of US1 changed. User Stories 1 and 2 together are the complete fix.

---

## Phase 5: User Story 3 - Developers learn the rule from the documentation (Priority: P2)

**Goal**: the documentation states the new rule, the old advice is gone, a runnable example shows
a shared declaration, and the changelog announces the behavior change (FR-011 to FR-013).

**Independent Test**: read the documentation, and run the runnable example.

- [X] T018 [P] [US3] In `problem/type.go`, replace the doc comment of `Type.Match` with the text of [contracts/public-api.md § Type.Match](contracts/public-api.md#typematch-fr-001-to-fr-005), verbatim. The sentence "a problem read with [Read] has its type URI resolved first, so a type declared to match another API's problems must use that API's absolute URI." disappears (FR-011).
- [X] T019 [P] [US3] In `problem/read.go`, replace the third paragraph of the doc comment of `Read`, the one that begins "The members are read as [Parse] reads them.", with the text of [contracts/public-api.md § Read](contracts/public-api.md#read-documentation-only), verbatim. Leave the other paragraphs unchanged (FR-011).
- [X] T020 [P] [US3] In `problem/doc.go`, section `# Reading problem details`, insert the paragraph of [contracts/public-api.md § Package documentation](contracts/public-api.md#package-documentation-docgo-fr-011), verbatim, as a new paragraph after the one that ends "against the URL of the request.". Separate it with a `//` line. Keep "Prefer an absolute URI; a relative one must begin with "/"." in the declaration section (FR-011; research R-07).
- [X] T021 [P] [US3] In `problem/README.md`, section "Reading problem details", after the paragraph that ends "`p.Errors()` returns the validation errors of a problem, when it has some.", add this paragraph, verbatim (FR-011; US3 scenario 2):

  > `OutOfCredit` can be the declaration that the service itself uses, even when its type URI is relative, such as `/probs/out-of-credit`. `Match` resolves a relative type URI against the same request URL as `Read`, so it matches the problems of that path on the server that answered. Declare an absolute URI to match the problems of one host only.

  Write it as plain Markdown, not as a quotation, wrapped at 100 columns like the rest of the README.
- [X] T022 [P] [US3] In `problem/CHANGELOG.md`, under `## [Unreleased]`, add a `### Fixed` section with the entry drafted in [research R-01](research.md#r-01-release-level-and-api-compatibility), verbatim (FR-013). Leave `## [0.2.0]` and the sections below it unchanged.
- [X] T023 [P] [US3] In `problem/example_test.go`, add `ExampleType_Match_shared`, after `ExampleType_Match` (FR-012; US3 scenario 2; [contracts/public-api.md § Added example](contracts/public-api.md#added-example-fr-012)):
  1. Declare `outOfCredit := problem.Must(problem.NewType("/probs/out-of-credit", "You do not have enough credit.", http.StatusForbidden))`, with the comment `// Declared by the service, and shared with its clients.`
  2. With the comment `// The service answers a request.`: `req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "https://api.example.com/v1/messages", nil)` and `rec := httptest.NewRecorder()`. If `problem.Write(rec, req, outOfCredit.New())` returns an error, print it and return.
  3. With the comment `// The client reads the response, with its request URL, as http.Client returns it.`: `resp := rec.Result()`, `resp.Request = req`, and `defer func() { _ = resp.Body.Close() }()`. If `p, err := problem.Read(resp)` returns an error, print it and return.
  4. `fmt.Println(p.Type())`, then `_, ok := outOfCredit.Match(p)` and `fmt.Println(ok)`.
  5. The output block is `// Output:`, `// https://api.example.com/probs/out-of-credit`, `// true`.

  Run `GOWORK=off go test -run 'ExampleType_Match_shared|ExampleRead|ExampleType_Match' -v .`: all three pass.
- [X] T024 [US3] Review the documentation in `problem/type.go`, `problem/read.go`, `problem/doc.go` and `problem/README.md` (FR-011; quickstart §6):
  - from the repository root, `grep -rn "absolute URI" problem/*.go problem/README.md` lists only the declaration rules (`doc.go`, the `NewType` doc comment and its error message) and the new advice to declare an absolute URI to match one host only. "must use that API's absolute URI" is gone;
  - `GOWORK=off go doc . Type.Match` and `GOWORK=off go doc . Read` show the texts of the contract;
  - `GOWORK=off go doc .` shows the new paragraph in "Reading problem details".

**Checkpoint**: every story is complete.

---

## Phase 6: Polish & Cross-Cutting Concerns

**Purpose**: verify the whole change against the contracts and the quickstart, and run the gates.

- [X] T025 [P] Verify that the exported surface did not change (FR-010). From the repository root, `git diff --stat -- problem/` must list only `CHANGELOG.md`, `README.md`, `doc.go`, `problem.go`, `read.go`, `type.go` and the test files of this plan. In `problem.go`, `read.go` and `type.go`, the diff adds no exported identifier and changes no signature. Then, from `problem/`, run `GOWORK=off go test -coverprofile=c.out . && go tool cover -func=c.out`: `hasType`, `Match` and `Read` must be at 100%, as `Match` and `Read` are at `problem/v0.2.0`. Only `Parse` may stay below, at its baseline of 96.3%. Delete `c.out` afterwards.
- [X] T026 [P] From `problem/`, check the dependencies and the platforms:
  - `GOWORK=off go mod tidy -diff` prints nothing;
  - `git diff --exit-code -- go.mod` passes; the module has no `go.sum`;
  - for every `os` in linux, darwin and windows, and every `arch` in amd64 and arm64, `GOWORK=off GOOS=$os GOARCH=$arch go vet ./...` passes.
- [X] T027 [P] Check API compatibility (FR-010; SC-005). If `gorelease` is installed at the version of `versions.env`, run `GORELEASE=$(command -v gorelease) scripts/apicompat.sh problem` from the repository root. The report must show no change and suggest `v0.2.1`. `apicompat.sh` reads the base from the tags, and `gorelease` refuses a repository with uncommitted changes: if the working tree is not committed, skip this check and note that the pull request workflow runs it. Do not install tools or commit to run it. (Run on 2026-11-10 in a throwaway clone in the session scratchpad, with the `gorelease` binary built during planning at the pinned version, and the changes committed only in that clone: no change, `Suggested version: v0.2.1`. `scripts/changelog-check.sh origin/main HEAD` passed in the same clone.)
- [X] T028 Run the checks of [quickstart.md](quickstart.md) on `problem/`, §2, §3 (except `changelog-check.sh` and `gorelease`, which need commits), §4 (one row at a time) and §5, with a 60-second fuzz run. Every command must pass. Discard any `testdata/fuzz/FuzzRead` directory the fuzz run creates, unless it holds a failure, which is then a defect to fix first.
- [X] T029 Run `make check MODULE=problem` from the repository root:
  - golangci-lint reports zero findings, and `git diff -- problem | grep -c 'nolint'` prints 0;
  - `go test -race -shuffle=on` passes, with coverage above 99%. Compare it with the baseline of T002.
- [X] T030 Draft the pull request for the changes in `problem/` and `specs/012-fix-relative-type-match/`, for the maintainer, and do not open it without their go-ahead:
  - **Title**: `fix(problem): match relative types on problems read from a response`.
  - **Description**:
    - the defect: a relative declaration shared by a service and its client never matched a problem read with `Read`, without any error;
    - the fix: `Match` resolves a relative declaration against the URL that `Read` used, and nothing else changes;
    - the behavior change, as in the changelog, and the release plan: `problem/v0.2.1` in a separate `chore(problem): prepare release 0.2.1` pull request;
    - the coverage of T029, and the zero allocations of `TestMatch_Allocs`;
    - the attribution line required for pull request descriptions.

---

## Dependencies & Execution Order

### Phase dependencies

- **Setup (Phase 1)** and **Foundational (Phase 2)** run first: T001 → T002 → T003.
- **US1 (Phase 3)** depends on Phase 2: its tests use the fixtures of T003.
- **US2 (Phase 4)** depends on US1. Its rows extend the table of T004, and its red step stashes
  the fix of T009–T011.
- **US3 (Phase 5)** depends on US1, since the example and the documentation describe the fixed
  behavior. T018 and T019 edit `type.go` and `read.go` after T010 and T011, and after T017 has
  popped its stash.
- **Polish (Phase 6)** depends on every story.

The recommended order is the priority order: US1 → US2 → US3. Each phase leaves
`make check MODULE=problem` green.

### Within each phase

- Test tasks come first, and are observed failing: T008 for US1, and the stash of T017 for US2.
  The documentation tasks of US3 have no red step: the output of the example guards T023.
- **Shared files**: tasks on the same file never run in parallel.

  | File | Edited by |
  |------|-----------|
  | `problem/helpers_test.go` | T003 |
  | `problem/type_test.go` | T004, T005, T006, T013, T014 |
  | `problem/fuzz_test.go` | T007 |
  | `problem/with_test.go` | T015 |
  | `problem/read_test.go` | T016 |
  | `problem/example_test.go` | T023 |
  | `problem/problem.go` | T009 |
  | `problem/read.go` | T010, T019 |
  | `problem/type.go` | T011, T018 |
  | `problem/doc.go` | T020 |
  | `problem/README.md` | T021 |
  | `problem/CHANGELOG.md` | T022 |

### Parallel opportunities

- **US1**: T004 → T005 → T006 (same file), with T007 in parallel with all of them. Then T008,
  then T009, then T010 and T011 together, then T012.
- **US2**: T013 → T014 (same file), with T015 and T016 in parallel with them. Then T017.
- **US3**: T018 to T023 together, six files. Then T024.
- **Polish**: T025, T026 and T027 together. Then T028, T029 and T030.

---

## Parallel Example: User Story 1

```bash
# Write the failing tests (T004–T006 share type_test.go, so they run in sequence):
Task: "T004–T006 TestMatch_ReadResponse, TestMatch_ReadFromServer, TestMatch_ReadTree in problem/type_test.go"
Task: "T007 FuzzRead in problem/fuzz_test.go"

# Observe the failures (T008), then implement:
Task: "T009 Error.base in problem/problem.go"
# then, together:
Task: "T010 Read keeps a copy of the request URL in problem/read.go"
Task: "T011 hasType and match in problem/type.go"
```

## Parallel Example: User Story 3

```bash
Task: "T018 Type.Match doc comment in problem/type.go"
Task: "T019 Read doc comment in problem/read.go"
Task: "T020 Reading problem details paragraph in problem/doc.go"
Task: "T021 Reading problem details paragraph in problem/README.md"
Task: "T022 Fixed entry in problem/CHANGELOG.md"
Task: "T023 ExampleType_Match_shared in problem/example_test.go"
# then T024 (documentation review)
```

---

## Requirement Coverage

| Requirement | Tasks |
|-------------|-------|
| FR-001 | T004, T005, T011 |
| FR-002 | T013 (absolute rows), T014, T011 |
| FR-003 | T013, T014, T011 |
| FR-004 | T005 (case and percent-encoded declarations match nothing else), T011 |
| FR-005 | T006 |
| FR-006 | T005 (derived), T015, T009 |
| FR-007 | T004 (rows 1–4), T013 (rows 5–10) |
| FR-008 | T017 (existing `TestNewType_Rules`) |
| FR-009 | T016, T010, T017 (existing `TestRead_Resolve`) |
| FR-010 | T025, T027 |
| FR-011 | T018, T019, T020, T021, T024 |
| FR-012 | T023 |
| FR-013 | T022 |
| FR-014 | T004, T005, T008, T017 |
| FR-015 | T007, T012, T028 |
| SC-001 | T004, T005, T006, T013, T015, T016 |
| SC-002 | T005 |
| SC-003 | T014 |
| SC-004 | T007 |
| SC-005 | T025, T027, T029, and `gorelease` in CI |

Spec edge cases:

| Edge case | Tasks |
|-----------|-------|
| Redirects | T005 (the `/redirect` request URL, and the `redirect` subtest) |
| Problems from several services in one error | T006.2 |
| Network-path reference | T004 (`network path` row), T005 (`network path` declaration), T007 (seed) |
| Query, fragment or percent-encoding in a declared type | T005 (the `query`, `fragment` and `percent-encoded` declarations), T007 |
| Document without a type | T004 (`document without a type` row), T007 (`{}` seed) |
| Absent problem, absent error or undeclared type | T006.3, and the existing `TestMatch_Tree` |

---

## Implementation Strategy

### MVP first (User Story 1)

1. Phase 1 and Phase 2: T001–T003. The branch, the baseline and the fixtures.
2. Phase 3, US1: T004–T012. `Match` resolves a relative declaration for problems read from a
   response.
3. **Stop and validate**: run `make check MODULE=problem`.

### Incremental delivery

1. US2 (P1): T013–T017. The guards of unchanged behavior, and the regression seen with the fix
   stashed.
2. US3 (P2): T018–T024. The doc comments, the package documentation, the README, the changelog
   and the example.
3. Polish: contracts, platforms, API compatibility, quickstart, gates, and the PR draft.

The fix ships as one pull request. Without US3, the doc comment of `Type.Match` would still state
the opposite rule, and the constitution requires the changelog entry in the same change. The
release `problem/v0.2.1` (research R-01) is prepared in a separate PR after the squash merge, as
for `auth`. The changelog entry stays under `[Unreleased]` until then.

---

## Notes

- Every task stays within `problem/` and this file.
- Never weaken a test to make it pass. Fix the implementation, or update the spec and the
  contracts first if the behavior itself must change.
- The reproduction probe and the spike live only in the session scratchpad, outside the
  repository. Re-implement from these tasks; do not copy files from them.

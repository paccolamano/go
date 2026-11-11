---

description: "Task list for the Read base URI fix in the problem module"
---

# Tasks: Read Resolves Against the URL the Request Sent (problem)

**Input**: Design documents from `specs/015-fix-read-base-uri/`

**Prerequisites**: [plan.md](plan.md), [spec.md](spec.md), [research.md](research.md),
[data-model.md](data-model.md), [contracts/](contracts/), [quickstart.md](quickstart.md)

**Tests**: REQUIRED. Constitution Principle III is non-negotiable, and FR-014 and FR-015 ask for
them:

- Tests are written first and observed failing (T007).
- The fix has regression tests that fail without it:
  - the in-memory table (T004);
  - the loopback server (T005);
  - the fuzz seeds (T006);
  - the tests of US2, observed with the fix stashed (T014).
- `FuzzRead` checks the properties of FR-015 (T006).
- Coverage stays at 99.6% with `-race -shuffle=on`, as at `problem/v0.2.1`.

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
  - `problem/helpers_test.go`:
    - `newRequest`, `sameProblem`, `typeOf` (in `type_test.go`) and `creditDoc`;
    - the declarations `sharedCredit` (`/probs/out-of-credit`), `apiCredit`
      (`https://api.example.com/probs/out-of-credit`) and `serviceUnavailable`;
    - the key `balance`, and the constant `apiTarget` (`https://api.example.com/v1/messages`);
    - `readProblem(tb, target, doc)`. It builds the request with `httptest.NewRequestWithContext`,
      which moves a fragment into the query ([research R-06](research.md#r-06-testing-approach-fr-014-fr-015-sc-001-to-sc-005-principle-iii)).
      Use it only for request URLs without userinfo and without a fragment.
  - `problem/read_test.go`: `response(tb, code, contentType, body)`, whose request is a GET of
    `apiTarget`.
  - `problem/with_test.go`: the key `requestID`.
  - the helpers that T003 adds: `withCredentials` and `readFrom`.
- **Test names**: those of [research R-06](research.md#r-06-testing-approach-fr-014-fr-015-sc-001-to-sc-005-principle-iii),
  chosen so that the `-run` patterns of [quickstart.md](quickstart.md) select them:
  `TestRead_RequestURL`, `TestRead_Credentials`, `TestMatch_ReadResponse`, `TestMatch_ReadTree`,
  `TestWith_ReadProblem` and `FuzzRead`.
- **Test style**:
  - table-driven, with `t.Run`;
  - loopback servers from `httptest.NewServer`, closed with `defer srv.Close()`. No real network
    and no `time.Sleep`;
  - requests from `newRequest`, `http.NewRequestWithContext(t.Context(), …)` or
    `httptest.NewRequestWithContext` (`noctx`, `usetesting`). Response bodies are closed with
    `_ = resp.Body.Close()`;
  - a server handler reports failures with `t.Errorf`, never `t.Fatal`;
  - helpers take `testing.TB` (or `*testing.T`) first and call `tb.Helper()` first (`thelper`);
  - a helper that may call `Fatalf` is called with the `*testing.T` of the subtest that runs it,
    never with the parent's.
- **Placeholder credentials** ([research R-08](research.md#r-08-placeholder-credentials-and-the-pull-request-scanners)):
  - The user name is `svc`, the password `s3cret`, and the fragments are `frag` and `moved`.
  - Never write a URL literal with a user name and a password, such as `http://svc:s3cret@…`, in
    a Go source. Build the userinfo with `url.UserPassword` or `url.User`, through
    `withCredentials` or a `url.URL` value.
  - Name no identifier `password`, `secret` or `token`.
- **Lint**: golangci-lint must report zero findings, and the change adds **no** `//nolint`
  directive. Imports group `github.com/paccolamano/go` last (`goimports`).
- **Documentation**: when [contracts/public-api.md](contracts/public-api.md) or a task gives the
  text of a doc comment or of a paragraph, use it verbatim.
- **Commits**: do not commit, push or open a pull request unless the maintainer asks for it.

---

## Phase 1: Setup (Shared Infrastructure)

**Purpose**: work on a feature branch from a known state.

- [X] T001 From the repository root:
  1. Confirm that `git status --short` lists only `go.work.sum` and `specs/015-fix-read-base-uri/` as untracked, with no change under `problem/`.
  2. Confirm that `git tag --list 'problem/*'` ends with `problem/v0.2.1`, and that `git diff --stat problem/v0.2.1 HEAD -- problem/` prints nothing.
  3. If the current branch is `main`, create the feature branch with `git switch -c 015-fix-read-base-uri` before editing any file. Creating the branch is not a commit.

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: record the baseline, and add the test helpers that the tests of US1 and US2 share.

**⚠️ CRITICAL**: no user story work begins until this phase is complete.

- [X] T002 Record the baseline of `problem/`:
  - from the repository root, run `make check MODULE=problem`, which must report `PASS` for lint and test;
  - from `problem/`, run `GOWORK=off go test -cover .`, and record the percentage: 99.6% at `problem/v0.2.1`.
- [X] T003 In `problem/helpers_test.go`, after `readProblem`, add two helpers ([research R-06](research.md#r-06-testing-approach-fr-014-fr-015-sc-001-to-sc-005-principle-iii), [R-08](research.md#r-08-placeholder-credentials-and-the-pull-request-scanners)):
  1. `withCredentials(tb testing.TB, target string) *url.URL`, with this doc comment:

     ```go
     // withCredentials returns target with the user name "svc", the password
     // "s3cret" and the fragment "frag" added, as an HTTP client may hold them. The
     // userinfo is built with url.UserPassword, so that no URL literal with
     // credentials appears in the sources.
     ```

     It calls `tb.Helper()` first, then:
     - `u, err := url.Parse(target)`, calling `tb.Fatalf("url.Parse(%q): %v", target, err)` on an error;
     - `u.User = url.UserPassword("svc", "s3cret")` and `u.Fragment = "frag"`;
     - returns `u`.

     For example, `withCredentials(t, "http://api.example.com/v1/accounts/42?api_key=XYZ")` is the request URL of FR-006.
  2. `readFrom(tb testing.TB, u *url.URL, doc string) *problem.Error`, with this doc comment:

     ```go
     // readFrom reads doc with Read from a response to a GET of u, built as a client
     // builds it: http.NewRequestWithContext keeps the userinfo and the fragment of
     // u in the request URL, where httptest.NewRequest would move the fragment into
     // the query. It fails tb when Read changes the request URL.
     ```

     It calls `tb.Helper()` first, then:
     - `req, err := http.NewRequestWithContext(tb.Context(), http.MethodGet, u.String(), nil)`, calling `tb.Fatalf` on an error;
     - `before := req.URL.String()`;
     - `resp := response(tb, http.StatusForbidden, problem.MediaType, strings.NewReader(doc))`, then `resp.Request = req`;
     - `p, err := problem.Read(resp)`, calling `tb.Fatalf("Read: %v", err)` on an error;
     - when `req.URL.String() != before`, `tb.Errorf("Read changed the request URL from %q to %q", before, req.URL.String())` (FR-005; US2 scenario 6);
     - returns `p`.

  `net/url`, `net/http` and `strings` are already imported. Run `GOWORK=off go vet . && GOWORK=off go test .`: both pass, because nothing uses the helpers yet. The `unused` linter reports them until T004 uses them, so do not run `make check` at this point.

**Checkpoint**: the helpers exist, and every user story can begin.

---

## Phase 3: User Story 1 - Credentials in the request URL never reach a problem read from the response (Priority: P1) 🎯 MVP

**Goal**: `Read` resolves relative members against the request URL without its userinfo and its
fragment, and keeps that URL for the type test (FR-001 to FR-006, FR-014, FR-015).

**Independent Test**: send a request with credentials, a query and a fragment in its URL to a test
server that answers with a problem. Read the response, and search the type URI, the instance URI,
the message and a rewritten document for the user name, the password and the fragment.

### Tests for User Story 1 ⚠️ (write first, see them fail)

- [X] T004 [US1] In `problem/read_test.go`, add `TestRead_RequestURL` after `TestRead_Resolve` (FR-001, FR-002, FR-005, FR-006, FR-009; US1 scenarios 1, 2, 4 and 6; US2 scenario 3; spec edge cases "Credentials without a password", "Fragment-only member", "Absolute members and network-path references" and "Request URL that is not hierarchical"). Add the imports `fmt` and `net/url`.

  Doc comment: `// TestRead_RequestURL resolves the members of the table of FR-006 (spec 015) against a request URL with credentials, a query and a fragment, and the edge cases of other request URLs.`, wrapped at 80 columns.

  1. **Table**: a table over `name string`, `u *url.URL`, `member string` (`"type"` or `"instance"`), `value string` and `want string`. Let `cred := withCredentials(t, "http://api.example.com/v1/accounts/42?api_key=XYZ")`. The rows of FR-006, each with `u: cred`:
     - `"type, absolute path"`: `type`, `/probs/out-of-credit`, want `http://api.example.com/probs/out-of-credit`;
     - `"instance, fragment"`: `instance`, `#msg-1`, want `http://api.example.com/v1/accounts/42?api_key=XYZ#msg-1`;
     - `"instance, empty fragment"`: `instance`, `#`, want `http://api.example.com/v1/accounts/42?api_key=XYZ`;
     - `"instance, query"`: `instance`, `?page=2`, want `http://api.example.com/v1/accounts/42?page=2`;
     - `"instance, relative path"`: `instance`, `msgs/1`, want `http://api.example.com/v1/accounts/msgs/1`;
     - `"type, network path"`: `type`, `//cdn.example.com/probs/x`, want `http://cdn.example.com/probs/x`;
     - `"type, absolute"`: `type`, `https://other.example.com/probs/x`, want `https://other.example.com/probs/x`;
     - `"no type"`: `type`, `""`, want `about:blank`.

     The edge-case rows, each of `type` `/probs/out-of-credit` unless stated:
     - `"user name only"`: `u: &url.URL{Scheme: "http", User: url.User("svc"), Host: "api.example.com", Path: "/v1"}`, want `http://api.example.com/probs/out-of-credit`;
     - `"empty userinfo"`: `u: &url.URL{Scheme: "http", User: url.User(""), Host: "api.example.com", Path: "/v1"}`, which is `http://@api.example.com/v1`, want `http://api.example.com/probs/out-of-credit` ([research R-03](research.md#r-03-what-changes-and-what-does-not-fr-006-to-fr-010));
     - `"not hierarchical"`: `u: &url.URL{Scheme: "mailto", Opaque: "a@b"}`, `type` `/probs/x`, want `mailto:///probs/x`, the result of 0.2.1.
  2. **Body of each subtest**:
     - the document is `{}` when `value` is `""`, and `fmt.Sprintf(`{%q:%q}`, tt.member, tt.value)` otherwise;
     - `p := readFrom(t, tt.u, doc)`. `readFrom` already checks that the request URL is unchanged (FR-005);
     - `got` is `p.Type()` for `type`, and `p.Instance()` for `instance`. Assert `got == tt.want`, and report the member, the value, `got` and `tt.want`.
  3. **The caller's URL** (FR-005; US2 scenario 6): after the table, a subtest `"caller's URL"` reads `{"type":"/probs/out-of-credit"}` with `readFrom(t, cred, …)`, then asserts that `cred.User` is not nil, and that `cred.Fragment` is still `frag`.
- [X] T005 [US1] In `problem/read_test.go`, add `TestRead_Credentials` after `TestRead_RequestURL` (FR-001, FR-003, FR-004, FR-014; SC-002; US1 scenarios 1 to 7; spec edge cases "Credentials without a password" and "Redirects"; [research R-04](research.md#r-04-redirects-fr-004)).

  Doc comment: `// TestRead_Credentials reads problems through http.Client, from request URLs that hold credentials and a fragment, and checks that neither reaches the problem, its message or the document written again from it (FR-014 of spec 015).`, wrapped at 80 columns.

  1. **Server**: `srv := httptest.NewServer(…)`, closed with `defer srv.Close()`. Its handler answers:
     - the path `/old` with `http.Redirect(w, r, "/v2/accounts/42?page=2#moved", http.StatusFound)`;
     - every other path with `w.Header().Set("Content-Type", problem.MediaType)`, `w.WriteHeader(http.StatusForbidden)` and `_, _ = io.WriteString(w, r.Header.Get("Test-Doc"))`.

     `http.Client` forwards the `Test-Doc` header across the redirect, to the same host.
  2. **Request URLs**: a table over `name`, `u *url.URL`, `sent string` (the URL the request sent) and `fragment string` (the fragment that the client's last request URL still holds):
     - `"user and password, query and fragment"`: `withCredentials(t, srv.URL+"/v1/accounts/42?api_key=XYZ")`, sent `srv.URL+"/v1/accounts/42?api_key=XYZ"`, fragment `frag`;
     - `"user name only"`: `url.Parse(srv.URL+"/v1/accounts/42")`, calling `t.Fatalf` on an error, then `u.User = url.User("svc")`. Sent is `srv.URL+"/v1/accounts/42"`, and the fragment is `""`;
     - `"relative redirect"`: `withCredentials(t, srv.URL+"/old")`, sent `srv.URL+"/v2/accounts/42?page=2"`, fragment `moved`.
  3. **Documents**, each with a status that `Write` can send:
     - `{"type":"/probs/out-of-credit","status":403,"instance":"#msg-1"}`;
     - `{"type":"/probs/out-of-credit","status":403,"instance":"#"}`;
     - `{"type":"/probs/out-of-credit","status":403,"instance":"?page=2"}`;
     - `{"type":"/probs/out-of-credit","status":403,"instance":"msgs/1"}`;
     - `{"type":"//cdn.example.com/probs/x","status":403}`;
     - `{"type":"https://other.example.com/probs/x","status":403}`;
     - `{"status":403}`.
  4. **One subtest per request URL**, and for each document:
     - build the request with `http.NewRequestWithContext(t.Context(), http.MethodGet, tt.u.String(), nil)`, set `Test-Doc` to the document, and send it with `srv.Client().Do(req)`;
     - close the body with `defer func() { _ = resp.Body.Close() }()`, in a function per document;
     - assert status 403, and read `p, err := problem.Read(resp)`, calling `t.Fatalf` on an error.
  5. **Assertions for each problem**:
     - **No leak** (SC-002; US1 scenarios 3 and 7): write `p` with `problem.Write(rec, newRequest(t, http.MethodGet, "/"), p)` to `rec := httptest.NewRecorder()`, and assert a nil error. Then none of `p.Type()`, `p.Instance()`, `p.Error()` and `rec.Body.String()` contains any of `svc`, `s3cret`, `frag` and `moved`. Report which value leaked, and the value.
     - **Resolved values** (US1 scenarios 1, 2, 5 and 6):
       - for the documents of type `/probs/out-of-credit`, `p.Type() == srv.URL+"/probs/out-of-credit"`;
       - for the instance `#msg-1`, `p.Instance() == tt.sent+"#msg-1"`;
       - for the instance `#`, `p.Instance() == tt.sent`.
     - **Type test** (US2 scenarios 2 and 4): for the documents of type `/probs/out-of-credit`, `sharedCredit.Match(p)` returns `p, true`, and `sharedCredit.Match(p.With(problem.WithDetail("derived")))` returns `true`.
     - **The caller's request** (FR-005): `resp.Request.URL.User` is not nil, and `resp.Request.URL.Fragment == tt.fragment`.
- [X] T006 [P] [US1] In `problem/fuzz_test.go`, extend `FuzzRead` (FR-005, FR-007, FR-015; SC-003, SC-004; [research R-06](research.md#r-06-testing-approach-fr-014-fr-015-sc-001-to-sc-005-principle-iii)). Keep its seeds and its type-test property unchanged.
  1. **Seeds**: after the existing `f.Add` calls, for each `inst` in `#msg-1`, `#`, `?page=2`, `msgs/1`, `/a/b` and `//`, add the document `{"type":"/probs/out-of-credit","instance":"<inst>"}`, built with `fmt.Sprintf` and `%q`, with three targets:
     - `withCredentials(f, "http://api.example.com/v1/accounts/42?api_key=XYZ").String()`;
     - `(&url.URL{Scheme: "http", User: url.User("svc"), Host: "api.example.com", Path: "/v1"}).String()`;
     - `"http://api.example.com/v1/accounts/42?api_key=XYZ"`.

     `*testing.F` implements `testing.TB`. Add the import `fmt`.
  2. **Body**:
     - declare `var before string` just before the existing `if target != ""` block, and, inside it, set `before = u.String()` after `req.URL = u`;
     - after `p, err := problem.Read(resp)` and its early return, when `resp.Request != nil`, call `checkResolved(t, resp.Request.URL, before, doc, p)`;
     - then the existing type-test property runs as before.
  3. **The helper** `checkResolved(t *testing.T, u *url.URL, before, doc string, p *problem.Error)`, after `FuzzRead`, with this doc comment:

     ```go
     // checkResolved checks the properties of FR-015 (spec 015) on p, read from doc
     // with the request URL u: Read leaves u unchanged; for a request URL without
     // userinfo and without a fragment, the members resolve as in problem/v0.2.1;
     // and a member without an authority of its own takes neither the userinfo nor
     // the fragment of u.
     ```

     It calls `t.Helper()`, then:
     - if `u.String() != before`, `t.Fatalf("Read changed the request URL from %q to %q", before, u)`;
     - `raw, err := problem.Parse([]byte(doc))`, calling `t.Fatalf` on an error, because `Read` accepted the same document;
     - for each pair (`"type"`, `raw.Type()`, `p.Type()`) and (`"instance"`, `raw.Instance()`, `p.Instance()`), skipping an empty `written`:
       - `ref, err := url.Parse(written)`, calling `t.Fatalf` on an error;
       - **0.2.1 on clean URLs** (FR-007; SC-003): when `u.User == nil && u.Fragment == "" && u.RawFragment == ""`, `want` is `written` if `ref.IsAbs()`, and `u.ResolveReference(ref).String()` otherwise. `got` must equal `want`;
       - **nothing from the request URL** (FR-015): skip the rest when `ref.IsAbs() || ref.Host != "" || ref.User != nil`. Otherwise:
         - `res, err := url.Parse(got)`, calling `t.Fatalf` on an error;
         - `res.User` must be nil;
         - `res.Fragment` must equal `ref.Fragment`.

     Each failure reports the member, `written`, the request URL `before` and `got`.
  4. **Doc comment of `FuzzRead`**: append, wrapped at 80 columns: `It also checks the properties of FR-015 of spec 015 with checkResolved.`
- [X] T007 [US1] From `problem/`, run `GOWORK=off go test -race -run 'TestRead_(RequestURL|Credentials)|FuzzRead' -v .` and observe the expected failures:
  - `TestRead_RequestURL`:
    - fail, each with `svc:s3cret@` or `svc@` or `@` in `got`: the five relative rows of FR-006, `user name only` and `empty userinfo`. `instance, empty fragment` gives `…?api_key=XYZ#frag`;
    - pass: `type, network path`, `type, absolute`, `no type`, `not hierarchical` and `caller's URL`.
  - `TestRead_Credentials`: every request URL fails on the leak checks, with the credentials in `Type()`, `Instance()`, `Error()` and the written document. In `relative redirect`, the instance `#` gives the fragment `moved`. The checks of the caller's request pass.
  - `FuzzRead`: the 12 new seeds with a userinfo fail in `checkResolved`. Every other seed passes.

  Do not continue until these failures are seen.

### Implementation for User Story 1

- [X] T008 [US1] In `problem/read.go`, in `Read`, replace the body of `if resp.Request != nil && resp.Request.URL != nil { … }` with the code of [research R-02](research.md#r-02-where-the-url-is-cleaned-fr-001-to-fr-005), verbatim (FR-001 to FR-005). The comment `// A copy: the caller may change its request after Read returns.` becomes three lines:

  ```go
  // The URL the request sent (RFC 9110 §4.2.4 and §7.1): a copy of the
  // request URL without its userinfo and its fragment. A copy, because the
  // caller may change its request after Read returns.
  base := *resp.Request.URL
  base.User = nil
  base.Fragment, base.RawFragment = "", ""
  e.base = &base
  e.typ = resolve(e.base, e.typ)
  e.instance = resolve(e.base, e.instance)
  ```

  No import changes. Leave the doc comment of `Read` for T015.
- [X] T009 [P] [US1] In `problem/problem.go`, replace the comment of the field `base` in `Error` with these lines, keeping the tab indentation (FR-003; [data-model §3](data-model.md#3-problem-error)):

  ```go
  	// base is the URL that Read resolved typ and instance against, or nil: the
  	// URL that the request sent, without its userinfo and its fragment. It is a
  	// copy that nothing modifies, and Write never writes it.
  ```

  Nothing else changes.
- [X] T010 [US1] Run these checks on `problem/`:
  - `GOWORK=off go test -race -shuffle=on ./...`: every test of T004–T006 passes, and so does every existing test, `ExampleRead` and `TestMatch_Allocs` included;
  - `GOWORK=off go test -run '^$' -fuzz '^FuzzRead$' -fuzztime 30s -fuzzminimizetime 1s .`, which must find no failure ([research R-07](research.md#r-07-fuzzing-throughput-sc-004)). Discard any `testdata/fuzz/FuzzRead` directory it creates;
  - from the repository root, `make check MODULE=problem`, which must report `PASS`.

**Checkpoint**: credentials and the request's fragment no longer reach a problem read from a
response. This is the MVP.

---

## Phase 4: User Story 2 - Everything else resolves and matches as before (Priority: P1)

**Goal**: prove that request URLs without credentials and without a fragment give the results of
`problem/v0.2.1`. With them, the type test gives the results it gives without them (FR-007, FR-008,
FR-010; SC-003).

**Independent Test**: read the same documents from the same request URLs with and without
credentials and a fragment. Compare the type URIs, the instance URIs and the type test results
with `problem/v0.2.1`.

### Tests for User Story 2 ⚠️ (T014 observes the regression with the fix stashed)

- [X] T011 [US2] In `problem/type_test.go`, make `TestMatch_ReadResponse` run its table twice: once from `apiTarget`, and once from `apiTarget` with credentials and a fragment (FR-008; US2 scenarios 1 and 2; [data-model §5](data-model.md#5-type-test-typematch)).
  1. **The two request URLs**: wrap the table and its loop in a loop over a slice of `name string` and `read func(tb testing.TB, doc string) *problem.Error`, one `t.Run(base.name, …)` each:
     - `"request URL"`: `func(tb testing.TB, doc string) *problem.Error { return readProblem(tb, apiTarget, doc) }`;
     - `"request URL with credentials and a fragment"`: `func(tb testing.TB, doc string) *problem.Error { return readFrom(tb, withCredentials(tb, apiTarget), doc) }`.
  2. **The table, built inside that subtest**: every row that now calls `readProblem(t, apiTarget, …)` calls `base.read(t, …)` instead, with the subtest's `t`:
     - `relative, same relative type`, `relative, same absolute type`, `relative, other host`, `relative, other path`;
     - `absolute, read relative type`, `network path` and `document without a type`.

     The other rows, `read without a request`, `parsed …` and `built locally`, stay as they are. Change the local helper `parse` to `parse := func(tb testing.TB, doc string) *problem.Error`, which calls `tb.Helper()` and `tb.Fatalf`. Call it with the subtest's `t`.
  3. **Unchanged**: every `wantType` and every `want`, because the results with credentials equal those without (FR-008). Also unchanged are the assertions of each row and `cdnCredit`. Keep the comment above the table, and extend the doc comment of the test, wrapped at 80 columns: `The table is read from apiTarget, and from apiTarget with credentials and a fragment added, which give the same results (FR-008 of spec 015).`
- [X] T012 [US2] In `problem/type_test.go`, in `TestMatch_ReadTree`, subtest `"two services"`, add after `pOther` (spec edge case "Problem from several services in one error"):
  - `pc := readFrom(t, withCredentials(t, "https://b.example.com/v1"), creditDoc("/probs/out-of-credit"))`;
  - two rows to the table:
    - `{"with credentials, first match wins", sharedCredit, errors.Join(pc, pa), pc}`;
    - `{"with credentials, each with its own URL", bCredit, errors.Join(pa, pc), pc}`.

  The second row matches only when the type of `pc` holds no credentials.
- [X] T013 [P] [US2] In `problem/with_test.go`, extend `TestWith_ReadProblem` (FR-010; US2 scenario 4):
  1. Add a field `wantType string` to the table of the derived-problem part:
     - `read from a response`: `https://api.example.com/probs/out-of-credit`;
     - `read without a request`: `/probs/out-of-credit`.
  2. Add a row `{"read with credentials", readFrom(t, withCredentials(t, apiTarget), doc), true, true, "https://api.example.com/probs/out-of-credit"}`, keeping the field order of the table.
  3. In each row, after `d := tt.p.With(…)`, assert `d.Type() == tt.wantType`. Keep the existing assertions.
- [X] T014 [US2] From `problem/`, run `GOWORK=off go test -race -run 'TestMatch_|TestWith_ReadProblem|TestRead_(Resolve|WriteAgain|RoundTrip)' -v .`. Every test passes:
  - `TestRead_Resolve` still checks a request URL without credentials, a response without a request, and `Parse` (FR-007, FR-009; US2 scenario 5);
  - `TestMatch_ReadFromServer` still matches from the target `/x;p?q#frag` (FR-007).

  Then observe the regression with the fix removed, from the repository root:
  1. Run `git stash push -- problem/read.go`. `read.go` holds only the change of T008 at this point.
  2. Run `(cd problem && GOWORK=off go test -run 'TestMatch_ReadResponse|TestMatch_ReadTree|TestWith_ReadProblem' -v .)`, and see these fail:
     - in `TestMatch_ReadResponse/request_URL_with_credentials_and_a_fragment`, the rows `relative, same relative type` and `relative, other path` fail on the type, and `relative, same absolute type` on the match. `absolute, read relative type` fails on both;
     - in `TestMatch_ReadTree`, `with credentials, each with its own URL`;
     - in `TestWith_ReadProblem`, the row `read with credentials`, on the absolute declaration and the type.

     Every subtest of `TestMatch_ReadResponse/request_URL`, and every other row, passes. The guards hold against `problem/v0.2.1`.
  3. Run `git stash pop`, then the same command, and see every test pass.

  Never leave the stash applied.

**Checkpoint**: only request URLs with credentials or a fragment changed, and the type test agrees
with and without them. User Stories 1 and 2 together are the complete fix.

---

## Phase 5: User Story 3 - Developers learn which URL relative members resolve against (Priority: P2)

**Goal**: the documentation states the URL the request sent, and the changelog announces the
security fix (FR-012, FR-013).

**Independent Test**: read the documentation and the changelog.

- [X] T015 [P] [US3] In `problem/read.go`, replace the third paragraph of the doc comment of `Read`, the one that begins "The members are read as [Parse] reads them.", with the text of [contracts/public-api.md § Read](contracts/public-api.md#read-fr-001-to-fr-006), verbatim (FR-012; US3 scenario 1). Leave the other paragraphs unchanged.
- [X] T016 [P] [US3] In `problem/type.go`, in the doc comment of `Type.Match`, drop "request" from "the request URL that Read resolved" ([contracts/public-api.md § Type.Match](contracts/public-api.md#typematch-fr-008-documentation-only)). Replace these two lines:

  ```go
  // resolved against the request URL that Read resolved the problem's members
  // against. A type that a service declares as "/probs/out-of-credit", and shares
  ```

  with these two:

  ```go
  // resolved against the URL that Read resolved the problem's members against.
  // A type that a service declares as "/probs/out-of-credit", and shares
  ```

  The rest of the comment is unchanged.
- [X] T017 [P] [US3] In `problem/doc.go`, section `# Reading problem details` ([contracts/public-api.md § Package documentation](contracts/public-api.md#package-documentation-docgo-fr-012); FR-012):
  - replace the line `// "type" and "instance" members against the URL of the request.` with the two lines `// "type" and "instance" members against the URL that the request sent: the` and `// request URL without its userinfo and its fragment.`;
  - in the next paragraph, replace `same request URL, so the` with `same URL, so the`.
- [X] T018 [P] [US3] In `problem/README.md`, section "Reading problem details" (FR-012; US3 scenario 1):
  1. Replace the paragraph that begins "`Read` follows the RFC's rules for consumers" with these four lines, verbatim:

     ```markdown
     `Read` follows the RFC's rules for consumers: members with the wrong JSON type are ignored, a
     missing type is `about:blank`, unknown extensions are kept, and relative URIs are resolved
     against the URL that the request sent: the request URL without its credentials and its fragment.
     It reads at most 1 MiB. `p.Errors()` returns the validation errors of a problem, when it has some.
     ```

  2. In the next paragraph, replace the line `request URL as `Read`, so it matches the problems of that path on the server that answered.` with `URL as `Read`, so it matches the problems of that path on the server that answered.`.
- [X] T019 [P] [US3] In `problem/example_test.go`, in `ExampleRead`, replace the comment `// resolved against the request URL` with `// resolved against the URL the request sent` ([contracts/public-api.md § ExampleRead](contracts/public-api.md#exampleread)). The example's output does not change.
- [X] T020 [P] [US3] In `problem/CHANGELOG.md`, under `## [Unreleased]`, add a `### Security` section with the entry drafted in [research R-01](research.md#r-01-release-level-and-changelog), verbatim (FR-013; US3 scenario 2). Leave `## [0.2.1]` and the sections below it unchanged.
- [X] T021 [US3] Review the documentation of `problem/` (FR-012, FR-013; [quickstart §7](quickstart.md#7-documentation-review-fr-012-fr-013)):
  - from `problem/`, run `grep -n "request URL\|URL of the request\|URL of resp.Request" read.go type.go doc.go example_test.go README.md`. Each remaining occurrence either says that the userinfo and the fragment are removed, or does not describe the base of resolution. `URL of the request` and `URL of resp.Request` no longer appear;
  - `GOWORK=off go doc . Read` and `GOWORK=off go doc . Type.Match` show the texts of the contract;
  - `GOWORK=off go doc .` shows the new sentence in "Reading problem details";
  - `GOWORK=off go test -run 'ExampleRead' -v .` passes;
  - `problem/CHANGELOG.md` has exactly one `### Security` section, under `## [Unreleased]`, and it contains **Behavior change**.

**Checkpoint**: every story is complete.

---

## Phase 6: Polish & Cross-Cutting Concerns

**Purpose**: verify the whole change against the contracts and the quickstart, and run the gates.

- [X] T022 [P] Verify that the exported surface of `problem/` did not change (FR-011).
  - From the repository root, `git diff --stat -- problem/` lists only:
    - `CHANGELOG.md`, `README.md`, `doc.go`, `problem.go`, `read.go` and `type.go`;
    - `example_test.go`, `fuzz_test.go`, `helpers_test.go`, `read_test.go`, `type_test.go` and `with_test.go`.
  - In `problem.go`, `read.go` and `type.go`, the diff changes comments and the three lines of T008 only.
  - From `problem/`, run `GOWORK=off go test -coverprofile=c.out . && go tool cover -func=c.out`. `Read` stays at 100%, and the total at 99.6%. Delete `c.out` afterwards.
- [X] T023 [P] From `problem/`, check the dependencies and the platforms:
  - `GOWORK=off go mod tidy -diff` prints nothing;
  - `git diff --exit-code -- go.mod` passes; the module has no `go.sum`;
  - for every `os` in linux, darwin and windows, and every `arch` in amd64 and arm64, `GOWORK=off GOOS=$os GOARCH=$arch go vet ./...` passes.
- [X] T024 [P] Check API compatibility (FR-011; SC-005). If `gorelease` is installed at the version of `versions.env`, run `GORELEASE=$(command -v gorelease) scripts/apicompat.sh problem` from the repository root. The report must show no change and suggest `v0.2.2`. `apicompat.sh` reads the base from the tags, and `gorelease` refuses a repository with uncommitted changes. If the working tree is not committed, or `gorelease` is not installed, skip this check, and note that the pull request workflow runs it. Do not install tools or commit to run it. (2026-11-11: `gorelease` is not installed locally, so the check was skipped. An offline comparison of `go doc -short` for the package and each exported type, against `problem/v0.2.1`, found the same exported API. The pull request workflow runs `gorelease`.)
- [X] T025 Run the checks of [quickstart.md](quickstart.md) on `problem/`:
  - §2;
  - §3, except `changelog-check.sh` and `gorelease`, which need commits;
  - §4, one row at a time;
  - §5, with the 60-second run and `-fuzzminimizetime 1s`;
  - §6: run the `grep`, and run `gitleaks` if it is installed. `gitleaks git` scans commits, so before any commit, run `gitleaks dir --redact --verbose --exit-code 1 problem` and `gitleaks dir --redact --verbose --exit-code 1 specs/015-fix-read-base-uri` instead, from the repository root.

  Every command must pass. Discard any `testdata/fuzz/FuzzRead` directory the fuzz run creates, unless it holds a failure, which is then a defect to fix first.
- [X] T026 Run `make check MODULE=problem` from the repository root:
  - golangci-lint reports zero findings, and `git diff -- problem | grep -c 'nolint'` prints 0;
  - `go test -race -shuffle=on` passes, with coverage at 99.6%. Compare it with the baseline of T002.
- [X] T027 Draft the pull request for the changes in `problem/` and `specs/015-fix-read-base-uri/`, for the maintainer, and do not open it without their go-ahead:
  - **Title**: `fix(problem): resolve read problems against the URL the request sent`.
  - **Description**:
    - **The defect**: `Read` copied the userinfo of the request URL into the type and instance URIs of the problem it read, and so into its message, the logs and any document written again from it. A member that is just `#` took the request's fragment. With credentials in the request URL, `Type.Match` also missed matches that it reports without them;
    - **The fix**: `Read` resolves against, and keeps, the request URL without its userinfo and its fragment: the URL the request sent (RFC 9110 §4.2.4 and §7.1). The query is kept (RFC 3986 §5.2.2);
    - **The behavior change**, as in the changelog. The release plan is `problem/v0.2.2`, in a separate `chore(problem): prepare release 0.2.2` pull request;
    - **Evidence**: the coverage of T026, and the fuzz run of T025;
    - the attribution line required for pull request descriptions.

---

## Dependencies & Execution Order

### Phase dependencies

- **Setup (Phase 1)** and **Foundational (Phase 2)** run first: T001 → T002 → T003.
- **US1 (Phase 3)** depends on Phase 2: its tests use the helpers of T003.
- **US2 (Phase 4)** depends on US1. Its tests use the helpers of T003, and its red step stashes the
  fix of T008.
- **US3 (Phase 5)** depends on US1, since the documentation describes the fixed behavior. T015
  edits `read.go` after T008, and after T014 has popped its stash.
- **Polish (Phase 6)** depends on every story.

The recommended order is the priority order: US1 → US2 → US3. Each phase leaves
`make check MODULE=problem` green.

### Within each phase

- Test tasks come first, and are observed failing: T007 for US1, and the stash of T014 for US2.
  The documentation tasks of US3 have no red step, and T021 reviews them.
- **Shared files**: tasks on the same file never run in parallel.

  | File | Edited by |
  |------|-----------|
  | `problem/helpers_test.go` | T003 |
  | `problem/read_test.go` | T004, T005 |
  | `problem/fuzz_test.go` | T006 |
  | `problem/read.go` | T008, T015 |
  | `problem/problem.go` | T009 |
  | `problem/type_test.go` | T011, T012 |
  | `problem/with_test.go` | T013 |
  | `problem/type.go` | T016 |
  | `problem/doc.go` | T017 |
  | `problem/README.md` | T018 |
  | `problem/example_test.go` | T019 |
  | `problem/CHANGELOG.md` | T020 |

### Parallel opportunities

- **US1**: T004 → T005, which share `read_test.go`, with T006 in parallel with both. Then T007,
  then T008 and T009 together, then T010.
- **US2**: T011 → T012, which share `type_test.go`, with T013 in parallel with both. Then T014.
- **US3**: T015 to T020 together, six files. Then T021.
- **Polish**: T022, T023 and T024 together. Then T025, T026 and T027.

---

## Parallel Example: User Story 1

```bash
# Write the failing tests (T004 and T005 share read_test.go, so they run in sequence):
Task: "T004–T005 TestRead_RequestURL, TestRead_Credentials in problem/read_test.go"
Task: "T006 FuzzRead seeds and checkResolved in problem/fuzz_test.go"

# Observe the failures (T007), then, together:
Task: "T008 Read clears User, Fragment and RawFragment of its copy in problem/read.go"
Task: "T009 Comment of Error.base in problem/problem.go"
```

## Parallel Example: User Story 3

```bash
Task: "T015 Read doc comment in problem/read.go"
Task: "T016 Type.Match doc comment in problem/type.go"
Task: "T017 Reading problem details in problem/doc.go"
Task: "T018 Reading problem details in problem/README.md"
Task: "T019 ExampleRead comment in problem/example_test.go"
Task: "T020 Security entry in problem/CHANGELOG.md"
# then T021 (documentation review)
```

---

## Requirement Coverage

| Requirement | Tasks |
|-------------|-------|
| FR-001 | T004, T005, T008 |
| FR-002 | T004 (rows `#msg-1`, `?page=2`, `msgs/1`), T005 |
| FR-003 | T005 (written document), T008, T009, T011 (the type test uses the cleaned URL) |
| FR-004 | T005 (`relative redirect`) |
| FR-005 | T003 (`readFrom`), T004 (`caller's URL`), T005, T006 |
| FR-006 | T004 |
| FR-007 | T006 (`checkResolved`, 0.2.1 on clean URLs), T011 (`request URL`), T014 (existing `TestRead_Resolve`, `TestMatch_ReadFromServer`) |
| FR-008 | T011 |
| FR-009 | T004 (rows `type, network path`, `type, absolute`, `no type`), T014 (existing `TestRead_Resolve`) |
| FR-010 | T005 (derived problem), T013 |
| FR-011 | T022, T024 |
| FR-012 | T015, T016, T017, T018, T019, T021 |
| FR-013 | T020, T021 |
| FR-014 | T004, T005, T007, T014 |
| FR-015 | T006, T010, T025 |
| SC-001 | T004, T005, T011, T012, T013 |
| SC-002 | T005 |
| SC-003 | T006, T011, T014 |
| SC-004 | T006, T010, T025 |
| SC-005 | T022, T024, T026, and `gorelease` in CI |

Spec edge cases:

| Edge case | Tasks |
|-----------|-------|
| Credentials without a password | T004 (`user name only`), T005 (`user name only`), T006 (seed) |
| Fragment-only member | T004 (`instance, fragment`, `instance, empty fragment`), T005, T006 (seeds `#msg-1`, `#`) |
| Redirects | T005 (`relative redirect`) |
| Absolute members and network-path references | T004 (`type, network path`, `type, absolute`), T005 |
| Response without a request | existing `TestRead_Resolve` (`no request`), T014 |
| Request URL that is not hierarchical | T004 (`not hierarchical`), T006 (existing seed `mailto:a@b`) |
| Problem from several services in one error | T012 |

---

## Implementation Strategy

### MVP first (User Story 1)

1. Phase 1 and Phase 2: T001–T003. The branch, the baseline and the helpers.
2. Phase 3, US1: T004–T010. `Read` resolves against the URL the request sent.
3. **Stop and validate**: run `make check MODULE=problem`.

### Incremental delivery

1. US2 (P1): T011–T014. The type test with and without credentials, derived problems and trees,
   and the regression seen with the fix stashed.
2. US3 (P2): T015–T021. The doc comments, the package documentation, the README, the example's
   comment and the changelog.
3. Polish: API surface, platforms, API compatibility, quickstart, gates, and the PR draft.

The fix ships as one pull request. The constitution requires the changelog entry in the same
change, and the `Read` doc comment would otherwise describe the old base. The release
`problem/v0.2.2` ([research R-01](research.md#r-01-release-level-and-changelog)) is prepared in a
separate PR after the squash merge, as for 0.2.1. The changelog entry stays under `[Unreleased]`
until then.

---

## Notes

- Every task stays within `problem/` and this file.
- Never weaken a test to make it pass. Fix the implementation, or update the spec and the
  contracts first if the behavior itself must change.
- The reproduction probe and the spike live only in the session scratchpad, outside the
  repository. Re-implement from these tasks; do not copy files from them.
- The wording fix for the opening paragraph of User Story 2 in spec.md, recorded in
  [plan.md](plan.md), is the maintainer's decision. No task applies it.

# Quickstart: Validating the Problem Module Fixes and Improvements

This guide proves that the feature works end to end. The expected behavior is defined in:

- [contracts/public-api.md](contracts/public-api.md);
- [contracts/document.md](contracts/document.md);
- [data-model.md](data-model.md).

This guide only says how to check it. Test names follow the `Test<Area>_<Scenario>` convention
of the module. `tasks.md` fixes the names.

## Prerequisites

- Go 1.26 or newer, and a C toolchain for `-race`.
- golangci-lint at the version pinned in `versions.env`.
- The working tree contains the changes. The checks run from `problem/`, with `GOWORK=off`,
  unless stated otherwise.

## 1. Local gates (constitution)

```bash
make check MODULE=problem     # lint with zero findings, then -race -shuffle=on tests with coverage ≥ 80%
```

**Expected**: a `PASS` line for `problem` from both `lint` and `test`, and no new
`.module-exceptions` entry.

## 2. Tidy and API compatibility

```bash
(cd problem && GOWORK=off go mod tidy -diff)     # prints nothing
grep -c require problem/go.mod                   # 0
```

`gorelease` runs in the pull request workflow, at the version pinned in `versions.env`.

**Expected**: it reports only compatible changes against `problem/v0.1.0`, and suggests
`v0.2.0` (research R-01).

## 3. Scenario checks per user story

| Story | Command | What must hold |
|-------|---------|----------------|
| US1: headers | `go test -race -run 'Write_RepresentationHeaders\|Write_HeadersKept\|Write_RealServer' -v .` | Through a real `httptest.NewServer`, `Content-Length: 5` and `Content-Encoding: gzip` set before a 404 give the client the complete `{"type":"about:blank","title":"Not Found","status":404}`, with no read error and a nil error from `Write`. Each of the 7 headers is absent after `Write`, including both values of a double `Trailer`, and also for the generic server error, an invalid instance, a plain error and HEAD. `Retry-After`, `Content-Language`, `Cache-Control`, `Vary` and `X-Content-Type-Options` keep their values, and `Content-Type` is `application/problem+json`. |
| US2: nil problem | `go test -race -run 'Lookup_NilProblem\|Errors_NilProblem' -v .` | `balance.Lookup(nil)` returns `0, false`. Reading `balance` from the ignored result of a failed `Match` returns `0, false`. `(*Error)(nil).Errors()` returns `nil, false`. No panic. |
| US3: derive | `go test -race -run 'With_' -v .` | A copy with `request_id` has every member of the original plus the new one, and the original's document is unchanged. The cause is found by `errors.Is` and `errors.As`, and `Match` matches. 100 concurrent derivations each carry only their own ID, with no race reported. A replaced `balance` differs between the copy and the original. An invalid original stays invalid in the copy, with the original reason. A new failure invalidates only the copy. A nil receiver gives nil. A problem read with `Read` keeps its resolved type and extensions. A copy with no options writes byte-identical bytes. |
| US4: validation | `go test -race -run 'WithErrors_\|Errors_\|Pointer_' -v .` | The RFC 9457 §3 occurrence is written exactly as in [document.md](contracts/document.md#the-errors-member). Every row of the pointer table matches. An item without a pointer has only `detail`. The items read back equal the originals. For the not-found rows of the reading table, `Errors()` gives `nil, false`, and extra members such as `code` are ignored. `WithErrors` and a key named `errors` follow "last wins", in both orders. `WithErrors()` gives `"errors":[]`. |
| US5: write errors in docs | see section 5 | No discarded `Write` error remains. |
| US6: domain errors | `go test -run 'Example_translateDomainErrors' -v .` | The output shows a 404, a 403 whose detail and `balance` come from the error's fields, a 409, and the generic 500 for an unknown error. No output contains `users_email_key` or `pq:`. |

## 4. Fuzzing (FR-013, FR-016, SC-005)

```bash
cd problem
GOWORK=off go test -run '^$' -fuzz '^FuzzPointer$' -fuzztime 10m .
GOWORK=off go test -run '^$' -fuzz '^FuzzParse$'   -fuzztime 10m .
```

**Expected**: no failure after 10 minutes for each target.

- `FuzzPointer` checks that every pointer is ASCII, is a URI reference, and decodes back to
  its tokens.
- `FuzzParse` also checks that `Errors()` never panics, and that the items it finds survive
  `WithErrors` unchanged.

## 5. Documentation

```bash
cd problem
grep -n '_ = problem.Write' README.md doc.go example*_test.go    # no output (SC-007)
grep -n '^## ' README.md                                     # includes "Translating domain errors"
GOWORK=off go doc -all . | grep -n -A3 'func Write'          # lists the removed headers
GOWORK=off go test -run 'Example' -v . | grep -c '^--- PASS: Example'
```

**Expected**:

- the `grep` for discarded errors prints nothing;
- the README has the sections of [research R-10](research.md#r-10-documentation-and-examples-i3-i4-fr-019-to-fr-025);
- the `Write` doc comment lists the 7 headers;
- every example passes, including these new ones:
  - `ExampleError_With`;
  - `ExampleWithErrors`, a validation function returning a 422 with two items;
  - `ExamplePointer`;
  - `ExampleError_Errors`;
  - `Example_translateDomainErrors`.

Check by reading the README:

- "Answering requests" explains `ErrInvalid` (a bug in how the problem was built) and other
  errors (the client usually went away);
- "Translating domain errors" shows the wrong `WithDetail(err.Error())` and the right version.

Then run `go doc -all . | less`. It shows the three new examples as whole files, with their
helper declarations.

## 6. Integration with the `middleware` module (manual)

Repeat step 6 of [004 quickstart.md](../004-problem-details/quickstart.md). Use the refusal
function of the new README section, which passes `rej.Err` as the cause and handles the error.

**Expected**:

- the refusal function is at most 5 lines;
- the second response is a 429 with `Retry-After` and the body
  `{"type":"about:blank","title":"Too Many Requests","status":429}`.

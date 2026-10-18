# Quickstart: Validating the Problem Module

This guide proves the feature works end to end. The expected behavior is defined in:

- [contracts/public-api.md](contracts/public-api.md);
- [contracts/document.md](contracts/document.md);
- [data-model.md](data-model.md).

This guide only says how to check it.

## Prerequisites

- Go 1.26 or newer, and a C toolchain for `-race`.
- golangci-lint at the version pinned in `versions.env`.
- From the repository root, run `make work` once so that `go.work` lists `./problem`. This is
  only for editor navigation and for step 6; no gate uses it.

## 1. Local gates (constitution)

```bash
make check MODULE=problem     # golangci-lint with zero findings, then go test -race -shuffle=on with coverage ≥ 80%
```

**Expected**: a `PASS` line for `problem` from both `lint` and `test`, with no
`.module-exceptions` entry.

## 2. Structure, tidy and cross-build (the same checks CI runs)

```bash
scripts/structure-check.sh problem   # README, CHANGELOG, no replace, go directive ≤ 1.26
(cd problem && GOWORK=off go mod tidy -diff)
for os in linux darwin windows; do for arch in amd64 arm64; do
  (cd problem && GOWORK=off GOOS=$os GOARCH=$arch go vet ./...) || echo "FAIL $os/$arch"
done; done
grep -c require problem/go.mod      # 0
```

**Expected**:

- the structure check reports `PASS` for `problem`;
- tidy prints nothing;
- there are no `FAIL` lines;
- `go.mod` has no `require` directive.

## 3. Scenario checks per user story

Run these from `problem/` with `GOWORK=off`. Test names follow the `Test<Area>_<Scenario>`
convention fixed in `tasks.md`.

| Story | Command | What must hold |
|-------|---------|----------------|
| US1: respond | `go test -race -run 'Status\|Occurrence\|Extension\|Write\|Must' .` | `Status(404)` gives status 404, `Content-Type: application/problem+json` and exactly `{"type":"about:blank","title":"Not Found","status":404}`. The out-of-credit occurrence gives 403 and the document of [document.md](contracts/document.md#examples), with `balance` a number and `accounts` an array. A replaced title is written. `Retry-After` and `Content-Language` survive, and `text/plain` is replaced. The `errors` extension keeps its nesting. HEAD gives the status and headers with an empty body. Two writes, and 64 concurrent writes, are byte-identical. Every document passes the Appendix A schema helper. |
| US2: errors | `go test -race -run 'Error\|Leak\|Cause\|Match' .` | A problem wrapped twice is written unchanged, without "charge message" or "send message". The `pq: password authentication failed` error gives the generic server error, and no token of the message appears in the response. `errors.Is` finds the internal cause, which is not in the response. From a join of a plain error and a problem, the problem is written. `OutOfCredit.Match` is true exactly for trees that contain an out-of-credit problem. `Error()` contains the title and the detail. |
| US3: catalog | `go test -race -run 'NewType\|NewKey\|Must\|Instance\|Concurrent' .` | The four accepted type URIs are accepted. `out-of-credit`, a malformed URI, `about:blank`, an empty title and statuses 200, 302 and 600 are refused with `ErrInvalid` and the element named. The names `type`…`instance`, `Status`, `id`, `user-id`, `2fa` and `_meta` are refused, and `balance`, `retry_after` and `errors` are accepted. `Must` panics on a refused declaration. An occurrence with the instance `/a b` is written as the generic server error, and `Write` returns `ErrInvalid`. Two occurrences of one type do not affect each other or the declaration. |
| US4: read | `go test -race -run 'Read\|Parse' .` | The out-of-credit response reads back with `Balance.Lookup` = 30 and `Accounts.Lookup` = the two accounts, and `Match` is true. `"status":"403"` and `"title":42` are absent with no error. A missing `type` gives `about:blank`. Relative `type` and `instance` resolve against `https://api.example.com/v1/messages`. Unknown extensions appear in `Extensions()`. A `text/html` 502 gives `ErrNotProblem`, with the body still fully readable. More than 1 MiB, invalid JSON, `[]` and `null` give `ErrMalformed`, and the counting reader shows at most 1 MiB + 1 byte read. `"status":500` on a 503 response gives `Status()` = 500. A written problem parses back equal. |
| Edge cases | `go test -race -run 'Edge' .` | These hold: `Status(200)` and `Status(0)` give the generic server error and `ErrInvalid`; `Status(499)` has no title; a `+Inf` extension gives the generic server error and `ErrInvalid`; a failing writer makes `Write` return the write error; empty extension values (`""`, `0`, `false`, `null`, `[]`, `{}`) are written as such; accents, emoji, quotes and `<script>` survive a round trip, with markup escaped as `<`; `Write(w, r, nil)` gives the generic server error and `ErrInvalid`; `TYPE` and `Status` read as extensions; the last of two duplicate names wins; deep nesting gives `ErrMalformed`; `Application/Problem+JSON; charset=utf-8` is recognized; `{}` gives `about:blank`. |

## 4. Fuzzing (FR-027, SC-005)

```bash
cd problem
GOWORK=off go test -run '^$' -fuzz '^FuzzParse$' -fuzztime 10m .
```

**Expected**: no failure after 10 minutes. The fuzz target checks two things:

- `Parse` never panics;
- every accepted document whose status is between 400 and 599 survives a write and a parse
  unchanged.

## 5. Benchmarks (SC-005)

```bash
cd problem
GOWORK=off go test -run '^$' -bench . -benchmem -count 3 .
```

**Expected**: `BenchmarkParse`, which parses a 1 MiB document, stays well under 100 ms per
operation. The spike measured about 5 ms ([research R-13](research.md#r-13-performance)).
`BenchmarkWrite` is recorded for reference.

## 6. Integration with the `middleware` module (SC-007, manual)

From a scratch directory **outside** the repository:

```bash
mkdir problem-mw && cd problem-mw
go mod init example.com/problemmw
go get github.com/paccolamano/go/middleware@v0.1.0
go work init . /path/to/repo/problem    # problem is not released yet
```

Write a `main.go` with these two pieces:

- the refusal function from the README's middleware section, at most 5 lines;
- a `ratelimit` middleware with a limit of 1 and that function, wrapping a handler that
  replies `ok`.

Then send two requests with `httptest`.

**Expected**: the second response has status 429, `Content-Type: application/problem+json`, a
`Retry-After` header, and the body `{"type":"about:blank","title":"Too Many Requests","status":429}`.
Neither module's `go.mod` changes.

## 7. Documentation

```bash
cd problem
GOWORK=off go doc -all . | less     # every exported identifier documented
GOWORK=off go test -run 'Example' -v . | grep -c '^--- PASS: Example'
```

**Expected**: the package documentation covers the topics of
[research R-18](research.md#r-18-documentation-fr-003-principle-i), and there is at least one
passing `Example` for each exported capability:

- `NewType`, `NewKey`, `Must`, `Type.New`, `Status`, `Type.Match`, `Key.Lookup`;
- the five `With*` options;
- `Write`, `Read` and `Parse`.

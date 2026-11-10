# Quickstart: Validating the Relative Type Match Fix (problem)

This guide proves that the feature works end to end. The expected behavior is defined in:

- [contracts/public-api.md](contracts/public-api.md);
- [data-model.md](data-model.md), whose table of results restates FR-007.

This guide only says how to check it. Test names follow the `Test<Area>_<Scenario>` convention of
the module, and `tasks.md` fixes them. The tests are listed in
[research R-06](research.md#r-06-testing-approach-fr-014-fr-015-sc-001-to-sc-005-principle-iii).

## Prerequisites

- Go 1.26 or newer, and a C toolchain for `-race`.
- golangci-lint at the version pinned in `versions.env`.
- The working tree contains the changes. The checks run from `problem/` with `GOWORK=off`, unless
  stated otherwise.

## 1. The defect, before the fix (red)

With the new tests in place, but `type.go` and `read.go` still as in `problem/v0.2.0`:

```bash
GOWORK=off go test -run 'TestMatch_Read|TestWith_ReadProblem' -v .
```

**Expected**: FAIL.

- `TestMatch_ReadResponse`: only the first two rows of FR-007 fail, each with the resolved type
  `https://api.example.com/probs/out-of-credit` and no match.
- `TestMatch_ReadFromServer`: every combination fails, with a type such as
  `http://127.0.0.1:<port>/problems/user-not-registered`.
- `TestMatch_ReadTree` and the relative case of `TestWith_ReadProblem` fail.
- `TestMatch_Allocs` and `TestRead_WriteAgain` pass: they guard behavior that does not change.

## 2. Local gates (constitution)

```bash
make check MODULE=problem     # from the repository root: lint with 0 findings, then -race -shuffle=on with coverage ≥ 80%
```

**Expected**: `PASS` for `problem` from both `lint` and `test`. Coverage stays above 99% (it was
99.6% at `problem/v0.2.0`). No `.module-exceptions` entry is added.

## 3. Tidy, changelog and API compatibility

```bash
GOWORK=off go mod tidy -diff                          # prints nothing
git diff --exit-code go.mod go.sum
scripts/changelog-check.sh origin/main HEAD           # from the repository root
GORELEASE=$(go env GOPATH)/bin/gorelease scripts/apicompat.sh problem   # optional locally; runs in the pull request workflow
```

**Expected**:

- no change to `go.mod` or `go.sum`;
- the changelog check passes, because `problem/CHANGELOG.md` changes with `type.go`, `read.go`
  and `problem.go`;
- `gorelease` reports no API change and suggests `v0.2.1`
  ([contracts/public-api.md](contracts/public-api.md#expected-gorelease-report-against-problemv020)).

## 4. Scenario checks per user story

| Story | Command | What must hold |
|-------|---------|----------------|
| US1: a shared relative declaration matches | `GOWORK=off go test -race -run 'TestMatch_Read(Response\|FromServer\|Tree)' -v .` | The ten rows of FR-007 give the stated result. Through a loopback server, at least 10 relative declarations each match their problem from 6 request URLs (paths, another port, a redirect), and never the problem of another declaration. In a tree, the problem is found wrapped, joined, as a cause and past other types. With two services in one tree, the first match wins. |
| US2: nothing else changes | `GOWORK=off go test -race -run 'TestMatch_(Allocs\|Tree)\|TestWith_ReadProblem\|TestRead_(Resolve\|WriteAgain\|RoundTrip)\|TestNewType_Rules' -v .` | Problems built locally, read with `Parse` or without a request, and absolute declarations give the results of 0.2.0, with 0 allocations. A derived problem matches exactly when the original does. `Type()` is the resolved URI, and writing a read problem gives the document of 0.2.0. `/probs/out-of-credit` is declared and `out-of-credit` is refused. |
| US2: redirects | `GOWORK=off go test -race -run 'TestMatch_ReadFromServer' -v .` | After a redirect, the relative declaration matches. An absolute declaration naming the first server does not. |
| US3: example | `GOWORK=off go test -run 'ExampleType_Match_shared\|ExampleRead' -v .` | Both examples pass, and `ExampleType_Match_shared` prints the resolved type, then `true`. |

## 5. Fuzzing (FR-015, SC-004)

```bash
GOWORK=off go test -run 'FuzzRead' -v .                       # the seed corpus, as in every test run
GOWORK=off go test -run '^$' -fuzz '^FuzzRead$' -fuzztime 60s .
```

**Expected**: the seed corpus passes, the documents of every row of FR-007 among them. The fuzz
run finds no failure: in the spike, 3.2 million executions of the same property found none.
Discard any `testdata/fuzz/FuzzRead` directory that a local fuzz run creates, unless it holds a
failure to keep as a regression seed.

## 6. Documentation review (FR-011 to FR-013)

- `problem/type.go` (`Type.Match`) and `problem/read.go` (`Read`) match
  [contracts/public-api.md](contracts/public-api.md#changed-behavior). The sentence "a type
  declared to match another API's problems must use that API's absolute URI" is gone:

  ```bash
  grep -rn "absolute URI" problem/*.go problem/README.md   # from the repository root
  ```

  Only the declaration rules remain (`doc.go`, `NewType`, the error message of `NewType`), and the
  new advice to declare an absolute URI to match one host only.
- `problem/doc.go`, "Reading problem details", and `problem/README.md`, "Reading problem details",
  state the rule of FR-001 to FR-003.
- `problem/CHANGELOG.md`, `[Unreleased]`: "Fixed", marked **Behavior change**, as drafted in
  [research R-01](research.md#r-01-release-level-and-api-compatibility).
- `go doc ./problem Type.Match` (from the repository root) shows the new rule.

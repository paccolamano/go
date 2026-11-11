# Quickstart: Validating the Read Base URI Fix (problem)

This guide proves that the feature works end to end. The expected behavior is defined in:

- [contracts/public-api.md](contracts/public-api.md);
- [data-model.md](data-model.md), whose tables restate FR-006 and FR-008.

This guide only says how to check it. Test names follow the `Test<Area>_<Scenario>` convention of
the module, and `tasks.md` fixes them. The tests are listed in
[research R-06](research.md#r-06-testing-approach-fr-014-fr-015-sc-001-to-sc-005-principle-iii).

## Prerequisites

- Go 1.26 or newer, and a C toolchain for `-race`.
- golangci-lint at the version pinned in `versions.env`.
- Gitleaks, for the optional local secret scan of step 6.
- The working tree contains the changes. The checks run from `problem/` with `GOWORK=off`, unless
  stated otherwise.

## 1. The defect, before the fix (red)

With the new tests in place, but `read.go` still as in `problem/v0.2.1`:

```bash
GOWORK=off go test -run 'TestRead_(RequestURL|Credentials)|TestMatch_Read(Response|Tree)|TestWith_ReadProblem|FuzzRead' -v .
```

**Expected**: FAIL.

- `TestRead_RequestURL`: the five relative rows of FR-006 fail, each with `svc:s3cret@` in the
  value, and `#` resolved to `…?api_key=XYZ#frag`. The network-path, absolute and missing-type
  rows pass.
- `TestRead_Credentials`: every request URL fails, with the credentials in `Type()`, `Instance()`,
  `Error()` and the written document. The redirect case shows the userinfo and the `Location`'s
  fragment.
- `TestMatch_ReadResponse`, with credentials: rows 1 and 4 fail on the type, and rows 2 and 9 on
  the match.
- `TestMatch_ReadTree` and `TestWith_ReadProblem`: the cases with credentials fail.
- `FuzzRead`: the seeds with a userinfo fail.

## 2. Local gates (constitution)

```bash
make check MODULE=problem     # from the repository root: lint with 0 findings, then -race -shuffle=on with coverage ≥ 80%
```

**Expected**: `PASS` for `problem` from both `lint` and `test`. Coverage stays at 99.6%. No
`.module-exceptions` entry is added.

## 3. Tidy, changelog and API compatibility

```bash
GOWORK=off go mod tidy -diff                          # prints nothing
git diff --exit-code go.mod go.sum
scripts/changelog-check.sh origin/main HEAD           # from the repository root
GORELEASE=$(go env GOPATH)/bin/gorelease scripts/apicompat.sh problem   # optional locally; runs in the pull request workflow
```

**Expected**:

- no change to `go.mod` or `go.sum`;
- the changelog check passes, because `problem/CHANGELOG.md` changes with `read.go`;
- `gorelease` reports no API change and suggests `v0.2.2`
  ([contracts/public-api.md](contracts/public-api.md#expected-gorelease-report-against-problemv021)).

## 4. Scenario checks per user story

| Story | Command | What must hold |
|-------|---------|----------------|
| US1: credentials never reach a problem | `GOWORK=off go test -race -run 'TestRead_(RequestURL\|Credentials)' -v .` | The eight rows of FR-006 give the "This spec" column. Through a loopback server and `http.Client`, with a user name and a password, a user name only, and a relative redirect, no type URI, instance URI, message or written document contains the user name, the password or the request's fragment. `#msg-1` keeps the request's query, and `#` has no fragment. The caller's request URL still holds its userinfo and its fragment. |
| US2: everything else as before | `GOWORK=off go test -race -run 'TestRead_(Resolve\|WriteAgain\|RoundTrip)\|TestMatch_\|TestWith_ReadProblem' -v .` | Request URLs without credentials give the results of 0.2.1. The table of spec 012 gives the same results with and without credentials and a fragment. A derived problem matches exactly when the original does, and holds no credentials. `Parse` keeps members as written. |
| US3: documentation | see step 7 | |

## 5. Fuzzing (FR-015, SC-003, SC-004)

```bash
GOWORK=off go test -run 'FuzzRead' -v .                                        # the seed corpus, as in every test run
GOWORK=off go test -run '^$' -fuzz '^FuzzRead$' -fuzztime 60s -fuzzminimizetime 1s .
```

**Expected**:

- The seed corpus passes, the seeds with a userinfo, a query and a fragment among them.
- The 60-second run finds no failure. In the spike, it ran 4.86 million executions.

Why `-fuzzminimizetime 1s`: with the default of 60 seconds, `FuzzRead` stops executing after about
9 seconds, while the engine minimizes new inputs. The run still passes. This already happens on
`problem/v0.2.1` ([research R-07](research.md#r-07-fuzzing-throughput-sc-004)).

Discard any `testdata/fuzz/FuzzRead` directory that a local fuzz run creates, unless it holds a
failure to keep as a regression seed.

## 6. Secret scan of the placeholders (optional locally)

```bash
gitleaks git --log-opts="origin/main..HEAD" --redact --verbose --exit-code 1 .   # from the repository root
grep -rnE '[a-z]+://[^/@ ]+:[^/@ ]+@' problem/*.go                                # prints nothing
```

**Expected**: no leak, and no URL literal with a user name and a password in the Go sources. The
tests build their userinfo with `url.UserPassword` and `url.User`
([research R-08](research.md#r-08-placeholder-credentials-and-the-pull-request-scanners)). Semgrep
and Bearer run in the pull request workflow. If either reports a placeholder, suppress that
finding only, with the tool's ignore mechanism and a justification.

## 7. Documentation review (FR-012, FR-013)

- `problem/read.go` (`Read`), `problem/type.go` (`Type.Match`), `problem/doc.go`,
  `problem/example_test.go` (`ExampleRead`) and `problem/README.md` ("Reading problem details")
  match
  [contracts/public-api.md](contracts/public-api.md#changed-behavior). They state that relative
  members are resolved against the URL the request sent: the request URL without its userinfo
  and its fragment, with its query.

  ```bash
  cd problem && grep -n "request URL\|URL of the request\|URL of resp.Request" read.go type.go doc.go example_test.go README.md
  ```

  Each remaining occurrence either says that the userinfo and the fragment are removed, or does
  not describe the base of resolution. Before the change, it lists `read.go:27`, `type.go:66`,
  `doc.go:116`, `doc.go:119`, `example_test.go:40`, `README.md:255` and `README.md:260`, among
  others.
- `problem/CHANGELOG.md`, `[Unreleased]`: a `### Security` section, marked **Behavior change**, as
  drafted in [research R-01](research.md#r-01-release-level-and-changelog).
- `go doc ./problem Read` (from the repository root) shows the new rule.

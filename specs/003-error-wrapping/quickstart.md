# Quickstart: Validating the Errors Module

This guide proves the feature works end to end. The expected behavior is defined in:

- [contracts/public-api.md](contracts/public-api.md);
- [contracts/detail-format.md](contracts/detail-format.md);
- [contracts/log-attributes.md](contracts/log-attributes.md).

This guide only says how to check it.

## Prerequisites

- Go 1.26 or newer, and a C toolchain for `-race`.
- golangci-lint at the version pinned in `versions.env`.
- From the repository root, run `make work` once so that `go.work` lists `./errors`. This is only
  for editor navigation; no gate uses it.

## 1. Local gates (constitution)

```bash
make check MODULE=errors      # golangci-lint with zero findings, then go test -race -shuffle=on with coverage ≥ 80%
```

**Expected**: a `PASS` line for `errors` from both `lint` and `test`, with no `.module-exceptions`
entry.

## 2. Structure, tidy and cross-build (the same checks CI runs)

```bash
scripts/structure-check.sh errors   # README, CHANGELOG, no replace, go directive ≤ 1.26
(cd errors && GOWORK=off go mod tidy -diff)
for os in linux darwin windows; do for arch in amd64 arm64; do
  (cd errors && GOWORK=off GOOS=$os GOARCH=$arch go vet ./...) || echo "FAIL $os/$arch"
done; done
```

**Expected**: the structure check reports `PASS` for `errors`. Tidy prints nothing, and there are
no `FAIL` lines. The `require` list of `errors/go.mod` is empty.

## 3. Scenario checks per user story

Run these from `errors/` with `GOWORK=off`. Test names follow the `Test<Feature>_<Scenario>`
convention fixed in `tasks.md`.

| Story | Command | What must hold |
|-------|---------|----------------|
| US1: wrapping | `go test -race -run 'Wrap\|WithStack\|Errorf\|Join\|Parity\|DropIn' .` | Library chains have the same messages as the equivalent `fmt.Errorf("%s: %w")` chains, and the same `Is`, `As` and `AsType` results. `Unwrap(Errorf("x: %w", e)) == e`. Two `%w` give two causes. Every operation applied to nil returns nil. `ErrUnsupported == stderrors.ErrUnsupported`. A file written against the standard package compiles and passes its tests after only its import changes. |
| US2: detail | `go test -race -run 'Describe\|Detail\|Format\|Location' .` | The 4-level chain shows `handle request`, `load tenant 42`, `open config` and `file does not exist`, with 3 locations and one call stack, at `open config`. `%+v` equals `Describe(err).String()`. `%v`, `%s`, `%q`, `%x` and `%X`, with flags, are byte-identical to a standard error. Branches are labeled `[0]` and `[1]`, and multi-line texts use `\|` continuations. The foreign level shows `query` without a location. Locations are `<import path>/<file>`. The JSON round-trip matches. A stack deeper than 32 frames is marked truncated. |
| US3: custom types | `go test -race -run 'Bind\|Custom\|Skip' .` | `AsType[*ConnError]` finds the error through outer wraps. Its location is the constructor's caller. `Skip(0).Bind` points inside the constructor and `Skip(2).Bind` to the caller's caller. The type `*errors_test.ConnError` appears in the detail and in `%+v`. An `Error()` override is honored by every verb. |
| US4: logs | `go test -race -run 'Attr\|Slog' .` | The JSON handler output contains `"error":{"message":…,"detail":{"levels":[…]}}`. A disabled level calls `Error()` zero times. `Attr(nil)` leaves the record unchanged. A library error logged as a plain value renders exactly like `stderrors.New` with the same message, in the text, JSON and default handlers. |
| US5: sentinels and cost | `go test -race -run 'Sentinel\|Init\|Origin' .` | Package-level sentinels (direct, through a helper, or wrapped in a declaration) have no location. The first wrap at run time becomes the origin. A wrap of a traced error records no second stack. |
| Edge cases | `go test -race -run 'Pathological\|Panicking\|Concurrent' .` | A self-referencing error, 10,000 levels and 1,000 causes each render within the limits (at most 64 levels of depth and 256 in total), with the cut marked. Their time is measured by `BenchmarkDescribePathological` in §6, because tests do not assert wall-clock time. Panicking `Error` and `Unwrap` methods show the fmt placeholder. Concurrent `Describe` and formatting pass under `-race`. |

## 4. vet catches misuse (FR-010, User Story 1, scenario 5)

```bash
cd errors
GOWORK=off go test -tags vetcheck -run '^TestVetReportsWrapfMisuse$' .
GOWORK=off go vet ./testdata/vetmisuse; echo "exit=$?"
```

**Expected**: the test passes. The direct `go vet` prints exactly three findings and `exit=1`:

- `errors.Wrapf does not support error-wrapping directive %w`;
- the same finding for `(…errors.Skip).Wrapf`;
- one `reads arg #1, but call has 0 args`.

The `Errorf` lines with one and two `%w` are not reported.

## 5. Fuzzing

```bash
cd errors
GOWORK=off go test -run '^$' -fuzz='^FuzzDetailText$' -fuzztime=30s .
```

**Expected**: no failures. Every rendered line matches the grammar of
[detail-format.md](contracts/detail-format.md#grammar). The seed corpus also runs in every normal
`go test`.

## 6. Performance (SC-004)

```bash
cd errors
GOWORK=off go test -run '^$' -bench . -benchmem -count 5 .
```

**Expected**, on a typical developer machine:

| Benchmark | Target |
|-----------|--------|
| `BenchmarkWrap` and `BenchmarkWrapBehindForeign` | ≤ 3 × the ns/op of `BenchmarkStdWrap`, and ≤ 2 × its B/op |
| `BenchmarkNew` (origin, call depth 10) | < 1500 ns/op |
| `BenchmarkDescribePathological` (10,000-level chain, SC-005) | < 100 ms/op |
| `BenchmarkAttrDisabled` | ≤ 1 alloc/op (the attribute value itself; the detail is never computed) |
| `BenchmarkDescribe` (5 levels) | reported for reference, with no threshold |

The spike measured about 1.9× in time and about 1.4× in memory for a wrap, and about 1.0 µs for
an origin ([research.md R-16](research.md#r-16-performance-sc-004)).

## 7. Manual checks (optional)

**Build-directory independence (SC-006)**: copy the same small program, one that prints
`fmt.Sprintf("%+v", err)` for a 3-level chain, into two different directories. Build and run
both copies, then compare the outputs:

```bash
for d in /tmp/a /tmp/b; do mkdir -p $d && cp main.go go.mod $d/ && (cd $d && GOWORK=off GOFLAGS=-mod=mod go run . > out.txt); done
diff /tmp/a/out.txt /tmp/b/out.txt && echo identical
```

Use a temporary `go.mod` with a local `replace` that points at `errors/`. It is a throwaway file
and is never committed. **Expected**: `identical`, with locations in the form
`main/main.go:<line>` and no absolute paths.

**Sentinels in a real program**: in the same program, declare
`var ErrX = errors.New("x")` and print `errors.Describe(errors.Wrap(ErrX, "op"))`. **Expected**:
`op` with a location and a call stack, then `x` with no location.

## 8. Release readiness

- `errors/README.md` contains:
  - the purpose;
  - `go get github.com/paccolamano/go/errors`;
  - a minimal example;
  - the minimum Go version (1.26);
  - a migration note ("replace the import");
  - the `%+v` and slog notes.
- `errors/CHANGELOG.md` has an `[Unreleased]` section that lists the API.
- The PR title is `feat(errors): add error wrapping and error detail module`, and every PR status
  is green.

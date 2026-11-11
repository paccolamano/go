# Quickstart: Validating middleware 0.2.0

**Feature**: [spec.md](spec.md) | **Plan**: [plan.md](plan.md)

Runnable checks that prove the feature works. Run them from the repository root unless a step
says otherwise. The API is in [contracts/public-api.md](contracts/public-api.md), the
documentation texts in [contracts/documentation.md](contracts/documentation.md).

## Prerequisites

- Go 1.26 or 1.27 (`versions.env`).
- `golangci-lint` v2.14.0, used by `make check`.
- `gorelease` at `GORELEASE_VERSION` of `versions.env`, for step 7 only:
  `go install golang.org/x/exp/cmd/gorelease@<GORELEASE_VERSION>`.
- `benchstat` (`golang.org/x/perf/cmd/benchstat`), optional, for step 8.

## 1. Red phase (Principle III)

Write the tests of [research R-10](research.md#r-10-testing-approach-principle-iii-sc-002-to-sc-007)
before the code, then run:

```bash
cd middleware && GOWORK=off go test ./ 2>&1 | head
```

**Expected**: the build fails on `Chain.Use`, `Chain.Err` and `Chain.AppendChain`, which do not
exist yet.

## 2. Local gates

```bash
make check MODULE=middleware
```

**Expected**: 0 lint issues. Tests pass under `-race -shuffle=on`, and module coverage is at
least 80%. The spike had 100% for the root package.

## 3. User stories 1 to 4

```bash
cd middleware && GOWORK=off go test -race -run 'TestChain_' -v . 2>&1 | grep -E '^(=== RUN|--- (PASS|FAIL)|ok|FAIL)'
```

**Expected**: every `TestChain_*` test passes, among them `TestChain_Use`, `TestChain_Err`,
`TestChain_UseWithConstructors`, `TestChain_ErrIsolation`, `TestChain_AppendChain`,
`TestChain_ThenWithErrorsPanics`, `TestChain_NilPanics` and `TestChain_UseSameAllocs`.

## 4. Examples (User Story 5)

```bash
cd middleware && GOWORK=off go test -run 'Example' -v . 2>&1 | grep -E '^(--- |ok|FAIL)'
```

**Expected**: `ExampleChain_Use`, `ExampleChain_Err`, `ExampleChain_AppendChain` and
`ExampleNew_recommended` pass, with the outputs of
[contracts/documentation.md](contracts/documentation.md#examples-example_testgo).

## 5. A forgotten check fails at startup (User Story 2)

Write this program in a scratch directory inside the module, such as `middleware/zz_panic/`, and
delete it afterwards:

```go
package main

import (
	"net/http"

	"github.com/paccolamano/go/middleware"
	"github.com/paccolamano/go/middleware/bodylimit"
	"github.com/paccolamano/go/middleware/cors"
)

func main() {
	c := middleware.New().Use(cors.New([]string{"bad"})).Use(bodylimit.New(0))
	http.Handle("/", c.Then(http.NotFoundHandler())) // Err not checked
}
```

```bash
cd middleware && GOWORK=off go run ./zz_panic; rm -r zz_panic
```

**Expected**: the program exits with status 2 before serving anything, and prints:

```text
panic: middleware: chain has configuration errors:
	middleware: invalid configuration: cors: origins: "bad" is not an origin (scheme://host[:port])
	middleware: invalid configuration: bodylimit: maxBytes: must be greater than 0
```

## 6. Misuse does not compile (FR-005)

In the same way, build a scratch `main` package with:

```go
var spanAttrs middleware.Middleware = func(h http.Handler) http.Handler { return h }
var c middleware.Chain
_ = c.Append(requestid.New())
_ = c.Use(spanAttrs)
_ = c.Extend(c)
```

```bash
cd middleware && GOWORK=off go build -o /dev/null ./zz_cfail; rm -r zz_cfail
```

**Expected**: three compile errors, as in
[research R-08](research.md#r-08-telling-use-and-append-apart-fr-005): `Append` refuses the
error result, `Use` wants two arguments, and `Extend` is undefined.

## 7. API compatibility (SC-006)

Commit the change first: `gorelease` refuses a working tree with uncommitted changes.

```bash
scripts/apicompat.sh middleware
```

**Expected**: the report of
[contracts/public-api.md](contracts/public-api.md#gorelease-report-against-middlewarev010):
`Chain.Extend` removed, and `Chain.AppendChain`, `Chain.Err` and `Chain.Use` added. The suggested
version is `v0.2.0`. The script exits 0, because the module is at 0.x.

## 8. Cost per request (SC-005)

```bash
cd middleware && GOWORK=off go test -run '^$' -bench 'BenchmarkChain_Use' -count 10 -cpu 1 . | tee /tmp/use.txt
benchstat /tmp/use.txt   # optional
```

**Expected**: the `New` and `Use` sub-benchmarks report the same B/op and allocs/op, and medians
within 5% of each other. The spike measured 1473.5 and 1474 ns/op.
`TestChain_UseSameAllocs` already checks the allocations in step 3.

## 9. Documentation (FR-018 to FR-021)

```bash
grep -rn 'Extend' middleware/
cd middleware && GOWORK=off go doc -all . | grep -n -A3 'func (c Chain) \(Use\|Err\|AppendChain\|Then\)'
```

**Expected**:

- `Extend` appears only in `middleware/CHANGELOG.md`, in the 0.1.0 entry and in the new
  **Removed** entry.
- Each new method's documentation states when it panics.
- The README usage and the Conventions section of `doc.go` build a chain with `Use` and check
  `Err` once.
- The CHANGELOG's `[Unreleased]` section has the entry of
  [contracts/public-api.md](contracts/public-api.md#changelog-entry).

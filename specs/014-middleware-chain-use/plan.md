# Implementation Plan: Building a Chain From the Constructors' Results (middleware 0.2.0)

**Branch**: `014-middleware-chain-use` (suggested; work currently on `main`) | **Date**: 2026-11-11 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `specs/014-middleware-chain-use/spec.md`

**Note**: This template is filled in by the `/speckit-plan` command; its definition describes the execution workflow.

## Summary

This feature changes the root package of the existing module
`github.com/paccolamano/go/middleware`, released as `middleware/v0.1.0`. It ships as
`middleware/v0.2.0`, a breaking minor release at 0.x. A chain takes a constructor's two results
directly, records the errors, and reports them with one check:

- **`Chain.Use(mw Middleware, err error) Chain`**: when `err` is not nil, it records the error
  and does not add `mw`. Otherwise it is `Append(mw)`, so `(nil, nil)` panics with `Append`'s
  message (R-04).
- **`Chain.Err() error`**: `errors.Join` of the errors recorded in the chain and in its
  ancestors, in order. It is nil when there is none. Siblings never see each other's errors
  (R-03, R-05).
- **`Chain.Then`** panics on a chain with recorded errors, before the nil-handler check. The
  panic value is an error that wraps them, with the prefix
  `middleware: chain has configuration errors:`. The panic covers only a forgotten `Err` check,
  as the nil-middleware panic covers an ignored constructor error (R-06).
- **`Chain.AppendChain(other Chain) Chain`** replaces `Extend`, with the same order, and also
  carries `other`'s errors. `Extend` is removed with no alias (R-07).
- **State**: `Chain` gains an unexported `errs []error`. Like `mws`, it is written only when the
  chain is created, and copied with `slices.Concat` by every derivation that adds to it (R-03).
- **Documentation**: `doc.go`, the README usage and three new examples show `Use` and one `Err`
  check. `ExampleNew_recommended` drops its error-collecting helper, which panicked instead of
  returning the error (R-12). The CHANGELOG records the removal and the additions.

The design was implemented in a scratch clone of the repository, outside the working tree:

- `golangci-lint` reported 0 issues;
- the existing suite passed under `-race -shuffle=on` with only the `Extend` rename, and the root
  package had 100% coverage;
- `gorelease` reported `Chain.Extend` removed and `Use`, `Err`, `AppendChain` added, and
  suggested `v0.2.0`;
- the prototype case of the request gave the expected results: an invalid CORS origin and a
  body limit of 0 reported together, the base usable, and `Then` panicking with both errors;
- `Append(requestid.New())`, `Use(spanAttrs)` and `Extend` failed to compile;
- the planned examples produced their documented output;
- a request cost the same through a chain built with `Use` as with `New`: 18 allocations,
  1474 and 1473.5 ns/op.

## Technical Context

**Language/Version**: Go. The `go` directive stays `go 1.26.0`. CI also tests on 1.27. The code
uses only `errors.Join` (Go 1.20), `slices.Concat` (Go 1.22) and `slices.Backward` (Go 1.23),
which the module already relies on.

**Primary Dependencies**: the standard library only, tests and examples included (FR-017).
`go.mod` does not change ([research R-13](research.md#r-13-lint-and-dependencies-fr-017)).

**Storage**: N/A. The only state is the immutable `Chain` value, built at startup
([data-model.md](data-model.md)).

**Testing**: the standard `testing` package, with table-driven tests and named subtests, in the
root package's external test package:

- the existing `recorder`, `handler`, `serve` and `must` helpers;
- the real constructors `cors.New` and `bodylimit.New` with invalid settings, for the prototype
  case;
- `recover` for the panics, and `errors.Is` on the panic value;
- `testing.AllocsPerRun` for the cost per request, and a benchmark for the time;
- examples for `Use`, `Err` and `AppendChain`.

Details: [research R-10](research.md#r-10-testing-approach-principle-iii-sc-002-to-sc-007).

**Target Platform**: any platform Go supports. Pure Go, with no build constraints.

**Project Type**: library. One existing Go module, `middleware/`. Only its root package changes.
The 13 subpackages and `internal/` do not.

**Performance Goals** (SC-005, [R-11](research.md#r-11-cost-fr-012-sc-005)):

- Serving a request through a chain built with `Use` makes the same allocations, and takes at
  most 5% more time, as the same chain built with `New`. The spike measured 18 allocations for
  both, and a 0.03% time difference.
- `Then` adds one length test on a chain without recorded errors, at startup only.

**Constraints**:

- Chains stay immutable: no derivation writes into an array that another chain can see (FR-001,
  FR-009, FR-015).
- Chains built without `Use` behave exactly as in 0.1.0 (FR-012, FR-016).
- No goroutine, no `init`, no global mutable state, no output of its own. The only panics are
  programming errors at startup, each with a `//nolint:forbidigo` that states its reason.

**Scale/Scope**:

- `chain.go` grows from 62 to about 100 lines: one field, three methods, one check in `Then`,
  and `Extend` removed.
- The exported surface gains 3 methods and loses 1
  ([contracts/public-api.md](contracts/public-api.md)).
- Tests: 6 new test functions in `chain_test.go` and 1 in the new `chain_internal_test.go`,
  `TestChain_Extend` renamed and extended, 1 benchmark, 3 examples, and the recommended example
  migrated.
- Documentation: `doc.go` (overview and Conventions), the README usage, and the CHANGELOG.

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

| Rule | Assessment | Status |
|------|------------|--------|
| **I. Independent modules** | Only `middleware/` changes. No dependency, no `replace`. The README and the CHANGELOG are updated. The module builds and tests with `GOWORK=off`. Its purpose is unchanged. | ✅ |
| **II. SemVer & stable API** | The tag is `middleware/v0.2.0`. At 0.x, a MINOR release may break, and `gorelease` suggests `v0.2.0` (R-01). The deprecation rule covers removals in a MAJOR version, so none applies at 0.x, as plans 012 and 013 noted. The surface grows by 3 methods, each mapped to FRs, and loses `Extend`, renamed. The CHANGELOG lists exactly the `gorelease` report, the removal marked **Breaking**, with its migration (SC-006, FR-021). | ✅ |
| **III. Test-first** | `tasks.md` will order every test before its code, and step 1 of the quickstart observes the red phase. Every exported method added or changed is exercised: `Use`, `Err`, `AppendChain`, `Append`, `Then`. Tests are table-driven and hermetic: in-memory handlers, `httptest` without a listener, no sleep. Examples cover the three additions. The benchmark covers SC-005. No new external input, so no fuzz test. Coverage: 100% for the root package in the spike. | ✅ |
| **IV. Idiomatic Go** | 0 lint issues in the spike. Every new method has a doc comment that states its failures, and `doc.go` is updated. The configuration errors stay returned errors, read with `Err` and matchable with `errors.Is`. **Panics**: `Then` panics only for the programming error of never calling `Err`. The recoverable condition is still returned as an error, so this is not a panic "for recoverable conditions". It follows R-03 of spec 002, and the `//nolint:forbidigo` states the reason (R-06). No `init`, no global, no goroutine, no I/O, so no new `context.Context` parameter. | ✅ |
| **V. Simplicity & dependencies** | No dependency. No new type: three methods on `Chain`, each mapped to a requirement. The rejected alternatives (collector type, builder, `Must`, placeholder middleware) each add a type or a runtime path (R-02, R-06). Methods return the concrete `Chain` and the `error` interface, the Go convention for errors. | ✅ |
| Go toolchain support | `go 1.26.0` is unchanged. Nothing newer is used. | ✅ |
| Platforms | Pure Go, with no OS-specific code. | ✅ |
| Repository layout | No tooling change, no lint override, no `.module-exceptions` entry. | ✅ |
| Dependency hygiene | `go.mod` is unchanged, so `go mod tidy -diff` stays clean. | ✅ |
| Vulnerability scanning / Secrets | No dependency to scan. Tests use example origins (`https://app.example.com`) and no credential. | ✅ |
| Local and PR gates | `make check MODULE=middleware`. `scripts/changelog-check.sh` passes because the CHANGELOG changes with `chain.go`. `scripts/apicompat.sh` prints the incompatible change and passes at 0.x. | ✅ |
| Commits / Release | The PR title is `feat(middleware)!: build a chain from the constructors' results, and rename Extend to AppendChain`, with the `middleware` scope and `!` for the breaking change. The release follows in `chore(middleware): prepare release 0.2.0`, which tags `middleware/v0.2.0`. | ✅ |

**Result (pre-research)**: PASS, with no deviation.

**Result (post-design re-check)**: PASS.

- The data model adds one field, written only when a chain is created. It adds no goroutine, no
  global and no allocation per request.
- The two contracts map every addition, the removal, each panic and each documentation change
  to an FR. The `gorelease` report of the spike matches the API contract.
- No `NEEDS CLARIFICATION` remains: the spec settled every decision, and the research filled in
  the details.
- The spec needs no change: no decision of the research contradicts it. The research records
  the details the spec left open:
  - `Err` joins the errors also when there is one (R-05);
  - the panic of `Then` is an `error` value that wraps the recorded errors, while the nil panics
    stay strings (R-06);
  - `Use(nil, nil)` reuses `Append`'s message and position (R-04);
  - telling `Use` and `Append` apart is left to the compiler, checked by hand in the quickstart
    (R-08);
  - `ExampleNew_recommended` is migrated to `Use`, which removes a helper that panicked on
    invalid configuration instead of returning the error (R-12).

## Project Structure

### Documentation (this feature)

```text
specs/014-middleware-chain-use/
├── spec.md                    # feature specification
├── plan.md                    # this file
├── research.md                # Phase 0: decisions R-01 … R-13, with spike evidence
├── data-model.md              # Phase 1: Chain fields, invariants, derivations, panics, derivation graph
├── quickstart.md              # Phase 1: validation guide
├── contracts/
│   ├── public-api.md          # API at 0.2.0, doc comments, panics, removal, gorelease report, CHANGELOG entry
│   └── documentation.md       # doc.go and README texts, the three examples with their output, the recommended example
├── checklists/
│   └── requirements.md        # spec quality checklist
└── tasks.md                   # Phase 2 (/speckit-tasks, not created here)
```

### Source Code (repository root)

```text
middleware/
├── go.mod                       # unchanged: go 1.26.0, no require
├── CHANGELOG.md                 # [Unreleased]: Removed (Breaking) Extend; Added Use, Err, AppendChain, the panic of Then
├── README.md                    # Usage: a base and a derived chain with Use, one Err check; the chain paragraph
├── doc.go                       # overview names Use and AppendChain; Conventions: Use, Err, the panic of Then
├── chain.go                     # Chain gains errs; Use, Err, AppendChain added; Extend removed; Then checks errs first
├── middleware.go                # unchanged
│
├── chain_test.go                # + TestChain_Use, _Err, _UseWithConstructors, _ErrIsolation, _ThenWithErrorsPanics,
│                                #   _UseSameAllocs; TestChain_Extend becomes TestChain_AppendChain, extended;
│                                #   TestChain_NilPanics gains the Use row
├── chain_internal_test.go       # NEW: TestChain_UseErrorSkipsMiddleware (the middleware is not added, FR-003)
├── example_test.go              # + ExampleChain_Use, ExampleChain_Err, ExampleChain_AppendChain
├── example_recommended_test.go  # recommendedChain built with Use, its add helper removed; one Err check
├── bench_test.go                # + BenchmarkChain_Use (New and Use sub-benchmarks)
├── conventions_test.go, integration_test.go, ordering_test.go,
│   rejection_integration_test.go, bandwidth_integration_test.go   # unchanged
│
├── internal/                    # unchanged
└── accesslog/ … secureheaders/  # the 13 subpackages, unchanged
```

**Structure Decision**: the existing module `middleware/` is changed in place, in its root
package only:

- the new methods live in `chain.go`, next to `Append` and `Then`;
- their tests extend `chain_test.go`, which already holds the chain helpers;
- the examples extend `example_test.go`, and the benchmark extends `bench_test.go`.

Nothing outside `middleware/` and this spec directory changes. `go.work` already lists
`./middleware`.

## Complexity Tracking

No deviation from the constitution, so there is nothing to justify. The panic of `Then` is
assessed under Principle IV in the Constitution Check: it covers a programming error, not a
recoverable condition.

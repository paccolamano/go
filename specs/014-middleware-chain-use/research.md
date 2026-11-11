# Research: Building a Chain From the Constructors' Results (middleware 0.2.0)

**Feature**: [spec.md](spec.md) | **Plan**: [plan.md](plan.md) | **Date**: 2026-11-11

Each section records a decision, its rationale and the alternatives rejected.

The spec settled the design, so this research had no open question to resolve. It fixes the
details the spec left to the plan, and checks the design with a spike kept outside the working
tree: a local clone of this repository in a scratch directory, where `middleware/chain.go` was
changed as described below and committed locally. In the spike:

- the existing suite passed under `-race -shuffle=on` with one change only, `Extend` renamed
  `AppendChain` in `chain_test.go`, and new tests covered the prototype case of the request;
- `golangci-lint` with the repository configuration reported 0 issues;
- statement coverage of the root package was 100%;
- `gorelease` against `middleware/v0.1.0` reported exactly the change of [R-01](#r-01-release-level-and-api-compatibility);
- the three misuses of [R-08](#r-08-telling-use-and-append-apart-fr-005) failed to compile;
- a request through a chain built with `Use` cost the same as through the same chain built with
  `New` ([R-11](#r-11-cost-fr-012-sc-005)).

Measurements come from Go 1.27.1 on linux/amd64 and an Intel i7-11800H.

## R-01 Release level and API compatibility

- **Decision**:
  - The release is `middleware/v0.2.0`, a breaking MINOR release while the module is at 0.x.
  - `gorelease -base=v0.1.0`, run on the spike, reports:

    ```text
    # github.com/paccolamano/go/middleware
    ## incompatible changes
    Chain.Extend: removed
    ## compatible changes
    Chain.AppendChain: added
    Chain.Err: added
    Chain.Use: added

    # summary
    Suggested version: v0.2.0 (with tag middleware/v0.2.0)
    ```

  - The CHANGELOG lists exactly this removal and these three additions (SC-006), the removal
    marked **Breaking** as in `slogx/v0.2.0` ([contracts/public-api.md](contracts/public-api.md#changelog-entry)).
  - The feature PR is titled
    `feat(middleware)!: build a chain from the constructors' results, and rename Extend to AppendChain`.
    The release follows in `chore(middleware): prepare release 0.2.0`, which tags
    `middleware/v0.2.0`.
- **Rationale**:
  - Principle II requires a new MAJOR version only from v1.0.0. At 0.x, `scripts/apicompat.sh`
    prints incompatible changes without failing, and the suggested version matches the request.
  - The deprecation rule of Principle II applies to "the MAJOR version that removes the
    identifier". No MAJOR version is involved at 0.x, as plans 012 and 013 also noted.
  - Only `chain_test.go` calls `Extend`, and no other module of this repository imports
    `middleware`.
- **Alternatives rejected**:
  - Keeping `Extend` as a deprecated alias of `AppendChain`: the request excludes it, and it
    would leave two names for one operation with no caller to protect.

## R-02 API shape

- **Decision**: three methods on the existing value type, and no new type:

  ```go
  func (c Chain) Use(mw Middleware, err error) Chain
  func (c Chain) Err() error
  func (c Chain) AppendChain(other Chain) Chain
  ```

  `Chain` gains one unexported field. Its zero value stays a valid empty chain with no recorded
  errors.
- **Rationale**:
  - Go passes the two results of a call straight to a function only when that function takes
    exactly those two parameters. `Use(mw Middleware, err error)` is that function, so
    `c.Use(requestid.New())` compiles, and no user helper can offer the same thing over a list of
    constructors.
  - One type keeps one concept: every way to derive a chain returns a `Chain`, so `Use`,
    `Append` and `AppendChain` mix freely, and one `Err` check on the most derived chain covers
    every ancestor.
  - Adding an unexported field to a struct is a compatible change for `gorelease`. `Chain` was
    already not comparable, since it holds a slice.
- **Alternatives rejected** (from the request, confirmed here):
  - A separate collector type with `Collect(mw, err)` and `Err`: two concepts for one job, and
    the chain would still need the collected middleware.
  - A builder whose `Build()` returns `(Chain, error)`: a second type, and one check per derived
    chain, since each route group would need its own `Build`.
  - `Must` variants of the constructors: configuration often comes from files, and R-04 of the
    research of spec 002 rejected them.
  - A mutable `chain.Use(m)`: R-02 of the research of spec 002 rejected it because two
    derivations from one base would overwrite each other. This `Use` returns a new chain.

## R-03 Storing the recorded errors

- **Decision**:
  - `Chain` holds `errs []error` next to `mws []Middleware`. Both slices are written only when
    the chain is created, and never afterwards.
  - Every derivation that adds an element builds a new array with `slices.Concat`, as `Append`
    already does for middleware. A derivation that adds nothing to a slice reuses the receiver's
    slice as is: `Append` reuses `c.errs`, and `Use` with an error reuses `c.mws`.
  - No code ever calls `append` on a slice of an existing chain.
- **Rationale**:
  - The slices are never written after creation, so sharing them read-only is safe, also across
    goroutines. `slices.Concat` may return an array with spare capacity, but no derivation ever
    writes into it. This is the rule that already protects `mws` (R-02 of spec 002, FR-015).
  - A flat, ordered slice gives `Err` the recording order directly (FR-008), and
    `AppendChain` concatenates two of them (FR-014).
  - Errors are recorded only while chains are built, at startup, so copying a few pointers per
    derivation costs nothing that matters.
- **Alternatives rejected**:
  - Accumulating one error with `errors.Join(c.err, err)` at each `Use`: the result nests joined
    errors into a tree, one level per call, and `AppendChain` would nest two trees. The message
    stays readable, but `Unwrap` no longer lists the recorded errors.
  - A parent pointer per chain, with the errors of each derivation step: it shares storage, but
    `AppendChain` must still copy the other chain's errors, so the chain would carry two
    representations.
  - `append(c.errs, err)`: two `Use` calls on the same base could write into the same spare
    capacity, so sibling chains would see each other's errors (FR-009).

## R-04 `Use`

- **Decision**:

  ```go
  func (c Chain) Use(mw Middleware, err error) Chain {
  	if err != nil {
  		return Chain{mws: c.mws, errs: slices.Concat(c.errs, []error{err})}
  	}
  	return c.Append(mw)
  }
  ```

  - The error is checked first. When it is not nil, the middleware is ignored, even when the
    constructor returned one (FR-003).
  - When the error is nil, `Use` is `Append` with one middleware, so a nil middleware panics with
    `Append`'s message, `middleware: nil middleware at position N`, where N counts the
    middleware already in the chain (FR-004).
  - Any error is recorded, whatever its type (FR-006).
- **Rationale**:
  - Go's convention for `(T, error)` results is that the value is meaningless when the error is
    not nil.
  - Reusing `Append` makes `Use(mw, nil)` and `Append(mw)` the same operation by construction
    (FR-002), with the same panic: one rule, as the request asks.
- **Alternatives rejected**:
  - A message of its own for `(nil, nil)`, such as "constructor returned neither a middleware
    nor an error": `Use` cannot know which constructor it was. The position already points at
    the call, and the request asks for `Append`'s behavior.
  - Adding the middleware when both results are present: it would run a middleware whose
    constructor reported a problem with it.

## R-05 `Err`

- **Decision**: `Err` returns `errors.Join(c.errs...)`.
  - It is nil when there is no recorded error.
  - Its message is the recorded messages, one per line, in the order recorded, with no text of
    its own (FR-008).
  - `errors.Is` and `errors.As` look through it, because the joined error has `Unwrap() []error`.
  - It is joined also when there is one error: the message is the same, and `errors.Is` and
    `errors.As` behave the same.
- **Rationale**:
  - The request says "joined in the order they were recorded". `errors.Join` does exactly that,
    and it is how the standard library combines errors.
  - The recorded errors already name the package and the setting at fault, so a prefix would
    repeat what the first user's wraps repeated (Context of the spec).
  - `Err` allocates the joined error on each call. It is called once, at startup. `Then` does
    not call it ([R-06](#r-06-then-on-a-chain-with-recorded-errors)).
- **Alternatives rejected**:
  - Returning one recorded error unchanged instead of joined: one more branch, and no
    requirement needs `==` on the result. The message and `errors.Is` are the same either way.
  - Returning `[]error`: the request fixes `Err() error`, and an `error` fits
    `if err := c.Err(); err != nil`.
  - A prefix such as "middleware: chain:": every recorded error already has one.

## R-06 `Then` on a chain with recorded errors

- **Decision**:
  - `Then` checks `len(c.errs) > 0` first, before the nil handler, and panics with an `error`
    value:

    ```go
    panic(fmt.Errorf("middleware: chain has configuration errors:\n%w", errors.Join(c.errs...)))
    ```

  - The `//nolint:forbidigo` comment states the reason: a programming error, `Err` was not
    checked.
  - The nil-middleware and nil-handler panics are unchanged and keep their `string` values
    (FR-016).
  - In the spike, an uncaught panic printed:

    ```text
    panic: middleware: chain has configuration errors:
    	middleware: invalid configuration: cors: origins: "bad" is not an origin (scheme://host[:port])
    	middleware: invalid configuration: bodylimit: maxBytes: must be greater than 0
    ```

- **Rationale**:
  - **Principle IV**: the recoverable condition, an invalid configuration, is still returned as
    an error, by `Err`. The panic covers only the programming error of never calling `Err`. This
    is the same rule as the panic on a nil middleware, which catches an ignored constructor error
    (R-03 of the research of spec 002). `Then` runs at startup, when the routes are registered,
    so the failure is immediate and shows its cause.
  - **The recorded errors win over a nil handler** (edge case of the spec): they happened first,
    and they are usually the reason the developer is looking.
  - **An `error` panic value**: the runtime prints an error value's message, so the output is
    the same as with a string. Code that recovers, such as a test of route setup, can still use
    `errors.Is(v.(error), middleware.ErrInvalidConfig)`. The nil panics stay strings because
    they wrap nothing, and changing them is out of scope (FR-016).
  - The check is a length test. A chain without recorded errors does not allocate and behaves
    as in 0.1.0 (FR-012).
- **Alternatives rejected**:
  - A placeholder middleware that answers 500 when `Err` was not checked (from the request): the
    failure appears only at the first request, only on some routes, and hides its cause.
  - A `string` panic value: the same output, but the recorded errors are lost to code that
    recovers.
  - An exported panic type: API surface for a programming error.
  - Checking the nil handler first: the recorded errors are the earlier failure.

## R-07 `AppendChain`, formerly `Extend`

- **Decision**:

  ```go
  func (c Chain) AppendChain(other Chain) Chain {
  	return Chain{mws: slices.Concat(c.mws, other.mws), errs: slices.Concat(c.errs, other.errs)}
  }
  ```

  - The middleware of `c` come first, so they stay outermost, as with `Extend` (FR-013). The
    errors of `other` follow those of `c` (FR-014).
  - There is no nil check on `other`: `mws` is unexported, and `New`, `Append` and `Use` check
    every middleware, so a `Chain` never contains a nil one.
  - Duplicates are kept: `c.AppendChain(c)` lists the middleware and the errors of `c` twice, as
    the spec's edge case states.
  - `Extend` is removed. Its test becomes `TestChain_AppendChain`, unchanged otherwise.
- **Rationale**:
  - Next to `Append`, the name says what the method does: it appends a chain.
  - Without the errors, `base.AppendChain(public)` would drop `public`'s errors, and `Then`
    would not catch a chain that lacks middleware.
- **Alternatives rejected**:
  - `Prepend` (from the request): `c.Extend(other)` puts `other` after `c`, so the name would say
    the opposite.
  - Removing duplicate errors: errors have no reliable identity to compare, and middleware are
    not deduplicated either.

## R-08 Telling `Use` and `Append` apart (FR-005)

- **Decision**: the signatures keep the two operations apart. The plan adds no runtime check
  and no automated compile test. The quickstart repeats the check by hand
  ([quickstart step 6](quickstart.md#6-misuse-does-not-compile-fr-005)).
- **Evidence**: built in the spike, the three misuses fail with:

  ```text
  cannot use requestid.New() (value of interface type error) as middleware.Middleware value in argument to c.Append
  not enough arguments in call to c.Use
  	have (middleware.Middleware)
  	want (middleware.Middleware, error)
  c.Extend undefined (type middleware.Chain has no field or method Extend)
  ```

- **Rationale**:
  - The property follows from the signatures. `gorelease` guards the signatures at every pull
    request, so a change that broke it would show up in the report.
  - An automated compile test would need to generate a module and run `go build` from a test, or
    load the package with `go/types` and an importer. Both are slow or brittle for a property
    the compiler already enforces.
- **Alternatives rejected**:
  - A `go build` subprocess in a test, over a module generated in `t.TempDir`: slow, and it
    depends on the toolchain being on `PATH` at test time.

## R-09 Behavior kept (FR-012, FR-016, SC-004)

- **Decision**:
  - `New` and `Append` keep their signatures, their copies and their panics. `Append` also
    carries the receiver's errors. For a chain built without `Use`, those are nil, so it behaves
    as in 0.1.0.
  - `Then` on a chain without recorded errors runs the 0.1.0 code: the nil check, then the
    middleware applied from the innermost.
  - The subpackages do not change.
- **Evidence**: in the spike, the whole existing suite passed, `chain_test.go` included, with
  only the rename of `Extend`.

## R-10 Testing approach (Principle III, SC-002 to SC-007)

- **Decision**: the tests stay in the root package's external test package, except one internal
  test, with the existing
  `recorder`, `handler` and `serve` helpers of `chain_test.go`, and `must` of
  `rejection_integration_test.go`. Every test is written first and observed failing.
  - `chain_test.go`:
    - `TestChain_Use`, table-driven:
      - valid results run in the order added, the same as `New` (US1-1);
      - an error is recorded and the middleware is not added, also when both results are
        present (US1-2, edge case);
      - `Use` and `Append` alternate in the order added (US1-5);
      - a function outside the module, and an error that is not an invalid configuration
        (US1-7, FR-006).
    - `TestChain_Err`:
      - nil on the zero chain and on chains built with `New` and `Append` (edge cases);
      - the order across three derivation levels (US1-4);
      - `errors.Is` with `ErrInvalidConfig` and with a specific sentinel;
      - the message equal to the recorded messages joined by newlines, with no prefix (FR-008);
      - repeated reads report the same errors (FR-010).
    - `TestChain_UseWithConstructors`: the prototype case of the request, with
      `cors.New([]string{"not an origin"})` and `bodylimit.New(0)`. Both errors are reported at
      once, `base.Err()` is nil, and `base.Then` serves a request (US1-3, US3-1).
    - `TestChain_ErrIsolation`: two siblings from one base, the base's errors inherited by both,
      each sibling with only its own errors besides, and the middleware not interfering (US3).
    - `TestChain_AppendChain`: the former `TestChain_Extend`, plus errors carried after the
      receiver's, both chains unchanged, an empty chain appended, and self-append duplicates
      (US4-1 to US4-3, edge cases).
    - `TestChain_ThenWithErrorsPanics`:
      - the message prefix, both errors in order, and a panic value matching `ErrInvalidConfig`
        (US2-1);
      - it still panics after `Err` was read (US2-2);
      - every application panics (US2-3);
      - the recorded errors win over a nil handler (edge case).
    - `TestChain_NilPanics` gains the `Use(nil, nil)` row, with the message
      `middleware: nil middleware at position 1` after one middleware (US1-6).
    - `TestChain_UseSameAllocs`: `testing.AllocsPerRun` on a request is equal for the same
      middleware built with `Use` and with `New` (US2-5, SC-005).
  - `chain_internal_test.go`, package `middleware`: `TestChain_UseErrorSkipsMiddleware` checks
    that `Use` with an error adds no middleware, even when the constructor returned one (FR-003).
    From outside the package, this cannot be observed: `Then` panics on such a chain before it
    builds anything.
  - `bench_test.go`: `BenchmarkChain_Use`, with `New` and `Use` sub-benchmarks over the same
    three middleware, for the time part of SC-005.
  - Examples ([R-12](#r-12-documentation-fr-018-to-fr-020)): `ExampleChain_Use`,
    `ExampleChain_Err` and `ExampleChain_AppendChain`. `ExampleNew_recommended` is migrated to
    `Use`.
- **Rationale**:
  - Every acceptance scenario and edge case maps to a test above (SC-007). The tests are
    hermetic: in-memory handlers, `httptest` without a listener, constructors with invalid
    settings, and no sleep.
  - The allocation test is deterministic. Time is compared by the benchmark, which a test cannot
    assert without flakiness.
  - The module parses no new external input, so no fuzz test is added.

## R-11 Cost (FR-012, SC-005)

- **Decision**: no cost is added per request. `Then` composes the same middleware in the same
  order, whichever operation added them, and the recorded errors are not consulted after `Then`
  returns.
- **Evidence**: requestid, bodylimit and cors, with a CORS request, built with `New` and with
  `Use`, `-count 10`, on one CPU:

  | Chain built with | ns/op (median) | B/op | allocs/op |
  |------------------|----------------|------|-----------|
  | `New`            | 1473.5         | 1569 | 18        |
  | `Use`            | 1474           | 1569 | 18        |

  The difference is 0.03%, under the 5% of SC-005. On all 16 CPUs the runs were noisier, from
  1222 to 2005 ns/op, but the fastest run of each differed by 1.3%. The quickstart compares
  medians of 10 runs on one CPU for that reason.

## R-12 Documentation (FR-018 to FR-020)

- **Decision**:
  - `doc.go`:
    - the overview names the three ways to derive a chain (`Append`, `Use`, `AppendChain`);
    - the Conventions section shows a chain built with `Use`, one `Err` check on the most
      derived chain before `Then`, and states that `Then` panics on a chain with recorded
      errors.
  - The doc comments of `Chain`, `Use`, `Err`, `AppendChain` and `Then` state each operation's
    failures (FR-019).
  - `README.md`:
    - the Usage example builds a base chain and a derived chain with `Use`, checks `Err` once,
      and registers both;
    - the sentence after it names `New`, `Append`, `Use` and `AppendChain`, `Err` and the panic
      of `Then`.
  - `example_test.go` gains `ExampleChain_Use`, `ExampleChain_Err` and
    `ExampleChain_AppendChain`. Together they cover adding constructor results, reading the
    errors and appending a chain (FR-020).
  - `example_recommended_test.go`: `recommendedChain` builds the chain with `Use` and returns it.
    `ExampleNew_recommended` checks `Err` once.
  - After the change, `grep -rn Extend middleware/` finds only the 0.1.0 entry of
    `CHANGELOG.md` (FR-020).
  - The texts are in [contracts/documentation.md](contracts/documentation.md).
- **Rationale**:
  - The current `recommendedChain` is the problem of the spec, inside the module. Its helper
    collects errors and returns the middleware, which is nil on error. So an invalid setting
    makes `New` panic with "nil middleware at position N" instead of returning the error. With
    `Use`, the helper goes, and the error is returned.
  - Examples per method appear under each method in the package documentation. Principle III
    requires one for each primary exported API.
  - Spec 002's documents keep `Extend`: they record the 0.1.0 design, and FR-020 covers the
    module only.

## R-13 Lint and dependencies (FR-017)

- **Decision**: no dependency. The new code uses `errors`, `fmt` and `slices` from the standard
  library. The tests import subpackages of the module, as `conventions_test.go` already does.
  The one new `//nolint:forbidigo` names its reason ([R-06](#r-06-then-on-a-chain-with-recorded-errors)).
- **Evidence**: `golangci-lint run` with the repository configuration reported 0 issues on the
  spike. `go.mod` is unchanged.

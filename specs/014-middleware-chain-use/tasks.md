---

description: "Task list for middleware 0.2.0: Chain.Use, Chain.Err, the panic of Then, and AppendChain"
---

# Tasks: Building a Chain From the Constructors' Results (middleware 0.2.0)

**Input**: Design documents from `specs/014-middleware-chain-use/`

**Prerequisites**: [plan.md](plan.md), [spec.md](spec.md), [research.md](research.md),
[data-model.md](data-model.md), [contracts/](contracts/), [quickstart.md](quickstart.md)

**Tests**: REQUIRED. Constitution Principle III is non-negotiable, and SC-007 asks for every
acceptance scenario and edge case to be covered by automated tests:

- Every test task precedes the implementation it covers, and is observed failing first: T004 to
  T006 for US1, T008 for US2, T011 and T012 for US4. T010 (US3) and `TestChain_UseSameAllocs`
  (T008) guard behavior that already holds by construction. T010 is checked against a deliberate
  bug instead: it must fail when `Use` appends in place.
- Each new exported method gets a runnable example (T006, T012).
- The benchmark covers SC-005 (T019).
- Coverage stays above 80% with `-race -shuffle=on`. The spike measured 100% for the root
  package.

**Organization**: tasks are grouped by user story (spec.md US1–US5). The only foundational
change is the new `errs` field, which every story reads. All paths are relative to the
repository root. All code lives in the root package of the existing module `middleware/`.
Nothing outside `middleware/` changes, apart from ticking the boxes of this file.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: can run in parallel with other [P] tasks of the same phase: different files, and no
  dependency on an incomplete task.
- **[Story]**: US1–US5 from spec.md. Setup, Foundational and Polish tasks have no story label.

## Conventions for every task

- **Commands** run from `middleware/` with `GOWORK=off`, unless a task says otherwise. `make`
  targets, `git` and `scripts/` run from the repository root.
- **Packages**: behavior tests use the external test package `middleware_test`. Only the new
  `middleware/chain_internal_test.go` uses the internal package `middleware`, to see that a
  middleware was not added.
- **Existing helpers** (reuse them, do not duplicate them):
  - `middleware/chain_test.go`: `recorder(name, &log)` (a middleware that appends `name>`
    before and `<name` after the next handler), `handler(&log)` (appends `H`), and
    `serve(t, h)` (one GET `/` through `h`);
  - `middleware/rejection_integration_test.go`: `must(tb)`, which turns a constructor's two
    results into the middleware, or fails the test;
  - `middleware/example_test.go`: `tag(name)` (prints `enter name` and `leave name`),
    `hello()` (prints `handler`) and `serveOnce(h)`.
- **Test names** are fixed by the tasks, so that the `-run` patterns of
  [quickstart.md §3](quickstart.md#3-user-stories-1-to-4) select them. The
  [coverage map](#coverage-map-sc-007) lists them by scenario.
- **Test style**: table-driven with `t.Run` where there are several cases; no `time.Sleep`, no
  network listener, no shared state between tests; `httptest.NewRequestWithContext(t.Context(),
  …)` and `httptest.NewRecorder`; helpers call `t.Helper()` first (`thelper`).
- **Lint**: golangci-lint must report zero findings at every checkpoint. Library code gains
  exactly one `//nolint`, the `forbidigo` one of the new panic in `Then` (T009). Imports group
  `github.com/paccolamano/go` last (`goimports`). Run `gofmt -w` on every Go file you edit. The
  `unused` linter fails on a helper that nothing uses yet: add each helper in the task that first
  uses it.
- **Documentation**: when a contract or a task gives the text of a doc comment, a paragraph or a
  CHANGELOG entry, use it verbatim, wrapped at 80 columns in Go comments and as the surrounding
  Markdown is wrapped.
- **Code blocks** in this file come from the spike, which passed lint and the suite under
  `-race -shuffle=on`. Use them as given.
- **Former name**: no new code, comment or test may mention `Extend`. Only `CHANGELOG.md`
  keeps it (FR-020).
- **Commits**: do not commit, push or open a pull request unless the maintainer asks for it.

---

## Phase 1: Setup (Shared Infrastructure)

**Purpose**: work on a feature branch from a known state.

- [X] T001 From the repository root, confirm that `git status --short` lists only `go.work.sum` and `specs/014-middleware-chain-use/` as untracked, with no change under `middleware/`; that `git tag --list 'middleware/*'` ends with `middleware/v0.1.0`; and that `git grep -n 'Extend(' -- middleware` lists only `middleware/chain.go` and `middleware/chain_test.go`. If the current branch is `main`, create the feature branch with `git switch -c 014-middleware-chain-use` before editing any file. Creating the branch is not a commit.
- [X] T002 Record the baseline. From the repository root, run `make check MODULE=middleware`, which must report `PASS` for lint and test. From `middleware/`, run `GOWORK=off go test -cover .` and record the coverage of the root package. Record the list of test names with `GOWORK=off go test -list '.*' .`, for the comparison of T024 (SC-004).

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: give `Chain` the recorded errors that every story reads, with no change of behavior
([data-model.md](data-model.md#chain), [research R-03](research.md#r-03-storing-the-recorded-errors)).

**⚠️ CRITICAL**: no user story work begins until this phase is complete.

- [X] T003 In `middleware/chain.go`, add the field `errs` to `Chain` and make `Append` carry it. Replace the struct body with:

  ```go
  type Chain struct {
  	// mws and errs are never modified after the chain is built: every
  	// derivation copies them into new arrays (slices.Concat), so chains
  	// derived from the same base never share an array they write to.
  	mws  []Middleware
  	errs []error
  }
  ```

  and the body of `Append` with `return Chain{mws: slices.Concat(c.mws, mws), errs: c.errs}`. The invariants of data-model.md apply from now on: "No code calls `append` on a slice of an existing chain", and `errs` "never contains nil". Leave every doc comment unchanged. From `middleware/`, `GOWORK=off go test -race -shuffle=on .` passes unchanged, and `GOWORK=off golangci-lint run --config ../.golangci.yml .` reports 0 issues.

**Checkpoint**: `make check MODULE=middleware` is green, and behavior is that of 0.1.0.

---

## Phase 3: User Story 1 - One check for the whole chain (Priority: P1) 🎯 MVP

**Goal**: a chain takes a constructor's two results with `Use`, and `Err` reports every recorded
error of the chain and of its ancestors (FR-001 to FR-010).

**Independent Test**: build a base chain and two levels of derived chains from constructor
results, with valid and invalid settings, then read the errors of the most derived chain.

### Tests for User Story 1 ⚠️ (write first, see them fail)

- [X] T004 [P] [US1] In `middleware/chain_test.go`:
  1. Extend the imports to `errors`, `fmt`, `net/http`, `net/http/httptest`, `slices`, `strings`, `testing`, then `github.com/paccolamano/go/middleware`, `github.com/paccolamano/go/middleware/bodylimit`, `github.com/paccolamano/go/middleware/cors` and `github.com/paccolamano/go/middleware/requestid`.
  2. In `TestChain_NilPanics`, add the row `{"Use", func() { middleware.New(recorder("A", &log)).Use(nil, nil) }, "middleware: nil middleware at position 1"},` between the `Append` and the `Then` rows (US1-6, FR-004).
  3. Append these three tests at the end of the file:

  ```go
  // TestChain_Use checks that valid constructor results run exactly as built
  // middleware, in the order they were added (US1 scenarios 1, 5 and 7).
  func TestChain_Use(t *testing.T) {
  	var log []string
  	outside := func() (func(http.Handler) http.Handler, error) { return recorder("T", &log), nil }
  	tests := []struct {
  		name  string
  		chain middleware.Chain
  		want  []string
  	}{
  		{
  			name:  "as New",
  			chain: middleware.New().Use(recorder("A", &log), nil).Use(recorder("B", &log), nil),
  			want:  []string{"A>", "B>", "H", "<B", "<A"},
  		},
  		{
  			name: "mixed with Append",
  			chain: middleware.New().
  				Use(recorder("A", &log), nil).
  				Append(recorder("B", &log)).
  				Use(recorder("C", &log), nil).
  				Append(recorder("D", &log)),
  			want: []string{"A>", "B>", "C>", "D>", "H", "<D", "<C", "<B", "<A"},
  		},
  		{
  			name:  "constructor outside the module",
  			chain: middleware.Chain{}.Use(outside()),
  			want:  []string{"T>", "H", "<T"},
  		},
  	}
  	for _, tt := range tests {
  		t.Run(tt.name, func(t *testing.T) {
  			if err := tt.chain.Err(); err != nil {
  				t.Fatalf("Err() = %v, want nil", err)
  			}
  			log = nil
  			serve(t, tt.chain.Then(handler(&log)))
  			if !slices.Equal(log, tt.want) {
  				t.Errorf("order = %v, want %v", log, tt.want)
  			}
  		})
  	}
  }

  // TestChain_Err checks which errors a chain reports, in which order, and that
  // they stay identifiable (US1 scenarios 2 and 4; FR-006 to FR-008, FR-010).
  func TestChain_Err(t *testing.T) {
  	var log []string
  	errA, errB, errC := errors.New("a failed"), errors.New("b failed"), errors.New("c failed")
  	errConfig := fmt.Errorf("%w: test: setting: invalid", middleware.ErrInvalidConfig)
  	base := middleware.New().Use(nil, errA)
  	public := base.Use(nil, errB).Append(recorder("A", &log))
  	authenticated := public.Append(recorder("B", &log)).Use(nil, errC)

  	tests := []struct {
  		name  string
  		chain middleware.Chain
  		want  []error
  	}{
  		{"zero chain", middleware.Chain{}, nil},
  		{"New and Append", middleware.New(recorder("A", &log)).Append(recorder("B", &log)), nil},
  		{"one error", base, []error{errA}},
  		{"three levels", authenticated, []error{errA, errB, errC}},
  		{"both results present", middleware.New().Use(recorder("A", &log), errA), []error{errA}},
  		{"invalid configuration", middleware.New().Use(nil, errConfig), []error{errConfig}},
  	}
  	for _, tt := range tests {
  		t.Run(tt.name, func(t *testing.T) {
  			err := tt.chain.Err()
  			if tt.want == nil {
  				if err != nil {
  					t.Errorf("Err() = %v, want nil", err)
  				}
  				return
  			}
  			msgs := make([]string, len(tt.want))
  			for i, w := range tt.want {
  				msgs[i] = w.Error()
  				if !errors.Is(err, w) {
  					t.Errorf("errors.Is(Err(), %q) = false", w)
  				}
  			}
  			if want := strings.Join(msgs, "\n"); err == nil || err.Error() != want {
  				t.Errorf("Err() = %q, want %q", err, want)
  			}
  			if got, want := errors.Is(err, middleware.ErrInvalidConfig), slices.Contains(tt.want, errConfig); got != want {
  				t.Errorf("errors.Is(Err(), ErrInvalidConfig) = %v, want %v", got, want)
  			}
  			if again := tt.chain.Err(); again == nil || again.Error() != err.Error() {
  				t.Errorf("second Err() = %v, want %v", again, err)
  			}
  		})
  	}
  }

  // TestChain_UseWithConstructors reproduces the prototype of the request with
  // the module's constructors: an invalid CORS origin and a body limit of 0
  // (US1 scenarios 2 and 3, US3 scenario 1).
  func TestChain_UseWithConstructors(t *testing.T) {
  	var log []string
  	origins := []string{"not an origin"}
  	base := middleware.New().
  		Use(requestid.New()).
  		Append(recorder("span", &log))
  	public := base.
  		Use(cors.New(origins, cors.WithCredentials())).
  		Use(bodylimit.New(0))

  	_, corsErr := cors.New(origins, cors.WithCredentials())
  	_, limitErr := bodylimit.New(0)
  	err := public.Err()
  	if want := corsErr.Error() + "\n" + limitErr.Error(); err == nil || err.Error() != want {
  		t.Errorf("public.Err() = %q, want %q", err, want)
  	}
  	if !errors.Is(err, middleware.ErrInvalidConfig) {
  		t.Errorf("errors.Is(public.Err(), ErrInvalidConfig) = false")
  	}
  	if err := base.Err(); err != nil {
  		t.Fatalf("base.Err() = %v, want nil", err)
  	}
  	serve(t, base.Then(handler(&log)))
  	if want := []string{"span>", "H", "<span"}; !slices.Equal(log, want) {
  		t.Errorf("order = %v, want %v", log, want)
  	}
  }
  ```

- [X] T005 [P] [US1] Create `middleware/chain_internal_test.go`, package `middleware` (FR-003, US1-2, edge case "both results present"):

  ```go
  package middleware

  import (
  	"errors"
  	"net/http"
  	"testing"
  )

  // TestChain_UseErrorSkipsMiddleware checks that a constructor result with an
  // error adds no middleware, even when the constructor also returned one, and
  // leaves the receiver unchanged (FR-003, US1 scenario 2).
  func TestChain_UseErrorSkipsMiddleware(t *testing.T) {
  	identity := func(h http.Handler) http.Handler { return h }
  	errA := errors.New("a failed")
  	base := New(identity)
  	for name, mw := range map[string]Middleware{"no middleware": nil, "a middleware too": identity} {
  		t.Run(name, func(t *testing.T) {
  			c := base.Use(mw, errA)
  			if len(c.mws) != 1 {
  				t.Errorf("len(mws) = %d, want 1: the middleware was added", len(c.mws))
  			}
  			if len(c.errs) != 1 || !errors.Is(c.errs[0], errA) {
  				t.Errorf("errs = %v, want [%v]", c.errs, errA)
  			}
  			if len(base.mws) != 1 || len(base.errs) != 0 {
  				t.Errorf("Use modified the receiver: mws %d, errs %v", len(base.mws), base.errs)
  			}
  		})
  	}
  }
  ```

- [X] T006 [P] [US1] In `middleware/example_test.go`, add the imports `errors`, `github.com/paccolamano/go/middleware/bodylimit`, `github.com/paccolamano/go/middleware/cors` and `github.com/paccolamano/go/middleware/requestid`, and append `ExampleChain_Use` and `ExampleChain_Err` exactly as in [contracts/documentation.md § Examples](contracts/documentation.md#examples-example_testgo), with their `// Output:` blocks (FR-020, US5-3).

### Implementation for User Story 1

- [X] T007 [US1] First observe the red phase: from `middleware/`, `GOWORK=off go test .` fails to build with `Use` and `Err` undefined (quickstart §1). Then, in `middleware/chain.go`, add `errors` to the imports and these two methods, `Use` after `Append` and `Err` after `Use`, with their doc comments verbatim from [contracts/public-api.md](contracts/public-api.md#doc-comments-of-the-new-and-changed-methods) ([research R-04](research.md#r-04-use), [R-05](research.md#r-05-err)):

  ```go
  func (c Chain) Use(mw Middleware, err error) Chain {
  	if err != nil {
  		return Chain{mws: c.mws, errs: slices.Concat(c.errs, []error{err})}
  	}
  	return c.Append(mw)
  }

  func (c Chain) Err() error {
  	return errors.Join(c.errs...)
  }
  ```

  Data-model constraints, verbatim: for `c.Use(mw, err)` with `err != nil`, "`c.mws`, reused; `mw` ignored even when not nil" and "`c.errs` + `err` (new array)"; for `c.Use(nil, nil)`, the panic is "`middleware: nil middleware at position len(c.mws)`". Do not change `Then`, the `Chain` doc comment or `Extend` yet. Then `GOWORK=off go test -race -shuffle=on -run 'TestChain_|ExampleChain_' -v .` passes, and golangci-lint reports 0 issues.

**Checkpoint**: one `Err` check on the most derived chain reports every configuration error.
`make check MODULE=middleware` is green.

---

## Phase 4: User Story 2 - A forgotten check fails at startup, with its cause (Priority: P1)

**Goal**: `Then` panics on a chain with recorded errors, listing them, before it builds any
middleware (FR-011, FR-012; [research R-06](research.md#r-06-then-on-a-chain-with-recorded-errors)).

**Independent Test**: build a chain with two invalid constructor results, skip the check, and
apply it to a handler.

### Tests for User Story 2 ⚠️ (write first, see them fail)

- [X] T008 [US2] In `middleware/chain_test.go`, add the helper below right after `serve`, and append the two tests at the end of the file. `TestChain_ThenWithErrorsPanics` must fail before T009, every subtest with `panic value = <nil>, want an error` except `nil handler`, which gets the string `"middleware: nil handler"`. `TestChain_UseSameAllocs` already passes: it guards US2-5 from now on.

  ```go
  // recovered calls f and returns the value it panicked with, or nil.
  func recovered(f func()) (v any) {
  	defer func() { v = recover() }()
  	f()
  	return nil
  }
  ```

  ```go
  // TestChain_ThenWithErrorsPanics checks that applying a chain with recorded
  // errors fails at once, with every error, and builds no middleware (US2
  // scenarios 1 to 3, SC-003, edge case "absent handler").
  func TestChain_ThenWithErrorsPanics(t *testing.T) {
  	builds := 0
  	counting := func(next http.Handler) http.Handler {
  		builds++
  		return next
  	}
  	errA := errors.New("a failed")
  	errB := fmt.Errorf("%w: test: setting: invalid", middleware.ErrInvalidConfig)
  	c := middleware.New(counting).Use(nil, errA).Use(nil, errB)
  	want := "middleware: chain has configuration errors:\n" + errA.Error() + "\n" + errB.Error()

  	check := func(t *testing.T, h http.Handler) {
  		t.Helper()
  		v := recovered(func() { c.Then(h) })
  		err, ok := v.(error)
  		if !ok {
  			t.Fatalf("panic value = %#v, want an error", v)
  		}
  		if err.Error() != want {
  			t.Errorf("panic = %q, want %q", err, want)
  		}
  		if !errors.Is(err, errA) || !errors.Is(err, middleware.ErrInvalidConfig) {
  			t.Errorf("panic value %v does not wrap the recorded errors", err)
  		}
  	}
  	t.Run("handler", func(t *testing.T) { check(t, http.NotFoundHandler()) })
  	t.Run("nil handler", func(t *testing.T) { check(t, nil) })
  	t.Run("after Err was read", func(t *testing.T) {
  		if c.Err() == nil {
  			t.Fatal("Err() = nil")
  		}
  		check(t, http.NotFoundHandler())
  	})
  	t.Run("every application", func(t *testing.T) {
  		for range 3 {
  			check(t, http.NotFoundHandler())
  		}
  	})
  	if builds != 0 {
  		t.Errorf("middleware built %d times, want 0", builds)
  	}
  }

  // TestChain_UseSameAllocs checks that a request through a chain built with
  // Use allocates as much as through the same chain built with New (US2
  // scenario 5, SC-005).
  func TestChain_UseSameAllocs(t *testing.T) {
  	origins := []string{"https://app.example.com"}
  	withNew := middleware.New(must(t)(requestid.New()), must(t)(bodylimit.New(1<<20)), must(t)(cors.New(origins)))
  	withUse := middleware.New().Use(requestid.New()).Use(bodylimit.New(1 << 20)).Use(cors.New(origins))
  	allocs := func(c middleware.Chain) float64 {
  		h := c.Then(http.NotFoundHandler())
  		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil)
  		req.Header.Set("Origin", origins[0])
  		return testing.AllocsPerRun(100, func() { h.ServeHTTP(httptest.NewRecorder(), req) })
  	}
  	if n, u := allocs(withNew), allocs(withUse); n != u {
  		t.Errorf("allocations per request: New %v, Use %v", n, u)
  	}
  }
  ```

### Implementation for User Story 2

- [X] T009 [US2] In `middleware/chain.go`, make `Then` check the recorded errors first, before the nil-handler check, and replace its doc comment with the one of [contracts/public-api.md](contracts/public-api.md#doc-comments-of-the-new-and-changed-methods) ("It panics if c has recorded errors (see [Chain.Err]), listing them, and if h is nil."). The first statement of `Then` becomes:

  ```go
  	if len(c.errs) > 0 {
  		panic(fmt.Errorf("middleware: chain has configuration errors:\n%w", errors.Join(c.errs...))) //nolint:forbidigo // programming error: Err was not checked (spec 014 research R-06)
  	}
  ```

  Keep the nil-handler panic and `checkNil` unchanged, with their `string` values (FR-016). Then `GOWORK=off go test -race -shuffle=on -run 'TestChain_' -v .` passes, and golangci-lint reports 0 issues.

**Checkpoint**: an unchecked chain fails when the routes are registered, with every error in the
message. User Stories 1 and 2 together are the MVP.

---

## Phase 5: User Story 3 - Route groups keep their errors apart (Priority: P2)

**Goal**: chains derived from one base never see each other's errors or middleware (FR-009,
FR-015; [research R-03](research.md#r-03-storing-the-recorded-errors)).

**Independent Test**: derive two chains from one base, add invalid constructor results to one of
them, then read the errors of the base and of the sibling, and apply both.

### Tests for User Story 3 ⚠️ (guard test, checked against a deliberate bug)

- [X] T010 [US3] In `middleware/chain_test.go`, append the test below. It passes at once, because T007 copies with `slices.Concat`. Prove that it guards the invariant: in `middleware/chain.go`, temporarily replace `errs: slices.Concat(c.errs, []error{err})` in `Use` with `errs: append(c.errs, err)`, run `GOWORK=off go test -run TestChain_ErrIsolation .`, and confirm that it fails with `sibling a: Err() = "e1\ne2\ne3\nb failed", want "e1\ne2\ne3\na failed"`. Restore the line, check that `grep -n 'append(' middleware/chain.go` prints nothing (data-model invariant 3), and rerun: it passes.

  ```go
  // TestChain_ErrIsolation checks that chains derived from one base keep their
  // errors and their middleware apart (US3). The base records three errors: if
  // Use appended to the base's slice instead of copying it, both siblings would
  // write into its spare capacity, and one would report the other's error.
  func TestChain_ErrIsolation(t *testing.T) {
  	var log []string
  	base := middleware.New(recorder("base", &log)).
  		Use(nil, errors.New("e1")).
  		Use(nil, errors.New("e2")).
  		Use(nil, errors.New("e3"))
  	a := base.Use(recorder("a", &log), nil).Use(nil, errors.New("a failed"))
  	b := base.Use(nil, errors.New("b failed")).Append(recorder("b", &log))
  	for name, tt := range map[string]struct {
  		chain middleware.Chain
  		want  string
  	}{
  		"base":      {base, "e1\ne2\ne3"},
  		"sibling a": {a, "e1\ne2\ne3\na failed"},
  		"sibling b": {b, "e1\ne2\ne3\nb failed"},
  	} {
  		if err := tt.chain.Err(); err == nil || err.Error() != tt.want {
  			t.Errorf("%s: Err() = %q, want %q", name, err, tt.want)
  		}
  	}

  	clean := middleware.New(recorder("base", &log))
  	broken := clean.Use(nil, errors.New("x failed"))
  	x := clean.Append(recorder("x", &log))
  	y := clean.Use(recorder("y", &log), nil)
  	if broken.Err() == nil {
  		t.Fatal("broken.Err() = nil")
  	}
  	for name, tt := range map[string]struct {
  		chain middleware.Chain
  		want  []string
  	}{
  		"clean base": {clean, []string{"base>", "H", "<base"}},
  		"sibling x":  {x, []string{"base>", "x>", "H", "<x", "<base"}},
  		"sibling y":  {y, []string{"base>", "y>", "H", "<y", "<base"}},
  	} {
  		if err := tt.chain.Err(); err != nil {
  			t.Errorf("%s: Err() = %v, want nil", name, err)
  		}
  		log = nil
  		serve(t, tt.chain.Then(handler(&log)))
  		if !slices.Equal(log, tt.want) {
  			t.Errorf("%s: order = %v, want %v", name, log, tt.want)
  		}
  	}
  }
  ```

**Checkpoint**: a base shared by route groups stays usable whatever its derived chains record.

---

## Phase 6: User Story 4 - Appending a chain, and the upgrade from 0.1.0 (Priority: P2)

**Goal**: `Extend` becomes `AppendChain`, with the same order, and carries the other chain's
errors. The CHANGELOG records the release (FR-013 to FR-015, FR-021;
[research R-07](research.md#r-07-appendchain-formerly-extend)).

**Independent Test**: append chains with and without recorded errors and compare the order with
0.1.0. Build code that uses the former name against this release.

### Tests for User Story 4 ⚠️ (write first, see them fail)

- [X] T011 [P] [US4] In `middleware/chain_test.go`, replace the whole `TestChain_Extend` function with:

  ```go
  // TestChain_AppendChain checks that the middleware of the receiver stay
  // outermost, that both chains are unchanged, and that the errors of the
  // appended chain are carried (US4 scenarios 1 to 3).
  func TestChain_AppendChain(t *testing.T) {
  	var log []string
  	first := middleware.New(recorder("A", &log))
  	second := middleware.New(recorder("B", &log), recorder("C", &log))
  	serve(t, first.AppendChain(second).Then(handler(&log)))
  	want := []string{"A>", "B>", "C>", "H", "<C", "<B", "<A"}
  	if !slices.Equal(log, want) {
  		t.Errorf("order = %v, want %v", log, want)
  	}
  	log = nil
  	serve(t, first.Then(handler(&log)))
  	if !slices.Equal(log, []string{"A>", "H", "<A"}) {
  		t.Errorf("AppendChain modified the receiver: %v", log)
  	}
  	log = nil
  	serve(t, second.Then(handler(&log)))
  	if !slices.Equal(log, []string{"B>", "C>", "H", "<C", "<B"}) {
  		t.Errorf("AppendChain modified the other chain: %v", log)
  	}

  	t.Run("empty chain", func(t *testing.T) {
  		for name, c := range map[string]middleware.Chain{
  			"appended":    first.AppendChain(middleware.Chain{}),
  			"as receiver": middleware.Chain{}.AppendChain(first),
  		} {
  			log = nil
  			serve(t, c.Then(handler(&log)))
  			if !slices.Equal(log, []string{"A>", "H", "<A"}) {
  				t.Errorf("%s: order = %v", name, log)
  			}
  			if err := c.Err(); err != nil {
  				t.Errorf("%s: Err() = %v, want nil", name, err)
  			}
  		}
  	})

  	t.Run("errors", func(t *testing.T) {
  		withA := middleware.New().Use(nil, errors.New("a failed"))
  		withB := middleware.New().Use(nil, errors.New("b failed"))
  		if got := withA.AppendChain(withB).Err(); got == nil || got.Error() != "a failed\nb failed" {
  			t.Errorf("Err() = %v, want the receiver's error, then the other's", got)
  		}
  		if withA.Err().Error() != "a failed" || withB.Err().Error() != "b failed" {
  			t.Errorf("AppendChain modified a chain: %v, %v", withA.Err(), withB.Err())
  		}
  		if _, ok := recovered(func() { first.AppendChain(withB).Then(handler(&log)) }).(error); !ok {
  			t.Error("Then did not panic on a chain with appended errors")
  		}
  	})

  	t.Run("self-append keeps duplicates", func(t *testing.T) {
  		self := first.Use(nil, errors.New("a failed"))
  		if got := self.AppendChain(self).Err(); got == nil || got.Error() != "a failed\na failed" {
  			t.Errorf("Err() = %v, want the error twice", got)
  		}
  		log = nil
  		serve(t, first.AppendChain(first).Then(handler(&log)))
  		if want := []string{"A>", "A>", "H", "<A", "<A"}; !slices.Equal(log, want) {
  			t.Errorf("order = %v, want %v", log, want)
  		}
  	})
  }
  ```

- [X] T012 [P] [US4] In `middleware/example_test.go`, append `ExampleChain_AppendChain` exactly as in [contracts/documentation.md § Examples](contracts/documentation.md#examples-example_testgo), with its `// Output:` block. It adds a constructor result, reads the errors and appends a chain (FR-020).

### Implementation for User Story 4

- [X] T013 [US4] Replace `Extend` with `AppendChain` in `middleware/chain.go`, and update `middleware/doc.go`. First observe the red phase: `GOWORK=off go test .` fails to build with `AppendChain` undefined. Then:
  1. In `middleware/chain.go`, delete `Extend` and its doc comment, and add, after `Use`, `AppendChain` with its doc comment verbatim from [contracts/public-api.md](contracts/public-api.md#doc-comments-of-the-new-and-changed-methods):

     ```go
     func (c Chain) AppendChain(other Chain) Chain {
     	return Chain{mws: slices.Concat(c.mws, other.mws), errs: slices.Concat(c.errs, other.errs)}
     }
     ```

     No nil check on `other` (research R-07). Data-model constraint, verbatim: "`c.mws` + `other.mws` (new array)" and "`c.errs` + `other.errs` (new array)".
  2. In `middleware/chain.go`, replace the doc comment of `Chain` with the one of [contracts/public-api.md § Chain at 0.2.0](contracts/public-api.md#chain-at-020), from "Chain is an immutable, ordered list of middleware." to "panics on a chain with recorded errors.". Keep the field comment of T003.
  3. In `middleware/doc.go`, replace the sentence "Chains are immutable: [Chain.Append] and [Chain.Extend] return new chains, so a base chain can be shared and specialized for different route groups." with the overview text of [contracts/documentation.md § doc.go](contracts/documentation.md#docgo).

  Then `GOWORK=off go test -race -shuffle=on .` passes, golangci-lint reports 0 issues, and `grep -rn 'Extend' --include='*.go' middleware/` prints nothing.
- [X] T014 [P] [US4] In `middleware/CHANGELOG.md`, under `## [Unreleased]`, add the entry of [contracts/public-api.md § CHANGELOG entry](contracts/public-api.md#changelog-entry) verbatim: `### Removed` first, with the **Breaking** removal of `Chain.Extend` and the migration "Replace `Extend` with `AppendChain`", then `### Added`, with `Chain.Use`, `Chain.Err`, `Chain.AppendChain` and the panic of `Chain.Then` (FR-021). Leave a blank line between `## [Unreleased]` and the first heading, and before `## [0.1.0]`. Do not touch the 0.1.0 entry.

**Checkpoint**: the API is complete. Code that uses `Extend` fails to build, and the CHANGELOG
says what replaces it.

---

## Phase 7: User Story 5 - The documentation shows the short way (Priority: P3)

**Goal**: the package documentation, the README and the recommended example build chains with
`Use` and check `Err` once (FR-018 to FR-020; [research R-12](research.md#r-12-documentation-fr-018-to-fr-020)).

**Independent Test**: read the README and the package documentation, and run the module's
examples as tests.

- [X] T015 [P] [US5] In `middleware/doc.go`, in the `# Conventions` section, insert the paragraph and code block of [contracts/documentation.md § doc.go](contracts/documentation.md#docgo) ("[Chain.Use] takes the two results of a constructor directly …" through "… such as those of other libraries.") right after the paragraph that ends "before any request is served.", followed by an empty `//` line, before the paragraph about `WithRejectFunc`. The code block lines start with `//` and a tab. Run `gofmt -l middleware/` (prints nothing) and `GOWORK=off go doc .` from `middleware/`: the code block renders indented.
- [X] T016 [P] [US5] In `middleware/README.md`, under `## Usage`, replace the Go code block (from `logger := slog.Default()` through `mux.Handle("/", base.Then(appHandler))`) with the **Usage** block of [contracts/documentation.md § README.md](contracts/documentation.md#readmemd), and replace the paragraph that starts "`New`, `Append` and `Extend` build immutable chains" with the paragraph given there, wrapped at about 88 columns like the rest of the file. Nothing else in the README changes.
- [X] T017 [P] [US5] In `middleware/example_recommended_test.go`, replace `recommendedChain` and its doc comment with the version below, and in `ExampleNew_recommended` replace the three lines `chain, err := recommendedChain(logger)` / `if err != nil {` / `panic(err)` with `chain := recommendedChain(logger)` / `if err := chain.Err(); err != nil {` / `panic(err)`. The `// Output:` block does not change. The `errors` import stays, because `jsonReject` uses it.

  ```go
  // recommendedChain builds every middleware of the module in the recommended
  // order, with the same refusal format everywhere. The caller checks its Err.
  func recommendedChain(logger *slog.Logger) middleware.Chain {
  	return middleware.New().
  		Use(realip.New(realip.TrustRanges("10.0.0.0/8"))).
  		Use(requestid.New()).
  		Use(accesslog.New(logger)).
  		Use(recovery.New(logger, recovery.WithRejectFunc(jsonReject))).
  		Use(secureheaders.New()).
  		Use(cors.New([]string{"https://app.example.com"})).
  		Use(ratelimit.New(100, time.Minute, ratelimit.WithRejectFunc(jsonReject))).
  		Use(maxconcurrency.New(256, maxconcurrency.WithRejectFunc(jsonReject))).
  		Use(bodylimit.New(1<<20, bodylimit.WithRejectFunc(jsonReject))).
  		Use(decompress.New(decompress.WithRejectFunc(jsonReject))).
  		Use(contenttype.New([]string{"application/json"}, contenttype.WithRejectFunc(jsonReject))).
  		Use(compress.New()).
  		Use(etag.New())
  }
  ```

  `GOWORK=off go test -run ExampleNew_recommended -v .` passes.
- [X] T018 [US5] Review `middleware/README.md`, `middleware/doc.go`, `middleware/CHANGELOG.md` and the examples against US5 and SC-001:
  - `grep -rn 'Extend' middleware/` prints only lines of `middleware/CHANGELOG.md`: the 0.1.0 entry and the new **Removed** entry (US5-4, FR-020).
  - From `middleware/`, `GOWORK=off go doc -all . | grep -n -A12 'func (c Chain) Use'` shows the panic of `Use`, and the same for `Then` shows "It panics if c has recorded errors" (FR-019).
  - The README usage block has exactly one `Err()` check and no `if err != nil` per constructor (US5-1, SC-001).
  - The `doc.go` overview names `Append`, `Use` and `AppendChain`, and its Conventions section shows `Use`, one `Err` check and the panic of `Then` (US5-2).
  - `GOWORK=off go test -run 'Example' -v .` passes all seven examples (US5-3).

**Checkpoint**: every story is complete.

---

## Phase 8: Polish & Cross-Cutting Concerns

**Purpose**: measure the cost, check the whole change against the contracts and the quickstart,
and run the gates.

- [X] T019 [P] In `middleware/bench_test.go`, append `BenchmarkChain_Use` (SC-005; [research R-11](research.md#r-11-cost-fr-012-sc-005)). The file already imports `bodylimit`, `cors`, `requestid`, `httptest` and `middleware`.

  ```go
  // BenchmarkChain_Use compares a request through the same middleware built with
  // New and with Use (SC-005: the same allocations, and at most 5% more time).
  func BenchmarkChain_Use(b *testing.B) {
  	origins := []string{"https://app.example.com"}
  	chains := []struct {
  		name  string
  		chain middleware.Chain
  	}{
  		{"New", middleware.New(must(b)(requestid.New()), must(b)(bodylimit.New(1<<20)), must(b)(cors.New(origins)))},
  		{"Use", middleware.New().Use(requestid.New()).Use(bodylimit.New(1 << 20)).Use(cors.New(origins))},
  	}
  	for _, c := range chains {
  		b.Run(c.name, func(b *testing.B) {
  			h := c.chain.Then(http.NotFoundHandler())
  			req := httptest.NewRequestWithContext(b.Context(), http.MethodGet, "/", nil)
  			req.Header.Set("Origin", origins[0])
  			b.ReportAllocs()
  			for b.Loop() {
  				h.ServeHTTP(httptest.NewRecorder(), req)
  			}
  		})
  	}
  }
  ```

  Run `GOWORK=off go test -run '^$' -bench 'BenchmarkChain_Use' -count 10 -cpu 1 .` (quickstart §8): both sub-benchmarks report the same `B/op` and `allocs/op`, and their medians differ by at most 5% (the spike measured 1473.5 and 1474 ns/op). Record the medians for the pull request.
- [X] T020 [P] Check the coverage of the spec (SC-007): from `middleware/`, run `GOWORK=off go test -race -shuffle=on -v . 2>&1 | grep -E '^(=== RUN|--- (PASS|FAIL))'` and confirm that every test of the [coverage map](#coverage-map-sc-007) runs and passes. Then `GOWORK=off go test -coverprofile=c.out ./... && go tool cover -func=c.out | grep -E 'chain.go|^total'`: every function of `chain.go` is at 100%, and the total is at least 80%. Delete `c.out`.
- [X] T021 [P] From `middleware/`, check the dependencies and the platforms (FR-017): `GOWORK=off go mod tidy -diff` prints nothing; `git diff --exit-code -- go.mod` passes (still `go 1.26.0`, no `require`); for every `os` in linux, darwin and windows, and every `arch` in amd64 and arm64, `GOWORK=off GOOS=$os GOARCH=$arch go vet .` passes.
- [X] T022 [P] Run [quickstart.md](quickstart.md) §5 and §6 inside `middleware/`, with the programs given there, in the scratch directories `middleware/zz_panic/` and `middleware/zz_cfail/`, and delete both directories afterwards (`rm -r`). §5: the program exits with status 2 and prints the three lines of the expected panic. §6: the build fails with the three compile errors of [research R-08](research.md#r-08-telling-use-and-append-apart-fr-005) (US4-4, FR-005). `git status --short middleware/` must not list either directory afterwards.
- [X] T023 [P] Check the API compatibility and the changelog gate (SC-006). `gorelease` and `scripts/changelog-check.sh` need commits, and `gorelease` refuses a working tree with uncommitted changes. In the session scratchpad, not in the repository: clone the repository with `git clone -q --no-hardlinks <repository root> <scratchpad>/repo`, replace its `middleware/` with a copy of the working tree's `middleware/`, and commit there only. If `gorelease` is not installed, install it there with `GOBIN=<scratchpad>/tools go install golang.org/x/exp/cmd/gorelease@<GORELEASE_VERSION of versions.env>`; when the network is unavailable, add `GOPROXY=file://$(go env GOMODCACHE)/cache/download GOSUMDB=off GOFLAGS=-mod=mod` (the spike installed it this way). In the clone, run `GORELEASE=<path to gorelease> scripts/apicompat.sh middleware`: the report equals [contracts/public-api.md § gorelease report](contracts/public-api.md#gorelease-report-against-middlewarev010), with `Suggested version: v0.2.0`, and the script exits 0 at 0.x. Then run `scripts/changelog-check.sh origin/main HEAD` in the clone: it passes. If the clone cannot be made, skip this task and note that the pull request workflow runs both checks.
- [X] T024 Run `make check MODULE=middleware` from the repository root: golangci-lint reports zero findings, and `go test -race -shuffle=on` passes with coverage of at least 80%; compare it with the baseline of T002. Compare `GOWORK=off go test -list '.*' .` with the list of T002: every former test is still there, except `TestChain_Extend`, now `TestChain_AppendChain` (SC-004). `grep -n 'nolint' middleware/chain.go` prints exactly three `forbidigo` lines: the two of 0.1.0 and the one of T009.
- [X] T025 Draft the pull request for the changes in `middleware/` and `specs/014-middleware-chain-use/`, for the maintainer, and do not open it without their go-ahead:
  - **Title**: `feat(middleware)!: build a chain from the constructors' results, and rename Extend to AppendChain`.
  - **Description**:
    - the problem: one `if err != nil` per constructor, 8 in the first user's route setup, each wrapping an error that already names the package and the setting; and the module's own `recommendedChain`, whose helper panicked instead of returning the error;
    - what changes: `Use`, `Err`, the panic of `Then` on recorded errors, and `Extend` renamed `AppendChain`, which carries errors;
    - the breaking change and its migration, as in the CHANGELOG, and the `gorelease` report of T023;
    - the coverage of T024 and the benchmark medians of T019;
    - the release plan: `middleware/v0.2.0`, in a separate `chore(middleware): prepare release 0.2.0` pull request;
    - the attribution line required for pull request descriptions.

---

## Coverage map (SC-007)

| Scenario or criterion | Test |
|-----------------------|------|
| US1-1 | `TestChain_Use/as_New` |
| US1-2 | `TestChain_UseWithConstructors`, `TestChain_Err/invalid_configuration`, `TestChain_UseErrorSkipsMiddleware` |
| US1-3 | `TestChain_UseWithConstructors` |
| US1-4 | `TestChain_Err/three_levels` |
| US1-5 | `TestChain_Use/mixed_with_Append` |
| US1-6 | `TestChain_NilPanics/Use` |
| US1-7, FR-006 | `TestChain_Use/constructor_outside_the_module`, `TestChain_Err/one_error` |
| US2-1 | `TestChain_ThenWithErrorsPanics/handler` |
| US2-2 | `TestChain_ThenWithErrorsPanics/after_Err_was_read` |
| US2-3 | `TestChain_ThenWithErrorsPanics/every_application` |
| US2-4 | the 0.1.0 tests, unchanged: `TestChain_Order`, `TestChain_Empty`, `TestChain_AppendDoesNotAlias`, `TestChain_NewCopiesInput`, `TestChain_ThenBuildsOnce`, `TestChain_NilPanics/Then`, `TestChain_ThirdPartyFuncs` |
| US2-5, SC-005 | `TestChain_UseSameAllocs`, `BenchmarkChain_Use` (T019) |
| US3-1 | `TestChain_UseWithConstructors`, `TestChain_ErrIsolation` (clean base) |
| US3-2 | `TestChain_ErrIsolation` (siblings x and y) |
| US3-3 | `TestChain_ErrIsolation` (siblings a and b) |
| US3-4 | `TestChain_ErrIsolation`, `TestChain_AppendDoesNotAlias` |
| US4-1, US4-3 | `TestChain_AppendChain` |
| US4-2 | `TestChain_AppendChain/errors` |
| US4-4, FR-005 | quickstart §6 (T022), `gorelease` (T023) |
| US4-5, SC-004 | the test list of T024 against T002 |
| US5-1 to US5-4 | the documentation review of T018; `ExampleChain_Use`, `ExampleChain_Err`, `ExampleChain_AppendChain`, `ExampleNew_recommended` |
| Edge "both results present" | `TestChain_Err/both_results_present`, `TestChain_UseErrorSkipsMiddleware/a_middleware_too` |
| Edge "neither result" | `TestChain_NilPanics/Use` |
| Edge "the empty chain" | `TestChain_Err/zero_chain`, `TestChain_Use/constructor_outside_the_module` |
| Edge "built only by building and appending" | `TestChain_Err/New_and_Append` |
| Edge "an error that is not an invalid configuration" | `TestChain_Err/one_error` |
| Edge "reading the errors several times" | `TestChain_Err` (second `Err`) |
| Edge "a chain that appends itself" | `TestChain_AppendChain/self-append_keeps_duplicates` |
| Edge "appending an empty chain" | `TestChain_AppendChain/empty_chain` |
| Edge "recorded errors and an absent handler" | `TestChain_ThenWithErrorsPanics/nil_handler` |
| Edge "many recorded errors" | `TestChain_Err/three_levels`, `TestChain_ErrIsolation` (four errors) |
| SC-001 | T018 (README), `ExampleChain_Use` |
| SC-002 | `TestChain_Err`, `TestChain_ErrIsolation`, `TestChain_AppendChain/errors` |
| SC-003 | `TestChain_ThenWithErrorsPanics` (no middleware built) |
| SC-006 | T023 |
| SC-007 | T020 |

---

## Dependencies & Execution Order

### Phase dependencies

- **Setup (Phase 1)** then **Foundational (Phase 2)**: T001 → T002 → T003.
- **US1 (Phase 3)** depends on Phase 2: `Use` writes the `errs` field.
- **US2 (Phase 4)** depends on US1: its tests record errors with `Use`.
- **US3 (Phase 5)** depends on US1 only: it applies only chains without recorded errors. It
  comes after US2 because both edit `chain_test.go`.
- **US4 (Phase 6)** depends on US2: `TestChain_AppendChain` uses `recovered` (T008) and
  expects the panic of `Then` (T009).
- **US5 (Phase 7)** depends on US1–US4: the documentation describes the final API.
- **Polish (Phase 8)** depends on every story.

The recommended order is the phase order. Each checkpoint leaves `make check MODULE=middleware`
green.

### Within each phase

- Test tasks come first, and are observed failing: T004–T006 before T007, T008 before T009,
  T011–T012 before T013. T010 is a guard test, checked against a deliberate bug.
- **Shared files**: tasks on the same file never run in parallel.

  | File | Edited by |
  |------|-----------|
  | `middleware/chain.go` | T003, T007, T009, T010 (temporary, restored), T013 |
  | `middleware/chain_test.go` | T004, T008, T010, T011 |
  | `middleware/chain_internal_test.go` | T005 |
  | `middleware/example_test.go` | T006, T012 |
  | `middleware/doc.go` | T013, T015 |
  | `middleware/CHANGELOG.md` | T014 |
  | `middleware/README.md` | T016 |
  | `middleware/example_recommended_test.go` | T017 |
  | `middleware/bench_test.go` | T019 |

### Parallel opportunities

- **US1**: T004, T005 and T006 together, three files. Then T007.
- **US2**: T008, then T009.
- **US3**: T010 alone.
- **US4**: T011 and T012 together. Then T013, with T014 in parallel with it.
- **US5**: T015, T016 and T017 together. Then T018.
- **Polish**: T019 to T023 together. Then T024 and T025.

---

## Parallel Example: User Story 1

```bash
# The three test tasks touch three files:
Task: "T004 Add TestChain_Use, TestChain_Err, TestChain_UseWithConstructors and the Use row of TestChain_NilPanics in middleware/chain_test.go"
Task: "T005 Create TestChain_UseErrorSkipsMiddleware in middleware/chain_internal_test.go"
Task: "T006 Add ExampleChain_Use and ExampleChain_Err in middleware/example_test.go"
# Then, alone:
Task: "T007 Add Use and Err in middleware/chain.go"
```

## Parallel Example: User Story 5

```bash
Task: "T015 Add the Use and Err paragraph to the Conventions section of middleware/doc.go"
Task: "T016 Rewrite the Usage block and the chain paragraph of middleware/README.md"
Task: "T017 Build recommendedChain with Use in middleware/example_recommended_test.go"
# Then, alone:
Task: "T018 Review the documentation"
```

## Parallel Example: Polish

```bash
Task: "T019 Add BenchmarkChain_Use in middleware/bench_test.go and compare the medians"
Task: "T020 Check the coverage map and chain.go coverage"
Task: "T021 Check go mod tidy, go.mod and the six platforms"
Task: "T022 Run quickstart §5 and §6 in throwaway directories"
Task: "T023 Run apicompat.sh and changelog-check.sh in a scratch clone"
```

---

## Implementation Strategy

### MVP first (User Stories 1 and 2)

1. Phase 1 and Phase 2: branch, baseline, the `errs` field.
2. Phase 3 (US1): `Use` and `Err`. **Validate**: one check reports every error.
3. Phase 4 (US2): the panic of `Then`. **Validate**: an unchecked chain fails at startup.

US1 alone already removes the error checks. With US2, a forgotten check can no longer remove a
middleware silently, so the two together are the smallest safe release.

### Incremental delivery

1. US3 adds the isolation guard, with no code change.
2. US4 renames `Extend` and writes the CHANGELOG: from here the API matches the `gorelease`
   report.
3. US5 updates the documentation and the recommended example.
4. Polish measures, checks and drafts the pull request. The release is a separate pull request.

---

## Notes

- [P] tasks touch different files and depend on no incomplete task.
- Each story ends at a checkpoint where `make check MODULE=middleware` is green.
- Verify that each test fails before implementing it, except the guard tests named above.
- Commit only when the maintainer asks.

# Implementation Plan: Service lifecycle (lifecycle)

**Branch**: `009-service-lifecycle` (suggested; work currently on `sse-release-0.1.0`, already merged into `main`) | **Date**: 2026-10-22 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `specs/009-service-lifecycle/spec.md`

**Note**: This template is filled in by the `/speckit-plan` command; its definition describes the execution workflow.

## Summary

This feature adds a new independent module, `github.com/paccolamano/go/lifecycle`, with two
packages:

- **`lifecycle`**: an `App` that launches registered components in order and stops them one at a
  time in reverse order.
  - **Components**: `Component` (`Run(ctx)`), the optional `Stopper` (`Stop(ctx)`), and `Func`.
  - **Adapters**: `HTTPServer` (with `WithListener`), `Closer` (three close shapes, generic) and
    `StartStop`.
  - **Stop contract**: `Stop` means "stop accepting work". The App waits for `Run` and `Stop`,
    and cancels the run's context only at the deadline.
  - **Shutdown**: triggers from the context, opt-in signals, or a component that returns early.
    A drain delay (0 by default) during which `Ready` fails, a stop timeout (15 s by default)
    that bounds `Run`, and a second signal that forces the return.
  - **Errors**: six sentinels, `*ComponentError` and `*PanicError`, joined in a defined order.
- **`lifecycle/health`**: a probe handler built from named checks, which imports nothing from
  `lifecycle`.
  - Concurrent requests share one evaluation, and a stuck check is never started twice.
  - Kubernetes-style text responses, which never carry a failure's reason: the logger receives
    it.
- **Documentation**: the model, a compatibility table for the common systems, the components to
  write yourself (RabbitMQ, gRPC, NATS), blocking functions, Kubernetes, and runnable examples.

Design decisions validated by a spike ([research.md](research.md)):

- **Stop contract** (R-07): over an in-memory `net.Pipe` inside `synctest`, a request in progress
  of 3 s completed during the server's turn, its shutdown hook ran, and the worker registered
  before it stopped after it.
- **Bounded shutdown** (R-08): with a component that ignores cancellation, the sequence returned
  after exactly the 5 s timeout, and the remaining component still received its cancellation.
- **HTTP adapter** (R-11): `Serve` after `Shutdown` returns `ErrServerClosed` and closes the
  listener, so an early stop leaks nothing. An invalid port fails without opening a socket, which
  keeps the failure test hermetic.
- **Close adapter** (R-12): the inline type set `func() | func() error | func(context.Context)
  error` infers from the method values of the target clients.
- **Signals** (R-09): `signal.Notify` on a channel created inside a `synctest` bubble is a fatal
  error of the runtime. Tests inject the registration through unexported fields, and one
  Unix-only test uses real signals.

Three spec refinements came out of the research (checklist, Iteration 3):

- **FR-007 and SC-006**: components stopped after the deadline may finish just after `Run` has
  returned;
- **FR-046**: the documentation warns against signals in `synctest`-based tests;
- **User Story 2, scenario 9, and FR-054**: the listener test and example use an in-memory
  listener, not port 0.

## Technical Context

**Language/Version**: Go. The `go` directive is `go 1.26.0`, the oldest version the
constitution supports, and CI also tests on 1.27. Features used:

- `testing/synctest` and `sync.WaitGroup.Go` (1.25);
- `slog.DiscardHandler` (1.24);
- `context.WithoutCancel` and `context.AfterFunc` (1.21);
- `errors.Join` and `context.WithCancelCause` (1.20);
- generics with a type set of function types (1.18).

**Primary Dependencies**: the standard library only, with no third-party module and no other
module of this repository, tests and examples included. `health` does not import `lifecycle`
([research.md R-02](research.md#r-02-dependencies-principle-v)).

**Storage**: none. The App keeps its registrations and outcomes in memory. The health handler
keeps only the evaluation in progress.

**Testing**: the standard `testing` package.

- External test packages `lifecycle_test` and `health_test`, and internal tests for the signal
  hooks, the states and the error composition.
- Virtual time with `synctest` for drains, stop timeouts, check timeouts and
  `http.Server.Shutdown`.
- An in-memory `net.Pipe` listener for the HTTP adapter. No socket.
- Fake signals through internal hooks, and one Unix-only test with real `SIGUSR1`.
- Whole-file runnable examples.
- No fuzz targets and no benchmarks: no external input is parsed and no code is
  performance-sensitive ([R-19](research.md#r-19-fuzzing-and-benchmarks-principle-iii)).

Details: [research.md R-20](research.md#r-20-testing-approach-principle-iii).

**Target Platform**: any platform Go supports. The code is pure Go with no build constraint,
and builds for linux, darwin and windows on amd64 and arm64. Only the real-signal test file is
constrained to Unix.

**Project Type**: library: one Go module (`lifecycle/go.mod`) with two packages, `lifecycle` and
`lifecycle/health`.

**Performance Goals**: no code is declared performance-sensitive. The App's cost is one goroutine
per component plus one per stop call. The handler's cost is two goroutines per check per
evaluation, and at most one evaluation at a time.

**Constraints**:

- Goroutines started by the module end when the components' and checks' operations return
  (FR-007). Only operations that ignore cancellation keep them alive, and the App reports those
  components.
- No package-level mutable state: the only package-level variables are the seven `Err…`
  sentinels, and defaults are `const`.
- No `init`, no `panic`, no output. Logging only through an injected `*slog.Logger`.
- Signals are handled only with `WithSignals`, and only while `Run` runs.

**Scale/Scope**: about 700 lines of library code in `lifecycle` and 300 in `health`, in about 12
files. The API has 37 exported identifiers ([contracts/public-api.md](contracts/public-api.md)).

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

| Rule | Assessment | Status |
|------|------------|--------|
| **I. Independent modules** | One new top-level module `lifecycle/`, with module path `github.com/paccolamano/go/lifecycle`, its own `go.mod`, a README.md and a CHANGELOG.md. It has no `replace` and no dependency on other repository modules, and builds and tests with `GOWORK=off`. Its single purpose is running a service process: starting it, stopping it, and reporting its state to probes. `health` is a subpackage of that purpose (R-01). | ✅ |
| **II. SemVer & stable API** | Tags are `lifecycle/vX.Y.Z`, starting at v0.1.0. The exported surface maps to FRs ([public-api.md](contracts/public-api.md), traceability table). `New`, `HTTPServer` and `health.New` take variadic options, so options can be added compatibly. `Stopper` is optional, so `Component` keeps one method. Every change goes into the CHANGELOG. | ✅ |
| **III. Test-first** | `tasks.md` will order test tasks before implementation tasks. The suite uses the standard `testing` package with table-driven subtests. It is deterministic and hermetic: `synctest` virtualizes time, the HTTP network is an in-memory `net.Pipe`, and signals are injected, except one Unix test that signals its own process. No external input is parsed, so no fuzz target is required (R-19). Every primary API has an `Example`. Coverage target: ≥ 80%. | ✅ |
| **IV. Idiomatic Go** | The repository's golangci-lint config applies unchanged, and no `nolint` is expected (R-22). Every exported identifier and both packages are documented. Errors are wrapped with `%w`, and branchable conditions are sentinels or `*ComponentError` and `*PanicError` (R-04). `Run`, `Ready`, `Stop` and the checks take a context first. There is no `panic`, `init` or global mutable state. Logging goes through an injected `*slog.Logger`. Signal handling is opt-in, which imposes no process-level behavior by default. Every goroutine has an owner, the App or the handler, and a shutdown path, the cancellation of the operation it waits for. | ✅ |
| **V. Simplicity & dependencies** | No third-party dependency. One small consumer-side interface (`Component`) plus an optional one (`Stopper`). One generic function, `Closer`, justified by the three close shapes of the target clients (R-12). Rejected as speculative: dependency injection, restarts, parallel stop groups, a result cache in `health`, and exported default constants. No cgo. | ✅ |
| Go toolchain support | `go 1.26.0` is not above `GO_VERSION_OLDEST=1.26`, and every feature used exists in 1.26 (Technical Context). | ✅ |
| Platforms | Pure Go. `os/signal` and `syscall.SIGTERM` compile on every supported platform. The README states the Windows behavior of signals. There is no `.module-exceptions` entry. | ✅ |
| Repository layout | No tooling change. `make work` adds `./lifecycle` to `go.work`, and the shared lint config is used without overrides. | ✅ |
| Dependency hygiene | With no `require` list, `go mod tidy -diff` is clean. | ✅ |
| Vulnerability scanning / Secrets | Trivy finds no dependency to scan. Fixtures contain no credential-like literals; the health tests use host names and connection strings without secrets. A possible Semgrep match on writing to a `ResponseWriter` would be suppressed with `nosemgrep` and the `text/plain` justification (R-22). | ✅ |
| Local and PR gates | `make check MODULE=lifecycle` runs lint, then race and shuffle tests with the coverage gate. The tooling discovers the new module on its own. | ✅ |
| Commits / Release | The PR title is `feat(lifecycle): add service lifecycle module`. The release is `lifecycle/v0.1.0`, with a CHANGELOG section. | ✅ |

**Result (pre-research)**: PASS, with no deviation.

**Result (post-design re-check)**: PASS.

- The data model and the three contracts add no dependency, no global state and no module
  exception.
- Every exported identifier maps to a functional requirement.
- The goroutines left behind by components that ignore cancellation are reported, not leaked
  silently. Their cause is in the component's code, which the App cannot stop (FR-007, FR-024).

## Project Structure

### Documentation (this feature)

```text
specs/009-service-lifecycle/
├── spec.md                      # feature specification
├── plan.md                      # this file
├── research.md                  # Phase 0: decisions R-01 … R-22, with spike results
├── data-model.md                # Phase 1: configuration, registrations, states, evaluations
├── quickstart.md                # Phase 1: validation guide
├── contracts/
│   ├── public-api.md            # complete exported Go API, with FR traceability
│   ├── lifecycle-semantics.md   # launch, triggers, drain, stop contract, expired path, adapters
│   └── health-http.md           # methods, headers, bodies, evaluations, logging
├── checklists/
│   └── requirements.md          # spec quality checklist
└── tasks.md                     # Phase 2 (/speckit-tasks, not created here)
```

### Source Code (repository root)

```text
go.work                              # + ./lifecycle (make work)

lifecycle/
├── go.mod                           # module github.com/paccolamano/go/lifecycle; go 1.26.0; no require
├── README.md                        # research R-21: main, model, stop contract, bounded shutdown,
│                                    # signals and testing, compatibility, blocking functions,
│                                    # sse and slogx, health, Kubernetes, defaults
├── CHANGELOG.md                     # Keep a Changelog; [Unreleased] → [0.1.0]
├── doc.go                           # package doc: the model and the stop contract in short form
├── component.go                     # Component, Stopper, Func, the validator interface
├── options.go                       # Option, With*, ErrInvalidConfig, config, defaults
├── errors.go                        # sentinels, ComponentError, PanicError, result composition
├── app.go                           # App, New, Add, Ready, states
├── run.go                           # Run: launch, triggers, drain, stop sequence, expired path
├── signal.go                        # signal registration, counting, the unexported hooks
├── http.go                          # HTTPServer, HTTPOption, WithListener
├── closer.go                        # Closer
├── startstop.go                     # StartStop
│
├── pipenet_test.go                  # in-memory listener over net.Pipe
├── recorder_test.go                 # recording components and a log recorder
├── options_internal_test.go         # defaults, validation, error format (FR-003)
├── errors_internal_test.go          # result order and composition (FR-026)
├── signal_internal_test.go          # fake signals: trigger, count, force, restore (US1, US5)
├── signal_unix_test.go              # //go:build unix: real SIGUSR1 (R-09)
├── register_test.go                 # Add validation, Add after start, Run twice (FR-010, FR-011)
├── order_test.go                    # US1: reverse order for 1 to 50 components (SC-001)
├── contract_test.go                 # US2: Stopper, Func, deadline, stop errors (SC-004)
├── failure_test.go                  # US3: error, nil, panic, near-simultaneous failures
├── drain_test.go                    # US4: drain, Ready states (SC-002)
├── bounded_test.go                  # US5: deadline, never-returning Stop, expired path (SC-003)
├── http_test.go                     # HTTPServer: in-flight, hooks, listener, TLS choice,
│                                    # invalid port (SC-002)
├── closer_test.go                   # Closer: three shapes, once, nil
├── startstop_test.go                # StartStop: start failure, stop
├── example_test.go                  # Example, ExampleCloser, ExampleStartStop
├── example_http_test.go             # whole-file ExampleHTTPServer with an in-memory listener
├── example_stopper_test.go          # whole-file ExampleStopper: a consumer that drains
│
└── health/
    ├── doc.go                       # package doc: readiness, liveness, reasons withheld
    ├── health.go                    # Check, Handler, New, ServeHTTP, Option, WithLogger,
    │                                # ErrInvalidConfig, validation
    ├── evaluation.go                # shared evaluations, running flags, timeouts, panics
    ├── response.go                  # status, headers, body lines
    ├── options_test.go              # validation (FR-036)
    ├── handler_test.go              # US6: statuses, bodies, methods, verbose, reasons withheld
    ├── evaluation_test.go           # timeouts, panics, still running, shared and new
    │                                # evaluations, cancellation (SC-005)
    ├── example_test.go              # ExampleNew
    └── example_readiness_test.go    # whole-file Example_readiness: wired to lifecycle.App.Ready
```

**Structure Decision**: the feature is one new top-level module, `lifecycle/`, laid out as above.

- **Two packages** in one module (R-01). `health` lives in a subdirectory and has no import of
  `lifecycle`, which `go list -deps` checks (quickstart, section 2).
- **Files** are split by capability: the App's run loop apart from its registration and state,
  and each adapter in its own file with its own tests.
- **Whole-file examples** show their helpers in the documentation: the in-memory listener and the
  draining consumer.
- **Nothing else changes** in the repository except `go.work`, which `make work` regenerates.

## Complexity Tracking

No violation of the constitution needs to be justified.

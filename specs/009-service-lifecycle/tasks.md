---

description: "Task list for the lifecycle service lifecycle module"
---

# Tasks: Service lifecycle (lifecycle)

**Input**: Design documents from `specs/009-service-lifecycle/`

**Prerequisites**: [plan.md](plan.md), [spec.md](spec.md), [research.md](research.md),
[data-model.md](data-model.md), [contracts/](contracts/), [quickstart.md](quickstart.md)

**Tests**: REQUIRED. Constitution Principle III is non-negotiable:

- Tests are written first and observed failing (a compilation failure counts as red).
- Coverage must be ≥ 80%, with `-race -shuffle=on`.
- Every primary API needs an `Example`.
- No fuzz targets and no benchmarks: no external input is parsed and no code is
  performance-sensitive (research R-19).

Every test task below precedes the implementation it covers.

**Organization**: tasks are grouped by user story (spec.md US1–US7). Every path is relative to the
repository root. All Go code lives in the module `lifecycle/`: package `lifecycle` in
`lifecycle/`, package `health` in `lifecycle/health/`.

User Story 7 (documentation) comes last because it documents every flow. Each story phase already
adds its own runnable examples and README section, so the documentation grows with the code.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: can run in parallel with other [P] tasks of the same phase: different files, and no
  dependency on an incomplete task.
- **[Story]**: US1–US7 from spec.md. Setup, Foundational and Polish tasks have no story label.

## Conventions for every task

- **Packages**:
  - Behavior tests use the external packages `lifecycle_test` and `health_test`.
  - Tests of unexported code use `package lifecycle`, in files named `*_internal_test.go`.
    Internal test files cannot see the helpers of the external test package, so they define their
    own small components.
- **Test names**: `Test<Feature>_<Scenario>`, chosen so that the `-run` patterns of
  [quickstart.md §3](quickstart.md) select them:
  - `TestOptions_*`, `TestErrors_*`;
  - `TestRegister_*`, `TestOrder_*`, `TestContext_*`, `TestSignal_*` (US1);
  - `TestStopper_*`, `TestHTTPServer_*`, `TestCloser_*`, `TestStartStop_*` (US2);
  - `TestFailure_*`, `TestHTTPServer_ListenError` (US3);
  - `TestDrain_*`, `TestReady_*` (US4);
  - `TestDeadline_*`, `TestExpired_*`, `TestForced_*`, `TestRealSignal_*` (US5);
  - `TestNew_*`, `TestHandler_*`, `TestEvaluation_*` (US6, in `lifecycle/health`).
- **Test style**:
  - table-driven, with `t.Run`;
  - no socket: HTTP tests use the in-memory listener of T010;
  - no real waiting: every test that involves time runs inside `synctest.Test`, where
    `time.Sleep` advances virtual time and `synctest.Wait` waits for quiescence. The only
    exception is `signal_unix_test.go` (T042), which waits on channels only;
  - goroutines are joined with `sync.WaitGroup.Go` and `Wait`;
  - randomized orders use a SplitMix64 generator with fixed seeds, written in the test file
    (Semgrep's `p/golang` rules forbid `math/rand`).
- **synctest rules** (research R-09, R-20):
  - Create Apps, handlers, channels, timers and servers **inside** the bubble.
  - **Never configure real signals inside a bubble**: `signal.Notify` on a bubbled channel is a
    fatal error of the runtime. Internal tests replace the App's unexported `notify` and
    `stopNotify` fields. External tests inside bubbles never use `WithSignals`.
  - `synctest.Test` fails when a goroutine outlives the test, which is how SC-006 is verified. A
    test that leaves a component ignoring cancellation must release it before the bubble ends.
  - `net.Pipe` is synchronous: test clients read responses in their own goroutine.
- **Requests**: always `http.NewRequestWithContext` (`noctx`).
- **Fixtures**: host names and connection strings without secrets, for example
  `postgres://app@db.internal:5432/app`. No credential-like values (Gitleaks, Bearer).
- **Errors**: every error from the standard library is wrapped with context using `%w`
  (`wrapcheck`). No `panic` (`forbidigo`). Expected `//nolint` directives: none (research R-22).
- **Test helpers** take `testing.TB` and call `tb.Helper()` first (`thelper`).
- **Documentation**: every exported identifier has a doc comment with the wording of
  [contracts/public-api.md](contracts/public-api.md), adapted. README sections are added by each
  story and completed in Phase 9.

---

## Phase 1: Setup (Shared Infrastructure)

**Purpose**: create the module and register it with the repository tooling.

- [X] T001 Create `lifecycle/go.mod` containing exactly `module github.com/paccolamano/go/lifecycle` and `go 1.26.0`, with no `require` and no `replace`.
- [X] T002 [P] Create `lifecycle/CHANGELOG.md` in Keep a Changelog format (the header text of `slogx/CHANGELOG.md`, adapted), with an empty `## [Unreleased]` section. The entries are added in T061.
- [X] T003 [P] Create `lifecycle/README.md` with:
  - the title `# lifecycle`;
  - the purpose paragraph: "Run a service process from start to graceful shutdown: launch its components in order, stop them in reverse order with a drain period and a bounded deadline, and answer readiness and liveness probes. The module has no dependency outside the standard library.";
  - `go get github.com/paccolamano/go/lifecycle`;
  - "Minimum Go version: 1.26";
  - empty headings in the order of research R-21: "A complete main", "The model", "The stop contract", "Bounded shutdown", "Signals and testing", "Compatibility", "Components to write yourself", "Blocking functions", "This repository's modules", "Health probes", "Kubernetes" and "Defaults". They are filled by the story phases and by Phase 9.
- [X] T004 Run `make work` from the repository root so that `go.work` lists `./lifecycle`. Then confirm that `scripts/structure-check.sh lifecycle` reports PASS (depends on T001–T003).

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: options, errors and the test helpers that every story uses.

**⚠️ CRITICAL**: no user story work can begin until this phase is complete.

- [X] T005 Create `lifecycle/doc.go` with a short package doc comment for `package lifecycle`: "Package lifecycle runs the components of a service process, such as servers, workers, consumers and the clients of databases, from start to graceful shutdown: launched in registration order, stopped one at a time in reverse order, with a drain period and a bounded deadline." It is expanded in T060.
- [X] T006 [P] Write failing internal tests in `lifecycle/options_internal_test.go` (`package lifecycle`) for `newConfig(opts []Option) (config, error)`:
  1. `TestOptions_Defaults`: with no options, the stop timeout is 15 s, the drain delay is 0, there are no signals, and the logger is not nil (a discarding logger).
  2. `TestOptions_Validation`. Each rule of data-model.md "App configuration" returns an error that wraps `ErrInvalidConfig`:

     | Option | Rule |
     |--------|------|
     | `WithSignals` | "at least one signal, none nil; duplicates count once": no argument fails, `nil` fails, `(os.Interrupt, os.Interrupt)` keeps one signal |
     | `WithDrainDelay` | "`d >= 0`": −1 ns fails, 0 passes |
     | `WithStopTimeout` | "`d > 0`": 0 and −1 ns fail, 1 ns passes |
     | `WithLogger` | "`l != nil`" |

     A nil `Option` in `opts` returns an error and never panics.
  3. `TestOptions_ErrorFormat`: messages are `lifecycle: invalid configuration: <Option>: <reason>`, with the reasons `no signal`, `nil signal`, `must not be negative`, `must be positive`, `nil logger`, and `lifecycle: invalid configuration: nil option`.
  4. `TestOptions_LastWins`: two `WithStopTimeout` calls keep the last.
- [X] T007 Implement `lifecycle/options.go` until T006 passes:
  - `var ErrInvalidConfig = errors.New("lifecycle: invalid configuration")`;
  - `type Option func(*config)`;
  - the `config` fields of data-model.md: signals, drain delay, stop timeout, logger, plus a field that records a nil option or an invalid value for `newConfig` to report;
  - the constants `defaultStopTimeout = 15 * time.Second` and `defaultDrainDelay = 0`;
  - `WithSignals`, `WithDrainDelay`, `WithStopTimeout` and `WithLogger`, with doc comments that state the defaults (contracts/public-api.md);
  - `newConfig`, which sets the defaults (logger `slog.New(slog.DiscardHandler)`), applies the options, removes duplicate signals, then validates.
- [X] T008 [P] Write failing internal tests in `lifecycle/errors_internal_test.go` (`package lifecycle`):
  1. `TestErrors_ComponentError`: `(&ComponentError{Name: "db", Err: errors.New("boom")}).Error()` is `lifecycle: component "db": boom`; with `Err: ErrStopTimeout` it is `lifecycle: component "db": not stopped in time` (the leading `lifecycle: ` of the wrapped message is removed, research R-04); `errors.Is` reaches the wrapped error through `Unwrap`.
  2. `TestErrors_PanicError`: `Error()` is `panic: <value>` formatted with `%v`; `Value` and `Stack` are kept.
  3. `TestErrors_Compose`: an unexported `outcomes` value with the fields `trigger error`, `later []error`, `stops []error`, `forced bool` and `refused []error` returns, from its `err()` method, nil when every field is empty, and otherwise `errors.Join` in the order of contracts/lifecycle-semantics.md "Result": trigger, later, stops, `ErrForced`, refused. `errors.Is` and `errors.As` (to `*ComponentError` and `*PanicError`) work through the join.
- [X] T009 Implement `lifecycle/errors.go` until T008 passes: the sentinels `ErrAlreadyStarted` (`lifecycle: already started`), `ErrExitedEarly` (`lifecycle: exited before being stopped`), `ErrStopTimeout` (`lifecycle: not stopped in time`), `ErrForced` (`lifecycle: stop forced by a second signal`) and `ErrNotRunning` (`lifecycle: not running`); `ComponentError` with `Error` and `Unwrap`; `PanicError` with `Error`; the unexported `outcomes` type and its `err` method.
- [X] T010 [P] Create `lifecycle/pipenet_test.go` (`package lifecycle_test`) with the in-memory listener of `sse/pipenet_test.go`, adapted: a `pipeNet` type with `Accept`, `Close`, `Addr` and `Dial(ctx, network, addr)`, where `Dial` returns the client end of a `net.Pipe` and hands the server end to `Accept`, and returns `net.ErrClosed` once the listener is closed. Add `newPipeClient(ln *pipeNet) *http.Client`, whose transport dials through `ln`, and `getAsync(tb testing.TB, ctx context.Context, c *http.Client, url string) <-chan response`, which performs a GET in its own goroutine and delivers the status, the body and the error.
- [X] T011 [P] Create `lifecycle/recorder_test.go` (`package lifecycle_test`) with the shared test doubles:
  - `eventLog`: a concurrency-safe ordered list of entries `{name, kind, at}`, where `kind` is `run`, `stop`, `canceled` or `returned`, and `at` is `time.Since` a start time (virtual inside bubbles). It offers `kinds(name)`, `order(kind)` and `at(name, kind)`;
  - `plainComp`: implements only `Component`. Configurable: return on its own after a delay with an error or nil; panic after a delay; ignore cancellation until a release channel is closed; work for a duration after cancellation before returning. It records `run`, `canceled` and `returned`;
  - `stopperComp`: implements `Component` and `Stopper`. Configurable `Stop` behavior: return at once, block until `Run` has returned, never return until released, return an error, return the context's error, or panic. `Run` processes an in-flight item of a configurable duration after `Stop` was called, and records whether its context was still valid when the item completed;
  - `logRecorder`: a concurrency-safe `slog.Handler` that keeps every record's level, message and attributes, with a `find(msg string) []record` helper.

**Checkpoint**: options, errors and test helpers are ready. User story phases can begin.

---

## Phase 3: User Story 1 - Run a service and stop it in order (Priority: P1) 🎯 MVP

**Goal**: register components, run the App, and stop them in strict reverse order when the
context ends or a configured signal arrives.

**Independent Test**: register recording components, cancel the context passed to the App, and
check the order of the stops and the result. Repeat with a fake signal.

### Tests for User Story 1 ⚠️

> Write these tests first and make sure they fail before the implementation.

- [X] T012 [P] [US1] Write failing tests in `lifecycle/register_test.go`:
  1. `TestRegister_Validation`: each invalid registration makes `Run` return an error wrapping `ErrInvalidConfig` without launching any component (no `run` entry in the event log), and `Ready` then reports `stopped`:
     - empty name: the message contains `component at position 2: empty name` for the second registration (positions start at 1);
     - a name already used: a `*ComponentError` with that name and the reason `name already used`;
     - a nil `Component` and `lifecycle.Func(nil)`: a `*ComponentError` with the name. Invalid adapters are tested with their adapters (T022, T023, T024).
  2. `TestRegister_AfterStart` (synctest): `Add` called while the App runs never launches the component. `Run`'s result contains a `*ComponentError` for it wrapping `ErrInvalidConfig` with the reason `registered after start`, and with `WithLogger` the refusal is logged at once, at level Error, with the attribute `component`.
  3. `TestRegister_AlreadyStarted` (synctest): a second `Run` while the first runs, and a third after it has returned, both return `ErrAlreadyStarted` at once, and the first `Run` is unaffected.
  4. `TestRegister_Concurrent`: 50 goroutines call `Add` with distinct names under `-race`, then every component is launched and stopped.
- [X] T013 [P] [US1] Write failing tests in `lifecycle/order_test.go` (synctest):
  1. `TestOrder_Reverse`: for every n from 1 to 50, register n components with names in a seeded random order, cancel the context, and assert that the `stop` (or `canceled`) entries are in exact reverse registration order, and that each one comes after the previous component's `returned` entry (SC-001). `Run` returns nil.
  2. `TestOrder_NoComponents`: an App with no components runs until its context is cancelled, then returns nil.
  3. `TestContext_NotPropagated`: when the context passed to `Run` is cancelled, the first-registered component's context is still valid while the last-registered one stops, and is cancelled only at its own turn.
  4. `TestContext_Values`: a value stored in the context passed to `Run` is visible in every component's context.
  5. `TestContext_AlreadyDone`: with a context already cancelled before `Run`, every component is launched, then stopped in reverse order, and `Run` returns nil.
  6. `TestOrder_Log`: with `WithLogger`, the records include `lifecycle: stop triggered` with `trigger=context`, and one `lifecycle: component stopped` per component, with `component` and `duration` attributes, in stop order (research R-15).
- [X] T014 [P] [US1] Write failing internal tests in `lifecycle/signal_internal_test.go` (`package lifecycle`, synctest). Each test replaces the App's `notify` and `stopNotify` fields with fakes that keep the channel, and sends fake signals on it from inside the bubble:
  1. `TestSignal_Trigger`: `WithSignals(syscall.SIGTERM)`; a fake `SIGTERM` starts the stop sequence and `Run` returns nil. With a logger, `lifecycle: stop triggered` has `trigger=signal` and `signal=terminated`.
  2. `TestSignal_NotConfigured`: without `WithSignals`, `notify` is never called.
  3. `TestSignal_Dedupe`: `WithSignals(os.Interrupt, os.Interrupt)` calls `notify` with one signal.
  4. `TestSignal_Restore`: registration and unregistration are balanced on every path: a normal stop calls `stopNotify` with the channel given to `notify` before `Run` returns; an invalid registration, which launches nothing, calls neither.

### Implementation for User Story 1

- [X] T015 [US1] Implement `lifecycle/component.go`: `Component`, `Stopper`, `Func` and `Func.Run`, with the doc comments of contracts/public-api.md; the unexported `validator` interface with `validate() error`; `Func.validate`, which reports a nil function as an error wrapping `ErrInvalidConfig` with the reason `nil function`.
- [X] T016 [US1] Implement `lifecycle/app.go`: the `App` type with its config, a mutex, the registrations (data-model.md "Registration"), the refused registrations, the state in an `atomic.Int32` (`starting`, `running`, `stopping`, `stopped`) and the `notify` and `stopNotify` fields, set by `New` to `signal.Notify` and `signal.Stop`; `New`; `Add`, which records the validation errors of data-model.md, or the refusal `registered after start` (logged at once) when the App has started; `Ready`, which returns nil while running and otherwise `fmt.Errorf("%w: %s", ErrNotRunning, state)`.
- [X] T017 [US1] Implement the first version of `Run` in `lifecycle/run.go` until T012 and T013 pass: the compare-and-swap that returns `ErrAlreadyStarted`; the registration errors, returned joined without launching; signal registration (T018); one goroutine per component with `context.WithCancel(context.WithoutCancel(ctx))`, reporting its return on a channel buffered for every component; the move to `running`; the wait for `ctx.Done()` or a signal; the move to `stopping`; the stop of each component, last registered first, by cancelling its context and waiting for its return, bounded by the stop timeout; signal unregistration; the move to `stopped`; the result from `outcomes.err()`; the logs of research R-15 for the trigger and each stopped component. Stoppers, failures, the drain and the expired path come in later phases.
- [X] T018 [US1] Implement `lifecycle/signal.go` until T014 passes: registration through `a.notify` on a channel buffered for two signals, before launching; unregistration through `a.stopNotify` on every return path of `Run`; the signal's name (`sig.String()`) for the trigger log; a counter of received signals that Phase 7 uses for the forced stop.

### Documentation for User Story 1

- [X] T019 [US1] Create `lifecycle/example_test.go` with `Example`: an App with the `Func` components `db` and `worker` (registered in this order), each printing `<name> stopped` when its context is cancelled, and a third component `ready` that closes a channel when it runs. The example waits for that channel, cancels the context, waits for `Run`, and prints `run: <nil>`. `// Output:` is `ready stopped`, `worker stopped`, `db stopped`, `run: <nil>`. No sleep.
- [X] T020 [US1] Fill the README section "The model" in `lifecycle/README.md`: components and the optional stop operation, the ordering rule ("register in the order you open or start things; they stop in reverse"), resources opened by the developer with ordinary error handling, registration errors reported by `Run`, and why nothing is restarted.

**Checkpoint**: an App stops its components in order on its context or a signal (MVP).

---

## Phase 4: User Story 2 - Let in-flight work complete before stopping (Priority: P1)

**Goal**: the stop contract (stop intake, wait, cancel only at the deadline) and the three
adapters.

**Independent Test**: a component whose stop only stops intake finishes its item with a valid
context; the HTTP server completes requests in progress during its turn; work that outlasts the
deadline is cancelled and reported.

### Tests for User Story 2 ⚠️

- [X] T021 [P] [US2] Write failing tests in `lifecycle/contract_test.go` (synctest):
  1. `TestStopper_InFlightCompletes`: a `stopperComp` processing a 3 s item when its turn comes; `Stop` returns at once; the item completes with a valid context, and nothing is reported (SC-004).
  2. `TestStopper_WaitsForRun`: `Stop` returns at once and `Run` returns 2 s later, with a 15 s stop timeout: the App waits 2 s, never cancels the run's context, then stops the previous component.
  3. `TestStopper_DeadlineCancels`: `WithStopTimeout(5 * time.Second)` and a `Run` that returns only when its context is cancelled: the context is cancelled at 5 s, and the result contains a `*ComponentError` wrapping `ErrStopTimeout`.
  4. `TestStopper_StopError`: `Stop` returns `errors.New("boom")`: the result contains `lifecycle: component "q": stop: boom`, and the App still waited for `Run`.
  5. `TestStopper_StopContextErrorSuppressed`: `Stop` returns its context's error after the deadline: only `ErrStopTimeout` is reported for that component, with no stop error.
  6. `TestStopper_WithoutStop`: a `plainComp` gets the cancellation of its context at the start of its turn.
  7. `TestStopper_CanceledIsClean`: a `Run` that returns `context.Canceled`, or an error wrapping it, after its stop is clean, and `Run` returns nil.
  8. `TestStopper_OtherErrorAfterStop`: a `Run` that returns `errors.New("x")` after its stop is reported as `lifecycle: component "<name>": x`.
- [X] T022 [P] [US2] Write failing tests in `lifecycle/http_test.go` (synctest, `pipeNet`):
  1. `TestHTTPServer_InFlight`: 100 concurrent GET requests to a handler that sleeps 3 s; once `synctest.Wait` shows them in progress, cancel the App's context: every request completes with 200 and its body, and `Run` returns nil (SC-002).
  2. `TestHTTPServer_NewConnectionsRefused`: once the server's turn has begun, `pipeNet.Dial` fails with `net.ErrClosed`.
  3. `TestHTTPServer_DeadlineCloses`: a request that takes 30 s with a 5 s stop timeout: the client sees its connection closed, and the result contains `ErrStopTimeout` for `http`.
  4. `TestHTTPServer_ShutdownHook`: a hook registered with `srv.RegisterOnShutdown` runs during the server's turn.
  5. `TestHTTPServer_Listener`: `HTTPServer(srv, WithListener(ln))` serves through `ln`, and once the App has stopped, `ln.Dial` fails with `net.ErrClosed`.
  6. `TestHTTPServer_StopBeforeServe`: with a context already cancelled before `Run`, the server is launched and stopped, the listener is closed, and `Run` returns nil.
  7. `TestHTTPServer_TLSChoice`: over `WithListener`, a plain HTTP request gets 200 when `TLSConfig` is nil and when it is a non-nil `tls.Config` without a certificate source. With `TLSConfig.Certificates` set to one empty `tls.Certificate`, with `GetCertificate` set, or with `GetConfigForClient` set, the server serves TLS, and the plain request gets the standard library's 400 answer, `Client sent an HTTP request to an HTTPS server.`. No certificate is generated.
  8. `TestHTTPServer_Validation`: `HTTPServer(nil)` and `HTTPServer(srv, WithListener(nil))` make `Run` return `ErrInvalidConfig` before launching anything.
- [X] T023 [P] [US2] Write failing tests in `lifecycle/closer_test.go` (synctest):
  1. `TestCloser_Shapes`: three Closers with a `func()`, a `func() error` and a `func(context.Context) error`, each called exactly once at its turn, in reverse order. The third receives a context with a deadline.
  2. `TestCloser_Error`: a `func() error` that fails produces the stop error `stop: <err>` for its component.
  3. `TestCloser_Nil`: a nil function of each of the three types makes `Run` return `ErrInvalidConfig` before launching anything.
- [X] T024 [P] [US2] Write failing tests in `lifecycle/startstop_test.go` (synctest):
  1. `TestStartStop_StartAtLaunch`: `start` is called at launch with a context that carries the values of `Run`'s context.
  2. `TestStartStop_StopAtTurn`: `stop` is called at its turn with a context that has a deadline, and the component's run returns afterwards.
  3. `TestStartStop_Nil`: a nil `start` or `stop` makes `Run` return `ErrInvalidConfig`.
  4. `TestStartStop_StopWaitsForStart` (added during implementation): with the stop triggered while `start` is still running, `stop` is called only after `start` has returned.
  5. `TestStartStop_StopSkippedAfterStartFailure` (added during implementation): a `start` that fails while the App is already stopping is reported, and `stop` is never called.

  The start error case as a trigger is tested in T033, with the other failures.

### Implementation for User Story 2

- [X] T025 [US2] Extend `lifecycle/run.go` with the stop contract until T021 passes (research R-07): the stop context `context.WithDeadline(context.WithoutCancel(ctx), end)` with `context.WithCancelCause` on top; for a `Stopper`, `Stop` in its own goroutine and a wait for both `Stop` and `Run` that sets each channel to nil once received; for other components, the cancellation at the start of their turn; when the stop context ends first, the cancellation of the run's context and `ErrStopTimeout`; a stop error equal to the stop context's own error is not reported; stop errors wrapped as `stop: %w`; a run error that is nil or wraps `context.Canceled` after its stop is clean.
- [X] T026 [P] [US2] Implement `lifecycle/http.go` until T022 passes (research R-11, contracts/lifecycle-semantics.md "HTTPServer"): `HTTPServer`, `HTTPOption`, `WithListener`; an unexported `httpServer` type whose `Run` picks `Serve`, `ServeTLS(ln, "", "")`, `ListenAndServe` or `ListenAndServeTLS("", "")`, maps `http.ErrServerClosed` to nil and wraps other errors as `lifecycle: http server: %w`; whose `Stop` calls `Shutdown(ctx)` and, when it returns because `ctx` ended, `Close()`, joining and wrapping both errors; whose `validate` reports a nil server or a nil listener; and the TLS rule "`TLSConfig` not nil and has `Certificates`, `GetCertificate` or `GetConfigForClient`".
- [X] T027 [P] [US2] Implement `lifecycle/closer.go` until T023 passes (research R-12): `func Closer[F func() | func() error | func(context.Context) error](f F) Component` with the constraint written inline; a type switch on `any(f)` whose `default` branch returns an error (never `panic`); `Run` waits for the release or its context and returns nil; `Stop` calls the function once with `sync.Once`, passing the stop context to the third shape, then releases `Run`; `validate` reports a nil function of any of the three types.
- [X] T028 [P] [US2] Implement `lifecycle/startstop.go` until T024 passes (research R-13): `Run` calls `start` with its context, returns a start error wrapped as `lifecycle: start: %w`, and otherwise waits for the release or its context; `Stop` waits until `start` has returned (or its context ends), skips `stop` when `start` failed, and otherwise calls `stop`, then releases `Run`; `validate` reports a nil `start` or `stop`. `launch` in `lifecycle/run.go` waits until each goroutine has called `Run` before launching the next, so that `Stop` never comes before `Run`.

### Documentation for User Story 2

- [X] T029 [P] [US2] Create `lifecycle/example_stopper_test.go` with the whole-file example `ExampleStopper`: a `consumer` type with a buffered intake channel of three items; its `Stop` stops intake; its `Run` processes every item already received, printing `processed <n>`, then returns. The example waits until the consumer runs, cancels the context and prints the result. `// Output:` lists `processed 1` to `processed 3`, then `run: <nil>`.
- [X] T030 [P] [US2] Create `lifecycle/example_http_test.go` with the whole-file example `ExampleHTTPServer`: a minimal in-memory listener declared in the file (named `memListener`, so that it does not clash with `pipeNet`), an `http.Server` registered with `HTTPServer(srv, WithListener(ln))`, one GET request through it that prints the body, then a stop through the context. `// Output:` shows the body and `run: <nil>`.
- [X] T031 [P] [US2] Add `ExampleCloser` and `ExampleStartStop` to `lifecycle/example_test.go`: `ExampleCloser` registers the three close shapes, each printing its name, and shows that they run in reverse order; `ExampleStartStop` prints `started` at launch and `stopped` at its turn.
- [X] T032 [US2] Fill the README section "The stop contract" in `lifecycle/README.md`: the three steps (stop intake, wait for the run, cancel only at the deadline), the case without a stop operation, the shutdown timeline of contracts/lifecycle-semantics.md, and the three adapters with one short example each, including the conversion of a named function type such as `context.CancelFunc` for `Closer` (research R-12).

**Checkpoint**: in-flight work completes, and servers, resources and start/stop services fit the
adapters.

---

## Phase 5: User Story 3 - Stop the whole process when a component fails (Priority: P1)

**Goal**: a component that returns or panics before being asked stops every other one in order,
and the App's error names it.

**Independent Test**: make one of several components return an error, return nil, or panic, and
check the reverse stop of the others and the error.

### Tests for User Story 3 ⚠️

- [X] T033 [P] [US3] Write failing tests in `lifecycle/failure_test.go` (synctest):
  1. `TestFailure_Error`: components A, B and C; B returns `errors.New("boom")` at 1 s: C then A are stopped, B is not stopped again (a `stopperComp` B gets no `Stop` call), the result contains a `*ComponentError` named `b` wrapping the error, and the log has `trigger=component` and `component=b`.
  2. `TestFailure_ExitedEarly`: B returns nil before any stop: the result contains `ErrExitedEarly` named `b`.
  3. `TestFailure_Panic`: B's run panics with `"bad"`: the result contains a `*PanicError` with `Value == "bad"` and a `Stack` that contains `failure_test.go`, inside a `*ComponentError` named `b`.
  4. `TestFailure_StopPanic`: a `Stop` that panics produces a stop error wrapping a `*PanicError`, and the App still waits for that component's run.
  5. `TestFailure_Simultaneous`: B and C fail at the same virtual instant: both appear in the result, and A is stopped exactly once.
  6. `TestFailure_DuringStop`: a component that returns on its own while another one is being stopped is reported as a later failure and skipped.
  7. `TestFailure_ReadyAfterTrigger`: right after a failure, `Ready` returns an error wrapping `ErrNotRunning` that contains `stopping`.
  8. `TestFailure_StartStopStartError`: a `StartStop` whose `start` fails is a failure of its component, its `stop` is never called, and the result wraps the start error.
- [X] T034 [P] [US3] Add `TestHTTPServer_ListenError` to `lifecycle/http_test.go`: a server with `Addr: "127.0.0.1:-1"` fails at once without opening a socket (research R-11); the other components are stopped in reverse order; the result contains a `*ComponentError` named `http` whose message contains `invalid port`.

### Implementation for User Story 3

- [X] T035 [US3] Extend `lifecycle/run.go` until T033 and T034 pass (research R-06, R-10): a run that returns before its turn is the trigger when it is the first, and a later failure otherwise, with `ErrExitedEarly` for nil; returned components are skipped by the stop sequence; the goroutines that call `Run` and `Stop` recover panics into a `*PanicError` with `debug.Stack()`; the error-level logs of research R-15 for failures, early exits and stop errors.

### Documentation for User Story 3

- [X] T036 [US3] Add a subsection "When a component fails" to the README section "The model" in `lifecycle/README.md`: a component that returns or panics stops the process in order; the App's error names it; recovered panics, and why a panic in a goroutine started by a component itself still ends the process; restarting is the orchestrator's job.

**Checkpoint**: failures stop the process in order and are reported by name.

---

## Phase 6: User Story 4 - Drain before shutdown behind a load balancer (Priority: P2)

**Goal**: readiness fails at the trigger while every component keeps serving for the drain delay.

**Independent Test**: with a drain delay and a server, readiness fails at once, a request sent
during the drain is served, and the first stop happens when the delay ends.

### Tests for User Story 4 ⚠️

- [X] T037 [P] [US4] Write failing tests in `lifecycle/drain_test.go` (synctest, `pipeNet`):
  1. `TestDrain_ServingContinues`: `WithDrainDelay(10 * time.Second)`, an HTTP server and a worker; after the trigger at t=0, `Ready` fails at once, a request sent at t=5 s gets 200, and the first `stop` or `canceled` entry is at t=10 s (SC-002).
  2. `TestDrain_DefaultZero`: without `WithDrainDelay`, the first stop happens at t=0.
  3. `TestDrain_FailureTrigger`: the drain also applies when a component's failure triggers the stop.
  4. `TestDrain_ReturnsDuringDrain`: a component that returns during the drain is reported as a later failure and skipped.
  5. `TestDrain_Log`: `lifecycle: drain started` is logged with `delay=10s`.
  6. `TestReady_States`: `Ready` returns `lifecycle: not running: starting` before `Run`, nil while running, `lifecycle: not running: stopping` during the drain and the stops, and `lifecycle: not running: stopped` after `Run` has returned; each error wraps `ErrNotRunning`.

### Implementation for User Story 4

- [X] T038 [US4] Extend `lifecycle/run.go` until T037 passes: the drain between the trigger and the first stop, on a timer, collecting component returns as later failures, with a hook that Phase 7 uses to end it on a forced stop; the end of the stop sequence's deadline at `end of drain + stop timeout`; the `drain started` log.

### Documentation for User Story 4

- [X] T039 [US4] Write the first part of the README section "Kubernetes" in `lifecycle/README.md`: why requests keep arriving after the terminate signal, the drain delay (0 by default) or a `preStop` sleep hook, and readiness failing during the drain. The probes and the grace period are added in T059.

**Checkpoint**: rollouts behind a load balancer produce no failed requests.

---

## Phase 7: User Story 5 - Keep the shutdown bounded (Priority: P2)

**Goal**: `Run` returns by the deadline whatever the components do, remaining components still get
their stop, and a second signal forces the return.

**Independent Test**: components that ignore cancellation or whose `Stop` never returns; with
simulated time, `Run` returns at the deadline with the right report.

### Tests for User Story 5 ⚠️

- [X] T040 [P] [US5] Write failing tests in `lifecycle/bounded_test.go` (synctest):
  1. `TestDeadline_IgnoresCancellation`: stop timeout 5 s; the last-registered component ignores cancellation: `Run` returns after exactly 5 s, the result names it with `ErrStopTimeout`, and the components registered before it still received `Stop` and the cancellation of their context. The test then releases the stuck component.
  2. `TestDeadline_StopNeverReturns`: a `Stop` that never returns gives the same outcome.
  3. `TestExpired_RemainingNotWaited`: the first component to stop uses the whole deadline; the remaining ones, a `plainComp` and a `Closer`, get their cancellation and their close call after the deadline, `Run` returns at the deadline without waiting for them, and each one whose run had not returned is reported with `ErrStopTimeout`. After `synctest.Wait`, the `Closer`'s function has run.
  4. `TestDeadline_Log`: `lifecycle: component not stopped in time` is logged at level Error with `component` and `duration`.
- [X] T041 [P] [US5] Add to `lifecycle/signal_internal_test.go` (fake signals, synctest):
  1. `TestForced_SecondSignal`: two fake signals, the second one during a stop that waits on a slow component: `Run` returns at once with an error wrapping `ErrForced`, the remaining components received their stop and cancellation, and `lifecycle: stop forced` is logged at level Warn.
  2. `TestForced_DuringDrain`: a second signal during a 10 s drain ends it at once.
  3. `TestForced_AfterOtherTrigger`: after a stop triggered by the context, one signal does not force the stop and a second one does.
- [X] T042 [P] [US5] Create `lifecycle/signal_unix_test.go` with `//go:build unix` (`package lifecycle_test`, no synctest):
  1. `TestRealSignal_Trigger`: `WithSignals(syscall.SIGUSR1)`; a `Func` component closes a channel when its run starts; after receiving from it, the test sends `syscall.Kill(os.Getpid(), syscall.SIGUSR1)`, and `Run` returns nil.
  2. `TestRealSignal_Forced`: a component whose `Stop` blocks until the test releases it; two `SIGUSR1` signals make `Run` return an error wrapping `ErrForced`; the test then releases the component.

  The tests wait only on channels, never on time.

### Implementation for User Story 5

- [X] T043 [US5] Extend `lifecycle/run.go` and `lifecycle/signal.go` until T040–T042 pass (research R-08, R-09): the expired path of contracts/lifecycle-semantics.md (remaining components get `Stop` in a goroutine with the expired context and their cancellation, without waiting, and `ErrStopTimeout` for each run not yet returned); the signal counter, with the second signal cancelling the stop context with the cause `ErrForced` and ending the drain; `ErrForced` in the result; the `stop forced` and `component not stopped in time` logs.

### Documentation for User Story 5

- [X] T044 [US5] Fill the README sections "Bounded shutdown" and "Signals and testing" in `lifecycle/README.md`:
  - the stop timeout, the expired path and how to read the App's error with `errors.Is` (`ErrStopTimeout`, `ErrForced`, `ErrExitedEarly`) and `errors.As` (`*ComponentError`, `*PanicError`);
  - signals: opt-in, which to configure (`os.Interrupt`, `syscall.SIGTERM`), the second signal, Windows (`os.Interrupt` delivered, `SIGTERM` not sent to console programs);
  - testing: stop an App through its context, because `signal.Notify` inside a `testing/synctest` bubble ends the test binary with a fatal error (FR-046).

**Checkpoint**: shutdowns finish before the orchestrator's grace period.

---

## Phase 8: User Story 6 - Expose readiness and liveness probes (Priority: P2)

**Goal**: the `health` handler with shared evaluations and withheld reasons, usable without the App.

**Independent Test**: build handlers with checks that pass, fail, block or panic, call them, and
check statuses, bodies, timing and logs, without any App.

### Tests for User Story 6 ⚠️

- [X] T045 [P] [US6] Write failing tests in `lifecycle/health/options_test.go` (`package health_test`):
  1. `TestNew_Validation`. Each rule of data-model.md "Check" returns an error that wraps `health.ErrInvalidConfig`:

     | Field | Rule |
     |-------|------|
     | `Name` | "non-empty, valid UTF-8, no whitespace or control rune, unique": `""`, `"\xff"`, `"my check"`, `"a\tb"`, `"a\nb"` and a duplicate name fail; `"postgres"`, `"redis:cache"` and `"lifecycle"` pass |
     | `Func` | "not nil" |
     | `Timeout` | "`>= 0`; 0 means 1 s": −1 ns fails, 0 passes |

     `WithLogger(nil)` and a nil `Option` fail too.
  2. `TestNew_NoChecks`: `health.New(nil)` succeeds.
- [X] T046 [P] [US6] Write failing tests in `lifecycle/health/handler_test.go` (`package health_test`, `httptest.ResponseRecorder`, synctest where time matters), against contracts/health-http.md:
  1. `TestHandler_AllPass`: 200, body `ok\n`, and exactly the headers `Content-Type: text/plain; charset=utf-8`, `Cache-Control: no-store`, `X-Content-Type-Options: nosniff` and `Content-Length`.
  2. `TestHandler_Failure`: checks `lifecycle`, `postgres` and `redis`, with `redis` failing with `dial tcp db.internal:6379: connection refused`: 503 and the body `[+]lifecycle ok\n[+]postgres ok\n[-]redis failed: reason withheld\nfailed\n`; `db.internal` appears nowhere in the response; the logger has `health: check failed` at level Warn with `check=redis`, `reason=error` and the error.
  3. `TestHandler_Verbose`: every check passes, and `?verbose` and `?verbose=0` both give one line per check then `ok`.
  4. `TestHandler_Head`: the same status and headers as GET, and no body.
  5. `TestHandler_MethodNotAllowed`: POST gets 405, `Allow: GET, HEAD`, the body `method not allowed\n`, and no check runs (call counters stay at 0).
  6. `TestHandler_NoChecks`: 200 and `ok\n`.
  7. `TestHandler_NeverLeaks`: 20 checks with mixed outcomes (errors containing `postgres://app@db.internal:5432/app`, host names and ports, timeouts, panics): no response contains any error text, panic value or duration (SC-005).
- [X] T047 [P] [US6] Write failing tests in `lifecycle/health/evaluation_test.go` (`package health_test`, synctest):
  1. `TestEvaluation_Timeout`: a check that blocks beyond the default timeout: the response arrives at exactly 1 s with that check failed, and the log has `reason=timeout`.
  2. `TestEvaluation_CustomTimeout`: `Timeout: 3 * time.Second` is honored.
  3. `TestEvaluation_Panic`: a panicking check fails, the log has `reason=panic` with `panic` and `stack` attributes, and a later request is served normally.
  4. `TestEvaluation_StillRunning`: a check that ignores its context and returns after 10 s, with a 1 s timeout: the first request fails at 1 s; a request at 2 s does not start it again (its call counter stays at 1) and fails with `reason=still running`; a request at 11 s starts it again (call counter 2).
  5. `TestEvaluation_Shared`: 100 concurrent requests during one evaluation: each check is called once, and every request gets the same status and body (SC-005).
  6. `TestEvaluation_NewAfterComplete`: a request after an evaluation has completed runs every check again.
  7. `TestEvaluation_CancelWhenAllGone`: the only waiting request's context is cancelled: the checks' contexts are cancelled, and the abandoned evaluation logs nothing.
  8. `TestEvaluation_KeptWhileWaiting`: of two waiting requests, one leaves: the checks' contexts stay valid and the other request gets the result.
  9. `TestEvaluation_ContextValues`: the checks' contexts carry the values of the first request's context.

### Implementation for User Story 6

- [X] T048 [P] [US6] Create `lifecycle/health/doc.go` with the package doc comment: "Package health answers readiness and liveness probes by running named checks. It never writes why a check failed; the logger receives the reason. It does not depend on package lifecycle: an App's readiness is passed to it as one more check."
- [X] T049 [US6] Implement `lifecycle/health/health.go` until T045 passes: `Check`, `Handler`, `New` with the validation of research R-18, `Option`, `WithLogger`, `var ErrInvalidConfig = errors.New("health: invalid configuration")`, the constant `defaultTimeout = time.Second`, and `ServeHTTP`, which answers 405 with `Allow: GET, HEAD` and `method not allowed` for other methods, and otherwise delegates to the evaluation and the response.
- [X] T050 [US6] Implement `lifecycle/health/evaluation.go` until T047 passes (research R-16, data-model.md "Evaluation"): the evaluation in progress under the handler's mutex; joining or starting; waiter counting, with the last waiter that leaves cancelling and detaching the evaluation; the per-check `running` flags; one goroutine per operation with its timeout context and panic recovery, and one watcher per check that records the result or the timeout; the single log of failures per completed, non-abandoned evaluation, with the attributes of contracts/health-http.md "Logging".
- [X] T051 [US6] Implement `lifecycle/health/response.go` until T046 passes: the status, the headers (`Content-Length` included), the body lines of contracts/health-http.md with a line feed after each line, `verbose` through `r.URL.Query().Has("verbose")`, no body for HEAD, and nothing written when the request's context ended before the evaluation completed. If Semgrep later reports a match on writing the body, add `// nosemgrep: <rule> -- text/plain body with nosniff, made only of developer-defined check names` (research R-22).

### Documentation for User Story 6

- [X] T052 [P] [US6] Create `lifecycle/health/example_test.go` with `ExampleNew`: a handler with a passing `postgres` check and a failing `redis` check, called through `httptest.NewRecorder`; it prints the status and the body. `// Output:` is `503` followed by the four body lines.
- [X] T053 [P] [US6] Create `lifecycle/health/example_readiness_test.go` with the whole-file example `Example_readiness`: a `lifecycle.App` with one `Func` component that closes a channel when it runs; a readiness handler with the check `{Name: "lifecycle", Func: app.Ready}` and a liveness handler without checks. It prints the readiness status before `Run` (503), while running (200), and the liveness status (200), then stops the App through its context. It imports `github.com/paccolamano/go/lifecycle`, which is the same module.
- [X] T054 [US6] Fill the README section "Health probes" in `lifecycle/README.md`: building readiness and liveness handlers, connecting `app.Ready`, using `health` without the App, the response format, reasons withheld and where to find them in the logs, shared evaluations and the "still running" rule, why liveness must include neither dependencies nor `app.Ready`, and the advice not to put secrets in error messages.

**Checkpoint**: probes work with or without the App, and never disclose a failure's reason.

---

## Phase 9: User Story 7 - Integrate any kind of component by following the documentation (Priority: P2)

**Goal**: the README covers every system and shape of FR-048 to FR-052.

**Independent Test**: follow the README for each system of the compatibility table; the snippets
match the adapters and the stop contract, and the runnable examples pass.

- [X] T055 [US7] Fill the README section "A complete main" in `lifecycle/README.md` (research R-21, item 2): a `run() error` function that opens a `slogx` handler, a tracer provider, a database and a Redis client; registers `Closer(h.Flush)`, `Closer(tp.Shutdown)`, `Closer(db.Close)`, `Closer(rdb.Close)`, the `sse` feed loop as a `Func`, a worker, and `HTTPServer(srv)` with `srv.RegisterOnShutdown(broker.Close)`; builds readiness (`app.Ready`, `db.PingContext`, a Redis ping closure) and liveness handlers; uses `WithSignals(os.Interrupt, syscall.SIGTERM)` and `WithDrainDelay`; and returns `app.Run(context.Background())`. Mark it as a sketch with the client versions of research R-21.
- [X] T056 [US7] Fill the README section "Compatibility" in `lifecycle/README.md` with the table of FR-048: for each system (standard library HTTP server, `sse` broker and feed loop, goroutine workers and tickers, `*sql.DB`, pgx v5 pool, go-redis v9, franz-go, amqp091-go, river, robfig/cron v3, OpenTelemetry tracer provider, `slogx`, gRPC, NATS), how to register it and the pitfall that applies: the Kafka client registered before its consumer loop, the `sse` broker as a shutdown hook rather than a component, a Redis ping that needs a closure, cron's `Stop` that returns a context.
- [X] T057 [US7] Fill the README section "Components to write yourself" in `lifecycle/README.md` (FR-049), each as a sketch with its client version:
  - a RabbitMQ consumer type whose `Stop` calls `Channel.Cancel(tag, false)` ("`noWait` must be false: with true, in-flight messages are dropped without an ack") and whose `Run` ranges over the deliveries until the broker closes them, then the `ConsumeWithContext` alternative, which cancels the consumer when its context ends and interrupts in-flight messages, fine only for idempotent handlers;
  - a gRPC server type whose `Stop` runs `GracefulStop` in a goroutine and calls `Stop` when the context ends;
  - a NATS connection whose `Stop` calls `Drain` and waits for the `ClosedHandler` or the context.
- [X] T058 [US7] Fill the README section "Blocking functions" in `lifecycle/README.md` (FR-050): a function that takes a context is a `Func`; a function that another operation unblocks is a component whose `Stop` calls it, with a `net.Listener` accept loop example that maps `net.ErrClosed` to nil; a function that nothing can unblock cannot be stopped, and the App reports it with `ErrStopTimeout` and moves on.
- [X] T059 [US7] Fill the README sections "This repository's modules", "Kubernetes" (second part) and "Defaults" in `lifecycle/README.md`:
  - `sse`: `srv.RegisterOnShutdown(broker.Close)` and why a component stopped before the server would make clients reconnect to the draining process; the feed loop as a `Func`; `slogx`: `Closer(h.Flush)` registered first so that it runs last;
  - Kubernetes: the drain delay plus the stop timeout kept below `terminationGracePeriodSeconds` (30 s by default), the readiness and liveness probes pointed at their handlers, the probe's `timeoutSeconds` above the longest check timeout, and a deployment snippet with probes and an optional `preStop` sleep;
  - Defaults: stop timeout 15 s, drain delay 0, no signals, no logger, check timeout 1 s.
- [X] T060 [US7] Expand `lifecycle/doc.go` with the model, the ordering rule and the stop contract in short form, and links to the examples; check that `lifecycle/health/doc.go` links to `ExampleNew` and `Example_readiness`.

**Checkpoint**: every flow is documented.

---

## Phase 10: Polish & Cross-Cutting Concerns

**Purpose**: release notes and the full gates.

- [X] T061 Fill `lifecycle/CHANGELOG.md` under `## [Unreleased]`, with an "Added" list covering the `App` (ordered launch and reverse stop, the stop contract, drain, stop timeout, signals, readiness), `Component`, `Stopper` and `Func`, the adapters `HTTPServer` (with `WithListener`), `Closer` and `StartStop`, the errors, and the `health` package. As for the earlier modules, the entries move to `## [0.1.0] - <date>` in the separate release pull request (`chore(lifecycle): prepare release 0.1.0`).
- [X] T062 Run `make check MODULE=lifecycle`. Fix every golangci-lint finding without `//nolint`, and raise coverage to ≥ 80% with tests for the uncovered branches.
- [X] T063 Run the checks of [quickstart.md §2](quickstart.md): `scripts/structure-check.sh lifecycle`, `go mod tidy -diff`, `go vet` for linux, darwin and windows on amd64 and arm64, and the `go list -deps ./health` check that `health` does not import `lifecycle`.
- [X] T064 Run [quickstart.md](quickstart.md) sections 3 and 4. Section 5, the manual check with real signals and `curl`, is recommended before release.

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: no dependencies.
- **Foundational (Phase 2)**: depends on Setup and blocks every story.
- **US1 (Phase 3)**: depends on Foundational.
- **US2 (Phase 4)**: depends on US1 (`run.go`, `component.go`, `app.go`).
- **US3 (Phase 5)**: depends on US2: the stop contract, the adapters (T034 uses `HTTPServer`, T033 uses `StartStop`).
- **US4 (Phase 6)**: depends on US3 (later failures during the drain).
- **US5 (Phase 7)**: depends on US4 (the drain hook for the forced stop).
- **US6 (Phase 8)**: depends on Foundational only for its package code and tests. T053 depends on US1 (`App.Ready`). It can run in parallel with Phases 3–7.
- **US7 (Phase 9)**: depends on every other story, because it documents all the flows.
- **Polish (Phase 10)**: depends on everything else.

### User Story Dependencies

```text
Foundational ─▶ US1 ─▶ US2 ─▶ US3 ─▶ US4 ─▶ US5 ─┐
Foundational ─▶ US6 (T053 after US1) ───────────┤
                                                ▼
                                     US7 ─▶ Polish
```

### Within Each User Story

- Tests are written first and fail, then the implementation, then the examples and the README
  section.
- `lifecycle/run.go` is extended by one task at a time: T017, T025, T035, T038, T043.
- Files shared across stories (`lifecycle/README.md`, `lifecycle/example_test.go`,
  `lifecycle/http_test.go`, `lifecycle/signal_internal_test.go`) are edited by one task at a time.

### Parallel Opportunities

- Phase 1: T002 and T003.
- Phase 2: T006, T008, T010 and T011.
- US1: T012, T013 and T014.
- US2: T021, T022, T023 and T024 (tests); then T026, T027 and T028 alongside T025.
- US3: T033 and T034.
- US5: T040, T041 and T042 (three different files).
- US2 documentation: T029, T030 and T031.
- US6: T045, T046 and T047 (tests); T048 with them; T052 and T053 once the handler exists.
- US6 as a whole, in parallel with US1–US5 once Foundational is done.

---

## Parallel Example: User Story 2

```bash
# Write all the failing tests of User Story 2 together:
Task: "TestStopper_* in lifecycle/contract_test.go"
Task: "TestHTTPServer_* in lifecycle/http_test.go"
Task: "TestCloser_* in lifecycle/closer_test.go"
Task: "TestStartStop_* in lifecycle/startstop_test.go"

# Then the implementation; the adapters are independent files:
Task: "stop contract in lifecycle/run.go (T025)"
Task: "HTTPServer in lifecycle/http.go (T026)"
Task: "Closer in lifecycle/closer.go (T027)"
Task: "StartStop in lifecycle/startstop.go (T028)"
```

## Parallel Example: health alongside the App

```bash
# Once Phase 2 is done, one developer takes US1 → US5 while another takes US6:
Task: "T045–T047 health tests in lifecycle/health/"
Task: "T048–T051 health implementation in lifecycle/health/"
```

---

## Implementation Strategy

### MVP First (User Story 1 Only)

1. Complete Phase 1 (Setup) and Phase 2 (Foundational).
2. Complete Phase 3 (User Story 1): registration, ordered launch and reverse stop, signals.
3. **Stop and validate**: run `go test -race -run 'Register|Order|Context|Signal' .` from
   `lifecycle/`.

### Incremental Delivery

1. Setup and Foundational: options, errors, test helpers.
2. US1: ordered stop (MVP).
3. US2: the stop contract and the adapters.
4. US3: failures.
5. US4: drain and readiness.
6. US5: bounded shutdown and the forced stop.
7. US6: health probes (possibly in parallel with 2–6).
8. US7: the documentation that ties the flows together.
9. Polish: CHANGELOG and gates.

Each step keeps every earlier test green. The module is released once, as `lifecycle/v0.1.0`,
after Phase 10.

---

## Notes

- Tasks marked [P] touch different files and have no dependency on an incomplete task.
- Story labels map tasks to spec.md user stories for traceability.
- Verify that each test fails before implementing it.
- The PR title is `feat(lifecycle): add service lifecycle module`, and the squash commit uses it.
- Avoid vague tasks, edits to the same file from two parallel tasks, and dependencies between
  stories that break their independence.

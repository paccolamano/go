# Research: Service lifecycle (lifecycle)

Phase 0 of [plan.md](plan.md). Every decision below resolves an open point of the Technical
Context or records a design choice. The decisions agreed with the user (spec Clarifications) are
not reopened.

**Spike**: a scratch module, outside the repository, built and tested with Go 1.27 and
`go 1.26.0` in its `go.mod`, linted with the repository's golangci-lint configuration (v2.14.0).
It checked:

- the inference of the close adapter's type parameter from method values (R-12);
- `signal.Notify` inside a `testing/synctest` bubble (R-09);
- `http.Server.Serve` called after `Shutdown`, and `ListenAndServe` on an invalid port (R-11);
- a prototype of the stop sequence with an `http.Server` over an in-memory `net.Pipe` listener,
  inside `synctest` (R-07, R-08).

## R-01 Module and package layout

- **Decision**: one module, `github.com/paccolamano/go/lifecycle` (`lifecycle/go.mod`), with two
  packages:
  - `lifecycle`: the App, the component contract, the adapters, the errors;
  - `lifecycle/health`: the probe handler. It imports nothing from `lifecycle`.
- **Rationale**: Principle I asks for one purpose per module. Running a process and answering
  its probes are one purpose: the probes report the lifecycle's state, and the spec ties them
  together (FR-029, FR-038). A subpackage keeps the `health` API separate, and usable without the
  App, without a second module to version.
- **Alternatives rejected**:
  - **Two modules** (`lifecycle`, `health`): two release lines for one feature, and the README of
    each would have to explain the other.
  - **One package**: mixes the App's options with the handler's, and `health.New` reads better
    than `lifecycle.NewHealthHandler`.

## R-02 Dependencies (Principle V)

- **Decision**: the standard library only, tests and examples included.
  - `lifecycle`: `context`, `errors`, `fmt`, `log/slog`, `net`, `net/http`, `os`, `os/signal`,
    `runtime/debug`, `sync`, `sync/atomic`, `time`.
  - `lifecycle/health`: `context`, `errors`, `fmt`, `log/slog`, `net/http`, `runtime/debug`,
    `strconv`, `strings`, `sync`, `time`, `unicode`, `unicode/utf8`.
- **Rationale**: FR-002, SC-007. The third-party systems of the compatibility table (FR-048) are
  reached through method values (R-12) and through prose in the README (R-21).
- **Alternatives rejected**: `golang.org/x/sync/errgroup`. The App needs ordered stops and a
  per-component outcome, which errgroup does not model, and `sync.WaitGroup` covers the rest.

## R-03 Component model

- **Decision**:
  - `Component` is an interface with one method, `Run(ctx) error`.
  - `Stopper`, with `Stop(ctx) error`, is optional and detected with a type assertion when the
    component's turn comes.
  - `Func` is a function type that implements `Component`, like `http.HandlerFunc`.
  - The adapters (R-11 to R-13) and `Func` implement an unexported `validate() error`. `Add`
    calls it, so that a nil function, a nil server or a nil listener is refused before anything is
    launched (FR-010).
- **Rationale**: agreed design (spec Clarifications). The optional interface keeps the common
  case, a function, at one method, as `io.WriterTo` does for `io.Writer`. The unexported
  validator reports invalid adapters through the App's configuration error, because the agreed
  adapter signatures return no error.
- **Alternatives rejected**:
  - **A required `Stop` method**: every worker would carry an empty method.
  - **Adapters that return `(Component, error)`**: the agreed signatures return a `Component`,
    and the error would be checked twice, once at the adapter and once at `Run`.
  - **Panicking adapters**, as `http.ServeMux.Handle` does: the spec requires errors, never panics
    (FR-003).

## R-04 Errors

- **Decision**:

  | Identifier | Meaning | FR |
  |------------|---------|----|
  | `ErrInvalidConfig` | invalid option, registration or adapter | FR-003, FR-010 |
  | `ErrAlreadyStarted` | `Run` called a second time | FR-011 |
  | `ErrExitedEarly` | a run returned no error before its stop was requested | FR-026 |
  | `ErrStopTimeout` | a run had not returned when the deadline expired | FR-024 |
  | `ErrForced` | a second signal forced the stop | FR-025 |
  | `ErrNotRunning` | readiness outside the running state, with the state in the message | FR-029 |
  | `*ComponentError` | `Name` and `Err`; wraps every per-component outcome | FR-026 |
  | `*PanicError` | `Value` and `Stack` of a recovered panic | FR-027 |

  - `Run` returns `errors.Join` of the outcomes, in this order: the trigger failure, later
    failures, stop errors and timeouts in stop order, the forced stop, refused registrations.
  - Stop errors are wrapped as `stop: <err>` inside the `ComponentError`, so that a reader tells
    them from run failures.
  - A stop error that is the stop context's own error, after the deadline has expired or the stop
    was forced, is not reported: the timeout or the forced stop already is.
  - Messages start with `lifecycle:`, as in the other modules. `ComponentError.Error` removes a
    leading `lifecycle: ` from the wrapped message, so that a wrapped sentinel reads
    `lifecycle: component "db": not stopped in time`, not with the prefix twice.
- **Rationale**: Principle IV (errors usable with `errors.Is` and `errors.As`), and the
  conventions of the repository (`ErrInvalidConfig` in `middleware`, `auth`, `sse`, `slogx`). A
  `ComponentError` lets a caller get the name with `errors.As`, without parsing messages.
- **Alternatives rejected**:
  - **One error type per outcome** (`*TimeoutError`, …): sentinels inside `ComponentError` give
    the same information with fewer types.
  - **An `Op` field in `ComponentError`**: the `stop:` prefix is enough, and a field would be one
    more thing to keep stable.

## R-05 Options and defaults

- **Decision**:
  - `Option` is `func(*config)` for `New`. `New` validates the result and reports an error
    wrapping `ErrInvalidConfig`.
  - `HTTPOption` is a separate function type for `HTTPServer`, with one option, `WithListener`.
    `validate` reports a nil listener.
  - `health.Option` is the handler's own type, with one option, `WithLogger`.
  - Defaults are unexported constants, documented on the options: stop timeout 15 s, drain delay
    0, check timeout 1 s, no signals, a discarding logger (`slog.DiscardHandler`).
  - Validation: `WithStopTimeout` > 0, `WithDrainDelay` ≥ 0, `WithLogger` non-nil, `WithSignals`
    non-empty with no nil signal. Signals named twice count once. The last value of a repeated
    option wins.
- **Rationale**: FR-003, the defaults in the spec's Assumptions, and the style of `sse` and
  `slogx`. A separate `HTTPOption` type makes passing an App option to the adapter a compile
  error, which the applicability checks of `sse` cannot be here, because `HTTPServer` returns no
  error.
- **Alternatives rejected**: exported default constants. They would become API that cannot
  change in a minor release, and the documentation already states the values.

## R-06 Running and states

- **Decision**:
  - The states are `starting` (before `Run`, and while launching), `running`, `stopping` (drain
    and stop) and `stopped`. They are kept in an atomic value, which `Ready` reads without a lock.
  - `Run`:
    1. moves from `starting` to launching with a compare-and-swap. A second call gets
       `ErrAlreadyStarted` at once;
    2. reports the registration errors, if any, and moves to `stopped` without launching
       anything;
    3. registers the signals (R-09) before launching, so that no signal is missed;
    4. launches every component in its own goroutine, in registration order. Each one gets
       `context.WithCancel(context.WithoutCancel(ctx))`: the values of the caller's context,
       never its cancellation (FR-013);
    5. moves to `running`, then waits for the first trigger: `ctx.Done()`, a signal, or a run
       that returns. Each run reports its return on a channel buffered for every component, so a
       run never blocks on it.
  - A component whose run returns before its own turn has come is a failure, or an exited-early
    outcome when it returned no error, whether this happens while running, during the drain, or
    while other components stop. The first one is the trigger. The later ones are reported
    (FR-026) and skipped by the stop sequence (FR-019).
  - A context already ended when `Run` is called is a trigger like any other: every component is
    launched, then stopped (spec Edge Cases).
  - `Add` after `Run` has started records the refusal, under the App's mutex. `Run` joins it to
    its result.
- **Rationale**: FR-011 to FR-015, FR-019, FR-029. Launching before acting on a trigger makes
  every component part of the stop sequence, so close operations always run.
- **Alternatives rejected**: deriving the components' contexts from the caller's context. Its
  cancellation would abort in-flight work at the trigger, against the stop contract (FR-020).

## R-07 Stop contract (spike)

- **Decision**: for each component, in reverse order, skipping the ones whose run has already
  returned:
  - **with a stop operation**: call `Stop` in a goroutine with the stop context, then wait until
    both `Stop` and `Run` have returned, or the stop context ends. The run's context stays valid
    until then (FR-020);
  - **without a stop operation**: cancel the run's context, then wait for `Run` or for the stop
    context (FR-021);
  - **when the stop context ends first**: cancel the run's context, record `ErrStopTimeout`, and
    switch to the expired path (R-08).
  - Waiting for both returns matters: the HTTP server's `Serve` returns `ErrServerClosed` as soon
    as `Shutdown` starts, while the requests in progress are still running (R-11).
- **Spike results** (`synctest`, `http.Server` over `net.Pipe`):
  - A request in progress that takes 3 s completed with its full body during the server's turn.
    The server's shutdown hook ran, and the worker registered before the server stopped after it.
    The server's turn lasted 3.16 s of virtual time: `Shutdown` polls for idle connections with an
    interval that grows up to 500 ms, so its end lags the last request by up to that interval.
  - The prototype first spun forever, because a received channel stayed in the `select`. The
    implementation sets each channel to nil once received.
- **Rationale**: FR-019 to FR-022. One goroutine per stop call keeps the wait bounded even when
  `Stop` blocks forever (FR-023).

## R-08 Bounded shutdown and the expired path (spike)

- **Decision**:
  - The stop context is `context.WithDeadline(context.WithoutCancel(ctx), end)` and
    `context.WithCancelCause` on top of it, where `end` is the end of the drain plus the stop
    timeout. A second signal cancels it with the cause `ErrForced`.
  - The drain waits on a timer, on the force, and on component returns, which are recorded as in
    R-06.
  - **Expired path**: once the stop context has ended, every component not yet handled receives,
    in reverse order, `Stop` in a goroutine with the expired context and the cancellation of its
    run's context. The App does not wait. Each one whose run has not returned at that moment is
    reported with `ErrStopTimeout` (FR-024).
  - `Run` unregisters the signals, moves to `stopped` and returns.
- **Spike results**: with a stop timeout of 5 s and a component that ignores cancellation, the
  sequence returned after exactly 5 s of virtual time. The component registered before it still
  received the cancellation and ran its code after the deadline, and both were reported as not
  stopped in time.
- **Spec refinement**: the goroutines of the components handled on the expired path may outlive
  `Run` by the time their operations take. FR-007 and SC-006 now say so (checklist, Iteration 3).
- **Rationale**: FR-023 to FR-025. The process must exit before the orchestrator's grace period,
  and the remaining components, such as a log flush, still get their chance.
- **Alternatives rejected**:
  - **Skipping the remaining components**: their resources would never be closed and their logs
    never flushed.
  - **A second, short grace period for them**: one more option, and `Run` would no longer be
    bounded by the stop timeout.

## R-09 Signals (spike)

- **Decision**:
  - When `WithSignals` is set, `Run` calls `signal.Notify` with a channel buffered for two
    signals before launching, and `signal.Stop` before returning. `signal.Stop` undoes this
    channel's registration only, so the process's previous handling, default or another
    `Notify` channel, applies again (FR-016).
  - Signals are counted: the first one received is a trigger, or does nothing if a stop is already
    in progress. The second one forces the stop, whatever the trigger was. A stop triggered by a
    component's failure is therefore not forced by the orchestrator's single terminate signal.
  - The App keeps `signal.Notify` and `signal.Stop` in two unexported fields, set by `New`. The
    internal tests replace them with functions that keep the channel, and deliver fake signals
    from inside the bubble.
  - One external test, built only on Unix, sends real `SIGUSR1` signals to the test process with
    `syscall.Kill`. A component reports when it runs, so the signal is sent after `Notify`
    without any sleep.
- **Spike results**: `signal.Notify` and `signal.Stop` on a channel created inside a `synctest`
  bubble end the test binary with `fatal error: select on synctest channel from outside bubble`,
  even when no signal is delivered. It is a fatal error of the runtime, not a panic, so it cannot
  be recovered.
- **Consequences**:
  - The library's tests never configure real signals inside a bubble.
  - The README warns developers to trigger the stop of an App through its context in their
    tests. FR-046 now requires it (checklist, Iteration 3).
- **Windows**: `os.Interrupt` (Ctrl-C) is delivered. `syscall.SIGTERM` compiles, but Windows does
  not send it to console programs. The README says so. No build constraint is needed in the
  library code.
- **Rationale**: FR-015, FR-016, FR-025, and Principle III (hermetic, deterministic tests).

## R-10 Panics

- **Decision**: the goroutines that call `Run` and `Stop` recover panics, and turn them into a
  `*PanicError` with the value and `debug.Stack()`, wrapped in the component's `ComponentError`.
  A panic in `Run` is a failure of the component. A panic in `Stop` is a stop error, and the App
  still waits for `Run` (FR-027). `health` recovers panics of checks in the same way and logs
  them (R-16).
- **Rationale**: FR-005, FR-027, FR-040. Recovering lets the other components stop in order. A
  panic in a goroutine started by a component itself cannot be recovered and ends the process, as
  the README says.
- **Lint**: `recover` is not forbidden. `forbidigo` forbids `panic`, which the module never calls.

## R-11 HTTP server adapter (spike)

- **Decision**:
  - **Run**: with a listener, `srv.Serve(ln)`, or `srv.ServeTLS(ln, "", "")` for TLS. Otherwise
    `ListenAndServe()` or `ListenAndServeTLS("", "")`. `http.ErrServerClosed` becomes nil. Any
    other error is wrapped (`lifecycle: http server: %w`).
  - **TLS** when `srv.TLSConfig` is not nil and has `Certificates`, `GetCertificate` or
    `GetConfigForClient` (FR-030).
  - **Stop**: `srv.Shutdown(ctx)`. When it returns because the context ended, `srv.Close()` closes
    the remaining connections. Both errors are joined and wrapped. The App does not report the
    context's own error (R-04).
  - The adapter keeps no state besides the server and the listener. `Shutdown` runs the server's
    shutdown hooks (`RegisterOnShutdown`), which is where the `sse` broker's `Close` goes (FR-051).
- **Spike results**:
  - `Serve(ln)` called after `Shutdown` returns `http.ErrServerClosed` at once and closes `ln`. A
    stop that comes before the server's goroutine has called `Serve` therefore neither leaks the
    listener nor serves.
  - `ListenAndServe` with `Addr: "127.0.0.1:-1"` returns `listen tcp: address -1: invalid port`
    without opening a socket. The test of a failing server (User Story 3, scenario 4) uses it,
    which keeps the suite hermetic.
- **Rationale**: FR-030, FR-031, User Story 2 and User Story 3.
- **Alternatives rejected**: binding the listener in the adapter and exposing its address
  (Clarifications Q2: B chose a listener supplied by the developer).

## R-12 Close adapter (spike)

- **Decision**:

  ```go
  func Closer[F func() | func() error | func(context.Context) error](f F) Component
  ```

  - The constraint is written inline: no exported constraint type.
  - A type switch on `any(f)` picks the call. The `default` branch returns an error and is
    unreachable for the three types of the set.
  - `validate` reports a nil function, of any of the three types.
  - Run waits until the stop or the cancellation of its context, and returns nil. Stop calls the
    function once (`sync.Once`), passing the stop context to the third shape, then releases Run.
  - Without `~` in the constraint, a named function type such as `context.CancelFunc` must be
    converted, `lifecycle.Closer(func() { cancel() })`. The documentation shows it.
- **Spike results**: `Closer(p.Close)` with `func()`, `Closer(db.Close)` with `func() error` and
  `Closer(tp.Shutdown)` with `func(context.Context) error` all infer `F`. Nil detection works for
  each type. The repository's lint reports nothing about the generic function.
- **Rationale**: FR-032, FR-034. Method values of `*sql.DB`, `pgxpool.Pool`, the go-redis client,
  OpenTelemetry's tracer provider and the `slogx` handler fit without a closure.
- **Alternatives rejected**:
  - **`~func()` in the constraint**: the type switch cannot match named types, and a conversion
    from a mixed type set is not allowed, so named types would need reflection.
  - **Three named adapters** (`CloseFunc`, `CloseErrFunc`, …): three identifiers for one idea.

## R-13 Start/stop adapter

- **Decision**: `StartStop(start, stop func(context.Context) error) Component`.
  - Run calls `start` with the component's context. A start error is returned wrapped, so the
    component fails and its stop is skipped (FR-019, FR-033). After a successful start, Run waits
    until the stop or the cancellation of its context.
  - Stop calls `stop` with the stop context, then releases Run.
  - `validate` reports a nil `start` or `stop`.
- **Rationale**: FR-033. river's `Start(ctx)` and `Stop(ctx)` fit as method values. robfig/cron
  v3 fits with two closures, shown in the README.

## R-14 Readiness

- **Decision**: `(*App).Ready(ctx context.Context) error` returns nil in the `running` state, and
  otherwise `fmt.Errorf("%w: %s", ErrNotRunning, state)`, where `state` is `starting`, `stopping`
  or `stopped`. It ignores its context, which is there to match the shape of a health check.
- **Rationale**: FR-029, FR-038. The drain is part of `stopping`, so readiness fails from the
  trigger on (FR-017).

## R-15 Logging

- **Decision**: with `WithLogger`, the App logs through `LogAttrs` with the caller's context:

  | Event | Level | Attributes |
  |-------|-------|------------|
  | stop triggered | Info | `trigger` (`context`, `signal` or `component`), `signal` or `component` |
  | drain started | Info | `delay` |
  | component stopped | Info | `component`, `duration` |
  | component failed, exited early, stop error | Error | `component`, `error` |
  | component not stopped in time | Error | `component`, `duration` |
  | stop forced | Warn | none |
  | registration refused | Error | `component`, `error` |

  Messages start with `lifecycle:`. Without a logger, a discarding logger is used, so the code has
  no nil checks.
- **Rationale**: FR-028. The caller's context carries the attributes that `slogx`'s context
  handler extracts, such as a service name.

## R-16 Health: shared evaluations

- **Decision**:
  - The handler keeps, under one mutex, the evaluation in progress, if any, and one flag per
    check telling whether its operation is still running.
  - **A request** joins the evaluation in progress, or starts one, and counts itself as a waiter.
    It then waits for the evaluation to complete or for its own context to end. A request that
    leaves decrements the waiters. The last one to leave cancels the evaluation and detaches it,
    so that the next request starts a new one (FR-039, FR-041).
  - **An evaluation** derives its context from `context.WithoutCancel` of the first request's
    context. For each check:
    - a check whose operation is still running from an earlier evaluation is not started: it
      fails, logged as still running;
    - otherwise one goroutine runs the operation, with a context bounded by the check's timeout,
      clears the flag when it returns, and recovers panics;
    - a second goroutine waits for the operation's result or for the timeout, whichever comes
      first, and records the outcome. When the last outcome is recorded, the evaluation logs its
      failures once, unless it was abandoned, then completes and detaches itself.
  - Results are never kept after the evaluation completes.
- **Rationale**: Clarifications Q1: B. Each check runs at most once at a time per handler, so
  neither a flood of probes nor a stuck check accumulates work. Two goroutines per check keep the
  response bounded by the timeout even when the operation ignores its context (SC-005).
- **Known limit**: a request that arrives just after an evaluation was abandoned may find checks
  still running, cancelled but not yet returned, and report them as failed. Probers retry, and the
  README says so.
- **Alternatives rejected**: `golang.org/x/sync/singleflight`. It is a dependency, and it does not
  model the per-check "still running" rule.

## R-17 Health: HTTP responses

- **Decision**:
  - GET and HEAD run the evaluation. Any other method gets 405, `Allow: GET, HEAD` and the body
    `method not allowed`, and runs no check.
  - Headers: `Content-Type: text/plain; charset=utf-8`, `Cache-Control: no-store`,
    `X-Content-Type-Options: nosniff`, `Content-Length`. HEAD gets the same headers and no body.
  - The body is made of lines, each ended by a line feed:
    - every check passed, without `verbose`: `ok`;
    - otherwise one line per check, in the order given, `[+]<name> ok` or
      `[-]<name> failed: reason withheld`, then `ok` or `failed`.
  - `verbose` is detected with `r.URL.Query().Has("verbose")`, whatever its value.
  - A request whose context ends before the evaluation completes writes nothing.
- **Rationale**: FR-042, FR-043, FR-044. The format follows the Kubernetes API server's
  `/readyz`. Only check names, which the developer defines, ever reach the response.

## R-18 Health: check validation

- **Decision**: `health.New` reports `health.ErrInvalidConfig` for:
  - a name that is empty, not valid UTF-8, or contains a rune for which `unicode.IsSpace` or
    `unicode.IsControl` is true;
  - a name used twice;
  - a nil `Func`;
  - a negative `Timeout`. Zero means the default.

  A nil logger passed to `WithLogger` is invalid too.
- **Rationale**: FR-036. Names appear in a line-oriented response, so whitespace and control
  characters would break its format.

## R-19 Fuzzing and benchmarks (Principle III)

- **Decision**: no fuzz targets and no benchmarks.
  - The module parses no external input of its own. The only request input it reads is the
    `verbose` query parameter, decoded by `net/url`. Check names come from the developer and are
    validated at build time.
  - No code is declared performance-sensitive.
- **Rationale**: Principle III requires fuzz tests for code that parses or decodes external
  input, and benchmarks for performance-sensitive code. Neither applies.

## R-20 Testing approach (Principle III)

- **Decision**:
  - The standard `testing` package only. External test packages `lifecycle_test` and
    `health_test`, plus internal tests for the signal hooks, the states and the error
    composition.
  - **Virtual time**: drain delays, stop timeouts, check timeouts, and the HTTP server's
    `Shutdown` polling all run inside `testing/synctest`, so no test waits for real time
    (SC-009).
  - **No socket**: the HTTP adapter is tested with an in-memory listener whose `Dial` returns one
    end of a `net.Pipe`, as in `sse`. The listen failure uses an invalid port (R-11). The runnable
    example of the listener option uses the same in-memory listener. The README shows a listener
    bound to port 0 for developers' own end-to-end tests (spec refinement, checklist
    Iteration 3).
  - **Signals**: fake signals through the internal hooks inside bubbles, and one Unix-only test
    with real `SIGUSR1` signals outside them (R-09).
  - **Goroutine leaks** (SC-006): `synctest.Test` fails when goroutines of the bubble outlive the
    test, so every test proves that no task remains once the components have returned.
  - **Order** (SC-001): Apps of 1 to 50 components, with recording components, assert the exact
    reverse order and that each stop starts after the previous one has ended.
  - **HTTP** (SC-002): 100 concurrent requests in progress during a stop, and requests sent during
    a drain, over the in-memory listener.
  - **Concurrency**: `Add`, `Ready` and the health handler under `-race`, with
    `sync.WaitGroup.Go`.
  - **Health** (SC-005): 20 checks with mixed outcomes, check errors that contain host names and
    connection strings asserted absent from every response, and 100 concurrent requests during
    one evaluation that each check counts once.
  - Coverage target: ≥ 80% for the module.
- **Rationale**: the repository's hermeticity rules, and the precedent of `auth` and `sse`, which
  open no socket in tests.
- **Alternatives rejected**:
  - **`httptest.Server`**: it opens a real socket.
  - **An injected clock**: `synctest` virtualizes the standard library's timers, including those
    of `http.Server.Shutdown`.

## R-21 Documentation

- **Decision**:
  - **README** sections, in order:
    1. purpose, `go get`, minimum Go version;
    2. a complete `main`: `slogx` flush, tracer provider, database, cache, an `sse` feed loop, a
       worker, the HTTP server with `RegisterOnShutdown(broker.Close)`, readiness and liveness;
    3. **the model**: components, the ordering rule, resources opened by the developer, no
       restarts;
    4. **the stop contract**, with the shutdown timeline (FR-045);
    5. **bounded shutdown**, the second signal, reading the App's error with `errors.Is` and
       `errors.As`;
    6. **signals and testing** (FR-046): which signals, Windows, and testing through the context
       because of `synctest`;
    7. **compatibility table** (FR-048), then the components to write yourself (FR-049): the
       RabbitMQ consumer and its `ConsumeWithContext` alternative, gRPC, NATS;
    8. **blocking functions** (FR-050), with the listener accept loop;
    9. **this repository's modules** (FR-051);
    10. **health**: readiness and liveness, the format, reasons withheld, shared evaluations
        (FR-047, FR-052);
    11. **Kubernetes** (FR-052): drain delay or `preStop` sleep, the sum against
        `terminationGracePeriodSeconds`, probe configuration and timeouts;
    12. defaults.
  - Third-party snippets are code blocks only, marked as sketches with the client and version
    they were written against: pgx v5, go-redis v9, franz-go, amqp091-go v1.16, river,
    robfig/cron v3, OpenTelemetry Go, grpc-go and nats.go (FR-053).
  - **Package documentation**: `doc.go` in each package, with the flows in short form and links
    to the examples.
  - **Runnable examples** (FR-054):

    | Example | Package | Kind |
    |---------|---------|------|
    | `Example` | `lifecycle` | an App with two `Func` components, stopped by cancelling its context; prints the stop order |
    | `ExampleHTTPServer` | `lifecycle` | whole-file example with an in-memory listener given through `WithListener` |
    | `ExampleCloser` | `lifecycle` | three close shapes, closed in reverse order |
    | `ExampleStartStop` | `lifecycle` | a scheduler-like service with start and stop |
    | `ExampleStopper` | `lifecycle` | whole-file example: a consumer type whose `Stop` stops intake and whose `Run` drains what it has received |
    | `ExampleNew` | `health` | a handler with passing and failing checks, called with `httptest.ResponseRecorder` |
    | `Example_readiness` | `health` | readiness wired to `App.Ready`, liveness without checks |

    `Example_readiness` lives in `health`'s external test package and imports `lifecycle`, which
    is the same module, so it adds no dependency.
- **Rationale**: FR-045 to FR-054.

## R-22 Lint and security scanners

- **Spike results**: with the repository configuration, the prototype's findings were only of
  kinds the implementation avoids: missing documentation, errors of `Stop` and `Close` left
  unchecked, and test code.
- **Decision**:
  - Every error from the standard library is wrapped with context (`wrapcheck`).
  - No `panic` (`forbidigo`). The close adapter's unreachable branch returns an error (R-12).
  - Tests build requests with `http.NewRequestWithContext` (`noctx`).
  - Expected `nolint` directives: none.
  - A Semgrep rule about writing to a `ResponseWriter` may match the health handler's body. The
    body is `text/plain` with `nosniff`, made only of check names defined by the developer, so a
    match would be suppressed with `nosemgrep` and that justification.
  - Bearer: logs carry error values of checks and components, which are the developer's own. The
    README warns not to put secrets in error messages, which applies to any logger.
- **Rationale**: Principle IV and the repository's scanner rules.

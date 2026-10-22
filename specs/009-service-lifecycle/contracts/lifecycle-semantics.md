# Contract: Lifecycle semantics

Behavior of `App.Run`, the stop sequence and the adapters. States and data are in
[../data-model.md](../data-model.md), identifiers in [public-api.md](public-api.md).

## Launch (FR-010 to FR-014)

1. `Run` on an App already run, or running, returns `ErrAlreadyStarted` and changes nothing.
2. Invalid registrations: `Run` returns their errors, joined, launches nothing, and the App is
   stopped.
3. Configured signals are registered.
4. Every component is launched in registration order, each in its own goroutine, with a context
   that carries the values of `Run`'s context and is not cancelled by it. Each goroutine has
   called the component's `Run` before the next one is launched, so `Stop` never comes before
   `Run`, although it may come while `Run` is still starting.
5. The App is running: `Ready` returns nil.
6. Triggers are acted upon only now. A context already ended, or a run that returned during
   launch, starts the stop at once.

## Triggers (FR-015, FR-016, FR-025)

| Trigger | Logged as | Result of `Run` when nothing else goes wrong |
|---------|-----------|----------------------------------------------|
| `Run`'s context ends | `trigger=context` | nil |
| configured signal | `trigger=signal signal=<name>` | nil |
| a run returns an error | `trigger=component component=<name>` | `ComponentError{name, err}` |
| a run returns nil | same | `ComponentError{name, ErrExitedEarly}` |
| a run panics | same | `ComponentError{name, *PanicError}` |

Only the first trigger starts the stop. Signals are counted: the second one received forces the
stop, whatever the first trigger was.

## Drain (FR-017)

- `Ready` fails from the trigger on.
- The App waits for the drain delay. Nothing is stopped or cancelled.
- A second signal ends the drain at once and forces the stop.
- Runs that return during the drain are later failures (FR-026), and their components are skipped.

## Stop of one component (FR-018 to FR-022)

The stop context has the deadline `end of drain + stop timeout`, and is cancelled with the cause
`ErrForced` by a second signal.

| Component | Steps | Clean when |
|-----------|-------|------------|
| run already returned | skipped; its outcome was recorded when it returned | n/a |
| implements `Stopper` | 1. `Stop(stopCtx)` in its own goroutine; 2. wait until `Stop` **and** `Run` have returned, or `stopCtx` ends; the run's context stays valid | `Run` returns nil or `context.Canceled`, and `Stop` returns nil |
| no `Stopper` | 1. cancel the run's context; 2. wait for `Run`, or `stopCtx` ends | `Run` returns nil or `context.Canceled` |
| `stopCtx` ends first | cancel the run's context; record `ErrStopTimeout`; switch to the expired path | never |

- A `Stop` error is reported as `ComponentError{name, "stop: " + err}`, unless it is `stopCtx`'s
  own error after `stopCtx` has ended.
- A panic in `Stop` is a stop error. The App still waits for `Run`.
- A `Run` error other than `context.Canceled`, returned after the stop was requested, is reported
  as `ComponentError{name, err}`.

## Expired path (FR-023, FR-024)

Once `stopCtx` has ended, by deadline or by force:

- every component not yet handled receives, last registered first, `Stop(stopCtx)` in its own
  goroutine, if it implements `Stopper`, and the cancellation of its run's context;
- the App does not wait for them. Each one whose run has not returned at that moment is reported
  with `ErrStopTimeout`;
- `Run` unregisters the signals and returns.

`Run` returns no later than `drain delay + stop timeout` after the trigger, plus scheduling
overhead, whatever the components do. A forced stop returns at once.

## Result (FR-026)

`errors.Join` of, in this order:

1. the trigger's failure, if it was a component;
2. later failures, in the order they were seen;
3. stop errors and timeouts, in stop order;
4. `ErrForced`, if forced;
5. registrations refused after start.

Nil when the list is empty.

## Shutdown timeline (example)

Components registered in this order: `log` (Closer), `db` (Closer), `worker` (Func), `http`
(HTTPServer). Drain delay 10 s, stop timeout 15 s.

```text
t=0     SIGTERM            Ready → ErrNotRunning: stopping; readiness answers 503
t=0–10  drain              http keeps serving; worker keeps working
t=10    http               Shutdown: listeners closed, shutdown hooks run (sse broker Close),
                           requests in progress complete
t=12    worker             run context cancelled; worker returns
t=12    db                 db.Close()
t=12    log                Flush(ctx)
t=12    Run returns nil
deadline: t=25. Had http not finished by then, its connections would be closed, it would be
reported with ErrStopTimeout, and worker, db and log would get their stop without waiting.
```

## Adapters (FR-030 to FR-033)

### `HTTPServer(srv, opts...)`

| Situation | Run | Stop |
|-----------|-----|------|
| no listener, no TLS certificate | `srv.ListenAndServe()` | `srv.Shutdown(ctx)`; `srv.Close()` if ctx ended |
| no listener, TLS certificate in `srv.TLSConfig` | `srv.ListenAndServeTLS("", "")` | same |
| `WithListener(ln)`, no TLS | `srv.Serve(ln)` | same; `ln` is closed by `Shutdown` |
| `WithListener(ln)`, TLS | `srv.ServeTLS(ln, "", "")` | same |

- `http.ErrServerClosed` from Run becomes nil. Other errors are wrapped.
- A TLS certificate means `TLSConfig.Certificates`, `GetCertificate` or `GetConfigForClient`.
- A stop that comes before `Serve` was called makes `Serve` return `ErrServerClosed` and close the
  listener.
- `Run` returns as soon as `Shutdown` starts. The App waits for `Stop`, which returns when the
  requests in progress have completed, or at the deadline. `Shutdown` checks for idle connections
  at intervals of up to 500 ms, so a server's turn can last up to that much after its last
  request ends.

### `Closer(f)`

- Run waits until Stop has run, or until its context is cancelled, then returns nil.
- Stop calls `f` once: `f()`, `f()` returning its error, or `f(stopCtx)`. Its error is the stop
  error.

### `StartStop(start, stop)`

- Run calls `start(runCtx)`. An error is returned wrapped: the component fails and is not stopped.
  After a successful start, Run waits until Stop has run, or until its context is cancelled.
- Stop first waits until `start` has returned, or until `stopCtx` ends, so that a stop triggered
  right after launch never reaches the service before it has started. It does not call `stop`
  when `start` failed. Otherwise it calls `stop(stopCtx)`, whose error is the stop error.

## Signals (FR-016, FR-046)

- Registered with `signal.Notify` on a channel buffered for two signals, before launch, and
  unregistered with `signal.Stop` before `Run` returns. The process's previous handling then
  applies again.
- No signal is handled without `WithSignals`.
- On Windows, `os.Interrupt` is delivered. `syscall.SIGTERM` compiles but is not sent to console
  programs.
- Inside a `testing/synctest` bubble, registering signals is a fatal error of the runtime. Tests
  stop an App through its context.

## Logging (FR-028)

See [research R-15](../research.md#r-15-logging) for the events, levels and attributes. Nothing
is logged without `WithLogger`.

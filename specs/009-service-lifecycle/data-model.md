# Data Model: Service lifecycle (lifecycle)

Phase 1 of [plan.md](plan.md). The exported API is in
[contracts/public-api.md](contracts/public-api.md). This file describes the values, their
validation and their state machines.

## Package `lifecycle`

### App configuration

Built by `New` from the options. Immutable once `New` has returned.

| Field | Option | Default | Validation (error wraps `ErrInvalidConfig`) |
|-------|--------|---------|---------------------------------------------|
| signals | `WithSignals(sig...)` | none | at least one signal, none nil; duplicates count once |
| drain delay | `WithDrainDelay(d)` | 0 | `d >= 0` |
| stop timeout | `WithStopTimeout(d)` | 15 s | `d > 0` |
| logger | `WithLogger(l)` | discarding logger | `l != nil` |

A repeated option keeps its last value.

### Registration

One entry per call to `Add`, in call order.

| Field | Meaning |
|-------|---------|
| name | non-empty, unique within the App |
| component | the `Component`; its optional `Stopper` is detected at its turn |
| run context | `context.WithCancel(context.WithoutCancel(ctx of Run))`, created at launch |
| run outcome | the error returned by `Run`, or a `*PanicError`; set when `Run` returns |
| run returned | set when `Run` returns, read by the stop sequence |

Validation, at `Add`, recorded and reported by `Run` before launching anything (FR-010):

- empty name: `lifecycle: invalid configuration: component at position N: empty name`;
- duplicate name: `ComponentError{Name, ErrInvalidConfig: name already used}`;
- nil component, a nil `Func`, or an adapter whose `validate` fails (nil server, nil listener, nil
  function): `ComponentError{Name, ErrInvalidConfig: …}`.

A call to `Add` after `Run` has started is not registered. It is recorded as
`ComponentError{Name, ErrInvalidConfig: registered after start}`, logged at once, and joined to
the result of `Run`.

### App state

```text
            Run (valid)                 every component launched
starting ───────────────▶ launching ─────────────────────────▶ running
   │                      (still "starting" for Ready)            │
   │ Run (invalid registrations)                                  │ trigger
   ▼                                                              ▼
stopped ◀──────────────────────────────────────────────────── stopping
              stop sequence completed, or deadline expired      (drain, then stops)
```

| State | `Ready` | Entered when |
|-------|---------|--------------|
| starting | `ErrNotRunning: starting` | `New` returns; also while `Run` launches |
| running | nil | every component has been launched |
| stopping | `ErrNotRunning: stopping` | the first trigger (context, signal, component return) |
| stopped | `ErrNotRunning: stopped` | `Run` is about to return |

A second call to `Run`, in any state but `starting` before the first call, returns
`ErrAlreadyStarted` and changes nothing.

### Triggers and signals

| Event | While running | While stopping |
|-------|---------------|----------------|
| context of `Run` ends | trigger | ignored |
| configured signal | trigger; counts as the first signal | the first signal received is ignored, the second forces the stop (`ErrForced`) |
| a run returns, before its turn | trigger; outcome is the failure, or `ErrExitedEarly` for nil | reported as a later failure; component skipped |

Signals are counted from the first one received, whatever the trigger was (research R-09). A stop
triggered by the context or by a failure is therefore forced only by the second signal.

### Stop sequence

```text
trigger ─▶ stopping ─▶ drain delay ─▶ for each component, last registered first:
                       (timer, or      ├─ already returned?           → skip
                        2nd signal)    ├─ has Stop? → Stop(stopCtx) ∥ wait Run and Stop
                                       │              until stopCtx ends
                                       ├─ no Stop?  → cancel run ctx, wait Run
                                       │              until stopCtx ends
                                       └─ stopCtx ended first → cancel run ctx,
                                            ErrStopTimeout, expired path:
                                            remaining: Stop(expired ctx) ∥ cancel, no wait
          ─▶ signal.Stop ─▶ stopped ─▶ Run returns errors.Join(outcomes)
```

- `stopCtx` has the deadline `end of drain + stop timeout`. A second signal cancels it with the
  cause `ErrForced`, and also ends the drain at once.
- A stop error equal to `stopCtx`'s own error is not reported (research R-04).

### Outcomes and the returned error

| Outcome | Form inside the joined error |
|---------|------------------------------|
| trigger failure | `*ComponentError{Name, err}`, or `{Name, *PanicError}`, or `{Name, ErrExitedEarly}` |
| later failure | same forms |
| stop error | `*ComponentError{Name, "stop: " + err}`, or `{Name, "stop: " + *PanicError}` |
| not stopped in time | `*ComponentError{Name, ErrStopTimeout}` |
| forced | `ErrForced` |
| refused registration | `*ComponentError{Name, ErrInvalidConfig …}` |

`Run` returns nil when the trigger was the context or a signal and the list is empty.

### Adapters

| Adapter | State | Run | Stop | `validate` |
|---------|-------|-----|------|------------|
| `Func` | none | calls the function | none | function not nil |
| `HTTPServer(srv, opts...)` | server, optional listener | `Serve`/`ServeTLS`/`ListenAndServe`/`ListenAndServeTLS`; `ErrServerClosed` → nil | `Shutdown(ctx)`; `Close` when ctx ended | server not nil; listener not nil when given |
| `Closer(f)` | function, once, released channel | waits for the release or its context | calls f once, then releases Run | f not nil |
| `StartStop(start, stop)` | functions, once, released channel | calls start; on success waits for the release or its context | calls stop, then releases Run | start and stop not nil |

## Package `lifecycle/health`

### Check

| Field | Meaning | Validation (error wraps `health.ErrInvalidConfig`) |
|-------|---------|----------------------------------------------------|
| Name | shown in responses and logs | non-empty, valid UTF-8, no whitespace or control rune, unique |
| Func | the operation | not nil |
| Timeout | per-check bound | `>= 0`; 0 means 1 s |

### Handler

| Field | Meaning |
|-------|---------|
| checks | the validated checks, in the order given |
| logger | from `WithLogger`, or a discarding logger |
| current | the evaluation in progress, or none |
| running | per check: its operation is still running, possibly from an earlier evaluation |

### Evaluation

| Field | Meaning |
|-------|---------|
| context | `context.WithCancel(context.WithoutCancel(first request's ctx))` |
| waiters | requests waiting for it |
| outcomes | per check: passed, failed with an error, timed out, panicked, or still running |
| remaining | checks whose outcome is not recorded yet |
| done | closed when every outcome is recorded |
| abandoned | set when the last waiter leaves before `done` |

```text
request ─▶ current evaluation? ── yes ─▶ join (waiters+1)
               │ no
               ▼
           start: for each check
             running? → outcome "still running"
             else     → mark running; run operation (timeout ctx, recover);
                        watcher records result or timeout
           ─▶ last outcome recorded → log failures (unless abandoned) → close done → detach

waiter leaves before done ─▶ waiters-1 ─▶ 0? → cancel context, mark abandoned, detach
operation returns ─▶ clear running flag
```

A check passes only with the outcome "passed". The response is computed from the outcomes and the
request's `verbose` parameter ([contracts/health-http.md](contracts/health-http.md)).

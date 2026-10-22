# Feature Specification: Service lifecycle (lifecycle)

**Feature Branch**: `009-service-lifecycle` (suggested; not yet created)

**Created**: 2026-10-22

**Status**: Draft

**Input**: User description: "ok direi allora di procedere per scrivere la spec. 0 di default va
bene". The design was agreed in conversation; the full request, as submitted, is reproduced
verbatim in [Appendix: original request](#appendix-original-request).

## Context

Every Go service runs the same few things side by side: one or more servers, some background
workers or queue consumers, and the clients of its databases and caches. Starting them is easy.
Stopping them well is not, and every service writes its own `main` that gets some of these wrong:

- the server is shut down as soon as the termination signal arrives, while the load balancer
  still sends traffic for a few seconds, so every rollout produces failed requests;
- the database is closed while handlers and workers are still using it;
- a worker is killed in the middle of a job, and the job is lost or processed twice;
- long-lived responses, such as event streams, make the server's graceful shutdown wait for its
  whole deadline;
- a second Ctrl-C is swallowed while a shutdown hangs, and the process has to be killed;
- a background worker dies silently while the server keeps answering, so the process looks
  healthy and does nothing;
- the shutdown takes longer than the orchestrator's grace period, the process is killed, and logs
  and traces that were not flushed are lost;
- the liveness probe checks the database, so a database outage restarts every replica in a loop;
- a readiness endpoint answers with the error of a failed check, which discloses host names or
  connection strings.

None of these is solved by a function of a few lines, which is why they belong in a library.

This feature delivers a new, independent module in this repository (Constitution, Principle I),
named `lifecycle`, with two packages:

- **lifecycle**: runs the components of a process (servers, workers, consumers, resources) from
  start to graceful shutdown: launched in registration order, stopped one at a time in reverse
  order, with a drain period, a bounded deadline, and a stop contract that lets in-flight work
  complete;
- **health**: an HTTP handler that runs named checks and answers readiness and liveness probes,
  without disclosing why a check failed.

**The library does not open resources, does not restart anything and does not know any client.**
The developer opens databases and clients in their own code, with ordinary error handling, and
gives the library only the way to stop or close them. A component that stops on its own stops
the whole process: restarting is the orchestrator's job. The library never imports a database,
cache or message bus client; method values of those clients fit its adapters directly.

The Go language, the standard library's HTTP server and the operating system's signals are given
by the request and by the constitution (v1.2.0). They are not design choices made here. They
appear in this spec because the shutdown behavior and the probe responses are the observable
contract of this library. How the module is built is left to `/speckit-plan`.

**Actors**:

- **Developer**: writes the service's `main`, registers its components, writes components of
  their own, and wires the probe endpoints.
- **Operator**: runs the service under an orchestrator such as Kubernetes, configures the probes,
  the grace period and the rollouts.
- **Maintainer**: evolves the module and publishes its releases.

### Clarifications

#### Session 2026-10-22

Decisions taken with the user while designing the module, before this spec was written:

- Q: How is a component described? → A: By a run operation that blocks until the component stops,
  plus an optional stop operation that receives a deadline. A function with the run signature is
  a component on its own (FR-008, FR-009).
- Q: In which order are components stopped? → A: One at a time, in strict reverse registration
  order. No parallel groups (FR-018).
- Q: What does stopping a component mean? → A: The stop operation means "stop accepting new work".
  The App then waits for the run operation to return, and only when the deadline expires does it
  cancel the run's context, which means "time is up, abort". A component without a stop operation
  receives the cancellation at the start of its turn (FR-020, FR-021).
- Q: Does the App handle operating system signals by default? → A: No. Signals are opt-in, so the
  library imposes no process-level behavior unless asked (FR-016).
- Q: What is the default drain delay? → A: Zero. Operators behind a load balancer set it, or use a
  pre-stop sleep of their orchestrator, and the documentation makes this prominent (FR-017,
  FR-052).
- Q: Where do readiness and liveness live? → A: In a subpackage, `health`, that does not depend on
  the `lifecycle` package. The App's readiness is passed to it as one more check (FR-038, FR-047).
- Q: Are readiness and liveness two different handlers? → A: The same handler, with different
  checks. The documentation explains which checks belong to each (FR-052).
- Q: When several requests reach a health handler while its checks run, or in quick succession,
  what happens? → A: Requests that arrive while an evaluation is in progress share it. A result is
  never reused once its evaluation has completed, and no check runs twice at the same time
  (FR-041).
- Q: Should the HTTP server adapter accept a listener supplied by the developer? → A: Yes, as an
  optional setting of the adapter, for tests on port 0, socket activation and pre-bound listeners
  (FR-030).

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Run a service and stop it in order (Priority: P1)

A developer writes the `main` of a service: they open the database and the cache clients, then
register, in this order, the flush of their logs, the closing of the database and of the cache,
a background worker and the HTTP server, and run the App. When the stop is requested, the App
stops the server first, then the worker, then closes the cache and the database, and flushes the
logs last. The developer never writes the sequence by hand.

**Why this priority**: it is the foundation of the module. On its own it already removes the most
common mistakes: resources closed while still in use, and a shutdown sequence that every service
rewrites.

**Independent Test**: register components that record when they run and stop, cancel the context
passed to the App, and check the order of the stops and the result. Repeat with a configured
signal.

**Acceptance Scenarios**:

1. **Given** components A, B and C registered in this order, **When** the context passed to the
   App is cancelled, **Then** C is stopped first, then B, then A, each only after the previous one
   has stopped, and the App returns no error.
2. **Given** an App configured to handle the interrupt and terminate signals, **When** the process
   receives the terminate signal, **Then** the same sequence runs and the App returns no error.
3. **Given** an App configured without signals, **When** the process receives the interrupt
   signal, **Then** the App does not intercept it, and the process's existing handling of that
   signal applies.
4. **Given** a function registered as a component that returns when its context is cancelled,
   **When** the stop sequence reaches it, **Then** its context is cancelled and, once it has
   returned, the App moves on to the previous component.
5. **Given** a running App, **When** the context passed to the App is cancelled, **Then** the
   components' contexts are not cancelled by that alone: each one is stopped only in its turn.
6. **Given** a value stored in the context passed to the App, **When** a component reads its own
   context, **Then** it finds that value.
7. **Given** an App that is running or has already run, **When** it is run again, **Then** the
   second run returns at once with a distinguishable error and affects nothing.

---

### User Story 2 - Let in-flight work complete before stopping (Priority: P1)

A developer has a queue consumer and an HTTP server. When the consumer's turn comes, it stops
receiving new messages, finishes the ones it has already received and returns. When the server's
turn comes, it stops accepting connections and lets the requests in progress complete. Only a
component that has not finished when the deadline expires is aborted.

**Why this priority**: without it, a graceful shutdown either waits for nothing, and work is lost
or done twice, or waits forever. The two-step contract is what makes the module usable for
consumers, schedulers and servers alike.

**Independent Test**: register a component whose stop operation only stops intake and whose run
keeps working on an item; trigger the stop and check that the item completes with a valid
context. Repeat with the HTTP server adapter and a request in progress, and with work that
outlasts the deadline.

**Acceptance Scenarios**:

1. **Given** a component with a stop operation whose run is processing an item, **When** its turn
   comes, **Then** its stop operation is called, its run's context stays valid, and the item
   completes before the run returns.
2. **Given** a component whose stop operation returns at once and whose run returns 2 seconds
   later, with 15 seconds left before the deadline, **When** its turn comes, **Then** the App
   waits for the run, never cancels its context, and moves on to the previous component.
3. **Given** a component whose run has not returned when the deadline expires, **When** the
   deadline expires, **Then** its run's context is cancelled, and the component is reported as
   not stopped in time.
4. **Given** the HTTP server adapter with a request in progress that takes 3 seconds, **When** its
   turn comes, **Then** new connections are refused, the request in progress completes
   successfully, and nothing is reported.
5. **Given** the HTTP server adapter with a request in progress that outlasts the deadline,
   **When** the deadline expires, **Then** the remaining connections are closed and the server is
   reported as not stopped in time.
6. **Given** a shutdown hook registered on the HTTP server, such as the closing of this
   repository's event broker, **When** the server's turn comes, **Then** the hook runs as the
   server stops accepting connections.
7. **Given** the closing of the database registered first through the close adapter, **When** the
   stop sequence runs, **Then** the database is closed exactly once, after every component
   registered later has stopped.
8. **Given** a service with separate start and stop operations registered through the start/stop
   adapter, **When** the App runs, **Then** start is called at launch, and **When** its turn
   comes, **Then** stop is called with the deadline.
9. **Given** the HTTP server adapter given a listener supplied by the developer, such as one bound
   to port 0 or an in-memory one, **When** the App runs, **Then** the server serves on that
   listener and a client reaches it through that listener. **When** the server's turn comes,
   **Then** the listener is closed.

---

### User Story 3 - Stop the whole process when a component fails (Priority: P1)

A background worker fails, or returns when nobody asked it to. Instead of leaving a process that
answers its probes and does nothing, the App stops every other component in order and returns an
error that names the worker. The orchestrator restarts the process.

**Why this priority**: a component that dies silently is the failure that monitoring misses most
often. Stopping everything in order is what lets the orchestrator recover, and it still flushes
logs and closes resources on the way out.

**Independent Test**: register several components, make one return an error, return no error, or
panic, and check that the others are stopped in reverse order and that the App's error names it.

**Acceptance Scenarios**:

1. **Given** components A, B and C running, **When** B's run returns an error, **Then** the stop
   sequence starts, C is stopped and then A, B is not stopped again, and the App returns an error
   that names B and wraps its error.
2. **Given** components A, B and C running, **When** B's run returns no error before any stop was
   requested, **Then** the same sequence runs and the App's error names B with a distinguishable
   exited-early error.
3. **Given** a component whose run panics, **When** the panic happens, **Then** it is recovered,
   handled as a failure of that component, and the App's error carries the panic value and the
   stack trace.
4. **Given** the HTTP server adapter configured on an address already in use, **When** the App
   runs, **Then** the server fails at once, the other components are stopped in reverse order,
   and the App's error names the server and wraps the listening error.
5. **Given** two components that fail at nearly the same time, **When** the first failure starts
   the stop sequence, **Then** no second sequence starts, and the App's error reports both
   failures.
6. **Given** a failure, **When** the stop sequence starts, **Then** the App's readiness reports
   that it is not running from that moment.

---

### User Story 4 - Drain before shutdown behind a load balancer (Priority: P2)

An operator runs the service on Kubernetes. When a pod receives the terminate signal, the
orchestrator removes it from the load balancer at the same time, and requests keep arriving for a
few seconds. The developer configures a drain delay of 10 seconds: the readiness probe fails at
once, the service keeps serving normally for 10 seconds, and only then are the components
stopped. Rollouts produce no failed requests.

**Why this priority**: it is the single most common cause of errors during rollouts, but it only
matters behind a load balancer, and the default is no delay.

**Independent Test**: configure a drain delay, wire a readiness handler to the App's readiness,
trigger the stop, and check that readiness fails at once while requests are still served until
the delay ends. Time is simulated.

**Acceptance Scenarios**:

1. **Given** an App with a drain delay of 10 seconds and a readiness handler wired to its
   readiness, **When** the terminate signal arrives, **Then** readiness answers 503 at once, every
   component keeps running and serving for 10 seconds, and then the stop sequence starts.
2. **Given** an App with the default drain delay, **When** a stop is triggered, **Then** the stop
   sequence starts at once.
3. **Given** a drain in progress, **When** a second configured signal arrives, **Then** the drain
   ends at once and the shutdown is forced (User Story 5).
4. **Given** an App, **When** its readiness is asked before it runs, while it runs, during the
   drain and stop, and after it has returned, **Then** it reports no error only while it runs,
   and otherwise a distinguishable not-running error that names the state.

---

### User Story 5 - Keep the shutdown bounded (Priority: P2)

An operator knows that the orchestrator kills the process when its grace period expires. The
developer sets a stop timeout below it. Whatever the components do, even one that ignores
cancellation, the App returns before the deadline, reports what did not stop in time, and still
gives the remaining components, such as the log flush, their stop. A second Ctrl-C makes it
return at once.

**Why this priority**: a shutdown that hangs is killed from outside, and loses exactly the logs
and traces needed to understand it. It is a safety net rather than the main flow.

**Independent Test**: register components that ignore cancellation, or whose stop operation never
returns, and check with simulated time that the App returns at the deadline with the right
report, and that later components still received their stop.

**Acceptance Scenarios**:

1. **Given** a stop timeout of 5 seconds and a component that ignores cancellation, **When** the
   stop sequence runs, **Then** the App returns no later than 5 seconds after the drain ended,
   its error names that component as not stopped in time, and the components registered before
   it still received their stop operation and the cancellation of their context.
2. **Given** a component whose stop operation never returns, **When** its turn comes, **Then** the
   same outcome as scenario 1 applies.
3. **Given** a first component that uses the whole deadline, **When** the deadline expires,
   **Then** every remaining component still receives its stop operation, with an expired
   deadline, and the cancellation of its context, the App does not wait for them, and it returns
   at the deadline.
4. **Given** a stop sequence in progress started by a signal, **When** a second configured signal
   arrives, **Then** the App returns promptly with a distinguishable forced-stop error, and every
   remaining component receives its stop operation and the cancellation of its context.
5. **Given** an App that has returned, **When** a signal it was configured to handle arrives,
   **Then** the process's previous handling of that signal applies again.

---

### User Story 6 - Expose readiness and liveness probes (Priority: P2)

A developer serves a readiness endpoint that checks the App's state, the database and the cache,
and a liveness endpoint with no checks. The orchestrator stops sending traffic while a dependency
is down, without restarting the pod, and restarts it only when the process stops answering. When
a check fails, the operator sees in the response which one failed, and finds why in the logs: the
response never carries the error.

**Why this priority**: probes are needed by almost every service, but they are useful on their
own, without the App, and the App works without them.

**Independent Test**: build handlers with checks that pass, fail, block or panic, call them with
an HTTP test client, and check statuses, bodies, timing and logs. Use the handler without any
App.

**Acceptance Scenarios**:

1. **Given** a readiness handler with the checks `lifecycle`, `postgres` and `redis`, all passing,
   **When** it is called, **Then** it answers 200 with the body `ok`.
2. **Given** the same handler with `redis` failing with an error that contains a host name,
   **When** it is called, **Then** it answers 503 with the lines `[+]lifecycle ok`,
   `[+]postgres ok`, `[-]redis failed: reason withheld` and `failed`, the host name appears
   nowhere in the response, and the failure is logged with the check's name and its error.
3. **Given** a handler whose checks all pass, **When** it is called with the `verbose` query
   parameter, **Then** it answers 200 with one line per check and the final line `ok`.
4. **Given** a check that blocks beyond its timeout of 1 second, **When** the handler is called,
   **Then** it answers within that timeout, plus negligible overhead, with that check failed.
5. **Given** a liveness handler with no checks, **When** it is called at any time, including
   during the App's drain, **Then** it answers 200 with the body `ok`.
6. **Given** a check that panics, **When** the handler is called, **Then** that check fails and
   the handler keeps answering later calls.
7. **Given** a handler, **When** it receives a HEAD request, **Then** it answers the same status
   without a body. **When** it receives any method other than GET and HEAD, **Then** it answers
   405 with `Allow: GET, HEAD` and runs no check.
8. **Given** checks with an empty name, two checks with the same name, or a check without its
   operation, **When** the handler is built, **Then** an error that identifies an invalid
   configuration is returned.
9. **Given** a prober that disconnects while checks run, and no other request waiting for the same
   evaluation, **When** it disconnects, **Then** the contexts of the running checks are cancelled.
10. **Given** 100 requests that arrive while one evaluation is in progress, **When** it completes,
    **Then** each check has run once, and every request receives that evaluation's result.
11. **Given** a request that arrives after an evaluation has completed, **When** it is handled,
    **Then** a new evaluation runs: no earlier result is reused.

---

### User Story 7 - Integrate any kind of component by following the documentation (Priority: P2)

A developer whose service uses a database pool, a cache, a message broker, a scheduler, a gRPC
server or this repository's event broker finds in the documentation how to register each of
them, which adapter fits, and what to write when none does. A developer with a function that
blocks learns how it can be stopped, and what happens when it cannot.

**Why this priority**: the module is only as useful as the range of components it can stop
correctly. Most of that range is covered by documentation and small developer-written types,
not by more API.

**Independent Test**: follow the README to register each system of the compatibility table and
check that the snippets match the agreed adapters and stop contract. The runnable examples pass.

**Acceptance Scenarios**:

1. **Given** the README, **When** a developer looks for a system listed in the compatibility table
   (FR-048), **Then** they find how to register it and the pitfall that applies to it, if any.
2. **Given** a RabbitMQ consumer written as the README shows, **When** its turn comes, **Then**
   the broker stops delivering to it, the messages already received are processed, and no
   message is lost.
3. **Given** a function that blocks without taking a context but can be unblocked by another
   operation, such as accepting connections on a listener, **When** the developer reads the
   README, **Then** they find how to write a component whose stop operation unblocks it.
4. **Given** a function that nothing can unblock, **When** the developer reads the README,
   **Then** they learn that it cannot be stopped, and that the App reports it as not stopped in
   time and moves on.
5. **Given** a service that uses this repository's event broker, **When** the developer reads the
   README, **Then** they find that the broker's closing is registered as a shutdown hook of the
   HTTP server, not as a component, and why.
6. **Given** a Kubernetes deployment, **When** the operator reads the README, **Then** they find
   how to set the drain delay or a pre-stop sleep, how the drain delay and the stop timeout relate
   to the grace period, and how to configure the probes.

### Edge Cases

- **No components**: an App with no components is valid. It runs until a stop is triggered, then
  returns no error.
- **Context already ended**: when the context passed to the App has already ended, every
  component is launched and then stopped as for any other trigger, so close operations still
  run.
- **Failure during launch**: launching completes before any trigger is acted upon, so every
  registered component is part of the stop sequence.
- **Invalid registration**: an empty name, a name already used, or a missing component makes the
  App refuse to run, before launching anything.
- **Registration after the App has started**: refused. The component is never launched, and the
  App reports the refusal when it returns.
- **Component already returned**: a component whose run has already returned, such as the one
  whose failure started the stop, is not stopped again: its stop operation is not called.
- **Stop operation fails**: its error is reported with the component's name, and the App still
  waits for the run to return.
- **Run returns a cancellation error after a requested stop**: a clean stop, not reported. Any
  other error returned after a requested stop is reported.
- **Run returns after the deadline**: reported as not stopped in time, even though it returned.
- **Same signal configured twice**: handled once.
- **Signals and Windows**: the interrupt signal works on every supported platform. The terminate
  signal compiles everywhere but is rarely sent on Windows. The documentation says so.
- **Two HTTP servers**: each one is its own component, stopped in its turn.
- **Health check without timeout**: the default timeout applies.
- **Health check that ignores its context**: the response is sent when its timeout expires, with
  the check failed. Its task ends when the check returns. Until then the check is not started
  again: later evaluations count it as timed out, so stuck checks never accumulate tasks.
- **Listener given to the HTTP server adapter**: the server serves on it instead of its configured
  address, and closes it when it stops.
- **Health handler without checks**: answers 200 with the body `ok`.
- **Probe timeout shorter than a check's timeout**: the prober gives up first and counts a
  failure. The documentation asks to set the probe's timeout above the longest check timeout.
- **Check names in the response**: listed in the order the checks were given.

## Requirements *(mandatory)*

### Functional Requirements

#### Module and shared conventions

- **FR-001**: The module MUST be delivered as one independent module in this repository, in the
  directory `lifecycle`, with a package `lifecycle` and a subpackage `health`.
- **FR-002**: The module MUST NOT depend on any module outside the standard library: neither
  third-party modules, nor database, cache or message bus clients, nor the other modules of this
  repository. This includes its tests and runnable examples. Code that uses a third-party client
  appears only as prose in the README. The `health` package MUST NOT depend on the `lifecycle`
  package.
- **FR-003**: Invalid configuration MUST be reported to the caller as an error that identifies an
  invalid configuration, never as a panic or a silent fallback. This includes a non-positive stop
  timeout, a negative drain delay, a missing logger, an empty signal list, invalid registrations
  (FR-010) and invalid checks (FR-036).
- **FR-004**: The App and the health handler MUST be safe for concurrent use.
- **FR-005**: The module MUST NOT let a panic escape from a component's run or stop operation or
  from a check. Such a panic MUST be recovered and handled as that operation's failure, with the
  panic value and the stack trace (FR-027, FR-040).
- **FR-006**: The module MUST NOT end the process, write to standard output or standard error, or
  hold global state. It logs only through a logger provided by the developer. It handles operating
  system signals only when configured to, and only while the App runs.
- **FR-007**: When every component and every check honors cancellation, no task started by the
  module MUST remain once the App has returned or a health response has been sent, except the
  tasks of components that the App stops after the deadline has expired (FR-024): those end as
  soon as their operations return. A task can remain longer only while it waits for a component
  or check that ignores cancellation, and the App reports that component (FR-024).

#### Registering components

- **FR-008**: The developer MUST be able to register components with a name, in order. A component
  is described by a run operation, which receives a context and blocks until the component has
  stopped, and optionally by a stop operation, which receives a context that carries the deadline.
- **FR-009**: A function with the run operation's signature MUST be usable as a component on its
  own.
- **FR-010**: Names MUST be non-empty and unique within an App, and a component MUST be present.
  An invalid registration MUST make the App refuse to run: it returns an error that identifies an
  invalid configuration, before launching anything. A registration made after the App has started
  MUST be refused: the component is never launched, and the App reports the refusal when it
  returns.
- **FR-011**: An App MUST run at most once. Running it again MUST return at once a distinguishable
  error and affect nothing.

#### Running

- **FR-012**: Running the App MUST launch every component in registration order, each in its own
  task, without waiting for a component to be ready before launching the next. Launching MUST
  complete before any trigger is acted upon.
- **FR-013**: Each component's context MUST carry the values of the context passed to the App, and
  MUST NOT be cancelled when that context is. It is cancelled only as FR-020 and FR-021 describe.
- **FR-014**: Running the App MUST block until the stop sequence has completed, or until its
  deadline has expired (FR-023).

#### Stopping: triggers and sequence

- **FR-015**: A stop MUST be triggered by the first of: the end of the context passed to the App;
  a configured signal; a component's run that returns or panics before a stop was requested. Later
  triggers MUST NOT start another sequence. A second configured signal forces the stop (FR-025),
  and later failures are reported (FR-026).
- **FR-016**: The developer MUST be able to configure the signals the App handles. By default it
  handles none. Configured signals are intercepted only while the App runs. When it returns, the
  process's previous handling of those signals MUST apply again.
- **FR-017**: When a stop is triggered, the App MUST enter its draining state, so that its
  readiness reports not running (FR-029), then wait for the drain delay, during which no component
  is stopped or cancelled. The drain delay is zero by default and applies whatever the trigger.
- **FR-018**: After the drain delay, the App MUST stop the components one at a time, in reverse
  registration order. A component's stop starts only when the previous one has stopped, or when
  the deadline has expired (FR-024).

#### Stopping: one component

- **FR-019**: A component whose run has already returned MUST NOT be stopped again: its stop
  operation is not called.
- **FR-020**: For a component with a stop operation, the App MUST: call the stop operation with a
  context that carries the deadline of the stop sequence; wait until both the stop operation and
  the run have returned, or until the deadline expires; and cancel the run's context only when the
  deadline expires. Until then, the run's context MUST stay valid, so that in-flight work
  completes.
- **FR-021**: For a component without a stop operation, the App MUST cancel the run's context at
  the start of its turn and wait for the run to return, until the deadline.
- **FR-022**: After a stop was requested, a run that returns no error or an error that identifies
  a cancellation MUST count as a clean stop. Any other error returned by the run, and any error
  returned by the stop operation, MUST be reported with the component's name.

#### Bounded shutdown

- **FR-023**: The developer MUST be able to configure the stop timeout, 15 seconds by default. It
  starts when the drain delay ends and bounds the stop of every component. The App MUST return no
  later than the drain delay plus the stop timeout after the trigger, plus negligible overhead,
  whatever the components do.
- **FR-024**: When the deadline expires, every component not yet stopped MUST still receive, in
  reverse registration order, its stop operation, with the expired deadline, and the cancellation
  of its run's context. The App MUST NOT wait for them. Every component whose run had not returned
  when the deadline expired MUST be reported as not stopped in time, with a distinguishable error.
- **FR-025**: A second configured signal, received after the first trigger, MUST end the drain
  delay and make the deadline expire at once (FR-024), so that the App returns promptly. The App
  MUST report the stop as forced, with a distinguishable error.

#### Result, errors and logs

- **FR-026**: The App MUST return no error when the stop was triggered by its context or by a
  signal, and every component stopped cleanly within the deadline. Otherwise it MUST return an
  error that joins, each identifiable with the standard error inspection functions:
  - the failure that triggered the stop, naming the component and wrapping its error, or a
    distinguishable exited-early error when its run returned no error;
  - later failures of other components, in the same form;
  - every stop error (FR-022), naming the component;
  - every component not stopped in time (FR-024);
  - the forced stop (FR-025);
  - refused registrations (FR-010).
- **FR-027**: A panic in a component's run MUST be handled as that component's failure. A panic in
  its stop operation MUST be handled as a stop error. Both carry the panic value and the stack
  trace.
- **FR-028**: The developer MUST be able to provide a logger. With a logger, the App MUST log the
  trigger, the start and end of the drain, the stop of each component with its duration and
  outcome, and every reported error. Without a logger, nothing is logged.

#### The App's readiness

- **FR-029**: The App MUST expose a readiness operation with the shape of a health check. It
  reports no error from the moment every component has been launched until a stop is triggered.
  Otherwise it reports a distinguishable not-running error that names the state: starting,
  stopping or stopped.

#### Adapters

- **FR-030**: The module MUST provide an adapter for the standard library's HTTP server. It serves
  on the server's configured address, and serves TLS when the server's TLS configuration provides
  certificates or a certificate callback. The developer MUST be able to give the adapter a
  listener of their own, as an optional setting: the server then serves on that listener instead
  of its configured address, with TLS under the same rule, and the listener is closed when the
  server stops.
- **FR-031**: The HTTP server adapter's run MUST return no error after a requested stop. Its stop
  operation MUST perform the server's graceful shutdown bounded by the deadline: the server stops
  accepting connections at once, its shutdown hooks run, and the requests in progress complete.
  When the deadline expires, the remaining connections MUST be closed.
- **FR-032**: The module MUST provide a close adapter that accepts a close operation of any of
  three shapes: no argument and no result; no argument and an error; a context and an error. The
  method values of common clients fit it directly. Its run waits until the stop. Its stop calls
  the close operation exactly once, passing the deadline to the third shape, and reports its
  error.
- **FR-033**: The module MUST provide a start/stop adapter that takes a start operation and a stop
  operation, both receiving a context. Its run calls start with the component's context and, when
  start succeeds, waits until the stop. A start error is that component's failure, and its stop
  operation is then not called (FR-019). Its stop calls the stop operation with the deadline.
- **FR-034**: No adapter MUST require a third-party import. Developers write components for other
  shapes as small types with a run and a stop operation (FR-049).

#### Health handler

- **FR-035**: The `health` package MUST provide an HTTP handler built from a list of checks. A check
  has a name, an operation that receives a context and reports an error, and an optional timeout,
  1 second by default.
- **FR-036**: Building a handler MUST refuse, with an error that identifies an invalid
  configuration: an empty name, a name that contains whitespace or control characters, a name
  used twice, a missing operation and a negative timeout. A zero timeout means the default.
- **FR-037**: A handler with no checks MUST be valid and answer as if every check passed.
- **FR-038**: The handler MUST NOT depend on the App. The App's readiness operation (FR-029) is
  connected by passing it as one of the checks.
- **FR-039**: An evaluation MUST run every check concurrently, each bounded by its timeout. A check
  passes when its operation returns no error within its timeout. It fails when it returns an
  error, times out or panics. The checks' contexts MUST be cancelled when every request waiting
  for the evaluation has gone.
- **FR-040**: A panicking check MUST be recovered and fail. The handler keeps serving.
- **FR-041**: A request that arrives while an evaluation is in progress MUST share it and receive
  its result. A request that arrives when no evaluation is in progress MUST start a new one: a
  result is never reused once its evaluation has completed. A check whose operation is still
  running from an earlier evaluation MUST NOT be started again, and fails in later evaluations as
  timed out until it returns. Each check therefore runs at most once at a time per handler.
- **FR-042**: The handler MUST answer 200 when every check passes, and 503 otherwise, with
  `Content-Type: text/plain; charset=utf-8`, `Cache-Control: no-store` and
  `X-Content-Type-Options: nosniff`. A HEAD request MUST receive the same status and headers
  without a body. Any other method than GET and HEAD MUST receive 405 with `Allow: GET, HEAD`,
  and no check runs.
- **FR-043**: When every check passes and the `verbose` query parameter is absent, the body MUST be
  `ok`. When a check fails, or the `verbose` query parameter is present with any value, the body
  MUST list one line per check, in the order the checks were given, `[+]<name> ok` or
  `[-]<name> failed: reason withheld`, followed by a final line `ok` or `failed`.
- **FR-044**: A response MUST NEVER contain a check's error, panic value or duration. The developer
  MUST be able to provide a logger. With a logger, the handler MUST log every failure with the
  check's name and the reason: the error, the timeout, or the panic value with the stack trace.
  Without a logger, reasons are not recorded.

#### Documentation

- **FR-045**: The README and the package documentation MUST document thoroughly, each in its own
  section with an explanation and a code example:
  - the model: components, the registration order and the reverse stop order, resources opened
    by the developer, and why nothing is restarted;
  - the stop contract (FR-019 to FR-022), with a timeline of a complete shutdown;
  - the bounded shutdown, the second signal and how to read the App's error;
  - a complete `main` with resources, workers, an HTTP server and the probes.
- **FR-046**: The documentation MUST explain the triggers and the signals: why signals are opt-in,
  which signals to configure, and the behavior on Windows. It MUST explain how to test an App:
  trigger the stop through its context, because handling signals inside a `testing/synctest`
  bubble ends the test binary with a fatal error.
- **FR-047**: The documentation MUST explain how to connect the App's readiness to a readiness
  handler, and that the `health` package can be used without the App.
- **FR-048**: The documentation MUST include a compatibility table that shows how to register, and
  the pitfall that applies to: the standard library's HTTP server; this repository's event broker
  and the feed loop of its multi-replica setup; goroutine workers and tickers; the standard
  library's database handle; the pgx connection pool; the go-redis v9 client; a Kafka client
  (franz-go), whose client is registered before its consumer loop; a RabbitMQ consumer (amqp091);
  the river job queue; the robfig cron v3 scheduler; OpenTelemetry's tracer provider; this
  repository's `slogx` handler; a gRPC server; a NATS connection.
- **FR-049**: The documentation MUST show how to write components for the shapes no adapter
  covers:
  - a RabbitMQ consumer whose stop operation cancels the consumer, waiting for the broker's
    confirmation, and whose run processes deliveries until the broker closes them, so that no
    message is lost; the alternative based on a context-aware consume call, and its trade-off:
    in-flight messages are interrupted and requeued, which suits only idempotent handlers;
  - a gRPC server, whose graceful stop has no deadline of its own;
  - a NATS connection, whose drain completes asynchronously.
- **FR-050**: The documentation MUST explain the three cases of a blocking function: one that takes
  a context is a function component; one that does not, but that another operation unblocks, is a
  component whose stop operation calls it, with a listener's accept loop as the example; one that
  nothing can unblock cannot be stopped, and the App reports it as not stopped in time and moves
  on.
- **FR-051**: The documentation MUST explain the interaction with this repository's modules: the
  event broker's closing is registered as a shutdown hook of the HTTP server, not as a component,
  because a component stopped before the server would make clients reconnect to the draining
  process; the `slogx` flush is registered first, so that it runs last.
- **FR-052**: The documentation MUST include a Kubernetes section: the drain delay or a pre-stop
  sleep, the drain delay plus the stop timeout kept below the grace period, the readiness and
  liveness probes pointed at their handlers, and the probe timeout kept above the longest check
  timeout. It MUST explain why liveness must include neither dependencies, which would restart
  every replica in a loop during an outage, nor the App's readiness, which would kill the process
  during its start or drain, and why reasons are withheld from responses.
- **FR-053**: Snippets that use third-party clients MUST appear only as code blocks in the README,
  never as compiled examples (FR-002).
- **FR-054**: Every primary capability MUST have a runnable example: running an App with a function
  component and stopping it through its context, the HTTP server adapter on a listener supplied
  by the developer, the close adapter, the
  start/stop adapter, a component with a stop operation, and readiness and liveness handlers wired
  to an App.

### Key Entities

- **App**: the set of registered components, run once, with its options and its state.
- **Component**: a named part of the process, described by a run operation and, optionally, a stop
  operation.
- **Run operation**: blocks while the component works, and returns when it has stopped or failed.
- **Stop operation**: asks a component to stop accepting new work, within a deadline.
- **Adapter**: turns a common shape, such as an HTTP server, a close operation, or a pair of start
  and stop operations, into a component.
- **Trigger**: the event that starts the stop: the end of the App's context, a signal, or a
  component that returns early.
- **Drain delay**: the time between the trigger and the first stop, during which readiness fails
  and every component keeps working.
- **Stop timeout**: the deadline for stopping every component, after the drain delay.
- **State**: starting, running, stopping or stopped, as reported by the App's readiness.
- **Check**: a named operation, with a timeout, that tells whether something the process needs is
  available.
- **Health handler**: the HTTP handler that runs checks and answers a probe.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: For Apps of 1 to 50 components registered in any order, 100% of runs stop the
  components exactly in reverse registration order, each stop starting only after the previous
  component has stopped or the deadline has expired.
- **SC-002**: During the stop of an HTTP server with 100 requests in progress that finish before
  the deadline, 100% of them complete successfully, and with a drain delay configured, 100% of the
  requests sent during the delay are served.
- **SC-003**: In 100% of the tests, the App returns no later than the drain delay plus the stop
  timeout after the trigger, including with components that ignore cancellation and stop
  operations that never return.
- **SC-004**: A component with a stop operation that finishes within the deadline completes 100%
  of the work it had already received, and its context is never cancelled.
- **SC-005**: A health handler with 20 checks answers no later than its longest check timeout,
  plus negligible overhead, and 0 responses across the tests contain the text of a check's error.
  However many requests arrive during one evaluation, each check runs once for it.
- **SC-006**: When every component and check honors cancellation, the module leaves 0 tasks running
  once the App has returned and the operations of every component have returned, and once each
  health response has been sent.
- **SC-007**: Adopting the module adds zero dependencies beyond the standard library to a consumer
  project.
- **SC-008**: 100% of the topics listed in FR-045 to FR-052 have a documented section with a code
  example, and 100% of the capabilities listed in FR-054 have a runnable example that passes.
- **SC-009**: 100% of the acceptance scenarios and edge cases in this spec are covered by automated
  tests that wait for no real time, and statement coverage of the module is at least 80%.

## Assumptions

- **Module and versioning**: one module, `github.com/paccolamano/go/lifecycle`, that contains both
  packages, released with `lifecycle/vX.Y.Z` tags. The first release is 0.1.0, and releases stay
  at 0.x until the API is considered stable.
- **Go version**: the minimum Go version is the oldest release the constitution supports (1.26 at
  the time of writing).
- **Default stop timeout**: 15 seconds. With a drain delay of 10 seconds, it stays within the
  default Kubernetes grace period of 30 seconds with a margin. The documentation explains the sum.
- **Default check timeout**: 1 second, the default timeout of Kubernetes probes. The documentation
  asks to raise the probe's timeout when raising a check's timeout.
- **Launch order**: components are launched in registration order, but the App does not wait for
  a component to be ready before launching the next. The ordering guarantees apply to the stop.
  The readiness of dependencies is expressed through checks.
- **Drain on every trigger**: the drain delay also applies when a component's failure triggers the
  stop, because the other components may still be serving traffic.
- **Recovered panics**: a panicking component still lets the others stop in order, close their
  resources and flush their logs. A panic in a task started by a component itself cannot be
  recovered by the App and ends the process as usual. The documentation says so.
- **Registration errors**: reported when the App runs, because registering returns no error in the
  agreed design.
- **Reasons withheld**: the probe response format follows the convention of the Kubernetes API
  server, which withholds failure reasons from responses and logs them.
- **TLS**: the HTTP server adapter serves TLS only from the server's TLS configuration, on its
  address or on the developer's listener. Certificate files are loaded by the developer into that
  configuration.
- **Defaults chosen in the plan**: the names of the states and of the distinguishable errors, the
  log messages and their attributes, and the validation rules of signal lists are fixed and
  documented in `/speckit-plan`.
- **Out of scope**:
  - opening resources, dependency injection and constructors;
  - restarting or supervising components, and retries;
  - stopping components in parallel groups;
  - probe response formats other than plain text, and excluding checks per request;
  - graceful binary upgrades and the hand-over of listening sockets between processes;
  - process exit codes, which the developer's `main` decides from the App's error.
- **Tests**: tests are hermetic and deterministic (Principle III). Drain delays, stop timeouts and
  check timeouts are verified with simulated time, without real waiting.

## Appendix: original request

The request, as submitted to `/speckit-specify`:

```text
New module `lifecycle` (github.com/paccolamano/go/lifecycle) with a subpackage `lifecycle/health`: runs a service process from startup to graceful shutdown — ordered start and stop of its components (servers, background workers, queue consumers, resources such as DB/Redis clients), drain before shutdown, readiness and liveness endpoints. Standard library only.

User's words when asking to proceed: "ok direi allora di procedere per scrivere la spec. 0 di default va bene". The design was agreed in conversation; decisions below are already agreed with the user (do not reopen them).

CORE MODEL
- `Component interface { Run(ctx context.Context) error }`; optional `Stopper interface { Stop(ctx context.Context) error }`, detected by type assertion. `Func func(ctx context.Context) error` implements Component (like http.HandlerFunc).
- `New(opts...) (*App, error)` (validates options, errors wrap a config sentinel as in the other modules), `(*App).Add(name string, c Component)`, `(*App).Run(ctx) error`, `(*App).Ready(ctx) error` (nil only while the App is running; meant to be used as a health check).
- Components are launched in registration order and stopped in strict reverse order, sequentially (no parallel groups). Rule for users: register in the order you open/start things; stop is the reverse.
- Resources (DB, Redis, Kafka clients, tracer providers) are opened by user code before Run, with normal Go error handling; the library only closes them. No dependency injection.
- A component whose Run returns before a stop was requested — with an error or with nil — triggers the shutdown of the whole App, and App.Run returns an error naming that component. No restarts, no retries, no supervision: restarting is the orchestrator's job. A component failing right at startup makes the App stop the already-launched ones in reverse order.

STOP CONTRACT (per component, in reverse order)
1. If it implements Stopper, Stop(ctx) is called with the remaining deadline. Stop means "stop accepting new work"; it may return immediately (e.g. RabbitMQ basic.cancel) or block (http.Server.Shutdown).
2. The App then waits for Run to return, within the deadline. Run's context stays valid meanwhile, so in-flight work completes normally.
3. Only when the deadline expires does the App cancel Run's context ("time is up, abort"). For components without Stopper, the cancellation of Run's context is the only signal and is delivered at the start of their turn.
4. If Run still does not return, the App records it in the returned error and moves on to the next component (goroutines cannot be killed); the process exits anyway.
- After a requested stop, `context.Canceled` returned by Run is ignored; any other error is reported. Mapping "closed" sentinels (net.ErrClosed, http.ErrServerClosed) to nil is the component's job; the HTTPServer adapter does it.
- App.Run joins the error that triggered the stop, stop errors and timeouts; error conditions callers branch on are sentinels usable with errors.Is.

ADAPTERS (stdlib only; they accept method values, so no third-party import is needed)
- `HTTPServer(srv *http.Server) Component`: Run = ListenAndServe (ErrServerClosed → nil); Stop = Shutdown(ctx), then Close when the deadline expires.
- `Closer[F func() | func() error | func(context.Context) error](f F) Component`: Run waits until the stop; Stop calls f. Covers sql.DB.Close, pgxpool.Pool.Close, go-redis Client.Close, OpenTelemetry TracerProvider.Shutdown, slogx Handler.Flush.
- `StartStop(start, stop func(context.Context) error) Component`: for Start/Stop shaped services (river; robfig/cron v3 via closures).
- gRPC (GracefulStop has no context), NATS (Drain is asynchronous, wait for the ClosedHandler) and other shapes: the user writes a small type with Run and Stop; documented in the README.

SHUTDOWN SEQUENCE AND OPTIONS
- Triggers: parent context cancelled, a signal configured with WithSignals, a component exiting early.
- Sequence: the App enters draining (Ready returns an error, so readiness answers 503); waits the drain delay while still serving normally; stops components in reverse order within the overall stop timeout.
- A second signal at any time makes Run return immediately. The library never calls os.Exit.
- `WithSignals(sig ...os.Signal)`: OPT-IN; without it the App reacts only to its context (constitution: no process-level behaviour imposed by default).
- `WithDrainDelay(d)`: default 0 (agreed). Kubernetes users set it or use a preStop sleep hook; the README must make this prominent.
- `WithStopTimeout(d)`: overall deadline for the stop sequence, split across components as remaining time.
- `WithLogger(*slog.Logger)`: injected logger for lifecycle events; no global logger.

INTERACTION WITH THE REPOSITORY'S MODULES
- sse.Broker is NOT a component: it must close during http.Server.Shutdown, when the listeners are already closed, so clients reconnect to another replica; this is done with `srv.RegisterOnShutdown(broker.Close)`. Stopping it as a component before the server would make clients reconnect to the draining pod. The sse feed loop of multi-replica setups is a Func.
- slogx Handler.Flush is registered first, so it runs last.

HEALTH SUBPACKAGE `lifecycle/health`
- `Check{Name string; Func func(ctx context.Context) error; Timeout time.Duration}`; `New(checks []Check, opts...) (*Handler, error)`; Handler implements http.Handler.
- It does not import `lifecycle`: the link is `health.Check{Name: "lifecycle", Func: app.Ready}`, so it is usable standalone.
- Checks run concurrently, each with its own timeout; the handler answers 200 or 503.
- `?verbose` lists checks in the kube-apiserver style (`[+]postgres ok`, `[-]redis failed: reason withheld`); the real reason goes to the injected logger and is never sent, because errors can contain hosts or DSNs.
- Readiness and liveness are the same type with different checks: readiness = dependencies + app.Ready; liveness = no checks, or in-process checks only. The docs must explain why liveness must include neither dependencies (restart loops across all replicas) nor app.Ready (the pod would be killed during startup or drain).

DOCUMENTATION (README and package docs, runnable Examples for the primary APIs; third-party snippets only as README code blocks, never compiled, so the module gains no dependency)
- The model, the ordering rule and the stop contract, with the shutdown timeline.
- Compatibility table: net/http Server, sse.Broker (RegisterOnShutdown) and its feed loop, goroutine workers and tickers, *sql.DB, pgxpool, go-redis v9, Kafka franz-go (client Closer registered before the consumer Func), RabbitMQ amqp091 (consumer type whose Stop calls Channel.Cancel with noWait=false and whose Run ranges over deliveries until closed; the ConsumeWithContext alternative and its trade-off: in-flight messages are interrupted and requeued, fine only for idempotent handlers), river, robfig/cron v3, OpenTelemetry, slogx, gRPC, NATS.
- Blocking functions, three cases: accepts a context → Func; does not accept one but a companion method unblocks it (Serve/Shutdown, Accept/Close, GracefulStop) → type with Run and Stop, e.g. a net.Listener accept loop; nothing can unblock it → it cannot be stopped, the App waits until the deadline, reports it and moves on.
- A complete main example; a Kubernetes section (drain delay, preStop sleep, terminationGracePeriodSeconds, probe configuration); readiness vs liveness.

OPEN POINTS FOR THE SPEC (not yet decided): default stop timeout (below Kubernetes' default 30s grace period), default per-check timeout, whether health caches check results to limit load from frequent probes, names of states and sentinel errors, TLS support in HTTPServer.
```

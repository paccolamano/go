# lifecycle

Run a service process from start to graceful shutdown: launch its components in order, stop them
in reverse order with a drain period and a bounded deadline, and answer readiness and liveness
probes. The module has no dependency outside the standard library.

```bash
go get github.com/paccolamano/go/lifecycle
```

Minimum Go version: 1.26

## A complete main

A service with structured logs, tracing, a database, a cache, server-sent events fed from Redis, a
background worker, an HTTP server and its probes. Third-party calls are sketches: pgx v5,
go-redis v9 and OpenTelemetry Go v1.

```go
func main() {
	if err := run(); err != nil {
		slog.Error("service stopped", "error", err)
		os.Exit(1)
	}
}

func run() error {
	ctx := context.Background()

	logHandler, err := slogx.New(slog.NewJSONHandler(os.Stdout, nil))
	if err != nil {
		return err
	}
	logger := slog.New(logHandler)
	tp := sdktrace.NewTracerProvider( /* exporter */ )
	db, err := sql.Open("pgx", os.Getenv("DATABASE_URL"))
	if err != nil {
		return err
	}
	rdb := redis.NewClient(&redis.Options{Addr: os.Getenv("REDIS_ADDR")})
	broker, err := sse.NewBroker()
	if err != nil {
		return err
	}

	app, err := lifecycle.New(
		lifecycle.WithSignals(os.Interrupt, syscall.SIGTERM),
		lifecycle.WithDrainDelay(10*time.Second),
		lifecycle.WithLogger(logger),
	)
	if err != nil {
		return err
	}
	// Resources, closed last, in reverse order.
	app.Add("logs", lifecycle.Closer(logHandler.Flush))
	app.Add("tracing", lifecycle.Closer(tp.Shutdown))
	app.Add("postgres", lifecycle.Closer(db.Close))
	app.Add("redis", lifecycle.Closer(rdb.Close))
	// Background work, stopped before the resources it uses.
	app.Add("sse-feed", lifecycle.Func(func(ctx context.Context) error { return feed(ctx, rdb, broker) }))
	app.Add("outbox", lifecycle.Func(outbox.Run))

	readiness, err := health.New([]health.Check{
		{Name: "lifecycle", Func: app.Ready},
		{Name: "postgres", Func: db.PingContext},
		{Name: "redis", Func: func(ctx context.Context) error { return rdb.Ping(ctx).Err() }},
	}, health.WithLogger(logger))
	if err != nil {
		return err
	}
	liveness, err := health.New(nil)
	if err != nil {
		return err
	}

	mux := http.NewServeMux()
	mux.Handle("GET /readyz", readiness)
	mux.Handle("GET /livez", liveness)
	mux.HandleFunc("GET /events", func(w http.ResponseWriter, r *http.Request) {
		_ = broker.Serve(w, r, "public")
	})
	srv := &http.Server{Addr: ":8080", Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	srv.RegisterOnShutdown(broker.Close) // ends event streams when the server stops
	// The server, registered last, stops first.
	app.Add("http", lifecycle.HTTPServer(srv))

	return app.Run(ctx)
}
```

## The model

A process is a list of **components**: servers, workers, queue consumers, schedulers, and the
clients of databases and caches. A component has a `Run` method that blocks while it works and
returns when it has stopped. It can also implement `Stopper`, whose `Stop` method asks it to stop
accepting new work (see [The stop contract](#the-stop-contract)). A function with the signature of
`Run` is a component on its own, through `lifecycle.Func`.

```go
app, err := lifecycle.New(lifecycle.WithSignals(os.Interrupt, syscall.SIGTERM))
if err != nil {
	return err
}
app.Add("db", lifecycle.Closer(db.Close))       // stopped last
app.Add("outbox", lifecycle.Func(outbox.Run))   // stopped second
app.Add("http", lifecycle.HTTPServer(srv))      // stopped first
return app.Run(ctx)
```

**The ordering rule**: register components in the order you open or start them. The App launches
them in that order, and stops them one at a time in the reverse order. The server stops accepting
requests before the worker stops, and the database is closed only when nothing uses it any more.

**Resources stay yours.** Open your databases and clients in your own code, before `Run`, with
ordinary error handling. The App does not construct anything: it only needs the way to stop or
close each component. There is no dependency injection.

**Registration errors** are reported by `Run`, before anything is launched: an empty name, a name
used twice, a nil component, or an adapter given a nil value. They wrap `ErrInvalidConfig`. A
component added after `Run` has started is never launched, and `Run` reports it when it returns.

**Nothing is restarted.** A component whose `Run` returns before it was asked to stop, with an
error or without one, stops the whole process. Restarting is the job of the orchestrator, which
sees the process exit.

### When a component fails

A worker that dies silently while the server keeps answering looks healthy and does nothing. The
App prevents this: a component whose `Run` returns or panics before its turn triggers the stop of
every other component, in reverse order, and `Run` returns an error that names it.

```go
err := app.Run(ctx)
var ce *lifecycle.ComponentError
if errors.As(err, &ce) {
	slog.Error("component failed", "component", ce.Name, "error", ce.Err)
}
if errors.Is(err, lifecycle.ErrExitedEarly) {
	// a component returned nil before it was asked to stop
}
```

- An error is wrapped in a `*ComponentError`. A `Run` that returns nil reports `ErrExitedEarly`.
- A panic in `Run` or `Stop` is recovered and reported as a `*PanicError`, with its value and
  stack trace. The other components still stop in order, close their resources and flush their
  logs. A panic in a goroutine that a component starts itself cannot be recovered by the App, and
  ends the process as usual.
- Components that fail while the others are stopping are reported too, and are not stopped again.

## The stop contract

When a component's turn comes, the App applies a two-step contract.

| The component | The App |
|---------------|---------|
| implements `Stopper` | 1. calls `Stop(ctx)`, which means "stop accepting new work"; `ctx` carries the deadline of the stop sequence. 2. waits until `Stop` **and** `Run` have returned. The run's context **stays valid**, so the work in progress completes. 3. cancels the run's context only if the deadline expires first: "time is up, abort". |
| does not implement `Stopper` | cancels the run's context at the start of its turn, then waits for `Run`, until the deadline. |

- `Stop` may return at once, like a consumer that cancels its subscription, or block until the
  component has stopped, like an HTTP server's graceful shutdown.
- `Stop` is called only after `Run` has been called, but possibly while `Run` is still starting.
- After its stop, a `Run` that returns nil or `context.Canceled` stopped cleanly. Any other error
  is reported. Map "closed" errors such as `net.ErrClosed` to nil yourself; the adapters do.
- A component whose `Run` has already returned is not stopped again.

A complete shutdown, with components registered as `log`, `db`, `worker` and `http`, a drain
delay of 10 seconds and the default stop timeout of 15 seconds:

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

### Adapters

Three adapters cover the common shapes, without importing any third-party package: they take
method values.

**`HTTPServer(srv, opts...)`** serves an `*http.Server` on `srv.Addr`, or on a listener of your own
with `WithListener(ln)`. It serves TLS when `srv.TLSConfig` provides a certificate. Its stop is
`srv.Shutdown`: the listeners close at once, the hooks registered with `srv.RegisterOnShutdown`
run, and the requests in progress complete. At the deadline, the remaining connections are
closed.

```go
srv := &http.Server{Addr: ":8080", Handler: mux}
app.Add("http", lifecycle.HTTPServer(srv))

// In a test, serve on a listener bound to port 0 and read its address:
ln, err := net.Listen("tcp", "127.0.0.1:0")
app.Add("http", lifecycle.HTTPServer(srv, lifecycle.WithListener(ln)))
```

**`Closer(f)`** calls a close function once, at its turn. It accepts `func()`, `func() error` and
`func(context.Context) error`, so the `Close` and `Shutdown` methods of most clients fit as they
are. Register it where the resource must be closed: before the components that use it.

```go
app.Add("logs", lifecycle.Closer(logHandler.Flush)) // func(context.Context) error
app.Add("tracing", lifecycle.Closer(tp.Shutdown))   // func(context.Context) error
app.Add("db", lifecycle.Closer(db.Close))           // func() error
app.Add("pool", lifecycle.Closer(pool.Close))       // func()
```

A named function type, such as `context.CancelFunc`, must be converted:
`lifecycle.Closer(func() { cancel() })`.

**`StartStop(start, stop)`** fits services with separate start and stop operations. `start` is
called at launch and must return once the service has started; an error is a failure of the
component. `stop` is called at its turn, once `start` has returned, and never when `start` failed.

```go
app.Add("jobs", lifecycle.StartStop(riverClient.Start, riverClient.Stop))
```

## Bounded shutdown

An orchestrator kills a process whose shutdown outlasts its grace period, and the logs and traces
that were not flushed are lost with it. The **stop timeout** (`WithStopTimeout`, 15 seconds by
default) bounds the stop of every component, counted from the end of the drain. Whatever the
components do, `Run` returns no later than `drain delay + stop timeout` after the trigger.

When the deadline expires:

- the component being stopped gets the cancellation of its context and is reported with
  `ErrStopTimeout`;
- every component not yet stopped still gets its `Stop`, with the expired context, and the
  cancellation of its context, so that a log flush or a fast `Close` still gets its chance. The
  App does not wait for them: each one that has not returned at that moment is reported with
  `ErrStopTimeout`, and may finish just after `Run` has returned;
- a component that ignores cancellation keeps its goroutine until it returns. Go cannot kill a
  goroutine; the process exits anyway.

A **second signal** forces the stop: `Run` returns at once, after giving every remaining component
its `Stop` and its cancellation, and reports `ErrForced`. The first signal received after another
trigger, such as a component's failure, only counts as the first: an orchestrator's single
`SIGTERM` never cuts a graceful shutdown short.

`Run` returns `errors.Join` of every outcome. Inspect it with the standard functions:

```go
err := app.Run(ctx)
switch {
case err == nil:
	// stopped by the context or a signal, every component cleanly
case errors.Is(err, lifecycle.ErrForced):
	// a second signal cut the shutdown short
case errors.Is(err, lifecycle.ErrStopTimeout):
	// a component did not stop in time
}
var pe *lifecycle.PanicError
if errors.As(err, &pe) {
	os.Stderr.Write(pe.Stack) // the stack trace of a recovered panic
}
```

| Error | Meaning |
|-------|---------|
| `*ComponentError` | wraps every per-component outcome, with the component's name |
| `ErrExitedEarly` | a `Run` returned nil before it was asked to stop |
| `ErrStopTimeout` | a component had not stopped when the deadline expired |
| `ErrForced` | a second signal forced the stop |
| `*PanicError` | a panic recovered from `Run` or `Stop` |
| `ErrInvalidConfig` | an invalid option, registration or adapter |
| `ErrAlreadyStarted` | `Run` was called twice |

## Signals and testing

Signals are **opt-in**: without `WithSignals`, the App reacts only to its context, so the library
never changes how a process handles signals unless asked. A service usually configures:

```go
lifecycle.WithSignals(os.Interrupt, syscall.SIGTERM)
```

- The first signal triggers the stop; the second forces it.
- The signals are handled only while `Run` runs. When it returns, the process's previous handling
  applies again, so a third Ctrl-C ends a program stuck after `Run` the usual way.
- On Windows, `os.Interrupt` (Ctrl-C) is delivered. `syscall.SIGTERM` compiles, but Windows does
  not send it to console programs.

**Testing an App**: trigger the stop by cancelling the context passed to `Run`, not with signals.
Inside a `testing/synctest` bubble, `signal.Notify` ends the test binary with a fatal error of the
runtime (`select on synctest channel from outside bubble`), so never use `WithSignals` in an App
that runs inside a bubble.

```go
synctest.Test(t, func(t *testing.T) {
	app, _ := lifecycle.New(lifecycle.WithDrainDelay(10 * time.Second))
	app.Add("worker", lifecycle.Func(worker.Run))
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- app.Run(ctx) }()
	synctest.Wait() // every component is running
	cancel()        // the trigger, instead of SIGTERM
	if err := <-done; err != nil {
		t.Fatal(err)
	}
})
```

## Compatibility

How to register common systems. The snippets are sketches written against the versions named in
the table; adapt them to your client.

| System | Register | Pitfall |
|--------|----------|---------|
| `net/http` server | `lifecycle.HTTPServer(srv)` | none: long-lived responses, such as event streams, must end in a shutdown hook (next row) |
| `sse.Broker` of this repository | `srv.RegisterOnShutdown(broker.Close)`, **not** a component | a component stopped before the server would make clients reconnect to the stopping process ([This repository's modules](#this-repositorys-modules)) |
| `sse` feed loop, goroutine workers, tickers | `lifecycle.Func(loop)` | the function must return when its context is cancelled |
| `*sql.DB` | `lifecycle.Closer(db.Close)` | register it before the components that use it |
| pgx v5 `*pgxpool.Pool` | `lifecycle.Closer(pool.Close)` | `pool.Ping` fits a health check as it is |
| go-redis v9 `*redis.Client` | `lifecycle.Closer(rdb.Close)` | `Ping` returns a command: check `func(ctx context.Context) error { return rdb.Ping(ctx).Err() }` |
| franz-go `*kgo.Client` | client: `lifecycle.Closer(cl.Close)`; consumer loop: `lifecycle.Func(consume)` | register the client **before** the consumer loop, so that the loop stops first and the client then leaves its group |
| amqp091-go v1.16 consumer | a type with `Run` and `Stop` ([below](#components-to-write-yourself)); the connection: `lifecycle.Closer(conn.Close)` | `Cancel` with `noWait` false, or in-flight messages are dropped |
| river | `lifecycle.StartStop(client.Start, client.Stop)` | none |
| robfig/cron v3 | `lifecycle.StartStop(func(context.Context) error { c.Start(); return nil }, func(ctx context.Context) error { select { case <-c.Stop().Done(): return nil; case <-ctx.Done(): return ctx.Err() } })` | `Stop` returns a context that ends when running jobs complete |
| OpenTelemetry Go v1 tracer provider | `lifecycle.Closer(tp.Shutdown)` | register it early, so that it exports the spans of the shutdown |
| `slogx.Handler` of this repository | `lifecycle.Closer(h.Flush)` | register it first, so that it runs last |
| grpc-go v1 server | a type with `Run` and `Stop` ([below](#components-to-write-yourself)) | `GracefulStop` has no deadline of its own |
| nats.go v1 connection | a type with `Stop` ([below](#components-to-write-yourself)) | `Drain` returns at once and completes asynchronously |

A consumer loop with franz-go, stopped by the cancellation of its context:

```go
func consume(ctx context.Context) error {
	for {
		fetches := cl.PollFetches(ctx)
		if ctx.Err() != nil {
			return nil // stopped
		}
		fetches.EachRecord(func(r *kgo.Record) { handle(ctx, r) })
	}
}

app.Add("kafka", lifecycle.Closer(cl.Close)) // registered first: closed after the loop
app.Add("orders", lifecycle.Func(consume))
```

## Components to write yourself

When no adapter fits, write a small type with `Run` and, if the component can stop accepting work
on request, `Stop`.

**RabbitMQ consumer** (amqp091-go v1.16). `Stop` cancels the consumer: the broker stops
delivering, and the channel of deliveries closes once the messages already received have been
handed over. `Run` processes them all, so no message is lost.

```go
type orderConsumer struct {
	ch  *amqp.Channel
	tag string
}

func (c *orderConsumer) Run(ctx context.Context) error {
	deliveries, err := c.ch.Consume("orders", c.tag, false, false, false, false, nil)
	if err != nil {
		return err
	}
	for d := range deliveries { // closed after Stop, or if the connection drops
		if err := handleOrder(ctx, d.Body); err != nil {
			_ = d.Nack(false, true) // back to the queue
			continue
		}
		_ = d.Ack(false)
	}
	return nil
}

func (c *orderConsumer) Stop(context.Context) error {
	return c.ch.Cancel(c.tag, false) // noWait must be false: with true, in-flight messages are dropped without an ack
}

app.Add("rabbitmq", lifecycle.Closer(conn.Close)) // closed after the consumer
app.Add("orders", &orderConsumer{ch: ch, tag: "orders-worker"})
```

At the deadline, the App cancels `ctx`: `handleOrder` is interrupted, the message is requeued, and
the connection's close requeues every message not acknowledged. A `Run` that returns while no stop
was requested, because the connection dropped, stops the process.

The alternative is shorter: `ConsumeWithContext(ctx, …)` cancels the consumer by itself when its
context is cancelled, so a `lifecycle.Func` is enough. But the same cancellation interrupts the
message in progress, which is requeued and processed again: fine only for idempotent handlers.

**gRPC server** (grpc-go v1). `GracefulStop` waits for the RPCs in progress, without a deadline, so
`Stop` bounds it:

```go
type grpcServer struct {
	srv *grpc.Server
	lis net.Listener
}

func (g grpcServer) Run(context.Context) error { return g.srv.Serve(g.lis) } // nil after a stop

func (g grpcServer) Stop(ctx context.Context) error {
	done := make(chan struct{})
	go func() { g.srv.GracefulStop(); close(done) }()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		g.srv.Stop() // closes the remaining connections
		return ctx.Err()
	}
}
```

**NATS connection** (nats.go v1). `Drain` returns at once; the connection closes asynchronously
when the drain completes, which the `ClosedHandler` reports:

```go
closed := make(chan struct{})
nc, err := nats.Connect(url, nats.ClosedHandler(func(*nats.Conn) { close(closed) }))

app.Add("nats", lifecycle.Closer(func(ctx context.Context) error {
	if err := nc.Drain(); err != nil {
		return err
	}
	select {
	case <-closed:
		return nil
	case <-ctx.Done():
		nc.Close()
		return ctx.Err()
	}
}))
```

## Blocking functions

How a blocking function can be stopped depends on what unblocks it.

| The function… | It stops when… | Register it as… |
|---------------|----------------|-----------------|
| takes a context, such as `rdb.XRead(ctx, …)` or `cl.PollFetches(ctx)` | its context is cancelled | `lifecycle.Func` |
| takes no context, but another operation unblocks it: `Serve`/`Shutdown`, `Accept`/`Close`, `GracefulStop` | that operation is called | a type whose `Stop` calls it |
| can be unblocked by nothing: a computation that never checks, a cgo call, a library without cancellation | never: Go cannot kill a goroutine | anything: the App waits until the deadline, reports `ErrStopTimeout` and moves on; the process exits anyway |

An accept loop on a `net.Listener`, which `Accept` blocks without a context:

```go
type tcpServer struct{ ln net.Listener }

func (s tcpServer) Run(ctx context.Context) error {
	for {
		conn, err := s.ln.Accept()
		if errors.Is(err, net.ErrClosed) {
			return nil // closed by Stop: a clean stop
		}
		if err != nil {
			return err
		}
		go serve(ctx, conn) // connections end when ctx is cancelled at the deadline
	}
}

func (s tcpServer) Stop(context.Context) error { return s.ln.Close() } // unblocks Accept
```

## This repository's modules

**`sse`**: the broker must close its streams **during** the server's shutdown, when the listeners
are already closed, so that clients reconnect to another replica. `http.Server.Shutdown` waits for
active connections, and an event stream never ends on its own, so register the broker's `Close` as
a shutdown hook of the server:

```go
srv.RegisterOnShutdown(broker.Close)
app.Add("http", lifecycle.HTTPServer(srv))
```

Registering `lifecycle.Closer(broker.Close)` after the server instead would close the streams
before the server's turn, during the drain: clients would reconnect at once to the same process,
which is still listening, and be cut again moments later. The feed loop of a multi-replica setup is
a `lifecycle.Func`, registered before the server.

**`slogx`**: register `lifecycle.Closer(h.Flush)` first, so that it runs last and reports the
records dropped by sampling during the shutdown.

## Health probes

The `health` subpackage answers readiness and liveness probes. It does not depend on the App:
the App's readiness is one more check, and a handler works without any App.

```go
import "github.com/paccolamano/go/lifecycle/health"

readiness, err := health.New([]health.Check{
	{Name: "lifecycle", Func: app.Ready},
	{Name: "postgres", Func: db.PingContext},
	{Name: "redis", Func: func(ctx context.Context) error { return rdb.Ping(ctx).Err() }},
}, health.WithLogger(logger))
if err != nil {
	return err
}
liveness, err := health.New(nil)
if err != nil {
	return err
}
mux.Handle("GET /readyz", readiness)
mux.Handle("GET /livez", liveness)
```

**Readiness and liveness are the same handler with different checks**, because Kubernetes reacts
to them in opposite ways:

| | Readiness | Liveness |
|---|---|---|
| Kubernetes asks | can you take traffic? | are you stuck? |
| When it fails | the pod leaves the load balancer; nothing restarts | the container is restarted |
| Checks | `app.Ready` and the dependencies | none, or checks of the process itself |

Liveness must include **neither the dependencies**, because restarting does not repair a database
that is down and every replica would restart in a loop, **nor `app.Ready`**, which fails during the
start and the drain, so Kubernetes would kill the process while it finishes its requests.

**Responses.** The handler answers GET and HEAD with 200 when every check passes, and 503
otherwise, as `text/plain` with `Cache-Control: no-store`. Other methods get 405.

```text
$ curl -i localhost:8080/readyz
HTTP/1.1 503 Service Unavailable

[+]lifecycle ok
[+]postgres ok
[-]redis failed: reason withheld
failed
```

When every check passes, the body is `ok`; add `?verbose` to list them anyway.

**Reasons are withheld.** An error such as `dial tcp db.internal:6379: connection refused` would
disclose host names, ports or connection strings to whoever can reach the probe. The response only
names the failed check. The reason goes to the logger given with `health.WithLogger`, at level Warn,
as `health: check failed` with `check`, `reason` (`error`, `timeout`, `panic` or
`still running`) and the error, timeout or panic. Do not put secrets in error messages: any logger
would record them.

**Checks** run concurrently, each bounded by its `Timeout` (1 second by default) even when it
ignores its context. A panic fails the check and is logged.

**Shared evaluations.** Requests that arrive while the checks run share their result, and a check
is never started again while it is still running: a check stuck past its timeout counts as failed,
`still running`, until it returns. A flood of probes, or a probe route exposed by mistake, never
multiplies the load on your dependencies. A result is never reused once its evaluation has
completed. When every waiting request has gone, the checks' contexts are cancelled, and a request
that arrives right after may briefly see them as still running.

## Kubernetes

When a pod is terminated, Kubernetes sends it `SIGTERM` and removes it from the Service's
endpoints **at the same time**. The removal takes a few seconds to reach every node, ingress and
load balancer, and requests keep arriving meanwhile. A process that shuts its server down as soon
as it receives `SIGTERM` refuses them, and every rollout produces errors.

The **drain delay** closes that gap. From the trigger on, `Ready` fails, so the readiness probe
answers 503, while every component keeps working normally. Only when the delay ends does the first
stop begin.

```go
app, err := lifecycle.New(
	lifecycle.WithSignals(os.Interrupt, syscall.SIGTERM),
	lifecycle.WithDrainDelay(10*time.Second),
)
```

The drain delay is **0 by default**: the library cannot know whether a load balancer reads your
readiness, and Ctrl-C during development should stop at once. Set it when you run behind one, or
use a `preStop` hook that sleeps, which Kubernetes runs before it sends `SIGTERM`:

```yaml
lifecycle:
  preStop:
    sleep:
      seconds: 10
```

The drain applies whatever triggered the stop, including a component's failure, because the
other components may still be serving traffic.

**The grace period.** Kubernetes kills the container `terminationGracePeriodSeconds` after the
termination starts, 30 seconds by default, counting the `preStop` hook. Keep the sum below it:

```text
preStop sleep + drain delay + stop timeout < terminationGracePeriodSeconds
      0       +     10      +      15      <              30
```

Use either the `preStop` sleep or the drain delay; with both, both count.

**Probes.** Point the readiness probe at the readiness handler and the liveness probe at the
liveness handler ([Health probes](#health-probes)). A probe's `timeoutSeconds`, 1 by default, must
exceed the longest check timeout: otherwise the kubelet gives up before the handler answers.

```yaml
spec:
  terminationGracePeriodSeconds: 30
  containers:
    - name: app
      readinessProbe:
        httpGet: { path: /readyz, port: 8080 }
        periodSeconds: 5
        timeoutSeconds: 2
      livenessProbe:
        httpGet: { path: /livez, port: 8080 }
        periodSeconds: 10
        timeoutSeconds: 2
      # lifecycle:
      #   preStop:
      #     sleep: { seconds: 10 }   # instead of WithDrainDelay
```

## Defaults

| Setting | Default | Option |
|---------|---------|--------|
| Stop timeout | 15 s | `WithStopTimeout` |
| Drain delay | 0 | `WithDrainDelay` |
| Signals | none | `WithSignals` |
| Logger | none | `WithLogger` |
| Check timeout | 1 s | `health.Check.Timeout` |
| Check logger | none | `health.WithLogger` |

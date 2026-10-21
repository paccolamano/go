# sse

Server-sent events for Go: stream events from a handler, deliver them to many clients by topic
with resuming after reconnections, and consume streams from Go with the same reconnection rules
as browsers. The module has no dependency outside the standard library.

```bash
go get github.com/paccolamano/go/sse
```

Minimum Go version: 1.26

## Building blocks

| You need to… | Use | Section |
|--------------|-----|---------|
| stream what one handler produces, such as the progress of a job | `NewStream`, `Stream.Send` | [Single stream](#single-stream) |
| publish once and deliver to every client that follows a topic | `NewBroker`, `Publish`, `Serve` | [Broker](#broker) |
| let clients resume after a reconnection | the broker's history, `WithResetEvent` | [Reconnection and resuming](#reconnection-and-resuming) |
| recover events older than the history from your message bus | `WithReplay` | [Replay from a durable bus](#replay-from-a-durable-bus) |
| run several replicas | a feed loop per replica | [Several replicas](#several-replicas) |
| consume a stream from Go | `NewClient`, `Client.Events`, `Decode` | [Consuming from Go](#consuming-from-go) |

The flows below build on each other; [Integrating into an existing server](#integrating-into-an-existing-server)
puts them together.

## Single stream

Use a `Stream` when one handler produces the events of one response, for example the progress
of a job or the tokens of a generated answer. `NewStream` sets the headers that make proxies and
browsers treat the response as a stream, and flushes them at once:

```go
func progress(w http.ResponseWriter, r *http.Request) {
	s, err := sse.NewStream(w, r, sse.WithRetry(5*time.Second))
	if err != nil {
		// ErrStreamingUnsupported: nothing was written yet.
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	for p := range jobs.Watch(r.Context(), r.PathValue("id")) {
		if err := s.Send(sse.Event{Type: "progress", Data: p.String()}); err != nil {
			return // the client has gone, or stopped reading
		}
	}
}
```

- `Send` refuses an invalid event (an identifier with a line break, text that is not UTF-8) with
  `ErrInvalidEvent`, writes nothing, and the stream stays usable. Line breaks in `Data` are
  allowed: each line becomes a `data:` line, and clients receive line feeds.
- `KeepAlive` writes a comment that clients ignore. Call it from a ticker when your events can be
  far apart, so that proxies do not close the idle connection.
- `NewStream` returns `ErrStreamingUnsupported` when the response writer cannot flush, for
  example inside `http.TimeoutHandler`. Nothing has been written, so you can still answer an
  error.
- **Keep the server's `WriteTimeout`.** A stream gives every write its own deadline
  (`WithWriteTimeout`, 10 seconds by default) and clears it after the write, so idle periods of
  any length are fine, while a client that stops reading is dropped.
- A stream uses the request's context: once the client goes away, `Send` fails without writing.

A stream does not interpret the `Last-Event-ID` header. A handler that can resume on its own
reads it:

```go
if last := r.Header.Get("Last-Event-ID"); last != "" {
	// send what follows last, then continue
}
```

For events delivered to many clients, with resuming handled for you, use a `Broker`.

## Broker

Use a `Broker` when an event is published once and delivered to every client that follows it:
notifications, live dashboards, chat. Each subscription follows one or more **topics**, plain
strings that you choose, such as `user:alice` or `room:42`.

```go
broker, err := sse.NewBroker(sse.WithRetry(3 * time.Second))
if err != nil {
	return err
}

mux.HandleFunc("GET /events", func(w http.ResponseWriter, r *http.Request) {
	p, _ := auth.FromContext(r.Context()) // github.com/paccolamano/go/auth
	err := broker.Serve(w, r, "user:"+p.Subject, "broadcast")
	if err != nil && !errors.Is(err, sse.ErrClosed) {
		logger.WarnContext(r.Context(), "event stream ended", "err", err)
	}
})
```

Publish from anywhere in the service:

```go
data, err := json.Marshal(order)
if err != nil {
	return err
}
if err := broker.Publish(sse.Event{Type: "order.created", Data: string(data)}, "user:"+order.Owner); err != nil {
	logger.ErrorContext(ctx, "publish", "err", err)
}
```

- **Authorization is yours.** The broker delivers a subscription only the events of the topics
  your handler gave it. Derive topics from the authenticated identity, never from an unchecked
  query parameter, and answer 401 or 403 yourself before calling `Serve`.
- **Exactly once.** An event published on several topics reaches a subscriber of more than one
  of them once. Every subscriber receives events in the same order.
- **Publishing never waits.** Each subscriber has a bounded queue (`WithBuffer`, 64 events). A
  client that falls behind is disconnected with `ErrSlowConsumer` and resumes from the history
  when it reconnects. Logging `ErrSlowConsumer` tells you which clients cannot keep up.
- **Identifiers.** An event without an `ID` gets one from the broker: a random prefix chosen when
  the broker is built, then a sequence number. Explicit identifiers are used as they are, and an
  identifier still in the history is refused with `ErrDuplicateID`. Do not reuse an entity's
  identifier as the event's: two updates of the same order would collide.
- **Keep-alive.** A subscription that stays silent for 15 seconds (`WithKeepAlive`) receives a
  comment, so that proxies keep the connection open.
- **Outcomes of `Serve`**: `nil` when the client leaves, `ErrClosed` on shutdown,
  `ErrSlowConsumer`, or a write error. The response always completes with status 200, so that
  browsers reconnect. Without topics, or when the writer cannot flush, `Serve` returns an error
  and writes nothing, so that your handler can answer it.

## Integrating into an existing server

A service that already has its middleware chain, its authentication, its timeouts and its
shutdown procedure needs three changes: a route that calls `Serve`, `Publish` calls in the
domain code, and the shutdown hook.

```go
func run(ctx context.Context, logger *slog.Logger, sessions *session.Manager) error {
	broker, err := sse.NewBroker(sse.WithRetry(3 * time.Second))
	if err != nil {
		return err
	}

	// The chain you already have (github.com/paccolamano/go/middleware and auth).
	rid, err := requestid.New()
	if err != nil {
		return err
	}
	alog, err := accesslog.New(logger)
	if err != nil {
		return err
	}
	rec, err := recovery.New(logger)
	if err != nil {
		return err
	}
	comp, err := compress.New()
	if err != nil {
		return err
	}
	authn, err := auth.New(sessions)
	if err != nil {
		return err
	}
	limit, err := maxconcurrency.New(200)
	if err != nil {
		return err
	}
	base := middleware.New(rid, alog, rec, comp, authn)

	mux := http.NewServeMux()
	// API routes keep the concurrency limit.
	mux.Handle("POST /orders", limit(createOrder(broker)))
	// The stream does not: it holds its slot for as long as it is open.
	mux.HandleFunc("GET /events", func(w http.ResponseWriter, r *http.Request) {
		p, _ := auth.FromContext(r.Context())
		err := broker.Serve(w, r, "user:"+p.Subject, "broadcast")
		if err != nil && !errors.Is(err, sse.ErrClosed) {
			logger.WarnContext(r.Context(), "event stream ended", "err", err)
		}
	})

	srv := &http.Server{
		Addr:              ":8080",
		Handler:           base.Then(mux),
		ReadHeaderTimeout: 5 * time.Second,
		WriteTimeout:      10 * time.Second, // keep it: streams manage their own deadline
	}
	srv.RegisterOnShutdown(broker.Close) // otherwise Shutdown waits for open streams

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()
	if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func createOrder(broker *sse.Broker) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		order, err := saveOrder(r) // your code
		if err != nil {
			problem.Write(w, r, err)
			return
		}
		data, _ := json.Marshal(order)
		if err := broker.Publish(sse.Event{Type: "order.created", Data: string(data)}, "user:"+order.Owner); err != nil {
			slog.ErrorContext(r.Context(), "publish", "err", err)
		}
		w.WriteHeader(http.StatusCreated)
	}
}
```

How the rest of the stack behaves with a stream:

| Component | Behavior with a stream |
|-----------|------------------------|
| `middleware/compress` | compresses `text/event-stream` (it matches `text/*`) and delivers every flush at once. The gzip header goes out with the first event, the response headers at once |
| `middleware/etag` | a flushed response passes through without an `ETag` |
| `middleware/maxconcurrency` | a stream holds its slot while open: **do not wrap stream routes** |
| `middleware/ratelimit` | limits connection attempts, which bounds reconnection storms after a deploy |
| `middleware/accesslog` | logs the stream when it ends, with the duration of the connection |
| `middleware/cors` | needed when the page is on another origin; see [Frontend notes](#frontend-notes) |
| `auth` with sessions | the session cookie travels with the browser's `EventSource` |
| `auth` with tokens or API keys | browsers cannot set `Authorization` on an `EventSource`: use them for clients written in Go or other languages |
| `auth` rejections | a 401 or 403 makes browsers stop reconnecting for good, which is right for an expired session |
| `http.TimeoutHandler` | cannot flush: `Serve` and `NewStream` return `ErrStreamingUnsupported` |

Every writer of the `middleware` module implements `Unwrap`, so the per-write deadlines reach the
connection. A writer of another library that hides `http.Flusher` makes `Serve` return
`ErrStreamingUnsupported`; one that hides only the deadline leaves streams bounded by the
server's `WriteTimeout`.

## Reconnection and resuming

Browsers reconnect on their own when a connection drops, and send the identifier of the last
event they received in the `Last-Event-ID` header. The broker keeps the most recent events in
memory (`WithHistory`, 1,000 by default) and resumes the client from there:

1. a laptop receives events up to `e5`, then goes to sleep;
2. `e6` to `e9` are published;
3. the laptop wakes up, the browser reconnects with `Last-Event-ID: e5`;
4. the broker sends `e6` to `e9` on the client's topics, then the new events, none of them
   twice.

The broker also sends a **position** to every new subscription: the identifier of the newest
event in the history, without an event. A client that disconnects before its first event can
then resume too. Browsers handle positions on their own.

### When missed events cannot be recovered

Suppose the laptop slept for an hour, and 3,000 events were published meanwhile. The history
holds the last 1,000, so `e6` to `e2000` are gone. The same happens when the server restarts,
because the history is in memory, or when the identifier comes from another process.

By default the client then receives the new events only, and a position so that it stops sending
the old identifier. If the frontend shows state built from events, for example the status of
orders, it now shows stale data, and nobody notices.

Configure a **reset event** to tell the client:

```go
broker, err := sse.NewBroker(sse.WithResetEvent(sse.Event{Type: "reset"}))
```

```js
es.addEventListener("reset", () => reloadOrders()); // fetch the current state again
```

The broker sets the reset event's identifier to the newest one in the history, so the next
reconnection resumes normally instead of triggering another reset. The reset event is opt-in:
for notifications that are not worth reloading, such as toasts, losing a few is fine.

To recover events older than the history, from a durable message bus, see the next section.

## Replay from a durable bus

When your events already flow through a durable bus (Redis Streams, Kafka, RabbitMQ Streams),
the broker can read missed events from it when a client was away longer than the history covers,
or when the replica restarted. You write the read; the broker handles the rest:

```go
// go-redis v9, sketch: adapt to your client version.
replay := func(ctx context.Context, lastID string, topics []string, send func(sse.Event, ...string) error) error {
	msgs, err := rdb.XRange(ctx, "events", "("+lastID, "+").Result() // after lastID, excluded
	if err != nil {
		if isInvalidID(err) {
			return sse.ErrUnknownID // the bus does not know lastID either
		}
		return err
	}
	for _, m := range msgs {
		ev := sse.Event{ID: m.ID, Type: m.Values["type"].(string), Data: m.Values["data"].(string)}
		if err := send(ev, strings.Split(m.Values["topics"].(string), ",")...); err != nil {
			return err // the client left, fell behind, or the event is invalid
		}
	}
	return nil
}
broker, err := sse.NewBroker(sse.WithReplay(replay), sse.WithResetEvent(sse.Event{Type: "reset"}))
```

- **Memory first.** The function is called only when `Last-Event-ID` is not in the history, so
  the reconnection storm after a deploy does not all hit the bus.
- **Every event with its topics.** `send` takes the event and its topics, like `Publish`, and the
  broker filters them by the subscription's topics. Forgetting to filter in your query can then
  never disclose another user's events. The `topics` argument is there only to narrow the query.
- **No loss, no duplicates.** The subscription is registered before the function is called, so
  events published meanwhile wait in its queue. When the replay ends, those it already sent are
  skipped. This assumes that the bus and the broker see events in the same order, as when the
  broker is fed by the bus (see the next section). If they do not, the worst case is a duplicate,
  never a loss.
- **`ErrUnknownID` against an empty replay.** Return `ErrUnknownID` when the bus does not know
  `lastID` (it was trimmed, or it is not a bus identifier): the client gets the reset event, if
  configured. Returning nil without calling `send` means that `lastID` is known and nothing
  follows it.
- **Failures are retried.** Any other error, a panic included, ends the stream without live
  events, and `Serve` returns the error. The client reconnects after the reconnection time with
  the identifier of the last event it received, and the replay runs again: a bus that is down for
  a while never loses events silently. Log the error returned by `Serve`.
- **Keep it short.** No keep-alive is written while the function runs. Return within the idle
  timeout of your proxies, typically 60 seconds; a long backlog is fine, a slow query is not.
- `send` returns an error once the client has left or fallen behind, or after the function has
  returned: stop and return it.

## Several replicas

A broker lives in one process. With several replicas behind a load balancer, an event published
on replica A must also reach the clients connected to replica B. The library never talks to your
message bus: you connect them in a few lines.

```text
POST /orders ── publish ──▶ bus "events" ──▶ replica A: feed loop ─▶ broker.Publish ─▶ its clients
                                         └─▶ replica B: feed loop ─▶ broker.Publish ─▶ its clients
```

1. The domain code publishes to the bus, not to the broker.
2. Each replica runs a feed loop that reads **every** message of the bus and calls
   `broker.Publish` with the **bus message identifier** as the event identifier. Every replica then
   holds the same identifiers in the same order, so a client that reconnects to another replica
   resumes from its history.
3. At-least-once buses re-deliver messages after a reconnection of the feed: ignore
   `ErrDuplicateID`.

```go
// go-redis v9, sketch: adapt to your client version.
// Publishing, in the domain code:
err := rdb.XAdd(ctx, &redis.XAddArgs{
	Stream: "events",
	MaxLen: 100_000, Approx: true, // the stream trims itself
	Values: map[string]any{"type": "order.created", "data": data, "topics": "user:" + owner},
}).Err()

// Feeding, in every replica:
func feed(ctx context.Context, rdb *redis.Client, broker *sse.Broker) error {
	last := "$" // messages added from now on
	for {
		res, err := rdb.XRead(ctx, &redis.XReadArgs{Streams: []string{"events", last}, Block: 0}).Result()
		if err != nil {
			return err // restart with backoff, from last
		}
		for _, m := range res[0].Messages {
			last = m.ID
			ev := sse.Event{ID: m.ID, Type: m.Values["type"].(string), Data: m.Values["data"].(string)}
			err := broker.Publish(ev, strings.Split(m.Values["topics"].(string), ",")...)
			if err != nil && !errors.Is(err, sse.ErrDuplicateID) {
				return err
			}
		}
	}
}
```

| System | How every replica receives every message | Stable identifier, same order everywhere | Replay (`WithReplay`) |
|--------|------------------------------------------|------------------------------------------|-----------------------|
| Redis Streams | `XREAD` without a consumer group | yes: the entry ID | yes: `XRANGE` after `lastID` |
| Redis Pub/Sub | every subscriber receives every message | an ID set by the publisher, in the order of the Redis server | no: Pub/Sub keeps nothing |
| Kafka | no consumer group, or one group per replica | yes: the offset, with a single partition | yes: a consumer that starts at offset `lastID + 1` and reads up to the high watermark |
| RabbitMQ classic queues | a fanout exchange with one exclusive queue per replica | an ID set by the publisher (`MessageId`) | no: a consumed message is gone |
| RabbitMQ Streams | each replica runs its own consumer | yes: the offset | yes: a consumer that starts at offset `lastID + 1` |

Two mistakes to avoid:

- **A consumer group shared by the replicas.** In Kafka and Redis Streams, a consumer group
  *divides* the messages among its members: each replica would receive only part of them, and its
  clients would miss events. Every replica must read everything.
- **Kafka with several partitions.** Order is guaranteed only within a partition, so replicas
  could see events in different orders and resume a client at the wrong place. Use a single
  partition for the event stream.

Kafka, with franz-go (sketch: adapt to your client version):

```go
cl, err := kgo.NewClient(
	kgo.SeedBrokers("kafka:9092"),
	kgo.ConsumeTopics("events"), // a single partition
	kgo.ConsumeResetOffset(kgo.NewOffset().AtEnd()),
	// No kgo.ConsumerGroup: every replica reads every record.
)
for {
	fetches := cl.PollFetches(ctx)
	if errs := fetches.Errors(); len(errs) > 0 {
		return errs[0].Err
	}
	fetches.EachRecord(func(r *kgo.Record) {
		ev := sse.Event{ID: strconv.FormatInt(r.Offset, 10), Data: string(r.Value)}
		_ = broker.Publish(ev, topicsFromHeaders(r.Headers)...) // ignore ErrDuplicateID, log the rest
	})
}
```

RabbitMQ classic queues, with amqp091-go (sketch: adapt to your client version):

```go
ch.ExchangeDeclare("events", "fanout", true, false, false, false, nil)
q, _ := ch.QueueDeclare("", false, true, true, false, nil) // server-named, exclusive: one per replica
ch.QueueBind(q.Name, "", "events", false, nil)
deliveries, _ := ch.Consume(q.Name, "", true, true, false, false, nil)
for d := range deliveries {
	topics, _ := d.Headers["topics"].(string)
	ev := sse.Event{ID: d.MessageId, Type: d.Type, Data: string(d.Body)}
	_ = broker.Publish(ev, strings.Split(topics, ",")...) // ignore ErrDuplicateID, log the rest
}
```

Without a bus identifier, the broker generates identifiers with a random prefix per instance: a
client that moves to another replica then gets the reset event, or a position, instead of a wrong
resume.

## Shutting down

`http.Server.Shutdown` waits for active connections to become idle, and an event stream never
does on its own: without help, every shutdown waits for its full deadline, then cuts the
connections. Register the broker's `Close` as a shutdown hook:

```go
srv.RegisterOnShutdown(broker.Close)
```

When `Shutdown` starts, `Close` ends every stream by completing its response normally, and `Serve`
returns `ErrClosed`. `Shutdown` then returns as soon as the handlers have returned.

- **Clients reconnect elsewhere.** A response that ends normally makes browsers reconnect after
  the reconnection time (`WithRetry`), to another replica behind the load balancer, with their
  `Last-Event-ID`. A status other than 200 would make them stop reconnecting for good, which is
  why the library never answers one on its own: a `Serve` that arrives after `Close` gets an
  empty 200 stream and returns `ErrClosed`.
- After `Close`, `Publish` returns `ErrClosed`. Calling `Close` again does nothing.
- **Single streams** end when their handler returns. If a handler must end on shutdown, have it
  select on a channel that your own shutdown hook closes.

## Frontend notes

```js
const es = new EventSource("/events"); // same origin: the session cookie is sent

es.addEventListener("order.created", (e) => render(JSON.parse(e.data)));
es.addEventListener("reset", () => reloadOrders()); // only with WithResetEvent
es.onmessage = (e) => console.log(e.data);         // events without a type
es.onerror = () => {
  if (es.readyState === EventSource.CLOSED) {
    // The server answered something other than 200, such as 401: the browser gave up.
    // Sign in again, then create a new EventSource.
  }
};
```

- **Named events** (`Event.Type`) are delivered only to `addEventListener` with that name;
  `onmessage` receives events without a type.
- **Reconnection is automatic**, with `Last-Event-ID`: do not write your own. The browser waits
  for the server's `retry` (`WithRetry`), or about 3 seconds.
- **Any status other than 200 stops reconnection for good.** The library never sends one on its
  own; a 401 or 403 from your authentication does, and the page must handle it.
- **Another origin**: create the source with `new EventSource(url, { withCredentials: true })` and
  allow credentials in the CORS middleware for that origin.
- **HTTP/1.1 allows about six connections per domain**, and each tab with an `EventSource` holds
  one. Serve over HTTP/2, where streams share one connection, or keep to one source per page.

## Consuming from Go

`Client` consumes a stream with the reconnection rules of browsers: an internal service, or a
third-party API that streams its answers.

```go
client, err := sse.NewClient(sse.WithHTTPClient(httpClient))
if err != nil {
	return err
}
req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.example.com/events", nil)
if err != nil {
	return err
}
req.Header.Set("Authorization", "Bearer "+token)

for ev, err := range client.Events(req) {
	if err != nil {
		return err // only terminal errors: transient ones are retried
	}
	handle(ev)
}
```

- **Retried**: network errors and streams that end normally. The client waits for the
  reconnection time (the server's `retry`, 3 seconds by default), doubling the delay after each
  consecutive failure up to 30 seconds, and sends `Last-Event-ID` with the last identifier
  received, positions included. Headers and body go out again on every connection, so use a body
  with `GetBody`, as `http.NewRequest` sets for `bytes` and `strings` readers.
- **Not retried**, as the standard requires: 204 ends the iteration without an error; any other
  status, or a content type other than `text/event-stream`, ends it with a `*ResponseError`. To
  retry deliberately, for example on 503, check `StatusCode` and start a new iteration.
- `ErrEventTooLarge` ends the iteration when an event exceeds `WithMaxEventSize` (1 MiB by
  default): a server cannot make the client use unbounded memory.
- Cancel the request's context to stop; breaking out of the loop closes the connection too.
- **Give it an `http.Client` without `Timeout`.** An overall timeout ends every stream when it
  expires; set timeouts on the transport instead (`DialContext`, `TLSHandshakeTimeout`,
  `ResponseHeaderTimeout`).

For one-shot parsing, of a recorded stream or of a response in a test, use `Decode`:

```go
for ev, err := range sse.Decode(resp.Body) {
	...
}
```

## Testing handlers

- **No sleeps.** `Serve` registers the subscription before it sends the response headers: as soon
  as your test's `client.Do` returns, publish and expect the event.
- **Decode the response** with `sse.Decode(resp.Body)` instead of parsing lines by hand.
- **Read in a goroutine**, as a real client would, when the handler writes more than you consume.
- **Virtual time.** Run the test inside `testing/synctest` to check keep-alives, write timeouts
  and reconnection delays without waiting. Serve over an in-memory listener (a `net.Listener`
  whose connections are `net.Pipe`s), and create the broker inside the bubble.

```go
func TestNotifications(t *testing.T) {
	broker, err := sse.NewBroker()
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(eventsHandler(broker)) // your handler, which calls broker.Serve
	defer srv.Close()

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, srv.URL+"/events", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	// The subscription is registered: publish right away.
	if err := broker.Publish(sse.Event{Type: "order.created", Data: "42"}, "user:alice"); err != nil {
		t.Fatal(err)
	}
	for ev, err := range sse.Decode(resp.Body) {
		if err != nil {
			t.Fatal(err)
		}
		if ev.Type != "order.created" || ev.Data != "42" {
			t.Fatalf("got %+v", ev)
		}
		break
	}
}
```

## Defaults and limits

| Setting | Default | Option |
|---------|---------|--------|
| Deadline of each write to a stream | 10 s | `WithWriteTimeout` |
| Keep-alive comment after a silent subscription | 15 s | `WithKeepAlive` |
| Events kept for resuming | 1,000 | `WithHistory` |
| Events waiting for one subscriber | 64 | `WithBuffer` |
| Reconnection time sent to clients | none (browsers use about 3 s) | `WithRetry` |
| Reset event | none | `WithResetEvent` |
| Client reconnection time | 3 s, or the server's `retry` | — |
| Client maximum delay after consecutive failures | 30 s, or the server's `retry` if larger | — |
| Maximum size of a decoded event | 1 MiB | `WithMaxEventSize` |

**Memory.** The broker holds at most `WithHistory` events, plus at most `WithBuffer` events per
subscriber: roughly (history + subscribers × buffer) × event size. The broker does not limit the
size of an event; keep events small and send identifiers rather than documents when they grow. A
client holds at most one event of `WithMaxEventSize`.

**Goroutines.** The module starts none. A subscription runs in the goroutine of its request, and
the client in the goroutine that iterates.

**Context.** `Stream`, `Serve` and `Client.Events` use the context of the HTTP request, as
`net/http` does, rather than a separate parameter: when the client goes away, everything stops.

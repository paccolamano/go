# Feature Specification: Server-Sent Events (sse)

**Feature Branch**: `008-server-sent-events` (suggested; not yet created)

**Created**: 2026-10-21

**Status**: Draft

**Input**: User description: "analizziamo la libreria sse [...] direi di includere WithReplay già
ora. L'utilizzo della libreria sse e i diversi flow dovrebbero essere documentati per bene." The
design was agreed in conversation; the full request, as submitted, is reproduced verbatim in
[Appendix: original request](#appendix-original-request).

## Context

Server-sent events let a server push a stream of events to a client over one long-lived HTTP
response. Browsers consume them natively and reconnect on their own. Sending a few lines of text
looks trivial, and that is why every service writes its own, and gets the same things wrong:

- the server's write timeout ends every stream after a few seconds, or it is removed for the
  whole server, which loses its protection everywhere else;
- a proxy buffers the response, or closes a connection that stays silent for a minute;
- a line break inside the data, or an identifier containing a newline, corrupts the stream or
  injects fields;
- a graceful shutdown waits for open streams until its deadline, because they never end on their
  own;
- an error answered with a status other than 200 makes browsers stop reconnecting, forever;
- a client that reconnects loses the events published while it was away, or receives some twice
  when missed events are replayed while new ones keep arriving;
- one slow client blocks the publisher, or makes memory grow without limit;
- with several replicas, a client reconnects to a replica that never saw its last event;
- consuming a stream from Go mishandles a carriage return split across two reads, the byte order
  mark, or the rule that the last event identifier persists across events.

None of these is solved by a function of a few lines, which is why they belong in a library.

This feature delivers a new, independent module in this repository (Constitution, Principle I),
named `sse`. It implements the server and client sides of server-sent events as defined by the
WHATWG HTML Living Standard, section "Server-sent events" (the standard):

- **stream**: one response whose events are produced by the developer's handler, for example the
  progress of a job;
- **broker**: events published once and delivered to many clients by topic, with an in-memory
  history to resume after a reconnection, an optional reset event when missed events cannot be
  recovered, an optional replay from the developer's durable message bus, and a shutdown that
  lets clients reconnect elsewhere;
- **client**: a decoder of event streams and a client that reconnects as browsers do;
- **documentation**: every flow, from a single stream to several replicas fed by a message bus,
  documented with examples.

**The library does not know the developer's message bus.** It never connects to Redis, Kafka,
RabbitMQ or any other system. The developer connects them through two integration points:

1. **publishing**: the developer's code publishes events to the broker, either directly or from
   a loop that reads the bus in every replica;
2. **replay**: an optional function, written by the developer, that reads missed events from the
   bus when the in-memory history does not cover them.

The Go language, the standard library's HTTP server and client, and the standard itself are given
by the request and by the constitution (v1.2.0). They are not design choices made here. They
appear in this spec because the protocol and the HTTP behavior are the observable contract of
this library. How the module is built is left to `/speckit-plan`.

**Actors**:

- **Developer**: adds event streams to a service, publishes events, consumes streams from Go, and
  tests their handlers.
- **Frontend developer**: consumes the streams in a browser with the native event source.
- **Operator**: runs the service as one or more replicas, deploys and shuts them down.
- **Maintainer**: evolves the module and publishes its releases.

### Clarifications

#### Session 2026-10-21

- Q: When a client's last event identifier cannot be resolved, should the broker send a reset
  event by default? → A: No. The reset event is opt-in and disabled by default (FR-035).
- Q: Should operations that block take an explicit context parameter, as Principle IV reads, or
  the context of the HTTP request? → A: The context of the request, as the standard library's
  HTTP package does for its server and client. This is the agreed interpretation of Principle IV
  for this module (FR-006).
- Q: Should the first release include the client side? → A: Yes: the decoder and the reconnecting
  client (User Story 8).
- Q: Should the history be pluggable through a store interface, for example on Redis? → A: No
  store interface. The history is in memory. An optional replay function reads from the
  developer's durable bus, and it ships in the first release (User Story 6).
- Q: How much documentation is expected? → A: Usage and every flow documented thoroughly, in the
  README and the package documentation, with runnable examples (User Story 4, FR-060 to FR-065).
- Q: Who enforces the topic filter on replayed events? → A: The broker. The replay function
  provides each event together with its topics, and still receives the subscription's topics to
  narrow what it reads. A filter forgotten by the developer can then never disclose other
  subscribers' events (FR-038, FR-039).
- Q: What happens when the replay function fails? → A: The stream ends normally without live
  events and the subscription reports the failure. The client reconnects with the same last event
  identifier, and the replay is attempted again, so a transient failure never loses events
  silently (FR-042).
- Q: After `/speckit-analyze` (finding C1), is the context deviation from Principle IV accepted?
  → A: Yes, as the deviation recorded in the plan's Complexity Tracking. A constitution amendment
  that codifies the exception will come in a separate pull request.
- Q: What happens when the developer's replay function panics? → A: The panic is recovered and
  treated as a replay failure, and `send` called after the function has returned reports an
  error (FR-007, FR-042).
- Q: Does the keep-alive interval apply while a replay is in progress? → A: No, it applies to the
  live phase. The documentation asks replay functions to return within the idle timeout of
  proxies (FR-029).

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Stream events from a handler (Priority: P1)

A developer streams events produced by a single handler to the client that made the request, for
example the progress of a long job or the tokens of a generated answer. The developer only sends
events. The library sets the response up so that proxies and browsers treat it as a stream,
encodes every event safely, and keeps the server's write timeout from ending it.

**Why this priority**: it is the foundation of every other server flow, and on its own it already
removes the most common mistakes: buffering, broken encoding and streams ended by the server's
write timeout.

**Independent Test**: open a stream in a handler served by a test server with a write timeout,
send events of every shape, and decode what the client receives. Repeat with a client that stops
reading and with a response writer that cannot flush.

**Acceptance Scenarios**:

1. **Given** a handler that opens a stream, **When** a client connects, **Then** it receives
   status 200 with `Content-Type: text/event-stream`, `Cache-Control: no-store` and
   `X-Accel-Buffering: no` before any event is sent. Headers set earlier by middleware, such as
   CORS headers, are kept.
2. **Given** an open stream, **When** the handler sends an event of type `progress` with data
   `50%`, **Then** the client decodes that event at once, not when the response ends.
3. **Given** an open stream, **When** the handler sends data `line1\r\nline2\rline3\n`, **Then**
   the client receives the data `line1\nline2\nline3\n`.
4. **Given** an open stream, **When** the handler sends an event with empty data, **Then** the
   client dispatches an event with empty data.
5. **Given** an open stream, **When** the handler sends an event whose identifier contains a line
   feed, or whose data is not valid UTF-8, **Then** the send is refused with an invalid-event
   error and nothing is written. **When** the handler then sends a valid event, **Then** the
   client receives it intact.
6. **Given** a server with a write timeout of 10 seconds and a stream that stays idle for longer,
   **When** the handler then sends an event, **Then** the client receives it.
7. **Given** a client that stops reading, **When** the handler keeps sending, **Then** a send
   fails within the configured write timeout, and the handler can return.
8. **Given** a response writer that cannot flush, such as one wrapped by the standard library's
   timeout handler, **When** the handler opens a stream, **Then** a streaming-unsupported error
   is returned and nothing is written, so the handler can answer with an error of its choice.
9. **Given** a reconnection time of 5 seconds configured for the stream, **When** a client
   connects, **Then** the stream sets the client's reconnection time to 5 seconds before any
   event.
10. **Given** a client that has disconnected, **When** the handler sends an event, **Then** the
    send reports an error promptly.

---

### User Story 2 - Deliver events to many clients by topic (Priority: P1)

A developer publishes an event once, from anywhere in the service, and every connected client
subscribed to one of its topics receives it. The developer's handler decides which topics a
request may follow, for example from the authenticated user. Publishing never waits for clients,
and a slow client cannot slow down the others.

**Why this priority**: notifications, live dashboards and chat are the common uses of
server-sent events, and they all need fan-out. Getting it right under concurrency and slow
clients is the hardest part to write by hand.

**Independent Test**: subscribe clients to different topics through a broker served by a test
server, publish from concurrent tasks, and compare what each client receives. Stall some clients
and keep publishing.

**Acceptance Scenarios**:

1. **Given** client A subscribed to `user:alice` and client B subscribed to `user:bob`, **When**
   an event is published on `user:alice`, **Then** A receives it and B does not.
2. **Given** a client subscribed to `user:alice` and `broadcast`, **When** an event is published
   on both topics, **Then** the client receives it once.
3. **Given** a client that has just received the response headers of its subscription, **When**
   an event is published immediately, without any wait, **Then** the client receives it.
4. **Given** 1,000 subscribers of which 999 have stopped reading, **When** 10,000 events are
   published, **Then** no publish call waits for a subscriber. The streams of the stalled
   subscribers end with the slow-consumer outcome once their queues are full, and the active
   subscriber receives all 10,000 events in order.
5. **Given** a subscription on which nothing is written for the keep-alive interval, **When** the
   interval elapses, **Then** the client receives a comment, which it ignores.
6. **Given** an event published without an identifier, **When** it is delivered, **Then** it
   carries an identifier generated by the broker. Identifiers generated by two broker instances
   never coincide.
7. **Given** an event with identifier `42` already published, **When** another event with
   identifier `42` is published, **Then** it is refused with a duplicate-identifier error and
   clients receive identifier `42` once.
8. **Given** a publish call with no topic, or with an invalid event, **When** it is made, **Then**
   it returns an error and nothing is delivered or recorded.
9. **Given** a subscribed client, **When** it disconnects, **Then** the subscription ends without
   an error, and later events are no longer queued for it.

---

### User Story 3 - Resume after a reconnection without losing events (Priority: P1)

A browser loses its connection, for example when a laptop sleeps or a network changes, and
reconnects on its own with the identifier of the last event it received. The broker sends the
events it missed, then the new ones, without gaps and without duplicates. When the missed events
are no longer available, the developer can choose to have the client told, so that the frontend
reloads its state instead of silently showing stale data.

**Why this priority**: without it, every disconnection silently loses events, which is the main
reason a hand-written stream shows stale data.

**Independent Test**: connect a client, disconnect it at chosen points while events keep being
published, reconnect it with its last identifier, and compare the received events with the
published ones. Repeat with an identifier older than the history, with and without a reset event.

**Acceptance Scenarios**:

1. **Given** a client that received events up to `e5` and disconnected while `e6` to `e9` were
   published, **When** it reconnects with last event identifier `e5`, **Then** it receives `e6`
   to `e9` in order, then the new events, none of them twice.
2. **Given** the same disconnection, with `e7` published on a topic the client does not follow,
   **When** it reconnects, **Then** it receives `e6`, `e8` and `e9` only.
3. **Given** a client that connects without a last event identifier while the history ends with
   `e9`, **When** it disconnects before receiving any event while `e10` is published, and then
   reconnects, **Then** it receives `e10`.
4. **Given** a last event identifier older than the history, no replay function and no reset
   event, **When** the client reconnects, **Then** it receives the new events only.
5. **Given** the same identifier and a reset event of type `reset`, **When** the client
   reconnects, **Then** the first event it receives is the reset event, followed by the new
   events. **When** it reconnects again with the identifier carried by the reset event, **Then**
   it receives no second reset event.
6. **Given** a broker that was restarted, **When** a client reconnects with an identifier issued
   by the previous process, **Then** the identifier is never mistaken for one issued by the new
   process: it is treated as unresolvable.
7. **Given** a history of 3 events, **When** 10 events are published, **Then** only the last 3
   are kept for resuming.

---

### User Story 4 - Integrate into an existing server by following the documentation (Priority: P1)

A developer adds event streams to a service that is already in production, with its middleware
chain, its authentication, its timeouts and its shutdown procedure, by reading the README and the
package documentation only. Every flow has its own section that explains when to use it and shows
working code.

**Why this priority**: the user asked explicitly for thorough documentation of usage and flows.
Most of the mistakes listed in the Context come from integration, not from the protocol, so a
library that is correct but poorly documented still leads to broken deployments.

**Independent Test**: a reviewer who did not take part in the design follows the README to add a
notification stream and a job progress stream to a sample server that uses this repository's
middleware and session authentication, scaled to two replicas fed by a bus. Every runnable example
passes as a test.

**Acceptance Scenarios**:

1. **Given** the README, **When** a developer looks for how to add a notification stream to a
   server that already uses the middleware chain and session cookies, **Then** they find a
   complete example: the route, topics derived from the authenticated user, the shutdown hook,
   the server timeouts to keep, and the middleware to keep off the route.
2. **Given** the README, **When** a developer looks for any flow listed in FR-060, **Then** it has
   its own section with an explanation of when to use it and a code example.
3. **Given** the package documentation, **When** its runnable examples are executed as tests,
   **Then** they pass.
4. **Given** a developer whose service uses Kafka, **When** they read the section on several
   replicas, **Then** they learn that each replica needs its own consumer group, that ordering
   needs a single partition, and how offsets serve as event identifiers and as the starting point
   of a replay.
5. **Given** a frontend developer, **When** they read the browser notes, **Then** they learn how
   to listen to named events, how to handle the reset event, that 401 and 403 answers stop
   reconnection, how credentials work across origins, and why HTTP/2 matters for browsers that
   limit connections per domain.

---

### User Story 5 - Shut down without hanging, and let clients reconnect elsewhere (Priority: P2)

An operator deploys a new version. Each replica being stopped ends its open streams at once, its
graceful shutdown completes, and the clients reconnect to the other replicas, where they resume
from their last event.

**Why this priority**: without it, every deploy waits for the full shutdown deadline and then cuts
the connections abruptly. It matters for every production deployment, but it builds on User
Stories 2 and 3.

**Independent Test**: open many streams on a test server whose shutdown hook closes the broker,
start a graceful shutdown with a generous deadline, and observe how long it takes and how each
response ends. Then call the broker after it is closed.

**Acceptance Scenarios**:

1. **Given** a server with 1,000 open streams and the broker's close registered as its shutdown
   hook, **When** a graceful shutdown starts, **Then** every stream ends and the shutdown
   completes without reaching its deadline.
2. **Given** streams ended by closing the broker, **When** their clients observe the end, **Then**
   each one sees a complete 200 response, so a client that follows the standard reconnects.
3. **Given** a closed broker, **When** a request is subscribed, **Then** it receives an empty 200
   stream that carries the configured reconnection time, and the subscription reports that the
   broker is closed. **When** an event is published, **Then** the publish call reports that the
   broker is closed.
4. **Given** a closed broker, **When** it is closed again, **Then** nothing happens.

---

### User Story 6 - Recover missed events from a durable message bus (Priority: P2)

A developer whose events already flow through a durable bus, such as Redis Streams, Kafka or
RabbitMQ Streams, lets the broker read missed events from it when a client was away longer than
the in-memory history covers, or when the replica was restarted. The broker switches from the
replayed events to the live ones without losing or duplicating any.

**Why this priority**: it extends resuming to long disconnections and restarts. It needs a durable
bus, which not every service has, and it builds on User Story 3.

**Independent Test**: configure a replay function over an in-memory list that stands in for the
bus, keep only part of the events in the broker's history, reconnect clients with old identifiers
while events keep being published, and compare the received events with the published ones.

**Acceptance Scenarios**:

1. **Given** a replay function over a source that holds `e1` to `e5000` and a history that holds
   the last 1,000 events, **When** a client reconnects with last event identifier `e100`,
   **Then** it receives `e101` to `e5000` in order, then the new events, none of them twice.
2. **Given** events published while the replay is in progress, some of which the replay also
   returns, **When** the replay ends, **Then** each of them is delivered exactly once.
3. **Given** a last event identifier found in the in-memory history, **When** the client
   reconnects, **Then** the replay function is not called.
4. **Given** a replay function that reports the identifier as unknown, **When** the client
   reconnects, **Then** the identifier is treated as unresolvable: the reset event is sent if it
   is configured, then the new events.
5. **Given** a replay function that provides `e101` to `e150` and then fails, **When** the failure
   occurs, **Then** the client has received `e101` to `e150`, the stream ends with a complete 200
   response without live events, and the subscription reports the failure. **When** the client
   reconnects with last event identifier `e150`, **Then** the replay is attempted again from
   `e150`.
6. **Given** a replay in progress, **When** the client disconnects, **Then** the replay is
   cancelled through its context and the subscription ends.
7. **Given** a replay function that provides events on topics the client does not follow, and an
   event on two of the client's topics, **When** the client reconnects, **Then** the broker
   delivers only the events on the client's topics, and the event on two of them once.
8. **Given** a replay longer than the subscriber's queue while live events keep arriving, **When**
   the queue fills, **Then** the stream ends with the slow-consumer outcome. **When** the client
   reconnects with the last identifier it received, **Then** the replay continues from there.

---

### User Story 7 - Run behind several replicas (Priority: P2)

An operator runs the service as several replicas behind a load balancer. Events published on any
replica reach clients connected to every replica, because the developer feeds each replica's
broker from a shared bus, using the bus message identifiers. A client that reconnects to a
different replica resumes from its last event.

**Why this priority**: most production services run more than one replica, but the library's part
is small: identifiers taken verbatim, duplicates refused, and resuming that works across
instances. The rest is the developer's feed, covered by the documentation (User Story 4).

**Independent Test**: feed two broker instances with the same events, identifiers and order, as a
bus would, then move a client from one to the other. Re-deliver some events to one instance, as an
at-least-once bus does after a reconnection.

**Acceptance Scenarios**:

1. **Given** two brokers fed with the same events, with the same identifiers and in the same
   order, **When** a client connected to the first reconnects to the second with its last event
   identifier, **Then** it receives exactly the events it missed.
2. **Given** a feed that re-delivers an event still in the history, **When** it is published
   again, **Then** it is refused with a duplicate-identifier error, which the feed can ignore,
   and clients receive the event once.
3. **Given** two brokers that generate their own identifiers, **When** a client moves from one to
   the other, **Then** its identifier is never matched to a position in the other broker's
   history: it is treated as unresolvable.

---

### User Story 8 - Consume event streams from Go (Priority: P2)

A developer consumes a stream from Go, for example an internal service or a third-party API that
streams its answers, with the same reconnection behavior as a browser. A developer testing their
own handlers decodes the responses with the same decoder.

**Why this priority**: it is needed to test streams without a browser, and for service-to-service
use, but browsers remain the main consumers.

**Independent Test**: serve scripted streams from a test server, including dropped connections,
reconnection times, error statuses and malformed input, and compare the events and outcomes the
client reports. Run the decoder over the examples of the standard and over fuzzed input.

**Acceptance Scenarios**:

1. **Given** a server that sends events, **When** the developer iterates over the client's
   events, **Then** each event carries its identifier, type and data.
2. **Given** a connection that drops after event `e3`, **When** the client reconnects after the
   reconnection time with last event identifier `e3`, **Then** iteration continues with the next
   events, and the developer sees no error.
3. **Given** a server that sets the reconnection time to 10 seconds, **When** the connection
   drops, **Then** the client waits 10 seconds before reconnecting. Tests verify this without
   real waiting.
4. **Given** a server that answers 204, **When** the client connects, **Then** iteration ends
   without an error. **Given** an answer of 401, **Then** iteration ends with an error that
   carries status 401, and the client does not reconnect. **Given** an answer of 200 with content
   type `text/html`, **Then** iteration ends with an error.
5. **Given** a request with an `Authorization` header and a body that can be sent again, **When**
   the client reconnects, **Then** every connection carries the same headers and body.
6. **Given** a request whose body cannot be sent again, **When** iteration starts, **Then** it
   ends at once with an error, before any connection is made.
7. **Given** an iteration in progress, **When** the request's context is cancelled, **Then**
   iteration ends promptly with the context's error. **When** the developer stops iterating,
   **Then** the connection is closed.
8. **Given** each example of event stream in the standard, **When** it is decoded, **Then** the
   decoder produces exactly the events the standard describes.
9. **Given** an event larger than the maximum event size, **When** it is decoded, **Then**
   decoding ends with an event-too-large error.

### Edge Cases

- **Carriage return split across reads**: a carriage return at the end of one read followed by a
  line feed at the start of the next counts as a single line break.
- **Byte order mark**: one byte order mark at the very start of a stream is skipped. One anywhere
  else is part of the data.
- **Malformed UTF-8 from a server**: the decoder replaces invalid sequences with the replacement
  character, as the standard requires, instead of failing.
- **Lines without a colon**: the whole line is the field name and the value is empty, so a line
  `data` adds an empty line to the data.
- **Unterminated event at the end of a stream**: discarded, as the standard requires.
- **Invalid reconnection time**: a `retry` value that is not made only of ASCII digits is
  ignored.
- **Identifier containing NUL**: refused by the server side. On the client side the `id` field is
  ignored, as the standard requires.
- **Leading spaces**: data, type and identifier values that start with spaces arrive with the same
  spaces.
- **Line break normalization**: carriage returns and CRLF pairs in data arrive as line feeds. The
  documentation says so.
- **Empty `Last-Event-ID` header**: treated as absent.
- **Last event identifier on a topic the client does not follow**: the broker resumes from that
  position and delivers only events on the client's topics.
- **Topics**: any non-empty string, compared exactly. A topic named twice in one call counts once.
- **Publishing with no subscriber**: the event is only recorded in the history.
- **Large events**: the broker does not limit event size. Its memory is bounded by the history
  size times the size of the events, and the documentation says so.
- **Write deadline not adjustable**: when the response writer does not allow changing the write
  deadline, the stream still works, bounded by the server's write timeout. The documentation says
  so.
- **HTTP client with an overall timeout**: such a client ends every stream when the timeout
  expires. The documentation recommends transport-level timeouts instead.
- **Redirects**: followed by the HTTP client. The last event identifier is sent again after a
  redirect.
- **Reset event**: its identifier is always set by the broker (FR-036). An invalid type or data in
  the configured reset event is reported when the broker is built.
- **Compression**: a compressing middleware may compress the stream, and clients decode it
  transparently. The stream flushes after every event, so the compressor delivers each one at
  once.
- **Explicit identifier equal to a generated one**: refused as a duplicate while the earlier event
  is in the history.
- **Replay that keeps failing**: while the source is unreachable, or while it holds an event that
  is invalid (FR-043), every reconnection attempts the replay again and fails. Each failure is
  reported by the subscription, so the developer's handler can log it.
- **Replay function that panics**: the panic is recovered and reported as a replay failure
  (FR-007, FR-042).
- **Slow replay**: no keep-alive is written while the replay function runs (FR-029). A replay
  that stays silent longer than a proxy's idle timeout may see its connection closed, and the
  client then reconnects.

## Requirements *(mandatory)*

### Functional Requirements

#### Module and shared conventions

- **FR-001**: The module MUST be delivered as one independent module in this repository, in the
  directory `sse`, with a single package named `sse` that contains the server and client sides.
- **FR-002**: The module MUST NOT depend on any module outside the standard library: neither
  third-party modules, nor message bus clients, nor the other modules of this repository. This
  includes its tests and runnable examples. Code that uses a bus client appears only as prose in
  the README.
- **FR-003**: The module MUST NOT start background work. Each stream runs in the task that serves
  its request. Publishing and closing return without waiting for subscribers. The client runs in
  the task that consumes its events. When every stream has ended and every iteration has stopped,
  no task started by the module remains.
- **FR-004**: Invalid configuration MUST be reported to the caller as an error when the stream,
  broker or client is built, never as a panic or a silent fallback. This includes non-positive
  durations and sizes, a missing function or HTTP client, and an invalid reset event.
- **FR-005**: The broker and the client MUST be safe for concurrent use. A stream MUST be safe for
  concurrent sends and keep-alives, and each write MUST reach the client whole, never interleaved
  with another.
- **FR-006**: Operations tied to an HTTP request MUST take their context from that request, as the
  standard library's HTTP server and client do. When that context ends, streams, subscriptions,
  replays and client iterations MUST end promptly.
- **FR-007**: The module MUST NOT panic, or let a panic escape, whatever the input from clients,
  servers or the developer's functions. A panic raised by the developer's replay function MUST be
  recovered and treated as a replay failure (FR-042).

#### Events

- **FR-008**: An event MUST consist of an optional identifier, an optional type and data.
- **FR-009**: Before anything is written, events MUST be validated. Identifiers MUST NOT contain
  NUL, carriage return or line feed. Types MUST NOT contain carriage return or line feed.
  Identifier, type and data MUST be valid UTF-8. An invalid event MUST be refused with a
  distinguishable invalid-event error, and nothing of it is written, delivered or recorded. The
  stream stays usable.
- **FR-010**: Events MUST be encoded as the standard defines:
  - each line of the data, split at carriage return, line feed or CRLF, becomes one data line;
  - an event with empty data still produces one data line, so that clients dispatch it;
  - an empty type produces no type field, and clients dispatch the event as `message`;
  - an empty identifier produces no identifier field;
  - leading spaces of every value are preserved.
- **FR-011**: Decoding an encoded valid event MUST return its type, its data with line breaks
  normalized to line feeds and, when it was set, its identifier. Fuzz tests MUST verify this
  round trip.

#### Single stream

- **FR-012**: Opening a stream MUST send status 200 with `Content-Type: text/event-stream`,
  `Cache-Control: no-store` and `X-Accel-Buffering: no`, keep the headers the handler or earlier
  middleware already set, and flush the response at once, so that the client sees the connection
  open before any event.
- **FR-013**: When the response cannot be flushed, opening a stream MUST return a distinguishable
  streaming-unsupported error and write nothing.
- **FR-014**: Each event and each keep-alive MUST be written and flushed at once.
- **FR-015**: The server's write timeout MUST NOT end a stream, and idle periods of any length
  between writes MUST NOT end it. A single write that does not complete within the configured
  write timeout, 10 seconds by default, MUST end the stream with an error, and later sends report
  an error. When the response writer does not allow changing the write deadline, the stream still
  works, bounded by the server's write timeout.
- **FR-016**: The developer MUST be able to configure a reconnection time, sent to the client when
  the stream opens. By default none is sent. It MUST be at least one millisecond and is sent in
  whole milliseconds.
- **FR-017**: The developer MUST be able to send a keep-alive, a comment line that clients
  ignore.
- **FR-018**: Once the client has disconnected or the request's context has ended, sends MUST
  report an error promptly and write nothing.
- **FR-019**: A stream MUST NOT interpret the client's last event identifier. The documentation
  shows how a handler reads it to resume on its own.

#### Broker: publishing and subscribing

- **FR-020**: Publishing MUST take an event and at least one topic, each a non-empty string. An
  invalid event or an invalid topic list MUST be refused with an error, and nothing is delivered
  or recorded. Publishing MUST NOT wait for subscribers: its duration does not depend on how
  subscribers read.
- **FR-021**: When an event has no identifier, the broker MUST assign one. Generated identifiers
  are unique within the broker instance, start with a random prefix chosen when the instance is
  built, and are valid per FR-009. Every delivered event therefore carries an identifier.
- **FR-022**: An identifier set by the developer MUST be used verbatim. An event whose identifier
  is already in the history MUST be refused with a distinguishable duplicate-identifier error,
  and nothing is delivered.
- **FR-023**: All subscribers MUST receive events in the same order, which is the order of the
  history. Events published by one task are delivered in the order they were published.
- **FR-024**: Subscribing MUST attach the request to one or more topics chosen by the developer's
  handler. The subscription receives every event published after it started on any of its topics,
  exactly once even when the event is on several of them, and never an event on other topics.
- **FR-025**: The subscription MUST be registered before the response headers are sent. Once the
  client has received the headers, every event published from then on is delivered to it.
- **FR-026**: Subscribing MUST block until the stream ends, and MUST report how it ended:
  - the client disconnected or the request's context ended: no error;
  - the broker was closed: a closed error;
  - the subscriber's queue overflowed: a slow-consumer error. This takes precedence over a write
    that fails or times out after the overflow, because the overflow is its cause;
  - a write failed or timed out: an error that wraps the cause.

  Errors detected before anything is written, namely a response that cannot be flushed and an
  empty topic list, MUST be returned with nothing written, so that the developer's handler
  answers them.
- **FR-027**: Once a stream has started, the broker MUST NOT answer with a status other than 200.
  It ends streams by completing the response normally, so that clients reconnect.
- **FR-028**: Each subscriber MUST have a queue of a configured size, 64 events by default. When it
  overflows, the stream MUST end with the slow-consumer outcome. Publishers and other subscribers
  are unaffected.
- **FR-029**: During the live phase of a subscription, when nothing has been written for the
  keep-alive interval, 15 seconds by default, the broker MUST write a comment. The interval does
  not apply while the replay function runs (FR-038). The documentation asks replay functions to
  return within the idle timeout of proxies.
- **FR-030**: The reconnection time (FR-016) and the write timeout (FR-015) MUST be configurable
  for the broker's streams, with the same defaults and meaning as for single streams.
- **FR-031**: The broker MUST NOT decide authorization. It delivers to a subscription only events
  on the topics it was given, when resuming as well. The documentation shows topics derived from
  the authenticated identity, never taken unchecked from client input.

#### Broker: resuming

- **FR-032**: The broker MUST keep the most recent events in an in-memory history of a configured
  size, 1,000 events by default, with the topics of each event. The history never holds more.
- **FR-033**: When a subscription starts without a last event identifier, and the history is not
  empty, the broker MUST first send the identifier of the most recent event in the history as a
  position, without dispatching an event. A client that disconnects before receiving its first
  event can then resume.
- **FR-034**: When a subscription starts with a last event identifier found in the history, the
  broker MUST first deliver, in order, every later event in the history on the subscription's
  topics, then the live events, with no loss and no duplicates.
- **FR-035**: A last event identifier is unresolvable when it is not in the history and either no
  replay function is configured or the replay function reports it as unknown (FR-041). For an
  unresolvable identifier, the broker MUST send the configured reset event before any live event.
  When no reset event is configured, it MUST send the position of FR-033, so that the client stops
  sending the unresolvable identifier, and continue with the live events only. The reset event is
  disabled by default.
- **FR-036**: The broker MUST set the identifier of the reset event to the identifier of the most
  recent event in the history, or to an empty identifier when the history is empty, overriding
  any identifier in the configured event. A client that reconnects right after a reset event then
  receives no second one while the history still holds that position.
- **FR-037**: Resuming MUST work across broker instances that received the same events, with the
  same identifiers and in the same order: an identifier found in an instance's history resumes
  from its position there.

#### Broker: replay from a durable source

- **FR-038**: The developer MUST be able to configure a replay function. The broker calls it only
  when a last event identifier is not in the history, with the request's context, that identifier
  and the subscription's topics. The function provides, in order, the events that follow that
  identifier in the developer's source, each together with its topics. It MAY use the
  subscription's topics to narrow what it reads.
- **FR-039**: The broker MUST filter the events provided by the replay function by topic: an event
  reaches the subscriber only when at least one of its topics is among the subscription's topics,
  and an event on several of them is delivered once. The broker never relies on the replay
  function to filter.
- **FR-040**: The broker MUST register the subscription before calling the replay function. Live
  events published during the replay are held in the subscriber's queue. When the replay ends,
  held events already delivered by the replay are discarded and the rest are delivered. The
  subscriber receives the source's order, then the live order, with no loss and no duplicates.
- **FR-041**: The replay function MUST be able to report that it does not know the identifier,
  with a distinguishable error. The broker then treats the identifier as unresolvable (FR-035).
  A replay that provides no event and no error means that the identifier is known and no later
  event exists.
- **FR-042**: When the replay function fails with any other error, the broker MUST end the stream
  by completing the response normally, without delivering live events and without sending the
  reset event, and the subscription MUST report an error that wraps the failure. Events delivered
  before the failure stay delivered, so the client reconnects with the identifier of the last of
  them, or with the same identifier as before when none was delivered, and the replay is
  attempted again.
- **FR-043**: An event provided by the replay function that is invalid per FR-009, has no
  identifier or has no topic MUST be treated as a replay failure (FR-042). Calling `send` after
  the replay function has returned MUST return an error and write nothing.
- **FR-044**: When the subscriber's queue overflows during a replay, the stream MUST end with the
  slow-consumer outcome. Each replay delivers events, so successive reconnections make progress.
- **FR-045**: When the client disconnects during a replay, the context passed to the replay
  function MUST be cancelled and the subscription MUST end.

#### Broker: closing

- **FR-046**: Closing the broker MUST end every active stream by completing its response normally,
  and each subscription reports the closed outcome. Closing returns without waiting for streams to
  end. Closing again has no effect.
- **FR-047**: After the broker is closed, subscribing MUST answer an empty 200 stream that carries
  the configured reconnection time, and report the closed outcome. Publishing MUST report the
  closed outcome.
- **FR-048**: Closing MUST be usable as the HTTP server's shutdown hook, so that a graceful
  shutdown completes without waiting for its deadline.

#### Client: decoding

- **FR-049**: The decoder MUST interpret event streams as the standard defines: UTF-8 decoding
  with replacement of invalid sequences, one leading byte order mark skipped, lines ended by CRLF,
  line feed or carriage return including a carriage return split across reads, comments, fields
  with and without a colon, one leading space removed from values, unknown fields ignored, `id`
  values containing NUL ignored, `retry` values accepted only when made of ASCII digits, events
  dispatched on a blank line, events with empty data not dispatched, the last line feed of the
  data removed, and an unterminated event at the end discarded.
- **FR-050**: Each decoded event MUST carry the stream's last event identifier at the time it is
  dispatched, which persists across events as the standard defines, its type, empty when the
  stream set none, and its data.
- **FR-051**: The size of a single event MUST be bounded by a documented maximum that the
  developer can change. An event that exceeds it ends decoding with a distinguishable
  event-too-large error, and memory does not grow beyond the maximum.
- **FR-052**: Decoding MUST stop as soon as the developer stops iterating. A read error MUST be
  reported and end the iteration.

#### Client: connecting

- **FR-053**: The client MUST connect with the developer's request, including its method, headers
  and body, and add `Accept: text/event-stream` when the request has no `Accept` header.
- **FR-054**: An answer of status 200 with content type `text/event-stream` MUST start delivering
  events. An answer of 204 MUST end the iteration without an error. Any other status MUST end it
  with a distinguishable error that carries the status. An answer of 200 with another content type
  MUST end it with an error. None of these is retried.
- **FR-055**: When a connection fails or the server ends the stream, the client MUST reconnect
  after the reconnection time, set by the server or a documented default. Each reconnection sends
  the last event identifier received, including identifiers received as positions (FR-033), when
  it is not empty. A last event identifier set by the developer on the initial request MUST be
  honored on the first connection.
- **FR-056**: After each consecutive connection attempt that fails before a response of status
  200, the client MUST double the delay, up to a documented maximum. After a successful
  connection, the delay MUST return to the reconnection time. Delays MUST be verifiable in tests
  without real waiting.
- **FR-057**: A request with a body that cannot be sent again MUST end the iteration with an error
  before the first connection.
- **FR-058**: When the request's context ends, the iteration MUST end promptly with the context's
  error. When the developer stops iterating, the connection MUST be closed.
- **FR-059**: The developer MUST be able to provide the HTTP client used to connect.

#### Documentation

- **FR-060**: The README and the package documentation MUST document thoroughly, each in its own
  section with an explanation of when to use it and a code example:
  - the single stream flow, including how a handler resumes on its own;
  - the broker flow, with topics derived from the authenticated identity in the handler;
  - publishing from the developer's code;
  - resuming after a reconnection from the in-memory history;
  - the unresolvable identifier scenario and the reset event;
  - the replay from a durable bus;
  - shutting down;
  - consuming streams from Go;
  - testing handlers that stream.
- **FR-061**: The documentation MUST explain how to integrate the module into an existing server:
  - the server's write timeout can stay, because streams manage their own deadline;
  - closing the broker must be registered as the server's shutdown hook, and why;
  - the standard library's timeout handler cannot wrap a stream;
  - an HTTP client with an overall timeout ends streams;
  - compatibility with this repository's middleware: the compressing middleware compresses event
    streams and delivers every flush; the entity tag middleware passes flushed responses through;
    the concurrency limit must not wrap stream routes, because a stream holds its slot while
    open; the rate limit bounds reconnection storms; the access log records a stream when it ends;
  - compatibility with this repository's authentication: session cookies work with the browser's
    event source, browsers cannot send an `Authorization` header so tokens and API keys suit only
    non-browser clients, and 401 or 403 answers stop browser reconnection.
- **FR-062**: The documentation MUST include notes for frontend developers: listening to named
  events, handling the reset event by reloading state, credentials across origins, the stop of
  reconnection on non-200 answers, and the per-domain connection limit of browsers on HTTP/1.1.
- **FR-063**: The documentation MUST explain deployments with several replicas: the broker is per
  process, the developer's code publishes to a bus and a loop in each replica feeds its broker,
  the bus message identifier becomes the event identifier, and duplicates from at-least-once
  delivery are refused (FR-022). It MUST give guidance for Redis Streams, Redis Pub/Sub, Kafka,
  RabbitMQ classic queues and RabbitMQ Streams: which of them support replay, how each replica
  receives every message, the pitfall of a consumer group shared by the replicas, and the single
  Kafka partition needed for a common order. The guidance MUST include code snippets as prose,
  never as compiled examples (FR-002).
- **FR-064**: The documentation MUST show the pattern for testing handlers that stream: publishing
  as soon as the response headers are received (FR-025), decoding the response, and no sleeps.
- **FR-065**: Every primary capability MUST have a runnable example: opening a stream and sending
  events, publishing and subscribing, closing on shutdown, the reset event, the replay function,
  decoding and the reconnecting client.

### Key Entities

- **Event**: what the server sends, made of an optional identifier, an optional type and data.
- **Stream**: one response that carries events produced by the developer's handler.
- **Broker**: the component that delivers each published event to the subscriptions on its topics,
  keeps the history and can be closed.
- **Topic**: a string that names a group of events, chosen by the developer, such as `user:alice`.
- **Subscription**: one client's stream attached to one or more topics through the broker.
- **Subscriber queue**: the bounded buffer of events waiting to be written to one subscription.
- **History**: the most recent events kept in memory, with their topics, to resume subscriptions.
- **Last event identifier**: the identifier of the last event a client received, sent back when
  it reconnects.
- **Position**: an identifier sent without an event, so that a client has a point to resume from
  before its first event.
- **Reset event**: an event chosen by the developer, sent when a last event identifier cannot be
  resolved, so that the frontend reloads its state.
- **Replay function**: a developer-supplied function that provides missed events, each with its
  topics, from a durable source.
- **Keep-alive**: a comment line that keeps idle connections open through proxies.
- **Reconnection time**: the delay before a client reconnects, which the server can set.
- **Decoder**: the parser that turns an event stream into events.
- **Client**: the component that connects to a stream, decodes it and reconnects.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: For 100% of the valid events generated by the fuzz tests, decoding the encoded event
  returns its type, its data with line breaks normalized and its identifier. The decoder never
  crashes on arbitrary input.
- **SC-002**: The decoder produces exactly the events the standard describes for 100% of the
  event stream examples in its "Server-sent events" section.
- **SC-003**: A client disconnected at any point of a sequence of 10,000 events published on mixed
  topics receives, after reconnecting, 100% of the missed events on its topics, with 0 duplicates
  and 0 events on other topics. This holds when resuming from the history, from a replay source,
  and across two broker instances fed with the same events.
- **SC-004**: Across 1,000 randomized interleavings of a replay with concurrent publishing, 0
  events are lost and 0 are delivered twice.
- **SC-005**: With 999 stalled subscribers and one active subscriber, publishing 10,000 events
  completes, no publish call waits for a subscriber, the active subscriber receives all 10,000
  events in order, and every stalled subscription ends with the slow-consumer outcome.
- **SC-006**: The history never holds more events than its configured size, a subscriber queue
  never holds more events than its configured size, and the decoder never holds more than the
  maximum event size for a single event.
- **SC-007**: A graceful shutdown of a server with 1,000 open streams, whose shutdown hook closes
  the broker, completes without reaching its deadline, and 100% of the clients observe a complete
  200 response.
- **SC-008**: Once every stream has ended and the broker is closed, and once every client
  iteration has ended or been cancelled, the module leaves 0 tasks running.
- **SC-009**: Adopting the module adds zero dependencies beyond the standard library to a consumer
  project.
- **SC-010**: 100% of the flows listed in FR-060 to FR-064 have a documented section with a code
  example, and 100% of the capabilities listed in FR-065 have a runnable example that passes.
- **SC-011**: 100% of the acceptance scenarios and edge cases in this spec are covered by
  automated tests that wait for no real time, and statement coverage of the module is at least
  80%.

## Assumptions

- **Module and versioning**: one module, `github.com/paccolamano/go/sse`, released with
  `sse/vX.Y.Z` tags. The first release is 0.1.0, and releases stay at 0.x until the API is
  considered stable.
- **Go version**: the minimum Go version is the oldest release the constitution supports (1.26 at
  the time of writing).
- **Reference**: the WHATWG HTML Living Standard, section "Server-sent events", as published in
  October 2026.
- **Middleware and authentication statements**: the compatibility notes of FR-061 are checked
  against the documented behavior of `middleware` 0.1.0 and `auth` 0.1.0. The `sse` module does
  not depend on them, so the notes are documentation, not tests of this module.
- **Duplicate identifiers**: refused with an error rather than ignored. An application that uses
  an entity identifier as event identifier, and publishes two updates of the same entity, sees the
  mistake at once instead of losing the second update silently. A feed from an at-least-once bus
  ignores the error.
- **Topics**: publishing requires at least one topic. Events for everyone go to a topic that every
  handler subscribes to, such as `broadcast`.
- **Positions and reset identifiers**: the identifier sent as a position (FR-033) or carried by a
  reset event (FR-036) may belong to an event on a topic the client does not follow. Only the
  identifier is disclosed, never the event. This is acceptable because identifiers are opaque.
- **Client status handling**: answers other than 200 and 204 are not retried, as the standard
  requires. The error carries the status, so a developer who wants to retry, for example on 503,
  starts a new iteration.
- **Defaults chosen in the plan**: the maximum event size of the decoder, the client's default
  reconnection time and maximum delay, the format of generated identifiers and the text of
  keep-alive comments are fixed and documented in `/speckit-plan`.
- **Out of scope**:
  - a store interface for the history;
  - adapters for message buses;
  - WebSockets;
  - authentication and CORS, provided by the `auth` and `middleware` modules;
  - compression, provided by the `middleware` module;
  - filters on subscriptions other than topics;
  - a JavaScript client: browsers use their native event source.
- **Tests**: tests are hermetic and deterministic (Principle III). Keep-alive intervals,
  reconnection times and write timeouts are verified without real waiting. The decoder has fuzz
  tests (Principle III, code that parses external input).

## Appendix: original request

The request, as submitted to `/speckit-specify`:

```text
New module `sse` (github.com/paccolamano/go/sse): Server-Sent Events per the WHATWG HTML Living Standard (section "Server-sent events"), standard library only, no dependency on any message bus client.

Decisions already agreed with the user (do not reopen them):

SERVER — EVENTS
- `Event{ID, Type, Data string}`. ID must not contain NUL, CR or LF; Type must not contain CR or LF; Data must be valid UTF-8. Invalid events are refused with `ErrInvalidEvent` instead of corrupting the stream.
- Line breaks in Data (CR, LF, CRLF) become one `data:` line each; an event with empty Data is still dispatched by clients.

SERVER — STREAM (one response, events produced by the handler)
- `NewStream(w, r, opts...)` sets `Content-Type: text/event-stream`, `Cache-Control: no-store`, `X-Accel-Buffering: no`, flushes immediately; returns `ErrStreamingUnsupported` with nothing written when the writer cannot flush (e.g. `http.TimeoutHandler`).
- `Send(ev)`, `KeepAlive()` (comment line).
- The server's `WriteTimeout` is replaced, for the stream, by a per-write deadline via `http.ResponseController` (`WithWriteTimeout`, default 10s), so a client that stops reading is dropped.
- `WithRetry(d)` sends the `retry:` field when the stream opens (default: not sent).
- Context comes from the `*http.Request`, as in net/http (`http.ServeContent`, `http.Client.Do`); this is the agreed interpretation of constitution principle IV for Stream, Broker.Serve and Client.Events.

SERVER — BROKER (fan-out to many clients, replay)
- `NewBroker(opts...)`, `Publish(ev, topics...)` never blocks; an event published on several topics reaches a subscriber of more than one of them only once. Empty IDs are generated with a random per-instance prefix, so an ID from a previous process is never mistaken for a valid one after a restart.
- `Serve(w, r, topics...)` blocks until the client disconnects, the broker is closed, the subscriber is too slow, or a write fails. Topics are chosen by the user's handler (after authentication); the broker does not decide authorization. The subscription is registered before the response headers are sent, so tests can publish as soon as `Do` returns, without sleeps.
- In-memory history for replay via `Last-Event-ID` (`WithHistory`, default 1000 events). Per-subscriber queue (`WithBuffer`, default 64); on overflow the stream is closed and `Serve` returns `ErrSlowConsumer`; the browser reconnects with Last-Event-ID and recovers from history. Keep-alive comments (`WithKeepAlive`, default 15s).
- `Close()` ends all streams with a normally terminated 200 (EventSource reconnects; any non-200 would make it stop forever). Intended use: `srv.RegisterOnShutdown(broker.Close)`, otherwise `http.Server.Shutdown` waits for streams until its timeout. `Serve` after `Close` answers an empty 200 (with retry) and returns `ErrClosed`, never 503. Serve never answers non-200, except when flushing is unsupported.
- No background goroutines: each stream runs in its request goroutine.
- `WithResetEvent(ev)`: OPT-IN, disabled by default. Sent when a Last-Event-ID cannot be resolved (not in memory and not recoverable via replay), so the frontend reloads its state instead of silently missing events.
- `WithReplay(fn func(ctx context.Context, lastID string, topics []string) iter.Seq2[Event, error])`: INCLUDED IN v0.1. Called only when the Last-Event-ID is not in the in-memory history (memory first, so reconnect storms after a deploy do not all hit the bus). The broker subscribes before replaying, holds live events, discards those already sent, and switches from replayed to live events with no loss and no duplicates.
- Multi-replica deployments: the broker is per process; the user's code publishes to a bus and a goroutine in each replica feeds `broker.Publish`, using the bus message ID as the event ID so every replica has the same IDs in the same order. The library never imports bus clients.

CLIENT
- `Decode(r io.Reader) iter.Seq2[Event, error]`: spec-conformant parser (CR/LF/CRLF, leading BOM, comments, dispatch on blank line, `id` containing NUL ignored, last-event-ID semantics, `retry`). Must have fuzz tests.
- `NewClient(opts...)` with `WithHTTPClient`; `Events(req *http.Request) iter.Seq2[Event, error]`: reconnects with Last-Event-ID honoring the server's `retry`; transient errors are retried internally; 204 stops; non-200 status or wrong content type are terminal errors.

DOCUMENTATION (explicit user requirement: usage and every flow must be documented thoroughly, in README and package docs, with runnable Examples for the primary APIs)
- Single Stream flow; Broker flow with topics and authorization in the handler.
- Integration in an existing server: compatibility with the repository's middleware (compress compresses text/event-stream and delivers every flush; etag passes flushed responses through; maxconcurrency must not wrap SSE routes; ratelimit limits reconnection storms; accesslog logs at stream end; auth: cookie sessions work with EventSource, browsers cannot send Authorization so JWT/API keys only for non-browser clients, 401/403 stop browser reconnection), server WriteTimeout, graceful shutdown with RegisterOnShutdown.
- Reconnection and replay from memory; the gap scenario and the reset event; WithReplay with a durable bus.
- Multi-replica feeding from a bus, with guidance for Redis Streams, Redis Pub/Sub, Kafka, RabbitMQ classic queues and RabbitMQ Streams (which support replay, how each replica must receive every message, consumer-group pitfall, single Kafka partition for ordering). Bus snippets live in the README as prose/code blocks, not as compiled Examples, so the module gains no dependency.
- Client consumption from Go; testing pattern for handlers.
```

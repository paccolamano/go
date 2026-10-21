# Research: Server-Sent Events (sse)

**Feature**: [spec.md](spec.md) | **Plan**: [plan.md](plan.md) | **Date**: 2026-10-21

Each section records a decision, its rationale and the alternatives rejected. Standard library
APIs were checked against the local Go 1.27 toolchain, and all of them are available in Go 1.26,
the module's minimum version.

The standard is the WHATWG HTML Living Standard, section "Server-sent events", read on
2026-10-21. The rules quoted below come from it.

The HTTP behaviors the design depends on were validated by a throwaway spike, kept outside this
repository (two modules in the session scratchpad). It was tested with `-race` on Go 1.27.0,
linux/amd64, over HTTP/1.1 and unencrypted HTTP/2:

- `ssespike`: a stream prototype served by `http.Server` over an in-memory `net.Pipe` network,
  inside `testing/synctest`;
- `ssespike-compat`: the same prototype behind this repository's `accesslog`, `compress` and
  `etag` middleware, imported from the local `middleware` module.

The broker algorithm was not spiked. Its correctness rests on the tests of R-16, in particular
the randomized interleavings of SC-004.

## R-01 Module layout, versioning and Go version

- **Decision**:
  - One module, `github.com/paccolamano/go/sse`, in the top-level directory `sse/`, with a single
    package `sse` and no internal packages (FR-001).
  - Files are split by capability: options, event encoding, stream, broker and history, serving
    and resuming, replay, identifiers, decoder, UTF-8 replacement and client (plan, Project
    Structure).
  - The `go` directive is `go 1.26.0`. CI tests on 1.26 and 1.27. `go.work` is regenerated with
    `make work`.
  - Features used: `iter` (1.23), `crypto/rand.Text` (1.24), `testing.B.Loop` (1.24),
    `net/http.Protocols` for the HTTP/2 tests (1.24), `testing/synctest` and
    `sync.WaitGroup.Go` (1.25), `http.ResponseController` (1.20).
  - Releases start at `sse/v0.1.0` and stay at 0.x until the API is declared stable.
- **Rationale**: the client and the server share the `Event` type and the wire format. One
  package keeps the API in one place, and no code has two consumers that would justify an
  internal package.
- **Alternatives rejected**:
  - **A separate `sse/client` package**: it would import `Event` from `sse` and split a small
    API in two for no gain.
  - **An `internal/wire` package for encoding and decoding**: a single consumer, so a
    speculative layer (Principle V).

## R-02 Dependencies (Principle V)

- **Decision**: the module requires nothing outside the standard library, tests and examples
  included. It does not depend on the other modules of this repository either. The middleware
  compatibility checks of R-19 were done in the spike, not in the module's tests.
- **Rationale**: FR-002. The protocol, the HTTP behavior and the concurrency need only `net/http`,
  `bufio`, `iter`, `sync`, `time`, `unicode/utf8`, `crypto/rand` and `mime`. Message bus clients
  appear only as prose in the README (R-17).
- **Alternatives rejected**: **testing against `middleware` in the module's tests**. It would add
  a dependency on a published `middleware` version for documentation claims that do not change
  this module's behavior.

## R-03 API shape and options

- **Decision**:
  - Constructors: `NewStream(w, r, opts...)`, `NewBroker(opts...)`, `NewClient(opts...)`, and the
    function `Decode(r, opts...)`. The complete surface is in
    [contracts/public-api.md](contracts/public-api.md).
  - **One `Option` type** for every constructor, with applicability checks, as in `slogx`. An
    option that a constructor does not accept makes it return an error wrapping
    `ErrInvalidConfig`.

    | Constructor | Accepted options |
    |-------------|------------------|
    | `NewStream` | `WithRetry`, `WithWriteTimeout` |
    | `NewBroker` | `WithRetry`, `WithWriteTimeout`, `WithKeepAlive`, `WithHistory`, `WithBuffer`, `WithResetEvent`, `WithReplay` |
    | `NewClient` | `WithHTTPClient`, `WithMaxEventSize` |
    | `Decode` | `WithMaxEventSize` |

  - `Decode` takes variadic options, which the agreed sketch did not have, so that FR-051 ("a
    maximum that the developer can change") holds for it too. An invalid option makes the
    iteration yield one error that wraps `ErrInvalidConfig`.
  - Error messages have the form
    `sse: invalid configuration: <Constructor>: <Option>: <reason>`, as in `middleware` and
    `slogx`.
- **Rationale**:
  - `WithRetry` and `WithWriteTimeout` apply to both streams and brokers. Separate option types
    would need an interface implemented by both, which is more code than a runtime check.
  - The repository already uses one option type with applicability checks (`slogx`), so readers
    meet one convention.
- **Alternatives rejected**:
  - **One option type per constructor** (`StreamOption`, `BrokerOption`, `ClientOption`): shared
    options would need duplicates or an interface.
  - **A `Decoder` type with a constructor**: one more type only to carry the maximum size.

## R-04 Errors

- **Decision**: the sentinel errors and the error type below, all usable with `errors.Is` and
  `errors.As`:

  | Error | Returned by | When |
  |-------|-------------|------|
  | `ErrInvalidConfig` | constructors, `Decode` | invalid or inapplicable option |
  | `ErrInvalidEvent` | `Send`, `Publish`, replay `send` | FR-009 violated; for replay also a missing identifier or topic (FR-043) |
  | `ErrStreamingUnsupported` | `NewStream`, `Serve` | the writer cannot flush; nothing written |
  | `ErrSlowConsumer` | `Serve` | queue overflow (FR-028, FR-044) |
  | `ErrClosed` | `Serve`, `Publish` | the broker is closed (FR-046, FR-047) |
  | `ErrDuplicateID` | `Publish` | the identifier is in the history (FR-022) |
  | `ErrUnknownID` | returned by the developer's replay function | the source does not know the identifier (FR-041) |
  | `ErrEventTooLarge` | `Decode`, `Events` | an event exceeds the maximum size (FR-051) |
  | `*ResponseError` | `Events` | status other than 200 and 204, or 200 with another content type (FR-054) |

  - `ResponseError` has the fields `StatusCode` and `ContentType`.
  - Write failures are wrapped with context, for example `sse: write event: <cause>`.
  - A client that disconnects is not an error: `Serve` returns nil.
  - Programming errors without a branch to take, an empty topic list or a request body that
    cannot be sent again, are plain descriptive errors without a sentinel.
- **Rationale**: Principle IV exposes as sentinels only the conditions callers branch on:
  logging slow consumers, ignoring duplicates in a bus feed, signaling an unknown identifier,
  retrying after an error status.
- **Alternatives rejected**:
  - **A sentinel for every failure**: it enlarges the API with errors nobody branches on.
  - **Separate errors for a bad status and a bad content type**: one type carries both, and
    `StatusCode` tells them apart.

## R-05 Event validation and encoding

- **Decision**:
  - **Validation (FR-009)**:
    - `utf8.ValidString` on the identifier, the type and the data;
    - the identifier must not contain `\x00`, `\r` or `\n`;
    - the type must not contain `\r` or `\n`.
  - **Encoding (FR-010)**: one line per field, `name ":" [" " value] "\n"`. The space is written
    only when the value is not empty, so a value that starts with spaces keeps them: clients
    remove exactly one space. Fields are written in the order `event`, `id`, then one `data` line
    for each line of the data. The data is split at CRLF, CR and LF. The block ends with `\n`.
  - **Empty data** gives one `data:` line. The client's buffer becomes `"\n"`, which is not
    empty, so the event is dispatched with empty data. This matches the standard's own example,
    where a block made of a single `data` line "fires" an event with empty data.
  - Other records:
    - reconnection time: `retry: <ms>`;
    - keep-alive: `: keep-alive`;
    - position: `id: <id>` followed by an empty line;
    - empty position: `id:` followed by an empty line.
  - Each event is encoded into a buffer that the stream reuses, then written in one `Write`,
    followed by one flush (FR-014).
- **Rationale**: the round trip of FR-011 holds by construction. Decoding gives back the type,
  the data with LF line breaks, and the identifier. The fuzz test of R-16 checks it.
- **Alternatives rejected**:
  - **Always writing the space**: correct but writes `data: ` with a trailing space for empty
    lines. Writing the space only before a value has the same meaning and no trailing space.
  - **Escaping line breaks**: the standard has no escaping. Splitting into data lines is its
    mechanism.

## R-06 Response setup and flush detection

- **Decision**:
  - The stream detects whether the writer can flush before writing anything. It walks the
    `Unwrap() http.ResponseWriter` chain, as `http.ResponseController` does, and looks for
    `FlushError() error` or `http.Flusher`.
  - When neither is found, it returns `ErrStreamingUnsupported` with nothing written (FR-013,
    FR-026).
  - Otherwise it sets `Content-Type: text/event-stream`, `Cache-Control: no-store` and
    `X-Accel-Buffering: no`, keeps every other header already set (FR-012), writes status 200,
    and flushes.
  - `Serve` runs the same check before registering the subscription.
- **Spike**: `http.TimeoutHandler`'s writer is reported as not flushable.
  `httptest.ResponseRecorder` is flushable.
- **Rationale**: `ResponseController.Flush` would write the headers to find out, so it cannot
  report support without writing.
- **Alternatives rejected**: **calling `Flush` and inspecting the error**. On a writer that can
  flush, this sends the headers before the subscription is registered (FR-025).

## R-07 Write deadlines

- **Decision**:
  - Before every write, `ResponseController.SetWriteDeadline(now + write timeout)`. After the
    write and the flush succeed, `SetWriteDeadline(time.Time{})` clears it. The same applies to
    the first flush of the headers.
  - `http.ErrNotSupported` from `SetWriteDeadline` is tolerated: the stream works, bounded by the
    server's write timeout (FR-015). This is the case of `httptest.ResponseRecorder`.
  - A failed write makes the stream broken. Later sends return the same error without writing.
- **Spike**, with the server's `WriteTimeout` at 10 s and identical results over HTTP/1.1 and
  unencrypted HTTP/2:
  - A stream idle for 60 s, then sending, delivers the event.
  - The control without deadline management fails: `i/o timeout` over HTTP/1.1, and a reset
    stream with `INTERNAL_ERROR` over HTTP/2.
  - A client that stops reading makes a write fail after exactly 10 s, the per-write timeout.
- **Rationale**:
  - The server's write deadline covers the whole response, so it must be replaced.
  - Clearing it between writes keeps idle periods of any length from expiring it. Per
    `ResponseController`'s documentation, an expired deadline cannot be extended.
- **Alternatives rejected**:
  - **One deadline far in the future, set once**: it does not catch a client that stops reading.
  - **Asking users to set `WriteTimeout: 0`**: it removes the protection for every other route.

## R-08 Context (Principle IV)

- **Decision**:
  - The stream keeps the request's context, captured by `NewStream`, and `Send` and `KeepAlive`
    check it before writing (FR-018).
  - `Serve` uses `r.Context()`, and passes it to the replay function.
  - The client uses `req.Context()` for connections and for the waits between them.
  - `Decode` reads an `io.Reader` without a context, as `encoding/json.Decoder` does.
    Cancellation belongs to the reader: an HTTP response body ends when its request's context
    ends.
- **Rationale**: this is the interpretation agreed in the spec's Clarifications. The request
  already carries the context, as in `http.ServeContent` and `http.Client.Do`, and a separate
  parameter could disagree with it. It is a deviation from the letter of Principle IV, recorded in
  the plan's Complexity Tracking.
- **Alternatives rejected**: **`Send(ctx, ev)` and `Serve(ctx, w, r, topics...)`**: redundant with
  the request, and a context unrelated to the request would leave a write running after the client
  has left.

## R-09 Broker state and publishing

- **Decision**:
  - One mutex guards the broker's state:
    - the history: a ring of `WithHistory` entries `{seq, id, topics, event}`, plus a map from
      identifier to sequence number, kept in sync on eviction;
    - a topic index: a map from topic to the set of subscribers;
    - the closed flag and a channel closed by `Close`.
  - `Publish(ev, topics...)` validates the event and the topics. Under the lock it:
    - checks for duplicates;
    - assigns the next sequence number, and the generated identifier when the event has none
      (R-10);
    - appends the entry to the ring;
    - for each topic, offers the entry to each subscriber with a non-blocking send on its queue
      channel. A subscriber whose last offered sequence equals the current one is skipped, which
      gives exactly-once delivery across topics (FR-024).
  - A full queue marks its subscriber as overflowed: it is removed from the index and its
    overflow channel is closed (FR-028).
  - Publishing never blocks on a subscriber (FR-020).
  - Topics named twice in one call are deduplicated by the same sequence check.
- **Rationale**:
  - The history order is the sequence order, and every subscriber receives entries in that order
    (FR-023).
  - The cost of a publish is proportional to the subscribers of its topics.
- **Alternatives rejected**:
  - **Sharded locks**: no measured need (Principle V).
  - **A goroutine per subscriber or a dispatcher goroutine**: forbidden by FR-003, and it adds
    shutdown paths.
  - **Unbounded queues**: memory would grow with a stalled client.

## R-10 Generated identifiers

- **Decision**:
  - Format `<prefix>-<seq>`. The prefix is the first 12 characters of `crypto/rand.Text()`,
    chosen once per broker: base32, 60 bits. The sequence is decimal, starting at 1. Example:
    `K7Q2M9XW4TBA-42`.
  - Explicit identifiers are used verbatim (FR-022). An explicit identifier equal to a generated
    one is a duplicate while the earlier event is in the history.
- **Rationale**:
  - The prefix makes the identifiers of a previous process, or of another instance, unknown to
    this one (FR-021, User Story 3 scenario 6, User Story 7 scenario 3).
  - 60 bits keep collisions negligible: about 4·10⁻⁷ across a million instances.
- **Alternatives rejected**:
  - **The full 26 characters**: longer headers for no practical gain.
  - **UUIDs**: no ordering, and an extra format.
  - **The sequence alone**: identifiers would collide across restarts.

## R-11 Subscription lifecycle and resuming

- **Decision**:
  - `Serve(w, r, topics...)`:
    1. Validate the topics, run the flush check (R-06).
    2. Under the lock:
       - if the broker is closed, answer an empty stream (headers and `retry` if configured),
         flush, and return `ErrClosed` (FR-047);
       - otherwise register the subscriber;
       - then resolve `Last-Event-ID`; an empty header is treated as absent.
    3. Resolution happens under the same lock as the registration:
       - **absent**: the position is the most recent history identifier, if any (FR-033);
       - **found**: snapshot the later history entries on the subscription's topics (FR-034);
       - **not found, with a replay function**: replay after the headers (R-12);
       - **not found, without one**: unresolvable. With a reset event, the reset carries the most
         recent identifier, or an empty identifier when the history is empty (FR-036). Without
         one, the position is sent instead (FR-035).
    4. Write the headers, then the first block, in one write: `retry` if configured, then the
       position, the reset event or nothing.
    5. Write the snapshot, then enter the live loop, which selects on:
       - the request context ending: return nil;
       - the broker closing: return `ErrClosed`;
       - overflow: return `ErrSlowConsumer`;
       - the queue: write the entry;
       - the keep-alive timer: write a comment.

       The timer restarts after every write (FR-029).
    6. On return, unregister the subscriber.
  - Taking the snapshot and registering under one lock makes the history and the live queue
    contiguous: entries up to the registration come from the snapshot, later ones from the queue.
    There is no gap and no duplicate.
- **Rationale**:
  - The position (FR-033) and the identifier of the reset event (FR-036) both rely on the
    standard's dispatch rule: the last event identifier is set before the check for empty data,
    so a block with only an `id` field updates the client without dispatching an event.
  - FR-035 was refined from this research (checklist, Iteration 3). Without the position, a
    client with an unresolvable identifier keeps sending it, and the replay function would be
    called at every reconnection.
- **Alternatives rejected**:
  - **Replaying from the history after releasing the lock**: events published in between could
    be sent twice or lost.
  - **Sending no position on a new subscription**: a client that disconnects before its first
    event would resume with no identifier and silently miss events.

## R-12 Replay from a durable source

- **Decision**:
  - The replay function has the type
    `ReplayFunc func(ctx context.Context, lastID string, topics []string, send func(ev Event, topics ...string) error) error`.
  - The developer reads the source and calls `send` for each event with its topics: the same
    shape as `Publish`.
  - `send` returns an error when the replay must stop: the client left, the queue overflowed, a
    write failed, or the event is invalid. The function should return it.
  - The function returns:
    - nil when it is done;
    - an error matching `ErrUnknownID` when the source does not know `lastID`;
    - any other error on failure.
  - **Topic filter (FR-039)**: `send` drops an event when none of its topics belongs to the
    subscription, and delivers it once otherwise. The function receives the subscription's topics
    only to narrow its query.
  - **Handoff (FR-040)**: the subscriber is registered before the call (R-11), so live events
    accumulate in its queue during the replay.
    - `send` remembers the identifiers of the last `WithBuffer` replayed events in a bounded set.
    - When the replay ends, queued entries whose identifier is in that set are skipped, and the
      rest are written.
    - `send` also skips an event whose identifier equals `lastID`, in case the source includes it.
  - **Outcomes**:
    - nil: the live loop starts;
    - `ErrUnknownID`: unresolvable, so the reset event if configured, otherwise the position,
      then the live loop (FR-035, FR-041);
    - other errors, including an invalid replayed event (FR-043): the stream ends with a complete
      response, without the reset and without live events, and `Serve` returns an error wrapping
      the failure (FR-042, Q2: A);
    - overflow during the replay: `ErrSlowConsumer` (FR-044);
    - the client leaving: the context passed to the function is cancelled (FR-045);
    - a panic in the function: recovered with a deferred `recover` around the call, and treated
      as a failure with the error `sse: replay panicked: <value>` (FR-007).
  - `send` called after the function has returned, for example from a goroutine the developer
    left running, returns an error and writes nothing (FR-043). A flag set when the call returns,
    under the stream's mutex, enforces it.
  - No keep-alive is written while the function runs, because the serving goroutine is inside it
    (FR-029). The README asks replay functions to return within the idle timeout of proxies.
- **Rationale**:
  - The overlap between replayed and live events is at most the queue capacity. A larger overlap
    overflows the queue and ends the stream first. Remembering the last `WithBuffer` replayed
    identifiers is therefore enough when the source and the broker see events in the same order,
    which FR-037 already assumes.
  - If the orders differ, the worst outcome is a duplicate, never a loss. The documentation says
    so.
  - A callback is easier to write than an iterator that yields pairs. Compare:

    ```go
    send(toEvent(m), topicsOf(m)...)
    ```

    against a function that returns a function that yields `{Event, Topics}` values and an
    error.
- **Alternatives rejected**:
  - **`iter.Seq2[Event, error]` as in the sketch**: it cannot carry the topics needed by Q1: B.
    An extra exported pair type would be needed.
  - **A set of every replayed identifier**: unbounded memory on long replays.
  - **Switching to the history after the replay**: it depends on the feed having caught up, which
    the broker cannot know.
  - **Q1: A and Q2: B**: rejected by the user (spec Clarifications).

## R-13 Closing

- **Decision**:
  - `Close()` takes the lock, sets the closed flag once, and closes the broker's closed channel.
    Live loops select on that channel, return `ErrClosed`, and the handler's return completes the
    response.
  - `Close` takes no context and returns nothing, so that `srv.RegisterOnShutdown(b.Close)`
    compiles (FR-048). Calling it again does nothing (FR-046).
  - After `Close`, `Publish` returns `ErrClosed` and `Serve` answers an empty stream (R-11).
- **Spike**, with 50 open streams over HTTP/1.1 and HTTP/2:
  - with the shutdown hook, `Shutdown` returned nil after about 1 ms of virtual time, and 50 of
    50 clients read a clean end of body;
  - without it, `Shutdown` returned `context.DeadlineExceeded` after its full 30 s.
- **Rationale**: the server's `Shutdown` waits for active connections to become idle, and a
  stream never does on its own.
- **Alternatives rejected**: **`Shutdown(ctx) error` waiting for every stream to end**. The
  server's own `Shutdown` already waits, and the signature would not fit `RegisterOnShutdown`.

## R-14 Decoder

- **Decision**:
  - **Line reading**: bytes are read from a `bufio.Reader`, and a line ends at the first carriage
    return or line feed.
    - A carriage return ends a line at once, and a flag skips an immediately following line feed,
      even across reads.
    - The reader never peeks past a carriage return. Peeking would block until the next byte, and
      would delay the dispatch of an event whose blank line is a lone CR.
  - **Byte order mark**: skipped only as the first three bytes of the stream (`EF BB BF`).
  - **Fields**, as the standard defines:
    - a line starting with `:` is a comment;
    - otherwise the field name is the text up to the first colon, and the value is the rest,
      without one leading space; a line without a colon is a name with an empty value;
    - `event` sets the type buffer;
    - `data` appends the value and `\n`;
    - `id` sets the last event identifier buffer, unless the value contains NUL;
    - `retry` is accepted only when made of ASCII digits;
    - other names are ignored.
  - **Dispatch** on a blank line:
    1. set the last event identifier;
    2. if the data buffer is empty, reset the buffers and yield nothing;
    3. otherwise remove the final `\n`, yield `Event{ID: lastEventID, Type: typeBuffer, Data: data}`,
       and reset the buffers.

    At the end of the stream, a pending event is discarded.
  - **UTF-8**: invalid sequences are replaced with U+FFFD, one per *maximal subpart*, as the
    WHATWG Encoding Standard's UTF-8 decoder does. `strings.ToValidUTF8` does not fit: it
    coalesces runs. `utf8.DecodeRune` does not fit either: it replaces each byte separately. The
    replacement is about 30 lines of code, tested against the Unicode Standard's table of U+FFFD
    substitution (chapter 3, Table 3-8).
  - **Maximum size (FR-051)**: one limit, 1 MiB by default and changeable with
    `WithMaxEventSize`, bounds a single line and the sum of the data, type and identifier
    buffers of the pending event. Exceeding it ends decoding with `ErrEventTooLarge`.
  - The parser is an unexported type. `Decode` and the client both use it, and the client reads
    its last event identifier and reconnection time.
- **Rationale**: FR-049 and FR-050 follow the standard's processing model step by step. A browser
  and this decoder then produce the same events for the same bytes.
- **Alternatives rejected**:
  - **`bufio.Scanner`**: its split functions cannot handle a carriage return at a chunk boundary
    without peeking, and its maximum token size gives a less precise error.
  - **Per-byte replacement**: it differs from browsers on truncated sequences.

## R-15 Client

- **Decision**:
  - `Events(req)` returns an iterator. Each connection uses `req.Clone(req.Context())`.
  - The request body comes back through `GetBody`. A body without `GetBody` ends the iteration
    before connecting (FR-057).
  - The client adds `Accept: text/event-stream` when the request has no `Accept` header
    (FR-053).
  - It sends `Last-Event-ID` when the last identifier is not empty. A `Last-Event-ID` set on the
    initial request is honored on the first connection (FR-055).
  - **Answers (FR-054)**:
    - 200 with media type `text/event-stream`, checked with `mime.ParseMediaType`: the stream is
      decoded;
    - 204: the iteration ends without an error;
    - any other status, or another content type: `*ResponseError`, and no reconnection.
  - **Reconnection**:
    - after a network error or a normal end of the body, the client waits for the reconnection
      time, then reconnects;
    - the reconnection time is the last `retry` received, otherwise 3 s;
    - each consecutive attempt that fails before a 200 doubles the delay, up to the larger of
      30 s and the server's `retry` (FR-056, now a MUST);
    - a successful connection resets the delay;
    - the wait is a timer in a `select` with the request's context, so `synctest` makes it
      virtual in tests.
  - **Ending (FR-058)**: when the request's context ends, the iteration yields its error once
    and stops. When the consumer stops iterating, the body is closed before the iterator returns.
  - An error from `Decode`, such as `ErrEventTooLarge`, is terminal.
  - The default HTTP client is `http.DefaultClient`, and `WithHTTPClient` replaces it (FR-059).
- **Rationale**:
  - These are the standard's rules: reestablish after a network error or a normal end, fail on
    other statuses or content types.
  - 3 s matches the standard's "a few seconds" and common browser behavior.
  - Backoff is allowed by the standard ("user agents might introduce an exponential backoff").
- **Alternatives rejected**:
  - **Retrying 5xx answers**: the standard says to fail the connection, and the agreed decision
    keeps the client conformant. `ResponseError.StatusCode` lets the developer retry on purpose.
  - **Jitter**: it makes tests non-deterministic. Go clients rarely reconnect in the thousands
    (Principle V).

## R-16 Testing approach (Principle III)

- **Decision**:
  - The standard `testing` package only. External test package `sse_test`, plus internal tests
    for the history ring, the identifier format and the UTF-8 replacement.
  - **End-to-end HTTP without a network** (spike):
    - an in-memory listener whose `Dial` returns one end of a `net.Pipe`, served by `http.Server`
      and dialed by `http.Transport`, inside `testing/synctest`;
    - every HTTP test runs over HTTP/1.1 and over unencrypted HTTP/2;
    - the HTTP/2 transport buffers up to 4 MiB per stream by default
      (`transportDefaultStreamFlow = 4 << 20` in `net/http/internal/http2`). A client that stops
      reading would then never block the server in tests with small events, and many stalled
      streams would use gigabytes of memory. The h2c test transport sets
      `HTTP2: &http.HTTP2Config{MaxReceiveBufferPerStream: 64 << 10}`. The connection-level
      window keeps its default, because every stream of a test shares one connection.
  - **Virtual time**: keep-alive intervals, write timeouts, the server's `WriteTimeout`,
    reconnection delays, backoff and shutdown are all tested with `synctest`, so no test waits
    for real time (SC-011).
  - **Goroutine leaks (SC-008)**: `synctest.Test` fails when goroutines of the bubble outlive the
    test, so every end-to-end test also proves that no task remains.
  - **`net.Pipe` is synchronous** (spike): a handler cannot complete its response until the
    client reads. Test clients read in their own goroutine, as they would over TCP. A test that
    reads only after `Shutdown` deadlocks, and the spike hit this.
  - **Unit tests** of headers and encoding use `httptest.ResponseRecorder`. It flushes and does
    not support write deadlines, which the stream tolerates (R-07).
  - **Vectors**:
    - every event stream example of the standard (SC-002);
    - the Unicode Table 3-8 substitutions (R-14);
    - a carriage return at a chunk boundary, through a reader that returns one byte per `Read`.
  - **Randomized interleavings (SC-004)**: 1,000 seeded runs (a SplitMix64 generator in the tests, with fixed seeds: Semgrep's `p/golang` rules forbid `math/rand`, and `crypto/rand` cannot be seeded)
    of a replay racing with publishers, under `synctest`, asserting no loss and no duplicate.
  - **Disconnection at any point (SC-003)**: 10,000 events on randomly assigned topics, a client
    following two of three topics, and a seeded random disconnection point. The test reconnects
    from the history, from a replay source, and across two brokers fed with the same events.
  - **Fuzz targets are written before the code they check** (Principle III): `FuzzDecode` and
    `FuzzRoundTrip` with the decoder tests, `FuzzValidate` with the stream tests.
  - **Concurrency**: `sync.WaitGroup.Go` with 50 publishing goroutines under `-race`. The 1,000
    subscribers of SC-005 and SC-007 run inside one bubble.
  - **Fuzz targets**:
    - `FuzzDecode`: no panic, no event over the maximum size, the identifier never contains NUL;
    - `FuzzRoundTrip`: encoding a valid event, then decoding it, returns its type, its normalized
      data and its identifier (SC-001);
    - `FuzzValidate`: an event that is accepted produces exactly one dispatched event.
  - **Benchmarks** (informative; no code is declared performance-sensitive): `BenchmarkPublish`
    with 1,000 subscribers, `BenchmarkEncode` and `BenchmarkDecode`.
  - Examples: R-17.
  - Coverage target: ≥ 80% for the module.
- **Rationale**: the repository's hermeticity rules (no network, no real waiting), the parser
  rule of Principle III, and the spec's success criteria.
- **Alternatives rejected**:
  - **`httptest.Server`**: it opens a real socket, as the `auth` plan already rejected.
  - **An injected clock**: `synctest` virtualizes the standard library's own timers, including
    those of `net/http` and `net.Pipe` deadlines, without an API for it.

## R-17 Documentation

- **Decision**:
  - **README** sections, in order:
    1. purpose, `go get`, minimum Go version;
    2. the three building blocks and when to use each;
    3. **single stream**: a job progress handler; resuming on its own with
       `r.Header.Get("Last-Event-ID")`;
    4. **broker**: a notifications route with topics derived from `auth.FromContext`; publishing
       from domain code; topic naming;
    5. **integrating into an existing server**: a complete `main`-like listing with the existing
       middleware chain, the route kept off `maxconcurrency`, `WriteTimeout` kept, and
       `RegisterOnShutdown(broker.Close)`, followed by the compatibility table of FR-061 (R-19);
    6. **reconnection and resuming**: the flow from the history, the position, the gap scenario
       with a timeline, and the reset event with the frontend's handling;
    7. **replay from a durable bus**: the handoff, the topic filter done by the broker,
       `ErrUnknownID`, failures and the retry they cause, and the ordering assumption;
    8. **several replicas**: the feed loop, bus identifiers, ignoring `ErrDuplicateID`, and a
       table and short snippets for Redis Streams, Redis Pub/Sub, Kafka, RabbitMQ classic queues
       and RabbitMQ Streams. The snippets are marked as sketches with the client library and
       version they were written against: go-redis v9, franz-go, amqp091-go and rabbitmq-stream-go;
    9. **frontend notes** (FR-062);
    10. **consuming from Go** with `Client`;
    11. **testing handlers** (FR-064);
    12. defaults and limits: history, queue, keep-alive, write timeout, event size and memory.
  - **Package documentation** (`doc.go`): the same flows in short form, with links to the
    examples.
  - **Runnable examples** (FR-065):

    | Example | File kind |
    |---------|-----------|
    | `ExampleStream`, `ExampleBroker`, `ExampleWithReplay`, `ExampleWithResetEvent` | whole-file examples with a small recorder that signals each flush |
    | `ExampleBroker_Close` | whole-file example: `RegisterOnShutdown(b.Close)` on a server that never listens, a running `Serve`, then `Shutdown`; it waits for `Serve` to return `ErrClosed` |
    | `ExampleClient` | whole-file example with an in-memory `RoundTripper` that serves two canned connections and checks `Last-Event-ID` |
    | `ExampleDecode` | plain example over a string |

    Whole-file examples show their helpers in the documentation, so readers see everything that
    runs.
- **Rationale**: the user's explicit request (spec Clarifications, User Story 4). Bus snippets
  cannot be compiled without dependencies (FR-002), so naming the client version they target
  tells readers what to adapt.
- **Alternatives rejected**:
  - **Examples that need a network**: forbidden by hermeticity.
  - **Compile-only examples without output**: FR-065 asks for examples that run and pass.

## R-18 Lint and security scanners

- **Spike results** with the repository configuration:
  - The prototype's findings were all of kinds the implementation avoids:
    - `wrapcheck`: errors from `ResponseController` and `ResponseWriter` returned unwrapped;
    - `noctx`: test requests without a context;
    - `errcheck` and `thelper` in test code.
  - No finding about storing the request's context: `containedctx` is not enabled.
- **Decision**:
  - Every error from the standard library is wrapped with context.
  - Tests build requests with `http.NewRequestWithContext`.
  - Expected `nolint` directives: none.
  - Not run in the spike: Semgrep and Bearer. A Semgrep rule about writing to a
    `ResponseWriter` without escaping may match the stream's `Write`. The content type is
    `text/event-stream`, never rendered as HTML, so a match would be suppressed with
    `nosemgrep` and that justification (constitution, Finding suppression).
  - Test fixtures use no credential-like strings.
- **Rationale**: Principle IV and the repository's scanner rules.

## R-19 Compatibility with this repository's middleware and auth

- **Spike** (`ssespike-compat`), with the stream behind `accesslog`, `compress` and `etag` and a
  request accepting gzip:
  - the response had status 200, `Content-Encoding: gzip`, `Vary: Accept-Encoding` and no
    `ETag`;
  - three events were each decoded before the next one was sent, so every flush reached the
    client compressed;
  - the gzip header is written together with the first data, not with the opening flush. The
    status and headers still reach the client at once.
- **Decision**: the README's compatibility table states:
  - `compress` compresses event streams and delivers every flush;
  - `etag` passes flushed responses through;
  - `maxconcurrency` must not wrap stream routes, as its own documentation says;
  - `ratelimit` bounds reconnection storms;
  - `accesslog` records a stream when it ends, with the duration of the connection;
  - `auth` cookie sessions work with the browser's event source; tokens and API keys suit only
    non-browser clients; 401 and 403 stop browser reconnection;
  - `http.TimeoutHandler` cannot wrap a stream (R-06).

  Every middleware writer implements `Unwrap`, so deadlines reach the connection.
- **Rationale**: FR-061. The statements were checked against `middleware` 0.1.0's documentation
  and the spike, and the `sse` module does not depend on either (R-02).

## R-20 Defaults

| Setting | Default | Source |
|---------|---------|--------|
| Write timeout | 10 s | spec FR-015 |
| Keep-alive interval | 15 s | spec FR-029; the standard suggests "every 15 seconds or so" |
| History | 1,000 events | spec FR-032 |
| Subscriber queue | 64 events | spec FR-028 |
| Reconnection time sent by the server | none | spec FR-016 |
| Client reconnection time | 3 s | R-15 |
| Client maximum backoff | 30 s, or the server's `retry` if larger | R-15 |
| Maximum event size | 1 MiB | R-14 |
| Keep-alive text | `: keep-alive` | R-05 |
| Generated identifier | 12-character random prefix, `-`, decimal sequence | R-10 |

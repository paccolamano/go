# Quickstart: Validating the sse Module

This guide proves the feature works end to end. It only says how to check it. The expected
behavior is defined in:

- [contracts/public-api.md](contracts/public-api.md);
- [contracts/wire-format.md](contracts/wire-format.md);
- [contracts/http-semantics.md](contracts/http-semantics.md);
- [contracts/broker-semantics.md](contracts/broker-semantics.md).

## Prerequisites

- Go 1.26 or newer, and a C toolchain for `-race`.
- golangci-lint at the version pinned in `versions.env`.
- From the repository root, run `make work` once so that `go.work` lists `./sse`. This is only
  for editor navigation; no gate uses it.
- For the manual check (section 6): `curl` and a browser.

## 1. Local gates (constitution)

```bash
make check MODULE=sse       # golangci-lint with zero findings, then go test -race -shuffle=on with coverage ≥ 80%
```

**Expected**: a `PASS` line for `sse` from both `lint` and `test`, with no `.module-exceptions`
entry.

## 2. Structure, tidy and cross-build (the same checks CI runs)

```bash
scripts/structure-check.sh sse      # README, CHANGELOG, no replace, go directive ≤ 1.26
(cd sse && GOWORK=off go mod tidy -diff)
for os in linux darwin windows; do for arch in amd64 arm64; do
  (cd sse && GOWORK=off GOOS=$os GOARCH=$arch go vet ./...) || echo "FAIL $os/$arch"
done; done
```

**Expected**: the structure check reports `PASS` for `sse`. Tidy prints nothing, and there are
no `FAIL` lines. `sse/go.mod` has no `require` list (SC-009).

## 3. Scenario checks per user story

Run these from `sse/` with `GOWORK=off`. Test names follow the `Test<Feature>_<Scenario>`
convention fixed in `tasks.md`. Every end-to-end test runs over HTTP/1.1 and unencrypted HTTP/2,
inside `synctest`, with no network and no real waiting.

| Story | Command | What must hold |
|-------|---------|----------------|
| Wire format | `go test -race -run 'Encode\|Decode\|Conformance\|UTF8' .` | The standard's example streams yield exactly the events of [wire-format.md](contracts/wire-format.md). CR, LF and CRLF are handled, including a CR split across reads. The byte order mark is skipped only at the start. NUL in `id` is ignored. Unicode Table 3-8 substitutions match. An event over the maximum size gives `ErrEventTooLarge` (SC-002). |
| US1: stream | `go test -race -run 'Stream' .` | Headers are set and flushed before any event, and CORS headers set earlier are kept. Multi-line and empty data arrive as specified. Invalid events are refused without writing. A 60 s idle period under a 10 s server `WriteTimeout` does not end the stream. A stalled client makes a send fail within 10 s. `TimeoutHandler` gives `ErrStreamingUnsupported` with nothing written. `retry` is sent first when configured. |
| US2: broker | `go test -race -run 'Broker\|Publish\|Serve' .` | Delivery by topic, once per subscriber across topics. Publishing right after the headers arrive is delivered. With 999 stalled and 1 active subscriber, 10,000 events all reach the active one and the stalled ones end with `ErrSlowConsumer` (SC-005). Keep-alive arrives after 15 s of silence. Generated IDs carry the broker's prefix. Duplicate IDs give `ErrDuplicateID`. A disconnect returns nil and unsubscribes. |
| US3: resume | `go test -race -run 'Resume\|Position\|Reset' .` | Missed events are resent in order without duplicates, on the client's topics only. A new subscription receives the position, and resuming from it works. An old ID without replay gives the reset event (if configured) or the position, and a second reconnection triggers no second reset. IDs from another instance or a previous process are unresolvable. The history never exceeds its size (SC-003, SC-006). |
| US4: documentation | `go test -run 'Example' .` | Every example passes (SC-010). Section 7 covers the README. |
| US5: shutdown | `go test -race -run 'Close\|Shutdown' .` | With `RegisterOnShutdown(b.Close)` and 1,000 streams, `Shutdown` returns nil before its deadline and every client reads a complete response (SC-007). After `Close`, `Serve` answers an empty 200 with `retry` and returns `ErrClosed`, and `Publish` returns `ErrClosed`. |
| US6: replay | `go test -race -run 'Replay\|Interleav' .` | Replay from `e100` delivers `e101`…`e5000`, then the live events. Events published during the replay arrive once (1,000 seeded interleavings, SC-004). An ID found in the history skips the replay. `ErrUnknownID` leads to the reset or the position. A failure ends the stream without live events, and reconnecting retries from the last replayed ID. The broker filters by topic. A disconnect cancels the replay's context. An overflow during the replay gives `ErrSlowConsumer`. |
| US7: replicas | `go test -race -run 'Replica\|DuplicateNotDelivered' .` | Two brokers fed with the same IDs resolve each other's identifiers. Re-delivered events give `ErrDuplicateID` and reach clients once. Generated IDs are not resolvable across instances. |
| US8: client | `go test -race -run 'Client\|Events' .` | Reconnection with `Last-Event-ID`, including after a position. The server's `retry` sets the delay, and backoff is capped. 204 ends without error. 401 and `text/html` give `*ResponseError`. Headers and body repeat on reconnection. A non-replayable body fails before connecting. Cancellation and early break close the connection. |
| Leaks | (all of the above) | `synctest.Test` fails if any goroutine outlives a test, so a full pass proves SC-008. |

## 4. Fuzzing (optional, longer)

```bash
go test -run '^$' -fuzz FuzzDecode -fuzztime 60s .
go test -run '^$' -fuzz FuzzRoundTrip -fuzztime 60s .
go test -run '^$' -fuzz FuzzValidate -fuzztime 60s .
```

**Expected**: no failure.

- The decoder never panics and never yields an event over the maximum size or an ID with NUL.
- Every valid event survives encoding and decoding with its type, normalized data and ID
  (SC-001).

## 5. Benchmarks (informative)

```bash
go test -run '^$' -bench 'Publish|Encode|Decode' -benchmem -count 5 .
```

**Expected**: the numbers are recorded in the pull request description. No target is set,
because no code is declared performance-sensitive (research R-16).

## 6. Manual end-to-end check

Write a small program outside the repository that serves `broker.Serve(w, r, "demo")` on
`GET /events`, publishes an event on `demo` every second, and registers
`srv.RegisterOnShutdown(b.Close)`. Configure `WithRetry(2*time.Second)` and
`WithResetEvent(sse.Event{Type: "reset"})`.

1. `curl -N localhost:8080/events`
   - **Expected**: the headers of [http-semantics.md](contracts/http-semantics.md), a first
     block with `retry: 2000` (and a position after the first event), then one event per second
     with generated identifiers.
2. `curl -N -H 'Last-Event-ID: <an id seen 5 events ago>' localhost:8080/events`
   - **Expected**: the 5 missed events at once, then the live ones.
3. `curl -N -H 'Last-Event-ID: unknown' localhost:8080/events`
   - **Expected**: a `reset` event whose `id` is the newest identifier, then the live events.
4. In a browser console on the same origin, run
   `es = new EventSource('/events'); es.onmessage = e => console.log(e.lastEventId, e.data)`.
   Stop the server with a graceful shutdown and start it again.
   - **Expected**: the shutdown completes at once, the browser reconnects after about 2 s, and
     the console logs either the missed events or a `reset` event (the history of the new
     process is empty).
5. Wrap the route in `middleware/compress` and repeat step 1 with
   `curl -N --compressed localhost:8080/events`.
   - **Expected**: `Content-Encoding: gzip`, and events still arrive one per second.

## 7. Documentation

```bash
go doc github.com/paccolamano/go/sse
```

**Expected**:

- The package documentation and the README cover every flow of FR-060 in its own section, with
  a code example:
  - the integration into an existing server and the compatibility table (FR-061);
  - the frontend notes (FR-062);
  - several replicas, with guidance for Redis Streams, Redis Pub/Sub, Kafka, RabbitMQ classic
    queues and RabbitMQ Streams (FR-063);
  - the testing pattern (FR-064).
- Every exported identifier has a doc comment.
- Every capability of FR-065 has a runnable `Example`.

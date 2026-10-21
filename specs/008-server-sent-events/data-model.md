# Data Model: Server-Sent Events (sse)

**Feature**: [spec.md](spec.md) | **Plan**: [plan.md](plan.md) | **Research**: [research.md](research.md)

The module stores no data outside memory. This document describes its values, their invariants
and the state machines of a subscription and of the client. Exported names are those of
[contracts/public-api.md](contracts/public-api.md). Unexported names are indicative.

## Event (exported)

| Field | Type | Meaning | Validation (FR-009) |
|-------|------|---------|---------------------|
| `ID` | `string` | Event identifier; empty means none | valid UTF-8; no NUL, CR or LF |
| `Type` | `string` | The `event` field; empty means `message` | valid UTF-8; no CR or LF |
| `Data` | `string` | Payload; any line breaks | valid UTF-8 |

Rules:

- `Send` and `Publish` refuse an invalid event with `ErrInvalidEvent`, and nothing is written or
  recorded.
- On the server side, an empty `ID` means no `id` field for streams. For brokers, it means the
  broker generates one (FR-021).
- A decoded event's `ID` is the stream's last event identifier when the event was dispatched,
  which may come from an earlier event (FR-050). Its `Type` is empty when the stream set none, and
  its `Data` uses LF line breaks.
- Events delivered by the replay function must also have a non-empty `ID` and at least one topic
  (FR-043).

## Configuration

### `config` (unexported), filled by `Option` values

| Field | Set by | Default | Validation (error wraps `ErrInvalidConfig`) | Accepted by |
|-------|--------|---------|---------------------------------------------|-------------|
| `retry time.Duration` | `WithRetry` | none | ≥ 1 ms | `NewStream`, `NewBroker` |
| `writeTimeout time.Duration` | `WithWriteTimeout` | 10 s | > 0 | `NewStream`, `NewBroker` |
| `keepAlive time.Duration` | `WithKeepAlive` | 15 s | > 0 | `NewBroker` |
| `history int` | `WithHistory` | 1,000 | ≥ 1 | `NewBroker` |
| `buffer int` | `WithBuffer` | 64 | ≥ 1 | `NewBroker` |
| `reset *Event` | `WithResetEvent` | none (disabled) | `Type` and `Data` valid per FR-009; `ID` ignored (FR-036) | `NewBroker` |
| `replay ReplayFunc` | `WithReplay` | none | not nil | `NewBroker` |
| `httpClient *http.Client` | `WithHTTPClient` | `http.DefaultClient` | not nil | `NewClient` |
| `maxEventSize int` | `WithMaxEventSize` | 1 MiB | ≥ 1 | `NewClient`, `Decode` |
| `set optionSet` | every option | — | used for the applicability checks (research R-03) | — |

When an option is given more than once, the last call wins.

## Stream (exported type, unexported fields)

| Field | Meaning |
|-------|---------|
| `mu sync.Mutex` | Serializes writes: each event reaches the client whole (FR-005) |
| `w http.ResponseWriter`, `rc *http.ResponseController` | The response |
| `ctx context.Context` | The request's context (research R-08) |
| `writeTimeout time.Duration` | Per-write deadline (FR-015) |
| `buf []byte` | Encoding buffer, reused |
| `err error` | First write failure. Once set, every send returns it without writing (research R-07) |

States: **open**, then **broken** after the first failed write, or once the request context has
ended. There is no explicit close: the handler's return ends the response.

## Broker (exported type, unexported fields)

| Field | Meaning |
|-------|---------|
| `mu sync.Mutex` | Guards everything below |
| `cfg config` | Immutable after `NewBroker` |
| `prefix string` | 12 random base32 characters, chosen at construction (research R-10) |
| `seq uint64` | Last assigned sequence number. Never decreases |
| `ring []entry`, `start, n int` | The history: at most `cfg.history` entries, in sequence order |
| `index map[string]uint64` | Identifier → sequence, for exactly the entries in `ring` |
| `topics map[string]map[*subscriber]struct{}` | Topic index of live subscribers |
| `closed bool`, `done chan struct{}` | Set and closed once by `Close` |

### `entry` (unexported)

| Field | Meaning |
|-------|---------|
| `seq uint64` | Position in the publication order |
| `ev Event` | The event, with `ID` set: explicit or generated |
| `topics []string` | Its topics, deduplicated |

Invariants:

- `len(index) == n ≤ cfg.history`. The sequence numbers in `ring` are strictly increasing.
- Identifiers in `ring` are unique (FR-022).
- Every subscriber receives entries in increasing `seq` order (FR-023).

### `subscriber` (unexported)

| Field | Meaning |
|-------|---------|
| `topics []string` | Its topics, deduplicated |
| `queue chan *entry` | Capacity `cfg.buffer` |
| `overflow chan struct{}` | Closed once when `queue` was full (FR-028) |
| `lastSeq uint64` | Last sequence offered, guarded by `Broker.mu`. Gives exactly-once delivery across topics (FR-024) |

## Subscription state machine (`Serve`)

```text
            ┌─ topics empty / writer cannot flush ──▶ [Rejected]  error, nothing written
 [Start] ───┤
            ├─ broker closed ──▶ [EmptyStream] headers (+retry), return ErrClosed
            │
            └─ register under lock + resolve Last-Event-ID ──▶ [Opening]
                    │ absent ................ first block = position (if history not empty)
                    │ found ................. first block = —; snapshot = later entries on topics
                    │ not found, no replay .. first block = reset event or position
                    │ not found, replay ..... first block = —; replay pending
                    ▼
                 [Opening] write headers + retry + first block
                    │
                    ├─ snapshot ──▶ [Resuming/history] write snapshot entries ──┐
                    ├─ replay ────▶ [Resuming/replay] (below) ──────────────────┤
                    └─ otherwise ───────────────────────────────────────────────┤
                                                                                ▼
                                                                             [Live]
     [Live] selects on:  request ctx done ─▶ [Ended] nil
                         broker done ──────▶ [Ended] ErrClosed
                         overflow ─────────▶ [Ended] ErrSlowConsumer
                         queue entry ──────▶ write (failure ─▶ [Ended] wrapped write error)
                         keep-alive timer ─▶ write ": keep-alive"
     Any state after registration, on return: unregister from the topic index.
```

### Replay sub-states

```text
[Resuming/replay] call ReplayFunc(r.Context(), lastID, topics, send)
   send(ev, topics...):  invalid / no ID / no topic ─▶ mark failure, return ErrInvalidEvent
                         ID == lastID ─────────────▶ skip
                         no topic of the subscription ─▶ skip
                         otherwise ─▶ write event; remember ID (last cfg.buffer IDs)
                         overflow / client gone / write failure ─▶ return a stop error
   function returns:
     nil ───────────────▶ drain queue skipping remembered IDs ─▶ [Live]
     ErrUnknownID ──────▶ write reset event (or position) ─▶ drain queue ─▶ [Live]
     other / failure ───▶ [Ended] error wrapping the failure (complete 200, no live events)
     panic ─────────────▶ recovered ─▶ same as other / failure ("sse: replay panicked: …")
     overflow ──────────▶ [Ended] ErrSlowConsumer
     client gone ───────▶ [Ended] nil
```

After the function returns, `send` returns an error and writes nothing. No keep-alive is written
while the function runs (FR-029).

For `ErrUnknownID`, the identifier of the reset event or of the position is the most recent
history identifier at registration. Queued entries are all later than it, so draining the queue
after it neither loses nor repeats events.

## Decoder (unexported)

| Field | Meaning |
|-------|---------|
| `r *bufio.Reader` | Source |
| `atStart bool` | A byte order mark may still be skipped |
| `afterCR bool` | The previous line ended with CR, so skip an immediate LF |
| `line []byte` | Current line, bounded by `maxEventSize` |
| `data, typ []byte`, `idBuf string` | Buffers of the pending event |
| `lastID string` | Last event identifier, set at dispatch (FR-050) |
| `retry time.Duration`, `retrySet bool` | Last valid `retry` value (used by the client) |
| `size int` | Bytes in the pending event's buffers. Never exceeds `maxEventSize` (FR-051) |

Invariant: memory held for one event never exceeds `maxEventSize` plus the reader's buffer (SC-006).

## Client state machine (`Events`)

```text
 [Start] ── body not replayable ─▶ [Ended] error
    │
    ▼
 [Connecting] clone request, set Accept / Last-Event-ID
    │  network error ───────────────────────────▶ [Waiting] delay doubles (cap) 
    │  204 ─────────────────────────────────────▶ [Ended] no error
    │  other status / wrong content type ───────▶ [Ended] *ResponseError
    │  200 text/event-stream ───────────────────▶ [Streaming] delay reset
    ▼
 [Streaming] decode, yield events, track lastID and retry
    │  normal end of body / network error ──────▶ [Waiting] reconnection time
    │  decode error (e.g. ErrEventTooLarge) ────▶ [Ended] error
    │  consumer stops iterating ────────────────▶ [Ended] body closed
    ▼
 [Waiting] timer or request ctx done ─▶ [Connecting] / [Ended] ctx error
```

From any state, the request's context ending leads to **Ended** with the context's error, and the
open body is closed.

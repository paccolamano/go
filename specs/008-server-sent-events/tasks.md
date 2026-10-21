---

description: "Task list for the sse server-sent events module"
---

# Tasks: Server-Sent Events (sse)

**Input**: Design documents from `specs/008-server-sent-events/`

**Prerequisites**: [plan.md](plan.md), [spec.md](spec.md), [research.md](research.md),
[data-model.md](data-model.md), [contracts/](contracts/), [quickstart.md](quickstart.md)

**Tests**: REQUIRED. Constitution Principle III is non-negotiable:

- Tests are written first and observed failing (a compilation failure counts as red).
- Coverage must be ≥ 80%, with `-race -shuffle=on`.
- The decoder parses external input, so it needs fuzz tests.
- Every primary API needs an `Example`.

Every test task below precedes the implementation it covers.

**Organization**: tasks are grouped by user story (spec.md US1–US8). Every path is relative to the
repository root. All Go code lives in the module `sse/`, package `sse`.

User Story 4 (documentation, P1) is scheduled after the other stories because it documents all of
their flows. Each story phase already adds its own README section and runnable example, so the
documentation grows with the code.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: can run in parallel with other [P] tasks of the same phase: different files, and no
  dependency on an incomplete task.
- **[Story]**: US1–US8 from spec.md. Setup, Foundational and Polish tasks have no story label.

## Conventions for every task

- **Packages**:
  - Behavior tests use the external package `sse_test`.
  - Tests of unexported code use `package sse`, in files named `*_internal_test.go`.
- **Test names**: `Test<Feature>_<Scenario>`, chosen so that the `-run` patterns of
  [quickstart.md §3](quickstart.md) select them:
  - `TestOptions_*`;
  - `TestEncode_*`, `TestDecode_*`, `TestConformance_*`, `TestUTF8_*`;
  - `TestStream_*`;
  - `TestBroker_*`, `TestPublish_*`, `TestServe_*`;
  - `TestResume_*`, `TestPosition_*`, `TestReset_*`;
  - `TestClose_*`, `TestShutdown_*`;
  - `TestReplay_*`, `TestInterleave_*`;
  - `TestReplica_*`;
  - `TestClient_*`, `TestEvents_*`;
  - `TestHistory_*`, `TestID_*`, `TestPipeNet_*`.

  Fuzz targets are `FuzzDecode`, `FuzzValidate` and `FuzzRoundTrip`. Benchmarks use `b.Loop()`.
- **Test style**:
  - table-driven, with `t.Run`;
  - no network: end-to-end tests use the in-memory network of T016;
  - no real waiting: every test that involves time runs inside `synctest.Test`, where
    `time.Sleep` advances virtual time and `synctest.Wait` waits for quiescence;
  - goroutines are joined with `sync.WaitGroup.Go` and `Wait`.
- **synctest rules** (research R-16):
  - Create brokers, clients, channels and timers **inside** the bubble. A channel created
    outside a bubble panics when used inside it.
  - `t.Cleanup` functions run inside the bubble, so register server and transport shutdown with
    `t.Cleanup`.
  - `synctest.Test` fails when a goroutine outlives the test, which is how SC-008 is verified.
  - `net.Pipe` is synchronous: a handler cannot complete a write until the client reads. Test
    clients read in their own goroutine (the helpers of T016 do this), except in tests that
    deliberately stall a client.
- **Requests**: always `http.NewRequestWithContext` (`noctx`).
- **Fixtures**: neutral strings, with no credential-like values (Gitleaks, Bearer).
- **Errors**: every error from the standard library is wrapped with context using `%w`
  (`wrapcheck`). Expected `//nolint` directives: none (research R-18).
- **Test helpers** take `testing.TB` and call `tb.Helper()` first (`thelper`).
- **Documentation**: every exported identifier has a doc comment with the wording of
  [contracts/public-api.md](contracts/public-api.md), adapted. README sections are added by each
  story and completed in Phase 10.

---

## Phase 1: Setup (Shared Infrastructure)

**Purpose**: create the module and register it with the repository tooling.

- [X] T001 Create `sse/go.mod` containing exactly `module github.com/paccolamano/go/sse` and `go 1.26.0`, with no `require` and no `replace`.
- [X] T002 [P] Create `sse/CHANGELOG.md` in Keep a Changelog format (the header text of `slogx/CHANGELOG.md`, adapted), with an empty `## [Unreleased]` section. The entries are added in T058.
- [X] T003 [P] Create `sse/README.md` with:
  - the title `# sse`;
  - the purpose paragraph: "Server-sent events for Go: stream events from a handler, deliver them to many clients by topic with resuming after reconnections, and consume streams from Go with the same reconnection rules as browsers. The module has no dependency outside the standard library.";
  - `go get github.com/paccolamano/go/sse`;
  - "Minimum Go version: 1.26";
  - empty headings in the order of research R-17: "Building blocks", "Single stream", "Broker", "Integrating into an existing server", "Reconnection and resuming", "Replay from a durable bus", "Several replicas", "Shutting down", "Frontend notes", "Consuming from Go", "Testing handlers" and "Defaults and limits". They are filled by the story phases and by Phase 10.
- [X] T004 Run `make work` from the repository root so that `go.work` lists `./sse`. Then confirm that `scripts/structure-check.sh sse` reports PASS (depends on T001–T003).

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: options, event validation and encoding, the decoder that every test uses to read
responses, and the in-memory network for end-to-end tests.

**⚠️ CRITICAL**: no user story work can begin until this phase is complete.

- [X] T005 Create `sse/doc.go` with a short package doc comment for `package sse`: "Package sse implements server-sent events as defined by the WHATWG HTML Living Standard: streams written by a handler, a broker that delivers events by topic and resumes clients after reconnections, and a client that decodes streams and reconnects." It is expanded in T055.
- [X] T006 [P] Write failing internal tests in `sse/options_internal_test.go` (`package sse`) for `newConfig(ctor string, accepted optionSet, opts []Option) (config, error)`:
  1. `TestOptions_Defaults`: with no options the config has:
     - `writeTimeout` 10 s;
     - `keepAlive` 15 s;
     - `history` 1000;
     - `buffer` 64;
     - `maxEventSize` 1<<20;
     - `httpClient == http.DefaultClient`;
     - no `retry`, no `reset` and no `replay`.
  2. `TestOptions_Validation`. Each rule of data-model.md "Configuration" returns an error that wraps `ErrInvalidConfig`:

     | Option | Rule |
     |--------|------|
     | `WithRetry` | "≥ 1 ms" (0, −1 and 999µs fail; 1 ms passes) |
     | `WithWriteTimeout` | "> 0" |
     | `WithKeepAlive` | "> 0" |
     | `WithHistory` | "≥ 1" |
     | `WithBuffer` | "≥ 1" |
     | `WithResetEvent` | "`Type` and `Data` valid per FR-009; `ID` ignored (FR-036)": a type with LF fails, data `"\xff"` fails, an ID with LF passes |
     | `WithReplay` | "not nil" |
     | `WithHTTPClient` | "not nil" |
     | `WithMaxEventSize` | "≥ 1" |

     A nil `Option` in `opts` returns an error and never panics.
  3. `TestOptions_Applicability`: using the accepted sets of [contracts/public-api.md](contracts/public-api.md) (`streamOptions`, `brokerOptions`, `clientOptions`, `decodeOptions`), an option outside the set returns an error wrapping `ErrInvalidConfig`. Examples: `WithHistory` for `NewStream`, `WithRetry` for `NewClient`, `WithHTTPClient` for `Decode`.
  4. `TestOptions_ErrorFormat`: messages are `sse: invalid configuration: <Constructor>: <Option>: <reason>`, for example `sse: invalid configuration: NewBroker: WithHistory: must be at least 1`.
  5. `TestOptions_LastWins`: two `WithHistory` calls keep the last.
- [X] T007 Implement `sse/options.go` until T006 passes:
  - `var ErrInvalidConfig = errors.New("sse: invalid configuration")`;
  - `type Option func(*config)`;
  - the unexported `optionSet` bits for the nine options;
  - the accepted sets `streamOptions`, `brokerOptions`, `clientOptions` and `decodeOptions`;
  - the `config` fields of data-model.md;
  - default constants: `defaultWriteTimeout`, `defaultKeepAlive`, `defaultHistory`, `defaultBuffer`, `defaultMaxEventSize`, `defaultReconnect = 3 * time.Second` and `maxBackoff = 30 * time.Second`;
  - `WithRetry`, `WithWriteTimeout`, `WithKeepAlive`, `WithHistory`, `WithBuffer`, `WithResetEvent`, `WithReplay`, `WithHTTPClient` and `WithMaxEventSize`, each doc comment naming the constructors that accept it;
  - `newConfig`, which sets the defaults, applies the options (reporting nil ones), checks applicability, then validates.

  `WithReplay` takes a `ReplayFunc`. Declare `type ReplayFunc` in `sse/replay.go` now, with its doc comment from the contract and no other code. The rest of that file comes in T044.
- [X] T008 [P] Write failing internal tests in `sse/event_internal_test.go` (`package sse`):
  1. `TestEncode_Validation`: `ev.validate()` returns an error wrapping `ErrInvalidEvent` for:
     - an ID containing NUL, CR or LF;
     - a type containing CR or LF;
     - invalid UTF-8 in the ID, the type or the data.

     It returns nil for valid events, including empty ones and a type containing NUL.
  2. `TestEncode_Event`: `appendEvent(nil, ev)` produces the exact bytes of [wire-format.md](contracts/wire-format.md) "Examples":

     | Event | Bytes |
     |-------|-------|
     | `{Data: "hello"}` | `"data: hello\n\n"` |
     | `{Type: "progress", ID: "7", Data: "50%"}` | `"event: progress\nid: 7\ndata: 50%\n\n"` |
     | `{Data: "a\r\nb\rc\n"}` | `"data: a\ndata: b\ndata: c\ndata:\n\n"` |
     | `{Data: ""}` | `"data:\n\n"` |
     | `{Data: "  x"}` | `"data:   x\n\n"` |

     Plus `{Data: "a\n\nb"}` gives `"data: a\ndata:\ndata: b\n\n"`.
  3. `TestEncode_Records`:
     - `appendRetry(nil, 3*time.Second)` is `"retry: 3000\n"`; 1500 µs gives `"retry: 1\n"` (whole milliseconds, truncated);
     - `appendPosition(nil, "K-1")` is `"id: K-1\n\n"`, and `appendPosition(nil, "")` is `"id:\n\n"`;
     - `keepAliveLine == ": keep-alive\n"`.
- [X] T009 Implement `sse/event.go` until T008 passes:
  - `type Event struct{ ID, Type, Data string }` with the contract's doc comment;
  - `var ErrInvalidEvent = errors.New("sse: invalid event")`;
  - `func (ev Event) validate() error`, with messages like `sse: invalid event: ID contains a line feed`;
  - `appendField(dst []byte, name, value string) []byte`, which writes the space only before a non-empty value (research R-05);
  - `appendEvent`, `appendRetry`, `appendPosition` and `const keepAliveLine`.
- [X] T010 [P] Write failing internal tests in `sse/utf8_internal_test.go` (`package sse`): `TestUTF8_MaximalSubparts`. `appendValidUTF8(nil, src)` replaces each maximal invalid subpart with U+FFFD, per the Unicode Standard chapter 3, Table 3-8. The vector `61 F1 80 80 E1 80 C2 62 80 63 80 BF 64` gives `"a���b�c��d"`. Also test:
  - `C0 AF` gives two replacements;
  - `E0 80 AF` gives three;
  - `ED A0 80` (a surrogate) gives three;
  - `F4 90 80 80` gives four;
  - `E2 82` at the end gives one;
  - `E2 82 41` gives `"�A"`;
  - valid input is returned unchanged.
- [X] T011 Implement `sse/utf8.go` until T010 passes: `appendValidUTF8(dst, src []byte) []byte`. It uses `utf8.DecodeRune` for valid runes. At an invalid position it consumes the longest prefix that is a valid start of a sequence, using the accept ranges of the Encoding Standard's UTF-8 decoder (lower and upper boundaries for the second byte after `E0`, `ED`, `F0` and `F4`), then writes one U+FFFD.
- [X] T012 [P] Write failing tests in `sse/decode_test.go` (`package sse_test`) for `Decode(r io.Reader, opts ...Option) iter.Seq2[Event, error]`:
  1. `TestConformance_StandardExamples`: the vectors of [wire-format.md](contracts/wire-format.md) "Conformance vectors", each as a subtest, comparing the full list of yielded events. This includes `" third event"` with its leading space, and the variant without the final line feed, which yields two events.
  2. `TestDecode_LineEndings`: the same two events encoded with LF, CRLF and CR-only line endings are decoded identically.
  3. `TestDecode_CRSplitAcrossReads`:
     - `"data: x\r"` and `"\ndata: y\r\n\r\n"` delivered by a reader that returns them in two `Read` calls yield one event `"x\ny"`;
     - over an `io.Pipe`, after writing `"data: x\r\r"` only, the event is yielded before any further write. The decoder does not wait for the byte after a CR.
  4. `TestDecode_BOM`: a leading `EF BB BF` is skipped. A second one at the start of a later line makes that line's field name start with U+FEFF, so the field is ignored.
  5. `TestDecode_Fields`:
     - `id` with NUL is ignored;
     - an `id` with no value resets the identifier to empty;
     - unknown fields are ignored;
     - `data` with no colon adds an empty line;
     - exactly one leading space is removed;
     - comments are ignored;
     - an empty data buffer dispatches nothing and resets the type;
     - the last event identifier persists into later events without an `id` field.
  6. `TestDecode_InvalidUTF8`: `"data: a\xffb\n\n"` yields `"a�b"`.
  7. `TestDecode_EventTooLarge`, with `WithMaxEventSize(16)`:
     - a 17-byte line yields an error matching `ErrEventTooLarge`;
     - two data lines of 10 bytes each also exceed the limit together;
     - events within the limit are yielded before the error.
  8. `TestDecode_ReadError`: a reader that fails after one event yields that event, then the error, wrapped, and stops.
  9. `TestDecode_StopEarly`: breaking after the first event stops reading. A counting reader shows no further `Read` calls.
  10. `TestDecode_InvalidOption`: `Decode(r, WithHistory(1))` yields exactly one error wrapping `ErrInvalidConfig`.
- [X] T013 [P] Write failing internal tests in `sse/decode_internal_test.go` (`package sse`) for the unexported parser: `TestDecode_Retry`.
  - `retry: 2500` sets `p.retry` to 2.5 s with `retrySet` true;
  - `retry: 2.5`, `retry: -1` and `retry:` are ignored;
  - a position block (`id: K-9\n\n`) sets `p.lastID` to `K-9` without yielding an event.
- [X] T014 [P] Write the failing fuzz targets of the decoder in `sse/fuzz_test.go` and `sse/fuzz_internal_test.go` (Principle III: written before T015, red as a compilation failure):
  - in `sse/fuzz_test.go` (`package sse_test`): `FuzzDecode(data []byte)`. Decoding with `WithMaxEventSize(256)` never panics, and every yielded event has `len(ID)+len(Type)+len(Data) ≤ 256` and no NUL in `ID`;
  - in `sse/fuzz_internal_test.go` (`package sse`): `FuzzRoundTrip(id, typ, data string)`. For events that pass `validate`, decoding `appendEvent` with the parser gives the type, the data with CRLF and CR replaced by LF, and the ID when it is not empty (SC-001).

  Seed both with the conformance vectors of T012.
- [X] T015 Implement `sse/decode.go` until T012, T013 and T014 pass (depends on T007 and T011):
  - `var ErrEventTooLarge = errors.New("sse: event too large")`;
  - `type parser struct` with the fields of data-model.md "Decoder";
  - `newParser(r io.Reader, maxEventSize int) *parser`;
  - `(p *parser) next() (Event, error)`, which returns `io.EOF` at a clean end of stream;
  - `Decode`, which builds the config with `decodeOptions` and yields from `next`.

  Line reading follows research R-14: a CR ends a line at once, and a flag skips the following LF. Text goes through `appendValidUTF8`.
- [X] T016 [P] Create the end-to-end helpers in `sse/pipenet_test.go` (`package sse_test`):
  - `type pipeNet`: a `net.Listener` whose `Dial(ctx, network, addr string) (net.Conn, error)` creates a `net.Pipe`, hands the server end to `Accept` and returns the client end. `Close` makes `Accept` return `net.ErrClosed`.
  - `type env struct { Client *http.Client; Server *http.Server; H2 bool }` and `func e2e(t *testing.T, fn func(e *env))`, where `e.T` is the test of the bubble (a callback without a `*testing.T` parameter keeps `thelper` from treating it as a helper). It runs `fn` in two subtests, `http1` and `h2c`, each inside `synctest.Test`. For `h2c`:
    - both `Server.Protocols` and `Transport.Protocols` have only `SetUnencryptedHTTP2(true)`;
    - the transport sets `HTTP2: &http.HTTP2Config{MaxReceiveBufferPerStream: 64 << 10}`, so that a client that stops reading blocks the server after 64 KiB instead of the default 4 MiB per stream;
    - the connection-level window keeps its default, because every stream of a test shares one connection and a small window would starve the active client too (research R-16).
  - `func (e *env) Start(h http.Handler, writeTimeout time.Duration)`: starts `Server.Serve` on a new `pipeNet` in a goroutine. It registers `t.Cleanup` to close the server, close the transport's idle connections and close the listener.
  - `func (e *env) Get(tb testing.TB, ctx context.Context, path string, header http.Header) *http.Response`, against `http://sse.test`.
  - `func readEvents(tb testing.TB, body io.ReadCloser) (<-chan Event, <-chan error)`: decodes with `sse.Decode` in a goroutine and forwards events. The error channel receives the terminal error (nil at a clean end) and both channels are closed.
  - `func readLines(tb testing.TB, body io.ReadCloser) <-chan string`: raw lines, for comments and positions.
  - `TestPipeNet_Smoke`: a handler that writes "ok" is reached over both protocols.

**Checkpoint**: options, encoding, decoding and the test network are ready. User story work can
begin.

---

## Phase 3: User Story 1 - Stream events from a handler (Priority: P1) 🎯 MVP

**Goal**: `NewStream`, `Send` and `KeepAlive`, with correct headers, safe encoding and per-write
deadlines.

**Independent Test**: a handler opens a stream on a test server with a write timeout. Events of
every shape are decoded by the client, and the stream is exercised with a stalled client and with
a writer that cannot flush.

### Tests for User Story 1 ⚠️

- [X] T017 [P] [US1] Write failing unit tests in `sse/stream_test.go` (`package sse_test`), using `httptest.ResponseRecorder` and `httptest.NewRequest`:
  1. `TestStream_Headers` (scenario 1): after `NewStream`:
     - status 200, `Content-Type: text/event-stream`, `Cache-Control: no-store`, `X-Accel-Buffering: no`;
     - `rec.Flushed` is true;
     - an `Access-Control-Allow-Origin` header set before `NewStream` is kept.
  2. `TestStream_Encoding` (scenarios 2–4): `Send` of the events of T008 appends exactly their bytes to `rec.Body`.
  3. `TestStream_InvalidEvent` (scenario 5): `Send(Event{ID: "a\nb"})` and `Send(Event{Data: "\xff"})` return `ErrInvalidEvent` and write nothing. A following valid `Send` succeeds.
  4. `TestStream_Unsupported` (scenario 8): inside `http.TimeoutHandler`, `NewStream` returns `ErrStreamingUnsupported`. The handler then writes `"fallback"`, and the recorded body is exactly `"fallback"`, with no event stream header.
  5. `TestStream_Retry` (scenario 9): with `WithRetry(5*time.Second)` the body starts with `"retry: 5000\n\n"`.
  6. `TestStream_KeepAlive`: `KeepAlive()` appends `": keep-alive\n"`.
  7. `TestStream_Concurrent`: 50 goroutines each `Send` one event with 1 KiB of data. Decoding the body yields 50 intact events.
  8. `TestStream_Options`: `NewStream(w, r, WithHistory(1))` returns an error wrapping `ErrInvalidConfig` and writes nothing.
- [X] T018 [P] [US1] Write failing end-to-end tests in `sse/stream_e2e_test.go` (`package sse_test`), all with `e2e`:
  1. `TestStream_Immediate` (scenario 2): the handler sends one event, then waits on a channel. The client receives the event before the channel is closed.
  2. `TestStream_IdleBeyondWriteTimeout` (scenario 6): `Start(h, 10*time.Second)`. The handler waits for a channel, then sends. The test sleeps 60 s of virtual time, closes the channel, and the client receives the event.
  3. `TestStream_StalledClient` (scenario 7): the client never reads the body. The handler sends 1 KiB events in a loop until `Send` fails. The failing call took at most `WithWriteTimeout(2*time.Second)` (virtual), and later calls return the same error.
  4. `TestStream_ClientGone` (scenario 10): the client cancels its request. After `synctest.Wait`, `Send` returns an error and writes nothing.

- [X] T019 [P] [US1] Add the failing fuzz target `FuzzValidate(id, typ, data string)` to `sse/fuzz_test.go` (`package sse_test`). An event accepted by `Stream.Send` on an `httptest.ResponseRecorder` decodes to exactly one event, and an event refused with `ErrInvalidEvent` writes nothing. Seed it with the events of T008.

### Implementation for User Story 1

- [X] T020 [US1] Implement `sse/stream.go` until T017, T018 and T019 pass:
  - `var ErrStreamingUnsupported = errors.New("sse: response writer cannot flush")`;
  - `canFlush(w http.ResponseWriter) bool`, which walks the `Unwrap` chain looking for `FlushError() error` or `http.Flusher` (research R-06);
  - `type Stream struct` with the fields of data-model.md "Stream";
  - `newStream(w, r, cfg config, first []byte) (*Stream, error)`, which sets the three headers, writes status 200 and `first` in one write, then flushes. `Serve` reuses it;
  - `NewStream`, which calls `newConfig("NewStream", streamOptions, opts)`; `first` is `appendRetry(…) + "\n"` when `WithRetry` is set;
  - `Send` (validate, encode into `buf`, write) and `KeepAlive`;
  - `(s *Stream) write(b []byte) error` under `mu`. It returns `s.err` if set, then `ctx.Err()` if done. It sets the write deadline to now + `writeTimeout`, ignoring `http.ErrNotSupported`, then writes, flushes, and clears the deadline. On failure it stores the wrapped error in `s.err` (research R-07).

### Documentation for User Story 1

- [X] T021 [P] [US1] Create the whole-file example `sse/example_stream_test.go` (`package sse_test`): one `ExampleStream` plus a small `progressHandler`. The handler opens a stream with `WithRetry(5*time.Second)` and sends three `progress` events. The example serves it into an `httptest.ResponseRecorder` and prints the body. The `// Output:` contains the exact bytes.
- [X] T022 [US1] Fill the README section "Single stream" in `sse/README.md`:
  - when to use a stream;
  - a job progress handler;
  - `ErrStreamingUnsupported` answered with an error;
  - how a handler resumes on its own by reading `r.Header.Get("Last-Event-ID")` (FR-019);
  - why the server's `WriteTimeout` can stay (FR-015).

  Add a matching paragraph to `sse/doc.go`.

**Checkpoint**: User Story 1 is complete. A single handler can stream events correctly (MVP).

---

## Phase 4: User Story 2 - Deliver events to many clients by topic (Priority: P1)

**Goal**: `NewBroker`, `Publish` and `Serve` for live delivery: topics, exactly once, no blocking,
slow consumers, keep-alive and generated identifiers.

**Independent Test**: clients subscribed to different topics through a broker on a test server
receive what is published, from concurrent tasks, while other clients stall.

### Tests for User Story 2 ⚠️

- [X] T023 [P] [US2] Write failing internal tests in `sse/id_internal_test.go` (`package sse`): `TestID_Format`.
  - `newPrefix()` returns 12 characters of the base32 alphabet of `crypto/rand.Text`;
  - two calls differ;
  - `formatID("ABCDEFGHJKLM", 42) == "ABCDEFGHJKLM-42"`;
  - the result passes `Event{ID: id}.validate()`.
- [X] T024 [P] [US2] Write failing internal tests in `sse/history_internal_test.go` (`package sse`) for `history`:
  1. `TestHistory_Bound` (SC-006): with capacity 3, adding 10 entries keeps sequences 8–10. `len(index) == 3`, and the evicted identifiers are absent from `index`.
  2. `TestHistory_Lookup`: `lookup(id)` finds kept entries and misses evicted or unknown ones.
  3. `TestHistory_After`: `after(seq, topics)` returns the later entries that share a topic with `topics`, in order, each once even when it matches several topics.
  4. `TestHistory_Latest`: `latestID()` is `""` when empty, and the newest identifier otherwise.
  5. `TestHistory_Contains`: duplicate detection for kept identifiers only.
- [X] T025 [US2] Implement `sse/id.go` (`newPrefix`, `formatID`) and `sse/history.go` (`type entry`, `type history` with `add`, `lookup`, `after`, `latestID` and `contains`) until T023 and T024 pass. The invariants of data-model.md "entry" are: "`len(index) == n ≤ cfg.history`. The sequence numbers in `ring` are strictly increasing" and "Identifiers in `ring` are unique (FR-022)".
- [X] T026 [P] [US2] Write failing tests in `sse/broker_test.go` (`package sse_test`):
  1. `TestPublish_Invalid` (scenario 8): each of these returns an error and records nothing (a later resume test sees nothing):
     - an invalid event (`ErrInvalidEvent`);
     - no topic;
     - an empty topic.
  2. `TestPublish_Duplicate` (scenario 7): publishing `ID: "42"` twice returns `ErrDuplicateID` the second time.
  3. `TestBroker_Options`: `NewBroker(WithHTTPClient(http.DefaultClient))` and `NewBroker(WithMaxEventSize(1))` return errors wrapping `ErrInvalidConfig`.
- [X] T027 [P] [US2] Write failing end-to-end tests in `sse/serve_test.go` (`package sse_test`). The handler is `func(w, r) { err := b.Serve(w, r, topicsFromQuery(r)...); record err }`, with the broker created inside the bubble:
  1. `TestServe_Topics` (scenario 1): A on `user:alice`, B on `user:bob`. A publish on `user:alice` reaches A only. After `synctest.Wait`, B's channel holds no event.
  2. `TestServe_OncePerSubscriber` (scenario 2): subscribed to `user:alice` and `broadcast`. Publishing on both topics, and again with a duplicated topic, delivers each event once.
  3. `TestServe_PublishAfterHeaders` (scenario 3): as soon as `Get` returns, publish without any wait. The event arrives.
  4. `TestServe_GeneratedIDs` (scenario 6): events published without an ID arrive with `ID`s of the form `<12 chars>-<n>`, with increasing n. Two brokers use different prefixes.
  5. `TestServe_KeepAlive`:
     - after 15 s of virtual silence, `readLines` delivers `": keep-alive"`;
     - with `WithKeepAlive(5*time.Second)`, events published every 4 s produce no comment.
  6. `TestServe_Disconnect` (scenario 9): the client cancels. `Serve` returns nil, and a later publish does not block or error.
  7. `TestServe_Order`: 50 goroutines publish 20 events each on one topic, with 3 subscribers. All three receive the same 1,000 events in the same order (FR-023).
  8. `TestServe_Unsupported`: under `http.TimeoutHandler`, `Serve` returns `ErrStreamingUnsupported` and writes nothing.
  9. `TestServe_NoTopics`: `Serve(w, r)` returns an error and writes nothing.
  10. `TestServe_Retry`: with `WithRetry(3*time.Second)`, the first line is `"retry: 3000"`.
  11. `TestServe_DuplicateNotDelivered` (User Story 7 scenario 2, written here because T029 implements it): a feed publishes `bus-1`…`bus-10`, then re-publishes `bus-5`…`bus-8` after a simulated reconnection. Each re-publish returns `ErrDuplicateID`, and the subscriber receives each ID once.
- [X] T028 [P] [US2] Write failing end-to-end tests in `sse/stalled_test.go` (`package sse_test`): `TestServe_StalledSubscribers` (scenario 4, SC-005).
  - 999 clients open a subscription and never read. One client reads.
  - 10,000 events with 256 bytes of data each (about 2.6 MB) are published from the test goroutine. Over h2c this exceeds the 64 KiB per-stream buffer of T016, so the stalled streams block as they do over HTTP/1.1. Virtual time does not advance during the publishing loop: `time.Since(start) == 0`, so no publish call waited.
  - The active client receives all 10,000 in order.
  - Every stalled `Serve` returns an error matching `ErrSlowConsumer`.
- [X] T029 [US2] Implement `sse/broker.go` until T026 passes:
  - `var ErrClosed = errors.New("sse: broker closed")` and `var ErrDuplicateID = errors.New("sse: duplicate event identifier")`;
  - `type Broker struct` with the fields of data-model.md "Broker";
  - `NewBroker`, using `brokerOptions` and `newPrefix`;
  - `Publish`, which validates the event, then the topics (non-empty, deduplicated), then takes the lock, checks closed, checks duplicates, assigns `seq` and the ID, and adds to the history. It offers the entry to each subscriber of each topic, skipping those whose `lastSeq == seq`, with a non-blocking send. A full queue calls `overflowLocked(sub)`, which removes the subscriber from the index and closes its `overflow` channel once (research R-09).
- [X] T030 [US2] Implement the live path of `sse/serve.go` until T027 and T028 pass:
  - `var ErrSlowConsumer = errors.New("sse: subscriber too slow")`;
  - `type subscriber struct`, with the fields of data-model.md;
  - `Serve`, which validates the topics and checks `canFlush` before anything else. Under the lock it registers the subscriber (resolution comes in T035), then opens with `newStream` and runs `live(ctx, s, sub)`;
  - `live`, a `select` on the request context (return nil), the broker's `done` (return `ErrClosed`), `overflow` (return `ErrSlowConsumer`), the queue (write the entry) and a keep-alive `time.Timer`. The timer is reset after every write.
  - When a write fails after the subscriber has overflowed, return `ErrSlowConsumer`, so that a client stalled in a write reports the slow-consumer outcome (SC-005).
  - On return, unregister under the lock.

### Documentation for User Story 2

- [X] T031 [P] [US2] Create the whole-file example `sse/example_broker_test.go` (`package sse_test`):
  - `ExampleBroker`, plus a `flushRecorder` type: an `http.ResponseWriter` and `http.Flusher` over a `bytes.Buffer` under a mutex, which signals each flush on a channel;
  - the example runs `b.Serve(w, r, "user:alice")` in a goroutine, waits for the first flush, publishes `{Type: "order.created", Data: `{"id":1}`}` on `user:alice` and waits for the next flush;
  - it cancels the request's context, waits for `Serve` to return, and prints the body with the generated ID masked as `<id>`;
  - the output is deterministic.
- [X] T032 [US2] Fill the README section "Broker" in `sse/README.md`:
  - a notifications route whose topics come from the authenticated user (`auth.FromContext(r.Context())`), never from unchecked query values (FR-031);
  - publishing from domain code;
  - topic naming;
  - the outcomes returned by `Serve`, and logging `ErrSlowConsumer`.

  Add a matching paragraph to `sse/doc.go`.

**Checkpoint**: User Stories 1 and 2 work independently.

---

## Phase 5: User Story 3 - Resume after a reconnection without losing events (Priority: P1)

**Goal**: resolution of `Last-Event-ID` under the registration lock: snapshot, position, reset
event and unresolvable identifiers.

**Independent Test**: clients disconnect at chosen points while events are published, reconnect
with their last identifier, and receive exactly the missed events. Identifiers older than the
history are tested with and without a reset event.

### Tests for User Story 3 ⚠️

- [X] T033 [P] [US3] Write failing end-to-end tests in `sse/resume_test.go` (`package sse_test`):
  1. `TestResume_FromHistory` (scenario 1): `e1`–`e5` received, disconnect, `e6`–`e9` published, reconnect with `Last-Event-ID: e5`. The client receives `e6`–`e9`, then a newly published `e10`, each once.
  2. `TestResume_TopicsOnly` (scenario 2): `e7` was on another topic, so `e6`, `e8` and `e9` arrive.
  3. `TestPosition_NewSubscription` (scenario 3):
     - the history ends with `e9`; a new subscription's first raw lines are `"id: e9"` and `""`;
     - after disconnecting before any event and publishing `e10`, reconnecting with `e9` delivers `e10`;
     - with an empty history, no position is written.
  4. `TestResume_Unresolvable` (scenario 4, FR-035): with an unknown ID, no replay and no reset, the first raw lines are the position of the newest history ID, then only new events arrive.
  5. `TestReset_Sent` (scenario 5):
     - with `WithResetEvent(Event{Type: "reset", ID: "ignored"})` and an unknown ID, the first decoded event has type `reset` and ID equal to the newest history ID, followed by new events;
     - reconnecting with that ID yields no second `reset`.
  6. `TestReset_EmptyHistory`: with an empty history the reset block carries `"id:"`, and the decoded event's ID is `""`.
  7. `TestResume_Restart` (scenario 6): IDs generated by one broker, presented to a new broker that holds events, are unresolvable: position or reset.
  8. `TestResume_HistoryBound` (scenario 7): with `WithHistory(3)` and 10 events, resuming from event 8 yields 9 and 10, and resuming from event 5 is unresolvable.
  9. `TestResume_EmptyHeader`: `Last-Event-ID:` with an empty value behaves as absent.
  10. `TestResume_OtherTopicID`: a `Last-Event-ID` of an event on a topic the client does not follow resumes from its position, with the client's topics only.
  11. `TestResume_AnyPoint` (SC-003, history mode). For seeds 0–9, a seeded SplitMix64 generator defined in the test (Semgrep forbids `math/rand`) assigns each of 10,000 events one of the topics `a`, `b` and `c`. The broker has `WithHistory(10000)`, and the client follows `a` and `b`. The client disconnects after a random number of received events, the remaining events are published, and it reconnects with its last ID. It receives exactly the remaining events on `a` and `b`, in order: 0 duplicates and 0 events on `c`.
- [X] T034 [P] [US7] Write failing tests in `sse/replica_test.go` (`package sse_test`). They are scheduled in this phase because T035 implements the resolution they exercise (Principle III); scenario 2 is covered in T027:
  1. `TestReplica_ResumeAcross` (scenario 1): brokers A and B, each behind its own test server, are fed by the same loop with IDs `bus-1`…`bus-20`. A client on A receives up to `bus-10`, disconnects, and reconnects to B with `bus-10`. It receives `bus-11`…`bus-20` and nothing else.
  2. `TestReplica_GeneratedIDs` (scenario 3): an ID generated by A is unresolvable on B: B sends the position, never a wrong suffix.
  3. `TestReplica_AnyPoint` (SC-003, across brokers): the random scenario of `TestResume_AnyPoint`, with A and B fed the same 10,000 events with IDs `bus-<n>`. The client disconnects from A and reconnects to B.

### Implementation for User Story 3

- [X] T035 [US3] Extend `sse/serve.go` until T033 and T034 pass (research R-11). Under the same lock as the registration, resolve `r.Header.Get("Last-Event-ID")` (empty = absent) into:
  - **absent**: `first` = `appendPosition(latestID)` when the history is not empty;
  - **found**: a snapshot of `history.after(seq, sub.topics)`;
  - **not found** (no replay yet; the replay case comes in T044):
    - with `cfg.reset` set: `first` = the reset event with `ID` replaced by `latestID`, encoded with `appendEvent`. When `latestID == ""`, use an `id:` line with no value, through a variant of `appendEvent` that writes the empty `id` field;
    - otherwise: `first` = `appendPosition(latestID)`, which also covers the empty case.

  Prefix `first` with the retry record when configured. Write the snapshot before entering `live`.

### Documentation for User Story 3

- [X] T036 [P] [US3] Create the whole-file example `sse/example_reset_test.go` (`package sse_test`): `ExampleWithResetEvent`, with the `flushRecorder` helper of T031 copied into the file, since a whole-file example must be self-contained. The broker is built with `WithResetEvent(Event{Type: "reset"})` and holds two events, and the request carries `Last-Event-ID: gone`. The printed body shows the reset event with the newest ID.
- [X] T037 [US3] Fill the README section "Reconnection and resuming" in `sse/README.md`:
  - the timeline of the gap scenario: laptop asleep, 3,000 events published, history of 1,000;
  - resuming from the history;
  - the position sent to new subscriptions;
  - the unresolvable cases;
  - the reset event with the frontend's handling (`es.addEventListener("reset", reloadState)`);
  - why the reset is opt-in.

**Checkpoint**: User Stories 1–3 work independently.

---

## Phase 6: User Story 5 - Shut down without hanging (Priority: P2)

**Goal**: `Close`, usable as the server's shutdown hook. `Serve` and `Publish` after `Close`.

**Independent Test**: many streams on a test server whose shutdown hook closes the broker. A
graceful shutdown completes before its deadline, and every response ends cleanly.

### Tests for User Story 5 ⚠️

- [X] T038 [P] [US5] Write failing end-to-end tests in `sse/close_test.go` (`package sse_test`):
  1. `TestShutdown_Hook` (scenarios 1–2, SC-007): 1,000 subscriptions, each read to the end by its own goroutine, and `Server.RegisterOnShutdown(b.Close)`.
     - `Server.Shutdown(ctx)` with a 30 s deadline returns nil;
     - `time.Since(start) < 30*time.Second` (virtual);
     - all 1,000 readers end with a nil error (a clean end of body);
     - every `Serve` returned `ErrClosed`.
  2. `TestClose_ServeAfter` (scenario 3): after `Close`, a subscription with `WithRetry(2*time.Second)` receives status 200, the event stream headers and exactly the body `"retry: 2000\n\n"`, and `Serve` returns `ErrClosed`.
  3. `TestClose_PublishAfter` (scenario 3): `Publish` returns `ErrClosed`.
  4. `TestClose_Twice` (scenario 4): a second `Close` returns without effect, and concurrent `Close` and `Publish` calls pass `-race`.

### Implementation for User Story 5

- [X] T039 [US5] Implement `Close` in `sse/broker.go`: under the lock, set `closed` once and close `done`. `Publish` already checks `closed` (T029). In `sse/serve.go`, a closed broker found under the lock answers `newStream` with only the retry record as `first`, then returns `ErrClosed`, before registering (research R-13). Make T038 pass.

### Documentation for User Story 5

- [X] T040 [P] [US5] Create the whole-file example `sse/example_close_test.go` (`package sse_test`): `ExampleBroker_Close`, with the `flushRecorder` helper of T031 copied into the file. It shows the documented idiom verbatim:
  1. register `srv.RegisterOnShutdown(b.Close)` on an `http.Server` that never listens;
  2. start `b.Serve(w, r, "news")` in a goroutine and wait for its first flush;
  3. call `srv.Shutdown(context.Background())`, which runs the hook in a goroutine;
  4. wait for `Serve` to return, so that no extra hook is needed, and print its error;
  5. print the error of a later `Publish`.

  The output is `sse: broker closed` twice.
- [X] T041 [US5] Fill the README section "Shutting down" in `sse/README.md`:
  - `RegisterOnShutdown(broker.Close)`, and what happens without it (`Shutdown` waits for its deadline);
  - clients reconnect elsewhere and resume;
  - single streams end on shutdown when their handler selects on its own shutdown channel.

**Checkpoint**: User Stories 1–3 and 5 work independently.

---

## Phase 7: User Story 6 - Recover missed events from a durable message bus (Priority: P2)

**Goal**: `WithReplay` and `ReplayFunc`. Topic filtering by the broker, a handoff without loss or
duplicates, and every outcome of the replay.

**Independent Test**: a replay function over an in-memory list standing in for the bus, a history
that keeps only part of the events, and reconnections with old identifiers while events keep being
published.

### Tests for User Story 6 ⚠️

- [X] T042 [P] [US6] Write failing end-to-end tests in `sse/replay_test.go` (`package sse_test`). The source is a slice of `{Event, topics}` under a mutex. The test's replay function sends every entry after `lastID` and returns `ErrUnknownID` if `lastID` is absent:
  1. `TestReplay_FromSource` (scenario 1): the source holds `e1`–`e5000` (published to the broker too, `WithHistory(1000)`). Reconnecting with `e100` yields `e101`–`e5000`, then a new `e5001`, each once. Use `WithBuffer(64)` and a reader that keeps up.
  2. `TestReplay_ConcurrentPublish` (scenario 2): the replay function, after sending half of its events, publishes three new events to both the source and the broker, then sends the rest including them. Each arrives once.
  3. `TestReplay_NotCalledWhenInHistory` (scenario 3): a counter shows zero calls when the ID is in the history.
  4. `TestReplay_UnknownID` (scenario 4): with `WithResetEvent`, the reset event arrives, then new events. Without it, the position arrives.
  5. `TestReplay_Failure` (scenario 5, FR-042):
     - the function sends `e101`–`e150`, then returns an error;
     - the client receives exactly those 50 events, then a clean end of body, and no live event published meanwhile;
     - `Serve` returns an error wrapping the failure, with no reset event sent;
     - reconnecting with `e150` calls the function with `lastID == "e150"`.
  6. `TestReplay_ClientGone` (scenario 6): the client cancels during the replay. The function observes `ctx.Done()`, `send` returns an error, and `Serve` returns nil.
  7. `TestReplay_TopicFilter` (scenario 7, FR-039): the source has events on other topics and one event on two of the client's topics. Only the client's events arrive, the double-topic one once.
  8. `TestReplay_Overflow` (scenario 8): with `WithBuffer(4)`, 10 live events published during a slow replay make `Serve` return `ErrSlowConsumer`. Reconnecting with the last received ID continues the replay from there.
  9. `TestReplay_InvalidEvent` (FR-043): a sent event with an invalid ID, with no ID, or with no topic makes `send` return `ErrInvalidEvent`, and `Serve` returns an error, as in a failure.
  10. `TestReplay_SkipsLastID`: a source that includes `lastID` itself does not resend it.
  11. `TestReplay_Empty`: the function sends nothing and returns nil, so live events follow.
  12. `TestReplay_Panic` (FR-007): a function that panics after sending one event does not crash the test. The client receives that event, then a clean end of body, and `Serve` returns an error whose message contains `replay panicked`.
  13. `TestReplay_SendAfterReturn` (FR-043): the function stores `send` and returns nil. A later call to the stored `send` returns an error, and the client receives nothing from it.
  14. `TestReplay_AnyPoint` (SC-003, replay mode): the random scenario of `TestResume_AnyPoint` (T033), with `WithHistory(100)` and a replay function over the same 10,000 events, so that most disconnection points need the replay.
- [X] T043 [P] [US6] Write failing end-to-end tests in `sse/interleave_test.go` (`package sse_test`): `TestInterleave_ReplayAndPublish` (SC-004). 1,000 runs, each seeded with the SplitMix64 generator of T033 for seeds 0–999. Each run, inside one `synctest.Test`:
  - a source and a broker share a random prefix of events;
  - the replay function yields at random points (`runtime.Gosched` or `time.Sleep(time.Millisecond)`) while a publisher goroutine adds events to both at random points;
  - after quiescence, the client's received IDs equal the expected suffix exactly: no loss, no duplicate, in order.

  To keep the run time reasonable, run the HTTP/1.1 protocol only, and mark the test as skipped under `testing.Short()`.

### Implementation for User Story 6

- [X] T044 [US6] Implement `sse/replay.go` and its use in `sse/serve.go` until T042 and T043 pass (research R-12):
  - `var ErrUnknownID = errors.New("sse: unknown event identifier")`;
  - a replay state with:
    - a bounded set of the last `cfg.buffer` replayed IDs (a ring plus a map);
    - `send(ev Event, topics ...string) error`, which validates (FR-009, non-empty ID, at least one non-empty topic), skips `ev.ID == lastID`, skips events with no topic of the subscription, writes the event, and remembers its ID;
    - a stop error when the request context is done, the subscriber has overflowed, or a write failed.
  - `send` returns an error and writes nothing once the replay function has returned: a flag set under the stream's mutex when the call ends.
  - The call to the replay function runs under a deferred `recover`. A panic becomes the failure `fmt.Errorf("sse: replay panicked: %v", v)` (FR-007).
  - In `Serve`, when the ID is not found and `cfg.replay` is set: open the stream with only the retry record, call `cfg.replay(r.Context(), lastID, sub.topics, send)`, then act on the outcome:
    - nil: drain the queue, skipping remembered IDs, then `live`;
    - `errors.Is(err, ErrUnknownID)`: write the reset event or the position computed at registration, then `live`;
    - overflow: `ErrSlowConsumer`;
    - client gone: nil;
    - otherwise: return `fmt.Errorf("sse: replay: %w", err)` without live events.

### Documentation for User Story 6

- [X] T045 [P] [US6] Create the whole-file example `sse/example_replay_test.go` (`package sse_test`): `ExampleWithReplay`, with an in-memory `source` type (a slice of `{Event, topics}`) and the `flushRecorder` helper. The broker has `WithHistory(1)`, and the request carries `Last-Event-ID: 1`. The printed body shows events 2 and 3 from the source.
- [X] T046 [US6] Fill the README section "Replay from a durable bus" in `sse/README.md`:
  - when the replay function is called (memory first);
  - the `send` callback with each event's topics, and why the broker filters (FR-039);
  - `ErrUnknownID` against an empty replay;
  - failures and the retry on reconnection, including a panicking function;
  - no keep-alive while the function runs, so it should return within the idle timeout of proxies (FR-029);
  - the ordering assumption and its worst case (a duplicate, never a loss);
  - a Redis Streams `XRANGE` sketch, marked "go-redis v9, sketch".

**Checkpoint**: User Stories 1–3, 5 and 6 work independently.

---

## Phase 8: User Story 7 - Run behind several replicas (Priority: P2)

**Goal**: the guidance for message buses. The behavior of this story was tested before it was
implemented: duplicates in T027 (scenario 2), and resuming across brokers in T034 (scenarios 1
and 3), which precede T029 and T035 (Principle III).

**Independent Test**: `go test -race -run 'Replica|DuplicateNotDelivered' ./...` from `sse/`. Two
brokers fed with the same events and identifiers, a client that moves from one to the other, and
events re-delivered to one instance.

### Documentation for User Story 7

- [X] T047 [US7] Fill the README section "Several replicas" in `sse/README.md` (FR-063):
  - the architecture: the domain code publishes to the bus, and a feed loop in each replica calls `broker.Publish` with the bus message ID, ignoring `ErrDuplicateID`;
  - a table with one row per system: Redis Streams, Redis Pub/Sub, Kafka, RabbitMQ classic queues, RabbitMQ Streams. Columns: "every replica receives every message (how)", "stable ID and common order", "replay";
  - the consumer-group pitfall;
  - the single Kafka partition;
  - short sketches as code blocks for Redis Streams (go-redis v9), Kafka (franz-go) and RabbitMQ (amqp091-go fanout exchange with an exclusive queue), each labeled with the library and "sketch, adapt to your client version". They must not be compiled examples (FR-002).

**Checkpoint**: User Stories 1–3 and 5–7 work independently.

---

## Phase 9: User Story 8 - Consume event streams from Go (Priority: P2)

**Goal**: `NewClient` and `Events`, reconnecting as browsers do.

**Independent Test**: scripted test servers with dropped connections, `retry` values, error
statuses and malformed input. The events and outcomes the client reports are compared with the
script.

### Tests for User Story 8 ⚠️

- [X] T048 [P] [US8] Write failing end-to-end tests in `sse/client_test.go` (`package sse_test`). Handlers count attempts and record the request headers. The client is built inside the bubble with `WithHTTPClient(e.Client)`:
  1. `TestEvents_Basic` (scenario 1): IDs, types and data are yielded.
  2. `TestEvents_Reconnect` (scenario 2):
     - attempt 1 sends `e1`–`e3`, then returns (a normal end);
     - attempt 2 sees `Last-Event-ID: e3` and sends `e4`;
     - the consumer sees `e1`–`e4` and no error;
     - attempt 2 starts 3 s (virtual) after attempt 1 ended.
  3. `TestEvents_Retry` (scenario 3): the server sends `retry: 10000`, and the next attempt starts 10 s later.
  4. `TestEvents_Statuses` (scenario 4):
     - 204 ends without an error and makes one attempt;
     - 401 yields `*ResponseError{StatusCode: 401}` (`errors.As`) and makes one attempt;
     - 200 with `text/html` yields `*ResponseError{StatusCode: 200, ContentType: "text/html"}`.
  5. `TestEvents_RepeatRequest` (scenario 5): a POST with an `Authorization` header and a `strings.NewReader` body (which has `GetBody`). Every attempt carries the same header and body.
  6. `TestEvents_BodyNotReplayable` (scenario 6): a body without `GetBody` ends the iteration with an error and zero attempts.
  7. `TestEvents_Cancel` (scenario 7):
     - cancelling the context mid-stream yields `context.Canceled` once and stops;
     - breaking out of the loop closes the body: the handler's request context ends.
  8. `TestEvents_Position`: attempt 1 sends only the position `id: K-7`, then ends. Attempt 2 carries `Last-Event-ID: K-7`.
  9. `TestEvents_Headers`: `Accept: text/event-stream` is added when absent, and kept when the developer set another `Accept`. A `Last-Event-ID` set on the initial request is sent on attempt 1.
  10. `TestEvents_Backoff`: a `RoundTripper` that fails the first 6 attempts with a network error. The gaps between attempts are 3, 6, 12, 24, 30 and 30 s. After a successful attempt that later drops, the next gap is 3 s again.
  11. `TestEvents_TooLarge`: with `WithMaxEventSize(16)`, a 32-byte event ends the iteration with `ErrEventTooLarge` and no reconnection.
  12. `TestEvents_Redirect`: `/a` redirects to `/b`, and the request to `/b` carries the same `Last-Event-ID` on reconnection.
  13. `TestClient_Options`: `NewClient(WithHTTPClient(nil))` and `NewClient(WithHistory(1))` return errors wrapping `ErrInvalidConfig`.
  14. `TestClient_Concurrent` (FR-005): two `Events` iterations on the same `Client` run concurrently against two handlers. Each receives exactly its own events, its own `Last-Event-ID` on reconnection, and its own `retry` delay, under `-race`.

### Implementation for User Story 8

- [X] T049 [US8] Implement `sse/client.go` until T048 passes (research R-15):
  - `type ResponseError struct{ StatusCode int; ContentType string }` with `Error()`, giving for example `sse: unexpected response: status 401, content type "text/plain"`;
  - `type Client struct` and `NewClient` (`clientOptions`);
  - `Events(req)` returns an iterator that:
    - checks `req.Body != nil && req.GetBody == nil` first;
    - loops: clone the request with its context, set the body from `GetBody`, `Accept` and `Last-Event-ID`; `Do`;
    - classifies the answer: 204 ends; another status, or a media type other than `text/event-stream` (`mime.ParseMediaType`), yields `*ResponseError`; otherwise streams through `newParser`, yields events, and tracks `lastID` and `retry`;
    - on a network error or a clean end, waits on a `time.Timer` in a `select` with `req.Context().Done()`;
    - doubles the delay after each consecutive failure before a 200, capped at `max(maxBackoff, retry)`, and resets it after a 200.
  - The body is closed on every exit path, including when `yield` returns false.

### Documentation for User Story 8

- [X] T050 [P] [US8] Create the whole-file example `sse/example_client_test.go` (`package sse_test`): `ExampleClient`, with a `cannedTransport` `RoundTripper`:
  - the first request gets `retry: 1\nid: 1\ndata: first\n\n`;
  - the second must carry `Last-Event-ID: 1` and gets `data: second\n\n`;
  - the third gets 204.

  The example prints `first` and `second`. Also add `ExampleDecode` to `sse/example_test.go`, decoding a string with two events and printing their fields.
- [X] T051 [US8] Fill the README section "Consuming from Go" in `sse/README.md`:
  - the iteration loop;
  - what is retried and what is not;
  - retrying deliberately on `*ResponseError` with status 503;
  - `WithHTTPClient` without an overall `Timeout`;
  - `Decode` for one-shot parsing.

**Checkpoint**: all eight stories' code is complete.

---

## Phase 10: User Story 4 - Integrate into an existing server by following the documentation (Priority: P1)

**Goal**: the cross-cutting documentation that ties the flows together (FR-060 to FR-065).

**Independent Test**: a reviewer who did not take part in the design adds a notification stream
and a job progress stream to a sample server, using this repository's middleware and session
authentication, by reading only the README and `go doc`. Every example passes.

- [X] T052 [US4] Fill the README section "Integrating into an existing server" in `sse/README.md` (FR-061):
  - a complete listing:
    - an existing `middleware.New(rid, alog, rec, compressMW, authMW)` chain;
    - `maxconcurrency` applied to the API routes only;
    - the `GET /events` route calling `broker.Serve` with topics from `auth.FromContext`;
    - a `POST /orders` handler that publishes;
    - `http.Server` with `WriteTimeout` kept;
    - `srv.RegisterOnShutdown(broker.Close)`;
  - the compatibility table of [contracts/http-semantics.md](contracts/http-semantics.md) "Middleware and authentication";
  - the notes on `http.TimeoutHandler` and on HTTP clients with an overall `Timeout`.
- [X] T053 [US4] Fill the README section "Frontend notes" in `sse/README.md` (FR-062):
  - `new EventSource("/events")` with `addEventListener` for named events;
  - handling `reset` by reloading state;
  - `withCredentials` across origins, with the CORS middleware;
  - 401 and 403 stop reconnection, so the frontend re-authenticates on `readyState === EventSource.CLOSED`;
  - the HTTP/1.1 limit of six connections per domain, and why HTTP/2 matters.
- [X] T054 [US4] Fill the README sections "Building blocks", "Testing handlers" (FR-064) and "Defaults and limits" in `sse/README.md`:
  - building blocks: when to use a stream, a broker, a replay, a client;
  - testing: publishing as soon as `Do` returns; decoding with `Decode`; `synctest` and an in-memory network instead of sleeps and sockets; a short sketch;
  - defaults: the table of research R-20, plus memory, which is roughly history × event size, plus buffer × event size per subscriber.
- [X] T055 [US4] Expand `sse/doc.go` into the full package documentation: the three building blocks; each flow of FR-060 in a few sentences, with links to the examples (`[ExampleStream]` and so on); the context interpretation (research R-08); and the defaults. Check the result with `go doc github.com/paccolamano/go/sse`.
- [X] T056 [US4] Review `sse/README.md`, `sse/doc.go` and the `sse/example*_test.go` files against FR-060 to FR-065 and SC-010. Every listed flow has its own README section with a code example. Every capability of FR-065 has a runnable example (`go test -run Example ./...` passes). The README matches the API of [contracts/public-api.md](contracts/public-api.md) exactly. Fix any gap.

**Checkpoint**: User Story 4 is complete. The module is documented end to end.

---

## Phase 11: Polish & Cross-Cutting Concerns

**Purpose**: fuzzing, benchmarks, release notes and the full gates.

- [X] T057 [P] Create `sse/bench_test.go` (`package sse_test`):
  - `BenchmarkPublish`, with 1,000 subscribers on one topic. There is no HTTP and no `synctest`, which benchmarks cannot use:
    - each subscriber is a goroutine running `b.Serve(w, r, "bench")`;
    - `w` is a `discardWriter` (`Header`, `Write` and `WriteHeader` that discard, a `Flush` that signals its first call on a channel); it does not support write deadlines, which the stream tolerates;
    - `r` is an `httptest.NewRequest` with a cancelable context;
    - the benchmark waits for every first flush, so that all subscriptions are registered, then publishes one 100-byte event per `b.Loop()` iteration;
    - at the end it cancels the contexts and waits for every `Serve` to return;
  - `BenchmarkEncode`, for a 1 KiB three-line event;
  - `BenchmarkDecode`, for 100 events.

  Use `b.Loop()` and `b.ReportAllocs()`.
- [X] T058 Fill `sse/CHANGELOG.md` under `## [Unreleased]`, with an "Added" list covering `Stream`, `Broker` (topics, history, position, reset event, replay, `Close`), `Client`, `Decode`, the options and the errors. As for the earlier modules, the entries move to `## [0.1.0] - <date>` in the separate release pull request (`chore(sse): prepare release 0.1.0`).
- [X] T059 Run `make check MODULE=sse` for the module in `sse/`. Fix every golangci-lint finding without `//nolint`, and raise coverage to ≥ 80% with tests for the uncovered branches. If the PR scanners later report a Semgrep match on the stream's `Write`, suppress it with `// nosemgrep: <rule> -- text/event-stream is never rendered as HTML` (research R-18).
- [X] T060 Run the structure, tidy and cross-build checks of [quickstart.md §2](quickstart.md): `scripts/structure-check.sh sse`, `go mod tidy -diff`, and `go vet` for linux, darwin and windows on amd64 and arm64.
- [X] T061 Run [quickstart.md](quickstart.md) sections 3–5 and 7. Record the benchmark numbers for the pull request description. Section 6, the manual check with `curl` and a browser, is recommended before release.

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: no dependencies.
- **Foundational (Phase 2)**: depends on Setup and blocks every story. T015 depends on T007 and
  T011. T016 depends on T015, because its helpers use `Decode`.
- **US1 (Phase 3)**: depends on Foundational.
- **US2 (Phase 4)**: depends on US1, because `Serve` reuses `newStream` from T020.
- **US3 (Phase 5)**: depends on US2 (`Serve`, the history).
- **US5 (Phase 6)**: depends on US2. It can run in parallel with US3, but both edit
  `sse/serve.go`, so sequence T035 and T039.
- **US6 (Phase 7)**: depends on US3 (resolution, reset, position).
- **US7 (Phase 8)**: documentation only. Its tests are written in Phase 4 (T027, scenario 2) and
  Phase 5 (T034, scenarios 1 and 3), before the code they exercise.
- **US8 (Phase 9)**: depends on Foundational only (the decoder). It can run in parallel with
  Phases 3–8.
- **US4 (Phase 10)**: depends on every other story, because it documents all the flows.
- **Polish (Phase 11)**: depends on everything else.

### User Story Dependencies

```text
Foundational ─▶ US1 ─▶ US2 ─┬─▶ US3 ─┬─▶ US6
                            │        └─▶ US7
                            └─▶ US5
Foundational ─▶ US8
US1…US8 ─▶ US4 ─▶ Polish
```

### Within Each User Story

- Tests are written first and fail, then the implementation, then the example and the README
  section.
- Files shared across stories (`sse/serve.go`, `sse/broker.go`, `sse/README.md`,
  `sse/example_test.go`) are edited by one task at a time.

### Parallel Opportunities

- Phase 1: T002 and T003.
- Phase 2: T006, T008, T010, T012, T013 and T014 (tests in different files). T016 once T015 is
  done.
- US1: T017, T018 and T019. T021 after T020.
- US3: T033 and T034.
- US2: T023, T024, T026, T027 and T028 (tests). T031 after T030.
- US6: T042 and T043.
- US8 as a whole, in parallel with US1–US7 once Foundational is done.

---

## Parallel Example: User Story 2

```bash
# Write all the failing tests of User Story 2 together:
Task: "TestID_Format in sse/id_internal_test.go"
Task: "TestHistory_* in sse/history_internal_test.go"
Task: "TestPublish_* and TestBroker_Options in sse/broker_test.go"
Task: "TestServe_* in sse/serve_test.go"
Task: "TestServe_StalledSubscribers in sse/stalled_test.go"

# Then the implementation, in order:
Task: "id.go and history.go (T025)"
Task: "broker.go (T029)"
Task: "live path of serve.go (T030)"
```

## Parallel Example: Client alongside the broker

```bash
# Once Phase 2 is done, one developer takes US1 → US2 → US3 while another takes US8:
Task: "T048 client tests in sse/client_test.go"
Task: "T049 client implementation in sse/client.go"
```

---

## Implementation Strategy

### MVP First (User Story 1 Only)

1. Complete Phase 1 (Setup) and Phase 2 (Foundational).
2. Complete Phase 3 (User Story 1): `NewStream`, `Send` and `KeepAlive`, with headers, encoding and
   deadlines.
3. **Stop and validate**: run `go test -race -run 'Stream|Encode|Decode|Conformance' ./...` from
   `sse/`.

### Incremental Delivery

1. Setup and Foundational: options, encoding, decoding, the test network.
2. US1: single streams (MVP).
3. US2: broker, live delivery.
4. US3: resuming, position, reset.
5. US5: shutdown.
6. US6: replay.
7. US7: replicas.
8. US8: client.
9. US4: the documentation that ties the flows together.
10. Polish: fuzzing, benchmarks, CHANGELOG, gates.

Each step keeps every earlier test green. The module is released once, as `sse/v0.1.0`, after
Phase 11.

---

## Notes

- Tasks marked [P] touch different files and have no dependency on an incomplete task.
- Story labels map tasks to spec.md user stories for traceability.
- Verify that each test fails before implementing it.
- The PR title is `feat(sse): add server-sent events module`, and the squash commit uses it.
- Avoid vague tasks, edits to the same file from two parallel tasks, and dependencies between
  stories that break their independence.

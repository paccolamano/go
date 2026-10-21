# Implementation Plan: Server-Sent Events (sse)

**Branch**: `008-server-sent-events` (suggested; work currently on `main`) | **Date**: 2026-10-21 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `specs/008-server-sent-events/spec.md`

**Note**: This template is filled in by the `/speckit-plan` command; its definition describes the execution workflow.

## Summary

This feature adds a new independent module, `github.com/paccolamano/go/sse`, with one package
that implements both sides of server-sent events, as defined by the WHATWG HTML Living Standard:

- **Stream**: `NewStream(w, r)` sets the headers and flushes. `Send(ev)` and `KeepAlive()` write
  one block each. Events are validated, and data is split into data lines.
- **Broker**: `Publish(ev, topics...)` never blocks. `Serve(w, r, topics...)` streams the events
  on the given topics, once each.
  - **Resuming**: an in-memory history serves `Last-Event-ID`. A new subscription receives a
    position, so that it can resume even before its first event.
  - **Gaps**: an opt-in reset event signals missed events that cannot be recovered.
  - **Replay**: an optional `ReplayFunc` reads missed events from the developer's durable bus.
    The broker filters by topic, and the switch to live events has no loss and no duplicates.
  - **Slow clients** are disconnected, then resume from the history.
  - **Shutdown**: `Close()` ends every stream normally and fits `srv.RegisterOnShutdown`.
- **Client**: `Decode(r)` follows the standard's parsing model. `Client.Events(req)` reconnects
  with `Last-Event-ID`, honors `retry`, stops on 204 and fails on other statuses.
- **Documentation**: every flow is documented in the README and the package docs, with runnable
  examples (User Story 4). This covers the integration into an existing server with this
  repository's middleware and `auth`, and several replicas fed by Redis, Kafka or RabbitMQ.

Design decisions validated by a spike ([research.md](research.md)), over HTTP/1.1 and
unencrypted HTTP/2:

- **Write deadlines** (R-07): set before each write and cleared after it. A stream survives 60 s
  of idleness under a 10 s server `WriteTimeout`, and a stalled client fails a write after exactly
  10 s. The control without deadline management dies.
- **Shutdown** (R-13): with `RegisterOnShutdown(b.Close)`, `Shutdown` returned in about 1 ms with
  50 streams, all ending cleanly. Without it, it waited the full 30 s.
- **Flush detection** (R-06): the `Unwrap` chain detects writers that cannot flush, such as
  `http.TimeoutHandler`, without writing anything.
- **Middleware** (R-19): behind `accesslog`, `compress` and `etag`, every event arrives at once,
  gzip-compressed and without an `ETag`.
- **Tests** (R-16): `http.Server` and `http.Transport` over an in-memory `net.Pipe` network run
  inside `testing/synctest`, with no socket and no real waiting.

Two refinements came out of the research:

- the replay function takes a `send(ev, topics...)` callback instead of returning an iterator,
  because it must provide each event's topics (Q1: B, R-12);
- FR-035 now sends the position when an unresolvable identifier meets no reset event (R-11; spec
  and checklist updated, Iteration 3).

## Technical Context

**Language/Version**: Go. The `go` directive is `go 1.26.0`, the oldest version the
constitution supports, and CI also tests on 1.27. Features used:

- `testing/synctest` and `sync.WaitGroup.Go` (1.25);
- `crypto/rand.Text`, `testing.B.Loop` and `net/http.Protocols` in tests (1.24);
- `iter` (1.23);
- `http.ResponseController` (1.20).

**Primary Dependencies**: the standard library only, with no third-party module and no other
module of this repository, tests and examples included. Packages: `bufio`, `context`,
`crypto/rand`, `errors`, `fmt`, `io`, `iter`, `mime`, `net/http`, `strconv`, `strings`, `sync`,
`time`, `unicode/utf8` ([research.md R-02](research.md#r-02-dependencies-principle-v)).

**Storage**: none. The history, the subscriber queues and the decoder buffers are bounded,
in-memory structures. Durable storage is the developer's bus, reached through `ReplayFunc`.

**Testing**: the standard `testing` package.

- External test package `sse_test`, and internal tests for the history ring, the identifier
  format and the UTF-8 replacement.
- End-to-end tests run `http.Server` and `http.Transport` over an in-memory `net.Pipe` network
  inside `synctest`, each over HTTP/1.1 and over unencrypted HTTP/2.
- Conformance vectors from the standard and from Unicode Table 3-8.
- 1,000 seeded interleavings for the replay handoff.
- `FuzzDecode`, `FuzzRoundTrip` and `FuzzValidate`.
- Informative benchmarks.
- Whole-file runnable examples.

Details: [research.md R-16](research.md#r-16-testing-approach-principle-iii).

**Target Platform**: any platform Go supports. The code is pure Go with no build constraints,
and builds for linux, darwin and windows on amd64 and arm64.

**Project Type**: library: one Go module (`sse/go.mod`) with one package, `sse`.

**Performance Goals**: no code is declared performance-sensitive. Publishing costs time
proportional to the subscribers of its topics, and never waits for them (FR-020, SC-005).
`BenchmarkPublish` (1,000 subscribers), `BenchmarkEncode` and `BenchmarkDecode` are recorded for
reference.

**Constraints**:

- No goroutine started by the module (FR-003).
- No package-level mutable state: the only package-level variables are the eight `Err…`
  sentinels, and defaults are `const`.
- No `init`, no `panic`, no output and no logging. Errors are returned for the developer's
  handler to log.
- Memory is bounded by the history size, `WithBuffer` per subscriber, and `WithMaxEventSize` per
  decoder.

**Scale/Scope**: about 1,300 lines of library code in about 12 files. The API has 35 exported
identifiers ([contracts/public-api.md](contracts/public-api.md)).

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

| Rule | Assessment | Status |
|------|------------|--------|
| **I. Independent modules** | One new top-level module `sse/`, with module path `github.com/paccolamano/go/sse`, its own `go.mod`, a README.md and a CHANGELOG.md. It has no `replace` and no dependency on other repository modules, and builds and tests with `GOWORK=off`. Its single purpose is server-sent events, and the name describes the content. | ✅ |
| **II. SemVer & stable API** | Tags are `sse/vX.Y.Z`, starting at v0.1.0. The exported surface maps to FRs ([public-api.md](contracts/public-api.md), traceability table). Every constructor takes `...Option`, so options can be added compatibly. `ReplayFunc` is a function type, not an interface, so it cannot gain methods that would break implementers. Every change goes into the CHANGELOG. | ✅ |
| **III. Test-first** | `tasks.md` will order test tasks before implementation tasks. The suite uses the standard `testing` package with table-driven subtests. It is deterministic and hermetic: `synctest` virtualizes time, including `net/http` deadlines; the network is an in-memory `net.Pipe`; randomized tests use fixed seeds. The decoder parses external input, so it has fuzz tests (`FuzzDecode`, `FuzzRoundTrip`, `FuzzValidate`). Every primary API has an `Example`. Coverage target: ≥ 80%. | ✅ |
| **IV. Idiomatic Go** | The repository's golangci-lint config applies unchanged. No `nolint` is expected (R-18). Every exported identifier and the package are documented. Errors are wrapped with `%w`, and branchable conditions are sentinels or `*ResponseError` (R-04). There is no `panic`, `init` or global mutable state, and the module writes and logs nothing on its own. It starts no goroutine: streams run in their request's goroutine and the client in the consumer's. **Deviation**: blocking operations take their context from the `*http.Request` instead of a first `ctx` parameter (Complexity Tracking). | ✅ (one recorded deviation) |
| **V. Simplicity & dependencies** | No third-party dependencies. One package, one `Option` type with applicability checks (as in `slogx`), one mutex in the broker, and no interfaces of its own. Rejected as speculative: a store interface (spec Clarifications), bus adapters, sharded locks, and a goroutine per subscriber. No cgo. | ✅ |
| Go toolchain support | `go 1.26.0` is not above `GO_VERSION_OLDEST=1.26`, and every feature used exists in 1.26 (Technical Context). | ✅ |
| Platforms | The code is pure Go with no OS-specific calls. There is no `.module-exceptions` entry. | ✅ |
| Repository layout | No tooling change. `make work` adds `./sse` to `go.work`, and the shared lint config is used without overrides. | ✅ |
| Dependency hygiene | With no `require` list, `go mod tidy -diff` is clean. | ✅ |
| Vulnerability scanning / Secrets | Trivy finds no dependencies to scan. Fixtures use neutral strings, and no credential-like literals. A possible Semgrep match on writing to a `ResponseWriter` would be suppressed with `nosemgrep` and the `text/event-stream` justification (R-18). | ✅ |
| Local and PR gates | `make check MODULE=sse` runs lint, then race and shuffle tests with the coverage gate. The tooling discovers the new module on its own. | ✅ |
| Commits / Release | The PR title is `feat(sse): add server-sent events module`. The release is `sse/v0.1.0`, with a CHANGELOG section. | ✅ |

**Result (pre-research)**: PASS, with one deviation from the letter of Principle IV, agreed in the
spec's Clarifications and recorded below. After `/speckit-analyze` (finding C1), the user confirmed
the deviation. The constitution amendment that codifies the exception comes in a separate pull
request.

**Result (post-design re-check)**: PASS.

- The data model and the four contracts add no dependency, no global state, no goroutine and no
  module exception.
- Every exported identifier maps to a functional requirement.
- The only deviation remains the context one.

## Project Structure

### Documentation (this feature)

```text
specs/008-server-sent-events/
├── spec.md                    # feature specification
├── plan.md                    # this file
├── research.md                # Phase 0: decisions R-01 … R-20, with spike results
├── data-model.md              # Phase 1: event, configuration, broker state, state machines
├── quickstart.md              # Phase 1: validation guide
├── contracts/
│   ├── public-api.md          # complete exported Go API, with FR traceability
│   ├── wire-format.md         # encoding, decoding, conformance vectors, round trip
│   ├── http-semantics.md      # headers, deadlines, shutdown, middleware, client statuses
│   └── broker-semantics.md    # publishing, resuming, replay handoff, closing
├── checklists/
│   └── requirements.md        # spec quality checklist
└── tasks.md                   # Phase 2 (/speckit-tasks, not created here)
```

### Source Code (repository root)

```text
go.work                          # + ./sse (make work)

sse/
├── go.mod                       # module github.com/paccolamano/go/sse; go 1.26.0; no require
├── README.md                    # every flow (research R-17): stream, broker, integration,
│                                # resuming and reset, replay, replicas and buses, frontend,
│                                # client, testing, defaults
├── CHANGELOG.md                 # Keep a Changelog; [Unreleased] → [0.1.0]
├── doc.go                       # package doc: the flows in short form, links to examples
├── options.go                   # Option, With*, ErrInvalidConfig, config, applicability
├── event.go                     # Event, ErrInvalidEvent, validation, encoding
├── stream.go                    # Stream, NewStream, Send, KeepAlive, ErrStreamingUnsupported,
│                                # flush detection, write deadlines
├── broker.go                    # Broker, NewBroker, Publish, Close, ErrClosed, ErrDuplicateID,
│                                # topic index
├── history.go                   # history ring and identifier index
├── id.go                        # generated identifiers
├── serve.go                     # Serve, subscriber, resolution, live loop, ErrSlowConsumer
├── replay.go                    # ReplayFunc, ErrUnknownID, send, handoff
├── decode.go                    # Decode, ErrEventTooLarge, the parser
├── utf8.go                      # maximal-subpart replacement
├── client.go                    # Client, NewClient, Events, ResponseError, reconnection
│
├── pipenet_test.go              # in-memory listener over net.Pipe; HTTP/1.1 and h2c helpers
├── options_internal_test.go     # defaults, validation, applicability, error format (FR-004)
├── event_internal_test.go       # validation and encoding table (FR-009, FR-010)
├── utf8_internal_test.go        # Unicode Table 3-8
├── decode_test.go               # wire-format vectors, CR split, BOM, size limit
├── decode_internal_test.go      # retry parsing, position blocks
├── fuzz_test.go                 # FuzzDecode, FuzzValidate
├── fuzz_internal_test.go        # FuzzRoundTrip (SC-001)
├── stream_test.go               # US1 with httptest.ResponseRecorder
├── stream_e2e_test.go           # US1 end to end: idle beyond WriteTimeout, stalled client
├── id_internal_test.go          # identifier format and prefix
├── history_internal_test.go     # ring bound and index consistency (SC-006)
├── broker_test.go               # US2: Publish validation, duplicates, options
├── serve_test.go                # US2: topics, exactly once, keep-alive, order, duplicates (US7)
├── stalled_test.go              # SC-005: 999 stalled subscribers
├── resume_test.go               # US3: history, position, reset, any-point resume (SC-003)
├── replica_test.go              # US7: resume across brokers, generated IDs (SC-003)
├── close_test.go                # US5: shutdown hook, Serve and Publish after Close
├── replay_test.go               # US6: handoff, filtering, outcomes, panic (SC-003)
├── interleave_test.go           # SC-004: 1,000 seeded interleavings
├── client_test.go               # US8, including concurrent iterations
├── bench_test.go                # BenchmarkPublish, BenchmarkEncode, BenchmarkDecode
├── example_test.go              # ExampleDecode
├── example_stream_test.go       # whole-file ExampleStream
├── example_broker_test.go       # whole-file ExampleBroker
├── example_reset_test.go        # whole-file ExampleWithResetEvent
├── example_close_test.go        # whole-file ExampleBroker_Close
├── example_replay_test.go       # whole-file ExampleWithReplay
└── example_client_test.go       # whole-file ExampleClient
```

**Structure Decision**: the feature is one new top-level module, `sse/`, laid out as above.

- **One package**: the server and the client share `Event` and the wire format (R-01).
- **Files** are split by capability. Each has its own test file, plus cross-cutting files for
  interleavings, fuzzing, benchmarks and examples.
- **Whole-file examples** show their small helpers in the documentation: a recorder that signals
  flushes, and an in-memory `RoundTripper` (R-17).
- **Nothing else changes** in the repository except `go.work`, which `make work` regenerates.

## Complexity Tracking

| Violation | Why Needed | Simpler Alternative Rejected Because |
|-----------|------------|-------------------------------------|
| Principle IV, "functions that perform I/O or may block MUST accept a `context.Context` as their first parameter": `Stream.Send`, `Stream.KeepAlive`, `Broker.Serve` and `Client.Events` take their context from the `*http.Request`, and `Stream` keeps it. `Decode` reads an `io.Reader` without a context. | The request already carries the context, which `net/http` cancels when the client leaves. A second parameter could disagree with it. This matches `http.ServeContent` and `http.Client.Do`, and `encoding/json.Decoder` for `Decode`, whose reader's cancellation is that of the HTTP body. Agreed with the user (spec Clarifications, Session 2026-10-21), and confirmed after `/speckit-analyze` (C1). | `Send(ctx, ev)`, `Serve(ctx, w, r, …)`, `Events(ctx, req)`: redundant with the request, and a context unrelated to it would keep writing after the client has left. |

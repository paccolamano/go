# Contract: Public Go API

**Module**: `github.com/paccolamano/go/sse` | **Package**: `sse` | **Minimum Go**: 1.26

This file lists the complete exported surface of the module at its first release. Anything not
listed here is unexported. Later releases may only **add** identifiers, options or fields
(Principle II). The exact doc-comment wording is decided at implementation time. Behavior is
specified in [wire-format.md](wire-format.md), [http-semantics.md](http-semantics.md) and
[broker-semantics.md](broker-semantics.md).

## Events (FR-008 to FR-011)

```go
// Event is a server-sent event. ID and Type are optional. Data may contain line
// breaks; each line is sent as its own data line, and clients receive line feeds.
type Event struct {
	ID   string // valid UTF-8, without NUL, CR or LF
	Type string // the "event" field; empty means "message"; valid UTF-8, without CR or LF
	Data string // valid UTF-8
}
```

## Errors (research R-04)

```go
var (
	ErrInvalidConfig        = errors.New("sse: invalid configuration")
	ErrInvalidEvent         = errors.New("sse: invalid event")
	ErrStreamingUnsupported = errors.New("sse: response writer cannot flush")
	ErrSlowConsumer         = errors.New("sse: subscriber too slow")
	ErrClosed               = errors.New("sse: broker closed")
	ErrDuplicateID          = errors.New("sse: duplicate event identifier")
	ErrUnknownID            = errors.New("sse: unknown event identifier")
	ErrEventTooLarge        = errors.New("sse: event too large")
)

// ResponseError reports an answer that does not start an event stream: a status
// other than 200 and 204, or status 200 with another content type. The client does
// not reconnect after it.
type ResponseError struct {
	StatusCode  int
	ContentType string
}

func (e *ResponseError) Error() string
```

## Options (research R-03)

```go
// Option configures a stream, a broker, a client or Decode. Each one accepts the
// options listed below; any other option makes it return an error wrapping
// ErrInvalidConfig.
type Option func(*config)

func WithRetry(d time.Duration) Option        // FR-016, FR-030; ≥ 1 ms, sent in whole ms
func WithWriteTimeout(d time.Duration) Option // FR-015, FR-030; default 10s
func WithKeepAlive(d time.Duration) Option    // FR-029; default 15s
func WithHistory(n int) Option                // FR-032; default 1000
func WithBuffer(n int) Option                 // FR-028; default 64
func WithResetEvent(ev Event) Option          // FR-035, FR-036; disabled by default; ev.ID is ignored
func WithReplay(fn ReplayFunc) Option         // FR-038 to FR-045
func WithHTTPClient(c *http.Client) Option    // FR-059; default http.DefaultClient
func WithMaxEventSize(n int) Option           // FR-051; bytes, default 1 MiB
```

| Constructor | Accepted |
|-------------|----------|
| `NewStream` | `WithRetry`, `WithWriteTimeout` |
| `NewBroker` | `WithRetry`, `WithWriteTimeout`, `WithKeepAlive`, `WithHistory`, `WithBuffer`, `WithResetEvent`, `WithReplay` |
| `NewClient` | `WithHTTPClient`, `WithMaxEventSize` |
| `Decode` | `WithMaxEventSize` |

The following also return errors wrapping `ErrInvalidConfig`:

- non-positive durations and sizes, or a retry under 1 ms;
- a nil function or HTTP client;
- a reset event whose type or data is invalid.

Messages have the form `sse: invalid configuration: <Constructor>: <Option>: <reason>`.

## Stream (User Story 1, FR-012 to FR-019)

```go
// Stream writes events to one HTTP response. It is safe for concurrent use. It
// uses the request's context: once it ends, sends fail without writing.
type Stream struct{ /* unexported */ }

// NewStream sets the event stream headers, sends status 200 and flushes. When the
// writer cannot flush it returns ErrStreamingUnsupported and writes nothing.
func NewStream(w http.ResponseWriter, r *http.Request, opts ...Option) (*Stream, error)

// Send writes and flushes one event. It returns ErrInvalidEvent, writing nothing,
// for an invalid event. After a failed write, every call returns that error.
func (s *Stream) Send(ev Event) error

// KeepAlive writes a comment line that clients ignore.
func (s *Stream) KeepAlive() error
```

## Broker (User Stories 2, 3, 5, 6, 7, FR-020 to FR-048)

```go
// Broker delivers published events to subscribed requests by topic, keeps a
// history to resume after reconnections, and can be closed on shutdown. It starts
// no goroutine and is safe for concurrent use.
type Broker struct{ /* unexported */ }

func NewBroker(opts ...Option) (*Broker, error)

// Publish records ev in the history and offers it to the subscribers of any of
// topics, once each. It never waits for subscribers. An empty ev.ID is replaced by
// a generated identifier. It returns ErrInvalidEvent, ErrDuplicateID, ErrClosed, or
// an error for an empty or invalid topic list.
func (b *Broker) Publish(ev Event, topics ...string) error

// Serve subscribes the request to topics and streams events until the client
// disconnects (nil), the broker is closed (ErrClosed), the subscriber falls behind
// (ErrSlowConsumer) or a write fails. It registers the subscription before
// sending the response headers. When the request carries a Last-Event-ID, it
// first sends the missed events from the history or the replay function.
// Without topics, or when the writer cannot flush, it returns an error and
// writes nothing.
func (b *Broker) Serve(w http.ResponseWriter, r *http.Request, topics ...string) error

// Close ends every stream normally, so that clients reconnect, and makes later
// calls to Serve and Publish report ErrClosed. It does not wait. Closing again
// does nothing. Use it as the server's shutdown hook:
// srv.RegisterOnShutdown(b.Close).
func (b *Broker) Close()

// ReplayFunc provides, in order, the events that follow lastID in a durable
// source, by calling send with each event and its topics. topics are those of
// the subscription and may narrow the query: the broker filters the events
// itself. When send returns an error, the function should stop and return it.
// It returns an error matching ErrUnknownID when the source does not know
// lastID, and any other error on failure.
type ReplayFunc func(ctx context.Context, lastID string, topics []string,
	send func(ev Event, topics ...string) error) error
```

## Client (User Story 8, FR-049 to FR-059)

```go
// Decode returns the events of an event stream, interpreted as the standard
// defines. It stops at the end of r, at a read error, or at ErrEventTooLarge.
func Decode(r io.Reader, opts ...Option) iter.Seq2[Event, error]

// Client connects to event streams and reconnects as browsers do. It is safe for
// concurrent use.
type Client struct{ /* unexported */ }

func NewClient(opts ...Option) (*Client, error)

// Events connects with req (method, headers, body) and yields its events,
// reconnecting with Last-Event-ID after network errors and normal ends. It ends
// without error on 204, with *ResponseError on other statuses or content types,
// with the context's error when req's context ends, and closes the connection when
// the caller stops iterating. A body that cannot be sent again is an error.
func (c *Client) Events(req *http.Request) iter.Seq2[Event, error]
```

## Traceability

| Identifier | Requirements |
|------------|--------------|
| `Event` | FR-008 to FR-011, FR-050 |
| `ErrInvalidConfig`, `Option`, `With*` | FR-004 |
| `ErrInvalidEvent` | FR-009, FR-020, FR-043 |
| `ErrStreamingUnsupported` | FR-013, FR-026 |
| `ErrSlowConsumer` | FR-026, FR-028, FR-044 |
| `ErrClosed` | FR-026, FR-046, FR-047 |
| `ErrDuplicateID` | FR-022 |
| `ErrUnknownID` | FR-041 |
| `ErrEventTooLarge` | FR-051 |
| `ResponseError` | FR-054 |
| `Stream`, `NewStream`, `Send`, `KeepAlive` | FR-012 to FR-019 |
| `Broker`, `NewBroker`, `Publish` | FR-020 to FR-023, FR-031, FR-032 |
| `Serve` | FR-024 to FR-031, FR-033 to FR-037 |
| `Close` | FR-046 to FR-048 |
| `ReplayFunc`, `WithReplay` | FR-038 to FR-045 |
| `WithResetEvent` | FR-035, FR-036 |
| `Decode` | FR-049 to FR-052 |
| `Client`, `NewClient`, `Events`, `WithHTTPClient` | FR-053 to FR-059 |

The module exports 35 identifiers: 7 types, 4 functions, 7 methods, 9 options and 8 error
variables.

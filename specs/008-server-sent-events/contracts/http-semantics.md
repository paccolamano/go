# Contract: HTTP Semantics

**Module**: `github.com/paccolamano/go/sse` | **Research**: [R-06](../research.md#r-06-response-setup-and-flush-detection),
[R-07](../research.md#r-07-write-deadlines), [R-13](../research.md#r-13-closing),
[R-15](../research.md#r-15-client), [R-19](../research.md#r-19-compatibility-with-this-repositorys-middleware-and-auth)

## Server responses (streams and `Serve`)

| Condition | Response | Returned |
|-----------|----------|----------|
| Writer cannot flush (no `Flusher`/`FlushError` along the `Unwrap` chain) | nothing written | `ErrStreamingUnsupported` |
| `Serve` without topics | nothing written | error |
| Stream opened | `200`, `Content-Type: text/event-stream`, `Cache-Control: no-store`, `X-Accel-Buffering: no`, other headers kept, flushed at once | — |
| `Serve` on a closed broker | `200` with the headers above, `retry` if configured, then the end of the response | `ErrClosed` |
| Any later outcome of `Serve` | the response completes normally (`200`) | see [broker-semantics.md](broker-semantics.md) |

The library never answers a status other than 200 on its own. Errors detected before anything is
written are returned to the developer's handler, which answers them.

## Writes and deadlines

- Each event, keep-alive, position or reset is one `Write` followed by one flush, under the
  stream's mutex.
- Before each write the write deadline is set to now + write timeout (10 s by default). After a
  successful flush it is cleared.
  - Idle periods of any length never end a stream, even when the server has a `WriteTimeout`.
  - A write that cannot complete within the timeout fails and breaks the stream.
- If the writer does not support write deadlines (`http.ErrNotSupported`), writes proceed, and
  the server's `WriteTimeout`, if any, bounds the stream.
- After the request's context ends, sends return its error and write nothing.

Validated over HTTP/1.1 and unencrypted HTTP/2 (research R-07).

## Shutdown

- `srv.RegisterOnShutdown(broker.Close)`: when `srv.Shutdown` starts, every `Serve` returns
  `ErrClosed` and its response completes. The server becomes idle, and `Shutdown` returns without
  waiting for its deadline.
- Without the hook, `Shutdown` waits until its deadline, because open streams never go idle.
- Single streams (`NewStream`) end when their handler returns. A handler that must end on
  shutdown selects on a channel closed by its own shutdown hook.

## Middleware and authentication (documentation, research R-19)

| Component | Behavior with a stream |
|-----------|------------------------|
| `middleware/compress` | compresses `text/event-stream` (it matches `text/*`) and delivers every flush |
| `middleware/etag` | flushed responses pass through without an `ETag` |
| `middleware/maxconcurrency` | a stream holds its slot while open: do not wrap stream routes |
| `middleware/ratelimit` | limits connection attempts, which bounds reconnection storms |
| `middleware/accesslog` | logs the stream when it ends, with the connection's duration |
| `auth` with sessions | the cookie travels with the browser's event source |
| `auth` with tokens or API keys | browsers cannot set `Authorization` on an event source: non-browser clients only |
| `auth` rejections | 401 and 403 make browsers stop reconnecting |
| `http.TimeoutHandler` | cannot flush: `ErrStreamingUnsupported` |

## Client

| Event | Behavior |
|-------|----------|
| First connection | `req` cloned with its context. `Accept: text/event-stream` added when absent. A `Last-Event-ID` set by the developer is sent |
| Request with a body and no `GetBody` | iteration ends with an error before connecting |
| `200` with media type `text/event-stream` | decode and yield events; the backoff delay resets |
| `204` | iteration ends, no error |
| Other status, or `200` with another content type | iteration ends with `*ResponseError{StatusCode, ContentType}`, no reconnection |
| Network error before a response | wait, then reconnect. The delay doubles for each consecutive failure, up to max(30 s, server `retry`) |
| Body ends normally, or a network error while streaming | wait the reconnection time (server `retry`, default 3 s), then reconnect with `Last-Event-ID` = last identifier, if not empty |
| Decoding error (`ErrEventTooLarge`) | iteration ends with it |
| Request context ends | iteration ends with the context's error; the body is closed |
| Caller stops iterating | the body is closed before the iterator returns |

The last identifier includes identifiers received as positions, which are blocks without data.
Redirects are followed by the HTTP client, and every redirected request carries the same
`Last-Event-ID`.

An `http.Client` with a non-zero `Timeout` ends every stream when it expires. The documentation
recommends transport-level timeouts instead.

# Contract: Log Records

Both middleware log only through the `*slog.Logger` supplied to `New`, with
`LogAttrs(r.Context(), level, msg, attrs...)`. They check `Enabled` first. The keys below are
part of the public contract: renaming or removing one is a breaking change, while adding one is
compatible.

## accesslog

- **Message**: `http request`
- **Level**: `slog.LevelInfo`, or the value returned by the `WithLevel` callback.

| Key | Type | Present | Value |
|-----|------|---------|-------|
| `method` | string | always | `r.Method` |
| `path` | string | always | `r.URL.Path` |
| `query` | string | `WithQuery()` and the query is non-empty | `r.URL.RawQuery` |
| `proto` | string | always | `r.Proto` (e.g. `HTTP/1.1`, `HTTP/2.0`) |
| `status` | int | always | the final status (see [http-behavior.md](http-behavior.md#accesslog)) |
| `bytes` | int64 | always | body bytes written at accesslog's position |
| `duration` | duration | always | from entry into accesslog to the handler's return |
| `client` | string | always | the IP from `r.RemoteAddr`, as seen at accesslog's position, without the port; the raw `RemoteAddr` if it cannot be parsed |
| `user_agent` | string | the header is present | `User-Agent` |
| `request_id` | string | requestid ran earlier | `requestid.FromContext` |
| `panic` | bool | only when the handler panicked | `true` |
| `header` | group | `WithHeaders(...)` and at least one named header present | one string attribute per header, keyed by its canonical name; multiple values joined with `, ` |
| *(custom)* | any | `WithAttrs` | appended last |

## recovery

- **Message**: `panic recovered`
- **Level**: `slog.LevelError`
- **Not logged**: `http.ErrAbortHandler`.

| Key | Type | Present | Value |
|-----|------|---------|-------|
| `panic` | string | always | `fmt.Sprint(value)` |
| `stack` | string | always | `debug.Stack()` taken at the recovery point; it includes the panicking frames |
| `method` | string | always | `r.Method` |
| `path` | string | always | `r.URL.Path` |
| `request_id` | string | requestid ran earlier | `requestid.FromContext` |
| `response_started` | bool | always | whether the response was already started (`true` means the connection was aborted) |

The `WithReporter` callback, if set, runs after the record is logged. It receives the raw value
and the stack.

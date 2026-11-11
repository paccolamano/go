# slogx

A `slog.Handler` for the standard structured logger: context attributes, redaction of sensitive
data, and a default level and a per-request level decided once for every output. It adds no
logger type: keep using `*slog.Logger`. The module has no dependency outside the standard
library.

```bash
go get github.com/paccolamano/go/slogx
```

Minimum Go version: 1.26

## Usage

```go
h, err := slogx.New(slog.NewJSONHandler(os.Stdout, nil),
	slogx.WithDefaultLevel(slog.LevelInfo),
	slogx.WithContextValue("request_id", requestid.FromContext),
	slogx.WithRedactKeys("authorization", "password", "token", "cookie"),
)
if err != nil {
	return err
}
logger := slog.New(h)
```

The handler applies, in this order:

```text
level → context attributes → redaction → output
```

- **Level first**: `slog.Logger` asks only the outermost handler whether a level is enabled, so
  the level is decided once, for every output.
- **Redaction after context**: a token stored in the context is redacted too.

Invalid configuration is reported as errors wrapping `ErrInvalidConfig`.

## Context attributes

Attach attributes to a context with `With`, and read a value that another package stored in it,
such as the identifier of the `requestid` middleware, with `WithContextValue`. Every record
logged with that context carries them, at the top level, even inside `logger.WithGroup`:

```go
h, err := slogx.New(next, slogx.WithContextValue("request_id", requestid.FromContext))

ctx = slogx.With(ctx, slog.String("tenant", tenantID))
logger.InfoContext(ctx, "order created")
// {"level":"INFO","msg":"order created","tenant":"t1","request_id":"abc"}
```

`WithContextFunc` remains for a function that returns several attributes, such as the trace and
span identifiers of the [stdout plus OpenTelemetry recipe](#stdout-plus-opentelemetry).

When the record already has an attribute with the same key at the top level, the record wins
and the context attribute is skipped. The `accesslog` and `recovery` middleware, which add
`request_id` themselves, therefore never produce it twice.

Context attributes and the per-request level reach only the methods that take a context
(`InfoContext`, `LogAttrs`, …). golangci-lint's `sloglint` with `context: all` reports the
calls that lose the context.

## Redaction and secrets

`WithRedactKeys` replaces the value of every attribute whose key matches, without regard to
case and at every depth, including groups such as the `header` group of an access log, logger
attributes, context attributes and the groups that `LogValuer` values resolve to:

```go
logger.Info("http request", slog.Group("header", "Authorization", "Bearer x", "Accept", "*/*"))
// {"level":"INFO","msg":"http request","header":{"Authorization":"[REDACTED]","Accept":"*/*"}}
```

Redaction never looks inside values that the output handler encodes on its own, such as
structs. Wrap those in a `Secret`, which renders as `[REDACTED]` through any handler, with every
`fmt` verb and in JSON:

```go
type credentials struct {
	User     string
	Password slogx.Secret[string]
}
c := credentials{User: "bob", Password: slogx.NewSecret(password)}
logger.Info("login", "credentials", c) // "credentials":{"User":"bob","Password":"[REDACTED]"}
c.Password.Reveal()                    // the password
```

## Levels

`WithDefaultLevel` sets the level of the records logged with a context that carries no level.
The handler applies it without asking the output, so it holds whatever the output enables: the
OpenTelemetry bridge, for example, enables every level. A `*slog.LevelVar` changes it at run
time:

```go
level := new(slog.LevelVar) // Info
h, err := slogx.New(next, slogx.WithDefaultLevel(level))
// later, for example on a signal:
level.Set(slog.LevelDebug)
```

Without `WithDefaultLevel`, the output decides, through its own level.

`WithLevel` sets the level of one request:

```go
if debugRequested(r) {
	ctx = slogx.WithLevel(ctx, slog.LevelDebug)
}
logger.DebugContext(ctx, "cache miss") // written for this request only
```

The context level replaces the default level and the output's own threshold for the records
logged with that context, in both directions: it can also silence a request.

## Several outputs

`Fanout` passes every record to several outputs without asking them whether they handle its
level, so that every output receives the same records: the same levels, the same context
attributes, the same redaction. The levels of the outputs are ignored: the level is decided
once, by `New`, which requires `WithDefaultLevel` over a `Fanout`.

- Give the `Fanout` to `New` as the next handler, directly: `New` does not recognize a `Fanout`
  wrapped in another handler.
- A `New` used as one output, to add attributes to that output only, is not asked about levels
  either: give the level to the outer one.
- For deliberately different thresholds per output, use `slog.NewMultiHandler` instead: each
  output applies its own level, and the per-request level does not go below it.

## Recipes

The recipes use other modules, which slogx itself does not depend on.

### Colored output while developing

With [`github.com/lmittmann/tint`](https://github.com/lmittmann/tint) and
[`golang.org/x/term`](https://pkg.go.dev/golang.org/x/term):

```go
noColor := os.Getenv("NO_COLOR") != "" || !term.IsTerminal(int(os.Stdout.Fd()))
h, err := slogx.New(tint.NewHandler(os.Stdout, &tint.Options{NoColor: noColor}),
	slogx.WithDefaultLevel(slog.LevelDebug),
)
if err != nil {
	return err
}
logger := slog.New(h)
```

Colors are off when `NO_COLOR` is set to a non-empty value, or when stdout is not a terminal.
The level is set on slogx, so tint's own level option is not needed.

### Stdout plus OpenTelemetry

With the OpenTelemetry bridge
[`go.opentelemetry.io/contrib/bridges/otelslog`](https://pkg.go.dev/go.opentelemetry.io/contrib/bridges/otelslog),
[`go.opentelemetry.io/otel/trace`](https://pkg.go.dev/go.opentelemetry.io/otel/trace), and
`github.com/paccolamano/go/middleware/requestid` for the request identifier. `provider` is the
service's `log.LoggerProvider`.

```go
// traceAttrs writes the trace and span IDs on stdout. The OpenTelemetry output
// carries them in its own fields.
func traceAttrs(ctx context.Context) []slog.Attr {
	sc := trace.SpanContextFromContext(ctx)
	if !sc.IsValid() {
		return nil
	}
	return []slog.Attr{
		slog.String("trace_id", sc.TraceID().String()),
		slog.String("span_id", sc.SpanID().String()),
	}
}
```

```go
stdout, err := slogx.New(slog.NewJSONHandler(os.Stdout, nil), slogx.WithContextFunc(traceAttrs))
if err != nil {
	return err
}
level := new(slog.LevelVar) // Info; level.Set(slog.LevelDebug) changes it at run time
h, err := slogx.New(
	slogx.Fanout(stdout, otelslog.NewHandler("orders", otelslog.WithLoggerProvider(provider))),
	slogx.WithDefaultLevel(level),
	slogx.WithContextValue("request_id", requestid.FromContext),
	slogx.WithRedactKeys("authorization", "password", "token", "cookie"),
)
if err != nil {
	return err
}
logger := slog.New(h)
```

- Both outputs receive the same records, with the same levels, context attributes and
  redaction; the level of each output is not asked.
- `Fanout` requires `WithDefaultLevel`, and must be the next handler of `New` directly.
- The trace and span IDs are added on stdout only, by a `New` nested on that output, because the
  OpenTelemetry bridge records them on its own.
- The nested `New` is not asked about levels: the level is given to the outer one.

### Different thresholds per output

With the same libraries as the previous recipe:

```go
h, err := slogx.New(
	slog.NewMultiHandler(
		slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}),
		otelslog.NewHandler("orders", otelslog.WithLoggerProvider(provider)),
	),
	slogx.WithDefaultLevel(slog.LevelDebug),
)
if err != nil {
	return err
}
logger := slog.New(h)
```

- This uses the standard fan-out on purpose: each output applies its own level.
- The default level is the lowest level any output receives, and each output's own level raises
  it: stdout writes Info and above, OpenTelemetry Debug and above.
- The per-request level does not go below an output's own level: a request at Debug still writes
  no Debug record on stdout.
- Without `WithDefaultLevel`, an output that enables every level, such as the OpenTelemetry
  bridge, would receive every level.

## Testing with slogxtest

`slogxtest.Recorder` is a handler that keeps what is logged, with the logger's attributes and
groups, for assertions:

```go
rec := slogxtest.NewRecorder()
logger := slog.New(rec)
// ... code under test ...
r, ok := rec.Last()
if v, found := r.Attr("request_id"); !ok || r.Message != "panic recovered" || !found || v.String() != "abc" {
	t.Errorf("unexpected record %+v", r)
}
```

`Attr("query", "ms")` looks up an attribute inside a group, and `slogxtest.WithLevel` sets the
minimum level the recorder accepts.

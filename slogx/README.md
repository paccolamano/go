# slogx

Composable `slog.Handler` middleware: context attributes, redaction of sensitive data, a
per-request level and sampling of repeated records. It adds no logger type: keep using
`*slog.Logger`. The module has no dependency outside the standard library.

```bash
go get github.com/paccolamano/go/slogx
```

Minimum Go version: 1.26

## Usage

```go
h, err := slogx.New(slog.NewJSONHandler(os.Stdout, nil),
	slogx.WithContextFunc(requestIDAttrs),
	slogx.WithRedactKeys("authorization", "password", "token", "cookie"),
	slogx.WithSampling(10, 100, time.Second),
)
if err != nil {
	return err
}
logger := slog.New(h)
// At shutdown, report the records dropped in the last sampling interval:
// err = h.Flush(context.Background())
```

`New` composes the handlers in the only correct order:

```text
LevelHandler → SamplingHandler → ContextHandler → RedactHandler → output
```

- **Level outermost**: the per-request level must be honored before the output handler's
  threshold discards debug records.
- **Sampling before context and redaction**: a dropped record costs nothing more, and summaries
  carry no request attribute.
- **Redaction after context**: a token stored in the context is redacted too.

Each handler is also available on its own (`NewLevelHandler`, `NewSamplingHandler`,
`NewContextHandler`, `NewRedactHandler`) for finer control. Invalid options are reported as
errors wrapping `ErrInvalidConfig`.

## Context attributes

Attach attributes to a context with `With`, and read values that other packages stored in it
with an extraction function. Every record logged with that context carries them, at the top
level, even inside `logger.WithGroup`:

```go
func requestIDAttrs(ctx context.Context) []slog.Attr {
	if id, ok := requestid.FromContext(ctx); ok {
		return []slog.Attr{slog.String("request_id", id)}
	}
	return nil
}

ctx = slogx.With(ctx, slog.String("tenant", tenantID))
logger.InfoContext(ctx, "order created")
// {"level":"INFO","msg":"order created","tenant":"t1","request_id":"abc"}
```

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

## Per-request level

```go
if debugRequested(r) {
	ctx = slogx.WithLevel(ctx, slog.LevelDebug)
}
logger.DebugContext(ctx, "cache miss") // written for this request only
```

The context level replaces the output handler's threshold for the records logged with that
context, in both directions.

## Sampling

With `WithSampling(10, 100, time.Second)`, the first 10 records of each level and message are
written in every second, then one every 100. After an interval with drops, one summary per
group reports them:

```json
{"level":"WARN","msg":"records dropped by sampling","sampling":{"msg":"db query failed","dropped":9891,"start":"…","end":"…"}}
```

Records at Error level and above are always written, unless `WithSampleErrors()` is given. At
most 1,024 groups are tracked per interval; the records of any other group are written.
Summaries are emitted by the next record after the interval ends, or by `Flush`, and no
goroutine is started.

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

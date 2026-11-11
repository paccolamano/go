# Contract: README Recipes (FR-024, SC-009)

**Module**: `github.com/paccolamano/go/slogx` | **File**: `slogx/README.md`, section "Recipes"

The README gains three recipes. The code below was built and run in a separate module against
the versions listed in [research R-13](../research.md#r-13-readme-recipes-fr-024-sc-009). Each
snippet is the body of a setup function: it uses `err` and returns it on failure. The prose
around each snippet may be reworded; the statements listed under "The text states" may not be
dropped. The recipes add no dependency to the module (FR-022).

## 1. Colored output while developing

Libraries: `github.com/lmittmann/tint`, `golang.org/x/term`.

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

The text states:

- colors are off when `NO_COLOR` is set to a non-empty value, or when stdout is not a terminal;
- the level is set on slogx, so tint's own level option is not needed.

Expected behavior (checked): colored output in a terminal; plain output when piped or with
`NO_COLOR=1`; Debug records written.

## 2. Stdout plus OpenTelemetry

Libraries: `go.opentelemetry.io/contrib/bridges/otelslog`, `go.opentelemetry.io/otel/trace`,
and `github.com/paccolamano/go/middleware/requestid` for the request identifier. `provider` is
the service's `log.LoggerProvider`.

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

The text states:

- both outputs receive the same records, with the same levels, context attributes and
  redaction; the level of each output is not asked;
- `Fanout` requires `WithDefaultLevel`, and must be the next handler of `New` directly;
- the trace and span IDs are added on stdout only, by a `New` nested on that output, because the
  OpenTelemetry bridge records them on its own;
- a nested `New` is not asked about levels: give the level to the outer one.

Expected behavior (checked, inside a span and behind the `requestid` middleware): stdout records
have `trace_id`, `span_id` and `request_id`; OpenTelemetry records have the same trace and span
in their trace fields, `request_id` as an attribute and no `trace_id` attribute; `token` is
`[REDACTED]` on both; a Debug record is dropped at Info; a Debug record of a request at
`WithLevel(ctx, slog.LevelDebug)` reaches both.

## 3. Different thresholds per output

Libraries: as in recipe 2.

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

The text states:

- this uses the standard fan-out on purpose: each output applies its own level;
- the default level is the lowest level any output receives, and each output's own level
  raises it;
- the per-request level does not go below an output's own level: a request at Debug still
  writes no Debug record on stdout;
- without `WithDefaultLevel`, an output that enables every level, such as the OpenTelemetry
  bridge, would receive every level.

Expected behavior (checked): stdout receives Info records only; OpenTelemetry receives Debug and
Info; a request at `WithLevel(ctx, slog.LevelDebug)` writes Debug records to OpenTelemetry only;
a request at `WithLevel(ctx, slog.LevelWarn)` writes Info records nowhere.

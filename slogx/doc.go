// Package slogx provides composable slog.Handler middleware: attributes from
// the context, redaction of sensitive data, a per-request level and sampling of
// repeated records. It adds no logger type: use *slog.Logger with the handlers
// of this package.
//
//	h, err := slogx.New(slog.NewJSONHandler(os.Stdout, nil),
//		slogx.WithContextFunc(requestIDAttrs),
//		slogx.WithRedactKeys("authorization", "password", "token", "cookie"),
//		slogx.WithSampling(10, 100, time.Second),
//	)
//	if err != nil {
//		return err
//	}
//	logger := slog.New(h)
//
// # Context attributes
//
// With attaches attributes to a context, and WithContextFunc reads values that
// other packages stored in it, such as a request identifier. ContextHandler
// adds both to every record logged with that context, so a service that only
// receives a context still logs the request identifier, the user or the tenant:
//
//	ctx = slogx.With(ctx, slog.String("tenant", tenantID))
//	logger.InfoContext(ctx, "order created") // the record carries tenant
//
// Context attributes are always written at the top level, also inside a
// logger.WithGroup, so that they are found in the same place in every record.
// When the record, or the logger before any group, already has an attribute
// with the same key, that attribute wins and the context attribute is skipped:
// a key is never written twice at the top level. Among context attributes, the
// latest attached value of a key wins over earlier ones, attached attributes
// win over extracted ones, and the first extraction function wins over later
// ones.
//
// Context attributes and the per-request level reach only the records logged
// with the methods that take a context: InfoContext, DebugContext, LogAttrs and
// the like. Info and the other methods without a context use
// context.Background(). golangci-lint's sloglint, with the setting
// "context: all", reports the calls that lose the context.
//
// # Redaction and secrets
//
// RedactHandler writes [REDACTED] as the value of every attribute whose key is
// one of the keys given with WithRedactKeys, compared without regard to case,
// as a whole key and at every depth. It covers the attributes of the record,
// those added with logger.With, the context attributes, groups, and the groups
// that LogValuer values resolve to; the key is checked before the value is
// resolved, so a sensitive LogValuer is never called. A matching group, or a
// matching logger.WithGroup, is written as key=[REDACTED] with its whole
// content dropped.
//
// Redaction does not change the message, and does not look inside values that
// the output handler encodes on its own: structs, maps and error messages. For
// those, wrap the sensitive value in a Secret: it renders as [REDACTED] as a log
// value through any handler, with every fmt verb and in JSON, also as a struct
// field, and only Secret.Reveal returns it. Reflection-based printers that read
// unexported fields and follow pointers can still reach the value.
//
// # Per-request level
//
// WithLevel attaches a minimum level to a context, and LevelHandler compares
// the records logged with that context to it alone, ignoring the threshold of
// the output handler. It can enable debug records for one request in
// production, for example one that carries a signed header, or silence one.
//
// # Sampling
//
// SamplingHandler groups records by level and message. In each interval, the
// first records of a group are written, then one every thereafter: with
// WithSampling(10, 100, time.Second), 10,000 identical records in one second
// write 109. Records at slog.LevelError and above are always written, unless
// WithSampleErrors is given. At most 1,024 groups are tracked per interval, and
// the records of any other group are written.
//
// After an interval with drops, one summary per group is written, with the
// group's level:
//
//	{"level":"WARN","msg":"records dropped by sampling","sampling":{"msg":"db query failed","dropped":9891,"start":"…","end":"…"}}
//
// Summaries are emitted by the first record handled after the interval ends,
// and by Flush: call Flush at shutdown so that the last interval is reported.
// They carry no attribute of the logger or of any request. No goroutine is
// started.
//
// # Composition
//
// New composes the handlers in the only correct order:
//
//	LevelHandler → SamplingHandler → ContextHandler → RedactHandler → output
//
// Each position has a reason:
//
//   - LevelHandler is outermost, because slog.Logger asks only the outermost
//     handler whether a level is enabled: anywhere else, the output handler's
//     threshold would discard the debug records of a request first.
//   - SamplingHandler comes before the context attributes and the redaction, so
//     that a dropped record costs nothing more and summaries carry no request
//     attribute.
//   - RedactHandler comes after ContextHandler, so that a token stored in the
//     context is redacted too.
//
// The standalone handlers give finer control; New's documentation shows the
// equivalent manual composition. Constructors report invalid options, and
// options that do not apply to them, as errors wrapping ErrInvalidConfig.
//
// # Robustness
//
// No handler panics or lets a panic escape. A LogValue method that panics is
// replaced by slog's own placeholder, an error whose text starts with
// "LogValue panicked". An extraction function that panics is replaced by the
// attribute !CONTEXTFUNC="panic: <value>". Redaction follows groups 64 levels
// deep, and a deeper group value becomes the string !DEPTH, so that a
// LogValuer that returns itself cannot exhaust the stack. A handler returns the
// next handler's error unchanged. When a record is disabled, no handler
// allocates memory.
//
// Package slogxtest provides a Recorder handler, to check in tests what code
// logs.
package slogx

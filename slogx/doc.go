// Package slogx provides a slog.Handler that adds three things to the standard
// structured logger, without a logger type of its own: attributes taken from
// the context of each record, redaction of sensitive data, and levels decided
// once for every output: a default level, and a minimum level attached to the
// context of one request. Use *slog.Logger with the Handler that New builds:
//
//	h, err := slogx.New(slog.NewJSONHandler(os.Stdout, nil),
//		slogx.WithDefaultLevel(slog.LevelInfo),
//		slogx.WithContextValue("request_id", requestid.FromContext),
//		slogx.WithRedactKeys("authorization", "password", "token", "cookie"),
//	)
//	if err != nil {
//		return err
//	}
//	logger := slog.New(h)
//
// # Context attributes
//
// With attaches attributes to a context, and WithContextValue and
// WithContextFunc read values that other packages stored in it, such as a
// request identifier. The Handler adds them to every record logged with that
// context, so a service that only receives a context still logs the request
// identifier, the user or the tenant:
//
//	ctx = slogx.With(ctx, slog.String("tenant", tenantID))
//	logger.InfoContext(ctx, "order created") // the record carries tenant
//
// WithContextValue adds one attribute from an accessor such as
// requestid.FromContext. WithContextFunc is for a function that returns several
// attributes, such as the trace and span identifiers.
//
// Context attributes are always written at the top level, also inside a
// logger.WithGroup, so that they are found in the same place in every record.
// When the record, or the logger before any group, already has an attribute
// with the same key, that attribute wins and the context attribute is skipped:
// a key is never written twice at the top level. Among context attributes, the
// latest attached value of a key wins over earlier ones, attached attributes
// win over the others, and among context values and extraction functions, the
// first configured wins.
//
// Context attributes and the per-request level reach only the records logged
// with the methods that take a context: InfoContext, DebugContext, LogAttrs and
// the like. Info and the other methods without a context use
// context.Background(). golangci-lint's sloglint, with the setting
// "context: all", reports the calls that lose the context.
//
// # Redaction and secrets
//
// With WithRedactKeys, the Handler writes [REDACTED] as the value of every
// attribute whose key is one of the given keys, compared without regard to
// case, as a whole key and at every depth. It covers the attributes of the
// record, those added with logger.With, the context attributes, groups, and
// the groups that LogValuer values resolve to; the key is checked before the
// value is resolved, so a sensitive LogValuer is never called. A matching
// group, or a matching logger.WithGroup, is written as key=[REDACTED] with its
// whole content dropped.
//
// Redaction does not change the message, and does not look inside values that
// the output handler encodes on its own: structs, maps and error messages. For
// those, wrap the sensitive value in a Secret: it renders as [REDACTED] as a log
// value through any handler, with every fmt verb and in JSON, also as a struct
// field, and only Secret.Reveal returns it. Reflection-based printers that read
// unexported fields and follow pointers can still reach the value.
//
// # Levels
//
// WithDefaultLevel sets the level of the records logged with a context that
// carries no level. The Handler compares them with it alone and does not ask
// the next handler, so the level holds whatever the outputs enable: the
// OpenTelemetry bridge, for example, enables every level. A *slog.LevelVar
// changes it at run time. Without WithDefaultLevel, the next handler decides.
//
// WithLevel attaches a minimum level to a context. The records logged with that
// context are compared with it alone, ignoring the default level and the
// output's own level, in both directions: it can enable debug records for one
// request in production, for example one that carries a signed header, or
// silence one.
//
// A record enabled by either level reaches the next handler whatever the next
// handler's own level: no handler after the Handler checks the level again.
//
// # Several outputs
//
// Fanout passes every record to several outputs, such as stdout and the
// OpenTelemetry bridge, without asking them whether they handle its level, so
// that they all receive the same records: the same levels, the same context
// attributes, the same redaction. Give it to New as the next handler, directly,
// with WithDefaultLevel, which New then requires:
//
//	h, err := slogx.New(slogx.Fanout(stdout, otel), slogx.WithDefaultLevel(level))
//
// A Fanout wrapped in another handler is not recognized. The levels of the
// outputs are ignored, and a New used as one output, to add attributes to that
// output only, is not asked about levels either: give the level to the outer
// one.
//
// For deliberately different thresholds per output, use slog.NewMultiHandler
// instead: each output applies its own level, and the per-request level does
// not go below it.
//
// # Composition
//
// The Handler applies, in this order:
//
//	level → context attributes → redaction → next handler
//
// Each position has a reason:
//
//   - The level comes first, because slog.Logger asks only the outermost
//     handler whether a level is enabled.
//   - The redaction comes after the context attributes, so that a token stored
//     in the context is redacted too.
//
// New reports invalid configuration, such as a nil default level or a Fanout
// without WithDefaultLevel, as errors wrapping ErrInvalidConfig.
//
// # Robustness
//
// No handler panics or lets a panic escape. A LogValue method that panics is
// replaced by slog's own placeholder, an error whose text starts with
// "LogValue panicked". An extraction function or a context value that panics
// is replaced by the attribute !CONTEXTFUNC="panic: <value>". Redaction follows
// groups 64 levels deep, and a deeper group value becomes the string !DEPTH, so
// that a LogValuer that returns itself cannot exhaust the stack. The Handler
// returns the next handler's error unchanged, and a Fanout returns the error of
// a single failing output unchanged and combines several with errors.Join.
// When a record is disabled, the Handler allocates no memory, and calls no
// extraction function and no context value.
//
// Package slogxtest provides a Recorder handler, to check in tests what code
// logs.
package slogx

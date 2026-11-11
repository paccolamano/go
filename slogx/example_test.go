package slogx_test

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"

	"github.com/paccolamano/go/slogx"
)

// stdoutJSON returns a JSON handler on standard output without the time, so
// that the examples have a stable output.
func stdoutJSON() slog.Handler {
	return slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{ReplaceAttr: dropTime})
}

// A service logs with the context it received; the attributes attached
// upstream appear in its records without passing a logger around.
func ExampleWith() {
	h, err := slogx.New(stdoutJSON())
	if err != nil {
		panic(err)
	}
	logger := slog.New(h)

	ctx := slogx.With(context.Background(), slog.String("tenant", "t1"))
	ctx = slogx.With(ctx, slog.String("user", "u42"))
	logger.InfoContext(ctx, "order created", "order", 1001)
	// Output:
	// {"level":"INFO","msg":"order created","tenant":"t1","user":"u42","order":1001}
}

type requestIDKey struct{}

// requestIDAttrs reads the identifier that a middleware stored in the context.
func requestIDAttrs(ctx context.Context) []slog.Attr {
	if id, ok := ctx.Value(requestIDKey{}).(string); ok {
		return []slog.Attr{slog.String("request_id", id)}
	}
	return nil
}

// requestIDFrom reads the identifier that a middleware stored in the context,
// as requestid.FromContext does.
func requestIDFrom(ctx context.Context) (string, bool) {
	id, ok := ctx.Value(requestIDKey{}).(string)
	return id, ok
}

// An extraction function reads a value stored in the context by another
// package, such as a request identifier, without importing that package here.
// Context attributes stay at the top level even inside a group.
func ExampleWithContextFunc() {
	h, err := slogx.New(stdoutJSON(), slogx.WithContextFunc(requestIDAttrs))
	if err != nil {
		panic(err)
	}
	logger := slog.New(h)

	ctx := context.WithValue(context.Background(), requestIDKey{}, "abc")
	logger.WithGroup("db").InfoContext(ctx, "query", "rows", 3)
	// Output:
	// {"level":"INFO","msg":"query","request_id":"abc","db":{"rows":3}}
}

// A context value logs one value that another package stored in the context,
// such as the identifier of a request ID middleware, with one option.
func ExampleWithContextValue() {
	h, err := slogx.New(stdoutJSON(), slogx.WithContextValue("request_id", requestIDFrom))
	if err != nil {
		panic(err)
	}
	logger := slog.New(h)

	ctx := context.WithValue(context.Background(), requestIDKey{}, "abc")
	logger.InfoContext(ctx, "order created", "order", 1001)
	logger.InfoContext(context.Background(), "no request")
	// Output:
	// {"level":"INFO","msg":"order created","request_id":"abc","order":1001}
	// {"level":"INFO","msg":"no request"}
}

// Sensitive keys are matched without regard to case and at any depth, such as
// the Authorization header inside the header group of an access log.
func ExampleWithRedactKeys() {
	h, err := slogx.New(stdoutJSON(), slogx.WithRedactKeys("authorization", "password", "token", "cookie"))
	if err != nil {
		panic(err)
	}
	logger := slog.New(h)

	logger.Info("http request",
		"path", "/orders",
		slog.Group("header", "Authorization", "Bearer x", "Accept", "*/*"),
	)
	// Output:
	// {"level":"INFO","msg":"http request","path":"/orders","header":{"Authorization":"[REDACTED]","Accept":"*/*"}}
}

// A Secret renders as [REDACTED] in logs, with fmt and in JSON, through any
// handler; Reveal returns the value.
func ExampleSecret() {
	type credentials struct {
		User     string
		Password slogx.Secret[string]
	}
	c := credentials{User: "bob", Password: slogx.NewSecret("hunter2")}

	slog.New(stdoutJSON()).Info("login", "credentials", c)
	fmt.Printf("%v\n", c)
	b, err := json.Marshal(c)
	if err != nil {
		panic(err)
	}
	fmt.Println(string(b))
	fmt.Println(c.Password.Reveal())
	// Output:
	// {"level":"INFO","msg":"login","credentials":{"User":"bob","Password":"[REDACTED]"}}
	// {bob [REDACTED]}
	// {"User":"bob","Password":"[REDACTED]"}
	// hunter2
}

// Debug records are written for one request only, for example one that carries
// a signed debug header, while the output handler stays at Info.
func ExampleWithLevel() {
	h, err := slogx.New(stdoutJSON())
	if err != nil {
		panic(err)
	}
	logger := slog.New(h)

	logger.DebugContext(context.Background(), "cache miss", "key", "a")
	debugCtx := slogx.WithLevel(context.Background(), slog.LevelDebug)
	logger.DebugContext(debugCtx, "cache miss", "key", "b")
	// Output:
	// {"level":"DEBUG","msg":"cache miss","key":"b"}
}

// The default level applies to the records logged with a context that carries
// no level, whatever the output enables. A *slog.LevelVar changes it at run
// time, and a request can still set its own.
func ExampleWithDefaultLevel() {
	level := new(slog.LevelVar) // Info
	h, err := slogx.New(stdoutJSON(), slogx.WithDefaultLevel(level))
	if err != nil {
		panic(err)
	}
	logger := slog.New(h)
	ctx := context.Background()

	logger.DebugContext(ctx, "hidden at Info")
	level.Set(slog.LevelDebug)
	logger.DebugContext(ctx, "written at Debug")
	level.Set(slog.LevelWarn)
	logger.InfoContext(ctx, "hidden at Warn")
	logger.InfoContext(slogx.WithLevel(ctx, slog.LevelInfo), "written for one request")
	// Output:
	// {"level":"DEBUG","msg":"written at Debug"}
	// {"level":"INFO","msg":"written for one request"}
}

// Both outputs receive the same records, whatever their own levels: the level
// is decided once, by the default level. The text output, built for errors
// only, receives the Info record too.
func ExampleFanout() {
	text := slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError, ReplaceAttr: dropTime})
	h, err := slogx.New(slogx.Fanout(stdoutJSON(), text), slogx.WithDefaultLevel(slog.LevelInfo))
	if err != nil {
		panic(err)
	}
	logger := slog.New(h)

	logger.Debug("cache miss")
	logger.Info("order created", "order", 1001)
	// Output:
	// {"level":"INFO","msg":"order created","order":1001}
	// level=INFO msg="order created" order=1001
}

// New composes the handlers in the correct order: the per-request level
// outermost, then the context attributes, then the redaction, which therefore
// covers the context attributes too.
func ExampleNew() {
	h, err := slogx.New(stdoutJSON(),
		slogx.WithContextFunc(requestIDAttrs),
		slogx.WithRedactKeys("authorization", "password", "token", "cookie"),
	)
	if err != nil {
		panic(err)
	}
	logger := slog.New(h)

	ctx := context.WithValue(context.Background(), requestIDKey{}, "abc")
	ctx = slogx.With(ctx, slog.String("token", "s3cr3t"))
	ctx = slogx.WithLevel(ctx, slog.LevelDebug)
	logger.DebugContext(ctx, "cache miss", "key", "orders:1001")
	// Output:
	// {"level":"DEBUG","msg":"cache miss","token":"[REDACTED]","request_id":"abc","key":"orders:1001"}
}

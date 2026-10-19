package slogx_test

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"time"

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
	h, err := slogx.NewContextHandler(stdoutJSON())
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

// An extraction function reads a value stored in the context by another
// package, such as a request identifier, without importing that package here.
// Context attributes stay at the top level even inside a group.
func ExampleNewContextHandler() {
	h, err := slogx.NewContextHandler(stdoutJSON(), slogx.WithContextFunc(requestIDAttrs))
	if err != nil {
		panic(err)
	}
	logger := slog.New(h)

	ctx := context.WithValue(context.Background(), requestIDKey{}, "abc")
	logger.WithGroup("db").InfoContext(ctx, "query", "rows", 3)
	// Output:
	// {"level":"INFO","msg":"query","request_id":"abc","db":{"rows":3}}
}

// Sensitive keys are matched without regard to case and at any depth, such as
// the Authorization header inside the header group of an access log.
func ExampleNewRedactHandler() {
	h, err := slogx.NewRedactHandler(stdoutJSON(), slogx.WithRedactKeys("authorization", "password", "token", "cookie"))
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
	h, err := slogx.NewLevelHandler(stdoutJSON())
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

// A burst of identical records is thinned out, and a summary reports the
// dropped ones. Flush emits the summary of the current interval, for example at
// shutdown.
func ExampleNewSamplingHandler() {
	// The output leaves out times, including the period of the summary.
	out := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			if a.Key == slog.TimeKey || len(groups) == 1 && groups[0] == "sampling" && (a.Key == "start" || a.Key == "end") {
				return slog.Attr{}
			}
			return a
		},
	})
	h, err := slogx.NewSamplingHandler(out, slogx.WithSampling(2, 10, time.Minute))
	if err != nil {
		panic(err)
	}
	logger := slog.New(h)

	for range 25 {
		logger.Warn("db query failed")
	}
	if err := h.Flush(context.Background()); err != nil {
		panic(err)
	}
	// Output:
	// {"level":"WARN","msg":"db query failed"}
	// {"level":"WARN","msg":"db query failed"}
	// {"level":"WARN","msg":"db query failed"}
	// {"level":"WARN","msg":"db query failed"}
	// {"level":"WARN","msg":"records dropped by sampling","sampling":{"msg":"db query failed","dropped":21}}
}

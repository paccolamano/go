// Package accesslog writes one structured log record per request.
//
// The record, with message "http request", is emitted after the response
// completes and contains the method, path, protocol, status, response body
// size, duration, client address, user agent and, when the requestid
// middleware ran earlier, the request identifier. The query string and header
// values are left out unless enabled, because they may carry credentials.
// When the downstream handler panics, the record is still emitted with
// panic=true and the panic continues unchanged.
//
// Mandatory settings: the *slog.Logger that receives the records.
//
// Options and defaults:
//   - [WithLevel]: every record at slog.LevelInfo.
//   - [WithAttrs]: no custom attributes.
//   - [WithSkip]: every request is logged.
//   - [WithQuery]: the query string is excluded.
//   - [WithHeaders]: no header values.
//
// Recommended position: after realip and requestid, so that the client address
// is the resolved one and the identifier is present, and before recovery, so
// that the 500 it writes is recorded. The bytes field counts what is written at
// accesslog's position: compressed bytes when compress runs after it.
package accesslog

import (
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/paccolamano/go/middleware"
	"github.com/paccolamano/go/middleware/internal/netaddr"
	"github.com/paccolamano/go/middleware/internal/respwriter"
	"github.com/paccolamano/go/middleware/requestid"
)

// Result describes the outcome of a request. It is passed to the WithLevel
// and WithAttrs functions. Fields may be added in minor releases.
type Result struct {
	// Status is the final status: 200 when the handler wrote a body, or
	// nothing, without setting one; 500 when it panicked before writing a
	// status; 101 when it hijacked the connection without setting one.
	Status int
	// Bytes is the number of body bytes written at accesslog's position.
	Bytes int64
	// Duration is the time from entering accesslog to the handler's return.
	Duration time.Duration
	// Panicked reports whether the handler panicked.
	Panicked bool
}

// New returns a middleware that logs every request to logger.
func New(logger *slog.Logger, opts ...Option) (middleware.Middleware, error) {
	if logger == nil {
		return nil, fmt.Errorf("%w: accesslog: logger: must not be nil", middleware.ErrInvalidConfig)
	}
	var cfg config
	for _, opt := range opts {
		opt(&cfg)
	}
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if cfg.skip != nil && cfg.skip(r) {
				next.ServeHTTP(w, r)
				return
			}
			start := time.Now()
			ow := respwriter.New(w)
			completed := false
			defer func() {
				res := Result{Status: ow.Status(), Bytes: ow.Bytes(), Duration: time.Since(start), Panicked: !completed}
				if res.Status == 0 {
					res.Status = http.StatusOK
					if res.Panicked {
						res.Status = http.StatusInternalServerError
					}
				}
				cfg.log(logger, r, res)
			}()
			next.ServeHTTP(ow, r)
			completed = true
		})
	}, nil
}

func (c *config) log(logger *slog.Logger, r *http.Request, res Result) {
	level := slog.LevelInfo
	if c.level != nil {
		level = c.level(r, res)
	}
	ctx := r.Context()
	if !logger.Enabled(ctx, level) {
		return
	}
	attrs := make([]slog.Attr, 0, 14)
	attrs = append(attrs, slog.String("method", r.Method), slog.String("path", r.URL.Path))
	if c.query && r.URL.RawQuery != "" {
		attrs = append(attrs, slog.String("query", r.URL.RawQuery))
	}
	attrs = append(attrs,
		slog.String("proto", r.Proto),
		slog.Int("status", res.Status),
		slog.Int64("bytes", res.Bytes),
		slog.Duration("duration", res.Duration),
		slog.String("client", client(r.RemoteAddr)),
	)
	if ua := r.UserAgent(); ua != "" {
		attrs = append(attrs, slog.String("user_agent", ua))
	}
	if id, ok := requestid.FromContext(ctx); ok {
		attrs = append(attrs, slog.String("request_id", id))
	}
	if res.Panicked {
		attrs = append(attrs, slog.Bool("panic", true))
	}
	if group := c.headerGroup(r.Header); len(group) > 0 {
		attrs = append(attrs, slog.Attr{Key: "header", Value: slog.GroupValue(group...)})
	}
	if c.attrs != nil {
		attrs = append(attrs, c.attrs(r, res)...)
	}
	logger.LogAttrs(ctx, level, "http request", attrs...)
}

func (c *config) headerGroup(h http.Header) []slog.Attr {
	var group []slog.Attr
	for _, name := range c.headers {
		if values := h.Values(name); len(values) > 0 {
			group = append(group, slog.String(name, strings.Join(values, ", ")))
		}
	}
	return group
}

func client(remoteAddr string) string {
	if addr, ok := netaddr.ParseRemoteAddr(remoteAddr); ok {
		return addr.String()
	}
	return remoteAddr
}

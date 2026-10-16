// Package recovery recovers panics raised by downstream handlers, so that one
// failing request never takes down the service or hides what happened.
//
// When a handler panics before the response has started, the middleware
// removes the representation headers the handler had set, sets
// Cache-Control: no-store and answers 500 Internal Server Error with a generic
// body. When the response has already started, it aborts the response, so the
// client can tell it is incomplete. Every recovered panic is logged at error
// level with its value, stack trace, method, path and request identifier, and
// the response never discloses those details. The http.ErrAbortHandler
// sentinel is propagated unchanged and not logged.
//
// Panics raised in goroutines started by a handler cannot be recovered by any
// middleware.
//
// Mandatory settings: the *slog.Logger that receives the records.
//
// Options and defaults:
//   - [WithReporter]: none.
//   - [WithRejectFunc]: a plain-text 500 response.
//
// Recommended position: just inside accesslog, so that the access log records
// the 500, and outside every other middleware, so that their panics are
// recovered too.
package recovery

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"runtime/debug"

	"github.com/paccolamano/go/middleware"
	"github.com/paccolamano/go/middleware/internal/respwriter"
	"github.com/paccolamano/go/middleware/requestid"
)

// ErrPanic is the Rejection.Err when the handler panicked before responding.
var ErrPanic = errors.New("recovery: handler panicked")

// representationHeaders describe the body the handler intended to send; they
// do not apply to the failure response.
func representationHeaders() []string {
	return []string{"Content-Length", "Content-Type", "Content-Encoding", "Content-Range",
		"ETag", "Last-Modified", "Content-Disposition", "Trailer"}
}

// New returns a middleware that recovers panics and logs them to logger.
func New(logger *slog.Logger, opts ...Option) (middleware.Middleware, error) {
	if logger == nil {
		return nil, fmt.Errorf("%w: recovery: logger: must not be nil", middleware.ErrInvalidConfig)
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
			ow := respwriter.New(w)
			defer func() {
				v := recover()
				if v == nil {
					return
				}
				if err, ok := v.(error); ok && errors.Is(err, http.ErrAbortHandler) {
					panic(v) //nolint:forbidigo // net/http abort protocol (research R-11)
				}
				stack := debug.Stack()
				started := ow.Started()
				attrs := []slog.Attr{
					slog.String("panic", fmt.Sprint(v)),
					slog.String("stack", string(stack)),
					slog.String("method", r.Method),
					slog.String("path", r.URL.Path),
				}
				if id, ok := requestid.FromContext(r.Context()); ok {
					attrs = append(attrs, slog.String("request_id", id))
				}
				attrs = append(attrs, slog.Bool("response_started", started))
				logger.LogAttrs(r.Context(), slog.LevelError, "panic recovered", attrs...)
				if cfg.reporter != nil {
					cfg.reporter(r, v, stack)
				}
				if started {
					panic(http.ErrAbortHandler) //nolint:forbidigo // net/http abort protocol (research R-11)
				}
				h := w.Header()
				for _, k := range representationHeaders() {
					h.Del(k)
				}
				h.Set("Cache-Control", "no-store")
				respwriter.Reject(w, r, cfg.reject, middleware.Rejection{Status: http.StatusInternalServerError, Err: ErrPanic})
			}()
			next.ServeHTTP(ow, r)
		})
	}, nil
}

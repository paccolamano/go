package middleware_test

import (
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/paccolamano/go/middleware"
	"github.com/paccolamano/go/middleware/accesslog"
	"github.com/paccolamano/go/middleware/bodylimit"
	"github.com/paccolamano/go/middleware/compress"
	"github.com/paccolamano/go/middleware/contenttype"
	"github.com/paccolamano/go/middleware/cors"
	"github.com/paccolamano/go/middleware/decompress"
	"github.com/paccolamano/go/middleware/etag"
	"github.com/paccolamano/go/middleware/maxconcurrency"
	"github.com/paccolamano/go/middleware/ratelimit"
	"github.com/paccolamano/go/middleware/realip"
	"github.com/paccolamano/go/middleware/recovery"
	"github.com/paccolamano/go/middleware/requestid"
	"github.com/paccolamano/go/middleware/secureheaders"
)

type constructor func() (middleware.Middleware, error)

// TestConventions_InvalidConfig checks, for every package, that the
// mandatory settings alone are enough and that an invalid setting is reported
// with ErrInvalidConfig and the package name (US1 scenarios 4 and 6).
func TestConventions_InvalidConfig(t *testing.T) {
	logger := slog.New(slog.DiscardHandler)
	tests := []struct {
		pkg     string
		valid   constructor
		invalid constructor
	}{
		{"accesslog", func() (middleware.Middleware, error) { return accesslog.New(logger) },
			func() (middleware.Middleware, error) { return accesslog.New(nil) }},
		{"bodylimit", func() (middleware.Middleware, error) { return bodylimit.New(1 << 20) },
			func() (middleware.Middleware, error) { return bodylimit.New(-1) }},
		{"compress", func() (middleware.Middleware, error) { return compress.New() },
			func() (middleware.Middleware, error) { return compress.New(compress.WithLevel(42)) }},
		{"contenttype", func() (middleware.Middleware, error) { return contenttype.New([]string{"application/json"}) },
			func() (middleware.Middleware, error) { return contenttype.New(nil) }},
		{"cors", func() (middleware.Middleware, error) { return cors.New([]string{"https://app.example.com"}) },
			func() (middleware.Middleware, error) { return cors.New([]string{"*"}, cors.WithCredentials()) }},
		{"decompress", func() (middleware.Middleware, error) { return decompress.New() },
			func() (middleware.Middleware, error) { return decompress.New(decompress.WithMaxSize(0)) }},
		{"etag", func() (middleware.Middleware, error) { return etag.New() },
			func() (middleware.Middleware, error) { return etag.New(etag.WithMaxSize(0)) }},
		{"maxconcurrency", func() (middleware.Middleware, error) { return maxconcurrency.New(64) },
			func() (middleware.Middleware, error) { return maxconcurrency.New(0) }},
		{"ratelimit", func() (middleware.Middleware, error) { return ratelimit.New(100, time.Minute) },
			func() (middleware.Middleware, error) { return ratelimit.New(100, 0) }},
		{"realip", func() (middleware.Middleware, error) { return realip.New(realip.TrustRanges("10.0.0.0/8")) },
			func() (middleware.Middleware, error) { return realip.New(realip.Trust{}) }},
		{"recovery", func() (middleware.Middleware, error) { return recovery.New(logger) },
			func() (middleware.Middleware, error) { return recovery.New(nil) }},
		{"requestid", func() (middleware.Middleware, error) { return requestid.New() },
			func() (middleware.Middleware, error) { return requestid.New(requestid.WithHeader("bad header")) }},
		{"secureheaders", func() (middleware.Middleware, error) { return secureheaders.New() },
			func() (middleware.Middleware, error) { return secureheaders.New(secureheaders.WithCSPNonce()) }},
	}
	for _, tt := range tests {
		t.Run(tt.pkg, func(t *testing.T) {
			if mw, err := tt.valid(); err != nil || mw == nil {
				t.Errorf("mandatory settings only: New = %v, %v; want a middleware", mw, err)
			}
			mw, err := tt.invalid()
			if !errors.Is(err, middleware.ErrInvalidConfig) || mw != nil {
				t.Fatalf("invalid setting: New = %v, %v; want nil, ErrInvalidConfig", mw, err)
			}
			if prefix := "middleware: invalid configuration: " + tt.pkg + ": "; !strings.HasPrefix(err.Error(), prefix) {
				t.Errorf("error = %q, want prefix %q", err, prefix)
			}
		})
	}
}

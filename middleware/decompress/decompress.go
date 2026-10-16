// Package decompress decodes compressed request bodies.
//
// A request whose body is declared with Content-Encoding gzip or deflate
// (plus the codings registered with [WithDecoder]) reaches the handler with
// its body decoded, without Content-Encoding and Content-Length, and with
// ContentLength -1. An unknown coding, or several stacked codings, is refused
// with 415 Unsupported Media Type and an Accept-Encoding header listing the
// supported codings. Requests without a body, without a coding or with the
// identity coding pass through unchanged.
//
// Decompression is dangerous without a bound: a small body can expand to a
// huge one. The decoded size is capped (10 MiB by default); reading past the
// cap fails with an *http.MaxBytesError, as with bodylimit. Corrupt or
// truncated data, including a malformed header, makes the handler's Read fail
// with an error that matches [ErrInvalidBody].
//
// Mandatory settings: none. Decompression is active only where the developer
// applies this middleware.
//
// Options and defaults:
//   - [WithMaxSize]: 10 MiB.
//   - [WithDecoder]: gzip and deflate only.
//   - [WithRejectFunc]: a plain-text 415 response, with Rejection.Err
//     matching [ErrUnsupportedEncoding].
//
// Recommended position: just after bodylimit, which then bounds the encoded
// size while this middleware bounds the decoded size.
package decompress

import (
	"compress/gzip"
	"compress/zlib"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/paccolamano/go/middleware"
	"github.com/paccolamano/go/middleware/internal/httpheader"
	"github.com/paccolamano/go/middleware/internal/respwriter"
)

// ErrInvalidBody is wrapped by every failure to decode the request body.
var ErrInvalidBody = errors.New("decompress: invalid encoded body")

// ErrUnsupportedEncoding is the Rejection.Err of an unknown or stacked
// content coding.
var ErrUnsupportedEncoding = errors.New("decompress: unsupported content encoding")

// Option configures the middleware returned by New.
type Option func(*config)

type decoder struct {
	name      string
	newReader func(r io.Reader) (io.ReadCloser, error)
}

type config struct {
	maxSize   int64
	decoders  []decoder
	invalid   []string
	reject    middleware.RejectFunc
	rejectSet bool
}

// WithMaxSize sets the maximum decoded body size in bytes. The default is
// 10 MiB.
func WithMaxSize(bytes int64) Option {
	return func(c *config) { c.maxSize = bytes }
}

// WithDecoder registers a content coding, such as "br" or "zstd", decoded by
// a reader that the caller provides. Registering "gzip" or "deflate" replaces
// the built-in decoder.
func WithDecoder(name string, newReader func(r io.Reader) (io.ReadCloser, error)) Option {
	return func(c *config) {
		switch {
		case !httpheader.ValidToken(name) || name != strings.ToLower(name) || name == "identity" || name == "*":
			c.invalid = append(c.invalid, fmt.Sprintf("%q is not a valid lowercase coding name", name))
		case newReader == nil:
			c.invalid = append(c.invalid, fmt.Sprintf("the constructor of %q must not be nil", name))
		default:
			for i := range c.decoders {
				if c.decoders[i].name == name {
					c.decoders[i].newReader = newReader
					return
				}
			}
			c.decoders = append(c.decoders, decoder{name: name, newReader: newReader})
		}
	}
}

// WithRejectFunc replaces the default plain-text 415 response.
// Accept-Encoding is already set when the function runs.
func WithRejectFunc(fn middleware.RejectFunc) Option {
	return func(c *config) {
		c.reject = fn
		c.rejectSet = true
	}
}

func defaultConfig() config {
	return config{
		maxSize: 10 << 20,
		decoders: []decoder{
			{name: "gzip", newReader: func(r io.Reader) (io.ReadCloser, error) {
				zr, err := gzip.NewReader(r)
				if err != nil {
					return nil, fmt.Errorf("gzip: %w", err)
				}
				return zr, nil
			}},
			{name: "deflate", newReader: func(r io.Reader) (io.ReadCloser, error) {
				zr, err := zlib.NewReader(r)
				if err != nil {
					return nil, fmt.Errorf("deflate: %w", err)
				}
				return zr, nil
			}},
		},
	}
}

// New returns a middleware that decodes compressed request bodies.
func New(opts ...Option) (middleware.Middleware, error) {
	cfg := defaultConfig()
	for _, opt := range opts {
		opt(&cfg)
	}
	switch {
	case len(cfg.invalid) > 0:
		return nil, fmt.Errorf("%w: decompress: WithDecoder: %s", middleware.ErrInvalidConfig, cfg.invalid[0])
	case cfg.maxSize <= 0:
		return nil, fmt.Errorf("%w: decompress: WithMaxSize: must be greater than 0", middleware.ErrInvalidConfig)
	case cfg.rejectSet && cfg.reject == nil:
		return nil, fmt.Errorf("%w: decompress: WithRejectFunc: the function must not be nil", middleware.ErrInvalidConfig)
	}
	decoders := map[string]decoder{}
	names := make([]string, 0, len(cfg.decoders))
	for _, d := range cfg.decoders {
		decoders[d.name] = d
		names = append(names, d.name)
	}
	accept := strings.Join(names, ", ")
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !hasBody(r) {
				next.ServeHTTP(w, r)
				return
			}
			var codings []string
			for _, c := range httpheader.SplitList(r.Header.Values("Content-Encoding")) {
				if c = strings.ToLower(c); c != "identity" {
					codings = append(codings, c)
				}
			}
			if len(codings) == 0 {
				next.ServeHTTP(w, r)
				return
			}
			d, ok := decoders[codings[0]]
			if !ok || len(codings) > 1 {
				w.Header().Set("Accept-Encoding", accept)
				respwriter.Reject(w, r, cfg.reject, middleware.Rejection{Status: http.StatusUnsupportedMediaType, Err: ErrUnsupportedEncoding})
				return
			}
			r.Header.Del("Content-Encoding")
			r.Header.Del("Content-Length")
			r.ContentLength = -1
			r.Body = http.MaxBytesReader(w, &lazyReader{src: r.Body, newReader: d.newReader}, cfg.maxSize)
			next.ServeHTTP(w, r)
		})
	}, nil
}

func hasBody(r *http.Request) bool {
	return r.ContentLength > 0 || (r.ContentLength == -1 && r.Body != nil && r.Body != http.NoBody)
}

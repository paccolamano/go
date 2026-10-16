// Package compress compresses response bodies according to the client's
// Accept-Encoding.
//
// gzip and deflate are built in; other codings, such as brotli or zstd, can be
// added with [WithCoding] using an implementation chosen by the caller. The
// coding is negotiated with quality values; when nothing acceptable is
// supported, or the request has no Accept-Encoding, the response is sent
// uncompressed.
//
// A response is compressed only when its media type is compressible, its size
// reaches the minimum (or the handler flushes, which means it is streaming),
// it is not already encoded, it is not partial content, its status and the
// request method allow a body, and it does not carry
// Cache-Control: no-transform. Compressed responses declare their coding, lose
// their Content-Length, and have a strong ETag weakened, so that it differs
// from the uncompressed representation while If-None-Match keeps matching.
// Every response carries Vary: Accept-Encoding. Flushes requested by the
// handler deliver the data written so far immediately, compressed.
//
// Compressing secrets together with data controlled by an attacker in the
// same response can leak the secrets (BREACH-style attacks). Exclude such
// responses by not applying the middleware to their routes, by their media
// type, or by setting Cache-Control: no-transform on them.
//
// Mandatory settings: none.
//
// Options and defaults:
//   - [WithLevel]: flate.DefaultCompression.
//   - [WithMinSize]: 1024 bytes.
//   - [WithContentTypes]: text/*, application/json, application/*+json,
//     application/javascript, application/xml, application/*+xml,
//     application/wasm, image/svg+xml.
//   - [WithCoding]: none beyond gzip and deflate.
//   - [WithPreference]: custom codings in registration order, then gzip, then
//     deflate.
//
// Recommended position: near the handler, just outside etag, so that etag
// computes its tag on the uncompressed content and compress weakens it.
package compress

import (
	"io"
	"net/http"
	"sync"

	"github.com/paccolamano/go/middleware"
	"github.com/paccolamano/go/middleware/internal/httpheader"
	"github.com/paccolamano/go/middleware/internal/mediatype"
)

// Writer is a compressing stream for one content coding. *gzip.Writer and
// *zlib.Writer satisfy it, as do the writers of the common brotli and zstd
// libraries.
type Writer interface {
	io.WriteCloser
	// Flush writes any pending compressed data to the underlying writer.
	Flush() error
	// Reset discards the state and writes to w from now on, so that writers
	// can be reused.
	Reset(w io.Writer)
}

type encoder struct {
	name      string
	newWriter func(w io.Writer) (Writer, error)
	pool      *sync.Pool
}

type compressor struct {
	encoders   map[string]*encoder
	preference []string
	types      mediatype.Matcher
	minSize    int
}

// New returns a middleware that compresses responses.
func New(opts ...Option) (middleware.Middleware, error) {
	cfg := defaultConfig()
	for _, opt := range opts {
		opt(&cfg)
	}
	codings, types, err := cfg.codings()
	if err != nil {
		return nil, err
	}
	c := &compressor{encoders: map[string]*encoder{}, types: types, minSize: cfg.minSize}
	for _, k := range codings {
		c.encoders[k.name] = &encoder{name: k.name, newWriter: k.newWriter, pool: &sync.Pool{}}
		c.preference = append(c.preference, k.name)
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			httpheader.AddVary(w.Header(), "Accept-Encoding")
			name := httpheader.Negotiate(r.Header.Values("Accept-Encoding"), c.preference)
			if name == "" || r.Method == http.MethodHead {
				next.ServeHTTP(w, r)
				return
			}
			cw := &responseWriter{w: w, c: c, enc: c.encoders[name]}
			next.ServeHTTP(cw, r)
			// Not deferred: after a panic the buffered data must not be sent,
			// so that an outer recovery can still answer 500.
			cw.close()
		})
	}, nil
}

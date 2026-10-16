// Package respwriter provides an http.ResponseWriter wrapper that observes the
// response (status, size, start, hijack) without hiding the capabilities of
// the underlying writer, and the shared implementation of the refusal
// standard.
package respwriter

import (
	"bufio"
	"fmt"
	"net"
	"net/http"
)

// Writer wraps an http.ResponseWriter and records what the handler does with
// it. Flush, Hijack and Unwrap keep the underlying capabilities reachable,
// including through http.ResponseController.
type Writer struct {
	w        http.ResponseWriter
	status   int
	bytes    int64
	started  bool
	hijacked bool
}

// New returns a Writer that observes w.
func New(w http.ResponseWriter) *Writer {
	return &Writer{w: w}
}

// Header returns the header map of the underlying writer.
func (w *Writer) Header() http.Header {
	return w.w.Header()
}

// WriteHeader forwards the status code. Informational codes (1xx except 101)
// are forwarded without being recorded; the first other code is recorded as
// the final status.
func (w *Writer) WriteHeader(code int) {
	if code >= 100 && code <= 199 && code != http.StatusSwitchingProtocols {
		w.w.WriteHeader(code)
		return
	}
	if !w.started {
		w.status = code
		w.started = true
	}
	w.w.WriteHeader(code)
}

// Write forwards p and counts the bytes written. A write without a prior
// status records 200.
func (w *Writer) Write(p []byte) (int, error) {
	if !w.started {
		w.status = http.StatusOK
		w.started = true
	}
	n, err := w.w.Write(p)
	w.bytes += int64(n)
	return n, err //nolint:wrapcheck // io contract: io.Writer errors are returned unchanged
}

// Flush sends any buffered data to the client. It implements http.Flusher; a
// flush failure is reported by FlushError.
func (w *Writer) Flush() {
	// http.Flusher has no error result; the error stays observable through
	// FlushError, which http.ResponseController prefers.
	_ = w.FlushError()
}

// FlushError flushes the underlying writer and reports any failure.
func (w *Writer) FlushError() error {
	if err := http.NewResponseController(w.w).Flush(); err != nil {
		return fmt.Errorf("respwriter: flush: %w", err)
	}
	return nil
}

// Hijack takes over the underlying connection. After a successful hijack the
// response counts as started, with status 101 if none was set.
func (w *Writer) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	conn, brw, err := http.NewResponseController(w.w).Hijack()
	if err != nil {
		return nil, nil, fmt.Errorf("respwriter: hijack: %w", err)
	}
	w.hijacked = true
	w.started = true
	if w.status == 0 {
		w.status = http.StatusSwitchingProtocols
	}
	return conn, brw, nil
}

// Unwrap returns the underlying writer, for http.ResponseController.
func (w *Writer) Unwrap() http.ResponseWriter {
	return w.w
}

// Status returns the final status code, or 0 if none has been written.
func (w *Writer) Status() int { return w.status }

// Bytes returns the number of body bytes written.
func (w *Writer) Bytes() int64 { return w.bytes }

// Started reports whether the response has started (status or body written,
// or connection hijacked).
func (w *Writer) Started() bool { return w.started }

// Hijacked reports whether the connection was hijacked.
func (w *Writer) Hijacked() bool { return w.hijacked }

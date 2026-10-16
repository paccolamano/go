package compress

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"

	"github.com/paccolamano/go/middleware/internal/httpheader"
)

type state int

const (
	statePending state = iota
	statePassthrough
	stateCompressing
	stateDetached
)

// responseWriter buffers the beginning of a response until it can decide
// whether to compress it.
type responseWriter struct {
	w      http.ResponseWriter
	c      *compressor
	enc    *encoder
	state  state
	status int
	buf    []byte
	zw     Writer
}

func (cw *responseWriter) Header() http.Header { return cw.w.Header() }

func (cw *responseWriter) WriteHeader(code int) {
	if code >= 100 && code <= 199 && code != http.StatusSwitchingProtocols {
		cw.w.WriteHeader(code)
		return
	}
	if cw.state != statePending {
		cw.w.WriteHeader(code) // superfluous call; net/http reports it
		return
	}
	if cw.status == 0 {
		cw.status = code
	}
}

func (cw *responseWriter) Write(p []byte) (int, error) {
	switch cw.state {
	case statePassthrough:
		return cw.w.Write(p) //nolint:wrapcheck // io contract: io.Writer errors are returned unchanged
	case stateCompressing:
		return cw.zw.Write(p) //nolint:wrapcheck // io contract: io.Writer errors are returned unchanged
	case stateDetached:
		return 0, http.ErrHijacked
	}
	if cw.status == 0 {
		cw.status = http.StatusOK
	}
	if !cw.eligibleStatic() {
		cw.startPassthrough()
		return cw.w.Write(p) //nolint:wrapcheck // io contract: io.Writer errors are returned unchanged
	}
	if cl := cw.w.Header().Get("Content-Length"); cl != "" {
		if n, err := strconv.Atoi(cl); err == nil && n < cw.c.minSize {
			cw.startPassthrough()
			return cw.w.Write(p) //nolint:wrapcheck // io contract: io.Writer errors are returned unchanged
		}
		cw.decide(p)
		return cw.Write(p)
	}
	cw.buf = append(cw.buf, p...)
	if len(cw.buf) >= cw.c.minSize {
		if err := cw.decideAndDrain(); err != nil {
			return 0, err
		}
	}
	return len(p), nil
}

// Flush decides immediately (a flush means the response is streamed), sends
// what is buffered and flushes the compressor and the connection.
func (cw *responseWriter) Flush() {
	// http.Flusher has no error result; FlushError reports failures to
	// http.ResponseController.
	_ = cw.FlushError()
}

// FlushError is Flush with error reporting, used by http.ResponseController.
func (cw *responseWriter) FlushError() error {
	switch cw.state {
	case stateDetached:
		return http.ErrHijacked
	case statePending:
		if cw.status == 0 {
			cw.status = http.StatusOK
		}
		if err := cw.decideAndDrain(); err != nil {
			return err
		}
	}
	if cw.state == stateCompressing {
		if err := cw.zw.Flush(); err != nil {
			return fmt.Errorf("compress: flush: %w", err)
		}
	}
	if err := http.NewResponseController(cw.w).Flush(); err != nil {
		return fmt.Errorf("compress: flush: %w", err)
	}
	return nil
}

// Hijack hands the connection over to the handler; the writer stops writing.
func (cw *responseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	conn, brw, err := http.NewResponseController(cw.w).Hijack()
	if err != nil {
		return nil, nil, fmt.Errorf("compress: hijack: %w", err)
	}
	cw.state = stateDetached
	return conn, brw, nil
}

// Unwrap returns the underlying writer, for http.ResponseController.
func (cw *responseWriter) Unwrap() http.ResponseWriter { return cw.w }

// close finishes the response after the handler returned.
func (cw *responseWriter) close() {
	switch cw.state {
	case statePending:
		if cw.status == 0 && len(cw.buf) == 0 {
			return // nothing written: net/http sends its default response
		}
		if cw.status == 0 {
			cw.status = http.StatusOK
		}
		cw.startPassthrough()
		if len(cw.buf) > 0 {
			_, _ = cw.w.Write(cw.buf) // the handler has returned: nobody can observe the error
		}
	case stateCompressing:
		_ = cw.zw.Close() // the handler has returned: nobody can observe the error
		cw.zw.Reset(io.Discard)
		cw.enc.pool.Put(cw.zw)
	}
}

// eligibleStatic checks the conditions that do not depend on the content.
func (cw *responseWriter) eligibleStatic() bool {
	h := cw.w.Header()
	switch {
	case cw.status < 200, cw.status == http.StatusNoContent, cw.status == http.StatusPartialContent,
		cw.status == http.StatusNotModified:
		return false
	case h.Get("Content-Encoding") != "", h.Get("Content-Range") != "":
		return false
	case httpheader.HasToken(h, "Cache-Control", "no-transform"):
		return false
	}
	return true
}

// decide chooses between compressing and passing through, sniffing the
// Content-Type from sample when the handler did not set one.
func (cw *responseWriter) decide(sample []byte) {
	if !cw.eligibleStatic() || !cw.compressible(sample) {
		cw.startPassthrough()
		return
	}
	cw.startCompressing()
}

func (cw *responseWriter) compressible(sample []byte) bool {
	h := cw.w.Header()
	ct, present := h["Content-Type"]
	if !present {
		if len(sample) == 0 {
			return false
		}
		// Set it as net/http would, but from the uncompressed bytes.
		sniffed := http.DetectContentType(sample)
		h.Set("Content-Type", sniffed)
		return cw.c.types.Match(sniffed)
	}
	return len(ct) > 0 && cw.c.types.Match(ct[0])
}

func (cw *responseWriter) decideAndDrain() error {
	cw.decide(cw.buf)
	buf := cw.buf
	cw.buf = nil
	if len(buf) == 0 {
		return nil
	}
	var err error
	if cw.state == stateCompressing {
		_, err = cw.zw.Write(buf)
	} else {
		_, err = cw.w.Write(buf)
	}
	if err != nil {
		return fmt.Errorf("compress: write: %w", err)
	}
	return nil
}

func (cw *responseWriter) startPassthrough() {
	cw.state = statePassthrough
	cw.w.WriteHeader(cw.status)
}

func (cw *responseWriter) startCompressing() {
	zw, err := cw.writer()
	if err != nil {
		// The coding cannot be produced: the response is still served,
		// uncompressed, which is always acceptable to the client.
		cw.startPassthrough()
		return
	}
	h := cw.w.Header()
	h.Del("Content-Length")
	h.Set("Content-Encoding", cw.enc.name)
	if et := h.Get("ETag"); et != "" && !strings.HasPrefix(et, "W/") {
		h.Set("ETag", "W/"+et)
	}
	cw.zw = zw
	cw.state = stateCompressing
	cw.w.WriteHeader(cw.status)
}

func (cw *responseWriter) writer() (Writer, error) {
	if zw, ok := cw.enc.pool.Get().(Writer); ok {
		zw.Reset(cw.w)
		return zw, nil
	}
	zw, err := cw.enc.newWriter(cw.w)
	if err != nil {
		return nil, fmt.Errorf("compress: %s: %w", cw.enc.name, err)
	}
	return zw, nil
}

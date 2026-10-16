package etag

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"net"
	"net/http"

	"github.com/paccolamano/go/middleware/internal/httpheader"
)

type state int

const (
	stateBuffering state = iota
	statePassthrough
	stateNotModified
	stateDetached
)

// responseWriter buffers a 200 response to compute its entity tag, and
// answers 304 when the request's If-None-Match matches.
type responseWriter struct {
	w       http.ResponseWriter
	r       *http.Request
	maxSize int
	weak    bool
	state   state
	status  int
	buf     bytes.Buffer
}

func (ew *responseWriter) Header() http.Header { return ew.w.Header() }

func (ew *responseWriter) WriteHeader(code int) {
	if code >= 100 && code <= 199 && code != http.StatusSwitchingProtocols {
		ew.w.WriteHeader(code)
		return
	}
	switch ew.state {
	case stateBuffering:
		if ew.status != 0 {
			return // superfluous call
		}
		ew.status = code
		h := ew.w.Header()
		switch {
		case code != http.StatusOK || h.Get("Content-Range") != "":
			ew.startPassthrough()
		case h.Get("ETag") != "":
			if httpheader.MatchIfNoneMatch(ew.r.Header.Values("If-None-Match"), h.Get("ETag")) {
				ew.notModified()
			} else {
				ew.startPassthrough()
			}
		}
	case statePassthrough:
		ew.w.WriteHeader(code) // superfluous call; net/http reports it
	}
}

func (ew *responseWriter) Write(p []byte) (int, error) {
	if ew.state == stateBuffering && ew.status == 0 {
		ew.WriteHeader(http.StatusOK)
	}
	switch ew.state {
	case stateBuffering:
		if ew.buf.Len()+len(p) > ew.maxSize {
			if err := ew.drain(); err != nil {
				return 0, err
			}
			return ew.w.Write(p) //nolint:wrapcheck // io contract: io.Writer errors are returned unchanged
		}
		return ew.buf.Write(p) //nolint:wrapcheck // io contract: bytes.Buffer never fails
	case stateNotModified:
		return len(p), nil
	case stateDetached:
		return 0, http.ErrHijacked
	}
	return ew.w.Write(p) //nolint:wrapcheck // io contract: io.Writer errors are returned unchanged
}

// Flush gives up on the tag: a flushed response is being streamed.
func (ew *responseWriter) Flush() {
	// http.Flusher has no error result; FlushError reports failures to
	// http.ResponseController.
	_ = ew.FlushError()
}

// FlushError is Flush with error reporting, used by http.ResponseController.
func (ew *responseWriter) FlushError() error {
	switch ew.state {
	case stateDetached:
		return http.ErrHijacked
	case stateNotModified:
		return nil
	case stateBuffering:
		if ew.status == 0 {
			ew.status = http.StatusOK
		}
		if err := ew.drain(); err != nil {
			return err
		}
	}
	if err := http.NewResponseController(ew.w).Flush(); err != nil {
		return fmt.Errorf("etag: flush: %w", err)
	}
	return nil
}

// Hijack hands the connection over to the handler; the writer stops writing.
func (ew *responseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	conn, brw, err := http.NewResponseController(ew.w).Hijack()
	if err != nil {
		return nil, nil, fmt.Errorf("etag: hijack: %w", err)
	}
	ew.state = stateDetached
	return conn, brw, nil
}

// Unwrap returns the underlying writer, for http.ResponseController.
func (ew *responseWriter) Unwrap() http.ResponseWriter { return ew.w }

// drain switches to pass-through and sends what is buffered, without a tag.
func (ew *responseWriter) drain() error {
	ew.startPassthrough()
	if ew.buf.Len() == 0 {
		return nil
	}
	_, err := ew.w.Write(ew.buf.Bytes())
	ew.buf = bytes.Buffer{}
	if err != nil {
		return fmt.Errorf("etag: write: %w", err)
	}
	return nil
}

func (ew *responseWriter) startPassthrough() {
	ew.state = statePassthrough
	ew.w.WriteHeader(ew.status)
}

func (ew *responseWriter) notModified() {
	ew.state = stateNotModified
	h := ew.w.Header()
	h.Del("Content-Type")
	h.Del("Content-Length")
	h.Del("Content-Encoding")
	ew.w.WriteHeader(http.StatusNotModified)
}

// finish computes the tag of a fully buffered response and sends it.
func (ew *responseWriter) finish() {
	if ew.state != stateBuffering {
		return
	}
	if ew.status == 0 && ew.buf.Len() == 0 && ew.r.Method == http.MethodHead {
		return // nothing written for HEAD: the content is unknown
	}
	if ew.status == 0 {
		ew.status = http.StatusOK
	}
	if ew.r.Method == http.MethodHead && ew.buf.Len() == 0 {
		ew.startPassthrough()
		return
	}
	tag := ew.tag()
	ew.w.Header().Set("ETag", tag)
	if httpheader.MatchIfNoneMatch(ew.r.Header.Values("If-None-Match"), tag) {
		ew.notModified()
		return
	}
	ew.startPassthrough()
	_, _ = ew.w.Write(ew.buf.Bytes()) // the handler has returned: nobody can observe the error
}

func (ew *responseWriter) tag() string {
	sum := sha256.Sum256(ew.buf.Bytes())
	tag := `"` + base64.RawURLEncoding.EncodeToString(sum[:16]) + `"`
	if ew.weak {
		return "W/" + tag
	}
	return tag
}

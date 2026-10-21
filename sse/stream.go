package sse

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"
)

// ErrStreamingUnsupported reports a response writer that cannot flush, such as
// the one of http.TimeoutHandler. Nothing has been written when it is
// returned, so the handler can still answer with an error of its choice.
var ErrStreamingUnsupported = errors.New("sse: response writer cannot flush")

// Stream writes events to one HTTP response. It is safe for concurrent use,
// and each event reaches the client whole.
//
// A stream uses the context of its request: once the client has gone, sends
// fail without writing. Each write has its own deadline (WithWriteTimeout), so
// the server's WriteTimeout does not end the stream and idle periods of any
// length are allowed. After a failed write, every call returns that error.
type Stream struct {
	mu  sync.Mutex
	w   http.ResponseWriter
	rc  *http.ResponseController
	ctx context.Context // the request's: a stream lives as long as its request

	writeTimeout time.Duration
	noDeadline   bool // the writer does not support write deadlines
	buf          []byte
	err          error
}

// NewStream sets the event stream headers (Content-Type: text/event-stream,
// Cache-Control: no-store and X-Accel-Buffering: no), keeps the other headers
// already set, sends status 200 and flushes, so that the client sees the
// connection open at once. When the writer cannot flush it returns
// ErrStreamingUnsupported and writes nothing.
//
// Accepted options: WithRetry and WithWriteTimeout.
func NewStream(w http.ResponseWriter, r *http.Request, opts ...Option) (*Stream, error) {
	cfg, err := newConfig("NewStream", streamOptions, opts)
	if err != nil {
		return nil, err
	}
	var first []byte
	if cfg.retry > 0 {
		first = append(appendRetry(nil, cfg.retry), '\n')
	}
	return newStream(w, r, cfg, first)
}

// newStream opens a stream and writes first, if any, together with the
// headers.
func newStream(w http.ResponseWriter, r *http.Request, cfg config, first []byte) (*Stream, error) {
	if !canFlush(w) {
		return nil, ErrStreamingUnsupported
	}
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-store")
	h.Set("X-Accel-Buffering", "no")
	s := &Stream{w: w, rc: http.NewResponseController(w), ctx: r.Context(), writeTimeout: cfg.writeTimeout}
	w.WriteHeader(http.StatusOK)
	if err := s.write(first); err != nil {
		return nil, err
	}
	return s, nil
}

// canFlush reports whether w, or a writer it wraps, can flush, without writing
// anything: it walks the Unwrap chain as http.ResponseController does.
func canFlush(w http.ResponseWriter) bool {
	for {
		switch t := w.(type) {
		case interface{ FlushError() error }, http.Flusher:
			return true
		case interface{ Unwrap() http.ResponseWriter }:
			w = t.Unwrap()
		default:
			return false
		}
	}
}

// Send writes and flushes one event. For an invalid event it returns an error
// wrapping ErrInvalidEvent, writes nothing, and the stream stays usable.
func (s *Stream) Send(ev Event) error {
	if err := ev.validate(); err != nil {
		return err
	}
	return s.sendValid(ev)
}

// sendValid writes an event that has already been validated.
func (s *Stream) sendValid(ev Event) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.buf = appendEvent(s.buf[:0], ev)
	return s.writeLocked(s.buf)
}

// KeepAlive writes a comment line that clients ignore. It keeps proxies from
// closing a connection on which no event is sent for a while.
func (s *Stream) KeepAlive() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.buf = append(s.buf[:0], keepAliveLine...)
	return s.writeLocked(s.buf)
}

func (s *Stream) write(b []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.writeLocked(b)
}

// writeLocked writes b and flushes, within the write timeout. The deadline is
// cleared after a successful flush, so idle periods never expire it
// (research R-07).
func (s *Stream) writeLocked(b []byte) error {
	if s.err != nil {
		return s.err
	}
	if err := s.ctx.Err(); err != nil {
		return fmt.Errorf("sse: request ended: %w", err)
	}
	if err := s.setDeadline(time.Now().Add(s.writeTimeout)); err != nil {
		return s.fail(err)
	}
	if len(b) > 0 {
		if _, err := s.w.Write(b); err != nil {
			return s.fail(fmt.Errorf("sse: write: %w", err))
		}
	}
	if err := s.rc.Flush(); err != nil {
		return s.fail(fmt.Errorf("sse: flush: %w", err))
	}
	// b has been delivered. A deadline that cannot be cleared means that the
	// connection has just closed: report it on the next write, not on this one.
	if err := s.setDeadline(time.Time{}); err != nil {
		s.err = err
	}
	return nil
}

// setDeadline sets the write deadline, tolerating writers that do not support
// one: the server's WriteTimeout, if any, then bounds the stream.
func (s *Stream) setDeadline(t time.Time) error {
	if s.noDeadline {
		return nil
	}
	err := s.rc.SetWriteDeadline(t)
	switch {
	case err == nil:
		return nil
	case errors.Is(err, http.ErrNotSupported):
		s.noDeadline = true // do not ask again: the answer will not change
		return nil
	}
	return fmt.Errorf("sse: set write deadline: %w", err)
}

// fail records the first write failure. Later calls return it.
func (s *Stream) fail(err error) error {
	s.err = err
	return err
}

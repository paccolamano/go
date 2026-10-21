package sse

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"iter"
	"strconv"
	"time"
)

// ErrEventTooLarge reports an event, or a line of the stream, larger than the
// maximum event size.
var ErrEventTooLarge = errors.New("sse: event too large")

// Decode returns the events of an event stream, interpreted as the standard
// defines: lines end with CRLF, LF or CR, comments and unknown fields are
// ignored, and an event is dispatched at each empty line. The last event
// identifier persists across events. Invalid UTF-8 is replaced with U+FFFD.
//
// Iteration stops at the end of r, discarding an unterminated event, or yields
// one error: a read error, ErrEventTooLarge, or an invalid option. Decode reads
// r without a context: cancellation is the reader's, as for an HTTP response
// body.
//
// Accepted option: WithMaxEventSize.
func Decode(r io.Reader, opts ...Option) iter.Seq2[Event, error] {
	return func(yield func(Event, error) bool) {
		cfg, err := newConfig("Decode", decodeOptions, opts)
		if err != nil {
			yield(Event{}, err)
			return
		}
		p := newParser(r, cfg.maxEventSize)
		for {
			ev, err := p.next()
			if errors.Is(err, io.EOF) {
				return
			}
			if err != nil {
				yield(Event{}, err)
				return
			}
			if !yield(ev, nil) {
				return
			}
		}
	}
}

// parser interprets an event stream (research R-14).
type parser struct {
	r        *bufio.Reader
	max      int
	atStart  bool // the first line may start with a byte order mark
	afterCR  bool // the previous line ended with CR: skip an immediate LF
	line     []byte
	text     []byte
	data     []byte
	typ      []byte
	idBuf    string
	lastID   string
	retry    time.Duration
	retrySet bool
	err      error
}

func newParser(r io.Reader, maxEventSize int) *parser {
	return &parser{r: bufio.NewReader(r), max: maxEventSize, atStart: true}
}

// next returns the next event, io.EOF at the end of the stream, or the error
// that ended it. Once it returns an error, it returns the same error again.
func (p *parser) next() (Event, error) {
	for p.err == nil {
		line, err := p.readLine()
		if err != nil {
			p.err = err
			break
		}
		ev, ok, err := p.process(line)
		if err != nil {
			p.err = err
			break
		}
		if ok {
			return ev, nil
		}
	}
	return Event{}, p.err
}

// readLine returns the next line without its terminator. A CR ends a line at
// once: the decoder never waits for the next byte to see whether it is an LF.
func (p *parser) readLine() ([]byte, error) {
	p.line = p.line[:0]
	for {
		b, err := p.r.ReadByte()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil, io.EOF
			}
			return nil, fmt.Errorf("sse: read stream: %w", err)
		}
		if p.afterCR {
			p.afterCR = false
			if b == '\n' {
				continue
			}
		}
		switch b {
		case '\r':
			p.afterCR = true
			return p.endLine(), nil
		case '\n':
			return p.endLine(), nil
		}
		if len(p.line) >= p.max {
			return nil, fmt.Errorf("%w: line longer than %d bytes", ErrEventTooLarge, p.max)
		}
		p.line = append(p.line, b)
	}
}

func (p *parser) endLine() []byte {
	if p.atStart {
		p.atStart = false
		return bytes.TrimPrefix(p.line, []byte("\xEF\xBB\xBF"))
	}
	return p.line
}

// process interprets one line, and reports the event it dispatches, if any.
func (p *parser) process(raw []byte) (Event, bool, error) {
	p.text = appendValidUTF8(p.text[:0], raw)
	line := p.text
	if len(line) == 0 {
		ev, ok := p.dispatch()
		return ev, ok, nil
	}
	if line[0] == ':' {
		return Event{}, false, nil
	}
	name, value := line, []byte(nil)
	if i := bytes.IndexByte(line, ':'); i >= 0 {
		name, value = line[:i], line[i+1:]
		value = bytes.TrimPrefix(value, []byte(" "))
	}
	switch string(name) {
	case "event":
		p.typ = append(p.typ[:0], value...)
	case "data":
		p.data = append(p.data, value...)
		p.data = append(p.data, '\n')
	case "id":
		if bytes.IndexByte(value, 0) < 0 {
			p.idBuf = string(value)
		}
	case "retry":
		if ms, ok := parseDigits(value); ok {
			p.retry, p.retrySet = time.Duration(ms)*time.Millisecond, true
		}
	}
	if n := len(p.data) + len(p.typ) + len(p.idBuf); n > p.max {
		return Event{}, false, fmt.Errorf("%w: more than %d bytes", ErrEventTooLarge, p.max)
	}
	return Event{}, false, nil
}

// dispatch ends the pending event at an empty line.
func (p *parser) dispatch() (Event, bool) {
	p.lastID = p.idBuf
	if len(p.data) == 0 {
		p.typ = p.typ[:0]
		return Event{}, false
	}
	ev := Event{ID: p.lastID, Type: string(p.typ), Data: string(p.data[:len(p.data)-1])}
	p.data, p.typ = p.data[:0], p.typ[:0]
	return ev, true
}

// parseDigits parses a non-empty value made only of ASCII digits.
func parseDigits(b []byte) (int64, bool) {
	if len(b) == 0 {
		return 0, false
	}
	for _, c := range b {
		if c < '0' || c > '9' {
			return 0, false
		}
	}
	n, err := strconv.ParseInt(string(b), 10, 64)
	if err != nil || n > int64(time.Duration(1<<63-1)/time.Millisecond) {
		return 0, false
	}
	return n, true
}

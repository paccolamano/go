package sse

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// ErrInvalidEvent is wrapped by the errors that report an event that cannot be
// sent: an identifier with NUL, CR or LF, a type with CR or LF, or text that is
// not valid UTF-8.
var ErrInvalidEvent = errors.New("sse: invalid event")

// Event is a server-sent event. ID and Type are optional. Data may contain line
// breaks: each line is sent as its own data line, and clients receive line
// feeds.
//
// A decoded event carries the stream's last event identifier when it was
// dispatched, which may come from an earlier event, and an empty Type when the
// stream set none.
type Event struct {
	// ID is the event identifier, sent back by clients that reconnect. It must
	// be valid UTF-8 without NUL, CR or LF.
	ID string
	// Type is the "event" field. Empty means "message". It must be valid UTF-8
	// without CR or LF.
	Type string
	// Data is the payload. It must be valid UTF-8.
	Data string
}

// keepAliveLine is the comment written to keep idle connections open.
const keepAliveLine = ": keep-alive\n"

// validate reports why ev cannot be sent.
func (ev Event) validate() error {
	switch {
	case !utf8.ValidString(ev.ID):
		return fmt.Errorf("%w: ID is not valid UTF-8", ErrInvalidEvent)
	case strings.ContainsRune(ev.ID, 0):
		return fmt.Errorf("%w: ID contains NUL", ErrInvalidEvent)
	case strings.ContainsRune(ev.ID, '\r'):
		return fmt.Errorf("%w: ID contains a carriage return", ErrInvalidEvent)
	case strings.ContainsRune(ev.ID, '\n'):
		return fmt.Errorf("%w: ID contains a line feed", ErrInvalidEvent)
	case !utf8.ValidString(ev.Type):
		return fmt.Errorf("%w: type is not valid UTF-8", ErrInvalidEvent)
	case strings.ContainsAny(ev.Type, "\r\n"):
		return fmt.Errorf("%w: type contains a line break", ErrInvalidEvent)
	case !utf8.ValidString(ev.Data):
		return fmt.Errorf("%w: data is not valid UTF-8", ErrInvalidEvent)
	}
	return nil
}

// appendField appends one field line. The separator space is written only
// before a non-empty value, so a value that starts with spaces keeps them:
// clients remove exactly one.
func appendField(dst []byte, name, value string) []byte {
	dst = append(dst, name...)
	dst = append(dst, ':')
	if value != "" {
		dst = append(dst, ' ')
		dst = append(dst, value...)
	}
	return append(dst, '\n')
}

// appendEvent appends the block of a valid event.
func appendEvent(dst []byte, ev Event) []byte {
	if ev.Type != "" {
		dst = appendField(dst, "event", ev.Type)
	}
	if ev.ID != "" {
		dst = appendField(dst, "id", ev.ID)
	}
	return appendData(dst, ev.Data)
}

// appendResetEvent appends a reset event. Its id field is always written, even
// empty, so that it replaces the client's unresolvable identifier.
func appendResetEvent(dst []byte, ev Event) []byte {
	if ev.Type != "" {
		dst = appendField(dst, "event", ev.Type)
	}
	dst = appendField(dst, "id", ev.ID)
	return appendData(dst, ev.Data)
}

// appendData appends one data line per line of data, split at CRLF, CR and LF,
// and the empty line that ends the block.
func appendData(dst []byte, data string) []byte {
	for {
		i := strings.IndexAny(data, "\r\n")
		if i < 0 {
			dst = appendField(dst, "data", data)
			break
		}
		dst = appendField(dst, "data", data[:i])
		if data[i] == '\r' && i+1 < len(data) && data[i+1] == '\n' {
			i++
		}
		data = data[i+1:]
	}
	return append(dst, '\n')
}

// appendRetry appends the reconnection time field, in whole milliseconds.
func appendRetry(dst []byte, d time.Duration) []byte {
	return appendField(dst, "retry", strconv.FormatInt(d.Milliseconds(), 10))
}

// appendPosition appends a block that sets the client's last event identifier
// without dispatching an event. An empty id clears it.
func appendPosition(dst []byte, id string) []byte {
	return append(appendField(dst, "id", id), '\n')
}

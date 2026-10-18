package problem

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"slices"
	"strconv"
)

// Write answers the request with err as a problem document.
//
//   - The first [*Error] in err's tree, in the order of [errors.As], is written
//     as is. The context of wrapping levels, the internal cause and the other
//     errors of the tree never reach the response.
//   - Any other error is answered with the generic server error: status 500,
//     type "about:blank" and title "Internal Server Error", with no other
//     member.
//   - A problem that cannot be written, because a value given while building it
//     was invalid or its status is outside 400–599, is replaced by the generic
//     server error.
//
// The document is complete before anything is written. Headers already set on
// w are kept, except Content-Type, which becomes [MediaType]. A HEAD request
// gets the status and the headers, and no body. Once the response has started,
// its status can no longer change: call Write before writing anything else.
//
// The returned error reports a replaced problem (it wraps [ErrInvalid]) and any
// failure to write the body. It is nil when err was written as intended,
// including when an error that is not a problem was answered with the generic
// server error.
func Write(w http.ResponseWriter, r *http.Request, err error) error {
	var (
		e      *Error
		body   []byte
		report error
	)
	found := errors.As(err, &e)
	switch {
	case err == nil:
		report = fmt.Errorf("%w: no error to write", ErrInvalid)
	case found && e == nil:
		report = fmt.Errorf("%w: nil *Error in the error tree", ErrInvalid)
	case found:
		body, report = e.document()
	}
	if body == nil {
		e = serverError()
		body, _ = e.document()
	}

	w.Header().Set("Content-Type", MediaType)
	w.WriteHeader(e.status)
	if r != nil && r.Method == http.MethodHead {
		return report
	}
	if _, werr := w.Write(body); werr != nil {
		return errors.Join(report, fmt.Errorf("problem: write response: %w", werr))
	}
	return report
}

// document encodes e as a problem document, or returns why it cannot.
func (e *Error) document() ([]byte, error) {
	if e.invalid != nil {
		return nil, e.invalid
	}
	if e.status < 400 || e.status > 599 {
		return nil, fmt.Errorf("%w: status %d is outside 400-599", ErrInvalid, e.status)
	}
	var b bytes.Buffer
	b.WriteString(`{"type":`)
	writeString(&b, e.typ)
	if e.title != "" {
		b.WriteString(`,"title":`)
		writeString(&b, e.title)
	}
	b.WriteString(`,"status":`)
	b.WriteString(strconv.Itoa(e.status))
	if e.detail != "" {
		b.WriteString(`,"detail":`)
		writeString(&b, e.detail)
	}
	if e.instance != "" {
		b.WriteString(`,"instance":`)
		writeString(&b, e.instance)
	}
	for _, name := range slices.Sorted(maps.Keys(e.ext)) {
		b.WriteByte(',')
		writeString(&b, name)
		b.WriteByte(':')
		b.Write(e.ext[name])
	}
	b.WriteByte('}')
	return b.Bytes(), nil
}

// writeString writes s as a JSON string. encoding/json escapes '<', '>' and '&',
// so that a browser sniffing the content never finds markup.
func writeString(b *bytes.Buffer, s string) {
	raw, _ := json.Marshal(s) // marshaling a string cannot fail
	b.Write(raw)
}

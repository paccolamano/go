package problem

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"mime"
	"net/http"
)

// maxDocumentSize is the largest problem document Read and Parse accept: 1 MiB.
const maxDocumentSize = 1 << 20

// Read reads the problem document in the body of resp, typically a response
// from another API.
//
// Read returns an error wrapping [ErrNotProblem], without reading the body,
// when resp is nil or its media type is not [MediaType] (type and subtype
// compared without regard to case, parameters ignored). It returns an error
// wrapping [ErrMalformed] when the body is missing, larger than 1 MiB, not valid
// JSON or not a JSON object. It reads at most 1 MiB plus one byte, and does not
// close the body.
//
// The members are read as [Parse] reads them. In addition, relative "type" and
// "instance" members are resolved against the URL of resp.Request, the last
// request of any redirect chain. The status code of resp is not consulted: the
// caller can compare it with [Error.Status].
func Read(resp *http.Response) (*Error, error) {
	if resp == nil {
		return nil, fmt.Errorf("%w: nil response", ErrNotProblem)
	}
	ct := resp.Header.Get("Content-Type")
	if mt, _, err := mime.ParseMediaType(ct); err != nil || mt != MediaType {
		return nil, fmt.Errorf("%w: media type %q", ErrNotProblem, ct)
	}
	if resp.Body == nil {
		return nil, fmt.Errorf("%w: no body", ErrMalformed)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxDocumentSize+1))
	if err != nil {
		return nil, fmt.Errorf("problem: read body: %w", err)
	}
	e, err := Parse(data)
	if err != nil {
		return nil, err
	}
	if resp.Request != nil && resp.Request.URL != nil {
		e.typ = resolve(resp.Request.URL, e.typ)
		e.instance = resolve(resp.Request.URL, e.instance)
	}
	return e, nil
}

// Parse reads a problem document, without resolving relative URIs. It returns
// an error wrapping [ErrMalformed] when data is larger than 1 MiB, is not valid
// JSON or is not a JSON object.
//
// Members are matched by their exact name, and read with the rules of RFC 9457
// for consumers:
//
//   - "type" is kept when it is a string and a URI reference; otherwise the
//     type is "about:blank".
//   - "title" and "detail" are kept when they are strings.
//   - "status" is kept when it is a whole number between 100 and 599.
//   - "instance" is kept when it is a string and a URI reference.
//   - Every other member is an extension, kept with its value compacted.
//
// A standard member whose value is rejected is treated as absent, and does not
// become an extension. When a name appears more than once, the last
// occurrence is used.
func Parse(data []byte) (*Error, error) {
	if len(data) > maxDocumentSize {
		return nil, fmt.Errorf("%w: larger than %d bytes", ErrMalformed, maxDocumentSize)
	}
	var members map[string]json.RawMessage
	if err := json.Unmarshal(data, &members); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrMalformed, err)
	}
	if members == nil {
		return nil, fmt.Errorf("%w: not a JSON object", ErrMalformed)
	}

	e := &Error{typ: blankType}
	if s, ok := stringMember(members, "type"); ok && validURIReference(s) {
		e.typ = s
	}
	e.title, _ = stringMember(members, "title")
	e.detail, _ = stringMember(members, "detail")
	if s, ok := stringMember(members, "instance"); ok && validURIReference(s) {
		e.instance = s
	}
	e.status = statusMember(members)

	for _, name := range []string{"type", "title", "status", "detail", "instance"} {
		delete(members, name)
	}
	for name, raw := range members {
		var b bytes.Buffer
		if err := json.Compact(&b, raw); err != nil {
			return nil, fmt.Errorf("%w: member %q: %w", ErrMalformed, name, err)
		}
		if e.ext == nil {
			e.ext = make(map[string]json.RawMessage, len(members))
		}
		e.ext[name] = b.Bytes()
	}
	return e, nil
}

// stringMember returns the member name when its value is a JSON string.
func stringMember(members map[string]json.RawMessage, name string) (string, bool) {
	raw, ok := members[name]
	if !ok {
		return "", false
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return "", false
	}
	return s, true
}

// statusMember returns the "status" member when it is a whole number between
// 100 and 599, and 0 otherwise. JSON numbers such as 403.0 and 4.03e2 are whole.
func statusMember(members map[string]json.RawMessage) int {
	raw, ok := members["status"]
	if !ok {
		return 0
	}
	var f float64
	if err := json.Unmarshal(raw, &f); err != nil || f != math.Trunc(f) || f < 100 || f > 599 {
		return 0
	}
	return int(f)
}

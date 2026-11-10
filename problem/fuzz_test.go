package problem_test

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/paccolamano/go/problem"
)

// rfcValidationExample is the validation example of RFC 9457 §3.
const rfcValidationExample = `{
 "type": "https://example.net/validation-error",
 "title": "Your request is not valid.",
 "errors": [
             {
               "detail": "must be a positive integer",
               "pointer": "#/age"
             },
             {
               "detail": "must be 'green', 'red' or 'blue'",
               "pointer": "#/profile/color"
             }
          ]
}`

// FuzzParse checks that Parse and Errors never panic, that the validation items
// of a document survive WithErrors unchanged, and that every document Parse
// accepts with a status Write can send survives a write and a parse unchanged.
func FuzzParse(f *testing.F) {
	for _, seed := range []string{
		creditDocument,
		rfcValidationExample,
		`{}`,
		`[]`,
		`null`,
		`{"status":"403"}`,
		`{"a":`,
		`{"status":400,"status":404,"x":1,"x":2}`,
		`{"type":"/x","title":"<b>","status":418,"detail":"😀","instance":"urn:a:b","":0}`,
		`{"status":422,"errors":[{"detail":"a","pointer":"#/a"},{"detail":"b"}]}`,
		`{"errors":[null]}`,
		`{"errors":[{"detail":1}]}`,
	} {
		f.Add([]byte(seed))
	}
	// A detail written as a JSON surrogate pair escape.
	esc := "\\"
	f.Add([]byte(`{"status":400,"detail":"` + esc + "ud83d" + esc + `ude00"}`))
	f.Fuzz(func(t *testing.T, data []byte) {
		p, err := problem.Parse(data)
		if err != nil {
			return
		}
		// Validation items found in any document survive WithErrors unchanged.
		if items, ok := p.Errors(); ok {
			again, ok := problem.Status(http.StatusBadRequest, problem.WithErrors(items...)).Errors()
			if !ok || !slices.Equal(again, items) {
				t.Fatalf("items %q became %q, %v after WithErrors", items, again, ok)
			}
		}
		if p.Status() < 400 || p.Status() > 599 {
			return
		}
		rec := httptest.NewRecorder()
		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil)
		if err := problem.Write(rec, req, p); err != nil {
			t.Fatalf("Write of a parsed problem: %v", err)
		}
		if rec.Body.Len() > maxDocumentSize {
			// Escaping can make the written document larger than the input,
			// beyond the size Parse accepts.
			return
		}
		back, err := problem.Parse(rec.Body.Bytes())
		if err != nil {
			t.Fatalf("Parse of the written document: %v\n%s", err, rec.Body)
		}
		sameProblem(t, back, p)
	})
}

// FuzzRead checks that a type declared with the type URI of a document, as
// written, matches the problem that Read reads from it, whatever the request
// URL.
func FuzzRead(f *testing.F) {
	for _, typ := range []string{
		"/probs/out-of-credit",
		"https://api.example.com/probs/out-of-credit",
		"https://other.example.com/probs/out-of-credit",
		"/probs/other",
		"//cdn.example.com/probs/out-of-credit",
		"/probs/./x/../out-of-credit",
		"/probs/out-of-credit?lang=en#top",
	} {
		f.Add(creditDoc(typ), apiTarget)
		f.Add(creditDoc(typ), "") // no request
	}
	f.Add(`{}`, apiTarget)
	f.Add(`{"type":"/x"}`, "/relative/base")
	f.Add(`{"type":"/x"}`, "about:blank")
	f.Add(`{"type":"/x"}`, "mailto:a@b")
	f.Fuzz(func(t *testing.T, doc, target string) {
		resp := &http.Response{
			StatusCode: http.StatusForbidden,
			Header:     http.Header{"Content-Type": {problem.MediaType}},
			Body:       io.NopCloser(strings.NewReader(doc)),
		}
		if target != "" {
			u, err := url.Parse(target)
			if err != nil {
				return
			}
			req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil)
			req.URL = u
			resp.Request = req
		}
		p, err := problem.Read(resp)
		if err != nil {
			return
		}
		var members map[string]json.RawMessage
		if err := json.Unmarshal([]byte(doc), &members); err != nil {
			return
		}
		var typ string
		if err := json.Unmarshal(members["type"], &typ); err != nil {
			return
		}
		decl, err := problem.NewType(typ, "t", http.StatusBadRequest)
		if err != nil {
			return // typ cannot be declared
		}
		if got, ok := decl.Match(p); !ok || got != p {
			t.Fatalf("declared %q, request URL %q: no match for the type %q", typ, target, p.Type())
		}
	})
}

// decodePointer decodes a JSON Pointer in URI fragment form (RFC 6901 §6)
// without the library: it removes "#", percent-decodes the rest, splits it on
// "/" and replaces "~1" with "/", then "~0" with "~", in each part.
func decodePointer(p string) ([]string, error) {
	frag, ok := strings.CutPrefix(p, "#")
	if !ok {
		return nil, errors.New("no leading #")
	}
	s, err := url.PathUnescape(frag)
	if err != nil {
		return nil, err
	}
	if s == "" {
		return nil, nil
	}
	rest, ok := strings.CutPrefix(s, "/")
	if !ok {
		return nil, errors.New("no leading /")
	}
	tokens := strings.Split(rest, "/")
	for i, tok := range tokens {
		tokens[i] = strings.ReplaceAll(strings.ReplaceAll(tok, "~1", "/"), "~0", "~")
	}
	return tokens, nil
}

// FuzzPointer checks that every pointer is ASCII, is a URI reference, and
// decodes back to its tokens. The prefixes of three free strings cover any byte
// in any position of up to three tokens, empty ones included.
func FuzzPointer(f *testing.F) {
	f.Add("foo", "0", "")
	f.Add("a/b", "m~n", "c%d")
	f.Add("città", "~1", " ")
	f.Add("\xff", "#?", "e^f")
	f.Fuzz(func(t *testing.T, a, b, c string) {
		for _, tokens := range [][]string{nil, {a}, {a, b}, {a, b, c}} {
			p := problem.Pointer(tokens...)
			for i := range len(p) {
				if p[i] >= 0x80 {
					t.Fatalf("Pointer(%q) = %q contains a non-ASCII byte", tokens, p)
				}
			}
			if !isURIReference(p) {
				t.Fatalf("Pointer(%q) = %q is not a URI reference", tokens, p)
			}
			got, err := decodePointer(p)
			if err != nil {
				t.Fatalf("decoding Pointer(%q) = %q: %v", tokens, p, err)
			}
			if !slices.Equal(got, tokens) && (len(got) != 0 || len(tokens) != 0) {
				t.Fatalf("Pointer(%q) = %q decodes to %q", tokens, p, got)
			}
		}
	})
}

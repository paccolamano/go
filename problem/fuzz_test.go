package problem_test

import (
	"errors"
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

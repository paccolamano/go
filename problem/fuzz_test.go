package problem_test

import (
	"net/http"
	"net/http/httptest"
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

// FuzzParse checks that Parse never panics, and that every document it accepts
// with a status Write can send survives a write and a parse unchanged.
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
	} {
		f.Add([]byte(seed))
	}
	// A detail written as a JSON surrogate pair escape.
	esc := "\\"
	f.Add([]byte(`{"status":400,"detail":"` + esc + "ud83d" + esc + `ude00"}`))
	f.Fuzz(func(t *testing.T, data []byte) {
		p, err := problem.Parse(data)
		if err != nil || p.Status() < 400 || p.Status() > 599 {
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

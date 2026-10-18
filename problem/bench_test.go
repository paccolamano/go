package problem_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/paccolamano/go/problem"
)

func BenchmarkWrite(b *testing.B) {
	e := creditOccurrence()
	req := newRequest(b, http.MethodGet, "/")
	for b.Loop() {
		_ = problem.Write(httptest.NewRecorder(), req, e)
	}
}

// BenchmarkParse parses a validation document just under the maximum size
// (SC-005: under 100 ms).
func BenchmarkParse(b *testing.B) {
	var sb strings.Builder
	sb.WriteString(`{"type":"https://example.net/validation-error","title":"Your request is not valid.","errors":[`)
	for i := 0; sb.Len() < maxDocumentSize-200; i++ {
		if i > 0 {
			sb.WriteByte(',')
		}
		fmt.Fprintf(&sb, `{"detail":"must be a positive integer","pointer":"#/items/%d/age"}`, i)
	}
	sb.WriteString(`]}`)
	data := []byte(sb.String())
	b.SetBytes(int64(len(data)))
	for b.Loop() {
		if _, err := problem.Parse(data); err != nil {
			b.Fatal(err)
		}
	}
}

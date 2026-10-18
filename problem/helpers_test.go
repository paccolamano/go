package problem_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/paccolamano/go/problem"
)

var (
	errWrite = errors.New("test: write failed")
	errRead  = errors.New("test: read failed")

	extensionName = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]{2,}$`)
)

// Shared declarations, as a consumer of the package would write them.
var (
	outOfCredit = problem.Must(problem.NewType(
		"https://example.com/probs/out-of-credit",
		"You do not have enough credit.",
		http.StatusForbidden,
	))
	validationError = problem.Must(problem.NewType(
		"https://example.net/validation-error",
		"Your request is not valid.",
		http.StatusUnprocessableEntity,
	))
	serviceUnavailable = problem.Must(problem.NewType(
		"https://example.com/probs/service-unavailable",
		"The service is unavailable.",
		http.StatusServiceUnavailable,
	))

	balance     = problem.Must(problem.NewKey[int]("balance"))
	accounts    = problem.Must(problem.NewKey[[]string]("accounts"))
	ratio       = problem.Must(problem.NewKey[float64]("ratio"))
	fieldErrors = problem.Must(problem.NewKey[[]fieldError]("errors"))
)

// fieldError is one entry of the "errors" extension of RFC 9457 §3.
type fieldError struct {
	Detail  string `json:"detail"`
	Pointer string `json:"pointer"`
}

// The documents of contracts/document.md.
const (
	creditDocument = `{"type":"https://example.com/probs/out-of-credit","title":"You do not have enough credit.","status":403,"detail":"Your current balance is 30, but that costs 50.","instance":"/account/12345/msgs/abc","accounts":["/account/12345","/account/67890"],"balance":30}`
	serverDocument = `{"type":"about:blank","title":"Internal Server Error","status":500}`
)

// creditOccurrence returns the occurrence of User Story 1, scenario 2.
func creditOccurrence() *problem.Error {
	return outOfCredit.New(
		problem.WithDetail("Your current balance is 30, but that costs 50."),
		problem.WithInstance("/account/12345/msgs/abc"),
		problem.WithExtension(balance, 30),
		problem.WithExtension(accounts, []string{"/account/12345", "/account/67890"}),
	)
}

// emptyValuesOccurrence carries one extension of each JSON kind, each with an
// empty value.
func emptyValuesOccurrence() *problem.Error {
	return outOfCredit.New(
		problem.WithExtension(problem.Must(problem.NewKey[string]("str")), ""),
		problem.WithExtension(problem.Must(problem.NewKey[int]("num")), 0),
		problem.WithExtension(problem.Must(problem.NewKey[bool]("flag")), false),
		problem.WithExtension(problem.Must(problem.NewKey[any]("nothing")), nil),
		problem.WithExtension(problem.Must(problem.NewKey[[]int]("list")), []int{}),
		problem.WithExtension(problem.Must(problem.NewKey[map[string]int]("obj")), map[string]int{}),
	)
}

// specialCharactersOccurrence carries markup, quotes and non-ASCII text.
func specialCharactersOccurrence() *problem.Error {
	return problem.Status(http.StatusNotFound, problem.WithDetail(`<script>alert("é😀")</script>`))
}

// sameProblem fails unless got and want have the same members and
// byte-identical extension values.
func sameProblem(tb testing.TB, got, want *problem.Error) {
	tb.Helper()
	if got.Type() != want.Type() || got.Title() != want.Title() || got.Status() != want.Status() ||
		got.Detail() != want.Detail() || got.Instance() != want.Instance() {
		tb.Errorf("members = (%q, %q, %d, %q, %q), want (%q, %q, %d, %q, %q)",
			got.Type(), got.Title(), got.Status(), got.Detail(), got.Instance(),
			want.Type(), want.Title(), want.Status(), want.Detail(), want.Instance())
	}
	sameBytes := func(a, b json.RawMessage) bool { return bytes.Equal(a, b) }
	if !maps.EqualFunc(got.Extensions(), want.Extensions(), sameBytes) {
		tb.Errorf("extensions = %s, want %s", fmtExtensions(got), fmtExtensions(want))
	}
}

func fmtExtensions(e *problem.Error) string {
	var b strings.Builder
	for _, name := range slices.Sorted(maps.Keys(e.Extensions())) {
		fmt.Fprintf(&b, "%s=%s ", name, e.Extensions()[name])
	}
	return b.String()
}

// twoFieldErrors is the list of the RFC 9457 §3 validation example.
func twoFieldErrors() []fieldError {
	return []fieldError{
		{Detail: "must be a positive integer", Pointer: "#/age"},
		{Detail: "must be 'green', 'red' or 'blue'", Pointer: "#/profile/color"},
	}
}

const twoFieldErrorsJSON = `[{"detail":"must be a positive integer","pointer":"#/age"},{"detail":"must be 'green', 'red' or 'blue'","pointer":"#/profile/color"}]`

// newRequest returns a request bound to the test's context.
func newRequest(tb testing.TB, method, target string) *http.Request {
	tb.Helper()
	return httptest.NewRequestWithContext(tb.Context(), method, target, nil)
}

// checkDocument checks body against the JSON schema of RFC 9457 Appendix A and
// the extension name syntax of §3.2, and returns its members.
func checkDocument(tb testing.TB, body []byte, wantStatus int) map[string]json.RawMessage {
	tb.Helper()
	var members map[string]json.RawMessage
	if err := json.Unmarshal(body, &members); err != nil || members == nil {
		tb.Fatalf("document is not a JSON object: %s", body)
	}
	for name, raw := range members {
		switch name {
		case "type", "instance":
			var s string
			if err := json.Unmarshal(raw, &s); err != nil || !isURIReference(s) {
				tb.Errorf("%s = %s: want a string in URI-reference format", name, raw)
			}
		case "title", "detail":
			var s string
			if err := json.Unmarshal(raw, &s); err != nil {
				tb.Errorf("%s = %s: want a string", name, raw)
			}
		case "status":
			var n int
			if err := json.Unmarshal(raw, &n); err != nil || n < 100 || n > 599 {
				tb.Errorf("status = %s: want an integer from 100 to 599", raw)
			} else if n != wantStatus {
				tb.Errorf("status = %d, want %d", n, wantStatus)
			}
		default:
			if !extensionName.MatchString(name) {
				tb.Errorf("extension name %q breaks RFC 9457 §3.2", name)
			}
		}
	}
	if _, ok := members["type"]; !ok {
		tb.Errorf("document has no type member: %s", body)
	}
	if _, ok := members["status"]; !ok {
		tb.Errorf("document has no status member: %s", body)
	}
	return members
}

// isURIReference checks the URI-reference format independently of the library.
func isURIReference(s string) bool {
	const allowed = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-._~:/?#[]@!$&'()*+,;=%"
	for _, r := range s {
		if !strings.ContainsRune(allowed, r) {
			return false
		}
	}
	_, err := url.Parse(s)
	return s != "" && err == nil
}

// failingWriter is a ResponseWriter whose Write always fails.
type failingWriter struct {
	header http.Header
	code   int
}

func (w *failingWriter) Header() http.Header {
	if w.header == nil {
		w.header = make(http.Header)
	}
	return w.header
}

func (w *failingWriter) WriteHeader(code int) { w.code = code }

func (w *failingWriter) Write([]byte) (int, error) { return 0, errWrite }

// countingReader counts the bytes read through it.
type countingReader struct {
	r io.Reader
	n int
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += n
	return n, err
}

// failingReader fails every read.
type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { return 0, errRead }

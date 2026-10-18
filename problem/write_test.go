package problem_test

import (
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/paccolamano/go/problem"
)

// write answers a GET request with err and returns the recorder and Write's result.
func write(tb testing.TB, err error) (*httptest.ResponseRecorder, error) {
	tb.Helper()
	rec := httptest.NewRecorder()
	werr := problem.Write(rec, newRequest(tb, http.MethodGet, "/"), err)
	return rec, werr
}

// wantResponse checks the code, the media type and the exact body.
func wantResponse(tb testing.TB, rec *httptest.ResponseRecorder, code int, body string) {
	tb.Helper()
	if rec.Code != code {
		tb.Errorf("code = %d, want %d", rec.Code, code)
	}
	if got := rec.Header().Get("Content-Type"); got != problem.MediaType {
		tb.Errorf("Content-Type = %q, want %q", got, problem.MediaType)
	}
	if got := rec.Body.String(); got != body {
		tb.Errorf("body =\n%s\nwant\n%s", got, body)
	}
	checkDocument(tb, rec.Body.Bytes(), code)
}

func TestWrite_StatusOnly(t *testing.T) {
	rec, err := write(t, problem.Status(http.StatusNotFound))
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	wantResponse(t, rec, http.StatusNotFound, `{"type":"about:blank","title":"Not Found","status":404}`)
}

func TestWrite_Occurrence(t *testing.T) {
	rec, err := write(t, creditOccurrence())
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	wantResponse(t, rec, http.StatusForbidden, creditDocument)
}

func TestWrite_ReplacedTitle(t *testing.T) {
	rec, err := write(t, problem.Status(http.StatusNotFound, problem.WithTitle("Risorsa non trovata")))
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	wantResponse(t, rec, http.StatusNotFound, `{"type":"about:blank","title":"Risorsa non trovata","status":404}`)
}

func TestWrite_HeadersKept(t *testing.T) {
	rec := httptest.NewRecorder()
	rec.Header().Set("Retry-After", "120")
	rec.Header().Set("Content-Language", "it")
	rec.Header().Set("Content-Type", "text/plain")
	if err := problem.Write(rec, newRequest(t, http.MethodGet, "/"), problem.Status(http.StatusServiceUnavailable)); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if got := rec.Header().Get("Retry-After"); got != "120" {
		t.Errorf("Retry-After = %q, want 120", got)
	}
	if got := rec.Header().Get("Content-Language"); got != "it" {
		t.Errorf("Content-Language = %q, want it", got)
	}
	if got := rec.Header().Get("Content-Type"); got != problem.MediaType {
		t.Errorf("Content-Type = %q, want %q", got, problem.MediaType)
	}
	if got := rec.Header().Get("Content-Length"); got != "" {
		t.Errorf("Content-Length = %q, want it unset", got)
	}
}

func TestWrite_NestedExtension(t *testing.T) {
	rec, err := write(t, validationError.New(problem.WithExtension(fieldErrors, twoFieldErrors())))
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	want := `{"type":"https://example.net/validation-error","title":"Your request is not valid.","status":422,"errors":` +
		twoFieldErrorsJSON + `}`
	wantResponse(t, rec, http.StatusUnprocessableEntity, want)
}

func TestWrite_Head(t *testing.T) {
	rec := httptest.NewRecorder()
	err := problem.Write(rec, newRequest(t, http.MethodHead, "/"), problem.Status(http.StatusServiceUnavailable))
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("code = %d, want 503", rec.Code)
	}
	if got := rec.Header().Get("Content-Type"); got != problem.MediaType {
		t.Errorf("Content-Type = %q", got)
	}
	if rec.Body.Len() != 0 {
		t.Errorf("HEAD body = %q, want empty", rec.Body.String())
	}
}

func TestWrite_Deterministic(t *testing.T) {
	first, _ := write(t, creditOccurrence())
	second, _ := write(t, creditOccurrence())
	if first.Body.String() != second.Body.String() {
		t.Fatalf("two writes differ:\n%s\n%s", first.Body, second.Body)
	}

	shared := creditOccurrence()
	bodies := make([]string, 64)
	var wg sync.WaitGroup
	for i := range bodies {
		wg.Go(func() {
			rec := httptest.NewRecorder()
			_ = problem.Write(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil), shared)
			bodies[i] = rec.Body.String()
		})
	}
	wg.Wait()
	for i, b := range bodies {
		if b != creditDocument {
			t.Errorf("goroutine %d wrote\n%s", i, b)
		}
	}
}

func TestWrite_Schema(t *testing.T) {
	for code := 400; code <= 599; code++ {
		rec, err := write(t, problem.Status(code))
		if err != nil {
			t.Fatalf("Write(Status(%d)): %v", code, err)
		}
		if rec.Code != code {
			t.Fatalf("Status(%d) written with code %d", code, rec.Code)
		}
		checkDocument(t, rec.Body.Bytes(), code)
	}
	for _, e := range []*problem.Error{
		creditOccurrence(),
		validationError.New(problem.WithExtension(fieldErrors, twoFieldErrors())),
		serviceUnavailable.New(problem.WithInstance("urn:example:request:42")),
	} {
		rec, err := write(t, e)
		if err != nil {
			t.Fatalf("Write(%v): %v", e, err)
		}
		checkDocument(t, rec.Body.Bytes(), e.Status())
	}
}

func TestWrite_EdgeInvalidStatus(t *testing.T) {
	for _, code := range []int{http.StatusOK, 0, 600} {
		t.Run(strconv.Itoa(code), func(t *testing.T) {
			rec, err := write(t, problem.Status(code))
			wantResponse(t, rec, http.StatusInternalServerError, serverDocument)
			if !errors.Is(err, problem.ErrInvalid) {
				t.Fatalf("Write returned %v, want ErrInvalid", err)
			}
			if !strings.Contains(err.Error(), strconv.Itoa(code)) {
				t.Errorf("error %q does not name status %d", err, code)
			}
		})
	}
}

func TestWrite_EdgeNoPhrase(t *testing.T) {
	rec, err := write(t, problem.Status(499))
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	wantResponse(t, rec, 499, `{"type":"about:blank","status":499}`)
}

func TestWrite_EdgeUnrepresentable(t *testing.T) {
	rec, err := write(t, outOfCredit.New(problem.WithExtension(ratio, math.Inf(1))))
	wantResponse(t, rec, http.StatusInternalServerError, serverDocument)
	if !errors.Is(err, problem.ErrInvalid) || !strings.Contains(err.Error(), "ratio") {
		t.Errorf("Write returned %v, want ErrInvalid naming ratio", err)
	}
}

func TestWrite_EdgeWriteFailure(t *testing.T) {
	t.Run("valid problem", func(t *testing.T) {
		w := &failingWriter{}
		err := problem.Write(w, newRequest(t, http.MethodGet, "/"), creditOccurrence())
		if !errors.Is(err, errWrite) || !strings.HasPrefix(err.Error(), "problem: write response") {
			t.Errorf("Write returned %v, want the write error", err)
		}
		if w.code != http.StatusForbidden {
			t.Errorf("code = %d, want 403", w.code)
		}
	})
	t.Run("replaced problem", func(t *testing.T) {
		err := problem.Write(&failingWriter{}, newRequest(t, http.MethodGet, "/"), problem.Status(http.StatusOK))
		if !errors.Is(err, problem.ErrInvalid) || !errors.Is(err, errWrite) {
			t.Errorf("Write returned %v, want ErrInvalid and the write error", err)
		}
	})
}

func TestWrite_EdgeEmptyValues(t *testing.T) {
	rec, err := write(t, emptyValuesOccurrence())
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	want := `{"type":"https://example.com/probs/out-of-credit","title":"You do not have enough credit.","status":403,` +
		`"flag":false,"list":[],"nothing":null,"num":0,"obj":{},"str":""}`
	wantResponse(t, rec, http.StatusForbidden, want)
}

func TestWrite_EdgeSpecialCharacters(t *testing.T) {
	rec, err := write(t, specialCharactersOccurrence())
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	// encoding/json escapes markup: '<' and '>' become backslash-u003c and
	// backslash-u003e. Non-ASCII characters stay raw UTF-8.
	lt, gt := "\\"+"u003c", "\\"+"u003e"
	want := `{"type":"about:blank","title":"Not Found","status":404,"detail":"` +
		lt + `script` + gt + `alert(\"é😀\")` + lt + `/script` + gt + `"}`
	wantResponse(t, rec, http.StatusNotFound, want)
	if strings.ContainsAny(rec.Body.String(), "<>") {
		t.Errorf("body contains raw markup: %s", rec.Body)
	}
}

// wantAbsent fails when any token appears in the body or in a header value.
func wantAbsent(tb testing.TB, rec *httptest.ResponseRecorder, tokens ...string) {
	tb.Helper()
	for _, tok := range tokens {
		if strings.Contains(rec.Body.String(), tok) {
			tb.Errorf("body contains %q: %s", tok, rec.Body)
		}
		for name, values := range rec.Header() {
			for _, v := range values {
				if strings.Contains(v, tok) {
					tb.Errorf("header %s contains %q", name, tok)
				}
			}
		}
	}
}

func TestWriteError_Wrapped(t *testing.T) {
	err := fmt.Errorf("send message: %w", fmt.Errorf("charge message: %w", creditOccurrence()))
	rec, werr := write(t, err)
	if werr != nil {
		t.Fatalf("Write: %v", werr)
	}
	wantResponse(t, rec, http.StatusForbidden, creditDocument)
	wantAbsent(t, rec, "charge message", "send message")
}

func TestWriteError_Leak(t *testing.T) {
	corpus := []string{
		`pq: password authentication failed for user "admin"`,
		`open /etc/app/secrets.yaml: permission denied`,
		`ERROR: relation "users" does not exist (SQLSTATE 42P01): SELECT id, password_hash FROM users WHERE email = $1`,
		`dial tcp 10.0.0.12:5432: connect: connection refused`,
		"goroutine 1 [running]:\nmain.main()\n\t/home/dev/app/main.go:42 +0x1d",
		`config: password=hunter2`,
	}
	tokens := []string{
		"pq:", "admin", "secrets.yaml", "/etc/app", "SQLSTATE", "password_hash",
		"10.0.0.12", "goroutine", "main.go", "hunter2",
	}
	for _, msg := range corpus {
		t.Run(msg[:12], func(t *testing.T) {
			rec, err := write(t, fmt.Errorf("handle request: %w", errors.New(msg)))
			if err != nil {
				t.Fatalf("Write: %v", err)
			}
			wantResponse(t, rec, http.StatusInternalServerError, serverDocument)
			wantAbsent(t, rec, tokens...)
		})
	}
}

func TestWriteError_CauseNotSent(t *testing.T) {
	err := serviceUnavailable.New(problem.WithCause(errors.New("pq: connection reset; password=hunter2")))
	rec, werr := write(t, err)
	if werr != nil {
		t.Fatalf("Write: %v", werr)
	}
	want := `{"type":"https://example.com/probs/service-unavailable","title":"The service is unavailable.","status":503}`
	wantResponse(t, rec, http.StatusServiceUnavailable, want)
	wantAbsent(t, rec, "hunter2", "pq:")
}

func TestWriteError_Join(t *testing.T) {
	t.Run("plain error first", func(t *testing.T) {
		rec, _ := write(t, errors.Join(errors.New("plain"), creditOccurrence()))
		wantResponse(t, rec, http.StatusForbidden, creditDocument)
	})
	t.Run("first problem wins", func(t *testing.T) {
		rec, _ := write(t, errors.Join(serviceUnavailable.New(), creditOccurrence()))
		wantResponse(t, rec, http.StatusServiceUnavailable,
			`{"type":"https://example.com/probs/service-unavailable","title":"The service is unavailable.","status":503}`)
	})
}

func TestInstance_Invalid(t *testing.T) {
	for _, uri := range []string{"/a b", "/é", "%zz", "http://[::1"} {
		t.Run("refuses "+uri, func(t *testing.T) {
			rec, err := write(t, outOfCredit.New(problem.WithInstance(uri)))
			wantResponse(t, rec, http.StatusInternalServerError, serverDocument)
			if !errors.Is(err, problem.ErrInvalid) || !strings.Contains(err.Error(), "instance") {
				t.Errorf("Write returned %v, want ErrInvalid naming the instance", err)
			}
		})
	}
	for _, uri := range []string{
		"/account/12345/msgs/abc",
		"urn:uuid:3f1c2a1e-9b7d-4f5e-8a0b-1c2d3e4f5a6b",
		"https://example.com/x",
	} {
		t.Run("accepts "+uri, func(t *testing.T) {
			rec, err := write(t, outOfCredit.New(problem.WithInstance(uri)))
			if err != nil {
				t.Fatalf("Write: %v", err)
			}
			members := checkDocument(t, rec.Body.Bytes(), http.StatusForbidden)
			if got := string(members["instance"]); got != strconv.Quote(uri) {
				t.Errorf("instance = %s, want %q", got, uri)
			}
		})
	}
}

func TestConcurrent_Occurrences(t *testing.T) {
	bodies := make([]string, 64)
	var wg sync.WaitGroup
	for i := range bodies {
		wg.Go(func() {
			e := outOfCredit.New(problem.WithDetail(strconv.Itoa(i)), problem.WithExtension(balance, i))
			rec := httptest.NewRecorder()
			_ = problem.Write(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil), e)
			bodies[i] = rec.Body.String()
		})
	}
	wg.Wait()
	for i, b := range bodies {
		want := fmt.Sprintf(`{"type":"https://example.com/probs/out-of-credit","title":"You do not have enough credit.","status":403,"detail":"%d","balance":%d}`, i, i)
		if b != want {
			t.Errorf("goroutine %d wrote\n%s\nwant\n%s", i, b, want)
		}
	}
}

// rejection stands in for middleware.Rejection, which this module cannot
// import (FR-001).
type rejection struct {
	Status int
	Err    error
}

func TestWrite_RejectFuncSnippet(t *testing.T) {
	// The body of the refusal function shown in README.md and doc.go.
	reject := func(w http.ResponseWriter, r *http.Request, rej rejection) {
		_ = problem.Write(w, r, problem.Status(rej.Status))
	}
	for _, code := range []int{
		http.StatusRequestEntityTooLarge,
		http.StatusUnsupportedMediaType,
		http.StatusTooManyRequests,
		http.StatusInternalServerError,
		http.StatusServiceUnavailable,
	} {
		t.Run(strconv.Itoa(code), func(t *testing.T) {
			rec := httptest.NewRecorder()
			if code == http.StatusTooManyRequests {
				rec.Header().Set("Retry-After", "30")
			}
			reject(rec, newRequest(t, http.MethodGet, "/"), rejection{Status: code, Err: errors.New("refused")})
			want := fmt.Sprintf(`{"type":"about:blank","title":%q,"status":%d}`, http.StatusText(code), code)
			wantResponse(t, rec, code, want)
			if code == http.StatusTooManyRequests && rec.Header().Get("Retry-After") != "30" {
				t.Errorf("Retry-After = %q, want 30", rec.Header().Get("Retry-After"))
			}
			wantAbsent(t, rec, "refused")
		})
	}
}

func TestWriteError_EdgeNil(t *testing.T) {
	for name, err := range map[string]error{
		"nil":                nil,
		"wrapped nil *Error": fmt.Errorf("x: %w", (*problem.Error)(nil)),
	} {
		t.Run(name, func(t *testing.T) {
			rec, werr := write(t, err)
			wantResponse(t, rec, http.StatusInternalServerError, serverDocument)
			if !errors.Is(werr, problem.ErrInvalid) {
				t.Errorf("Write returned %v, want ErrInvalid", werr)
			}
		})
	}
}

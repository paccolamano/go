package problem_test

import (
	"errors"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/paccolamano/go/problem"
)

var requestID = problem.Must(problem.NewKey[string]("request_id"))

func TestWith_AddsMembers(t *testing.T) {
	orig := creditOccurrence()
	d := orig.With(problem.WithExtension(requestID, "req-42"))

	rec, err := write(t, d)
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	want := `{"type":"https://example.com/probs/out-of-credit","title":"You do not have enough credit.","status":403,` +
		`"detail":"Your current balance is 30, but that costs 50.","instance":"/account/12345/msgs/abc",` +
		`"accounts":["/account/12345","/account/67890"],"balance":30,"request_id":"req-42"}`
	wantResponse(t, rec, http.StatusForbidden, want)

	rec, err = write(t, orig)
	if err != nil {
		t.Fatalf("Write of the original: %v", err)
	}
	wantResponse(t, rec, http.StatusForbidden, creditDocument)

	t.Run("option rules", func(t *testing.T) {
		d := orig.With(problem.WithDetail(""), problem.WithTitle(""))
		if d.Detail() != "" {
			t.Errorf("Detail() = %q, want it cleared", d.Detail())
		}
		if d.Title() != orig.Title() {
			t.Errorf("Title() = %q, want %q kept", d.Title(), orig.Title())
		}
		if orig.Detail() == "" {
			t.Error("the original lost its detail")
		}
	})
}

func TestWith_KeepsCause(t *testing.T) {
	cause := &strconv.NumError{Func: "Atoi", Num: "x", Err: strconv.ErrSyntax}
	orig := outOfCredit.New(problem.WithCause(cause))
	d := orig.With(problem.WithExtension(requestID, "r"))

	for name, e := range map[string]*problem.Error{"original": orig, "derived": d} {
		t.Run(name, func(t *testing.T) {
			if !errors.Is(e, strconv.ErrSyntax) {
				t.Error("errors.Is does not find the cause's sentinel")
			}
			var ne *strconv.NumError
			if !errors.As(e, &ne) || ne != cause {
				t.Errorf("errors.As = %v, want the cause %p", ne, cause)
			}
			if got, ok := outOfCredit.Match(e); !ok || got != e {
				t.Errorf("outOfCredit.Match = %p, %v; want %p, true", got, ok, e)
			}
			if got, ok := validationError.Match(e); ok || got != nil {
				t.Errorf("validationError.Match = %p, %v; want nil, false", got, ok)
			}
		})
	}
}

func TestWith_Concurrent(t *testing.T) {
	orig := creditOccurrence()
	bodies := make([]string, 100)
	var wg sync.WaitGroup
	for i := range bodies {
		wg.Go(func() {
			d := orig.With(problem.WithExtension(requestID, "req-"+strconv.Itoa(i)))
			rec := httptest.NewRecorder()
			_ = problem.Write(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil), d)
			bodies[i] = rec.Body.String()
		})
	}
	wg.Wait()
	for i, body := range bodies {
		want := strings.TrimSuffix(creditDocument, "}") + `,"request_id":"req-` + strconv.Itoa(i) + `"}`
		if body != want {
			t.Errorf("derivation %d wrote\n%s\nwant\n%s", i, body, want)
		}
	}
	rec, err := write(t, orig)
	if err != nil {
		t.Fatalf("Write of the original: %v", err)
	}
	wantResponse(t, rec, http.StatusForbidden, creditDocument)
}

func TestWith_ReplacesMember(t *testing.T) {
	orig := creditOccurrence()
	d := orig.With(problem.WithExtension(balance, 10))
	if got, ok := balance.Lookup(d); !ok || got != 10 {
		t.Errorf("derived balance = %d, %v; want 10, true", got, ok)
	}
	if got, ok := balance.Lookup(orig); !ok || got != 30 {
		t.Errorf("original balance = %d, %v; want 30, true", got, ok)
	}
}

func TestWith_CarriesFailure(t *testing.T) {
	bad := problem.Status(http.StatusConflict, problem.WithInstance("/a b"))
	tests := []struct {
		name string
		opt  problem.Option
	}{
		{"valid options", problem.WithDetail("ok")},
		{"second failure", problem.WithInstance("/c d")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec, err := write(t, bad.With(tt.opt))
			wantResponse(t, rec, http.StatusInternalServerError, serverDocument)
			if !errors.Is(err, problem.ErrInvalid) {
				t.Fatalf("Write returned %v, want ErrInvalid", err)
			}
			// The first failure recorded is the one reported.
			if !strings.Contains(err.Error(), `"/a b"`) || strings.Contains(err.Error(), `"/c d"`) {
				t.Errorf("Write returned %q, want the original reason", err)
			}
		})
	}
}

func TestWith_NewFailure(t *testing.T) {
	orig := creditOccurrence()
	tests := []struct {
		name string
		opt  problem.Option
	}{
		{"invalid instance", problem.WithInstance("/c d")},
		{"unrepresentable extension", problem.WithExtension(ratio, math.Inf(1))},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec, err := write(t, orig.With(tt.opt))
			wantResponse(t, rec, http.StatusInternalServerError, serverDocument)
			if !errors.Is(err, problem.ErrInvalid) {
				t.Errorf("Write returned %v, want ErrInvalid", err)
			}
		})
	}
	rec, err := write(t, orig)
	if err != nil {
		t.Fatalf("Write of the original: %v", err)
	}
	wantResponse(t, rec, http.StatusForbidden, creditDocument)
}

func TestWith_Nil(t *testing.T) {
	if got := (*problem.Error)(nil).With(problem.WithDetail("x")); got != nil {
		t.Errorf("With on nil = %v, want nil", got)
	}
	rec, err := write(t, creditOccurrence().With(nil))
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	wantResponse(t, rec, http.StatusForbidden, creditDocument)
}

func TestWith_ReadProblem(t *testing.T) {
	const doc = `{"type":"/probs/out-of-credit","title":"You do not have enough credit.","status":403,"balance":30,"code":"E1"}`
	p, err := problem.Read(response(t, http.StatusForbidden, problem.MediaType, strings.NewReader(doc)))
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	d := p.With(problem.WithExtension(requestID, "r"))
	if got, want := d.Type(), "https://api.example.com/probs/out-of-credit"; got != want {
		t.Errorf("Type() = %q, want %q", got, want)
	}
	ext := d.Extensions()
	for name, want := range map[string]string{"balance": "30", "code": `"E1"`, "request_id": `"r"`} {
		if got := string(ext[name]); got != want {
			t.Errorf("extension %s = %s, want %s", name, got, want)
		}
	}
	if len(ext) != 3 {
		t.Errorf("extensions = %s, want balance, code and request_id", fmtExtensions(d))
	}
}

func TestWith_NoOptions(t *testing.T) {
	orig := creditOccurrence()
	d := orig.With()
	sameProblem(t, d, orig)
	first, _ := write(t, orig)
	second, _ := write(t, d)
	if first.Body.String() != second.Body.String() {
		t.Errorf("documents differ:\n%s\n%s", first.Body, second.Body)
	}
}

func TestWith_EdgeIdentity(t *testing.T) {
	orig := creditOccurrence()
	if errors.Is(orig.With(), orig) {
		t.Error("errors.Is(derived, original) = true; the derived problem is a distinct occurrence")
	}
}

func TestWith_EdgeReplaceCause(t *testing.T) {
	orig := outOfCredit.New(problem.WithCause(io.ErrUnexpectedEOF))
	d := orig.With(problem.WithCause(io.ErrClosedPipe))
	if !errors.Is(d, io.ErrClosedPipe) {
		t.Error("the derived problem does not have the new cause")
	}
	if errors.Is(d, io.ErrUnexpectedEOF) {
		t.Error("the derived problem still has the original cause")
	}
	if !errors.Is(orig, io.ErrUnexpectedEOF) {
		t.Error("the original lost its cause")
	}
}

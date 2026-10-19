package problem_test

import (
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/paccolamano/go/problem"
)

// validationDocument is the validation example of RFC 9457 §3, as Write sends
// it (contracts/document.md).
const validationDocument = `{"type":"https://example.net/validation-error","title":"Your request is not valid.","status":422,` +
	`"errors":[{"detail":"must be a positive integer","pointer":"#/age"},` +
	`{"detail":"must be 'green', 'red' or 'blue'","pointer":"#/profile/color"}]}`

// rfcItems returns the two items of the RFC 9457 §3 validation example.
func rfcItems() []problem.Item {
	return []problem.Item{
		{Detail: "must be a positive integer", Pointer: problem.Pointer("age")},
		{Detail: "must be 'green', 'red' or 'blue'", Pointer: problem.Pointer("profile", "color")},
	}
}

func TestWithErrors_RFCExample(t *testing.T) {
	rec, err := write(t, validationError.New(problem.WithErrors(rfcItems()...)))
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	wantResponse(t, rec, http.StatusUnprocessableEntity, validationDocument)
}

func TestWithErrors_Items(t *testing.T) {
	tests := []struct {
		name string
		opt  problem.Option
		want string
	}{
		{"no pointer", problem.WithErrors(problem.Item{Detail: "the body is empty"}), `[{"detail":"the body is empty"}]`},
		{"empty detail", problem.WithErrors(problem.Item{Pointer: "#/a"}), `[{"detail":"","pointer":"#/a"}]`},
		{"no items", problem.WithErrors(), `[]`},
		{"nil items", problem.WithErrors([]problem.Item(nil)...), `[]`},
		{
			"pointer written as given",
			problem.WithErrors(problem.Item{Detail: "x", Pointer: "not a pointer <&>"}),
			`[{"detail":"x","pointer":"not a pointer \u003c\u0026\u003e"}]`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := validationError.New(tt.opt)
			if got := string(e.Extensions()["errors"]); got != tt.want {
				t.Errorf("errors = %s, want %s", got, tt.want)
			}
			rec, err := write(t, e)
			if err != nil {
				t.Fatalf("Write: %v", err)
			}
			checkDocument(t, rec.Body.Bytes(), http.StatusUnprocessableEntity)
		})
	}
}

func TestWithErrors_LastWins(t *testing.T) {
	items := problem.WithErrors(problem.Item{Detail: "a", Pointer: "#/a"})
	declared := problem.WithExtension(fieldErrors, twoFieldErrors())
	tests := []struct {
		name string
		opts []problem.Option
		want string
	}{
		{"declared key last", []problem.Option{items, declared}, twoFieldErrorsJSON},
		{"WithErrors last", []problem.Option{declared, items}, `[{"detail":"a","pointer":"#/a"}]`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := validationError.New(tt.opts...)
			if got := string(e.Extensions()["errors"]); got != tt.want {
				t.Errorf("errors = %s, want %s", got, tt.want)
			}
		})
	}
	if _, err := problem.NewKey[int]("errors"); err != nil {
		t.Errorf(`NewKey("errors"): %v, want it accepted`, err)
	}
}

func TestErrors_RoundTrip(t *testing.T) {
	e := validationError.New(problem.WithErrors(rfcItems()...))
	rec, err := write(t, e)
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	parsed, err := problem.Parse(rec.Body.Bytes())
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	read, err := problem.Read(response(t, http.StatusUnprocessableEntity, problem.MediaType, strings.NewReader(rec.Body.String())))
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	for name, p := range map[string]*problem.Error{"built": e, "parsed": parsed, "read": read} {
		t.Run(name, func(t *testing.T) {
			got, ok := p.Errors()
			if !ok || !slices.Equal(got, rfcItems()) {
				t.Errorf("Errors() = %q, %v; want %q, true", got, ok, rfcItems())
			}
		})
	}
}

func TestErrors_NotFound(t *testing.T) {
	docs := []string{
		`{}`,
		`{"errors":null}`,
		`{"errors":"x"}`,
		`{"errors":1}`,
		`{"errors":true}`,
		`{"errors":{}}`,
		`{"errors":[null]}`,
		`{"errors":["a","b"]}`,
		`{"errors":[1]}`,
		`{"errors":[{"detail":42}]}`,
		`{"errors":[{"detail":"x","pointer":false}]}`,
		`{"errors":[{"detail":"ok"},{"detail":[]}]}`,
	}
	for _, doc := range docs {
		t.Run(doc, func(t *testing.T) {
			p, err := problem.Parse([]byte(doc))
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			if got, ok := p.Errors(); ok || got != nil {
				t.Errorf("Errors() = %q, %v; want nil, false", got, ok)
			}
		})
	}
	t.Run("built without errors", func(t *testing.T) {
		if got, ok := problem.Status(http.StatusBadRequest).Errors(); ok || got != nil {
			t.Errorf("Errors() = %q, %v; want nil, false", got, ok)
		}
	})
}

func TestErrors_Found(t *testing.T) {
	tests := []struct {
		doc  string
		want []problem.Item
	}{
		{`{"errors":[]}`, []problem.Item{}},
		{`{"errors":[{"detail":"x","pointer":"#/a","code":"E1"}]}`, []problem.Item{{Detail: "x", Pointer: "#/a"}}},
		{`{"errors":[{"pointer":"#/a"}]}`, []problem.Item{{Pointer: "#/a"}}},
		// Names are exact: Detail and POINTER are other members.
		{`{"errors":[{"Detail":"x","POINTER":"#/a"}]}`, []problem.Item{{}}},
		// The last of two duplicate names wins, as in Parse.
		{`{"errors":[{"detail":"a","detail":"b"}]}`, []problem.Item{{Detail: "b"}}},
	}
	for _, tt := range tests {
		t.Run(tt.doc, func(t *testing.T) {
			p, err := problem.Parse([]byte(tt.doc))
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			got, ok := p.Errors()
			if !ok || got == nil || !slices.Equal(got, tt.want) {
				t.Errorf("Errors() = %#v, %v; want %#v, true", got, ok, tt.want)
			}
		})
	}
	t.Run("other members stay in the extension", func(t *testing.T) {
		p, err := problem.Parse([]byte(`{"errors":[{"detail":"x","code":"E1"}]}`))
		if err != nil {
			t.Fatalf("Parse: %v", err)
		}
		if got := string(p.Extensions()["errors"]); !strings.Contains(got, `"code":"E1"`) {
			t.Errorf(`Extensions()["errors"] = %s, want it to keep "code"`, got)
		}
	})
}

func TestErrors_NilProblem(t *testing.T) {
	if got, ok := (*problem.Error)(nil).Errors(); ok || got != nil {
		t.Errorf("Errors() on nil = %q, %v; want nil, false", got, ok)
	}
}

func TestPointer_RFC6901(t *testing.T) {
	// The examples of RFC 6901 §6, then the cases contracts/document.md pins.
	tests := []struct {
		tokens []string
		want   string
	}{
		{nil, "#"},
		{[]string{"foo"}, "#/foo"},
		{[]string{"foo", "0"}, "#/foo/0"},
		{[]string{""}, "#/"},
		{[]string{"a/b"}, "#/a~1b"},
		{[]string{"c%d"}, "#/c%25d"},
		{[]string{"e^f"}, "#/e%5Ef"},
		{[]string{"g|h"}, "#/g%7Ch"},
		{[]string{`i\j`}, "#/i%5Cj"},
		{[]string{`k"l`}, "#/k%22l"},
		{[]string{" "}, "#/%20"},
		{[]string{"m~n"}, "#/m~0n"},
		{[]string{"città"}, "#/citt%C3%A0"},
		{[]string{"items", "0", "qty"}, "#/items/0/qty"},
		{[]string{"~1"}, "#/~01"},
		{[]string{"a?b:c@d"}, "#/a?b:c@d"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if got := problem.Pointer(tt.tokens...); got != tt.want {
				t.Errorf("Pointer(%q) = %q, want %q", tt.tokens, got, tt.want)
			}
		})
	}
}

func TestPointer_EdgeBytes(t *testing.T) {
	tests := []struct {
		token string
		want  string
	}{
		{"\x00\x7f\xff", "#/%00%7F%FF"},
		{"#[]{}`", "#/%23%5B%5D%7B%7D%60"},
		{"!$&'()*+,;=-._", "#/!$&'()*+,;=-._"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := problem.Pointer(tt.token)
			if got != tt.want {
				t.Errorf("Pointer(%q) = %q, want %q", tt.token, got, tt.want)
			}
			if !isURIReference(got) {
				t.Errorf("Pointer(%q) = %q is not a URI reference", tt.token, got)
			}
		})
	}
}

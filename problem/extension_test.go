package problem_test

import (
	"errors"
	"net/http"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/paccolamano/go/problem"
)

func TestExtension_Values(t *testing.T) {
	tests := []struct {
		name string
		e    *problem.Error
		key  string
		want string
	}{
		{"number", creditOccurrence(), "balance", "30"},
		{"list of strings", creditOccurrence(), "accounts", `["/account/12345","/account/67890"]`},
		{
			"list of objects",
			validationError.New(problem.WithExtension(fieldErrors, twoFieldErrors())),
			"errors",
			twoFieldErrorsJSON,
		},
		{
			"second value replaces the first",
			outOfCredit.New(problem.WithExtension(balance, 30), problem.WithExtension(balance, 40)),
			"balance",
			"40",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := string(tt.e.Extensions()[tt.key]); got != tt.want {
				t.Errorf("Extensions()[%q] = %s, want %s", tt.key, got, tt.want)
			}
		})
	}
}

func TestNewKey_Rules(t *testing.T) {
	refused := []string{
		"type", "title", "status", "detail", "instance", "Status", "TYPE", "Detail",
		"id", "ab", "", "user-id", "2fa", "_meta",
	}
	for _, name := range refused {
		t.Run("refuses "+name, func(t *testing.T) {
			_, err := problem.NewKey[int](name)
			if !errors.Is(err, problem.ErrInvalid) {
				t.Fatalf("NewKey(%q) = %v, want ErrInvalid", name, err)
			}
			if !strings.Contains(err.Error(), strconv.Quote(name)) {
				t.Errorf("error %q does not quote the name", err)
			}
		})
	}
	for _, name := range []string{"balance", "retry_after", "errors", "abc", "X12"} {
		t.Run("accepts "+name, func(t *testing.T) {
			if _, err := problem.NewKey[int](name); err != nil {
				t.Errorf("NewKey(%q): %v", name, err)
			}
		})
	}
}

func TestNewKey_Undeclared(t *testing.T) {
	rec, err := write(t, outOfCredit.New(problem.WithExtension(problem.Key[int]{}, 1)))
	wantResponse(t, rec, http.StatusInternalServerError, serverDocument)
	if !errors.Is(err, problem.ErrInvalid) {
		t.Errorf("Write returned %v, want ErrInvalid", err)
	}
	if v, ok := (problem.Key[int]{}).Lookup(creditOccurrence()); ok || v != 0 {
		t.Errorf("zero Key Lookup = %d, %v; want 0, false", v, ok)
	}
}

func TestLookup_NilProblem(t *testing.T) {
	// Match finds nothing in a plain error, and returns a nil problem.
	p, _ := outOfCredit.Match(errors.New("plain"))
	tests := []struct {
		name   string
		lookup func() (any, bool)
		want   any
	}{
		{"nil", func() (any, bool) { return balance.Lookup(nil) }, 0},
		{"failed Match", func() (any, bool) { return balance.Lookup(p) }, 0},
		{"typed nil", func() (any, bool) { return accounts.Lookup((*problem.Error)(nil)) }, []string(nil)},
		{"zero Key", func() (any, bool) { return (problem.Key[int]{}).Lookup(nil) }, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := tt.lookup()
			if ok || !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Lookup = %#v, %v; want %#v, false", got, ok, tt.want)
			}
		})
	}
}

func TestExtension_Lookup(t *testing.T) {
	e := creditOccurrence()
	if got, ok := balance.Lookup(e); !ok || got != 30 {
		t.Errorf("balance.Lookup = %d, %v; want 30, true", got, ok)
	}
	if got, ok := accounts.Lookup(e); !ok || !slices.Equal(got, []string{"/account/12345", "/account/67890"}) {
		t.Errorf("accounts.Lookup = %v, %v", got, ok)
	}
	if got, ok := ratio.Lookup(e); ok || got != 0 {
		t.Errorf("ratio.Lookup on a missing member = %v, %v; want 0, false", got, ok)
	}
	asString := problem.Must(problem.NewKey[string]("balance"))
	if got, ok := asString.Lookup(e); ok || got != "" {
		t.Errorf("Key[string].Lookup on a number = %q, %v; want \"\", false", got, ok)
	}
}

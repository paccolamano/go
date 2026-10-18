package problem_test

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/paccolamano/go/problem"
)

func TestNewType_Rules(t *testing.T) {
	accepted := []string{
		"https://example.com/probs/out-of-credit",
		"/probs/out-of-credit",
		"//example.com/probs/x",
		"tag:example.com,2026:out-of-credit",
		"urn:example:out-of-credit",
	}
	for _, uri := range accepted {
		t.Run("accepts "+uri, func(t *testing.T) {
			if _, err := problem.NewType(uri, "t", http.StatusForbidden); err != nil {
				t.Errorf("NewType(%q): %v", uri, err)
			}
		})
	}
	refused := []struct {
		name    string
		uri     string
		title   string
		status  int
		element string
	}{
		{"relative without full path", "out-of-credit", "t", http.StatusForbidden, "type URI"},
		{"relative path", "probs/x", "t", http.StatusForbidden, "type URI"},
		{"fragment only", "#x", "t", http.StatusForbidden, "type URI"},
		{"query only", "?x", "t", http.StatusForbidden, "type URI"},
		{"malformed host", "http://[::1", "t", http.StatusForbidden, "type URI"},
		{"space", "/a b", "t", http.StatusForbidden, "type URI"},
		{"non-ASCII", "/é", "t", http.StatusForbidden, "type URI"},
		{"bad escape", "%zz", "t", http.StatusForbidden, "type URI"},
		{"about:blank", "about:blank", "t", http.StatusForbidden, "type URI"},
		{"empty URI", "", "t", http.StatusForbidden, "type URI"},
		{"empty title", "/x", "", http.StatusForbidden, "title"},
		{"status 0", "/x", "t", 0, "status"},
		{"status 200", "/x", "t", http.StatusOK, "status"},
		{"status 302", "/x", "t", http.StatusFound, "status"},
		{"status 399", "/x", "t", 399, "status"},
		{"status 600", "/x", "t", 600, "status"},
	}
	for _, tt := range refused {
		t.Run("refuses "+tt.name, func(t *testing.T) {
			_, err := problem.NewType(tt.uri, tt.title, tt.status)
			if !errors.Is(err, problem.ErrInvalid) {
				t.Fatalf("NewType(%q, %q, %d) = %v, want ErrInvalid", tt.uri, tt.title, tt.status, err)
			}
			if !strings.Contains(err.Error(), tt.element) {
				t.Errorf("error %q does not name %q", err, tt.element)
			}
		})
	}
	for _, code := range []int{http.StatusBadRequest, 599} {
		if _, err := problem.NewType("/x", "t", code); err != nil {
			t.Errorf("NewType with status %d: %v", code, err)
		}
	}
}

func TestMust_Refused(t *testing.T) {
	defer func() {
		v := recover()
		err, ok := v.(error)
		if !ok || !errors.Is(err, problem.ErrInvalid) {
			t.Errorf("recovered %v, want an error matching ErrInvalid", v)
		}
	}()
	problem.Must(problem.NewType("out-of-credit", "t", http.StatusForbidden))
	t.Error("Must did not panic")
}

func TestNewType_Independent(t *testing.T) {
	a := outOfCredit.New(problem.WithDetail("a"), problem.WithExtension(balance, 1))
	b := outOfCredit.New(problem.WithDetail("b"), problem.WithExtension(balance, 2))
	for _, tc := range []struct {
		e      *problem.Error
		detail string
		bal    int
	}{{a, "a", 1}, {b, "b", 2}} {
		rec, err := write(t, tc.e)
		if err != nil {
			t.Fatalf("Write: %v", err)
		}
		want := fmt.Sprintf(`{"type":"https://example.com/probs/out-of-credit","title":"You do not have enough credit.","status":403,"detail":%q,"balance":%d}`,
			tc.detail, tc.bal)
		wantResponse(t, rec, http.StatusForbidden, want)
	}
	c := outOfCredit.New()
	if c.Type() != "https://example.com/probs/out-of-credit" || c.Title() != "You do not have enough credit." ||
		c.Status() != http.StatusForbidden || c.Detail() != "" || len(c.Extensions()) != 0 {
		t.Errorf("the declaration changed: %q %q %d %q %v", c.Type(), c.Title(), c.Status(), c.Detail(), c.Extensions())
	}
}

func TestNewType_Undeclared(t *testing.T) {
	rec, err := write(t, problem.Type{}.New())
	wantResponse(t, rec, http.StatusInternalServerError, serverDocument)
	if !errors.Is(err, problem.ErrInvalid) || !strings.Contains(err.Error(), "undeclared type") {
		t.Errorf("Write returned %v, want ErrInvalid with \"undeclared type\"", err)
	}
}

func TestMatch_Tree(t *testing.T) {
	occurrence := creditOccurrence()
	plain := errors.New("plain")
	found := []struct {
		name string
		err  error
	}{
		{"itself", occurrence},
		{"wrapped twice", fmt.Errorf("send message: %w", fmt.Errorf("charge message: %w", occurrence))},
		{"joined with a plain error", errors.Join(plain, occurrence)},
		{"after a problem of another type", errors.Join(serviceUnavailable.New(), occurrence)},
		{"as the cause of another problem", serviceUnavailable.New(problem.WithCause(occurrence))},
	}
	for _, tt := range found {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := outOfCredit.Match(tt.err)
			if !ok || got != occurrence {
				t.Errorf("Match = %v, %v; want the occurrence, true", got, ok)
			}
		})
	}
	notFound := []struct {
		name string
		err  error
	}{
		{"nil", nil},
		{"plain error", plain},
		{"another type", serviceUnavailable.New()},
		{"about:blank with the same status", problem.Status(http.StatusForbidden)},
		{"wrapped nil problem", fmt.Errorf("x: %w", (*problem.Error)(nil))},
	}
	for _, tt := range notFound {
		t.Run(tt.name, func(t *testing.T) {
			if got, ok := outOfCredit.Match(tt.err); ok || got != nil {
				t.Errorf("Match = %v, %v; want nil, false", got, ok)
			}
		})
	}
	t.Run("zero Type", func(t *testing.T) {
		if _, ok := (problem.Type{}).Match(occurrence); ok {
			t.Error("the zero Type matched")
		}
	})
}

func TestMust_Basic(t *testing.T) {
	t.Run("returns the value", func(t *testing.T) {
		if got := problem.Must(7, nil); got != 7 {
			t.Errorf("Must(7, nil) = %d", got)
		}
	})
	t.Run("panics with the error", func(t *testing.T) {
		errX := errors.New("x")
		defer func() {
			v := recover()
			err, ok := v.(error)
			if !ok || !errors.Is(err, errX) {
				t.Errorf("recovered %v, want an error matching errX", v)
			}
		}()
		problem.Must(0, errX)
		t.Error("Must did not panic")
	})
}

package problem_test

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"slices"
	"testing"

	"github.com/paccolamano/go/problem"
)

func TestError_Cause(t *testing.T) {
	pathErr := &fs.PathError{Op: "open", Path: "/var/lib/app/db", Err: fs.ErrPermission}
	dbErr := fmt.Errorf("query: %w", errors.Join(io.ErrUnexpectedEOF, pathErr))
	p := serviceUnavailable.New(problem.WithCause(dbErr))

	if !errors.Is(p, io.ErrUnexpectedEOF) {
		t.Error("errors.Is does not find the cause")
	}
	var target *fs.PathError
	if !errors.As(p, &target) || target != pathErr {
		t.Error("errors.As does not find the *fs.PathError in the cause")
	}
	if !errors.Is(errors.Unwrap(p), dbErr) {
		t.Errorf("Unwrap() = %v, want the cause", errors.Unwrap(p))
	}
	if got := serviceUnavailable.New(problem.WithCause(nil)).Unwrap(); got != nil {
		t.Errorf("WithCause(nil): Unwrap() = %v, want nil", got)
	}
}

func TestError_MessageWithCause(t *testing.T) {
	p := outOfCredit.New(
		problem.WithDetail("Your current balance is 30, but that costs 50."),
		problem.WithCause(io.ErrUnexpectedEOF),
	)
	want := "You do not have enough credit.: Your current balance is 30, but that costs 50.: unexpected EOF"
	if got := p.Error(); got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}

func TestStatus_Members(t *testing.T) {
	tests := []struct {
		name      string
		e         *problem.Error
		wantTitle string
		wantCode  int
	}{
		{"recommended phrase", problem.Status(http.StatusNotFound), "Not Found", http.StatusNotFound},
		{"no phrase", problem.Status(499), "", 499},
		{
			"replaced title",
			problem.Status(http.StatusServiceUnavailable, problem.WithTitle("Servizio non disponibile")),
			"Servizio non disponibile",
			http.StatusServiceUnavailable,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.e.Type(); got != "about:blank" {
				t.Errorf("Type() = %q, want about:blank", got)
			}
			if got := tt.e.Title(); got != tt.wantTitle {
				t.Errorf("Title() = %q, want %q", got, tt.wantTitle)
			}
			if got := tt.e.Status(); got != tt.wantCode {
				t.Errorf("Status() = %d, want %d", got, tt.wantCode)
			}
			if tt.e.Detail() != "" || tt.e.Instance() != "" || len(tt.e.Extensions()) != 0 {
				t.Errorf("unexpected members: detail %q, instance %q, extensions %v",
					tt.e.Detail(), tt.e.Instance(), tt.e.Extensions())
			}
		})
	}
}

func TestOccurrence_Options(t *testing.T) {
	t.Run("type members and options", func(t *testing.T) {
		e := creditOccurrence()
		if e.Type() != "https://example.com/probs/out-of-credit" ||
			e.Title() != "You do not have enough credit." ||
			e.Status() != http.StatusForbidden {
			t.Errorf("type members = %q, %q, %d", e.Type(), e.Title(), e.Status())
		}
		if e.Detail() != "Your current balance is 30, but that costs 50." {
			t.Errorf("Detail() = %q", e.Detail())
		}
		if e.Instance() != "/account/12345/msgs/abc" {
			t.Errorf("Instance() = %q", e.Instance())
		}
	})
	tests := []struct {
		name  string
		opts  []problem.Option
		check func(*problem.Error) bool
	}{
		{
			"last detail wins",
			[]problem.Option{problem.WithDetail("a"), problem.WithDetail("b")},
			func(e *problem.Error) bool { return e.Detail() == "b" },
		},
		{
			"empty title keeps the type's title",
			[]problem.Option{problem.WithTitle("")},
			func(e *problem.Error) bool { return e.Title() == "You do not have enough credit." },
		},
		{
			"empty detail clears",
			[]problem.Option{problem.WithDetail("x"), problem.WithDetail("")},
			func(e *problem.Error) bool { return e.Detail() == "" },
		},
		{
			"empty instance clears",
			[]problem.Option{problem.WithInstance("/x"), problem.WithInstance("")},
			func(e *problem.Error) bool { return e.Instance() == "" },
		},
		{
			"nil option is ignored",
			[]problem.Option{nil, problem.WithDetail("d"), nil},
			func(e *problem.Error) bool { return e.Detail() == "d" },
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if e := outOfCredit.New(tt.opts...); !tt.check(e) {
				t.Errorf("unexpected occurrence: title %q, detail %q, instance %q",
					e.Title(), e.Detail(), e.Instance())
			}
		})
	}
}

func TestOccurrence_Message(t *testing.T) {
	tests := []struct {
		name string
		e    *problem.Error
		want string
	}{
		{"title and detail", outOfCredit.New(problem.WithDetail("d")), "You do not have enough credit.: d"},
		{"title only", problem.Status(http.StatusNotFound), "Not Found"},
		{"neither", problem.Status(499), "about:blank"},
		{"detail only", problem.Status(499, problem.WithDetail("d")), "d"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.e.Error(); got != tt.want {
				t.Errorf("Error() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestOccurrence_Immutable(t *testing.T) {
	t.Run("Extensions returns a copy", func(t *testing.T) {
		e := creditOccurrence()
		ext := e.Extensions()
		ext["balance"][0] = '9'
		delete(ext, "accounts")
		ext["extra"] = []byte("1")
		again := e.Extensions()
		if string(again["balance"]) != "30" || again["accounts"] == nil || again["extra"] != nil {
			t.Errorf("Extensions() changed after mutation: %v", again)
		}
	})
	t.Run("values are captured when built", func(t *testing.T) {
		list := []string{"/account/1"}
		e := outOfCredit.New(problem.WithExtension(accounts, list))
		list[0] = "/account/2"
		got, _ := accounts.Lookup(e)
		if !slices.Equal(got, []string{"/account/1"}) {
			t.Errorf("accounts = %v, want [/account/1]", got)
		}
	})
}

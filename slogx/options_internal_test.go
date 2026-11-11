package slogx

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
)

func fnAttrs(key string) func(context.Context) []slog.Attr {
	return func(context.Context) []slog.Attr { return []slog.Attr{slog.String(key, "v")} }
}

func getString(context.Context) (string, bool) { return "v", true }

func TestOptions_Validation(t *testing.T) {
	tests := []struct {
		name string
		opts []Option
		want string
	}{
		{name: "nil extraction function", opts: []Option{WithContextFunc(nil)}, want: "WithContextFunc: function must not be nil"},
		{name: "no key", opts: []Option{WithRedactKeys()}, want: "WithRedactKeys: at least one key is required"},
		{name: "empty key", opts: []Option{WithRedactKeys("token", "")}, want: "WithRedactKeys: empty key"},
		{name: "whitespace key", opts: []Option{WithRedactKeys(" \t")}, want: "WithRedactKeys: empty key"},
		{name: "nil option", opts: []Option{WithRedactKeys("token"), nil}, want: "options[1]: must not be nil"},
		{name: "first invalid option reported", opts: []Option{WithRedactKeys(""), WithContextFunc(nil)}, want: "WithRedactKeys: empty key"},
		{name: "context value empty key", opts: []Option{WithContextValue("", getString)}, want: "WithContextValue: empty key"},
		{name: "context value blank key", opts: []Option{WithContextValue(" \t", getString)}, want: "WithContextValue: empty key"},
		{name: "context value nil function", opts: []Option{WithContextValue[string]("k", nil)}, want: "WithContextValue: function must not be nil"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := newConfig(tt.opts)
			if !errors.Is(err, ErrInvalidConfig) {
				t.Fatalf("err = %v, want ErrInvalidConfig", err)
			}
			if !strings.HasSuffix(err.Error(), tt.want) {
				t.Errorf("err = %q, want suffix %q", err, tt.want)
			}
		})
	}
}

func TestOptions_Valid(t *testing.T) {
	if _, err := newConfig(nil); err != nil {
		t.Fatalf("no options: err = %v, want nil", err)
	}
	if _, err := newConfig([]Option{WithContextFunc(fnAttrs("a")), WithRedactKeys("token")}); err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
}

func TestOptions_ErrorFormat(t *testing.T) {
	_, err := newConfig([]Option{WithRedactKeys("")})
	const want = "slogx: invalid configuration: New: WithRedactKeys: empty key"
	if err == nil || err.Error() != want {
		t.Errorf("err = %v, want %q", err, want)
	}
	nilNext := invalid("next", "must not be nil")
	if got, want := nilNext.Error(), "slogx: invalid configuration: New: next: must not be nil"; got != want {
		t.Errorf("invalid = %q, want %q", got, want)
	}
	if !errors.Is(nilNext, ErrInvalidConfig) {
		t.Error("invalid does not wrap ErrInvalidConfig")
	}
}

func TestOptions_Accumulation(t *testing.T) {
	cfg, err := newConfig([]Option{
		WithContextFunc(fnAttrs("first")),
		WithRedactKeys("a", "b"),
		WithContextValue("second", getString),
		WithContextFunc(fnAttrs("third")),
		WithRedactKeys("c"),
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	s := cfg.sources
	if len(s) != 3 ||
		s[0].fn == nil || s[0].fn(ctx)[0].Key != "first" ||
		s[1].fn != nil || s[1].key != "second" ||
		s[2].fn == nil || s[2].fn(ctx)[0].Key != "third" {
		t.Errorf("sources not kept in call order")
	}
	if got := strings.Join(cfg.keys, ","); got != "a,b,c" {
		t.Errorf("keys = %q, want a,b,c", got)
	}
}

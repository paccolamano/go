package slogx

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"
)

const allOptions = optContextFunc | optRedactKeys | optSampling | optSampleErrors

func fnAttrs(key string) func(context.Context) []slog.Attr {
	return func(context.Context) []slog.Attr { return []slog.Attr{slog.String(key, "v")} }
}

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
		{name: "first below 1", opts: []Option{WithSampling(0, 1, time.Second)}, want: "WithSampling: first must be at least 1"},
		{name: "thereafter below 1", opts: []Option{WithSampling(1, 0, time.Second)}, want: "WithSampling: thereafter must be at least 1"},
		{name: "zero interval", opts: []Option{WithSampling(1, 1, 0)}, want: "WithSampling: interval must be positive"},
		{name: "negative interval", opts: []Option{WithSampling(1, 1, -time.Second)}, want: "WithSampling: interval must be positive"},
		{name: "sample errors without sampling", opts: []Option{WithSampleErrors()}, want: "WithSampleErrors: requires WithSampling"},
		{name: "nil option", opts: []Option{WithRedactKeys("token"), nil}, want: "options[1]: must not be nil"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := newConfig("New", allOptions, 0, tt.opts)
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
	opts := []Option{
		WithContextFunc(fnAttrs("a")),
		WithRedactKeys("token"),
		WithSampling(1, 1, time.Nanosecond),
		WithSampleErrors(),
	}
	if _, err := newConfig("New", allOptions, 0, opts); err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if _, err := newConfig("New", allOptions, 0, nil); err != nil {
		t.Fatalf("no options: err = %v, want nil", err)
	}
}

func TestOptions_Applicability(t *testing.T) {
	tests := []struct {
		name     string
		accepted optionSet
		required optionSet
		opts     []Option
		want     string
	}{
		{name: "context func not accepted", accepted: optRedactKeys, required: optRedactKeys, opts: []Option{WithRedactKeys("x"), WithContextFunc(fnAttrs("a"))}, want: "WithContextFunc: does not apply"},
		{name: "redact keys not accepted", accepted: optContextFunc, opts: []Option{WithRedactKeys("x")}, want: "WithRedactKeys: does not apply"},
		{name: "sampling not accepted", accepted: 0, opts: []Option{WithSampling(1, 1, time.Second)}, want: "WithSampling: does not apply"},
		{name: "sample errors not accepted", accepted: optSampling, required: optSampling, opts: []Option{WithSampling(1, 1, time.Second), WithSampleErrors()}, want: "WithSampleErrors: does not apply"},
		{name: "required redact keys missing", accepted: optRedactKeys, required: optRedactKeys, want: "WithRedactKeys: required"},
		{name: "required sampling missing", accepted: optSampling | optSampleErrors, required: optSampling, want: "WithSampling: required"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := newConfig("NewX", tt.accepted, tt.required, tt.opts)
			if !errors.Is(err, ErrInvalidConfig) {
				t.Fatalf("err = %v, want ErrInvalidConfig", err)
			}
			if !strings.HasSuffix(err.Error(), tt.want) {
				t.Errorf("err = %q, want suffix %q", err, tt.want)
			}
		})
	}
}

func TestOptions_ErrorFormat(t *testing.T) {
	_, err := newConfig("NewRedactHandler", optRedactKeys, optRedactKeys, []Option{WithRedactKeys("")})
	const want = "slogx: invalid configuration: NewRedactHandler: WithRedactKeys: empty key"
	if err == nil || err.Error() != want {
		t.Errorf("err = %v, want %q", err, want)
	}
	if got, want := errNilHandler("New").Error(), "slogx: invalid configuration: New: next: must not be nil"; got != want {
		t.Errorf("errNilHandler = %q, want %q", got, want)
	}
	if !errors.Is(errNilHandler("New"), ErrInvalidConfig) {
		t.Error("errNilHandler does not wrap ErrInvalidConfig")
	}
}

func TestOptions_Accumulation(t *testing.T) {
	cfg, err := newConfig("New", allOptions, 0, []Option{
		WithContextFunc(fnAttrs("first")),
		WithRedactKeys("a", "b"),
		WithSampling(5, 50, time.Minute),
		WithContextFunc(fnAttrs("second")),
		WithRedactKeys("c"),
		WithSampling(2, 20, time.Second),
		WithSampleErrors(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.fns) != 2 || cfg.fns[0](context.Background())[0].Key != "first" || cfg.fns[1](context.Background())[0].Key != "second" {
		t.Errorf("fns not kept in call order")
	}
	if got := strings.Join(cfg.keys, ","); got != "a,b,c" {
		t.Errorf("keys = %q, want a,b,c", got)
	}
	if cfg.first != 2 || cfg.thereafter != 20 || cfg.interval != time.Second {
		t.Errorf("sampling = (%d, %d, %v), want the last call (2, 20, 1s)", cfg.first, cfg.thereafter, cfg.interval)
	}
	if !cfg.sampleErrors {
		t.Error("sampleErrors not set")
	}
	if cfg.set != allOptions {
		t.Errorf("set = %b, want %b", cfg.set, allOptions)
	}
}

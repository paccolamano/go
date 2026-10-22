package health_test

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/paccolamano/go/lifecycle/health"
)

func ok(context.Context) error { return nil }

func TestNew_Validation(t *testing.T) {
	tests := []struct {
		name    string
		checks  []health.Check
		opts    []health.Option
		wantErr bool
	}{
		{"empty name", []health.Check{{Name: "", Func: ok}}, nil, true},
		{"invalid UTF-8", []health.Check{{Name: "\xff", Func: ok}}, nil, true},
		{"space", []health.Check{{Name: "my check", Func: ok}}, nil, true},
		{"tab", []health.Check{{Name: "a\tb", Func: ok}}, nil, true},
		{"line feed", []health.Check{{Name: "a\nb", Func: ok}}, nil, true},
		{"control rune", []health.Check{{Name: "a\x00b", Func: ok}}, nil, true},
		{"duplicate", []health.Check{{Name: "db", Func: ok}, {Name: "db", Func: ok}}, nil, true},
		{"nil Func", []health.Check{{Name: "db"}}, nil, true},
		{"negative timeout", []health.Check{{Name: "db", Func: ok, Timeout: -time.Nanosecond}}, nil, true},
		{"nil logger", nil, []health.Option{health.WithLogger(nil)}, true},
		{"nil option", nil, []health.Option{nil}, true},
		{"valid names", []health.Check{{Name: "postgres", Func: ok}, {Name: "redis:cache", Func: ok}, {Name: "lifecycle", Func: ok}}, nil, false},
		{"zero timeout", []health.Check{{Name: "db", Func: ok, Timeout: 0}}, nil, false},
		{"logger", nil, []health.Option{health.WithLogger(slog.New(slog.DiscardHandler))}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h, err := health.New(tt.checks, tt.opts...)
			if tt.wantErr {
				if !errors.Is(err, health.ErrInvalidConfig) || h != nil {
					t.Fatalf("New = %v, %v; want nil and an error wrapping ErrInvalidConfig", h, err)
				}
				return
			}
			if err != nil || h == nil {
				t.Fatalf("New = %v, %v; want a handler", h, err)
			}
		})
	}
}

func TestNew_NoChecks(t *testing.T) {
	if _, err := health.New(nil); err != nil {
		t.Fatalf("New(nil) = %v, want nil", err)
	}
}

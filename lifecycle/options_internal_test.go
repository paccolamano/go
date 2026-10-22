package lifecycle

import (
	"errors"
	"log/slog"
	"os"
	"syscall"
	"testing"
	"time"
)

func TestOptions_Defaults(t *testing.T) {
	c, err := newConfig(nil)
	if err != nil {
		t.Fatalf("newConfig: %v", err)
	}
	if c.stopTimeout != 15*time.Second {
		t.Errorf("stop timeout = %v, want 15s", c.stopTimeout)
	}
	if c.drainDelay != 0 {
		t.Errorf("drain delay = %v, want 0", c.drainDelay)
	}
	if len(c.signals) != 0 {
		t.Errorf("signals = %v, want none", c.signals)
	}
	if c.logger == nil {
		t.Error("logger is nil, want a discarding logger")
	}
}

func TestOptions_Validation(t *testing.T) {
	tests := []struct {
		name    string
		opts    []Option
		wantErr bool
	}{
		{"signals none", []Option{WithSignals()}, true},
		{"signals nil", []Option{WithSignals(nil)}, true},
		{"signals nil among others", []Option{WithSignals(os.Interrupt, nil)}, true},
		{"signals valid", []Option{WithSignals(os.Interrupt, syscall.SIGTERM)}, false},
		{"drain negative", []Option{WithDrainDelay(-time.Nanosecond)}, true},
		{"drain zero", []Option{WithDrainDelay(0)}, false},
		{"drain positive", []Option{WithDrainDelay(10 * time.Second)}, false},
		{"stop timeout zero", []Option{WithStopTimeout(0)}, true},
		{"stop timeout negative", []Option{WithStopTimeout(-time.Nanosecond)}, true},
		{"stop timeout positive", []Option{WithStopTimeout(time.Nanosecond)}, false},
		{"logger nil", []Option{WithLogger(nil)}, true},
		{"logger valid", []Option{WithLogger(slog.New(slog.DiscardHandler))}, false},
		{"nil option", []Option{nil}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := newConfig(tt.opts)
			if tt.wantErr {
				if !errors.Is(err, ErrInvalidConfig) {
					t.Fatalf("err = %v, want one wrapping ErrInvalidConfig", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("err = %v, want nil", err)
			}
		})
	}
}

func TestOptions_DuplicateSignals(t *testing.T) {
	c, err := newConfig([]Option{WithSignals(os.Interrupt, syscall.SIGTERM, os.Interrupt)})
	if err != nil {
		t.Fatalf("newConfig: %v", err)
	}
	if len(c.signals) != 2 || c.signals[0] != os.Interrupt || c.signals[1] != syscall.SIGTERM {
		t.Fatalf("signals = %v, want [interrupt terminated]", c.signals)
	}
}

func TestOptions_ErrorFormat(t *testing.T) {
	tests := []struct {
		opt  Option
		want string
	}{
		{WithSignals(), "lifecycle: invalid configuration: WithSignals: no signal"},
		{WithSignals(nil), "lifecycle: invalid configuration: WithSignals: nil signal"},
		{WithDrainDelay(-1), "lifecycle: invalid configuration: WithDrainDelay: must not be negative"},
		{WithStopTimeout(0), "lifecycle: invalid configuration: WithStopTimeout: must be positive"},
		{WithLogger(nil), "lifecycle: invalid configuration: WithLogger: nil logger"},
		{nil, "lifecycle: invalid configuration: nil option"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			_, err := newConfig([]Option{tt.opt})
			if err == nil || err.Error() != tt.want {
				t.Fatalf("err = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestOptions_LastWins(t *testing.T) {
	c, err := newConfig([]Option{WithStopTimeout(time.Second), WithStopTimeout(3 * time.Second)})
	if err != nil {
		t.Fatalf("newConfig: %v", err)
	}
	if c.stopTimeout != 3*time.Second {
		t.Fatalf("stop timeout = %v, want 3s", c.stopTimeout)
	}
}

package sse

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"
)

func noReplay(context.Context, string, []string, func(Event, ...string) error) error { return nil }

func TestOptions_Defaults(t *testing.T) {
	cfg, err := newConfig("NewBroker", brokerOptions|clientOptions, nil)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.writeTimeout != 10*time.Second {
		t.Errorf("writeTimeout = %v, want 10s", cfg.writeTimeout)
	}
	if cfg.keepAlive != 15*time.Second {
		t.Errorf("keepAlive = %v, want 15s", cfg.keepAlive)
	}
	if cfg.history != 1000 {
		t.Errorf("history = %d, want 1000", cfg.history)
	}
	if cfg.buffer != 64 {
		t.Errorf("buffer = %d, want 64", cfg.buffer)
	}
	if cfg.maxEventSize != 1<<20 {
		t.Errorf("maxEventSize = %d, want 1 MiB", cfg.maxEventSize)
	}
	if cfg.httpClient != http.DefaultClient {
		t.Error("httpClient is not http.DefaultClient")
	}
	if cfg.retry != 0 || cfg.reset != nil || cfg.replay != nil {
		t.Errorf("retry, reset and replay must be unset, got %v, %v, %v", cfg.retry, cfg.reset, cfg.replay != nil)
	}
}

func TestOptions_Validation(t *testing.T) {
	all := brokerOptions | clientOptions
	tests := []struct {
		name string
		opt  Option
		ok   bool
	}{
		{"retry zero", WithRetry(0), false},
		{"retry negative", WithRetry(-1), false},
		{"retry below 1ms", WithRetry(999 * time.Microsecond), false},
		{"retry 1ms", WithRetry(time.Millisecond), true},
		{"write timeout zero", WithWriteTimeout(0), false},
		{"write timeout positive", WithWriteTimeout(time.Second), true},
		{"keep-alive zero", WithKeepAlive(0), false},
		{"keep-alive positive", WithKeepAlive(time.Second), true},
		{"history zero", WithHistory(0), false},
		{"history one", WithHistory(1), true},
		{"buffer zero", WithBuffer(0), false},
		{"buffer one", WithBuffer(1), true},
		{"reset type with LF", WithResetEvent(Event{Type: "a\nb"}), false},
		{"reset invalid data", WithResetEvent(Event{Data: "\xff"}), false},
		{"reset ID with LF is ignored", WithResetEvent(Event{ID: "a\nb", Type: "reset"}), true},
		{"replay nil", WithReplay(nil), false},
		{"replay set", WithReplay(noReplay), true},
		{"http client nil", WithHTTPClient(nil), false},
		{"http client set", WithHTTPClient(&http.Client{}), true},
		{"max event size zero", WithMaxEventSize(0), false},
		{"max event size one", WithMaxEventSize(1), true},
		{"nil option", nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := newConfig("NewBroker", all, []Option{tt.opt})
			if tt.ok && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !tt.ok && !errors.Is(err, ErrInvalidConfig) {
				t.Fatalf("err = %v, want ErrInvalidConfig", err)
			}
		})
	}
}

func TestOptions_Applicability(t *testing.T) {
	tests := []struct {
		ctor     string
		accepted optionSet
		opt      Option
	}{
		{"NewStream", streamOptions, WithHistory(1)},
		{"NewStream", streamOptions, WithKeepAlive(time.Second)},
		{"NewClient", clientOptions, WithRetry(time.Second)},
		{"NewBroker", brokerOptions, WithHTTPClient(http.DefaultClient)},
		{"NewBroker", brokerOptions, WithMaxEventSize(1)},
		{"Decode", decodeOptions, WithHTTPClient(http.DefaultClient)},
		{"Decode", decodeOptions, WithReplay(noReplay)},
	}
	for _, tt := range tests {
		_, err := newConfig(tt.ctor, tt.accepted, []Option{tt.opt})
		if !errors.Is(err, ErrInvalidConfig) {
			t.Errorf("%s: err = %v, want ErrInvalidConfig", tt.ctor, err)
		}
	}
	for _, opt := range []Option{WithRetry(time.Second), WithWriteTimeout(time.Second)} {
		if _, err := newConfig("NewStream", streamOptions, []Option{opt}); err != nil {
			t.Errorf("NewStream rejected an accepted option: %v", err)
		}
	}
}

func TestOptions_ErrorFormat(t *testing.T) {
	_, err := newConfig("NewBroker", brokerOptions, []Option{WithHistory(0)})
	want := "sse: invalid configuration: NewBroker: WithHistory: must be at least 1"
	if err == nil || err.Error() != want {
		t.Fatalf("err = %v, want %q", err, want)
	}
	_, err = newConfig("NewStream", streamOptions, []Option{WithHistory(1)})
	if err == nil || !strings.HasPrefix(err.Error(), "sse: invalid configuration: NewStream: WithHistory: ") {
		t.Fatalf("err = %v, want the NewStream: WithHistory prefix", err)
	}
}

func TestOptions_LastWins(t *testing.T) {
	cfg, err := newConfig("NewBroker", brokerOptions, []Option{WithHistory(5), WithHistory(7)})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.history != 7 {
		t.Fatalf("history = %d, want 7", cfg.history)
	}
}

package sse

import (
	"errors"
	"testing"
	"time"
)

func TestEncode_Validation(t *testing.T) {
	tests := []struct {
		name string
		ev   Event
		ok   bool
	}{
		{"empty", Event{}, true},
		{"full", Event{ID: "7", Type: "progress", Data: "50%\nmore"}, true},
		{"type with NUL", Event{Type: "a\x00b"}, true},
		{"ID with NUL", Event{ID: "a\x00b"}, false},
		{"ID with CR", Event{ID: "a\rb"}, false},
		{"ID with LF", Event{ID: "a\nb"}, false},
		{"type with CR", Event{Type: "a\rb"}, false},
		{"type with LF", Event{Type: "a\nb"}, false},
		{"ID not UTF-8", Event{ID: "\xff"}, false},
		{"type not UTF-8", Event{Type: "\xff"}, false},
		{"data not UTF-8", Event{Data: "a\xffb"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.ev.validate()
			if tt.ok && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !tt.ok && !errors.Is(err, ErrInvalidEvent) {
				t.Fatalf("err = %v, want ErrInvalidEvent", err)
			}
		})
	}
}

func TestEncode_Event(t *testing.T) {
	tests := []struct {
		ev   Event
		want string
	}{
		{Event{Data: "hello"}, "data: hello\n\n"},
		{Event{Type: "progress", ID: "7", Data: "50%"}, "event: progress\nid: 7\ndata: 50%\n\n"},
		{Event{Data: "a\r\nb\rc\n"}, "data: a\ndata: b\ndata: c\ndata:\n\n"},
		{Event{Data: ""}, "data:\n\n"},
		{Event{Data: "  x"}, "data:   x\n\n"},
		{Event{Data: "a\n\nb"}, "data: a\ndata:\ndata: b\n\n"},
	}
	for _, tt := range tests {
		if got := string(appendEvent(nil, tt.ev)); got != tt.want {
			t.Errorf("appendEvent(%+v) = %q, want %q", tt.ev, got, tt.want)
		}
	}
}

func TestEncode_Records(t *testing.T) {
	if got := string(appendRetry(nil, 3*time.Second)); got != "retry: 3000\n" {
		t.Errorf("appendRetry(3s) = %q", got)
	}
	if got := string(appendRetry(nil, 1500*time.Microsecond)); got != "retry: 1\n" {
		t.Errorf("appendRetry(1.5ms) = %q", got)
	}
	if got := string(appendPosition(nil, "K-1")); got != "id: K-1\n\n" {
		t.Errorf("appendPosition(K-1) = %q", got)
	}
	if got := string(appendPosition(nil, "")); got != "id:\n\n" {
		t.Errorf("appendPosition(empty) = %q", got)
	}
	if keepAliveLine != ": keep-alive\n" {
		t.Errorf("keepAliveLine = %q", keepAliveLine)
	}
}

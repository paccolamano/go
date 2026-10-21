package sse

import (
	"errors"
	"io"
	"strings"
	"testing"
	"time"
)

func TestDecode_Retry(t *testing.T) {
	tests := []struct {
		stream string
		want   time.Duration
		set    bool
	}{
		{"retry: 2500\n\n", 2500 * time.Millisecond, true},
		{"retry: 2.5\n\n", 0, false},
		{"retry: -1\n\n", 0, false},
		{"retry:\n\n", 0, false},
		{"retry: 10x\n\n", 0, false},
		{"retry: 99999999999999999999\n\n", 0, false}, // overflows a duration
	}
	for _, tt := range tests {
		p := newParser(strings.NewReader(tt.stream), defaultMaxEventSize)
		if _, err := p.next(); !errors.Is(err, io.EOF) {
			t.Fatalf("%q: next() = %v, want io.EOF", tt.stream, err)
		}
		if p.retrySet != tt.set || p.retry != tt.want {
			t.Errorf("%q: retry = %v (set %v), want %v (set %v)", tt.stream, p.retry, p.retrySet, tt.want, tt.set)
		}
	}
}

func TestDecode_Position(t *testing.T) {
	p := newParser(strings.NewReader("id: K-9\n\n"), defaultMaxEventSize)
	if _, err := p.next(); !errors.Is(err, io.EOF) {
		t.Fatalf("next() = %v, want io.EOF", err)
	}
	if p.lastID != "K-9" {
		t.Fatalf("lastID = %q, want K-9", p.lastID)
	}
}

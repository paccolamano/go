package sse_test

import (
	"errors"
	"net/http"
	"testing"

	"github.com/paccolamano/go/sse"
)

func newBroker(tb testing.TB, opts ...sse.Option) *sse.Broker {
	tb.Helper()
	b, err := sse.NewBroker(opts...)
	if err != nil {
		tb.Fatal(err)
	}
	return b
}

func TestPublish_Invalid(t *testing.T) {
	b := newBroker(t)
	tests := []struct {
		name   string
		ev     sse.Event
		topics []string
		target error
	}{
		{"invalid event", sse.Event{ID: "x1", Type: "a\nb"}, []string{"t"}, sse.ErrInvalidEvent},
		{"no topic", sse.Event{ID: "x2"}, nil, nil},
		{"empty topic", sse.Event{ID: "x3"}, []string{"t", ""}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := b.Publish(tt.ev, tt.topics...)
			if err == nil || (tt.target != nil && !errors.Is(err, tt.target)) {
				t.Fatalf("Publish = %v, want an error matching %v", err, tt.target)
			}
			// Nothing was recorded: the same identifier is still free.
			if err := b.Publish(sse.Event{ID: tt.ev.ID}, "t"); err != nil {
				t.Fatalf("the refused event was recorded: %v", err)
			}
		})
	}
}

func TestPublish_Duplicate(t *testing.T) {
	b := newBroker(t)
	if err := b.Publish(sse.Event{ID: "42", Data: "a"}, "t"); err != nil {
		t.Fatal(err)
	}
	if err := b.Publish(sse.Event{ID: "42", Data: "b"}, "t"); !errors.Is(err, sse.ErrDuplicateID) {
		t.Fatalf("second Publish = %v, want ErrDuplicateID", err)
	}
}

func TestBroker_Options(t *testing.T) {
	for _, opt := range []sse.Option{sse.WithHTTPClient(http.DefaultClient), sse.WithMaxEventSize(1)} {
		if _, err := sse.NewBroker(opt); !errors.Is(err, sse.ErrInvalidConfig) {
			t.Errorf("NewBroker = %v, want ErrInvalidConfig", err)
		}
	}
}

package sse

import (
	"strings"
	"testing"
)

func TestID_Format(t *testing.T) {
	const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZ234567"
	p := newPrefix()
	if len(p) != 12 || strings.Trim(p, alphabet) != "" {
		t.Fatalf("prefix %q is not 12 base32 characters", p)
	}
	if q := newPrefix(); q == p {
		t.Fatalf("two prefixes are equal: %q", p)
	}
	id := formatID("ABCDEFGHJKLM", 42)
	if id != "ABCDEFGHJKLM-42" {
		t.Fatalf("formatID = %q", id)
	}
	if err := (Event{ID: formatID(p, 1)}).validate(); err != nil {
		t.Fatalf("generated ID is invalid: %v", err)
	}
}

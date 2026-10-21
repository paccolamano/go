package sse

import (
	"fmt"
	"slices"
	"testing"
)

// filled returns a history of the given capacity holding entries 1 to n, with
// IDs e1…en. Even sequences are on topic "even", odd ones on "odd", and every
// tenth also on "ten".
func filled(capacity, n int) *history {
	h := newHistory(capacity)
	for i := 1; i <= n; i++ {
		topics := []string{"odd"}
		if i%2 == 0 {
			topics = []string{"even"}
		}
		if i%10 == 0 {
			topics = append(topics, "ten")
		}
		h.add(entry{seq: uint64(i), ev: Event{ID: fmt.Sprintf("e%d", i)}, topics: topics})
	}
	return h
}

func seqs(es []entry) []uint64 {
	out := make([]uint64, 0, len(es))
	for _, e := range es {
		out = append(out, e.seq)
	}
	return out
}

func TestHistory_Bound(t *testing.T) {
	h := filled(3, 10)
	if got := seqs(h.after(0, []string{"odd", "even"})); !slices.Equal(got, []uint64{8, 9, 10}) {
		t.Fatalf("kept %v, want [8 9 10]", got)
	}
	if len(h.index) != 3 {
		t.Fatalf("index has %d entries, want 3", len(h.index))
	}
	for i := 1; i <= 7; i++ {
		if _, ok := h.index[fmt.Sprintf("e%d", i)]; ok {
			t.Fatalf("evicted e%d is still indexed", i)
		}
	}
}

func TestHistory_Lookup(t *testing.T) {
	h := filled(3, 10)
	if seq, ok := h.lookup("e9"); !ok || seq != 9 {
		t.Fatalf("lookup(e9) = %d, %v", seq, ok)
	}
	for _, id := range []string{"e7", "e1", "nope", ""} {
		if _, ok := h.lookup(id); ok {
			t.Fatalf("lookup(%q) found an entry", id)
		}
	}
}

func TestHistory_After(t *testing.T) {
	h := filled(100, 20)
	if got := seqs(h.after(14, []string{"even"})); !slices.Equal(got, []uint64{16, 18, 20}) {
		t.Fatalf("after(14, even) = %v", got)
	}
	// 20 is on "even" and "ten": once.
	if got := seqs(h.after(14, []string{"ten", "even"})); !slices.Equal(got, []uint64{16, 18, 20}) {
		t.Fatalf("after(14, ten+even) = %v", got)
	}
	if got := h.after(20, []string{"even"}); len(got) != 0 {
		t.Fatalf("after(last) = %v, want none", seqs(got))
	}
	if got := h.after(0, []string{"none"}); len(got) != 0 {
		t.Fatalf("after(0, none) = %v, want none", seqs(got))
	}
}

func TestHistory_Latest(t *testing.T) {
	if got := newHistory(3).latestID(); got != "" {
		t.Fatalf("latestID of an empty history = %q", got)
	}
	if got := filled(3, 10).latestID(); got != "e10" {
		t.Fatalf("latestID = %q, want e10", got)
	}
}

func TestHistory_Contains(t *testing.T) {
	h := filled(3, 10)
	if !h.contains("e10") || h.contains("e1") {
		t.Fatal("contains must report kept identifiers only")
	}
}

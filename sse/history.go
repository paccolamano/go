package sse

// entry is a published event with its position and topics.
type entry struct {
	seq    uint64
	ev     Event // ID always set: explicit or generated
	topics []string
}

// history keeps the most recent entries, in sequence order, and indexes them
// by identifier. Sequences in the ring are consecutive.
type history struct {
	ring  []entry
	start int // index of the oldest entry
	n     int
	index map[string]uint64
}

func newHistory(capacity int) *history {
	return &history{ring: make([]entry, capacity), index: make(map[string]uint64, capacity)}
}

// add appends e, evicting the oldest entry when the history is full. e.seq
// must follow the newest entry's.
func (h *history) add(e entry) {
	if h.n == len(h.ring) {
		delete(h.index, h.ring[h.start].ev.ID)
		h.ring[h.start] = e
		h.start = (h.start + 1) % len(h.ring)
	} else {
		h.ring[(h.start+h.n)%len(h.ring)] = e
		h.n++
	}
	h.index[e.ev.ID] = e.seq
}

// at returns the i-th oldest entry.
func (h *history) at(i int) *entry {
	return &h.ring[(h.start+i)%len(h.ring)]
}

// lookup returns the sequence of the kept entry with identifier id.
func (h *history) lookup(id string) (uint64, bool) {
	seq, ok := h.index[id]
	return seq, ok
}

// contains reports whether an entry with identifier id is kept.
func (h *history) contains(id string) bool {
	_, ok := h.index[id]
	return ok
}

// latestID returns the identifier of the newest entry, or "" when empty.
func (h *history) latestID() string {
	if h.n == 0 {
		return ""
	}
	return h.at(h.n - 1).ev.ID
}

// after returns the kept entries that follow seq and share a topic with
// topics, in order, each once.
func (h *history) after(seq uint64, topics []string) []entry {
	if h.n == 0 {
		return nil
	}
	first := 0
	if oldest := h.at(0).seq; seq >= oldest {
		first = int(seq-oldest) + 1
	}
	var out []entry
	for i := first; i < h.n; i++ {
		if e := h.at(i); sharesTopic(e.topics, topics) {
			out = append(out, *e)
		}
	}
	return out
}

// sharesTopic reports whether a and b have a topic in common.
func sharesTopic(a, b []string) bool {
	for _, x := range a {
		for _, y := range b {
			if x == y {
				return true
			}
		}
	}
	return false
}

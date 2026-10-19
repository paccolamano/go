package slogxtest

import (
	"log/slog"
	"time"
)

// Record is a captured record. Its attributes are normalized as an output
// handler would write them: the logger's attributes and groups are included,
// values are resolved, and empty attributes and groups are removed.
type Record struct {
	Time    time.Time
	Level   slog.Level
	Message string
	Attrs   []slog.Attr
}

// Attr returns the value at path, one key per group level, and reports whether
// it exists: Attr("query", "ms") is the attribute ms inside the group query.
// When a key repeats at one level, the last occurrence wins, as it would when
// the record is decoded from JSON.
func (r Record) Attr(path ...string) (slog.Value, bool) {
	if len(path) == 0 {
		return slog.Value{}, false
	}
	attrs := r.Attrs
	for i, key := range path {
		found := -1
		for j, a := range attrs {
			if a.Key == key {
				found = j
			}
		}
		if found < 0 {
			return slog.Value{}, false
		}
		v := attrs[found].Value
		if i == len(path)-1 {
			return v, true
		}
		if v.Kind() != slog.KindGroup {
			return slog.Value{}, false
		}
		attrs = v.Group()
	}
	return slog.Value{}, false
}

package sse

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"
)

func FuzzRoundTrip(f *testing.F) {
	f.Add("", "", "hello")
	f.Add("7", "progress", "50%")
	f.Add("K-1", " spaced", "a\r\nb\rc\n")
	f.Add("", "", "")
	f.Add(" x", "a:b", "  two\n\nlines")
	f.Fuzz(func(t *testing.T, id, typ, data string) {
		ev := Event{ID: id, Type: typ, Data: data}
		if ev.validate() != nil {
			return
		}
		p := newParser(bytes.NewReader(appendEvent(nil, ev)), 1<<30)
		got, err := p.next()
		if err != nil {
			t.Fatalf("next() = %v for %+v", err, ev)
		}
		want := strings.ReplaceAll(strings.ReplaceAll(data, "\r\n", "\n"), "\r", "\n")
		if got.Type != typ || got.Data != want || (id != "" && got.ID != id) {
			t.Fatalf("round trip of %+v gave %+v", ev, got)
		}
		if _, err := p.next(); !errors.Is(err, io.EOF) {
			t.Fatalf("second next() = %v, want io.EOF", err)
		}
	})
}

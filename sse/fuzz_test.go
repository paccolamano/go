package sse_test

import (
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/paccolamano/go/sse"
)

func FuzzValidate(f *testing.F) {
	f.Add("", "", "hello")
	f.Add("7", "progress", "50%")
	f.Add("a\nb", "", "")
	f.Add("", "x\ry", "")
	f.Add("", "", "\xff")
	f.Add("", "", "a\r\nb\rc\n")
	f.Fuzz(func(t *testing.T, id, typ, data string) {
		rec := httptest.NewRecorder()
		s, err := sse.NewStream(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil))
		if err != nil {
			t.Fatal(err)
		}
		err = s.Send(sse.Event{ID: id, Type: typ, Data: data})
		if errors.Is(err, sse.ErrInvalidEvent) {
			if rec.Body.Len() != 0 {
				t.Fatalf("a refused event wrote %q", rec.Body)
			}
			return
		}
		if err != nil {
			t.Fatal(err)
		}
		events, err := collect(t, sse.Decode(bytes.NewReader(rec.Body.Bytes())))
		if err != nil || len(events) != 1 {
			t.Fatalf("accepted event decoded to %d events (err %v): %q", len(events), err, rec.Body)
		}
	})
}

func FuzzDecode(f *testing.F) {
	for _, v := range conformanceVectors {
		f.Add([]byte(v.stream))
	}
	f.Add([]byte("\xEF\xBB\xBFid: a\x00\r\nretry: 12\rdata: \xff\xfe\n\n"))
	f.Fuzz(func(t *testing.T, data []byte) {
		const limit = 256
		for ev, err := range sse.Decode(bytes.NewReader(data), sse.WithMaxEventSize(limit)) {
			if err != nil {
				return
			}
			if n := len(ev.ID) + len(ev.Type) + len(ev.Data); n > limit {
				t.Fatalf("event of %d bytes exceeds the limit of %d: %+v", n, limit, ev)
			}
			if strings.Contains(ev.ID, "\x00") {
				t.Fatalf("ID contains NUL: %q", ev.ID)
			}
		}
	})
}

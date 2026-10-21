package sse_test

import (
	"errors"
	"io"
	"strings"
	"testing"
	"testing/synctest"

	"github.com/paccolamano/go/sse"
)

// collect gathers the events of seq until it ends or yields an error.
func collect(tb testing.TB, seq func(func(sse.Event, error) bool)) ([]sse.Event, error) {
	tb.Helper()
	var events []sse.Event
	for ev, err := range seq {
		if err != nil {
			return events, err
		}
		events = append(events, ev)
	}
	return events, nil
}

func decodeString(tb testing.TB, s string, opts ...sse.Option) ([]sse.Event, error) {
	tb.Helper()
	return collect(tb, sse.Decode(strings.NewReader(s), opts...))
}

func equalEvents(a, b []sse.Event) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// conformanceVectors are every example event stream of the standard's
// "Server-sent events" section (contracts/wire-format.md).
var conformanceVectors = []struct {
	name   string
	stream string
	want   []sse.Event
}{
	{
		name:   "introduction messages",
		stream: "data: This is the first message.\n\ndata: This is the second message, it\ndata: has two lines.\n\ndata: This is the third message.\n\n",
		want: []sse.Event{
			{Data: "This is the first message."},
			{Data: "This is the second message, it\nhas two lines."},
			{Data: "This is the third message."},
		},
	},
	{
		name:   "introduction typed events",
		stream: "event: add\ndata: 73857293\n\nevent: remove\ndata: 2153\n\nevent: add\ndata: 113411\n\n",
		want: []sse.Event{
			{Type: "add", Data: "73857293"},
			{Type: "remove", Data: "2153"},
			{Type: "add", Data: "113411"},
		},
	},
	{
		name:   "stock ticker",
		stream: "data: YHOO\ndata: +2\ndata: 10\n\n",
		want:   []sse.Event{{Data: "YHOO\n+2\n10"}},
	},
	{
		name:   "four blocks",
		stream: ": test stream\n\ndata: first event\nid: 1\n\ndata:second event\nid\n\ndata:  third event\n\n",
		want: []sse.Event{
			{ID: "1", Data: "first event"},
			{ID: "", Data: "second event"},
			{ID: "", Data: " third event"},
		},
	},
	{
		name:   "four blocks without the final empty line",
		stream: ": test stream\n\ndata: first event\nid: 1\n\ndata:second event\nid\n\ndata:  third event\n",
		want: []sse.Event{
			{ID: "1", Data: "first event"},
			{ID: "", Data: "second event"},
		},
	},
	{
		name:   "empty and newline data",
		stream: "data\n\ndata\ndata\n\ndata:",
		want:   []sse.Event{{Data: ""}, {Data: "\n"}},
	},
	{
		name:   "space after the colon",
		stream: "data:test\n\ndata: test\n\n",
		want:   []sse.Event{{Data: "test"}, {Data: "test"}},
	},
}

func TestConformance_StandardExamples(t *testing.T) {
	for _, tt := range conformanceVectors {
		t.Run(tt.name, func(t *testing.T) {
			got, err := decodeString(t, tt.stream)
			if err != nil {
				t.Fatal(err)
			}
			if !equalEvents(got, tt.want) {
				t.Fatalf("got %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestDecode_LineEndings(t *testing.T) {
	lf := "event: a\nid: 1\ndata: x\ndata: y\n\ndata: z\n\n"
	want := []sse.Event{{ID: "1", Type: "a", Data: "x\ny"}, {ID: "1", Data: "z"}}
	for name, stream := range map[string]string{
		"LF":   lf,
		"CRLF": strings.ReplaceAll(lf, "\n", "\r\n"),
		"CR":   strings.ReplaceAll(lf, "\n", "\r"),
	} {
		t.Run(name, func(t *testing.T) {
			got, err := decodeString(t, stream)
			if err != nil {
				t.Fatal(err)
			}
			if !equalEvents(got, want) {
				t.Fatalf("got %+v, want %+v", got, want)
			}
		})
	}
}

// chunkReader returns one chunk per Read call.
type chunkReader struct{ chunks []string }

func (r *chunkReader) Read(p []byte) (int, error) {
	if len(r.chunks) == 0 {
		return 0, io.EOF
	}
	n := copy(p, r.chunks[0])
	r.chunks[0] = r.chunks[0][n:]
	if r.chunks[0] == "" {
		r.chunks = r.chunks[1:]
	}
	return n, nil
}

func TestDecode_CRSplitAcrossReads(t *testing.T) {
	t.Run("one line break", func(t *testing.T) {
		got, err := collect(t, sse.Decode(&chunkReader{chunks: []string{"data: x\r", "\ndata: y\r\n\r\n"}}))
		if err != nil {
			t.Fatal(err)
		}
		if want := []sse.Event{{Data: "x\ny"}}; !equalEvents(got, want) {
			t.Fatalf("got %+v, want %+v", got, want)
		}
	})
	t.Run("no wait after CR", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			pr, pw := io.Pipe()
			events := make(chan sse.Event, 1)
			go func() {
				for ev, err := range sse.Decode(pr) {
					if err != nil {
						break
					}
					events <- ev
				}
				close(events)
			}()
			if _, err := pw.Write([]byte("data: x\r\r")); err != nil {
				t.Fatal(err)
			}
			synctest.Wait()
			select {
			case ev := <-events:
				if ev.Data != "x" {
					t.Fatalf("got %+v", ev)
				}
			default:
				t.Fatal("the event was not dispatched before the next byte arrived")
			}
			_ = pw.Close()
			for range events {
			}
		})
	})
}

func TestDecode_BOM(t *testing.T) {
	got, err := decodeString(t, "\xEF\xBB\xBFdata: a\n\n\xEF\xBB\xBFdata: b\n\ndata: c\n\n")
	if err != nil {
		t.Fatal(err)
	}
	if want := []sse.Event{{Data: "a"}, {Data: "c"}}; !equalEvents(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestDecode_Fields(t *testing.T) {
	tests := []struct {
		name   string
		stream string
		want   []sse.Event
	}{
		{"id with NUL ignored", "id: a\x00b\ndata: x\n\n", []sse.Event{{Data: "x"}}},
		{"id without value resets", "id: 1\ndata: a\n\nid\ndata: b\n\n", []sse.Event{{ID: "1", Data: "a"}, {Data: "b"}}},
		{"unknown field ignored", "foo: bar\ndata: x\n\n", []sse.Event{{Data: "x"}}},
		{"data without colon", "data\ndata: x\n\n", []sse.Event{{Data: "\nx"}}},
		{"one leading space removed", "data:  two\n\n", []sse.Event{{Data: " two"}}},
		{"comment ignored", ": hello\ndata: x\n\n", []sse.Event{{Data: "x"}}},
		{"empty data resets the type", "event: foo\n\ndata: x\n\n", []sse.Event{{Data: "x"}}},
		{"last ID persists", "id: 5\ndata: a\n\ndata: b\n\n", []sse.Event{{ID: "5", Data: "a"}, {ID: "5", Data: "b"}}},
		{"value with colons", "event: a:b\ndata: c: d\n\n", []sse.Event{{Type: "a:b", Data: "c: d"}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := decodeString(t, tt.stream)
			if err != nil {
				t.Fatal(err)
			}
			if !equalEvents(got, tt.want) {
				t.Fatalf("got %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestDecode_InvalidUTF8(t *testing.T) {
	got, err := decodeString(t, "data: a\xffb\n\n")
	if err != nil {
		t.Fatal(err)
	}
	if want := []sse.Event{{Data: "a�b"}}; !equalEvents(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestDecode_EventTooLarge(t *testing.T) {
	tests := []struct {
		name   string
		stream string
		want   []sse.Event
	}{
		{"long line", "data: ok\n\ndata: 0123456789a\n\n", []sse.Event{{Data: "ok"}}},
		{"buffers together", "data: ok\n\ndata: 0123456789\ndata: 0123456789\n\n", []sse.Event{{Data: "ok"}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := decodeString(t, tt.stream, sse.WithMaxEventSize(16))
			if !errors.Is(err, sse.ErrEventTooLarge) {
				t.Fatalf("err = %v, want ErrEventTooLarge", err)
			}
			if !equalEvents(got, tt.want) {
				t.Fatalf("got %+v before the error, want %+v", got, tt.want)
			}
		})
	}
}

var errBoom = errors.New("boom")

type failingReader struct {
	data string
	done bool
}

func (r *failingReader) Read(p []byte) (int, error) {
	if r.done {
		return 0, errBoom
	}
	r.done = true
	return copy(p, r.data), nil
}

func TestDecode_ReadError(t *testing.T) {
	var events []sse.Event
	var errs []error
	for ev, err := range sse.Decode(&failingReader{data: "data: a\n\n"}) {
		if err != nil {
			errs = append(errs, err)
			continue
		}
		events = append(events, ev)
	}
	if want := []sse.Event{{Data: "a"}}; !equalEvents(events, want) {
		t.Fatalf("events = %+v, want %+v", events, want)
	}
	if len(errs) != 1 || !errors.Is(errs[0], errBoom) {
		t.Fatalf("errors = %v, want one error wrapping errBoom", errs)
	}
}

// endlessReader produces events forever and counts its Read calls.
type endlessReader struct{ reads int }

func (r *endlessReader) Read(p []byte) (int, error) {
	r.reads++
	const ev = "data: x\n\n"
	n := 0
	for n+len(ev) <= len(p) {
		n += copy(p[n:], ev)
	}
	return n, nil
}

func TestDecode_StopEarly(t *testing.T) {
	r := &endlessReader{}
	for range sse.Decode(r) {
		break
	}
	if r.reads > 2 {
		t.Fatalf("Read called %d times after stopping at the first event", r.reads)
	}
}

func TestDecode_InvalidOption(t *testing.T) {
	var errs []error
	for _, err := range sse.Decode(strings.NewReader("data: x\n\n"), sse.WithHistory(1)) {
		errs = append(errs, err)
	}
	if len(errs) != 1 || !errors.Is(errs[0], sse.ErrInvalidConfig) {
		t.Fatalf("errors = %v, want one ErrInvalidConfig", errs)
	}
}

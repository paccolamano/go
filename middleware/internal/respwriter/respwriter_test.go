package respwriter

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/paccolamano/go/middleware"
)

func TestWriter_Observer(t *testing.T) {
	tests := []struct {
		name        string
		do          func(w *Writer)
		wantStatus  int
		wantBytes   int64
		wantStarted bool
	}{
		{"nothing written", func(*Writer) {}, 0, 0, false},
		{"write without status", func(w *Writer) { _, _ = w.Write([]byte("hello")) }, http.StatusOK, 5, true},
		{"explicit status", func(w *Writer) { w.WriteHeader(http.StatusCreated) }, http.StatusCreated, 0, true},
		{"first status wins", func(w *Writer) {
			w.WriteHeader(http.StatusAccepted)
			w.WriteHeader(http.StatusTeapot)
		}, http.StatusAccepted, 0, true},
		{"informational is not recorded", func(w *Writer) { w.WriteHeader(http.StatusEarlyHints) }, 0, 0, false},
		{"informational then final", func(w *Writer) {
			w.WriteHeader(http.StatusEarlyHints)
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte("no"))
		}, http.StatusNotFound, 2, true},
		{"101 is final", func(w *Writer) { w.WriteHeader(http.StatusSwitchingProtocols) }, http.StatusSwitchingProtocols, 0, true},
		{"bytes accumulate", func(w *Writer) {
			_, _ = w.Write([]byte("ab"))
			_, _ = w.Write([]byte("cde"))
		}, http.StatusOK, 5, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := &fakeWriter{header: http.Header{}}
			w := New(fake)
			tt.do(w)
			if w.Status() != tt.wantStatus || w.Bytes() != tt.wantBytes || w.Started() != tt.wantStarted {
				t.Errorf("got status=%d bytes=%d started=%v, want %d %d %v",
					w.Status(), w.Bytes(), w.Started(), tt.wantStatus, tt.wantBytes, tt.wantStarted)
			}
			if w.Hijacked() {
				t.Error("Hijacked() = true, want false")
			}
		})
	}
}

func TestWriter_UnwrapAndHeader(t *testing.T) {
	rec := httptest.NewRecorder()
	w := New(rec)
	if w.Unwrap() != rec {
		t.Error("Unwrap() did not return the underlying writer")
	}
	w.Header().Set("X-Test", "1")
	if rec.Header().Get("X-Test") != "1" {
		t.Error("Header() is not the underlying header map")
	}
}

func TestWriter_HijackNotSupported(t *testing.T) {
	w := New(httptest.NewRecorder())
	if _, _, err := w.Hijack(); !errors.Is(err, http.ErrNotSupported) {
		t.Errorf("Hijack() error = %v, want http.ErrNotSupported", err)
	}
	if w.Hijacked() || w.Started() {
		t.Error("a failed hijack must not change the state")
	}
}

func TestWriter_FlushReachesClient(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, _ *http.Request) {
		w := New(rw)
		_, _ = w.Write([]byte("first"))
		w.Flush()
		<-release
		_, _ = w.Write([]byte("second"))
	}))
	t.Cleanup(srv.Close)

	resp := get(t, srv.URL)
	buf := make([]byte, len("first"))
	if _, err := io.ReadFull(resp.Body, buf); err != nil || string(buf) != "first" {
		t.Fatalf("read before release = %q, %v; want %q", buf, err, "first")
	}
	close(release)
	rest, err := io.ReadAll(resp.Body)
	if err != nil || string(rest) != "second" {
		t.Fatalf("rest = %q, %v; want %q", rest, err, "second")
	}
}

func TestWriter_ResponseControllerReachesServer(t *testing.T) {
	errc := make(chan error, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, _ *http.Request) {
		w := New(rw)
		errc <- http.NewResponseController(w).SetWriteDeadline(time.Now().Add(time.Minute))
	}))
	t.Cleanup(srv.Close)

	_ = get(t, srv.URL)
	if err := <-errc; err != nil {
		t.Fatalf("SetWriteDeadline through the wrapper: %v", err)
	}
}

func TestWriter_HijackThroughWrapper(t *testing.T) {
	type state struct {
		status   int
		hijacked bool
		started  bool
	}
	states := make(chan state, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, _ *http.Request) {
		w := New(rw)
		conn, brw, err := w.Hijack()
		if err != nil {
			t.Errorf("Hijack: %v", err)
			return
		}
		defer func() { _ = conn.Close() }()
		states <- state{w.Status(), w.Hijacked(), w.Started()}
		_, _ = brw.WriteString("HTTP/1.1 200 OK\r\nContent-Length: 2\r\nConnection: close\r\n\r\nok")
		_ = brw.Flush()
	}))
	t.Cleanup(srv.Close)

	resp := get(t, srv.URL)
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "ok" {
		t.Errorf("body = %q, want ok", body)
	}
	if got := <-states; got != (state{http.StatusSwitchingProtocols, true, true}) {
		t.Errorf("state after hijack = %+v", got)
	}
}

func TestReject_Default(t *testing.T) {
	rec := httptest.NewRecorder()
	Reject(rec, newRequest(t), nil, middleware.Rejection{Status: http.StatusTooManyRequests})
	if rec.Code != http.StatusTooManyRequests {
		t.Errorf("status = %d, want 429", rec.Code)
	}
	if got := strings.TrimSpace(rec.Body.String()); got != http.StatusText(http.StatusTooManyRequests) {
		t.Errorf("body = %q, want the reason phrase", got)
	}
	if rec.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Error("missing X-Content-Type-Options: nosniff")
	}
	if !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/plain") {
		t.Errorf("Content-Type = %q, want text/plain", rec.Header().Get("Content-Type"))
	}
}

var errCause = errors.New("cause")

func TestReject_CustomFunc(t *testing.T) {
	rec := httptest.NewRecorder()
	want := middleware.Rejection{Status: http.StatusRequestEntityTooLarge, Err: errCause}
	var got middleware.Rejection
	Reject(rec, newRequest(t), func(w http.ResponseWriter, _ *http.Request, rej middleware.Rejection) {
		got = rej
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTeapot)
		_, _ = io.WriteString(w, `{"error":"x"}`)
	}, want)
	if got != want {
		t.Errorf("rejection = %+v, want %+v", got, want)
	}
	if rec.Code != http.StatusTeapot || rec.Body.String() != `{"error":"x"}` {
		t.Errorf("response = %d %q, want the custom one", rec.Code, rec.Body.String())
	}
}

func TestReject_FallbackStatus(t *testing.T) {
	t.Run("writes nothing", func(t *testing.T) {
		rec := httptest.NewRecorder()
		Reject(rec, newRequest(t), func(http.ResponseWriter, *http.Request, middleware.Rejection) {},
			middleware.Rejection{Status: http.StatusServiceUnavailable})
		if rec.Code != http.StatusServiceUnavailable || rec.Body.Len() != 0 {
			t.Errorf("response = %d %q, want 503 with an empty body", rec.Code, rec.Body.String())
		}
	})
	t.Run("only sets headers", func(t *testing.T) {
		rec := httptest.NewRecorder()
		Reject(rec, newRequest(t), func(w http.ResponseWriter, _ *http.Request, _ middleware.Rejection) {
			w.Header().Set("X-Reason", "busy")
		}, middleware.Rejection{Status: http.StatusServiceUnavailable})
		if rec.Code != http.StatusServiceUnavailable || rec.Header().Get("X-Reason") != "busy" {
			t.Errorf("response = %d %v, want 503 with X-Reason", rec.Code, rec.Header())
		}
	})
}

// fakeWriter accepts every status code and write, unlike
// httptest.ResponseRecorder, which treats 1xx codes as final.
type fakeWriter struct {
	header http.Header
	codes  []int
	body   strings.Builder
}

func (f *fakeWriter) Header() http.Header         { return f.header }
func (f *fakeWriter) WriteHeader(code int)        { f.codes = append(f.codes, code) }
func (f *fakeWriter) Write(p []byte) (int, error) { return f.body.Write(p) }

func TestWriter_ForwardsInformational(t *testing.T) {
	fake := &fakeWriter{header: http.Header{}}
	w := New(fake)
	w.WriteHeader(http.StatusEarlyHints)
	w.WriteHeader(http.StatusOK)
	if len(fake.codes) != 2 || fake.codes[0] != http.StatusEarlyHints || fake.codes[1] != http.StatusOK {
		t.Errorf("forwarded codes = %v, want [103 200]", fake.codes)
	}
}

func newRequest(t *testing.T) *http.Request {
	t.Helper()
	return httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil)
}

func get(t *testing.T, url string) *http.Response {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	return resp
}

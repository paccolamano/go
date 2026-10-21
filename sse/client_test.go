package sse_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/paccolamano/go/sse"
)

// attempt is one request received by a script.
type attempt struct {
	path   string
	header http.Header
	body   string
	at     time.Time
}

// script serves one step per request, repeating the last step, and records
// every request.
type script struct {
	mu       sync.Mutex
	steps    []http.HandlerFunc
	attempts []attempt
}

func newScript(steps ...http.HandlerFunc) *script { return &script{steps: steps} }

func (s *script) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	s.mu.Lock()
	n := len(s.attempts)
	s.attempts = append(s.attempts, attempt{path: r.URL.Path, header: r.Header.Clone(), body: string(body), at: time.Now()})
	step := s.steps[min(n, len(s.steps)-1)]
	s.mu.Unlock()
	step(w, r)
}

func (s *script) list() []attempt {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.attempts)
}

// stream answers an event stream with body, then ends it.
func stream(body string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, body)
	}
}

func status(code int) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(code) }
}

func newClient(tb testing.TB, opts ...sse.Option) *sse.Client {
	tb.Helper()
	c, err := sse.NewClient(opts...)
	if err != nil {
		tb.Fatal(err)
	}
	return c
}

func request(tb testing.TB, ctx context.Context, method, path string, body io.Reader) *http.Request {
	tb.Helper()
	req, err := http.NewRequestWithContext(ctx, method, baseURL+path, body)
	if err != nil {
		tb.Fatal(err)
	}
	return req
}

// consume iterates over the events of req until the iteration ends.
func consume(c *sse.Client, req *http.Request) ([]sse.Event, error) {
	var events []sse.Event
	for ev, err := range c.Events(req) {
		if err != nil {
			return events, err
		}
		events = append(events, ev)
	}
	return events, nil
}

func TestEvents_Basic(t *testing.T) {
	e2e(t, func(e *env) {
		t := e.T
		e.Start(newScript(stream("id: 1\nevent: a\ndata: x\n\n"), status(http.StatusNoContent)), 0)
		events, err := consume(newClient(t, sse.WithHTTPClient(e.Client)), request(t, t.Context(), http.MethodGet, "/", nil))
		if err != nil {
			t.Fatal(err)
		}
		if want := []sse.Event{{ID: "1", Type: "a", Data: "x"}}; !equalEvents(events, want) {
			t.Fatalf("got %+v", events)
		}
	})
}

func TestEvents_Reconnect(t *testing.T) {
	e2e(t, func(e *env) {
		t := e.T
		s := newScript(
			stream("id: e1\ndata: 1\n\nid: e2\ndata: 2\n\nid: e3\ndata: 3\n\n"),
			stream("id: e4\ndata: 4\n\n"),
			status(http.StatusNoContent),
		)
		e.Start(s, 0)
		events, err := consume(newClient(t, sse.WithHTTPClient(e.Client)), request(t, t.Context(), http.MethodGet, "/", nil))
		if err != nil {
			t.Fatal(err)
		}
		if got := ids(events); !slices.Equal(got, []string{"e1", "e2", "e3", "e4"}) {
			t.Fatalf("got %v", got)
		}
		a := s.list()
		if got := a[1].header.Get("Last-Event-ID"); got != "e3" {
			t.Fatalf("second attempt Last-Event-ID %q, want e3", got)
		}
		if gap := a[1].at.Sub(a[0].at); gap != 3*time.Second {
			t.Fatalf("reconnected after %v, want the default 3s", gap)
		}
	})
}

func TestEvents_Retry(t *testing.T) {
	e2e(t, func(e *env) {
		t := e.T
		s := newScript(stream("retry: 10000\ndata: x\n\n"), status(http.StatusNoContent))
		e.Start(s, 0)
		if _, err := consume(newClient(t, sse.WithHTTPClient(e.Client)), request(t, t.Context(), http.MethodGet, "/", nil)); err != nil {
			t.Fatal(err)
		}
		if a := s.list(); a[1].at.Sub(a[0].at) != 10*time.Second {
			t.Fatalf("reconnected after %v, want 10s", a[1].at.Sub(a[0].at))
		}
	})
}

func TestEvents_Statuses(t *testing.T) {
	tests := []struct {
		name string
		step http.HandlerFunc
		want *sse.ResponseError
	}{
		{"204", status(http.StatusNoContent), nil},
		{"401", status(http.StatusUnauthorized), &sse.ResponseError{StatusCode: http.StatusUnauthorized, ContentType: "text/plain; charset=utf-8"}},
		{"text/html", func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "text/html")
			_, _ = io.WriteString(w, "<p>hi</p>")
		}, &sse.ResponseError{StatusCode: http.StatusOK, ContentType: "text/html"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e2e(t, func(e *env) {
				t := e.T
				s := newScript(tt.step)
				e.Start(s, 0)
				_, err := consume(newClient(t, sse.WithHTTPClient(e.Client)), request(t, t.Context(), http.MethodGet, "/", nil))
				var re *sse.ResponseError
				switch {
				case tt.want == nil && err != nil:
					t.Fatalf("err = %v, want nil", err)
				case tt.want != nil && (!errors.As(err, &re) || re.StatusCode != tt.want.StatusCode ||
					(tt.want.StatusCode == http.StatusOK && re.ContentType != tt.want.ContentType)):
					t.Fatalf("err = %v, want %v", err, tt.want)
				case tt.want != nil && !strings.Contains(err.Error(), fmt.Sprintf("status %d", tt.want.StatusCode)):
					t.Fatalf("message %q does not name the status", err)
				}
				if n := len(s.list()); n != 1 {
					t.Fatalf("%d attempts, want 1", n)
				}
			})
		})
	}
}

func TestEvents_RepeatRequest(t *testing.T) {
	e2e(t, func(e *env) {
		t := e.T
		s := newScript(stream("data: x\n\n"), status(http.StatusNoContent))
		e.Start(s, 0)
		req := request(t, t.Context(), http.MethodPost, "/", strings.NewReader("payload"))
		req.Header.Set("Authorization", "Bearer test-token")
		if _, err := consume(newClient(t, sse.WithHTTPClient(e.Client)), req); err != nil {
			t.Fatal(err)
		}
		for i, a := range s.list() {
			if a.header.Get("Authorization") != "Bearer test-token" || a.body != "payload" {
				t.Fatalf("attempt %d: header %q, body %q", i, a.header.Get("Authorization"), a.body)
			}
		}
	})
}

func TestEvents_BodyNotReplayable(t *testing.T) {
	e2e(t, func(e *env) {
		t := e.T
		s := newScript(stream("data: x\n\n"))
		e.Start(s, 0)
		req := request(t, t.Context(), http.MethodPost, "/", io.MultiReader(strings.NewReader("once")))
		if _, err := consume(newClient(t, sse.WithHTTPClient(e.Client)), req); err == nil {
			t.Fatal("a body that cannot be sent again was accepted")
		}
		if n := len(s.list()); n != 0 {
			t.Fatalf("%d attempts, want none", n)
		}
	})
}

func TestEvents_Cancel(t *testing.T) {
	e2e(t, func(e *env) {
		t := e.T
		e.Start(newScript(func(w http.ResponseWriter, r *http.Request) {
			stream("data: first\n\n")(w, r)
			w.(http.Flusher).Flush()
			<-r.Context().Done()
		}), 0)
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		var errs []error
		for _, err := range newClient(t, sse.WithHTTPClient(e.Client)).Events(request(t, ctx, http.MethodGet, "/", nil)) {
			if err != nil {
				errs = append(errs, err)
				continue
			}
			cancel()
		}
		if len(errs) != 1 || !errors.Is(errs[0], context.Canceled) {
			t.Fatalf("errors %v, want one context.Canceled", errs)
		}
	})
	e2e(t, func(e *env) {
		t := e.T
		gone := make(chan struct{})
		e.Start(newScript(func(w http.ResponseWriter, r *http.Request) {
			stream("data: first\n\n")(w, r)
			w.(http.Flusher).Flush()
			<-r.Context().Done()
			close(gone)
		}), 0)
		for range newClient(t, sse.WithHTTPClient(e.Client)).Events(request(t, t.Context(), http.MethodGet, "/", nil)) {
			break
		}
		// Over HTTP/1.1 the transport first tries to drain the body for up to
		// 50 ms before closing the connection: wait in virtual time.
		select {
		case <-gone:
		case <-time.After(time.Second):
			t.Fatal("stopping the iteration did not close the connection")
		}
	})
}

func TestEvents_Position(t *testing.T) {
	e2e(t, func(e *env) {
		t := e.T
		s := newScript(stream("id: K-7\n\n"), status(http.StatusNoContent))
		e.Start(s, 0)
		if _, err := consume(newClient(t, sse.WithHTTPClient(e.Client)), request(t, t.Context(), http.MethodGet, "/", nil)); err != nil {
			t.Fatal(err)
		}
		if got := s.list()[1].header.Get("Last-Event-ID"); got != "K-7" {
			t.Fatalf("Last-Event-ID %q after a position, want K-7", got)
		}
	})
}

func TestEvents_Headers(t *testing.T) {
	e2e(t, func(e *env) {
		t := e.T
		s := newScript(status(http.StatusNoContent))
		e.Start(s, 0)
		c := newClient(t, sse.WithHTTPClient(e.Client))
		req := request(t, t.Context(), http.MethodGet, "/", nil)
		req.Header.Set("Last-Event-ID", "saved-42")
		if _, err := consume(c, req); err != nil {
			t.Fatal(err)
		}
		custom := request(t, t.Context(), http.MethodGet, "/", nil)
		custom.Header.Set("Accept", "text/event-stream, */*;q=0.1")
		if _, err := consume(c, custom); err != nil {
			t.Fatal(err)
		}
		a := s.list()
		if a[0].header.Get("Accept") != "text/event-stream" || a[0].header.Get("Last-Event-ID") != "saved-42" {
			t.Fatalf("first request headers %v", a[0].header)
		}
		if a[1].header.Get("Accept") != "text/event-stream, */*;q=0.1" {
			t.Fatalf("the developer's Accept was replaced: %q", a[1].header.Get("Accept"))
		}
	})
}

// flakyTransport fails the first n round trips with a network error.
type flakyTransport struct {
	base http.RoundTripper
	mu   sync.Mutex
	n    int
	at   []time.Time
}

func (f *flakyTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	f.mu.Lock()
	f.at = append(f.at, time.Now())
	fail := len(f.at) <= f.n
	f.mu.Unlock()
	if fail {
		return nil, errors.New("connection refused")
	}
	return f.base.RoundTrip(r)
}

func TestEvents_Backoff(t *testing.T) {
	e2e(t, func(e *env) {
		t := e.T
		e.Start(newScript(stream("data: x\n\n"), status(http.StatusNoContent)), 0)
		ft := &flakyTransport{base: e.Client.Transport, n: 6}
		c := newClient(t, sse.WithHTTPClient(&http.Client{Transport: ft}))
		if _, err := consume(c, request(t, t.Context(), http.MethodGet, "/", nil)); err != nil {
			t.Fatal(err)
		}
		var gaps []time.Duration
		for i := 1; i < len(ft.at); i++ {
			gaps = append(gaps, ft.at[i].Sub(ft.at[i-1]))
		}
		want := []time.Duration{3, 6, 12, 24, 30, 30, 3}
		for i := range want {
			want[i] *= time.Second
		}
		if !slices.Equal(gaps, want) {
			t.Fatalf("gaps %v, want %v", gaps, want)
		}
	})
}

func TestEvents_CancelDuringBackoff(t *testing.T) {
	e2e(t, func(e *env) {
		t := e.T
		ft := &flakyTransport{base: e.Client.Transport, n: 1 << 30} // always failing
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		time.AfterFunc(10*time.Second, cancel) // during the third wait
		_, err := consume(newClient(t, sse.WithHTTPClient(&http.Client{Transport: ft})), request(t, ctx, http.MethodGet, "/", nil))
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("err = %v, want context.Canceled", err)
		}
		if n := len(ft.at); n != 3 {
			t.Fatalf("%d attempts before the cancellation, want 3 (at 0, 3 and 9 s)", n)
		}
	})
}

func TestEvents_TooLarge(t *testing.T) {
	e2e(t, func(e *env) {
		t := e.T
		s := newScript(stream("data: " + strings.Repeat("x", 32) + "\n\n"))
		e.Start(s, 0)
		_, err := consume(newClient(t, sse.WithHTTPClient(e.Client), sse.WithMaxEventSize(16)), request(t, t.Context(), http.MethodGet, "/", nil))
		if !errors.Is(err, sse.ErrEventTooLarge) {
			t.Fatalf("err = %v, want ErrEventTooLarge", err)
		}
		if n := len(s.list()); n != 1 {
			t.Fatalf("%d attempts, want 1", n)
		}
	})
}

func TestEvents_Redirect(t *testing.T) {
	e2e(t, func(e *env) {
		t := e.T
		target := newScript(stream("id: r1\ndata: x\n\n"), status(http.StatusNoContent))
		mux := http.NewServeMux()
		mux.HandleFunc("/a", func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, "/b", http.StatusTemporaryRedirect)
		})
		mux.Handle("/b", target)
		e.Start(mux, 0)
		if _, err := consume(newClient(t, sse.WithHTTPClient(e.Client)), request(t, t.Context(), http.MethodGet, "/a", nil)); err != nil {
			t.Fatal(err)
		}
		if got := target.list()[1].header.Get("Last-Event-ID"); got != "r1" {
			t.Fatalf("redirected reconnection carried Last-Event-ID %q, want r1", got)
		}
	})
}

func TestClient_Options(t *testing.T) {
	for _, opt := range []sse.Option{sse.WithHTTPClient(nil), sse.WithHistory(1)} {
		if _, err := sse.NewClient(opt); !errors.Is(err, sse.ErrInvalidConfig) {
			t.Errorf("NewClient = %v, want ErrInvalidConfig", err)
		}
	}
}

func TestClient_Concurrent(t *testing.T) {
	e2e(t, func(e *env) {
		t := e.T
		x := newScript(stream("retry: 1000\nid: x1\ndata: x\n\n"), stream("id: x2\ndata: x\n\n"), status(http.StatusNoContent))
		y := newScript(stream("retry: 2000\nid: y1\ndata: y\n\n"), stream("id: y2\ndata: y\n\n"), status(http.StatusNoContent))
		mux := http.NewServeMux()
		mux.Handle("/x", x)
		mux.Handle("/y", y)
		e.Start(mux, 0)
		c := newClient(t, sse.WithHTTPClient(e.Client))
		var wg sync.WaitGroup
		got := make(map[string][]string)
		var mu sync.Mutex
		for _, path := range []string{"/x", "/y"} {
			wg.Go(func() {
				events, err := consume(c, request(t, t.Context(), http.MethodGet, path, nil))
				if err != nil {
					t.Error(err)
				}
				mu.Lock()
				got[path] = ids(events)
				mu.Unlock()
			})
		}
		wg.Wait()
		if !slices.Equal(got["/x"], []string{"x1", "x2"}) || !slices.Equal(got["/y"], []string{"y1", "y2"}) {
			t.Fatalf("got %v", got)
		}
		for _, tc := range []struct {
			s    *script
			last string
			gap  time.Duration
		}{{x, "x1", time.Second}, {y, "y1", 2 * time.Second}} {
			a := tc.s.list()
			if a[1].header.Get("Last-Event-ID") != tc.last || a[1].at.Sub(a[0].at) != tc.gap {
				t.Fatalf("%s: Last-Event-ID %q after %v", tc.last, a[1].header.Get("Last-Event-ID"), a[1].at.Sub(a[0].at))
			}
		}
	})
}

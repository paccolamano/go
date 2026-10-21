package sse_test

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/paccolamano/go/sse"
)

func TestStream_Immediate(t *testing.T) {
	e2e(t, func(e *env) {
		t := e.T
		release := make(chan struct{})
		e.Start(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			s, err := sse.NewStream(w, r)
			if err != nil {
				t.Error(err)
				return
			}
			if err := s.Send(sse.Event{Type: "progress", Data: "50%"}); err != nil {
				t.Error(err)
			}
			<-release
		}), 0)
		events, _ := readEvents(t, e.Get(t, t.Context(), "/", nil).Body)
		ev := <-events
		if ev.Type != "progress" || ev.Data != "50%" {
			t.Fatalf("got %+v", ev)
		}
		close(release)
	})
}

func TestStream_IdleBeyondWriteTimeout(t *testing.T) {
	e2e(t, func(e *env) {
		t := e.T
		release := make(chan struct{})
		sent := make(chan error, 1)
		e.Start(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			s, err := sse.NewStream(w, r)
			if err != nil {
				sent <- err
				return
			}
			<-release
			sent <- s.Send(sse.Event{Data: "after a minute"})
			<-r.Context().Done()
		}), 10*time.Second)
		events, _ := readEvents(t, e.Get(t, t.Context(), "/", nil).Body)
		time.Sleep(60 * time.Second)
		close(release)
		if err := <-sent; err != nil {
			t.Fatalf("Send after an idle minute: %v", err)
		}
		if ev := <-events; ev.Data != "after a minute" {
			t.Fatalf("got %+v", ev)
		}
	})
}

func TestStream_StalledClient(t *testing.T) {
	e2e(t, func(e *env) {
		t := e.T
		type result struct {
			took       time.Duration
			err, again error
		}
		done := make(chan result, 1)
		e.Start(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			s, err := sse.NewStream(w, r, sse.WithWriteTimeout(2*time.Second))
			if err != nil {
				done <- result{err: err}
				return
			}
			ev := sse.Event{Data: strings.Repeat("x", 1024)}
			for {
				start := time.Now()
				if err := s.Send(ev); err != nil {
					done <- result{took: time.Since(start), err: err, again: s.Send(ev)}
					return
				}
			}
		}), 0)
		resp := e.Get(t, t.Context(), "/", nil) // the body is never read
		defer func() { _ = resp.Body.Close() }()
		res := <-done
		if res.err == nil || res.took > 2*time.Second {
			t.Fatalf("failing send took %v with error %v", res.took, res.err)
		}
		if res.again == nil || res.again.Error() != res.err.Error() {
			t.Fatalf("send after the failure returned %v, want %v", res.again, res.err)
		}
	})
}

func TestStream_ClientGone(t *testing.T) {
	e2e(t, func(e *env) {
		t := e.T
		ready := make(chan *sse.Stream, 1)
		release := make(chan struct{})
		sent := make(chan error, 1)
		e.Start(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			s, err := sse.NewStream(w, r)
			if err != nil {
				t.Error(err)
				return
			}
			ready <- s
			<-release
			sent <- s.Send(sse.Event{Data: "too late"})
		}), 0)
		ctx, cancel := context.WithCancel(t.Context())
		resp := e.Get(t, ctx, "/", nil)
		<-ready
		cancel()
		_ = resp.Body.Close()
		synctest.Wait()
		close(release)
		if err := <-sent; err == nil {
			t.Fatal("Send succeeded after the client left")
		}
	})
}

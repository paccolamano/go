package lifecycle_test

import (
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/paccolamano/go/lifecycle"
)

func sleepHandler(d time.Duration) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(d):
			_, _ = io.WriteString(w, "done")
		case <-r.Context().Done():
		}
	})
}

func TestHTTPServer_InFlight(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ln := newPipeNet()
		srv := &http.Server{Handler: sleepHandler(3 * time.Second)}
		app := mustNew(t)
		app.Add("http", lifecycle.HTTPServer(srv, lifecycle.WithListener(ln)))

		ctx, cancel := context.WithCancel(t.Context())
		done := runAsync(ctx, app)
		client := newPipeClient(ln)
		var results []<-chan response
		for range 100 {
			results = append(results, getAsync(t, t.Context(), client, baseURL+"/"))
		}
		synctest.Wait()
		cancel()

		for i, ch := range results {
			r := <-ch
			if r.err != nil || r.status != http.StatusOK || r.body != "done" {
				t.Fatalf("request %d = %+v, want 200 done", i, r)
			}
		}
		if err := <-done; err != nil {
			t.Fatalf("Run = %v, want nil", err)
		}
	})
}

func TestHTTPServer_NewConnectionsRefused(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ln := newPipeNet()
		srv := &http.Server{Handler: sleepHandler(3 * time.Second)}
		app := mustNew(t)
		app.Add("http", lifecycle.HTTPServer(srv, lifecycle.WithListener(ln)))

		ctx, cancel := context.WithCancel(t.Context())
		done := runAsync(ctx, app)
		inFlight := getAsync(t, t.Context(), newPipeClient(ln), baseURL+"/")
		synctest.Wait()
		cancel()
		synctest.Wait() // the server's turn has begun

		if _, err := ln.Dial(t.Context(), "tcp", "x"); !errors.Is(err, net.ErrClosed) {
			t.Fatalf("Dial during the server's turn = %v, want net.ErrClosed", err)
		}
		if r := <-inFlight; r.status != http.StatusOK {
			t.Fatalf("request in progress = %+v, want 200", r)
		}
		if err := <-done; err != nil {
			t.Fatalf("Run = %v, want nil", err)
		}
	})
}

func TestHTTPServer_DeadlineCloses(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ln := newPipeNet()
		srv := &http.Server{Handler: sleepHandler(30 * time.Second)}
		app := mustNew(t, lifecycle.WithStopTimeout(5*time.Second))
		app.Add("http", lifecycle.HTTPServer(srv, lifecycle.WithListener(ln)))

		ctx, cancel := context.WithCancel(t.Context())
		done := runAsync(ctx, app)
		slow := getAsync(t, t.Context(), newPipeClient(ln), baseURL+"/")
		synctest.Wait()
		cancel()

		err := <-done
		var ce *lifecycle.ComponentError
		if !errors.Is(err, lifecycle.ErrStopTimeout) || !errors.As(err, &ce) || ce.Name != "http" {
			t.Fatalf("Run = %v, want ErrStopTimeout for http", err)
		}
		if r := <-slow; r.err == nil {
			t.Fatalf("slow request = %+v, want its connection closed", r)
		}
	})
}

func TestHTTPServer_ShutdownHook(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var hooked atomic.Bool
		srv := &http.Server{Handler: http.NotFoundHandler()}
		srv.RegisterOnShutdown(func() { hooked.Store(true) })
		app := mustNew(t)
		app.Add("http", lifecycle.HTTPServer(srv, lifecycle.WithListener(newPipeNet())))

		if err := stopAfterLaunch(t, app); err != nil {
			t.Fatalf("Run = %v, want nil", err)
		}
		synctest.Wait()
		if !hooked.Load() {
			t.Fatal("the shutdown hook did not run")
		}
	})
}

func TestHTTPServer_Listener(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ln := newPipeNet()
		srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = io.WriteString(w, "hello")
		})}
		app := mustNew(t)
		app.Add("http", lifecycle.HTTPServer(srv, lifecycle.WithListener(ln)))

		ctx, cancel := context.WithCancel(t.Context())
		done := runAsync(ctx, app)
		if r := <-getAsync(t, t.Context(), newPipeClient(ln), baseURL+"/"); r.err != nil || r.body != "hello" {
			t.Fatalf("GET = %+v, want hello", r)
		}
		cancel()
		if err := <-done; err != nil {
			t.Fatalf("Run = %v, want nil", err)
		}
		if _, err := ln.Dial(t.Context(), "tcp", "x"); !errors.Is(err, net.ErrClosed) {
			t.Fatalf("Dial after the stop = %v, want net.ErrClosed", err)
		}
	})
}

func TestHTTPServer_StopBeforeServe(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ln := newPipeNet()
		app := mustNew(t)
		app.Add("http", lifecycle.HTTPServer(&http.Server{Handler: http.NotFoundHandler()}, lifecycle.WithListener(ln)))

		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		if err := app.Run(ctx); err != nil {
			t.Fatalf("Run = %v, want nil", err)
		}
		if _, err := ln.Dial(t.Context(), "tcp", "x"); !errors.Is(err, net.ErrClosed) {
			t.Fatalf("Dial after the stop = %v, want net.ErrClosed", err)
		}
	})
}

func TestHTTPServer_TLSChoice(t *testing.T) {
	getCert := func(*tls.ClientHelloInfo) (*tls.Certificate, error) { return nil, errors.New("unused") }
	getConf := func(*tls.ClientHelloInfo) (*tls.Config, error) { return nil, errors.New("unused") }
	tests := []struct {
		name    string
		config  *tls.Config
		wantTLS bool
	}{
		{"no TLS config", nil, false},
		{"no certificate source", &tls.Config{}, false},
		{"certificates", &tls.Config{Certificates: []tls.Certificate{{}}}, true},
		{"GetCertificate", &tls.Config{GetCertificate: getCert}, true},
		{"GetConfigForClient", &tls.Config{GetConfigForClient: getConf}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				ln := newPipeNet()
				srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					_, _ = io.WriteString(w, "plain")
				}), TLSConfig: tt.config}
				app := mustNew(t)
				app.Add("http", lifecycle.HTTPServer(srv, lifecycle.WithListener(ln)))

				ctx, cancel := context.WithCancel(t.Context())
				done := runAsync(ctx, app)
				r := <-getAsync(t, t.Context(), newPipeClient(ln), baseURL+"/")
				cancel()
				if err := <-done; err != nil {
					t.Fatalf("Run = %v, want nil", err)
				}
				if tt.wantTLS {
					if r.status != http.StatusBadRequest || !strings.Contains(r.body, "Client sent an HTTP request to an HTTPS server.") {
						t.Fatalf("plain request to a TLS server = %+v, want the 400 answer", r)
					}
					return
				}
				if r.err != nil || r.status != http.StatusOK || r.body != "plain" {
					t.Fatalf("plain request = %+v, want 200 plain", r)
				}
			})
		})
	}
}

func TestHTTPServer_ListenError(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		log := newEventLog()
		app := mustNew(t)
		app.Add("a", &plainComp{name: "a", log: log})
		// An invalid port fails without opening a socket (research R-11).
		app.Add("http", lifecycle.HTTPServer(&http.Server{Addr: "127.0.0.1:-1"}))

		err := app.Run(t.Context())
		ce := componentError(err, "http")
		if ce == nil || !strings.Contains(ce.Error(), "invalid port") {
			t.Fatalf("Run = %v, want the listen error of http", err)
		}
		if !log.has("a", "canceled") {
			t.Fatal("a was not stopped")
		}
	})
}

func TestHTTPServer_Validation(t *testing.T) {
	tests := []struct {
		name string
		c    lifecycle.Component
	}{
		{"nil server", lifecycle.HTTPServer(nil)},
		{"nil listener", lifecycle.HTTPServer(&http.Server{}, lifecycle.WithListener(nil))},
		{"nil option", lifecycle.HTTPServer(&http.Server{}, nil)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app := mustNew(t)
			app.Add("http", tt.c)
			err := app.Run(t.Context())
			var ce *lifecycle.ComponentError
			if !errors.Is(err, lifecycle.ErrInvalidConfig) || !errors.As(err, &ce) || ce.Name != "http" {
				t.Fatalf("Run = %v, want ErrInvalidConfig for http", err)
			}
		})
	}
}

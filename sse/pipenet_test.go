package sse_test

import (
	"bufio"
	"context"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/paccolamano/go/sse"
)

// pipeNet is an in-memory network: every Dial creates a net.Pipe whose server
// end is returned by Accept. It keeps tests hermetic, and inside a synctest
// bubble its deadlines use virtual time.
type pipeNet struct {
	conns  chan net.Conn
	closed chan struct{}
	once   sync.Once
}

func newPipeNet() *pipeNet {
	return &pipeNet{conns: make(chan net.Conn), closed: make(chan struct{})}
}

func (l *pipeNet) Accept() (net.Conn, error) {
	select {
	case c := <-l.conns:
		return c, nil
	case <-l.closed:
		return nil, net.ErrClosed
	}
}

func (l *pipeNet) Close() error {
	l.once.Do(func() { close(l.closed) })
	return nil
}

func (l *pipeNet) Addr() net.Addr { return &net.TCPAddr{} }

func (l *pipeNet) Dial(ctx context.Context, _, _ string) (net.Conn, error) {
	server, client := net.Pipe()
	select {
	case l.conns <- server:
		return client, nil
	case <-l.closed:
		return nil, net.ErrClosed
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

const baseURL = "http://sse.test"

// env is one HTTP server and its client over a pipeNet. T is the test of the
// synctest bubble the environment belongs to.
type env struct {
	T      *testing.T
	Client *http.Client
	Server *http.Server
	H2     bool

	ln *pipeNet
	tr *http.Transport
}

// e2e runs fn over HTTP/1.1 and over unencrypted HTTP/2, each inside its own
// synctest bubble. Everything fn creates belongs to the bubble.
func e2e(t *testing.T, fn func(e *env)) {
	t.Helper()
	for _, h2 := range []bool{false, true} {
		name := "http1"
		if h2 {
			name = "h2c"
		}
		t.Run(name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) { fn(newEnv(t, h2)) })
		})
	}
}

// newEnv returns an environment that is not started yet. Tests that need
// several servers call it inside the bubble with the protocol of e2e's env.
func newEnv(t *testing.T, h2 bool) *env {
	t.Helper()
	ln := newPipeNet()
	tr := &http.Transport{DialContext: ln.Dial}
	srv := &http.Server{ReadHeaderTimeout: 10 * time.Second}
	if h2 {
		p := new(http.Protocols)
		p.SetUnencryptedHTTP2(true)
		srv.Protocols = p
		tp := new(http.Protocols)
		tp.SetUnencryptedHTTP2(true)
		tr.Protocols = tp
		// A stalled client blocks the server after 64 KiB instead of the
		// default 4 MiB per stream. The connection window keeps its
		// default: every stream of a test shares one connection.
		tr.HTTP2 = &http.HTTP2Config{MaxReceiveBufferPerStream: 64 << 10}
	}
	return &env{T: t, Client: &http.Client{Transport: tr}, Server: srv, H2: h2, ln: ln, tr: tr}
}

// Start serves h with the given server WriteTimeout (0 for none) until the
// test ends.
func (e *env) Start(h http.Handler, writeTimeout time.Duration) {
	e.Server.Handler = h
	e.Server.WriteTimeout = writeTimeout
	go func() { _ = e.Server.Serve(e.ln) }()
	e.T.Cleanup(func() {
		_ = e.Server.Close()
		e.tr.CloseIdleConnections()
		_ = e.ln.Close()
	})
}

// Get sends a GET request for path and returns the response. It fails the test
// when the request fails.
func (e *env) Get(tb testing.TB, ctx context.Context, path string, header http.Header) *http.Response {
	tb.Helper()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+path, nil)
	if err != nil {
		tb.Fatal(err)
	}
	for k, v := range header {
		req.Header[k] = v
	}
	resp, err := e.Client.Do(req)
	if err != nil {
		tb.Fatalf("GET %s: %v", path, err)
	}
	return resp
}

// readEvents decodes body in its own goroutine, as a client over TCP would
// read it. The error channel receives the terminal error, nil at a clean end.
// Both channels are closed when the body ends.
func readEvents(tb testing.TB, body io.ReadCloser) (<-chan sse.Event, <-chan error) {
	tb.Helper()
	events := make(chan sse.Event, 1<<15)
	errc := make(chan error, 1)
	go func() {
		defer func() { _ = body.Close() }()
		var end error
		for ev, err := range sse.Decode(body) {
			if err != nil {
				end = err
				break
			}
			events <- ev
		}
		errc <- end
		close(events)
		close(errc)
	}()
	return events, errc
}

// readLines returns the raw lines of body, without their line feed, read in
// its own goroutine. The channel is closed when the body ends.
func readLines(tb testing.TB, body io.ReadCloser) <-chan string {
	tb.Helper()
	lines := make(chan string, 1<<15)
	go func() {
		defer func() { _ = body.Close() }()
		br := bufio.NewReader(body)
		for {
			line, err := br.ReadString('\n')
			if err != nil {
				break
			}
			lines <- strings.TrimSuffix(line, "\n")
		}
		close(lines)
	}()
	return lines
}

func TestPipeNet_Smoke(t *testing.T) {
	e2e(t, func(e *env) {
		t := e.T
		e.Start(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = io.WriteString(w, "ok")
		}), 0)
		resp := e.Get(t, t.Context(), "/", nil)
		defer func() { _ = resp.Body.Close() }()
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatal(err)
		}
		if string(body) != "ok" || (resp.ProtoMajor == 2) != e.H2 {
			t.Fatalf("body %q over %s", body, resp.Proto)
		}
	})
}

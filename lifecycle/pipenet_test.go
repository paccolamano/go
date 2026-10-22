package lifecycle_test

import (
	"context"
	"io"
	"net"
	"net/http"
	"sync"
	"testing"
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

const baseURL = "http://lifecycle.test"

// newPipeClient returns an HTTP client whose connections go through ln. Each
// request uses its own connection, so that a server's shutdown is not delayed by
// idle keep-alive connections of earlier requests.
func newPipeClient(ln *pipeNet) *http.Client {
	return &http.Client{Transport: &http.Transport{DialContext: ln.Dial, DisableKeepAlives: true}}
}

type response struct {
	status int
	body   string
	err    error
}

// getAsync performs a GET in its own goroutine, because net.Pipe is synchronous,
// and delivers the outcome on the returned channel.
func getAsync(tb testing.TB, ctx context.Context, c *http.Client, url string) <-chan response {
	tb.Helper()
	ch := make(chan response, 1)
	go func() {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			ch <- response{err: err}
			return
		}
		resp, err := c.Do(req)
		if err != nil {
			ch <- response{err: err}
			return
		}
		defer func() { _ = resp.Body.Close() }()
		b, err := io.ReadAll(resp.Body)
		ch <- response{status: resp.StatusCode, body: string(b), err: err}
	}()
	return ch
}

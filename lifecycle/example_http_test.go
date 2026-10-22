package lifecycle_test

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"sync"

	"github.com/paccolamano/go/lifecycle"
)

// memListener is an in-memory listener: every Dial creates a net.Pipe whose
// server end is returned by Accept. It stands for a listener bound to port 0 in a
// test, or one received through socket activation.
type memListener struct {
	conns  chan net.Conn
	closed chan struct{}
	once   sync.Once
}

func (l *memListener) Accept() (net.Conn, error) {
	select {
	case c := <-l.conns:
		return c, nil
	case <-l.closed:
		return nil, net.ErrClosed
	}
}

func (l *memListener) Close() error {
	l.once.Do(func() { close(l.closed) })
	return nil
}

func (l *memListener) Addr() net.Addr { return &net.TCPAddr{} }

func (l *memListener) Dial(ctx context.Context, _, _ string) (net.Conn, error) {
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

// HTTPServer serves an http.Server on its address, or on a listener of your own
// given with WithListener. Its stop is the server's graceful shutdown.
func ExampleHTTPServer() {
	ln := &memListener{conns: make(chan net.Conn), closed: make(chan struct{})}
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "hello")
	})}

	app, err := lifecycle.New()
	if err != nil {
		fmt.Println(err)
		return
	}
	app.Add("http", lifecycle.HTTPServer(srv, lifecycle.WithListener(ln)))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- app.Run(ctx) }()

	client := &http.Client{Transport: &http.Transport{DialContext: ln.Dial, DisableKeepAlives: true}}
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://example.test/", nil)
	if err != nil {
		fmt.Println(err)
		return
	}
	resp, err := client.Do(req)
	if err != nil {
		fmt.Println(err)
		return
	}
	body, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(string(body))

	cancel()
	fmt.Println("run:", <-done)
	// Output:
	// hello
	// run: <nil>
}

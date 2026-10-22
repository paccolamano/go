package lifecycle

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
)

// HTTPOption configures HTTPServer.
type HTTPOption func(*httpConfig)

type httpConfig struct {
	ln    net.Listener
	lnSet bool
}

// WithListener makes the server serve on ln instead of its address: a listener
// bound to port 0 in a test, an in-memory listener, or one received through
// socket activation. The listener is closed when the server stops.
func WithListener(ln net.Listener) HTTPOption {
	return func(c *httpConfig) {
		c.ln = ln
		c.lnSet = true
	}
}

// HTTPServer returns a component that serves srv, on srv.Addr or on the listener
// given with WithListener. It serves TLS when srv.TLSConfig provides a
// certificate, through Certificates, GetCertificate or GetConfigForClient; load
// certificate files into that configuration yourself. Its stop calls
// srv.Shutdown: the server stops accepting connections at once, runs the hooks
// registered with srv.RegisterOnShutdown, and lets the requests in progress
// complete. When the deadline expires, the remaining connections are closed.
func HTTPServer(srv *http.Server, opts ...HTTPOption) Component {
	h := &httpServer{srv: srv}
	for _, opt := range opts {
		if opt == nil {
			h.nilOption = true
			continue
		}
		opt(&h.cfg)
	}
	return h
}

type httpServer struct {
	srv       *http.Server
	cfg       httpConfig
	nilOption bool
}

func (h *httpServer) validate() error {
	switch {
	case h.nilOption:
		return fmt.Errorf("%w: HTTPServer: nil option", ErrInvalidConfig)
	case h.srv == nil:
		return fmt.Errorf("%w: HTTPServer: nil server", ErrInvalidConfig)
	case h.cfg.lnSet && h.cfg.ln == nil:
		return fmt.Errorf("%w: WithListener: nil listener", ErrInvalidConfig)
	}
	return nil
}

// Run serves until the server is shut down. http.ErrServerClosed, which Serve
// returns as soon as Shutdown starts, is not an error.
func (h *httpServer) Run(context.Context) error {
	useTLS := hasCertificate(h.srv.TLSConfig)
	var err error
	switch {
	case h.cfg.ln != nil && useTLS:
		err = h.srv.ServeTLS(h.cfg.ln, "", "")
	case h.cfg.ln != nil:
		err = h.srv.Serve(h.cfg.ln)
	case useTLS:
		err = h.srv.ListenAndServeTLS("", "")
	default:
		err = h.srv.ListenAndServe()
	}
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return fmt.Errorf("lifecycle: http server: %w", err)
}

// Stop shuts the server down gracefully, and closes the remaining connections
// when ctx ends first.
func (h *httpServer) Stop(ctx context.Context) error {
	err := h.srv.Shutdown(ctx)
	if err == nil {
		return nil
	}
	if ctx.Err() != nil {
		err = errors.Join(err, h.srv.Close())
	}
	return fmt.Errorf("lifecycle: http server shutdown: %w", err)
}

// hasCertificate reports whether c provides a certificate, the same rule that
// http.Server.ServeTLS applies when it is given no certificate file.
func hasCertificate(c *tls.Config) bool {
	return c != nil && (len(c.Certificates) > 0 || c.GetCertificate != nil || c.GetConfigForClient != nil)
}

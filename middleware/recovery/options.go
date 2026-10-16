package recovery

import (
	"fmt"
	"net/http"

	"github.com/paccolamano/go/middleware"
)

// Option configures the middleware returned by New.
type Option func(*config)

type config struct {
	reporter    func(r *http.Request, value any, stack []byte)
	reporterSet bool
	reject      middleware.RejectFunc
	rejectSet   bool
}

// WithReporter sets a function called for every recovered panic, after the
// failure is logged, for example to forward it to an error tracker. It
// receives the request, the panic value and the stack trace.
func WithReporter(fn func(r *http.Request, value any, stack []byte)) Option {
	return func(c *config) {
		c.reporter = fn
		c.reporterSet = true
	}
}

// WithRejectFunc replaces the default plain-text 500 response. The function
// receives a Rejection with Status 500 and Err matching ErrPanic.
func WithRejectFunc(fn middleware.RejectFunc) Option {
	return func(c *config) {
		c.reject = fn
		c.rejectSet = true
	}
}

func (c *config) validate() error {
	if c.reporterSet && c.reporter == nil {
		return fmt.Errorf("%w: recovery: WithReporter: the function must not be nil", middleware.ErrInvalidConfig)
	}
	if c.rejectSet && c.reject == nil {
		return fmt.Errorf("%w: recovery: WithRejectFunc: the function must not be nil", middleware.ErrInvalidConfig)
	}
	return nil
}

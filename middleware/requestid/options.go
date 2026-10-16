package requestid

import (
	"crypto/rand"
	"fmt"
	"net/http"

	"github.com/paccolamano/go/middleware"
	"github.com/paccolamano/go/middleware/internal/httpheader"
)

// Option configures the middleware returned by New.
type Option func(*config)

type config struct {
	header    string
	trust     bool
	generator func() string
}

func defaultConfig() config {
	return config{header: "X-Request-ID", trust: true, generator: rand.Text}
}

// WithHeader sets the header that carries the identifier, in requests and
// responses. The default is X-Request-ID.
func WithHeader(name string) Option {
	return func(c *config) { c.header = name }
}

// WithTrustIncoming sets whether a valid identifier received from the client
// is reused. The default is true; with false a new identifier is always
// generated.
func WithTrustIncoming(trust bool) Option {
	return func(c *config) { c.trust = trust }
}

// WithGenerator sets the function that generates new identifiers. The default
// is crypto/rand.Text. When the function returns an invalid identifier,
// crypto/rand.Text is used instead.
func WithGenerator(fn func() string) Option {
	return func(c *config) { c.generator = fn }
}

func (c *config) validate() error {
	if !httpheader.ValidToken(c.header) {
		return fmt.Errorf("%w: requestid: WithHeader: %q is not a valid header name", middleware.ErrInvalidConfig, c.header)
	}
	c.header = http.CanonicalHeaderKey(c.header)
	if c.generator == nil {
		return fmt.Errorf("%w: requestid: WithGenerator: the function must not be nil", middleware.ErrInvalidConfig)
	}
	return nil
}

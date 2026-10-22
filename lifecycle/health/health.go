package health

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"
)

// ErrInvalidConfig is wrapped by every error New reports for an invalid check or
// option.
var ErrInvalidConfig = errors.New("health: invalid configuration")

// defaultTimeout bounds a check without its own Timeout. It is the default
// timeout of Kubernetes probes.
const defaultTimeout = time.Second

// Check is a named operation that tells whether something the process needs is
// available. Func receives a context bounded by Timeout and must return nil when
// the check passes. Timeout zero means 1 second.
//
// Name appears in responses, so it must not contain whitespace or control
// characters. The error of Func never appears in responses: it goes to the
// logger given with WithLogger, so it may describe the failure in detail.
type Check struct {
	Name    string
	Func    func(ctx context.Context) error
	Timeout time.Duration
}

// Option configures a Handler.
type Option func(*config)

type config struct {
	logger    *slog.Logger
	loggerSet bool
}

// WithLogger sets the logger that receives the reasons of failed checks, at
// level Warn. Default: none, the reasons are not recorded.
func WithLogger(l *slog.Logger) Option {
	return func(c *config) {
		c.logger = l
		c.loggerSet = true
	}
}

// check is a validated Check.
type check struct {
	name    string
	fn      func(ctx context.Context) error
	timeout time.Duration
}

// Handler answers probes by running its checks. It answers 200 when every check
// passes and 503 otherwise, with a plain text body that names the failed checks
// but never their reasons. Requests that arrive while the checks run share their
// result, and a check is never started again while it is still running. It is
// safe for concurrent use.
type Handler struct {
	checks []check
	logger *slog.Logger

	mu      sync.Mutex
	current *evaluation // in progress, or nil
	running []bool      // per check: its operation has not returned yet
}

// New returns a Handler that runs checks; responses list them in the order
// given. An invalid check or option is reported as an error wrapping
// ErrInvalidConfig. A Handler without checks answers 200, as a liveness probe
// usually does.
func New(checks []Check, opts ...Option) (*Handler, error) {
	cfg := config{logger: slog.New(slog.DiscardHandler)}
	for _, opt := range opts {
		if opt == nil {
			return nil, fmt.Errorf("%w: nil option", ErrInvalidConfig)
		}
		opt(&cfg)
	}
	if cfg.loggerSet && cfg.logger == nil {
		return nil, fmt.Errorf("%w: WithLogger: nil logger", ErrInvalidConfig)
	}
	h := &Handler{logger: cfg.logger, running: make([]bool, len(checks))}
	names := make(map[string]struct{}, len(checks))
	for i, c := range checks {
		if err := validate(i, c, names); err != nil {
			return nil, err
		}
		names[c.Name] = struct{}{}
		timeout := c.Timeout
		if timeout == 0 {
			timeout = defaultTimeout
		}
		h.checks = append(h.checks, check{name: c.Name, fn: c.Func, timeout: timeout})
	}
	return h, nil
}

func validate(i int, c Check, names map[string]struct{}) error {
	switch {
	case c.Name == "":
		return fmt.Errorf("%w: check at position %d: empty name", ErrInvalidConfig, i+1)
	case !validName(c.Name):
		return fmt.Errorf("%w: check %q: the name must be valid UTF-8 without whitespace or control characters", ErrInvalidConfig, c.Name)
	case c.Func == nil:
		return fmt.Errorf("%w: check %q: nil Func", ErrInvalidConfig, c.Name)
	case c.Timeout < 0:
		return fmt.Errorf("%w: check %q: negative timeout", ErrInvalidConfig, c.Name)
	}
	if _, used := names[c.Name]; used {
		return fmt.Errorf("%w: check %q: name used twice", ErrInvalidConfig, c.Name)
	}
	return nil
}

// validName reports whether name can appear on a line of a response.
func validName(name string) bool {
	if !utf8.ValidString(name) {
		return false
	}
	for _, r := range name {
		if unicode.IsSpace(r) || unicode.IsControl(r) {
			return false
		}
	}
	return true
}

// ServeHTTP runs the checks for GET and HEAD requests, or joins the evaluation
// in progress, and answers when it completes. Any other method gets 405. When the
// request's context ends first, nothing is written.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		writeText(w, r, http.StatusMethodNotAllowed, "method not allowed\n")
		return
	}
	verbose := r.URL.Query().Has("verbose")
	if len(h.checks) == 0 {
		status, body := render(nil, nil, verbose)
		writeText(w, r, status, body)
		return
	}
	e := h.join(r.Context())
	select {
	case <-e.done:
		h.leave(e)
	case <-r.Context().Done():
		h.leave(e)
		return
	}
	status, body := render(h.checks, e.outcomes, verbose)
	writeText(w, r, status, body)
}

package lifecycle

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"time"
)

// ErrInvalidConfig is wrapped by every error reported for an invalid option, an
// invalid registration or an invalid adapter.
var ErrInvalidConfig = errors.New("lifecycle: invalid configuration")

// Defaults (research R-05).
const (
	defaultStopTimeout = 15 * time.Second
	defaultDrainDelay  = 0
)

// Option configures an App.
type Option func(*config)

type config struct {
	signals     []os.Signal
	drainDelay  time.Duration
	stopTimeout time.Duration
	logger      *slog.Logger

	signalsSet bool
	loggerSet  bool
}

// WithSignals makes Run handle sig while it runs: the first signal received
// triggers the stop, the second forces it. Signals named twice count once. By
// default the App handles no signal, so it imposes no process-level behavior.
func WithSignals(sig ...os.Signal) Option {
	return func(c *config) {
		c.signals = append([]os.Signal(nil), sig...)
		c.signalsSet = true
	}
}

// WithDrainDelay sets the time between the trigger and the first stop, during
// which Ready fails and every component keeps working. It must not be negative.
// Default: 0.
func WithDrainDelay(d time.Duration) Option {
	return func(c *config) { c.drainDelay = d }
}

// WithStopTimeout sets the deadline for stopping every component, counted from
// the end of the drain. It must be positive. Default: 15 seconds.
func WithStopTimeout(d time.Duration) Option {
	return func(c *config) { c.stopTimeout = d }
}

// WithLogger sets the logger of the App's lifecycle events. Default: none,
// nothing is logged.
func WithLogger(l *slog.Logger) Option {
	return func(c *config) {
		c.logger = l
		c.loggerSet = true
	}
}

// newConfig applies opts over the defaults and validates the result.
func newConfig(opts []Option) (config, error) {
	c := config{
		drainDelay:  defaultDrainDelay,
		stopTimeout: defaultStopTimeout,
		logger:      slog.New(slog.DiscardHandler),
	}
	for _, opt := range opts {
		if opt == nil {
			return config{}, fmt.Errorf("%w: nil option", ErrInvalidConfig)
		}
		opt(&c)
	}
	if err := c.validate(); err != nil {
		return config{}, err
	}
	c.signals = dedupeSignals(c.signals)
	return c, nil
}

func (c *config) validate() error {
	if c.signalsSet {
		if len(c.signals) == 0 {
			return fmt.Errorf("%w: WithSignals: no signal", ErrInvalidConfig)
		}
		for _, s := range c.signals {
			if s == nil {
				return fmt.Errorf("%w: WithSignals: nil signal", ErrInvalidConfig)
			}
		}
	}
	if c.drainDelay < 0 {
		return fmt.Errorf("%w: WithDrainDelay: must not be negative", ErrInvalidConfig)
	}
	if c.stopTimeout <= 0 {
		return fmt.Errorf("%w: WithStopTimeout: must be positive", ErrInvalidConfig)
	}
	if c.loggerSet && c.logger == nil {
		return fmt.Errorf("%w: WithLogger: nil logger", ErrInvalidConfig)
	}
	return nil
}

// dedupeSignals keeps the first occurrence of each signal, in order.
func dedupeSignals(sigs []os.Signal) []os.Signal {
	out := make([]os.Signal, 0, len(sigs))
	for _, s := range sigs {
		seen := false
		for _, o := range out {
			if o == s {
				seen = true
				break
			}
		}
		if !seen {
			out = append(out, s)
		}
	}
	return out
}

package sse

import (
	"errors"
	"fmt"
	"net/http"
	"time"
)

// ErrInvalidConfig is wrapped by every error that NewStream, NewBroker,
// NewClient and Decode report for an invalid or inapplicable option.
var ErrInvalidConfig = errors.New("sse: invalid configuration")

// Option configures a stream, a broker, a client or Decode. Each one accepts
// the options listed in its documentation; any other option makes it report an
// error wrapping ErrInvalidConfig.
type Option func(*config)

// optionSet records which options were given, for the applicability checks.
type optionSet uint16

const (
	optRetry optionSet = 1 << iota
	optWriteTimeout
	optKeepAlive
	optHistory
	optBuffer
	optResetEvent
	optReplay
	optHTTPClient
	optMaxEventSize
)

// The options accepted by each constructor.
const (
	streamOptions = optRetry | optWriteTimeout
	brokerOptions = optRetry | optWriteTimeout | optKeepAlive | optHistory | optBuffer | optResetEvent | optReplay
	clientOptions = optHTTPClient | optMaxEventSize
	decodeOptions = optMaxEventSize
)

// Defaults (research R-20).
const (
	defaultWriteTimeout = 10 * time.Second
	defaultKeepAlive    = 15 * time.Second
	defaultHistory      = 1000
	defaultBuffer       = 64
	defaultMaxEventSize = 1 << 20
	defaultReconnect    = 3 * time.Second
	maxBackoff          = 30 * time.Second
)

type config struct {
	retry        time.Duration
	writeTimeout time.Duration
	keepAlive    time.Duration
	history      int
	buffer       int
	reset        *Event
	replay       ReplayFunc
	httpClient   *http.Client
	maxEventSize int
	set          optionSet
}

// WithRetry sets the reconnection time sent to clients when a stream opens, as
// the retry field. It must be at least one millisecond and is sent in whole
// milliseconds. By default none is sent.
//
// Accepted by NewStream and NewBroker.
func WithRetry(d time.Duration) Option {
	return func(c *config) {
		c.retry = d
		c.set |= optRetry
	}
}

// WithWriteTimeout sets the deadline of each write to a stream. A client that
// does not read a write within it is dropped. Idle periods between writes are
// not limited, and the server's WriteTimeout does not apply to streams. The
// default is 10 seconds.
//
// Accepted by NewStream and NewBroker.
func WithWriteTimeout(d time.Duration) Option {
	return func(c *config) {
		c.writeTimeout = d
		c.set |= optWriteTimeout
	}
}

// WithKeepAlive sets how long a subscription may stay silent before the broker
// writes a comment, which keeps proxies from closing idle connections. The
// default is 15 seconds.
//
// Accepted by NewBroker.
func WithKeepAlive(d time.Duration) Option {
	return func(c *config) {
		c.keepAlive = d
		c.set |= optKeepAlive
	}
}

// WithHistory sets how many recent events the broker keeps in memory to resume
// clients that reconnect with a Last-Event-ID. The default is 1000.
//
// Accepted by NewBroker.
func WithHistory(n int) Option {
	return func(c *config) {
		c.history = n
		c.set |= optHistory
	}
}

// WithBuffer sets how many events may wait to be written to one subscriber. A
// subscriber whose queue overflows is disconnected and resumes from the
// history when it reconnects. The default is 64.
//
// Accepted by NewBroker.
func WithBuffer(n int) Option {
	return func(c *config) {
		c.buffer = n
		c.set |= optBuffer
	}
}

// WithResetEvent sets the event sent to a client whose Last-Event-ID can no
// longer be resolved, so that it reloads its state instead of silently missing
// events. The broker sets the event's identifier itself, so ev.ID is ignored.
// By default no reset event is sent.
//
// Accepted by NewBroker.
func WithResetEvent(ev Event) Option {
	return func(c *config) {
		c.reset = &ev
		c.set |= optResetEvent
	}
}

// WithReplay sets the function that provides missed events from a durable
// source when a Last-Event-ID is not in the broker's history.
//
// Accepted by NewBroker.
func WithReplay(fn ReplayFunc) Option {
	return func(c *config) {
		c.replay = fn
		c.set |= optReplay
	}
}

// WithHTTPClient sets the HTTP client used to connect. Its Timeout must be zero,
// because it would end every stream; use transport-level timeouts instead. The
// default is http.DefaultClient.
//
// Accepted by NewClient.
func WithHTTPClient(hc *http.Client) Option {
	return func(c *config) {
		c.httpClient = hc
		c.set |= optHTTPClient
	}
}

// WithMaxEventSize sets the maximum size in bytes of one decoded event, and of
// one line of the stream. A larger event ends decoding with ErrEventTooLarge.
// The default is 1 MiB.
//
// Accepted by NewClient and Decode.
func WithMaxEventSize(n int) Option {
	return func(c *config) {
		c.maxEventSize = n
		c.set |= optMaxEventSize
	}
}

// optionNames lists every option in the order the checks report them.
func optionNames() []struct {
	opt  optionSet
	name string
} {
	return []struct {
		opt  optionSet
		name string
	}{
		{optRetry, "WithRetry"},
		{optWriteTimeout, "WithWriteTimeout"},
		{optKeepAlive, "WithKeepAlive"},
		{optHistory, "WithHistory"},
		{optBuffer, "WithBuffer"},
		{optResetEvent, "WithResetEvent"},
		{optReplay, "WithReplay"},
		{optHTTPClient, "WithHTTPClient"},
		{optMaxEventSize, "WithMaxEventSize"},
	}
}

// newConfig applies opts over the defaults, then checks that every option is
// accepted by ctor and valid.
func newConfig(ctor string, accepted optionSet, opts []Option) (config, error) {
	c := config{
		writeTimeout: defaultWriteTimeout,
		keepAlive:    defaultKeepAlive,
		history:      defaultHistory,
		buffer:       defaultBuffer,
		httpClient:   http.DefaultClient,
		maxEventSize: defaultMaxEventSize,
	}
	for _, opt := range opts {
		if opt == nil {
			return config{}, fmt.Errorf("%w: %s: nil option", ErrInvalidConfig, ctor)
		}
		opt(&c)
	}
	for _, o := range optionNames() {
		if c.set&o.opt != 0 && accepted&o.opt == 0 {
			return config{}, fmt.Errorf("%w: %s: %s: not accepted by %s", ErrInvalidConfig, ctor, o.name, ctor)
		}
	}
	for _, o := range optionNames() {
		if c.set&o.opt == 0 {
			continue
		}
		if reason := c.check(o.opt); reason != "" {
			return config{}, fmt.Errorf("%w: %s: %s: %s", ErrInvalidConfig, ctor, o.name, reason)
		}
	}
	return c, nil
}

// check returns why the value set by opt is invalid, or "" when it is valid.
func (c *config) check(opt optionSet) string {
	switch opt {
	case optRetry:
		if c.retry < time.Millisecond {
			return "must be at least 1ms"
		}
	case optWriteTimeout:
		if c.writeTimeout <= 0 {
			return "must be greater than 0"
		}
	case optKeepAlive:
		if c.keepAlive <= 0 {
			return "must be greater than 0"
		}
	case optHistory:
		if c.history < 1 {
			return "must be at least 1"
		}
	case optBuffer:
		if c.buffer < 1 {
			return "must be at least 1"
		}
	case optResetEvent:
		ev := *c.reset
		ev.ID = ""
		if err := ev.validate(); err != nil {
			return err.Error()
		}
	case optReplay:
		if c.replay == nil {
			return "function must not be nil"
		}
	case optHTTPClient:
		if c.httpClient == nil {
			return "client must not be nil"
		}
	case optMaxEventSize:
		if c.maxEventSize < 1 {
			return "must be at least 1"
		}
	}
	return ""
}

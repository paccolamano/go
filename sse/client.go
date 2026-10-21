package sse

import (
	"context"
	"errors"
	"fmt"
	"io"
	"iter"
	"mime"
	"net/http"
	"time"
)

var errBodyNotReplayable = errors.New("sse: the request body cannot be sent again: set Request.GetBody")

// ResponseError reports an answer that does not start an event stream: a
// status other than 200 and 204, or status 200 with another content type. The
// client does not reconnect after it; a caller that wants to retry, for
// example on 503, starts a new iteration.
type ResponseError struct {
	// StatusCode is the status of the answer.
	StatusCode int
	// ContentType is its Content-Type header.
	ContentType string
}

func (e *ResponseError) Error() string {
	return fmt.Sprintf("sse: unexpected response: status %d, content type %q", e.StatusCode, e.ContentType)
}

// Client connects to event streams and reconnects as browsers do. It is safe
// for concurrent use.
type Client struct {
	cfg config
}

// NewClient returns a client.
//
// Accepted options: WithHTTPClient and WithMaxEventSize.
func NewClient(opts ...Option) (*Client, error) {
	cfg, err := newConfig("NewClient", clientOptions, opts)
	if err != nil {
		return nil, err
	}
	return &Client{cfg: cfg}, nil
}

// Events connects with req, keeping its method, headers and body, and yields
// the events of the stream.
//
// When the connection fails or the server ends the stream, it reconnects after
// the reconnection time (the server's retry field, 3 seconds by default) with
// a Last-Event-ID header carrying the last identifier received. A
// Last-Event-ID already set on req is sent on the first connection.
// Consecutive failed attempts double the delay, up to 30 seconds or the
// server's retry if larger.
//
// The iteration ends without an error on status 204, with a *ResponseError on
// another status or content type, with ErrEventTooLarge, and with the error of
// req's context when it ends. Stopping the iteration closes the connection. A
// request body without GetBody, which could not be sent again, is an error.
func (c *Client) Events(req *http.Request) iter.Seq2[Event, error] {
	return func(yield func(Event, error) bool) {
		if req.Body != nil && req.Body != http.NoBody && req.GetBody == nil {
			yield(Event{}, errBodyNotReplayable)
			return
		}
		ctx := req.Context()
		lastID := req.Header.Get("Last-Event-ID")
		reconnect, backoff := defaultReconnect, defaultReconnect
		for {
			resp, err := c.connect(req, lastID)
			switch {
			case ctx.Err() != nil:
				if resp != nil {
					_ = resp.Body.Close()
				}
				yield(Event{}, fmt.Errorf("sse: %w", ctx.Err()))
				return
			case err != nil:
				if errors.Is(err, errBodyNotReplayable) {
					yield(Event{}, err)
					return
				}
				if !sleep(ctx, backoff) {
					yield(Event{}, fmt.Errorf("sse: %w", ctx.Err()))
					return
				}
				backoff = min(backoff*2, max(maxBackoff, reconnect))
				continue
			}

			if err := checkResponse(resp); err != nil {
				_ = resp.Body.Close()
				if resp.StatusCode != http.StatusNoContent {
					yield(Event{}, err)
				}
				return
			}
			end, stop := c.stream(resp.Body, &lastID, &reconnect, yield)
			_ = resp.Body.Close()
			switch {
			case stop:
				return
			case ctx.Err() != nil:
				yield(Event{}, fmt.Errorf("sse: %w", ctx.Err()))
				return
			case end != nil:
				yield(Event{}, end)
				return
			}
			backoff = reconnect
			if !sleep(ctx, reconnect) {
				yield(Event{}, fmt.Errorf("sse: %w", ctx.Err()))
				return
			}
		}
	}
}

// connect sends one attempt of req.
func (c *Client) connect(req *http.Request, lastID string) (*http.Response, error) {
	r := req.Clone(req.Context())
	if req.GetBody != nil {
		body, err := req.GetBody()
		if err != nil {
			return nil, fmt.Errorf("%w: %w", errBodyNotReplayable, err)
		}
		r.Body = body
	}
	if r.Header.Get("Accept") == "" {
		r.Header.Set("Accept", "text/event-stream")
	}
	if lastID != "" {
		r.Header.Set("Last-Event-ID", lastID)
	}
	resp, err := c.cfg.httpClient.Do(r)
	if err != nil {
		return nil, fmt.Errorf("sse: connect: %w", err)
	}
	return resp, nil
}

// errNoContent ends an iteration without an error: the server asked the client
// to stop reconnecting.
var errNoContent = errors.New("sse: 204 No Content")

// checkResponse accepts status 200 with an event stream.
func checkResponse(resp *http.Response) error {
	if resp.StatusCode == http.StatusNoContent {
		return errNoContent
	}
	ct := resp.Header.Get("Content-Type")
	mt, _, err := mime.ParseMediaType(ct)
	if resp.StatusCode != http.StatusOK || err != nil || mt != "text/event-stream" {
		return &ResponseError{StatusCode: resp.StatusCode, ContentType: ct}
	}
	return nil
}

// stream yields the events of one connection, keeping the last identifier and
// the reconnection time up to date. It reports a terminal error, or stop when
// the caller stopped iterating. A read error or a normal end returns neither,
// so that the client reconnects.
func (c *Client) stream(body io.Reader, lastID *string, reconnect *time.Duration, yield func(Event, error) bool) (end error, stop bool) {
	p := newParser(body, c.cfg.maxEventSize)
	// Browsers carry the last event identifier over to the new connection.
	p.idBuf, p.lastID = *lastID, *lastID
	defer func() {
		*lastID = p.lastID
		if p.retrySet {
			*reconnect = p.retry
		}
	}()
	for {
		ev, err := p.next()
		if errors.Is(err, ErrEventTooLarge) {
			return err, false
		}
		if err != nil {
			return nil, false
		}
		if !yield(ev, nil) {
			return nil, true
		}
	}
}

// sleep waits for d, and reports false when ctx ends first.
func sleep(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

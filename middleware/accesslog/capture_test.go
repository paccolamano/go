package accesslog_test

import (
	"context"
	"log/slog"
	"sync"
)

// capture is a slog.Handler that keeps every record for inspection.
type capture struct {
	mu       sync.Mutex
	records  []slog.Record
	minLevel slog.Level
}

func (c *capture) Enabled(_ context.Context, l slog.Level) bool { return l >= c.minLevel }

func (c *capture) Handle(_ context.Context, r slog.Record) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.records = append(c.records, r.Clone())
	return nil
}

func (c *capture) WithAttrs([]slog.Attr) slog.Handler { return c }
func (c *capture) WithGroup(string) slog.Handler      { return c }

func (c *capture) all() []slog.Record {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]slog.Record(nil), c.records...)
}

// attrs flattens the top-level attributes of r into a map.
func attrs(r slog.Record) map[string]slog.Value {
	m := map[string]slog.Value{}
	r.Attrs(func(a slog.Attr) bool {
		m[a.Key] = a.Value
		return true
	})
	return m
}

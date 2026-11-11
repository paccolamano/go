package slogx

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
)

// Fanout returns a handler that passes every record to every output, in order,
// without asking the outputs whether they handle its level: every output
// receives the same records, whatever its own level. Give it to New as the next
// handler, with WithDefaultLevel, which New then requires: the level is decided
// once, by the Handler. New does not recognize a Fanout wrapped in another
// handler.
//
// Each output receives its own copy of the record. An output that fails does
// not stop the next ones: Handle returns the error of a single failing output
// unchanged, and combines several with errors.Join.
//
// Used on its own, a Fanout reports every level as enabled. For deliberately
// different levels per output, use slog.NewMultiHandler instead: each output
// applies its own level, and the per-request level does not go below it.
func Fanout(outputs ...slog.Handler) slog.Handler {
	f := &fanout{outputs: slices.Clone(outputs)}
	if len(outputs) == 0 {
		f.invalid = "no output"
	}
	for i, o := range outputs {
		if o == nil {
			f.invalid = fmt.Sprintf("outputs[%d]: must not be nil", i)
			break
		}
		if inner, ok := o.(*fanout); ok && inner.invalid != "" {
			f.invalid = fmt.Sprintf("outputs[%d]: %s", i, inner.invalid)
			break
		}
	}
	return f
}

// fanout is the handler that Fanout returns.
type fanout struct {
	outputs []slog.Handler
	invalid string // why New refuses the fanout, or "" when it is valid
}

// Enabled reports every level as enabled: the Handler built over the fanout
// decides the level.
func (*fanout) Enabled(context.Context, slog.Level) bool { return true }

// Handle passes a copy of r to every output, in order, without asking them
// whether they handle its level. It returns the error of a single failing
// output unchanged, and joins the errors of several.
func (f *fanout) Handle(ctx context.Context, r slog.Record) error {
	var errs []error
	for _, o := range f.outputs {
		if o == nil { // only in an invalid fanout used on its own
			continue
		}
		if err := handle(ctx, o, r.Clone()); err != nil {
			errs = append(errs, err)
		}
	}
	if len(errs) == 1 {
		return errs[0]
	}
	return errors.Join(errs...)
}

// WithAttrs returns a fanout whose outputs have the attributes.
func (f *fanout) WithAttrs(attrs []slog.Attr) slog.Handler {
	if len(attrs) == 0 {
		return f
	}
	return f.each(func(h slog.Handler) slog.Handler { return h.WithAttrs(attrs) })
}

// WithGroup returns a fanout whose outputs have the group open.
func (f *fanout) WithGroup(name string) slog.Handler {
	if name == "" {
		return f
	}
	return f.each(func(h slog.Handler) slog.Handler { return h.WithGroup(name) })
}

// each returns a fanout whose outputs are those of f passed through fn, and
// which keeps the reason why f is invalid.
func (f *fanout) each(fn func(slog.Handler) slog.Handler) *fanout {
	out := make([]slog.Handler, len(f.outputs))
	for i, o := range f.outputs {
		if o != nil {
			out[i] = fn(o)
		}
	}
	return &fanout{outputs: out, invalid: f.invalid}
}

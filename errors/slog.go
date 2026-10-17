package errors

import (
	"log/slog"
)

// defaultAttrKey is the key of the attribute returned by Attr.
const defaultAttrKey = "error"

// Attr returns a log attribute with the key "error" that holds the message and
// the detail of err:
//
//	logger.Error("request failed", errors.Attr(err))
//
// The value is a group with the attributes "message" (err.Error()) and
// "detail" (Describe(err)). It is computed only when a handler processes the
// record, so a disabled level costs nothing beyond the attribute itself.
// Attr(nil) adds nothing to the record. Use DetailValue to choose another key.
//
// Errors of this package logged as plain values, without Attr, appear exactly
// like standard library errors with the same message.
func Attr(err error) slog.Attr {
	return slog.Attr{Key: defaultAttrKey, Value: DetailValue(err)}
}

// DetailValue returns the value of Attr without a key, so the caller can choose
// it:
//
//	logger.Error("request failed", slog.Any("err", errors.DetailValue(err)))
func DetailValue(err error) slog.Value {
	if err == nil {
		return slog.GroupValue()
	}
	return slog.AnyValue(detailValuer{err})
}

// detailValuer resolves to the message and the detail of err when a handler
// processes the record.
type detailValuer struct{ err error }

func (v detailValuer) LogValue() slog.Value {
	return slog.GroupValue(
		slog.String("message", messageOf(v.err)),
		slog.Any("detail", Describe(v.err)),
	)
}

// plainError is how the errors of this package present themselves to slog: it
// has no Format method, so handlers that print values with %+v print only the
// message, and its Unwrap method keeps Is and As working on the resolved value.
type plainError struct{ err error }

func (p plainError) Error() string { return p.err.Error() }
func (p plainError) Unwrap() error { return p.err }

// LogValue makes the error appear in log records exactly like a standard
// library error with the same message.
func (e *wrapError) LogValue() slog.Value { return slog.AnyValue(plainError{e}) }

// LogValue makes the error appear in log records exactly like a standard
// library error with the same message.
func (e *joinError) LogValue() slog.Value { return slog.AnyValue(plainError{e}) }

// LogValue makes the custom error that embeds b appear in log records exactly
// like a standard library error with the same message.
func (b *BaseError) LogValue() slog.Value { return slog.AnyValue(plainError{b.outer()}) }

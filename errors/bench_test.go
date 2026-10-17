package errors_test

import (
	stderrors "errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"testing"

	"github.com/paccolamano/go/errors"
)

var (
	sink       error
	sinkDetail errors.Detail
)

// depth calls f at a call depth of n, so that capturing call stacks costs what
// it costs in real programs.
func depth(n int, f func() error) error {
	if n == 0 {
		return f()
	}
	return depth(n-1, f)
}

func BenchmarkStdWrap(b *testing.B) {
	base := stderrors.New("x")
	for b.Loop() {
		sink = depth(10, func() error { return fmt.Errorf("ctx: %w", base) })
	}
}

func BenchmarkWrap(b *testing.B) {
	base := errors.New("x")
	for b.Loop() {
		sink = depth(10, func() error { return errors.Wrap(base, "ctx") })
	}
}

func BenchmarkWrapBehindForeign(b *testing.B) {
	base := fmt.Errorf("q: %w", errors.New("x"))
	for b.Loop() {
		sink = depth(10, func() error { return errors.Wrap(base, "ctx") })
	}
}

func BenchmarkNew(b *testing.B) {
	for b.Loop() {
		sink = depth(10, func() error { return errors.New("x") })
	}
}

func BenchmarkWrapForeign(b *testing.B) {
	for b.Loop() {
		sink = depth(10, func() error { return errors.Wrap(fs.ErrNotExist, "ctx") })
	}
}

func BenchmarkErrorf(b *testing.B) {
	for b.Loop() {
		sink = depth(10, func() error { return errors.Errorf("ctx %d: %w", 3, fs.ErrNotExist) })
	}
}

func BenchmarkDescribe(b *testing.B) {
	err := error(fs.ErrNotExist)
	for _, text := range []string{"open", "read", "load", "handle"} {
		err = errors.Wrap(err, text)
	}
	for b.Loop() {
		sinkDetail = errors.Describe(err)
	}
}

func BenchmarkDescribePathological(b *testing.B) {
	err := linkedChain(10000)
	for b.Loop() {
		sinkDetail = errors.Describe(err)
	}
}

// BenchmarkAttrDisabled measures Attr on a disabled level. It uses LogAttrs:
// with Info's ...any arguments, slog itself allocates to box every attribute,
// with or without this package.
func BenchmarkAttrDisabled(b *testing.B) {
	logger := slog.New(slog.NewJSONHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelWarn}))
	err := errors.New("x")
	ctx := b.Context()
	for b.Loop() {
		logger.LogAttrs(ctx, slog.LevelInfo, "m", errors.Attr(err))
	}
}

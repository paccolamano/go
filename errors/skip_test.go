package errors_test

import (
	stderrors "errors"
	"io/fs"
	"testing"

	"github.com/paccolamano/go/errors"
)

func helperNew() error              { return errors.Skip(1).New("x") }
func helperErrorf() error           { return errors.Skip(1).Errorf("x: %w", fs.ErrNotExist) }
func helperWrap(err error) error    { return errors.Skip(1).Wrap(err, "x") }
func helperWrapf(err error) error   { return errors.Skip(1).Wrapf(err, "x %d", 1) }
func helperWithStack(e error) error { return errors.Skip(1).WithStack(e) }
func helperJoin(errs ...error) error {
	return errors.Skip(1).Join(errs...)
}

// checkLocation checks that the outermost level of err is located at line of
// the function fn, and that its call stack, if any, starts there.
func checkLocation(t *testing.T, err error, fn string, line int) {
	t.Helper()
	l := errors.Describe(err).Levels[0]
	if l.Location == nil || l.Location.Function != testPkg+"."+fn || l.Location.Line != line {
		t.Errorf("location = %+v, want %s line %d", l.Location, fn, line)
		return
	}
	if l.Stack != nil && l.Stack.Frames[0] != *l.Location {
		t.Errorf("call stack starts at %+v, want %+v", l.Stack.Frames[0], *l.Location)
	}
}

func TestSkip_HelperAttribution(t *testing.T) {
	const fn = "TestSkip_HelperAttribution"
	err, line := helperNew(), callerLine(t)
	checkLocation(t, err, fn, line)
	err, line = helperErrorf(), callerLine(t)
	checkLocation(t, err, fn, line)
	err, line = helperWrap(fs.ErrNotExist), callerLine(t)
	checkLocation(t, err, fn, line)
	err, line = helperWrapf(fs.ErrNotExist), callerLine(t)
	checkLocation(t, err, fn, line)
	err, line = helperWithStack(fs.ErrNotExist), callerLine(t)
	checkLocation(t, err, fn, line)
	err, line = helperJoin(stderrors.New("a"), stderrors.New("b")), callerLine(t)
	checkLocation(t, err, fn, line)
}

func TestSkip_ZeroIsPackageLevel(t *testing.T) {
	const fn = "TestSkip_ZeroIsPackageLevel"
	err, line := errors.Skip(0).Wrap(fs.ErrNotExist, "x"), callerLine(t)
	checkLocation(t, err, fn, line)
	err, line = errors.Wrap(fs.ErrNotExist, "x"), callerLine(t)
	checkLocation(t, err, fn, line)
}

func TestSkip_Negative(t *testing.T) {
	err, line := errors.Skip(-3).New("x"), callerLine(t)
	checkLocation(t, err, "TestSkip_Negative", line)
}

func innerHelper() error { return errors.Skip(2).New("x") }
func outerHelper() error { return innerHelper() }

func TestSkip_TwoLevels(t *testing.T) {
	err, line := outerHelper(), callerLine(t)
	checkLocation(t, err, "TestSkip_TwoLevels", line)
}

func TestSkip_KeepsSemantics(t *testing.T) {
	cause := stderrors.New("cause")
	if errors.Unwrap(errors.Skip(1).Errorf("x: %w", cause)) != cause { //nolint:errorlint // identity check (FR-007)
		t.Error("Skip(1).Errorf lost the cause")
	}
	nils := map[string]error{
		"Wrap":      errors.Skip(1).Wrap(nil, "x"),
		"Wrapf":     errors.Skip(1).Wrapf(nil, "x %d", 1),
		"WithStack": errors.Skip(1).WithStack(nil),
		"Join":      errors.Skip(1).Join(nil),
	}
	for name, err := range nils {
		if err != nil {
			t.Errorf("Skip(1).%s(nil) = %v, want nil", name, err)
		}
	}
}

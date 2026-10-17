package errors_test

import (
	stderrors "errors"
	"fmt"
	"testing"

	"github.com/paccolamano/go/errors"
)

func newSentinel(text string) error { return errors.New(text) }

// Package-level declarations run while packages are being initialized.
var (
	errSentinel        = errors.New("sentinel")
	errFromHelper      = newSentinel("helper sentinel")
	errWrappedAtInit   = errors.Wrap(errSentinel, "wrapped at init")
	errAnnotatedAtInit = errors.WithStack(errSentinel)
	errJoinedAtInit    = errors.Join(errSentinel, errFromHelper)
)

func allLevels(levels []errors.Level) []errors.Level {
	var out []errors.Level
	for _, l := range levels {
		out = append(out, l)
		for _, b := range l.Branches {
			out = append(out, allLevels(b)...)
		}
	}
	return out
}

func TestSentinel_NoLocation(t *testing.T) {
	for name, err := range map[string]error{
		"direct":    errSentinel,
		"helper":    errFromHelper,
		"wrapped":   errWrappedAtInit,
		"annotated": errAnnotatedAtInit,
		"joined":    errJoinedAtInit,
	} {
		for _, l := range allLevels(errors.Describe(err).Levels) {
			if l.Location != nil || l.Stack != nil {
				t.Errorf("%s: level %q has a location or a call stack", name, l.Text)
			}
		}
	}
	if got := texts(errors.Describe(errAnnotatedAtInit).Levels); len(got) != 1 || got[0] != "sentinel" {
		t.Errorf("annotation at init = %q, want only the sentinel", got)
	}
}

func TestSentinel_RootOfChain(t *testing.T) {
	levels := errors.Describe(errors.Wrap(errSentinel, "get user")).Levels
	if len(levels) != 2 || levels[0].Location == nil || levels[0].Stack == nil {
		t.Fatalf("levels = %+v, want the first wrap at run time to be the origin", levels)
	}
	if levels[1].Text != "sentinel" || levels[1].Location != nil {
		t.Errorf("sentinel level = %+v", levels[1])
	}
}

func TestSentinel_Identity(t *testing.T) {
	if !errors.Is(errors.Wrap(errors.Wrap(errSentinel, "a"), "b"), errSentinel) {
		t.Error("the sentinel is not found through two wraps")
	}
}

func stacks(levels []errors.Level) int {
	n := 0
	for _, l := range allLevels(levels) {
		if l.Stack != nil {
			n++
		}
	}
	return n
}

func TestOrigin_NoSecondStack(t *testing.T) {
	foreign := stderrors.New("foreign")
	traced := errors.New("traced")
	for name, err := range map[string]error{
		"wrap chain":          errors.Wrap(errors.Wrap(foreign, "a"), "b"),
		"behind a fmt level":  errors.Wrap(fmt.Errorf("q: %w", traced), "c"),
		"errorf over traced":  errors.Errorf("d: %w", traced),
		"withstack of traced": errors.WithStack(traced),
	} {
		if n := stacks(errors.Describe(err).Levels); n != 1 {
			t.Errorf("%s: %d call stacks, want 1", name, n)
		}
	}
	join := errors.Describe(errors.Join(errors.New("t1"), errors.New("t2"))).Levels[0]
	if join.Stack != nil {
		t.Error("a join of traced errors recorded its own call stack")
	}
}

func TestInit_RuntimeErrorsKeepLocation(t *testing.T) {
	l := errors.Describe(errors.New("at run time")).Levels[0]
	if l.Location == nil || l.Stack == nil {
		t.Errorf("an error created at run time has no location: %+v", l)
	}
}

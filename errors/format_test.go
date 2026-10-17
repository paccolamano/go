package errors_test

import (
	stderrors "errors"
	"fmt"
	"testing"

	"github.com/paccolamano/go/errors"
)

func formatSubjects() map[string]error {
	a, b := stderrors.New("a"), stderrors.New("b")
	return map[string]error{
		"wrap":   errors.Wrap(errors.Wrap(a, "inner"), "outer"),
		"join":   errors.Join(a, b),
		"errorf": errors.Errorf("%w and %w", a, b),
	}
}

func TestFormat_StringVerbsParity(t *testing.T) {
	directives := []string{"%v", "%s", "%q", "%x", "%X", "%10s", "%-12v|", "%.3s", "%#q", "%+q"}
	for name, err := range formatSubjects() {
		std := stderrors.New(err.Error())
		for _, d := range directives {
			if got, want := fmt.Sprintf(d, err), fmt.Sprintf(d, std); got != want {
				t.Errorf("%s %s: got %q, want %q", name, d, got, want)
			}
		}
	}
}

func TestFormat_DetailVerb(t *testing.T) {
	for name, err := range formatSubjects() {
		if got, want := fmt.Sprintf("%+v", err), errors.Describe(err).String(); got != want {
			t.Errorf("%s: %%+v =\n%s\nwant\n%s", name, got, want)
		}
	}
}

func TestFormat_OtherVerbs(t *testing.T) {
	for name, err := range formatSubjects() {
		for _, d := range []string{"%d", "%#v"} {
			if got, want := fmt.Sprintf(d, err), fmt.Sprintf(d, err.Error()); got != want {
				t.Errorf("%s %s: got %q, want %q", name, d, got, want)
			}
		}
	}
}

func TestFormat_WrapDirective(t *testing.T) {
	lib := errors.Wrap(stderrors.New("root"), "lib")
	if got := fmt.Errorf("x: %w", lib).Error(); got != "x: lib: root" {
		t.Errorf("fmt.Errorf with a library operand = %q", got)
	}
}

func TestFormat_NilError(t *testing.T) {
	if got := fmt.Sprintf("%+v", error(nil)); got != "<nil>" {
		t.Errorf("%%+v of nil = %q", got)
	}
}

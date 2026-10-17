package errors_test

import (
	stderrors "errors"
	"fmt"
	"io/fs"
	"testing"

	"github.com/paccolamano/go/errors"
)

func TestWrap_ParityWithStd(t *testing.T) {
	const want = "handle request: load tenant 42: open config: file does not exist"
	base := fs.ErrNotExist
	std := fmt.Errorf("handle request: %w", fmt.Errorf("load tenant 42: %w", fmt.Errorf("open config: %w", base)))
	tests := []struct {
		name string
		ours error
	}{
		{"Wrap", errors.Wrap(errors.Wrap(errors.Wrap(base, "open config"), "load tenant 42"), "handle request")},
		{"Wrapf", errors.Wrapf(errors.Wrapf(errors.Wrap(base, "open config"), "load tenant %d", 42), "handle %s", "request")},
		{"Errorf", errors.Errorf("handle request: %w", errors.Errorf("load tenant %d: %w", 42, errors.Errorf("open config: %w", base)))},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.ours.Error(); got != std.Error() || got != want {
				t.Errorf("message = %q, want %q", got, want)
			}
			if !errors.Is(tt.ours, base) || !stderrors.Is(tt.ours, base) {
				t.Error("the original cause is not found")
			}
		})
	}
}

func TestWrap_Nil(t *testing.T) {
	tests := map[string]error{
		"Wrap":      errors.Wrap(nil, "x"),
		"Wrapf":     errors.Wrapf(nil, "x %d", 1),
		"WithStack": errors.WithStack(nil),
		"Join()":    errors.Join(),
		"Join(nil)": errors.Join(nil, nil),
	}
	for name, err := range tests {
		if err != nil {
			t.Errorf("%s returned %#v, want nil", name, err)
		}
	}
}

func TestWrap_EmptyContext(t *testing.T) {
	cause := stderrors.New("cause")
	if got := errors.Wrap(cause, "").Error(); got != "cause" {
		t.Errorf("Wrap(err, \"\") = %q, want %q", got, "cause")
	}
}

func TestWithStack_MessageUnchanged(t *testing.T) {
	cause := fmt.Errorf("q: %w", fs.ErrNotExist)
	err := errors.WithStack(cause)
	if err.Error() != cause.Error() {
		t.Errorf("WithStack changed the message: %q", err.Error())
	}
	if !errors.Is(err, fs.ErrNotExist) {
		t.Error("WithStack lost the cause")
	}
}

func TestWrap_CauseIdentity(t *testing.T) {
	cause := stderrors.New("cause")
	for name, err := range map[string]error{
		"Wrap":      errors.Wrap(cause, "x"),
		"Wrapf":     errors.Wrapf(cause, "x %d", 1),
		"WithStack": errors.WithStack(cause),
		"Errorf":    errors.Errorf("x: %w", cause),
	} {
		if errors.Unwrap(err) != cause { //nolint:errorlint // identity check (FR-014)
			t.Errorf("%s: Unwrap does not return the original cause", name)
		}
	}
}

func TestErrorf_Parity(t *testing.T) {
	e1, e2 := stderrors.New("e1"), stderrors.New("e2")
	tests := []struct {
		name   string
		format string
		args   []any
		refs   []error
	}{
		{"no wrap", "plain %d", []any{1}, nil},
		{"one", "x: %w", []any{e1}, []error{e1}},
		{"two", "%w and %w", []any{e1, e2}, []error{e1, e2}},
		{"nil operand", "x: %w", []any{nil}, nil},
		{"repeated operand", "%w %w", []any{e1, e1}, []error{e1}},
		{"not at the end", "%w happened later", []any{e1}, []error{e1}},
		{"all operands nil", "%w %w", []any{nil, nil}, nil},
		{"one of two nil", "%w %w", []any{e1, nil}, []error{e1}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := errors.Errorf(tt.format, tt.args...)
			want := fmt.Errorf(tt.format, tt.args...)
			if got.Error() != want.Error() {
				t.Errorf("message = %q, want %q", got.Error(), want.Error())
			}
			for _, ref := range tt.refs {
				if !errors.Is(got, ref) {
					t.Errorf("referenced error %q not found", ref)
				}
			}
			if errors.Unwrap(got) != stderrors.Unwrap(want) { //nolint:errorlint // identity check (FR-007)
				t.Errorf("Unwrap = %v, standard library returns %v", errors.Unwrap(got), stderrors.Unwrap(want))
			}
			if m, ok := want.(interface{ Unwrap() []error }); ok && len(m.Unwrap()) > 0 {
				gm, ok := got.(interface{ Unwrap() []error })
				if !ok || len(gm.Unwrap()) != len(m.Unwrap()) {
					t.Fatalf("causes differ from the standard library: %v", want)
				}
				for i, c := range m.Unwrap() {
					if gm.Unwrap()[i] != c { //nolint:errorlint // identity check (FR-007)
						t.Errorf("cause %d = %v, want %v", i, gm.Unwrap()[i], c)
					}
				}
			}
		})
	}
}

func TestWrap_ThroughStdLevel(t *testing.T) {
	base := stderrors.New("base")
	pathErr := &fs.PathError{Op: "open", Path: "a", Err: base}
	err := errors.Wrap(fmt.Errorf("query: %w", errors.Wrap(pathErr, "inner")), "outer")
	if !errors.Is(err, base) {
		t.Error("Is does not cross the standard level")
	}
	if got, ok := errors.AsType[*fs.PathError](err); !ok || got != pathErr {
		t.Error("AsType does not cross the standard level")
	}
}

func TestJoin_Semantics(t *testing.T) {
	a, b := stderrors.New("a"), stderrors.New("b")
	j := errors.Join(a, nil, b)
	if want := stderrors.Join(a, nil, b).Error(); j.Error() != "a\nb" || j.Error() != want {
		t.Errorf("message = %q, want %q", j.Error(), want)
	}
	m, ok := j.(interface{ Unwrap() []error })
	if !ok || len(m.Unwrap()) != 2 || m.Unwrap()[0] != a || m.Unwrap()[1] != b { //nolint:errorlint // identity check (FR-012)
		t.Errorf("Join(a, nil, b) causes = %v", j)
	}
	if !errors.Is(j, a) || !errors.Is(j, b) {
		t.Error("joined errors are not found")
	}
	single := errors.Join(a)
	if sm, ok := single.(interface{ Unwrap() []error }); !ok || len(sm.Unwrap()) != 1 {
		t.Errorf("Join(a) = %#v, want one cause", single)
	}
}

func TestWrapf_Context(t *testing.T) {
	cause := stderrors.New("cause")
	if got := errors.Wrapf(cause, "load tenant %d", 42).Error(); got != "load tenant 42: cause" {
		t.Errorf("Wrapf message = %q", got)
	}
}

type boomError struct{}

func (boomError) Error() string { panic("boom") }

type nilDerefError struct{ msg *string }

func (e *nilDerefError) Error() string { return *e.msg }

// TestWrap_PanickingCause checks that wrapping an error whose Error method
// panics behaves like fmt.Errorf, which prints a placeholder instead of
// panicking (SC-001).
func TestWrap_PanickingCause(t *testing.T) {
	var nilPtr *nilDerefError
	for name, cause := range map[string]error{"panic": boomError{}, "nil receiver": nilPtr} {
		t.Run(name, func(t *testing.T) {
			want := fmt.Errorf("ctx: %w", cause).Error()
			if got := errors.Wrap(cause, "ctx").Error(); got != want {
				t.Errorf("Wrap message = %q, want %q", got, want)
			}
			if got := errors.WithStack(cause).Error(); got != fmt.Sprintf("%v", cause) {
				t.Errorf("WithStack message = %q", got)
			}
			if j := errors.Join(cause, stderrors.New("x")); j == nil {
				t.Error("Join returned nil")
			}
		})
	}
}

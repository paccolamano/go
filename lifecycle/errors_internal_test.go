package lifecycle

import (
	"errors"
	"testing"
)

func TestErrors_ComponentError(t *testing.T) {
	boom := errors.New("boom")
	tests := []struct {
		err  *ComponentError
		want string
	}{
		{&ComponentError{Name: "db", Err: boom}, `lifecycle: component "db": boom`},
		{&ComponentError{Name: "db", Err: ErrStopTimeout}, `lifecycle: component "db": not stopped in time`},
		{&ComponentError{Name: "q", Err: errors.Join(errors.New("stop: boom"))}, `lifecycle: component "q": stop: boom`},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if got := tt.err.Error(); got != tt.want {
				t.Fatalf("Error() = %q, want %q", got, tt.want)
			}
		})
	}
	if err := error(&ComponentError{Name: "db", Err: boom}); !errors.Is(err, boom) {
		t.Fatal("errors.Is does not reach the wrapped error")
	}
}

func TestErrors_PanicError(t *testing.T) {
	err := &PanicError{Value: 42, Stack: []byte("stack")}
	if got := err.Error(); got != "panic: 42" {
		t.Fatalf("Error() = %q, want %q", got, "panic: 42")
	}
	if err.Value != 42 || string(err.Stack) != "stack" {
		t.Fatalf("fields not kept: %+v", err)
	}
}

func TestErrors_Compose(t *testing.T) {
	var empty outcomes
	if err := empty.err(); err != nil {
		t.Fatalf("empty outcomes: err = %v, want nil", err)
	}

	trigger := &ComponentError{Name: "b", Err: &PanicError{Value: "bad"}}
	later := &ComponentError{Name: "c", Err: ErrExitedEarly}
	stop := &ComponentError{Name: "a", Err: ErrStopTimeout}
	refused := &ComponentError{Name: "late", Err: ErrInvalidConfig}
	o := outcomes{
		trigger: trigger,
		later:   []error{later},
		stops:   []error{stop},
		forced:  true,
		refused: []error{refused},
	}
	err := o.err()

	var joined interface{ Unwrap() []error }
	if !errors.As(err, &joined) {
		t.Fatalf("err = %T, want a joined error", err)
	}
	want := []error{trigger, later, stop, ErrForced, refused}
	got := joined.Unwrap()
	if len(got) != len(want) {
		t.Fatalf("joined %d errors, want %d", len(got), len(want))
	}
	for i := range want {
		if !errors.Is(got[i], want[i]) {
			t.Errorf("error %d = %v, want %v", i, got[i], want[i])
		}
	}

	for _, target := range []error{ErrExitedEarly, ErrStopTimeout, ErrForced, ErrInvalidConfig} {
		if !errors.Is(err, target) {
			t.Errorf("errors.Is(err, %v) = false", target)
		}
	}
	var pe *PanicError
	if !errors.As(err, &pe) || pe.Value != "bad" {
		t.Errorf("errors.As(*PanicError) = %v", pe)
	}
	var ce *ComponentError
	if !errors.As(err, &ce) || ce.Name != "b" {
		t.Errorf("errors.As(*ComponentError) = %v", ce)
	}
}

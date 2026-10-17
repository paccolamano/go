package errors

import (
	stderrors "errors"
	"fmt"
	"runtime"
	"slices"
	"strings"
	"testing"
)

type nilUnwrapError struct{}

func (nilUnwrapError) Error() string { return "nil unwrap" }
func (nilUnwrapError) Unwrap() error { return nil }

type multiError struct{ errs []error }

func (m multiError) Error() string   { return "multi" }
func (m multiError) Unwrap() []error { return m.errs }

func TestCauses_Shapes(t *testing.T) {
	a, b := stderrors.New("a"), stderrors.New("b")
	tests := []struct {
		name string
		err  error
		want []error
	}{
		{"nil", nil, nil},
		{"no unwrap", a, nil},
		{"unwrap returns nil", nilUnwrapError{}, nil},
		{"single", fmt.Errorf("x: %w", a), []error{a}},
		{"multi drops nil", multiError{[]error{a, nil, b}}, []error{a, b}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := causes(tt.err); !slices.Equal(got, tt.want) {
				t.Errorf("causes() = %v, want %v", got, tt.want)
			}
		})
	}
}

func frameOf(pc uintptr) runtime.Frame {
	f, _ := runtime.CallersFrames([]uintptr{pc}).Next()
	return f
}

func captureHere() (uintptr, *callStack, int) {
	_, _, base, _ := runtime.Caller(0)
	pc, cs := capture(0, false)
	return pc, cs, base + 1
}

func captureHereOrigin() (uintptr, *callStack, int) {
	_, _, base, _ := runtime.Caller(0)
	pc, cs := capture(0, true)
	return pc, cs, base + 1
}

func captureDeep(n int) (uintptr, *callStack) {
	if n == 0 {
		return capture(0, true)
	}
	return captureDeep(n - 1)
}

func TestCapture_Location(t *testing.T) {
	pc, cs, line := captureHere()
	if cs != nil {
		t.Errorf("plain capture returned a call stack")
	}
	f := frameOf(pc)
	if !strings.HasSuffix(f.Function, ".captureHere") || f.Line != line {
		t.Errorf("capture resolved to %s:%d, want captureHere:%d", f.Function, f.Line, line)
	}
}

func TestCapture_Origin(t *testing.T) {
	pc, cs, line := captureHereOrigin()
	if cs == nil {
		t.Fatal("origin capture returned no call stack")
	}
	if len(cs.pcs) == 0 || len(cs.pcs) > maxStackDepth || cs.pcs[0] != pc {
		t.Fatalf("pcs = %d entries, pcs[0] == pc: %v", len(cs.pcs), len(cs.pcs) > 0 && cs.pcs[0] == pc)
	}
	if f := frameOf(pc); !strings.HasSuffix(f.Function, ".captureHereOrigin") || f.Line != line {
		t.Errorf("origin resolved to %s:%d, want captureHereOrigin:%d", f.Function, f.Line, line)
	}
	if cs.truncated {
		t.Errorf("shallow stack reported as truncated")
	}

	t.Run("deep", func(t *testing.T) {
		_, cs := captureDeep(40)
		if len(cs.pcs) != maxStackDepth || !cs.truncated {
			t.Errorf("40 frames deep: %d pcs, truncated=%v; want %d, true", len(cs.pcs), cs.truncated, maxStackDepth)
		}
	})
	t.Run("shallow", func(t *testing.T) {
		_, cs := captureDeep(10)
		if cs.truncated {
			t.Errorf("10 frames deep reported as truncated (%d pcs)", len(cs.pcs))
		}
	})
}

// initPCs is captured while the package is being initialized.
var initPCs = callersForTest()

func callersForTest() []uintptr {
	var buf [64]uintptr
	n := runtime.Callers(1, buf[:])
	return buf[:n]
}

func TestDuringInit(t *testing.T) {
	if !duringInit(initPCs) {
		t.Error("PCs captured during initialization are not recognized")
	}
	if duringInit(callersForTest()) {
		t.Error("PCs captured in a test are reported as initialization")
	}
	ch := make(chan []uintptr)
	go func() { ch <- callersForTest() }()
	if duringInit(<-ch) {
		t.Error("PCs captured in a goroutine are reported as initialization")
	}
}

type fakeTracerError struct{ pc uintptr }

func (f fakeTracerError) Error() string                { return "fake" }
func (f fakeTracerError) trace() (uintptr, *callStack) { return f.pc, nil }

func TestIsTraced(t *testing.T) {
	traced, untraced := fakeTracerError{pc: 1}, fakeTracerError{}
	deep := error(traced)
	for range 70 {
		deep = fmt.Errorf("l: %w", deep)
	}
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"untraced tracer", untraced, false},
		{"traced tracer", traced, true},
		{"foreign over traced", fmt.Errorf("q: %w", traced), true},
		{"foreign over foreign", fmt.Errorf("q: %w", stderrors.New("x")), false},
		{"join with traced branch", stderrors.Join(stderrors.New("a"), traced), true},
		{"traced beyond the depth limit", deep, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isTraced(tt.err); got != tt.want {
				t.Errorf("isTraced() = %v, want %v", got, tt.want)
			}
		})
	}
}

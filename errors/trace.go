package errors

import (
	"runtime"
	"strings"
)

// Limits of the work done by the package. They are documented in doc.go.
const (
	// maxStackDepth is the number of frames kept in an origin's call stack.
	maxStackDepth = 32
	// maxDetailDepth is the number of levels followed along any path of a tree.
	maxDetailDepth = 64
	// maxDetailLevels is the number of levels described in one detail.
	maxDetailLevels = 256
)

// callStack is the call stack recorded at an origin, most recent call first.
type callStack struct {
	pcs       []uintptr
	truncated bool
}

// tracer is implemented by every error of this package, including types that
// embed BaseError. A zero pc means that the error has no location.
type tracer interface {
	trace() (pc uintptr, origin *callStack)
}

// capture records the location of the code skip frames above its caller, and
// the call stack too when origin is true. skip == 0 designates the function
// that calls capture.
func capture(skip int, origin bool) (uintptr, *callStack) {
	if !origin {
		var pcs [1]uintptr
		if runtime.Callers(skip+2, pcs[:]) == 0 {
			return 0, nil
		}
		return pcs[0], nil
	}
	var buf [maxStackDepth + 1]uintptr
	n := runtime.Callers(skip+2, buf[:])
	if n == 0 {
		return 0, nil
	}
	truncated := n > maxStackDepth
	if !truncated && duringInit(buf[:n]) {
		return 0, nil
	}
	if truncated {
		n = maxStackDepth
	}
	pcs := make([]uintptr, n)
	copy(pcs, buf[:n])
	return pcs[0], &callStack{pcs: pcs, truncated: truncated}
}

// duringInit reports whether pcs, a complete call stack, was captured while
// packages were being initialized. The main goroutine then runs runtime.main,
// which initializes the packages before it calls main.main: scanning from the
// outermost frame and skipping the runtime, the first other function is a
// package initializer during initialization, and main.main afterwards. Other
// goroutines do not start from runtime.main.
func duringInit(pcs []uintptr) bool {
	sawMain := false
	for i := len(pcs) - 1; i >= 0; i-- {
		fn := runtime.FuncForPC(pcs[i] - 1)
		if fn == nil {
			return false
		}
		name := fn.Name()
		if strings.HasPrefix(name, "runtime.") {
			sawMain = sawMain || name == "runtime.main"
			continue
		}
		return sawMain && name != "main.main"
	}
	return false
}

// causes returns the direct causes of err: the result of Unwrap() error, or the
// non-nil entries of Unwrap() []error.
func causes(err error) []error {
	switch u := err.(type) { //nolint:errorlint // walking the tree must inspect each level's own Unwrap method, not its chain
	case interface{ Unwrap() error }:
		if c := u.Unwrap(); c != nil {
			return []error{c}
		}
	case interface{ Unwrap() []error }:
		var cs []error
		for _, c := range u.Unwrap() {
			if c != nil {
				cs = append(cs, c)
			}
		}
		return cs
	}
	return nil
}

// safeCauses is causes, but it reports false instead of panicking when the
// Unwrap method of err panics.
func safeCauses(err error) (cs []error, ok bool) {
	defer func() {
		if recover() != nil {
			cs, ok = nil, false
		}
	}()
	return causes(err), true
}

// unwrapInto stores u.Unwrap() in *next without allocating, and reports false
// instead of panicking when the method panics.
func unwrapInto(u interface{ Unwrap() error }, next *error) (ok bool) {
	defer func() {
		if recover() != nil {
			ok = false
		}
	}()
	*next = u.Unwrap()
	return true
}

// isTraced reports whether err, or an error below it, carries a location. It
// stops at the first error of this package on each branch, and gives up after
// maxDetailDepth levels in depth or maxDetailLevels levels in total.
func isTraced(err error) bool {
	budget := maxDetailLevels
	return traced(err, 0, &budget)
}

func traced(err error, depth int, budget *int) bool {
	for ; err != nil && depth < maxDetailDepth && *budget > 0; depth++ {
		*budget--
		if t, ok := err.(tracer); ok {
			pc, _ := t.trace()
			return pc != 0
		}
		if u, ok := err.(interface{ Unwrap() error }); ok {
			var next error
			if !unwrapInto(u, &next) {
				return false
			}
			err = next
			continue
		}
		cs, _ := safeCauses(err)
		switch len(cs) {
		case 0:
			return false
		case 1:
			err = cs[0]
		default:
			for _, c := range cs {
				if traced(c, depth+1, budget) {
					return true
				}
			}
			return false
		}
	}
	return false
}

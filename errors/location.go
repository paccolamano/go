package errors

import (
	"path"
	"runtime"
	"strings"
)

// locationOf converts a runtime frame into a Location whose file is named by
// its package import path and base name, so it does not depend on the directory
// where the program was built.
func locationOf(f runtime.Frame) Location {
	file := "unknown"
	if f.File != "" {
		file = path.Base(f.File)
	}
	return Location{
		Function: unescape(f.Function),
		File:     packagePath(f.Function) + "/" + file,
		Line:     f.Line,
	}
}

// packagePath returns the import path of the package of the function named fn,
// as the runtime names it: the linker escapes the dots of the last path element
// as %2e, so the package ends at the first dot after the last slash.
func packagePath(fn string) string {
	if i := strings.IndexByte(fn, '['); i >= 0 {
		fn = fn[:i]
	}
	slash := strings.LastIndexByte(fn, '/')
	if dot := strings.IndexByte(fn[slash+1:], '.'); dot >= 0 {
		fn = fn[:slash+1+dot]
	}
	return unescape(fn)
}

func unescape(s string) string { return strings.ReplaceAll(s, "%2e", ".") }

// resolve returns the location of pc, or nil when pc is 0.
func resolve(pc uintptr) *Location {
	if pc == 0 {
		return nil
	}
	f, _ := runtime.CallersFrames([]uintptr{pc}).Next()
	loc := locationOf(f)
	return &loc
}

// resolveStack returns the frames of cs, or nil when cs is nil.
func resolveStack(cs *callStack) *Stack {
	if cs == nil {
		return nil
	}
	st := &Stack{Frames: make([]Location, 0, len(cs.pcs)), Truncated: cs.truncated}
	frames := runtime.CallersFrames(cs.pcs)
	for len(st.Frames) < maxStackDepth {
		f, more := frames.Next()
		st.Frames = append(st.Frames, locationOf(f))
		if !more {
			break
		}
	}
	return st
}

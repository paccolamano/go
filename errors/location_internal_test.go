package errors

import (
	"runtime"
	"testing"
)

func TestLocation_FromFrame(t *testing.T) {
	tests := []struct {
		name  string
		frame runtime.Frame
		want  Location
	}{
		{
			"absolute path",
			runtime.Frame{Function: "example.com/app/api.Handler", File: "/home/u/src/app/api/handler.go", Line: 37},
			Location{Function: "example.com/app/api.Handler", File: "example.com/app/api/handler.go", Line: 37},
		},
		{
			"escaped dot in the last path element",
			runtime.Frame{Function: "gopkg.in/yaml%2ev3.(*T).M", File: "/go/pkg/mod/gopkg.in/yaml.v3/t.go", Line: 5},
			Location{Function: "gopkg.in/yaml.v3.(*T).M", File: "gopkg.in/yaml.v3/t.go", Line: 5},
		},
		{
			"main package",
			runtime.Frame{Function: "main.main", File: "/a/b/main.go", Line: 9},
			Location{Function: "main.main", File: "main/main.go", Line: 9},
		},
		{
			"closure",
			runtime.Frame{Function: "example.com/app.F.func1", File: "/a/app/f.go", Line: 1},
			Location{Function: "example.com/app.F.func1", File: "example.com/app/f.go", Line: 1},
		},
		{
			"generic function",
			runtime.Frame{Function: "example.com/app.G[...]", File: "/a/app/f.go", Line: 2},
			Location{Function: "example.com/app.G[...]", File: "example.com/app/f.go", Line: 2},
		},
		{
			"method",
			runtime.Frame{Function: "example.com/app.(*S).M", File: "/a/app/f.go", Line: 3},
			Location{Function: "example.com/app.(*S).M", File: "example.com/app/f.go", Line: 3},
		},
		{
			"runtime assembly",
			runtime.Frame{Function: "runtime.goexit", File: "/usr/lib/go/src/runtime/asm_amd64.s", Line: 1700},
			Location{Function: "runtime.goexit", File: "runtime/asm_amd64.s", Line: 1700},
		},
		{
			"unknown file and line",
			runtime.Frame{Function: "example.com/app.F"},
			Location{Function: "example.com/app.F", File: "example.com/app/unknown", Line: 0},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := locationOf(tt.frame); got != tt.want {
				t.Errorf("locationOf() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

// TestLocation_BuildDirectoryIndependent checks that the same function built in
// different directories, or with -trimpath, yields the same location (FR-018).
func TestLocation_BuildDirectoryIndependent(t *testing.T) {
	fn := "example.com/app/api.Handler"
	want := locationOf(runtime.Frame{Function: fn, File: "/home/u/src/app/api/handler.go", Line: 37})
	for _, file := range []string{"/tmp/x/api/handler.go", "example.com/app/api/handler.go", "C:/build/app/api/handler.go"} {
		if got := locationOf(runtime.Frame{Function: fn, File: file, Line: 37}); got != want {
			t.Errorf("file %q: got %+v, want %+v", file, got, want)
		}
	}
}

func TestLocation_RealFrame(t *testing.T) {
	var pcs [1]uintptr
	_, _, line, _ := runtime.Caller(0)
	runtime.Callers(1, pcs[:])
	loc := resolve(pcs[0])
	if loc == nil {
		t.Fatal("resolve returned nil")
	}
	want := Location{
		Function: "github.com/paccolamano/go/errors.TestLocation_RealFrame",
		File:     "github.com/paccolamano/go/errors/location_internal_test.go",
		Line:     line + 1,
	}
	if *loc != want {
		t.Errorf("resolve() = %+v, want %+v", *loc, want)
	}
	if resolve(0) != nil || resolveStack(nil) != nil {
		t.Error("no location must resolve to nil")
	}
}

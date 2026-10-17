package errors_test

import (
	stderrors "errors"
	"fmt"
	"io/fs"
	"testing"

	"github.com/paccolamano/go/errors"
)

func TestDropIn_Forwarders(t *testing.T) {
	pathErr := &fs.PathError{Op: "open", Path: "a.txt", Err: fs.ErrNotExist}
	other := stderrors.New("other")
	trees := map[string]error{
		"fmt":      fmt.Errorf("load: %w", pathErr),
		"std join": stderrors.Join(other, pathErr),
		"library":  errors.Wrap(errors.Wrap(pathErr, "open config"), "load"),
		"mixed":    errors.Wrap(fmt.Errorf("q: %w", errors.Join(other, pathErr)), "outer"),
	}
	for name, tree := range trees {
		t.Run(name, func(t *testing.T) {
			for _, target := range []error{pathErr, fs.ErrNotExist, other, stderrors.New("unrelated")} {
				if got, want := errors.Is(tree, target), stderrors.Is(tree, target); got != want {
					t.Errorf("Is(%v) = %v, standard library says %v", target, got, want)
				}
			}
			var ours, theirs *fs.PathError
			if errors.As(tree, &ours) != stderrors.As(tree, &theirs) || ours != theirs {
				t.Errorf("As found %v, standard library found %v", ours, theirs)
			}
			got, gotOK := errors.AsType[*fs.PathError](tree)
			want, wantOK := stderrors.AsType[*fs.PathError](tree)
			if got != want || gotOK != wantOK {
				t.Errorf("AsType = %v, %v; standard library: %v, %v", got, gotOK, want, wantOK)
			}
			if errors.Unwrap(tree) != stderrors.Unwrap(tree) { //nolint:errorlint // identity check (FR-001)
				t.Errorf("Unwrap differs from the standard library")
			}
		})
	}
}

func TestDropIn_ErrUnsupported(t *testing.T) {
	if errors.ErrUnsupported != stderrors.ErrUnsupported { //nolint:errorlint // identity check (FR-001)
		t.Fatal("ErrUnsupported is not the standard library's value")
	}
	if !stderrors.Is(errors.Wrap(errors.ErrUnsupported, "op"), stderrors.ErrUnsupported) {
		t.Error("standard Is does not match the library sentinel wrapped by the library")
	}
	if !errors.Is(fmt.Errorf("op: %w", stderrors.ErrUnsupported), errors.ErrUnsupported) {
		t.Error("library Is does not match the standard sentinel wrapped by fmt")
	}
}

// TestDropIn_StdlibSurface is written exactly as code against the standard
// errors package would be (User Story 1, scenario 8).
func TestDropIn_StdlibSurface(t *testing.T) {
	err := errors.New("x")
	if err.Error() != "x" {
		t.Errorf("New: message %q", err.Error())
	}
	if errors.Unwrap(err) != nil {
		t.Error("Unwrap(New(x)) is not nil")
	}
	if errors.Join(nil, nil) != nil {
		t.Error("Join(nil, nil) is not nil")
	}
	a, b := errors.New("a"), errors.New("b")
	if !errors.Is(errors.Join(a, b), b) {
		t.Error("Is(Join(a, b), b) is false")
	}
	wrapped := fmt.Errorf("open: %w", &fs.PathError{Op: "open", Path: "x", Err: fs.ErrNotExist})
	var pe *fs.PathError
	if !errors.As(wrapped, &pe) || pe.Path != "x" {
		t.Error("As did not fill the target")
	}
	if got, ok := errors.AsType[*fs.PathError](wrapped); !ok || got != pe {
		t.Error("AsType did not find the same error as As")
	}
	if !errors.Is(fmt.Errorf("op: %w", errors.ErrUnsupported), errors.ErrUnsupported) {
		t.Error("Is(ErrUnsupported) is false")
	}
}

func TestDropIn_MixedPackages(t *testing.T) {
	stdRoot := stderrors.New("std root")
	libRoot := errors.New("lib root")
	stdOverLib := fmt.Errorf("std: %w", errors.Wrap(libRoot, "lib"))
	libOverStd := errors.Wrap(stderrors.Join(stdRoot), "lib")
	for _, tc := range []struct {
		err, target error
	}{
		{stdOverLib, libRoot},
		{libOverStd, stdRoot},
	} {
		if !errors.Is(tc.err, tc.target) || !stderrors.Is(tc.err, tc.target) {
			t.Errorf("%q: target %q not found by both packages", tc.err, tc.target)
		}
	}
}

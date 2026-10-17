// Package vetmisuse holds deliberate misuse of the errors package, analyzed by
// TestVetReportsWrapfMisuse. The go tool ignores testdata in ./... patterns.
package vetmisuse

import (
	"io/fs"

	"github.com/paccolamano/go/errors"
)

// Misuse returns errors built with valid and invalid format directives.
func Misuse(err error) []error {
	return []error{
		errors.Errorf("ok: %w", fs.ErrNotExist),
		errors.Errorf("ok: %w %w", err, fs.ErrNotExist),
		errors.Wrapf(err, "bad: %w", fs.ErrNotExist),
		errors.Wrapf(err, "bad arg count %d"),
		errors.Skip(1).Wrapf(err, "bad: %w", fs.ErrNotExist),
		errors.Skip(1).Errorf("ok: %w", fs.ErrNotExist),
	}
}

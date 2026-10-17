package errors

import (
	"fmt"
	"io"
)

// formatError implements fmt.Formatter for the errors of this package: %+v
// writes the detail of err, and every other verb formats its message exactly as
// it would format a standard library error with the same message.
func formatError(err error, s fmt.State, verb rune) {
	if verb == 'v' && s.Flag('+') {
		_, _ = io.WriteString(s, Describe(err).String())
		return
	}
	_, _ = fmt.Fprintf(s, fmt.FormatString(s, verb), err.Error())
}

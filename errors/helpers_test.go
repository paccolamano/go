package errors_test

import (
	"regexp"
	"runtime"
	"testing"
)

// callerLine returns the line of the statement that calls it, so a test can
// capture the expected location on the same line as the call under test.
func callerLine(tb testing.TB) int {
	tb.Helper()
	_, _, line, ok := runtime.Caller(1)
	if !ok {
		tb.Fatal("runtime.Caller failed")
	}
	return line
}

// here is callerLine for helpers that have no testing.TB.
func here() int {
	_, _, line, _ := runtime.Caller(1)
	return line
}

var lineNumbers = regexp.MustCompile(`:\d+\)`)

// normalizeLines replaces the line numbers of rendered locations with N.
func normalizeLines(s string) string {
	return lineNumbers.ReplaceAllString(s, ":N)")
}

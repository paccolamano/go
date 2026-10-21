package sse

import (
	"crypto/rand"
	"strconv"
)

// newPrefix returns the random prefix of a broker's generated identifiers: 12
// base32 characters, 60 bits. Identifiers of another instance, or of a previous
// process, therefore never match this one's (research R-10).
func newPrefix() string {
	return rand.Text()[:12]
}

// formatID returns the generated identifier of the event with sequence seq.
func formatID(prefix string, seq uint64) string {
	return prefix + "-" + strconv.FormatUint(seq, 10)
}

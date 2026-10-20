package auth_test

import (
	"bytes"
	"encoding/base32"
	"encoding/base64"
	"encoding/hex"
	"io"
	"net/http"
	"strings"
)

// downIdP is an identity provider whose key server answers 503.
type downIdP struct{}

func (downIdP) RoundTrip(r *http.Request) (*http.Response, error) {
	return &http.Response{StatusCode: http.StatusServiceUnavailable, Body: io.NopCloser(bytes.NewReader(nil)), Header: http.Header{}, Request: r}, nil
}

// encodings returns s raw and in the encodings a leak could take.
func encodings(s string) []string {
	b := []byte(s)
	return []string{
		s,
		hex.EncodeToString(b),
		base64.StdEncoding.EncodeToString(b),
		base64.RawStdEncoding.EncodeToString(b),
		base64.RawURLEncoding.EncodeToString(b),
		base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(b),
		strings.ToLower(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(b)),
	}
}

package slogx_test

import (
	"bytes"
	"fmt"
	"log/slog"
	"strings"
	"testing"

	"github.com/paccolamano/go/slogx"
)

// FuzzRedact checks that a value under a sensitive key never reaches the
// output, whatever the case of the key and the depth of the attribute
// (SC-004), and that redaction never panics.
func FuzzRedact(f *testing.F) {
	f.Add("token", uint8(0), uint8(0))
	f.Add("Authorization", uint8(5), uint8(3))
	f.Add("cookie", uint8(255), uint8(7))
	f.Fuzz(func(t *testing.T, key string, caseMask, depth uint8) {
		if strings.TrimSpace(key) == "" {
			t.Skip()
		}
		variant := []byte(key)
		for i, c := range variant[:min(len(variant), 8)] {
			if caseMask&(1<<i) != 0 && ('a' <= c && c <= 'z' || 'A' <= c && c <= 'Z') {
				variant[i] = c ^ 0x20 // flip the case of an ASCII letter
			}
		}
		const sentinel = "SENTINEL-VALUE"
		attr := slog.String(string(variant), sentinel)
		for i := int(depth%8) - 1; i >= 0; i-- {
			attr = slog.Group(fmt.Sprintf("g%d", i), attr)
		}
		var buf bytes.Buffer
		h := must(slogx.NewRedactHandler(slog.NewJSONHandler(&buf, nil), slogx.WithRedactKeys(key)))
		slog.New(h).Info("m", attr)
		if strings.Contains(buf.String(), sentinel) {
			t.Errorf("key %q (variant %q): sentinel leaked: %s", key, string(variant), buf.String())
		}
	})
}

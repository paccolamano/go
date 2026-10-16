package mediatype

import (
	"errors"
	"mime"
	"testing"
)

func FuzzMediaTypeMatch(f *testing.F) {
	f.Add("application/json", "application/json; charset=utf-8")
	f.Add("text/*", "text/html")
	f.Add("application/*+json", "application/ld+json")
	f.Add("*/*", "")
	f.Fuzz(func(t *testing.T, pattern, contentType string) {
		m, err := ParsePatterns([]string{pattern})
		if err != nil {
			return
		}
		got := m.Match(contentType)
		if _, _, perr := mime.ParseMediaType(contentType); perr != nil && !errors.Is(perr, mime.ErrInvalidMediaParameter) && got {
			t.Fatalf("Match(%q) = true for an unparseable content type (pattern %q)", contentType, pattern)
		}
	})
}

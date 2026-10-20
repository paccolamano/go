package jwt

import (
	"testing"
	"time"
)

func TestCacheLifetime(t *testing.T) {
	for header, want := range map[string]time.Duration{
		"":                                  time.Hour,
		"max-age=120":                       2 * time.Minute,
		"public, MAX-AGE=600":               10 * time.Minute,
		"no-cache":                          time.Minute,
		"no-store":                          time.Minute,
		"max-age=0":                         time.Minute,
		"max-age=999999":                    24 * time.Hour,
		"max-age=-5":                        time.Hour,
		"max-age=abc":                       time.Hour,
		"max-age=99999999999999999999":      time.Hour,
		"max-age=300, no-cache":             time.Minute,
		"private, max-age=3600, s-maxage=1": time.Hour,
	} {
		if got := cacheLifetime(header); got != want {
			t.Errorf("cacheLifetime(%q) = %v, want %v", header, got, want)
		}
	}
}

func FuzzCacheControl(f *testing.F) {
	for _, s := range []string{"", "max-age=120", "no-cache", "max-age=99999999999999999999", "max-age=", ",,,"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, header string) {
		if got := cacheLifetime(header); got < time.Minute || got > 24*time.Hour {
			t.Fatalf("cacheLifetime(%q) = %v, outside [1m, 24h]", header, got)
		}
	})
}

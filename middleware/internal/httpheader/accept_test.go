package httpheader

import "testing"

func TestNegotiate(t *testing.T) {
	pref := []string{"br", "gzip", "deflate"}
	tests := []struct {
		name   string
		accept []string
		want   string
	}{
		{"absent", nil, ""},
		{"empty", []string{""}, ""},
		{"single", []string{"gzip"}, "gzip"},
		{"server preference breaks ties", []string{"deflate, gzip, br"}, "br"},
		{"quality wins", []string{"br;q=0.5, gzip"}, "gzip"},
		{"x-gzip alias", []string{"x-gzip"}, "gzip"},
		{"case-insensitive", []string{"GZip;Q=1"}, "gzip"},
		{"star", []string{"*"}, "br"},
		{"star with exclusion", []string{"*, br;q=0"}, "gzip"},
		{"explicit beats star", []string{"*;q=0.1, deflate;q=0.5"}, "deflate"},
		{"everything excluded", []string{"gzip;q=0, *;q=0"}, ""},
		{"identity only", []string{"identity"}, ""},
		{"unsupported only", []string{"zstd"}, ""},
		{"malformed q ignored", []string{"br;q=abc, gzip;q=0.3"}, "gzip"},
		{"q above one ignored", []string{"br;q=2, deflate"}, "deflate"},
		{"several lines", []string{"deflate;q=0.2", "gzip;q=0.9"}, "gzip"},
		{"spaces", []string{" gzip ; q=0.7 , deflate ;q=0.8"}, "deflate"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Negotiate(tt.accept, pref); got != tt.want {
				t.Errorf("Negotiate(%q) = %q, want %q", tt.accept, got, tt.want)
			}
		})
	}
}

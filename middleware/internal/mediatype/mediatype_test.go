package mediatype

import "testing"

func TestParsePatterns(t *testing.T) {
	valid := [][]string{
		{"application/json"},
		{"text/*"},
		{"application/*+json"},
		{"*/*"},
		{"application/vnd.api+json", "image/svg+xml"},
	}
	for _, p := range valid {
		if _, err := ParsePatterns(p); err != nil {
			t.Errorf("ParsePatterns(%q) error = %v, want nil", p, err)
		}
	}
	invalid := [][]string{
		nil,
		{""},
		{"json"},
		{"Application/json"},
		{"application/JSON"},
		{"*/json"},
		{"application/json; charset=utf-8"},
		{"text/"},
		{"/plain"},
		{"application/*+"},
		{"application/*json"},
		{"a b/c"},
	}
	for _, p := range invalid {
		if _, err := ParsePatterns(p); err == nil {
			t.Errorf("ParsePatterns(%q) error = nil, want an error", p)
		}
	}
}

func TestMatcher_Match(t *testing.T) {
	m, err := ParsePatterns([]string{"application/json", "text/*", "application/*+xml"})
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		ct   string
		want bool
	}{
		{"application/json", true},
		{"application/json; charset=utf-8", true},
		{"APPLICATION/JSON", true},
		{"text/plain", true},
		{"text/html; charset=utf-8", true},
		{"application/atom+xml", true},
		{"application/xml", false},
		{"application/jsonx", false},
		{"image/png", false},
		{"", false},
		{"not a media type", false},
		{"application/", false},
	}
	for _, tt := range tests {
		t.Run(tt.ct, func(t *testing.T) {
			if got := m.Match(tt.ct); got != tt.want {
				t.Errorf("Match(%q) = %v, want %v", tt.ct, got, tt.want)
			}
		})
	}
	all, err := ParsePatterns([]string{"*/*"})
	if err != nil {
		t.Fatal(err)
	}
	if !all.Match("image/png") || all.Match("") {
		t.Error("*/* must match any declared type and nothing else")
	}
}

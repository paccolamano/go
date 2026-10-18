package problem

import "testing"

func TestURI_ValidReference(t *testing.T) {
	tests := []struct {
		in   string
		want bool
	}{
		{"https://example.com/probs/out-of-credit", true},
		{"/probs/out-of-credit", true},
		{"//example.com/p", true},
		{"tag:example.com,2026:out-of-credit", true},
		{"urn:example:out-of-credit", true},
		{"out-of-credit", true},
		{"/a%20b", true},
		{"#frag", true},
		{"about:blank", true},
		{"", false},
		{"/a b", false},
		{"/é", false},
		{"%zz", false},
		{"%2", false},
		{"http://[::1", false},
		{"http://h:port", false},
		{"/a\x7f", false},
		{`/a"b`, false},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			if got := validURIReference(tt.in); got != tt.want {
				t.Errorf("validURIReference(%q) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

func TestURI_AbsoluteOrFullPath(t *testing.T) {
	tests := []struct {
		in   string
		want bool
	}{
		{"https://example.com/probs/out-of-credit", true},
		{"/probs/out-of-credit", true},
		{"//example.com/p", true},
		{"tag:example.com,2026:out-of-credit", true},
		{"urn:example:out-of-credit", true},
		{"out-of-credit", false},
		{"probs/x", false},
		{"#x", false},
		{"?x", false},
		{"/a b", false},
		{"", false},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			if got := isTypeURI(tt.in); got != tt.want {
				t.Errorf("isTypeURI(%q) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

func TestName_Valid(t *testing.T) {
	tests := []struct {
		in   string
		want bool
	}{
		{"balance", true},
		{"retry_after", true},
		{"errors", true},
		{"abc", true},
		{"X12", true},
		{"", false},
		{"ab", false},
		{"id", false},
		{"user-id", false},
		{"2fa", false},
		{"_meta", false},
		{"bal ance", false},
		{"bälance", false},
		{"type", false},
		{"title", false},
		{"status", false},
		{"detail", false},
		{"instance", false},
		{"Status", false},
		{"TYPE", false},
		{"Detail", false},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			if got := validExtensionName(tt.in); got != tt.want {
				t.Errorf("validExtensionName(%q) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

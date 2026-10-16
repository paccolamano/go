package httpheader

import "testing"

func TestMatchIfNoneMatch(t *testing.T) {
	tests := []struct {
		name   string
		values []string
		etag   string
		want   bool
	}{
		{"absent", nil, `"a"`, false},
		{"star", []string{"*"}, `"a"`, true},
		{"exact strong", []string{`"a"`}, `"a"`, true},
		{"weak request, strong etag", []string{`W/"a"`}, `"a"`, true},
		{"strong request, weak etag", []string{`"a"`}, `W/"a"`, true},
		{"list", []string{`"x", W/"a", "y"`}, `"a"`, true},
		{"several lines", []string{`"x"`, `"a"`}, `"a"`, true},
		{"no match", []string{`"x", "y"`}, `"a"`, false},
		{"comma inside tag", []string{`"a,b"`}, `"a,b"`, true},
		{"malformed list", []string{`"x", y`}, `"x"`, false},
		{"unquoted", []string{`a`}, `"a"`, false},
		{"empty etag", []string{`""`}, ``, false},
		{"empty tag matches empty tag", []string{`""`}, `""`, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := MatchIfNoneMatch(tt.values, tt.etag); got != tt.want {
				t.Errorf("MatchIfNoneMatch(%q, %q) = %v, want %v", tt.values, tt.etag, got, tt.want)
			}
		})
	}
}

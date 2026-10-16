package httpheader

import (
	"net/http"
	"slices"
	"testing"
)

func TestValidToken(t *testing.T) {
	tests := []struct {
		in   string
		want bool
	}{
		{"X-Request-ID", true},
		{"gzip", true},
		{"!#$%&'*+-.^_`|~09azAZ", true},
		{"", false},
		{"two words", false},
		{"semi;colon", false},
		{"quote\"", false},
		{"tab\t", false},
		{"ünicode", false},
		{"a\x00", false},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			if got := ValidToken(tt.in); got != tt.want {
				t.Errorf("ValidToken(%q) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

func TestValidValue(t *testing.T) {
	tests := []struct {
		in   string
		want bool
	}{
		{"default-src 'none'", true},
		{"", true},
		{"tab\tinside", true},
		{"cr\rinside", false},
		{"lf\ninside", false},
		{"nul\x00inside", false},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			if got := ValidValue(tt.in); got != tt.want {
				t.Errorf("ValidValue(%q) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

func TestSplitList(t *testing.T) {
	tests := []struct {
		name string
		in   []string
		want []string
	}{
		{"nil", nil, nil},
		{"single", []string{"gzip"}, []string{"gzip"}},
		{"comma separated with OWS", []string{" gzip ,\tdeflate , br"}, []string{"gzip", "deflate", "br"}},
		{"multiple lines keep order", []string{"a, b", "c"}, []string{"a", "b", "c"}},
		{"empty items dropped", []string{",a,, ,b,"}, []string{"a", "b"}},
		{"only separators", []string{" , ,"}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := SplitList(tt.in); !slices.Equal(got, tt.want) {
				t.Errorf("SplitList(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestHasToken(t *testing.T) {
	h := http.Header{}
	h.Add("Vary", "Origin, Accept-Encoding")
	h.Add("Vary", "Cookie")
	tests := []struct {
		token string
		want  bool
	}{
		{"origin", true},
		{"ACCEPT-ENCODING", true},
		{"Cookie", true},
		{"Accept", false},
		{"", false},
	}
	for _, tt := range tests {
		t.Run(tt.token, func(t *testing.T) {
			if got := HasToken(h, "Vary", tt.token); got != tt.want {
				t.Errorf("HasToken(Vary, %q) = %v, want %v", tt.token, got, tt.want)
			}
		})
	}
	if HasToken(h, "Missing", "x") {
		t.Error("HasToken on an absent header = true, want false")
	}
}

func TestAddVary(t *testing.T) {
	t.Run("adds when absent", func(t *testing.T) {
		h := http.Header{}
		AddVary(h, "Origin")
		if got := h.Values("Vary"); !slices.Equal(got, []string{"Origin"}) {
			t.Errorf("Vary = %q, want [Origin]", got)
		}
	})
	t.Run("does not duplicate case-insensitively", func(t *testing.T) {
		h := http.Header{}
		h.Set("Vary", "accept-encoding, Origin")
		AddVary(h, "Accept-Encoding")
		AddVary(h, "origin")
		if got := h.Values("Vary"); !slices.Equal(got, []string{"accept-encoding, Origin"}) {
			t.Errorf("Vary = %q, want unchanged", got)
		}
	})
	t.Run("appends a new value", func(t *testing.T) {
		h := http.Header{}
		h.Set("Vary", "Cookie")
		AddVary(h, "Origin")
		if got := h.Values("Vary"); !slices.Equal(got, []string{"Cookie", "Origin"}) {
			t.Errorf("Vary = %q, want [Cookie Origin]", got)
		}
	})
	t.Run("star already covers everything", func(t *testing.T) {
		h := http.Header{}
		h.Set("Vary", "*")
		AddVary(h, "Origin")
		if got := h.Values("Vary"); !slices.Equal(got, []string{"*"}) {
			t.Errorf("Vary = %q, want [*]", got)
		}
	})
}

package authheader_test

import (
	"net/http/httptest"
	"testing"

	"github.com/paccolamano/go/auth/internal/authheader"
)

func TestCredentials(t *testing.T) {
	for _, c := range []struct {
		header  string
		set     bool
		token   string
		present bool
	}{
		{"Bearer abc", true, "abc", true},
		{"bearer abc", true, "abc", true},
		{"BEARER  abc", true, "abc", true},
		{"Bearer", true, "", true},
		{"Basic abc", true, "", false},
		{"Bearerabc", true, "", false},
		{"", false, "", false},
	} {
		r := httptest.NewRequestWithContext(t.Context(), "GET", "/", nil)
		if c.set {
			r.Header.Set("Authorization", c.header)
		}
		token, present := authheader.Credentials(r, "Bearer")
		if token != c.token || present != c.present {
			t.Errorf("Credentials(%q) = %q, %v; want %q, %v", c.header, token, present, c.token, c.present)
		}
	}
}

func TestQuote(t *testing.T) {
	if got, want := authheader.Quote(`a"b\c`), `"a\"b\\c"`; got != want {
		t.Fatalf("Quote = %s, want %s", got, want)
	}
}

func TestScheme(t *testing.T) {
	for in, want := range map[string]string{
		`Bearer error="x"`: "bearer",
		"Basic":            "basic",
		"":                 "",
	} {
		if got := authheader.Scheme(in); got != want {
			t.Errorf("Scheme(%q) = %q, want %q", in, got, want)
		}
	}
}

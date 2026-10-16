package httpheader

import (
	"strings"
	"testing"
)

func FuzzSplitList(f *testing.F) {
	for _, s := range []string{"gzip, deflate", " , ,", "a,,b", "\t x \t", ""} {
		f.Add(s, "c")
	}
	f.Fuzz(func(t *testing.T, a, b string) {
		for _, item := range SplitList([]string{a, b}) {
			if item == "" {
				t.Fatalf("SplitList returned an empty item for %q, %q", a, b)
			}
			if strings.Contains(item, ",") {
				t.Fatalf("item %q contains a comma", item)
			}
			if strings.Trim(item, " \t") != item {
				t.Fatalf("item %q has surrounding whitespace", item)
			}
		}
	})
}

func FuzzAcceptEncoding(f *testing.F) {
	for _, s := range []string{"gzip", "br;q=0.5, gzip", "*;q=0", "x-gzip", "gzip;q=abc", ";;,", ""} {
		f.Add(s)
	}
	pref := []string{"gzip", "deflate"}
	f.Fuzz(func(t *testing.T, accept string) {
		got := Negotiate([]string{accept}, pref)
		if got != "" && got != "gzip" && got != "deflate" {
			t.Fatalf("Negotiate(%q) = %q, not in the preference list", accept, got)
		}
	})
}

func FuzzEntityTagList(f *testing.F) {
	for _, s := range []string{`"a"`, `W/"a", "b"`, `*`, `"a,b"`, `W/`, `"unterminated`, ""} {
		f.Add(s, `"a"`)
	}
	f.Fuzz(func(t *testing.T, header, etag string) {
		_ = MatchIfNoneMatch([]string{header}, etag)
	})
}

package errors_test

import (
	"strings"
	"testing"

	"github.com/paccolamano/go/errors"
)

var (
	locHandler = &errors.Location{Function: "example.com/app/api.Handler", File: "example.com/app/api/handler.go", Line: 37}
	locOpen    = &errors.Location{Function: "example.com/app/config.Open", File: "example.com/app/config/open.go", Line: 35}
	locParse   = &errors.Location{Function: "example.com/app.parse", File: "example.com/app/parse.go", Line: 9}
)

func TestDetail_TextLayout(t *testing.T) {
	tests := []struct {
		name   string
		detail errors.Detail
		want   []string
	}{
		{
			"linear chain with a call stack",
			errors.Detail{Levels: []errors.Level{
				{Text: "handle request", Location: locHandler},
				{Text: "open config", Location: locOpen, Stack: &errors.Stack{Frames: []errors.Location{*locOpen, *locHandler}}},
				{Text: "file does not exist"},
			}},
			[]string{
				"handle request",
				"    at example.com/app/api.Handler (example.com/app/api/handler.go:37)",
				"open config",
				"    at example.com/app/config.Open (example.com/app/config/open.go:35)",
				"    stack:",
				"        example.com/app/config.Open (example.com/app/config/open.go:35)",
				"        example.com/app/api.Handler (example.com/app/api/handler.go:37)",
				"file does not exist",
			},
		},
		{
			"branches and placeholders",
			errors.Detail{Levels: []errors.Level{
				{Location: locHandler, Branches: [][]errors.Level{
					{{Text: "a"}},
					{{}, {Text: "open a.txt", Type: "*fs.PathError"}},
				}},
			}},
			[]string{
				"(2 errors)",
				"    at example.com/app/api.Handler (example.com/app/api/handler.go:37)",
				"    [0]",
				"        a",
				"    [1]",
				"        (no message)",
				"        open a.txt [*fs.PathError]",
			},
		},
		{
			"hostile multi-line message",
			errors.Detail{Levels: []errors.Level{
				{Text: "bad input\n    at evil.Fake (x.go:1)", Location: locParse},
			}},
			[]string{
				"bad input",
				"    |     at evil.Fake (x.go:1)",
				"    at example.com/app.parse (example.com/app/parse.go:9)",
			},
		},
		{
			"quoting",
			errors.Detail{Levels: []errors.Level{
				{Text: " leading space"},
				{Text: "tab\there"},
				{Text: "escape \x1b[31m"},
				{Text: "first\nsecond\twith tab"},
				{Text: "\nempty head"},
				{Text: `"quoted" by the author` + "\n" + `"also" quoted`},
			}},
			[]string{
				`" leading space"`,
				`"tab\there"`,
				`"escape \x1b[31m"`,
				"first",
				`    | "second\twith tab"`,
				`""`,
				"    | empty head",
				`"\"quoted\" by the author"`,
				`    | "\"also\" quoted"`,
			},
		},
		{
			"truncation markers",
			errors.Detail{Levels: []errors.Level{
				{Text: "deep", Location: locOpen, Stack: &errors.Stack{Frames: []errors.Location{*locOpen}, Truncated: true}, Truncated: true},
			}},
			[]string{
				"deep",
				"    at example.com/app/config.Open (example.com/app/config/open.go:35)",
				"    stack:",
				"        example.com/app/config.Open (example.com/app/config/open.go:35)",
				"        ... (truncated)",
				"    ... (more causes not shown: limit reached)",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got, want := tt.detail.String(), strings.Join(tt.want, "\n"); got != want {
				t.Errorf("String() =\n%s\nwant\n%s", got, want)
			}
		})
	}
}

func TestDetail_TextEmpty(t *testing.T) {
	if got := (errors.Detail{}).String(); got != "" {
		t.Errorf("empty detail = %q", got)
	}
}

// TestDetail_TextRealChain checks the text of the chain of
// TestDescribe_LinearChain, without the call-stack frames, which depend on the
// runtime and the test framework.
func TestDetail_TextRealChain(t *testing.T) {
	var lines []int
	text := normalizeLines(errors.Describe(handleRequest(&lines)).String())
	var kept []string
	for line := range strings.SplitSeq(text, "\n") {
		if !strings.HasPrefix(line, "        ") {
			kept = append(kept, line)
		}
	}
	want := []string{
		"handle request",
		"    at " + testPkg + ".handleRequest (" + testPkg + "/detail_test.go:N)",
		"load tenant 42",
		"    at " + testPkg + ".loadTenant (" + testPkg + "/detail_test.go:N)",
		"open config",
		"    at " + testPkg + ".openConfig (" + testPkg + "/detail_test.go:N)",
		"    stack:",
		"file does not exist",
	}
	if got := strings.Join(kept, "\n"); got != strings.Join(want, "\n") {
		t.Errorf("text =\n%s\nwant\n%s", got, strings.Join(want, "\n"))
	}
	if strings.HasSuffix(text, "\n") {
		t.Error("the text ends with a newline")
	}
}

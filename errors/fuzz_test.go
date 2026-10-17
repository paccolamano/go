package errors_test

import (
	stderrors "errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/paccolamano/go/errors"
)

// detailParser checks that a rendered detail follows the grammar of
// contracts/detail-format.md, and counts its entries. A successful parse shows
// that every line can be attributed to its level.
type detailParser struct {
	lines []string
	pos   int
}

var (
	locationLine = regexp.MustCompile(`^at \S+ \(\S+:\d+\)$`)
	branchLabel  = regexp.MustCompile(`^\[\d+\]$`)
)

func parseDetail(text string) (int, error) {
	if text == "" {
		return 0, nil
	}
	p := &detailParser{lines: strings.Split(text, "\n")}
	n, err := p.chain(0)
	if err != nil {
		return 0, err
	}
	if p.pos != len(p.lines) {
		return 0, fmt.Errorf("line %d not part of any entry: %q", p.pos, p.lines[p.pos])
	}
	return n, nil
}

func (p *detailParser) next() (string, bool) {
	if p.pos >= len(p.lines) {
		return "", false
	}
	return p.lines[p.pos], true
}

func atIndent(line string, indent int) (string, bool) {
	pad := strings.Repeat(" ", indent)
	if !strings.HasPrefix(line, pad) || len(line) == indent || line[indent] == ' ' {
		return "", false
	}
	return line[indent:], true
}

func (p *detailParser) chain(indent int) (int, error) {
	n := 0
	for {
		line, ok := p.next()
		if !ok {
			break
		}
		head, ok := atIndent(line, indent)
		if !ok {
			break
		}
		if strings.HasPrefix(head, `"`) {
			unquoted := head
			if i := strings.LastIndex(head, `" [`); i > 0 && strings.HasSuffix(head, "]") {
				unquoted = head[:i+1]
			}
			if _, err := strconv.Unquote(unquoted); err != nil {
				return 0, fmt.Errorf("line %d: invalid quoted head %q", p.pos, head)
			}
		}
		p.pos++
		n++
		m, err := p.body(indent)
		if err != nil {
			return 0, err
		}
		n += m
	}
	if n == 0 {
		return 0, fmt.Errorf("line %d: expected an entry at indent %d", p.pos, indent)
	}
	return n, nil
}

func (p *detailParser) body(indent int) (int, error) {
	sub := strings.Repeat(" ", indent+4)
	n, stage := 0, 0
	for {
		line, ok := p.next()
		if !ok || !strings.HasPrefix(line, sub) {
			return n, nil
		}
		rest := line[len(sub):]
		switch {
		case strings.HasPrefix(rest, "| ") && stage == 0:
			if cont := rest[2:]; strings.HasPrefix(cont, `"`) {
				if _, err := strconv.Unquote(cont); err != nil {
					return 0, fmt.Errorf("line %d: invalid quoted continuation %q", p.pos, cont)
				}
			}
			p.pos++
		case strings.HasPrefix(rest, "at ") && stage == 0:
			if !locationLine.MatchString(rest) {
				return 0, fmt.Errorf("line %d: malformed location %q", p.pos, rest)
			}
			stage = 1
			p.pos++
		case rest == "stack:" && stage <= 1:
			stage = 2
			p.pos++
			frames := 0
			for {
				frame, ok := p.next()
				if !ok {
					break
				}
				if _, ok := atIndent(frame, indent+8); !ok {
					break
				}
				frames++
				p.pos++
			}
			if frames == 0 {
				return 0, fmt.Errorf("line %d: empty stack", p.pos)
			}
		case rest == "... (more causes not shown: limit reached)" && stage <= 2:
			stage = 3
			p.pos++
		case branchLabel.MatchString(rest):
			stage = 4
			p.pos++
			m, err := p.chain(indent + 8)
			if err != nil {
				return 0, err
			}
			n += m
		default:
			return 0, fmt.Errorf("line %d: unexpected %q", p.pos, line)
		}
	}
}

func FuzzDetailText(f *testing.F) {
	f.Add("bad input\n    at evil.Fake (x.go:1)", "query", "root")
	f.Add("tab\there", "\n", "\r")
	f.Add("", "", "")
	f.Add("  leading", "ünïcode", "\x1b[31m")
	f.Add("[0]", "    [1]", "... (truncated)")
	f.Add("\xff\xfe", "    | fake", "stack:")
	f.Fuzz(func(t *testing.T, a, b, c string) {
		err := errors.Wrap(errors.Join(errors.New(a), errors.Errorf("%s: %w", b, stderrors.New(c))), a)
		d := errors.Describe(err)
		n, perr := parseDetail(d.String())
		if perr != nil {
			t.Fatalf("%v\n%s", perr, d.String())
		}
		if want := countLevels(d.Levels); n != want {
			t.Fatalf("parsed %d entries, the detail has %d levels\n%s", n, want, d.String())
		}
	})
}

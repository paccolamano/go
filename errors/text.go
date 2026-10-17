package errors

import (
	"strconv"
	"strings"
	"unicode/utf8"
)

// String renders the detail as text: one entry per level, with its own text,
// its type, its location and, on the origin, its call stack. Causes of a level
// with several causes are listed as numbered branches. Lines are separated by
// "\n" and there is no trailing newline.
//
// A text line that contains non-printable characters or starts with a double
// quote is written Go-quoted, and so is a first line that is empty or starts
// with a space, so that no message can impersonate a location, a marker or
// another level.
func (d Detail) String() string {
	var b strings.Builder
	writeChain(&b, d.Levels, 0)
	return strings.TrimSuffix(b.String(), "\n")
}

func writeChain(b *strings.Builder, levels []Level, indent int) {
	for _, l := range levels {
		writeLevel(b, l, indent)
	}
}

func writeLevel(b *strings.Builder, l Level, indent int) {
	pad := strings.Repeat(" ", indent)
	lines := []string{headPlaceholder(l)}
	if l.Text != "" {
		lines = strings.Split(l.Text, "\n")
		lines[0] = quoteIf(lines[0], lines[0] == "" || lines[0][0] == ' ')
	}
	b.WriteString(pad + lines[0])
	if l.Type != "" {
		b.WriteString(" [" + l.Type + "]")
	}
	b.WriteByte('\n')
	for _, line := range lines[1:] {
		b.WriteString(pad + "    | " + quoteIf(line, false) + "\n")
	}
	if l.Location != nil {
		b.WriteString(pad + "    at " + formatLocation(*l.Location) + "\n")
	}
	if l.Stack != nil {
		b.WriteString(pad + "    stack:\n")
		for _, f := range l.Stack.Frames {
			b.WriteString(pad + "        " + formatLocation(f) + "\n")
		}
		if l.Stack.Truncated {
			b.WriteString(pad + "        ... (truncated)\n")
		}
	}
	if l.Truncated {
		b.WriteString(pad + "    ... (more causes not shown: limit reached)\n")
	}
	for i, branch := range l.Branches {
		b.WriteString(pad + "    [" + strconv.Itoa(i) + "]\n")
		writeChain(b, branch, indent+8)
	}
}

func headPlaceholder(l Level) string {
	if len(l.Branches) > 0 {
		return "(" + strconv.Itoa(len(l.Branches)) + " errors)"
	}
	return "(no message)"
}

// quoteIf returns line Go-quoted when force is true, when it starts with a
// double quote, or when it contains invalid UTF-8 or a character that is not
// printable.
func quoteIf(line string, force bool) string {
	if force || strings.HasPrefix(line, `"`) || !utf8.ValidString(line) ||
		strings.IndexFunc(line, func(r rune) bool { return !strconv.IsPrint(r) }) >= 0 {
		return strconv.Quote(line)
	}
	return line
}

func formatLocation(l Location) string {
	return l.Function + " (" + l.File + ":" + strconv.Itoa(l.Line) + ")"
}

package errors

import (
	"fmt"
	"reflect"
	"strings"
)

// Detail describes an error tree. Describe builds it, String renders it as
// text, and encoding/json serializes it with the documented field names.
type Detail struct {
	// Levels is the outermost chain: from the outermost error down to the root
	// cause, or to the first error with several causes.
	Levels []Level `json:"levels"`
}

// Level describes one error of a tree.
type Level struct {
	// Text is the part of the message that this error adds to the messages of
	// its causes. It is empty when the error adds nothing.
	Text string `json:"text"`
	// Type is the error's type, empty for the types of this package and for the
	// generic types of the standard library (errors.New, fmt.Errorf, errors.Join).
	Type string `json:"type,omitempty"`
	// Location is where the error was created, wrapped or combined, when known.
	Location *Location `json:"location,omitempty"`
	// Stack is the call stack that led to the origin of the chain. Only the
	// origin has one.
	Stack *Stack `json:"stack,omitempty"`
	// Branches holds one chain per cause, in order, when the error has several.
	Branches [][]Level `json:"branches,omitempty"`
	// Truncated reports that some causes were not described because a limit
	// was reached.
	Truncated bool `json:"truncated,omitempty"`
}

// Location identifies a line of source code.
type Location struct {
	// Function is the fully qualified function name, such as
	// example.com/app/api.(*Server).Handle.
	Function string `json:"function"`
	// File is the package import path followed by the file name, such as
	// example.com/app/api/handler.go. It never contains the build directory.
	File string `json:"file"`
	// Line is the line number, or 0 when it is not known.
	Line int `json:"line"`
}

// Stack is a call stack, most recent call first.
type Stack struct {
	// Frames holds at most 32 locations.
	Frames []Location `json:"frames"`
	// Truncated reports that the call stack was deeper than 32 frames.
	Truncated bool `json:"truncated,omitempty"`
}

// Describe returns the detail of err, including errors created by other
// packages. Describe(nil) returns a Detail with no levels.
//
// Describe follows at most 64 levels along any path of the tree and describes
// at most 256 levels in total; a level whose causes are not described has
// Truncated set. It never panics, even when an Error or Unwrap method does.
func Describe(err error) Detail {
	d := describer{budget: maxDetailLevels}
	levels, _ := d.chain(err, 0)
	return Detail{Levels: levels}
}

type describer struct {
	budget int
}

// chain describes err and the single causes below it. It reports true when a
// limit stopped it before it could describe any level.
func (d *describer) chain(err error, depth int) ([]Level, bool) {
	levels := []Level{}
	var (
		pending       bool
		pendingPC     uintptr
		pendingOrigin *callStack
	)
	for err != nil {
		if depth >= maxDetailDepth || d.budget <= 0 {
			if n := len(levels); n > 0 {
				levels[n-1].Truncated = true
				return levels, false
			}
			return levels, true
		}
		depth++
		in := inspect(err)
		if in.annotation {
			if in.pc == 0 {
				err = in.causes[0]
				continue
			}
			if !pending && !hasLocation(in.causes[0]) {
				pending, pendingPC, pendingOrigin = true, in.pc, in.origin
				err = in.causes[0]
				continue
			}
		}
		pc, origin := in.pc, in.origin
		if pending {
			pc, origin, pending = pendingPC, pendingOrigin, false
		}
		level := Level{Text: in.text, Type: in.typ, Location: resolve(pc), Stack: resolveStack(origin)}
		d.budget--
		if len(in.causes) <= 1 {
			levels = append(levels, level)
			if len(in.causes) == 0 {
				break
			}
			err = in.causes[0]
			continue
		}
		for _, c := range in.causes {
			if depth >= maxDetailDepth || d.budget <= 0 {
				level.Truncated = true
				break
			}
			branch, cut := d.chain(c, depth)
			if len(branch) > 0 {
				level.Branches = append(level.Branches, branch)
			}
			if cut {
				level.Truncated = true
				break
			}
		}
		levels = append(levels, level)
		break
	}
	return levels, false
}

// inspected is what Describe needs to know about one error.
type inspected struct {
	text       string
	typ        string
	causes     []error
	pc         uintptr
	origin     *callStack
	annotation bool
}

func inspect(err error) inspected {
	var in inspected
	in.typ = typeName(err)
	t, isTracer := err.(tracer)
	if isTracer {
		in.pc, in.origin = t.trace()
	}
	msg, panicked := safeMessage(err)
	if panicked {
		in.text = msg
		return in
	}
	in.causes, _ = safeCauses(err)
	in.text = ownText(msg, in.causes)
	in.annotation = isTracer && in.typ == "" && in.text == "" && len(in.causes) == 1
	return in
}

// ownText returns the part of msg that an error adds to the messages of its
// causes.
func ownText(msg string, causes []error) string {
	switch len(causes) {
	case 0:
		return msg
	case 1:
		c := messageOf(causes[0])
		if msg == c {
			return ""
		}
		if text, ok := strings.CutSuffix(msg, ": "+c); ok {
			return text
		}
		return msg
	default:
		msgs := make([]string, len(causes))
		for i, c := range causes {
			msgs[i] = messageOf(c)
		}
		if msg == strings.Join(msgs, "\n") {
			return ""
		}
		return msg
	}
}

func hasLocation(err error) bool {
	t, ok := err.(tracer)
	if !ok {
		return false
	}
	pc, _ := t.trace()
	return pc != 0
}

// typeName returns the type of err, or "" for the types of this package and
// for the generic error types of the standard library.
func typeName(err error) string {
	t := reflect.TypeOf(err)
	base := t
	if base.Kind() == reflect.Pointer {
		base = base.Elem()
	}
	switch base.PkgPath() {
	case reflect.TypeFor[wrapError]().PkgPath():
		return ""
	case "errors":
		if name := base.Name(); name == "errorString" || name == "joinError" {
			return ""
		}
	case "fmt":
		if name := base.Name(); name == "wrapError" || name == "wrapErrors" {
			return ""
		}
	}
	return t.String()
}

// safeMessage returns err.Error(). If the Error method panics, it returns the
// placeholder that fmt prints in that case, and true.
func safeMessage(err error) (msg string, panicked bool) {
	defer func() {
		if recover() != nil {
			msg, panicked = fmt.Sprintf("%v", err), true
		}
	}()
	return err.Error(), false
}

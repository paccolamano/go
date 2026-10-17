package errors_test

import (
	"encoding/json"
	stderrors "errors"
	"fmt"
	"io/fs"
	"reflect"
	"slices"
	"strings"
	"sync"
	"syscall"
	"testing"

	"github.com/paccolamano/go/errors"
)

const testPkg = "github.com/paccolamano/go/errors_test"

// The chain of User Story 2, scenario 1, built across three functions. Each
// function appends the line of its call to lines.
func openConfig(lines *[]int) error {
	err, line := errors.Wrap(fs.ErrNotExist, "open config"), here()
	*lines = append(*lines, line)
	return err
}

func loadTenant(lines *[]int) error {
	err := openConfig(lines)
	wrapped, line := errors.Wrapf(err, "load tenant %d", 42), here()
	*lines = append(*lines, line)
	return wrapped
}

func handleRequest(lines *[]int) error {
	err := loadTenant(lines)
	wrapped, line := errors.Wrap(err, "handle request"), here()
	*lines = append(*lines, line)
	return wrapped
}

func texts(levels []errors.Level) []string {
	out := make([]string, len(levels))
	for i, l := range levels {
		out[i] = l.Text
	}
	return out
}

func countLevels(levels []errors.Level) int {
	n := len(levels)
	for _, l := range levels {
		for _, b := range l.Branches {
			n += countLevels(b)
		}
	}
	return n
}

func TestDescribe_LinearChain(t *testing.T) {
	var lines []int
	err := handleRequest(&lines)
	d := errors.Describe(err)
	if got, want := texts(d.Levels), []string{"handle request", "load tenant 42", "open config", "file does not exist"}; !slices.Equal(got, want) {
		t.Fatalf("texts = %q, want %q", got, want)
	}
	wantLoc := []errors.Location{
		{Function: testPkg + ".handleRequest", File: testPkg + "/detail_test.go", Line: lines[2]},
		{Function: testPkg + ".loadTenant", File: testPkg + "/detail_test.go", Line: lines[1]},
		{Function: testPkg + ".openConfig", File: testPkg + "/detail_test.go", Line: lines[0]},
	}
	for i, want := range wantLoc {
		l := d.Levels[i]
		if l.Location == nil || *l.Location != want {
			t.Errorf("level %d location = %+v, want %+v", i, l.Location, want)
		}
		if (l.Stack != nil) != (i == 2) {
			t.Errorf("level %d: stack present = %v, want only on the origin", i, l.Stack != nil)
		}
	}
	origin := d.Levels[2]
	if origin.Stack == nil || len(origin.Stack.Frames) == 0 || origin.Stack.Frames[0] != *origin.Location {
		t.Fatalf("origin stack does not start at its location: %+v", origin.Stack)
	}
	if root := d.Levels[3]; root.Location != nil || root.Type != "" || root.Stack != nil {
		t.Errorf("root level = %+v, want text only", root)
	}
}

type rewriterError struct{ cause error }

func (e rewriterError) Error() string { return "rewritten" }
func (e rewriterError) Unwrap() error { return e.cause }

type passthroughError struct{ cause error }

func (e passthroughError) Error() string { return e.cause.Error() }
func (e passthroughError) Unwrap() error { return e.cause }

func TestDescribe_OwnText(t *testing.T) {
	a, b := stderrors.New("a"), stderrors.New("b")
	tests := []struct {
		name string
		err  error
		want string
	}{
		{"library wrap", errors.Wrap(a, "ctx"), "ctx"},
		{"fmt wrap", fmt.Errorf("q: %w", a), "q"},
		{"foreign rewrites the message", rewriterError{a}, "rewritten"},
		{"foreign pass-through", passthroughError{a}, ""},
		{"join", stderrors.Join(a, b), ""},
		{"Errorf with two causes", errors.Errorf("%w + %w", a, b), "a + b"},
		{"no cause", errors.New("plain"), "plain"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := errors.Describe(tt.err).Levels[0].Text; got != tt.want {
				t.Errorf("own text = %q, want %q", got, tt.want)
			}
		})
	}
}

type thirdPartyError struct{}

func (*thirdPartyError) Error() string { return "third party" }

// joinError has the name of a standard library type, in another package.
type joinError struct{}

func (*joinError) Error() string { return "local joinError" }

func TestDescribe_Types(t *testing.T) {
	a := stderrors.New("a")
	tests := []struct {
		name string
		err  error
		want string
	}{
		{"path error", &fs.PathError{Op: "open", Path: "a.txt", Err: syscall.EACCES}, "*fs.PathError"},
		{"errno", syscall.EACCES, "syscall.Errno"},
		{"third party", &thirdPartyError{}, "*errors_test.thirdPartyError"},
		{"same name, other package", &joinError{}, "*errors_test.joinError"},
		{"standard New", a, ""},
		{"fmt one cause", fmt.Errorf("x: %w", a), ""},
		{"fmt two causes", fmt.Errorf("%w %w", a, a), ""},
		{"standard Join", stderrors.Join(a), ""},
		{"library New", errors.New("x"), ""},
		{"library Join", errors.Join(a), ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := errors.Describe(tt.err).Levels[0].Type; got != tt.want {
				t.Errorf("type = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestDescribe_ForeignLevel(t *testing.T) {
	base := stderrors.New("base")
	inner := errors.Wrap(base, "inner")
	outer := errors.Wrap(fmt.Errorf("query: %w", inner), "fetch")
	d := errors.Describe(outer)
	if got, want := texts(d.Levels), []string{"fetch", "query", "inner", "base"}; !slices.Equal(got, want) {
		t.Fatalf("texts = %q, want %q", got, want)
	}
	for i, wantLoc := range []bool{true, false, true, false} {
		if got := d.Levels[i].Location != nil; got != wantLoc {
			t.Errorf("level %d has location = %v, want %v", i, got, wantLoc)
		}
	}
	if d.Levels[0].Stack != nil || d.Levels[2].Stack == nil {
		t.Error("the call stack must be on the origin (inner) only")
	}
}

func TestDescribe_Annotations(t *testing.T) {
	t.Run("merged with a cause without location", func(t *testing.T) {
		pathErr := &fs.PathError{Op: "open", Path: "a.txt", Err: syscall.EACCES}
		err, line := errors.WithStack(pathErr), callerLine(t)
		d := errors.Describe(err)
		if len(d.Levels) != 2 {
			t.Fatalf("levels = %d, want 2: %+v", len(d.Levels), d.Levels)
		}
		merged := d.Levels[0]
		if merged.Text != "open a.txt" || merged.Type != "*fs.PathError" || merged.Location == nil || merged.Location.Line != line || merged.Stack == nil {
			t.Errorf("merged level = %+v", merged)
		}
		if errno := d.Levels[1]; errno.Text != syscall.EACCES.Error() || errno.Type != "syscall.Errno" {
			t.Errorf("errno level = %+v", errno)
		}
	})
	t.Run("kept over a cause with location", func(t *testing.T) {
		lib := errors.New("lib")
		d := errors.Describe(errors.Wrap(lib, ""))
		if got := texts(d.Levels); !slices.Equal(got, []string{"", "lib"}) {
			t.Fatalf("texts = %q", got)
		}
		if d.Levels[0].Location == nil || d.Levels[0].Stack != nil {
			t.Errorf("annotation level = %+v, want a location and no stack", d.Levels[0])
		}
	})
}

func TestDescribe_Branches(t *testing.T) {
	a, b := errors.New("a"), errors.New("b")
	d := errors.Describe(errors.Wrap(errors.Join(a, b), "sync"))
	if got := texts(d.Levels); !slices.Equal(got, []string{"sync", ""}) {
		t.Fatalf("texts = %q", got)
	}
	join := d.Levels[1]
	if len(join.Branches) != 2 || join.Location == nil || join.Stack != nil {
		t.Fatalf("join level = %+v", join)
	}
	if !slices.Equal(texts(join.Branches[0]), []string{"a"}) || !slices.Equal(texts(join.Branches[1]), []string{"b"}) {
		t.Errorf("branches = %+v", join.Branches)
	}

	shared := errors.New("shared")
	d = errors.Describe(errors.Join(errors.Wrap(shared, "x"), errors.Wrap(shared, "y")))
	for i, br := range d.Levels[0].Branches {
		if got := texts(br); got[len(got)-1] != "shared" {
			t.Errorf("branch %d = %q, want it to end with the shared cause", i, got)
		}
	}
}

func workerOrigin() error { return errors.New("worker failed") }

func TestDescribe_ConcurrentTasks(t *testing.T) {
	ch := make(chan error)
	go func() { ch <- workerOrigin() }()
	origin := <-ch
	d := errors.Describe(errors.Wrap(origin, "collect"))
	if loc := d.Levels[0].Location; loc == nil || loc.Function != testPkg+".TestDescribe_ConcurrentTasks" {
		t.Errorf("wrap location = %+v", loc)
	}
	stack := d.Levels[1].Stack
	if stack == nil || stack.Frames[0].Function != testPkg+".workerOrigin" {
		t.Fatalf("origin stack = %+v", stack)
	}
	for _, f := range stack.Frames {
		if f.Function == testPkg+".TestDescribe_ConcurrentTasks" {
			t.Errorf("the origin stack contains the test goroutine's frame %+v", f)
		}
	}
}

func deepOrigin(n int) error {
	if n == 0 {
		return errors.New("deep")
	}
	return deepOrigin(n - 1)
}

func TestDescribe_StackTruncated(t *testing.T) {
	stack := errors.Describe(deepOrigin(40)).Levels[0].Stack
	if stack == nil || len(stack.Frames) != 32 || !stack.Truncated {
		t.Fatalf("stack = %d frames, truncated = %v; want 32, true", len(stack.Frames), stack != nil && stack.Truncated)
	}
}

type selfRefError struct{}

func (selfRefError) Error() string   { return "self" }
func (e selfRefError) Unwrap() error { return e }

type linkedError struct{ next *linkedError }

func (*linkedError) Error() string { return "link" }

func (e *linkedError) Unwrap() error {
	if e.next == nil {
		return nil
	}
	return e.next
}

func linkedChain(n int) error {
	head := &linkedError{}
	for range n - 1 {
		head = &linkedError{next: head}
	}
	return head
}

func TestDescribe_Pathological(t *testing.T) {
	t.Run("self-referencing", func(t *testing.T) {
		levels := errors.Describe(selfRefError{}).Levels
		if len(levels) == 0 || len(levels) > 64 || !levels[len(levels)-1].Truncated {
			t.Errorf("levels = %d, last truncated = %v", len(levels), len(levels) > 0 && levels[len(levels)-1].Truncated)
		}
	})
	t.Run("10000 levels", func(t *testing.T) {
		levels := errors.Describe(linkedChain(10000)).Levels
		if len(levels) > 64 || !levels[len(levels)-1].Truncated {
			t.Errorf("levels = %d, last truncated = %v", len(levels), levels[len(levels)-1].Truncated)
		}
	})
	t.Run("1000 causes", func(t *testing.T) {
		errs := make([]error, 1000)
		for i := range errs {
			errs[i] = stderrors.New(fmt.Sprint("cause ", i))
		}
		levels := errors.Describe(stderrors.Join(errs...)).Levels
		if n := countLevels(levels); n > 256 {
			t.Errorf("total levels = %d, want at most 256", n)
		}
		if !levels[0].Truncated {
			t.Error("the join level is not marked truncated")
		}
	})
}

type panickyError struct{}

func (panickyError) Error() string { panic("boom") }

type nilReceiverError struct{ msg *string }

func (e *nilReceiverError) Error() string { return *e.msg }

type panicUnwrapError struct{}

func (panicUnwrapError) Error() string { return "unwrap panics" }
func (panicUnwrapError) Unwrap() error { panic("unwrap boom") }

func TestDescribe_PanickingMethods(t *testing.T) {
	var nilPtr *nilReceiverError
	tests := []struct {
		name string
		err  error
		want string
	}{
		{"Error panics", panickyError{}, "%!v(PANIC=Error method: boom)"},
		{"nil pointer receiver", nilPtr, "<nil>"},
		{"Unwrap panics", panicUnwrapError{}, "unwrap panics"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			levels := errors.Describe(errors.Wrap(tt.err, "ctx")).Levels
			if len(levels) != 2 || levels[1].Text != tt.want {
				t.Errorf("levels = %+v, want the second text to be %q", levels, tt.want)
			}
		})
	}
}

func TestDescribe_Nil(t *testing.T) {
	d := errors.Describe(nil)
	if d.Levels == nil || len(d.Levels) != 0 || d.String() != "" {
		t.Errorf("Describe(nil) = %#v, %q", d, d.String())
	}
}

type pkgStackError struct{}

func (pkgStackError) Error() string         { return "with its own stack" }
func (pkgStackError) StackTrace() []uintptr { return []uintptr{1, 2, 3} }

func TestDescribe_ForeignStackTrace(t *testing.T) {
	l := errors.Describe(pkgStackError{}).Levels[0]
	if l.Text != "with its own stack" || l.Type != "errors_test.pkgStackError" || l.Location != nil || l.Stack != nil {
		t.Errorf("level = %+v", l)
	}
}

func TestDescribe_Concurrent(t *testing.T) {
	var lines []int
	err := handleRequest(&lines)
	wantDetail, wantText := errors.Describe(err), fmt.Sprintf("%+v", err)
	var wg sync.WaitGroup
	for range 16 {
		wg.Go(func() {
			if got := errors.Describe(err); !reflect.DeepEqual(got, wantDetail) {
				t.Error("concurrent Describe differs")
			}
			if got := fmt.Sprintf("%+v", err); got != wantText {
				t.Error("concurrent detail verb differs")
			}
			if err.Error() != "handle request: load tenant 42: open config: file does not exist" {
				t.Error("concurrent Error differs")
			}
		})
	}
	wg.Wait()
}

func keys(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}

func TestDetail_JSON(t *testing.T) {
	var lines []int
	chain := errors.Join(handleRequest(&lines), &fs.PathError{Op: "open", Path: "a", Err: syscall.EACCES})
	d := errors.Describe(errors.Wrap(chain, "outer"))
	data, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	if got := keys(raw); !slices.Equal(got, []string{"levels"}) {
		t.Errorf("top-level keys = %v", got)
	}
	allowed := map[string]bool{"text": true, "type": true, "location": true, "stack": true, "branches": true, "truncated": true}
	var walk func(levels []any)
	walk = func(levels []any) {
		for _, l := range levels {
			level := l.(map[string]any)
			for k := range level {
				if !allowed[k] {
					t.Errorf("unexpected level key %q", k)
				}
			}
			if loc, ok := level["location"].(map[string]any); ok {
				if got := keys(loc); !slices.Equal(got, []string{"file", "function", "line"}) {
					t.Errorf("location keys = %v", got)
				}
			}
			if st, ok := level["stack"].(map[string]any); ok {
				for k := range st {
					if k != "frames" && k != "truncated" {
						t.Errorf("unexpected stack key %q", k)
					}
				}
			}
			if branches, ok := level["branches"].([]any); ok {
				for _, b := range branches {
					walk(b.([]any))
				}
			}
		}
	}
	walk(raw["levels"].([]any))

	var back errors.Detail
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(back, d) {
		t.Errorf("round trip differs:\n got %+v\nwant %+v", back, d)
	}
	if nilJSON, _ := json.Marshal(errors.Describe(nil)); string(nilJSON) != `{"levels":[]}` {
		t.Errorf("nil detail JSON = %s", nilJSON)
	}
	if strings.Contains(string(data), "/home/") {
		t.Error("the JSON contains a build-machine path")
	}
}

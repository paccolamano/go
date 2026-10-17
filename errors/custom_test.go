package errors_test

import (
	stderrors "errors"
	"fmt"
	"strings"
	"testing"

	"github.com/paccolamano/go/errors"
)

// ConnError is a custom error type with one embedded field and a constructor,
// and no hand-written methods (SC-007).
type ConnError struct {
	errors.BaseError
	Addr string
}

// NewConnError returns a ConnError located at its caller.
func NewConnError(addr string, cause error) *ConnError {
	return errors.Bind(&ConnError{Addr: addr}, cause, "connect "+addr)
}

func newConnErrorAt(level int, addr string, cause error) error {
	return errors.Skip(level).Bind(&ConnError{Addr: addr}, cause, "connect "+addr)
}

func connErrorAtTwo() error { return newConnErrorAt(2, "db", nil) }

// OverrideError defines its own Error method.
type OverrideError struct {
	errors.BaseError
	Code int
}

func (e *OverrideError) Error() string { return fmt.Sprintf("code %d", e.Code) }

// NewOverrideError returns an OverrideError without a cause.
func NewOverrideError(code int) *OverrideError {
	return errors.Bind(&OverrideError{Code: code}, nil, "ignored")
}

func TestCustom_AsTypeThroughWraps(t *testing.T) {
	root := stderrors.New("root")
	ce := NewConnError("db:5432", root)
	err := errors.Wrap(errors.Wrap(ce, "a"), "b")
	if got, ok := errors.AsType[*ConnError](err); !ok || got != ce || got.Addr != "db:5432" {
		t.Errorf("AsType = %v, %v", got, ok)
	}
	var target *ConnError
	if !errors.As(err, &target) || target != ce {
		t.Error("As did not find the custom error")
	}
	if !errors.Is(ce, root) {
		t.Error("the cause of the custom error is not found")
	}
}

func TestCustom_Location(t *testing.T) {
	ce, line := NewConnError("db", nil), callerLine(t)
	checkLocation(t, ce, "TestCustom_Location", line)
	if typ := errors.Describe(ce).Levels[0].Type; typ != "*errors_test.ConnError" {
		t.Errorf("type = %q", typ)
	}
}

func TestCustom_SkipLevels(t *testing.T) {
	l := errors.Describe(newConnErrorAt(0, "db", nil)).Levels[0]
	if l.Location == nil || l.Location.Function != testPkg+".newConnErrorAt" {
		t.Errorf("Skip(0).Bind location = %+v, want inside the constructor", l.Location)
	}
	err, line := newConnErrorAt(1, "db", nil), callerLine(t)
	checkLocation(t, err, "TestCustom_SkipLevels", line)
	err, line = connErrorAtTwo(), callerLine(t)
	checkLocation(t, err, "TestCustom_SkipLevels", line)
}

func TestCustom_Message(t *testing.T) {
	if got := NewConnError("db", nil).Error(); got != "connect db" {
		t.Errorf("without cause: %q", got)
	}
	if got := NewConnError("db", stderrors.New("root")).Error(); got != "connect db: root" {
		t.Errorf("with cause: %q", got)
	}
}

func TestCustom_Override(t *testing.T) {
	oe := NewOverrideError(7)
	if got := fmt.Sprintf("%v|%s|%q", oe, oe, oe); got != `code 7|code 7|"code 7"` {
		t.Errorf("formatted = %s", got)
	}
	if first, _, _ := strings.Cut(fmt.Sprintf("%+v", oe), "\n"); first != "code 7 [*errors_test.OverrideError]" {
		t.Errorf("first detail line = %q", first)
	}
}

func TestCustom_DetailVerb(t *testing.T) {
	ce := NewConnError("db", stderrors.New("root"))
	got := fmt.Sprintf("%+v", ce)
	if got != errors.Describe(ce).String() || !strings.Contains(got, "[*errors_test.ConnError]") {
		t.Errorf("%%+v =\n%s", got)
	}
}

func TestCustom_OriginRule(t *testing.T) {
	if errors.Describe(NewConnError("a", stderrors.New("x"))).Levels[0].Stack == nil {
		t.Error("a custom error over an untraced cause has no call stack")
	}
	if errors.Describe(NewConnError("a", errors.New("traced"))).Levels[0].Stack != nil {
		t.Error("a custom error over a traced cause has a second call stack")
	}
}

func TestCustom_ZeroBase(t *testing.T) {
	zero := &ConnError{Addr: "db"}
	if zero.Error() != "" {
		t.Errorf("Error() = %q", zero.Error())
	}
	if errors.Describe(zero).Levels[0].Location != nil {
		t.Error("an unbound custom error has a location")
	}
	_ = fmt.Sprintf("%v %+v", zero, zero)
}

package errors_test

import (
	"fmt"
	"io/fs"
	"log/slog"
	"os"

	"github.com/paccolamano/go/errors"
)

func ExampleWrap() {
	err := errors.Wrap(fs.ErrNotExist, "open config")
	err = errors.Wrapf(err, "load tenant %d", 42)

	fmt.Println(err)
	fmt.Println(errors.Is(err, fs.ErrNotExist))
	// Output:
	// load tenant 42: open config: file does not exist
	// true
}

func ExampleNew() {
	err := errors.New("connection refused")

	fmt.Println(err)
	fmt.Println(errors.Describe(err).Levels[0].Location != nil)
	// Output:
	// connection refused
	// true
}

func ExampleWrapf() {
	err := errors.Wrapf(fs.ErrPermission, "write %s", "report.csv")

	fmt.Println(err)
	// Output:
	// write report.csv: permission denied
}

// WithStack adds a location to an error returned by another package, without
// changing its message; the detail shows them as a single level.
func ExampleWithStack() {
	err := errors.WithStack(&fs.PathError{Op: "open", Path: "config.yaml", Err: fs.ErrNotExist})

	fmt.Println(err)
	level := errors.Describe(err).Levels[0]
	fmt.Println(level.Text, level.Type, level.Location != nil)
	// Output:
	// open config.yaml: file does not exist
	// open config.yaml *fs.PathError true
}

func ExampleErrorf() {
	err := errors.Errorf("read %s: %w", "config.yaml", fs.ErrPermission)

	fmt.Println(err)
	fmt.Println(errors.Is(err, fs.ErrPermission))
	// Output:
	// read config.yaml: permission denied
	// true
}

func ExampleJoin() {
	err := errors.Join(errors.New("upload a.txt"), nil, errors.New("upload b.txt"))

	fmt.Println(err)
	// Output:
	// upload a.txt
	// upload b.txt
}

func ExampleDescribe() {
	err := errors.Wrap(errors.Wrap(fs.ErrNotExist, "open config"), "load tenant")

	for _, level := range errors.Describe(err).Levels {
		fmt.Printf("%q has location: %v, has call stack: %v\n", level.Text, level.Location != nil, level.Stack != nil)
	}
	// Output:
	// "load tenant" has location: true, has call stack: false
	// "open config" has location: true, has call stack: true
	// "file does not exist" has location: false, has call stack: false
}

// ConnectionError is a custom error type: it embeds BaseError, so it has a
// message, a cause, a location and a detail without hand-written methods.
type ConnectionError struct {
	errors.BaseError
	Addr string
}

// NewConnectionError locates the error at the caller of the constructor.
func NewConnectionError(addr string, cause error) *ConnectionError {
	return errors.Bind(&ConnectionError{Addr: addr}, cause, "connect "+addr)
}

func ExampleBind() {
	err := errors.Wrap(NewConnectionError("db:5432", os.ErrDeadlineExceeded), "load users")

	fmt.Println(err)
	if connErr, ok := errors.AsType[*ConnectionError](err); ok {
		fmt.Println("retry", connErr.Addr)
	}
	// Output:
	// load users: connect db:5432: i/o timeout
	// retry db:5432
}

var errValidation = errors.New("validation failed")

// invalid builds errors on behalf of its caller, so the location of the error
// is the line that calls invalid.
func invalid(field string) error {
	return errors.Skip(1).Errorf("invalid %s: %w", field, errValidation)
}

func ExampleSkip() {
	err := invalid("email")

	fmt.Println(err)
	fmt.Println(errors.Is(err, errValidation))
	fmt.Println(errors.Describe(err).Levels[0].Location.Function)
	// Output:
	// invalid email: validation failed
	// true
	// github.com/paccolamano/go/errors_test.ExampleSkip
}

func ExampleAttr() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		ReplaceAttr: func(_ []string, a slog.Attr) slog.Attr {
			if a.Key == slog.TimeKey {
				return slog.Attr{}
			}
			return a
		},
	}))

	logger.Error("open failed", errors.Attr(fs.ErrNotExist))
	// Output:
	// {"level":"ERROR","msg":"open failed","error":{"message":"file does not exist","detail":{"levels":[{"text":"file does not exist"}]}}}
}

// ErrNotFound is a sentinel error. Errors created while packages are being
// initialized carry no location, so a sentinel shows only its message.
var ErrNotFound = errors.New("not found")

func Example_sentinel() {
	err := errors.Wrap(ErrNotFound, "get user")

	for _, level := range errors.Describe(err).Levels {
		fmt.Println(level.Text, level.Location != nil)
	}
	fmt.Println(errors.Is(err, ErrNotFound))
	// Output:
	// get user true
	// not found false
	// true
}

func ExampleDetailValue() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		ReplaceAttr: func(_ []string, a slog.Attr) slog.Attr {
			if a.Key == slog.TimeKey {
				return slog.Attr{}
			}
			return a
		},
	}))

	logger.Error("open failed", slog.Any("err", errors.DetailValue(fs.ErrNotExist)))
	// Output:
	// {"level":"ERROR","msg":"open failed","err":{"message":"file does not exist","detail":{"levels":[{"text":"file does not exist"}]}}}
}

func ExampleAsType() {
	err := errors.Wrap(&fs.PathError{Op: "open", Path: "config.yaml", Err: fs.ErrNotExist}, "load")

	if pathErr, ok := errors.AsType[*fs.PathError](err); ok {
		fmt.Println(pathErr.Path)
	}
	// Output:
	// config.yaml
}

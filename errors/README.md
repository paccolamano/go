# errors

A drop-in replacement for the standard `errors` package that records where errors are created
and wrapped, and renders readable and structured error details. The module has no dependency
outside the standard library.

```bash
go get github.com/paccolamano/go/errors
```

Minimum Go version: 1.26

## Usage

```go
package main

import (
	"fmt"
	"os"

	"github.com/paccolamano/go/errors"
)

var ErrNotFound = errors.New("not found")

func openConfig() error {
	if _, err := os.Open("config.yaml"); err != nil {
		return errors.Wrap(err, "open config")
	}
	return nil
}

func loadTenant(id int) error {
	return errors.Wrapf(openConfig(), "load tenant %d", id)
}

func main() {
	err := loadTenant(42)
	fmt.Println(err)
	fmt.Printf("%+v\n", err)
}
```

`fmt.Println` prints the same message as the standard library would:

```text
load tenant 42: open config: open config.yaml: no such file or directory
```

`%+v` prints the detail:

```text
load tenant 42
    at main.loadTenant (main/main.go:20)
open config
    at main.openConfig (main/main.go:14)
    stack:
        main.openConfig (main/main.go:14)
        main.loadTenant (main/main.go:20)
        main.main (main/main.go:24)
        runtime.main (runtime/proc.go:302)
        runtime.goexit (runtime/asm_amd64.s:1264)
open config.yaml [*fs.PathError]
no such file or directory [syscall.Errno]
```

Each level shows only the text it adds, where it happened, and its type when the type is
meaningful. The call stack appears once, at the origin of the chain. Errors created while
packages are initialized, such as `ErrNotFound` above, carry no location.

## Migrating from the standard errors package

Replace the import. `Is`, `As`, `AsType`, `Unwrap`, `Join`, `New` and `ErrUnsupported` behave
exactly like the standard ones, and messages built with `Wrap` are identical to those built
with `fmt.Errorf("...: %w", err)`. `ErrUnsupported` is the standard library's own value.

The package adds:

- `Wrap`, `Wrapf`, `WithStack` and `Errorf`, to add context;
- `Skip`, for helpers that build errors on behalf of their caller;
- `BaseError` and `Bind`, for custom error types;
- `Describe`, for the detail.

## Error detail

- `fmt.Sprintf("%+v", err)` and `errors.Describe(err).String()` return the text above. Every
  other verb formats the message exactly like a standard error.
- `errors.Describe(err)` returns the same information as data. It serializes to JSON with
  stable field names: `levels`, then `text`, `type`, `location` (`function`, `file`, `line`),
  `stack` (`frames`, `truncated`), `branches` and `truncated` for each level.
- Files are named by package import path (`example.com/app/api/handler.go`), never by the
  directory where the program was built.

## Logging

```go
logger.Error("request failed", errors.Attr(err))
```

`Attr` adds an `error` attribute with the message and the detail, computed only when the record
is emitted. Use `slog.Any("err", errors.DetailValue(err))` for another key. Errors logged as
plain values (`"err", err`) appear exactly like standard errors.

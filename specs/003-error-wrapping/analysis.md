# Reference Analysis: sorintlab/errors

**Feature**: [spec.md](spec.md)

**Created**: 2026-10-16

**Subject**: [github.com/sorintlab/errors](https://github.com/sorintlab/errors) at commit `3d76026`
(2025-06-03), Apache-2.0 license.

**Scope**: the error wrapping and error detail features. They are implemented in five files:
`errors.go`, `wrapper.go`, `stack.go`, `format.go` and `go113.go` (569 lines).

## Method

1. The source was read in full. The repository has six commits and no test files.
2. A probe program, kept outside this repository, exercised the library through its public API:
   - message composition;
   - formatting verbs;
   - detail output for linear chains, joined errors and chains with standard-library levels;
   - sentinel errors;
   - caller-skip options;
   - deep stacks;
   - an `Unwrap` cycle.
3. Benchmarks compared the library with the standard library on Go 1.27.0, linux/amd64, on an
   Intel i7-11800H. Each error was created at a call depth of 10, and each benchmark ran 3
   times.

## What the library provides

| Capability | API |
|------------|-----|
| Create with location | `New`, `Errorf` |
| Wrap with location | `Wrap`, `Wrapf`, `WithStack` (location only, no message) |
| Combine | `Join` (records a location) |
| Inspect | `Is`, `As`, `Unwrap` (forwarders to the standard library) |
| Custom typed errors | `WrapperError`, embedded in a user type, with the options `WithWrapperErrorMsg`, `WithWrapperErrorMsgf` and `WithWrapperErrorCallerDepth` |
| Detail | `PrintErrorDetails(err) []string` |
| Stack primitives | `Callers`, `Stack`, `StackTrace`, `Frame` (all exported) |

Every call to `New`, `Errorf`, `Wrap`, `Wrapf`, `WithStack`, `Join` or `NewWrapperError` captures
up to 32 program counters. The detail walks the error tree. For each node it prints the Go type,
the first frame of the node's stack and the node's full message.

## Strengths to keep

| # | Strength | Evidence | Spec |
|---|----------|----------|------|
| S1 | Familiar vocabulary (`New`, `Errorf`, `Wrap`, `Wrapf`, `WithStack`), the same as `github.com/pkg/errors`. Developers learn it quickly and existing code migrates easily. | API | FR-006, FR-007, FR-009 to FR-011 |
| S2 | Interoperates with the standard library. Every type implements `Unwrap() error` or `Unwrap() []error`. The detail also walks through levels built with `fmt.Errorf("%w")`. | Probe 10: a `*fmt.wrapError` level in the middle of a chain is traversed, and the levels below it are shown. | FR-002 |
| S3 | `Wrap`, `Wrapf` and `WithStack` return nil for a nil error, so `return errors.Wrap(err, "...")` is safe. | `errors.go:53-90` | FR-013 |
| S4 | A location is recorded at every wrap point. The detail shows the path the error took through the code (handler, service, repository). One stack captured at the origin cannot show that path when the error crosses concurrent tasks. | Probe 2 | FR-015 |
| S5 | Error trees (`Join`, `Unwrap() []error`) appear in the detail with positional indexes and indentation. | Probe 1 | FR-012, FR-022 |
| S6 | `WrapperError` can be embedded in a user type. The type can then be matched with `errors.As` without hand-writing `Error`, `Unwrap` or `StackTrace`. | `wrapper.go` | FR-026 to FR-029 |
| S7 | The message is composed as `context: cause`, the same convention as the standard library. | `errors.go:15-24` | FR-009 |
| S8 | Zero dependencies and a small codebase. | `go.mod` | FR-004 |
| S9 | Call sites inlined by the compiler get the correct location. | Probe: an inlined helper `mk()` is reported as `main.mk` at its own line. | FR-015 |

## Weaknesses to fix

Ordered by impact.

### W1: `Errorf` silently breaks `%w` (high)

`Errorf` formats with `fmt.Sprintf`, so the error-wrapping directive is not supported:

```text
errors.Errorf("lookup: %w", fs.ErrNotExist).Error()
  → "lookup: %!w(*errors.errorString=&{file does not exist})"
errors.Is(…, fs.ErrNotExist) → false
```

`go build` accepts this code. `go vet` reports it ("does not support error-wrapping directive
%w"), so the mistake is caught only where vet runs. The function name promises `fmt.Errorf`
behavior. → FR-007, FR-010.

### W2: Formatting verbs are ignored (high)

`werror.Format` writes `Error()` whatever the verb is (`errors.go:26-28`). Probe 3 shows:

| Verb | Output | Expected |
|------|--------|----------|
| `%+v` | `ctx: root` | the detail, as `pkg/errors` does (the API the library imitates) |
| `%q` | `ctx: root` | `"ctx: root"` (quoted) |
| `%x` | `ctx: root` | hex of the message, as for any standard error |
| `%d` | `ctx: root` | `%!d(...)`, as for any standard error |

`WrapperError` implements no `Format`, so `%+v` on a custom error never shows anything more. →
FR-023.

### W3: The detail is redundant and hard to read (high)

From probe 2 (paths shortened):

```text
(*errors.werror) main.handler
	/tmp/…/probe/main.go:37: handle request: load tenant 42: open config: file does not exist
(*errors.werror) main.service
	/tmp/…/probe/main.go:36: load tenant 42: open config: file does not exist
(*errors.werror) main.repository
	/tmp/…/probe/main.go:35: open config: file does not exist
(*errors.errorString) file does not exist
```

- **Cumulative messages**: every entry prints the node's full `Error()`, so each suffix is repeated
  at every lower level. The output grows quadratically with the depth of the chain, and the text
  each level added is never isolated.
- **Duplicate entries**: `WithStack` adds an entry identical to its cause's (probe 1, lines 00 to
  03).
- **Newlines inside entries**: a joined error's message contains `\n`. One element of the returned
  `[]string` then spans several lines (probe 1, line 01), which breaks line-oriented consumers.
- **Internal type names**: `(*errors.werror)`, `(*errors.joinError)`, `(*errors.errorString)`
  and `(*fmt.wrapError)` are noise. Types such as `*fs.PathError` or a domain error are worth
  showing, but they are lost among the noise.
- **Absolute build paths**: unless the program is built with `-trimpath`, the build machine's
  directory layout (for example `/home/<user>/…`) reaches every log, and the detail differs
  between local and CI builds.

→ FR-018, FR-019, FR-020, FR-022.

### W4: Full stacks are captured but never shown (medium)

Every operation stores up to 32 frames, but `PrintErrorDetails` prints only `stack[0]`
(`format.go:97`). The cost of a full stack is paid at every level, yet the origin's call stack
(who called the function that failed) never appears in the detail. → FR-016, FR-021.

### W5: Cost (medium)

| Operation | Time/op | Memory/op | Allocs/op |
|-----------|---------|-----------|-----------|
| standard `errors.New` | 24 ns | 16 B | 1 |
| reference `New` | 718–744 ns | 328 B | 3 |
| standard `fmt.Errorf("ctx: %w")` | 162–171 ns | 40 B | 2 |
| reference `Wrap` | 704–725 ns | 328 B | 3 |
| reference `PrintErrorDetails`, 5 levels | 4.0–4.1 µs | 3,643 B | 61 |

- A wrap costs about 4.3 times the standard wrapping in time and 8 times in memory. Creating an
  error costs about 30 times the standard `errors.New`.
- `Callers` always allocates a fixed 256-byte array (`[32]uintptr`), because the slice it returns
  escapes to the heap.
- Stacks deeper than 32 frames are cut without any marker (probe 9).

→ FR-016, SC-004.

### W6: Sentinel errors get a meaningless location (medium)

The idiomatic declaration `var ErrNotFound = errors.New("not found")` captures a stack at package
initialization. That location then appears in every detail where the sentinel is the root
(probe 6):

```text
(*errors.werror) main.init
	<autogenerated>:1: not found
```

The package offers no way to declare a sentinel without a location, so users import the standard
`errors` package under an alias. → FR-008.

### W7: Inconsistent caller-skip numbering (medium)

The probe recorded the following top frames:

| Call | Recorded top frame |
|------|--------------------|
| `Callers(0)` called from `main.main` | `runtime.main`: the direct caller is skipped |
| `NewWrapperError` (default) inside a constructor | the constructor's caller |
| `WithWrapperErrorCallerDepth(0)` | the constructor itself |
| `WithWrapperErrorCallerDepth(1)` | the constructor's caller: equal to the default |

The default differs from depth 0, and the exported `Callers` is off by one for direct use. The
stack primitives (`Callers`, `Stack`, `StackTrace`, `Frame`) are all exported, which widens the
API without a clear use case. → FR-017, FR-028.

### W8: Details are text only (medium)

`PrintErrorDetails` returns `[]string` (and, despite its name, prints nothing). There is no
structured form with fields such as message, function, file and line. Consumers rebuild one: the
upstream slog example joins the lines into a single multi-line string attribute, which a log
platform cannot index. → FR-024, FR-030.

### W9: Unbounded recursion in the detail (medium)

`getStackErr` recurses with no depth limit. An error whose `Unwrap` returns itself makes the
detail crash the process with a `fatal error: stack overflow`, which `recover` cannot intercept.
The probe reproduced it with a 1 MiB stack limit. Detail rendering usually runs in logging and
panic-recovery paths, where a crash hides the original problem. → FR-025.

### W10: Partial mirror of the standard library (low)

The package is named `errors` and forwards `Is`, `As` and `Unwrap`. It does not offer `AsType`
(Go 1.26) or `ErrUnsupported` (Go 1.21), so code that needs them imports both packages. → FR-001.

### W11: Quality and maintenance (low)

- No tests and no runnable examples, so the behavior is defined only by the code.
- The README has 4 lines.
- 12 exported functions, types and methods have no doc comment, including `PrintErrorDetails`,
  `NewWrapperError`, `Callers` and the `WithWrapperError…` options.
- Legacy `// +build go1.13` constraint and a `go 1.20` directive.

→ FR-005, and Constitution Principles III and IV.

## Verified non-issues

- **Inlining**: locations are correct for inlined call sites (S9). Go's runtime reports inlined
  frames, so the `runtime.FuncForPC` lookup works here.
- **Interop**: `errors.Is` and `errors.As` work through every level, including the library's
  `Join` and standard-library wrapping levels (S2).

## Direction for the new library

The spec turns these findings into requirements. In short:

1. **Same vocabulary, standard semantics**:
   - a drop-in replacement for the standard `errors` package, offering all of its capabilities
     (`Is`, `As`, `AsType`, `Unwrap`, `Join`, `ErrUnsupported`);
   - create, wrap, annotate and combine, with messages identical to the standard library's;
   - `%w` honored wherever a format can create causes;
   - every formatting verb behaving as on a standard error, except `%+v`, which prints the
     detail.
2. **Locations**:
   - one location per level, which is cheap;
   - the full call stack once per chain, at its origin, with truncation marked;
   - no location for errors created during package initialization (sentinels);
   - one skip numbering everywhere.
3. **Readable detail**:
   - each level shows only the text it adds, plus its location;
   - developer and third-party types are shown, and the library's own and generic standard
     types are not;
   - trees are indented, and multi-line messages are indented within their entry;
   - paths do not depend on the build directory;
   - rendering is bounded and never crashes.
4. **Structured detail**: the same information as data, with stable JSON field names. An opt-in
   helper turns an error into a `slog` attribute, and the detail is computed only when the record
   is emitted.
5. **Custom types**: an embeddable building block. The detail shows the developer's type, and
   the default location is the caller of the developer's constructor.

An illustrative detail for the chain of W3. The exact layout is decided in `/speckit-plan`:

```text
handle request
    at example.com/app/api.Handler  example.com/app/api/handler.go:37
load tenant 42
    at example.com/app/tenant.Load  example.com/app/tenant/load.go:36
open config
    at example.com/app/config.Open  example.com/app/config/open.go:35
    call stack:
        example.com/app/config.Open      example.com/app/config/open.go:35
        example.com/app/tenant.Load      example.com/app/tenant/load.go:36
        example.com/app/api.Handler      example.com/app/api/handler.go:37
        net/http.HandlerFunc.ServeHTTP   net/http/server.go:2322
        …
file does not exist
```

## Licensing note

The reference is Apache-2.0. Its `stack.go` closely follows `github.com/pkg/errors`
(BSD-2-Clause), with identical doc comments. The new library is an original implementation: the
reference is used only as a functional benchmark, and no code is copied from it.

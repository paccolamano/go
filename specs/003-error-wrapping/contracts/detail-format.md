# Contract: Detail Format and Formatting Verbs

**Requirements**: FR-018 to FR-025 and FR-023 | **Data model**: [../data-model.md](../data-model.md)

This contract fixes what `Describe(err)`, `Detail.String()`, `%+v` and the JSON serialization
produce. Tests assert it. Line numbers in the examples are illustrative, and the call stacks of
the second example and of the JSON example are abbreviated.

## How levels are derived

The following steps apply to any error (FR-019).

1. **Walk**: start at `err` and follow its causes: `Unwrap() error`, or `Unwrap() []error` with
   nil entries dropped.
   - A level with one cause continues the same chain.
   - A level with several causes ends the chain, and each cause starts a branch.
2. **Own text** (FR-020): `m` is the level's message, and `c` is the message of its single
   cause.

   | Situation | Own text |
   |-----------|----------|
   | One cause, `m == c` | `""` |
   | One cause, `m` ends with `": " + c` | `m` without that suffix |
   | One cause, any other case | `m` |
   | Several causes, `m` equals their messages joined with `"\n"` | `""` |
   | Several causes, any other case | `m` |
   | No cause | `m` |

3. **Type**: `reflect.TypeOf(err).String()`, except for these hidden types, which are matched by
   package path and type name:
   - this module's node types;
   - `errors.errorString`;
   - `errors.joinError`;
   - `fmt.wrapError`;
   - `fmt.wrapErrors`.

   A custom type that embeds `BaseError` shows its own type.
4. **Location and call stack**: a library level carries its location (FR-015). It carries its
   call stack only if it is the origin (FR-016). Levels from other code have neither.
5. **Annotations**: a library level with a hidden type, an empty own text and one cause is an
   annotation.
   - If it has a location and its cause has none, the two are **merged** into one level: the
     cause's text, type and causes, with the annotation's location and call stack.
   - If it has no location, it is **skipped**.
   - Otherwise it is shown with an empty text.
6. **Limits** (FR-025): at most 64 levels along any path from the root, and at most 256 levels
   in total. A level whose causes, or remaining branches, are cut has `Truncated: true`.
7. **Panic safety**: a foreign `Error` or `Unwrap` method that panics yields a level whose text
   is the standard formatting placeholder (`<nil>` for a nil pointer receiver, otherwise
   `%!v(PANIC=Error method: <value>)`). Its causes are not followed.

## Text layout (`Detail.String()`, `%+v`)

### Grammar

`I` is the indentation of the current chain: 0 at the root, and the parent's `I + 8` inside a
branch. Lines are joined with `\n`, and there is no trailing newline.

```text
entry      = head *continuation [at] [stack] [cut] [branches]
head       = I text-line-1 [" [" type "]"]
continuation = I "    | " text-line-k                 ; k ≥ 2, one per extra line of the text
at         = I "    at " function " (" file ":" line ")"
stack      = I "    stack:" 1*( NL I "        " function " (" file ":" line ")" ) [NL I "        ... (truncated)"]
cut        = I "    ... (more causes not shown: limit reached)"
branches   = 1*( NL I "    [" index "]" NL branch-chain )   ; branch-chain entries use I + 8
```

### Rules

- **Placeholders**: an empty own text renders as `(no message)`, or as `(N errors)` for a level
  with N causes.
- **Splitting**: a text is split into lines at `\n`.
- **Quoting**: a line is written Go-quoted (`strconv.Quote`) when it contains invalid UTF-8 or a
  character that `strconv.IsPrint` rejects (tab included), or when it starts with `"`. Without
  the last rule, a raw line starting with `"` could not be told apart from a quoted one; the fuzz
  test found this. A head line is also quoted when it is empty or starts with a space. As a
  result, a message can never produce a line that looks like a location, a marker or another
  level (FR-022). The structured form keeps the original, unquoted text.
- **`at` lines**: they appear only for levels with a location. The `stack:` block appears only
  on origins, and its first frame repeats the `at` line.

### Example: linear chain

The chain is built as follows: `Wrap(fs.ErrNotExist, "open config")` in `config.Open`, wrapped
by `Wrapf(err, "load tenant %d", 42)` in `tenant.Load`, then wrapped by
`Wrap(err, "handle request")` in `api.Handler`.

```text
handle request
    at example.com/app/api.Handler (example.com/app/api/handler.go:37)
load tenant 42
    at example.com/app/tenant.Load (example.com/app/tenant/load.go:36)
open config
    at example.com/app/config.Open (example.com/app/config/open.go:35)
    stack:
        example.com/app/config.Open (example.com/app/config/open.go:35)
        example.com/app/tenant.Load (example.com/app/tenant/load.go:36)
        example.com/app/api.Handler (example.com/app/api/handler.go:37)
        net/http.HandlerFunc.ServeHTTP (net/http/server.go:2322)
        runtime.goexit (runtime/asm_amd64.s:1700)
file does not exist
```

### Example: combined errors, a foreign level and a merged annotation

The chain is built as follows: `Wrap(Join(a, b), "sync")`, where:

- `a` is `Wrap(pathErr, "upload a.txt")`, where `pathErr` is an `*fs.PathError` whose message
  is `"open a.txt: permission denied"` and whose cause is `syscall.EPERM`;
- `b` is `WithStack(fmt.Errorf("query: %w", context.DeadlineExceeded))`, where
  `WithStack(…)` is merged with the foreign `fmt` level.

```text
sync
    at example.com/app.Sync (example.com/app/sync.go:20)
(2 errors)
    at example.com/app.Sync (example.com/app/sync.go:18)
    [0]
        upload a.txt
            at example.com/app.upload (example.com/app/upload.go:12)
            stack:
                example.com/app.upload (example.com/app/upload.go:12)
                example.com/app.Sync (example.com/app/sync.go:17)
        open a.txt [*fs.PathError]
        permission denied [syscall.Errno]
    [1]
        query
            at example.com/app.fetch (example.com/app/fetch.go:30)
            stack:
                example.com/app.fetch (example.com/app/fetch.go:30)
                example.com/app.Sync (example.com/app/sync.go:17)
        context deadline exceeded [context.deadlineExceededError]
```

### Example: hostile message

A message `"bad input\n    at evil.Fake (x.go:1)"` renders as:

```text
bad input
    |     at evil.Fake (x.go:1)
    at example.com/app.parse (example.com/app/parse.go:9)
```

## JSON (`encoding/json` of `Detail`)

The field names are stable from the first release. New fields may be added, always with
`omitempty`. Example for the linear chain above:

```json
{
  "levels": [
    {"text": "handle request",
     "location": {"function": "example.com/app/api.Handler", "file": "example.com/app/api/handler.go", "line": 37}},
    {"text": "load tenant 42",
     "location": {"function": "example.com/app/tenant.Load", "file": "example.com/app/tenant/load.go", "line": 36}},
    {"text": "open config",
     "location": {"function": "example.com/app/config.Open", "file": "example.com/app/config/open.go", "line": 35},
     "stack": {"frames": [
       {"function": "example.com/app/config.Open", "file": "example.com/app/config/open.go", "line": 35},
       {"function": "example.com/app/tenant.Load", "file": "example.com/app/tenant/load.go", "line": 36}
     ]}},
    {"text": "file does not exist"}
  ]
}
```

A level with several causes has `"branches": [[…], […]]`, where each element is a chain with the
same structure as `levels`. A nil error serializes as `{"levels": []}`.

## Formatting verbs

These apply to every library node and to every type that embeds `BaseError`.

| Verb | Output |
|------|--------|
| `%+v` | `Describe(err).String()` |
| `%v`, `%s`, `%q`, `%x`, `%X`, with any flags, width and precision | byte-identical to the same directive applied to a standard library error with the same message (SC-001, User Story 2, scenario 2) |
| any other verb (`%d`, `%#v`, …) | the same directive applied to the message string, for example `%#v` prints the Go-quoted message |
| `%w` inside `fmt.Errorf` | unaffected: fmt formats the operand with `v` |

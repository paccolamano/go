# Contract: Public Go API Changes

**Module**: `github.com/paccolamano/go/problem` | **Package**: `problem` | **Minimum Go**: 1.26
| **From**: `problem/v0.2.0` | **To**: `problem/v0.2.1`

This contract lists the changes to the module's public contract, relative to
[004 contracts/public-api.md](../../004-problem-details/contracts/public-api.md) and
[005 contracts/public-api.md](../../005-problem-fixes/contracts/public-api.md).

- **Exported surface**: unchanged. No identifier is added, removed or renamed, and no signature
  changes (FR-010).
- **Wire format**: unchanged. `Write` produces the documents of 0.2.0, for every problem,
  including a problem read from a response (FR-009). See
  [005 contracts/document.md](../../005-problem-fixes/contracts/document.md).
- **Documented behavior**: `Type.Match` changes for one case. `Read` and the package
  documentation describe the change.

The exact wording of the doc comments is decided at implementation time, but it must state every
rule below.

## Changed behavior

### `Type.Match` (FR-001 to FR-005)

```go
// Match returns the first problem of type t in err's tree, and true. Unlike
// errors.As, it looks past problems of other types. The tree is walked in the
// order errors.As uses.
//
// A problem is of type t when its type URI equals the URI of t. For a problem
// that [Read] read from a response to a request, a relative URI of t is first
// resolved against the request URL that Read resolved the problem's members
// against. A type that a service declares as "/probs/out-of-credit", and shares
// with its clients, therefore matches the problems of that path written by the
// server that answered, whichever server it is. An absolute URI of t, and the
// URI of t for any other problem, is compared as written: declare an absolute
// URI to match the problems of one host only. The comparison is exact: letter
// case, default ports and percent-encodings are not normalized.
func (t Type) Match(err error) (*Error, bool)
```

| Rule | Was (0.2.0) | Now (0.2.1) |
|------|-------------|-------------|
| Relative declaration, problem read from a response to a request | declaration as written against the resolved type: never equal | declaration resolved against the same URL, then compared |
| Absolute declaration, any problem | as written | unchanged |
| Any declaration; problem built locally, read with `Parse`, or read without a request | as written | unchanged |
| Problem derived with `With` | as the original | unchanged: the derived problem keeps the URL of the original |
| Walk of the tree, first match wins, zero `Type` matches nothing | — | unchanged |

Removed sentence: "a problem read with [Read] has its type URI resolved first, so a type declared
to match another API's problems must use that API's absolute URI."

### `Read` (documentation only)

The behavior of `Read` does not change. Its doc comment gains one sentence after the paragraph on
resolution:

```go
// The members are read as [Parse] reads them. In addition, relative "type" and
// "instance" members are resolved against the URL of resp.Request, the last
// request of any redirect chain. [Type.Match] resolves a relative declared type
// against the same URL, so a type declared with the URI that the server writes
// matches the problem. The status code of resp is not consulted: the caller can
// compare it with [Error.Status].
```

### Package documentation, `doc.go` (FR-011)

Section "Reading problem details", after "Read also resolves relative "type" and "instance"
members against the URL of the request.":

```go
// [Type.Match] compares type URIs exactly. For a problem read with Read, it
// first resolves a relative declared URI against the same request URL, so the
// declarations that a service shares with its client package match the problems
// in its responses, from whichever server answered. Absolute declarations, and
// every declaration tested against a problem built locally or read with Parse,
// are compared as written.
```

The declaration section keeps "Prefer an absolute URI; a relative one must begin with "/"."

### README (FR-011)

Section "Reading problem details", after the paragraph that follows the example: `OutOfCredit`
can be the declaration that the service itself uses, even with a relative type URI such as
`/probs/out-of-credit`. `Match` resolves it against the same request URL, so it matches the
problems of that path on the server that answered. An absolute URI matches one host only.

## Added example (FR-012)

`ExampleType_Match_shared`, in `example_test.go`:

1. A declaration `/probs/out-of-credit`, as a service shares it with its clients.
2. The service writes an occurrence with `Write`, to an `httptest.ResponseRecorder`, in answer to
   a request for `https://api.example.com/v1/messages`.
3. The client reads the recorder's response with `Read`, after setting `Request` to that request,
   as `http.Client` does.
4. It prints the problem's type and the result of `Match` with the same declaration.

Expected output:

```text
https://api.example.com/probs/out-of-credit
true
```

## Expected `gorelease` report against `problem/v0.2.0`

Observed in the spike ([research R-01](../research.md#r-01-release-level-and-api-compatibility)):

```text
# summary
Suggested version: v0.2.1 (with tag problem/v0.2.1)
```

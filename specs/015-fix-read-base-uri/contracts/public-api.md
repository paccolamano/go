# Contract: Public Go API Changes

**Module**: `github.com/paccolamano/go/problem` | **Package**: `problem` | **Minimum Go**: 1.26
| **From**: `problem/v0.2.1` | **To**: `problem/v0.2.2`

This contract lists the changes to the module's public contract, relative to
[004 contracts/public-api.md](../../004-problem-details/contracts/public-api.md),
[005 contracts/public-api.md](../../005-problem-fixes/contracts/public-api.md) and
[012 contracts/public-api.md](../../012-fix-relative-type-match/contracts/public-api.md).

- **Exported surface**: unchanged. No identifier is added, removed or renamed, and no signature
  changes (FR-011).
- **Wire format**: unchanged. `Write` writes the members of a problem as before. A problem read
  from a response simply holds URIs without the request's credentials and fragment.
- **Documented behavior**: `Read` changes for request URLs with a userinfo or a fragment. The
  package documentation and the README describe the URL it resolves against.

The exact wording of the doc comments is decided at implementation time, but it must state every
rule below.

## Changed behavior

### `Read` (FR-001 to FR-006)

```go
// The members are read as [Parse] reads them. In addition, relative "type" and
// "instance" members are resolved against the URL that resp.Request sent: the
// URL of the last request of any redirect chain, without its userinfo and its
// fragment, and with its query. Credentials that the client keeps in the
// request URL therefore never reach the problem. [Type.Match] resolves a
// relative declared type against the same URL, so a type declared with the URI
// that the server writes matches the problem. The status code of resp is not
// consulted: the caller can compare it with [Error.Status].
func Read(resp *http.Response) (*Error, error)
```

| Rule | Was (0.2.1) | Now (0.2.2) |
|------|-------------|-------------|
| Base of relative `type` and `instance` | the request URL, as the client holds it | the request URL without its userinfo and its fragment |
| Userinfo of the request URL (user name, password, or empty) | copied into every relative member without an authority of its own | never copied |
| Fragment of the request URL | taken by a member that is just `#` | never taken |
| Query of the request URL | taken by a member with an empty path and no query, such as `#msg-1` | unchanged (RFC 3986 §5.2.2) |
| Scheme, host, port, path | from the request URL | unchanged |
| Absolute members, network-path references with a host | as in 0.2.1 | unchanged |
| The caller's request and its URL | not modified | unchanged (FR-005) |
| Response without a request, or with a nil URL | nothing resolved | unchanged |
| Request URL without userinfo and without a fragment | — | every result unchanged (FR-007) |

The full table of results is in [data-model.md §4](../data-model.md#4-resolution-results-fr-006).

### `Type.Match` (FR-008, documentation only)

The rule of spec 012 does not change: a relative declaration is resolved against the URL that
`Read` resolved the problem's members against. That URL is now the URL the request sent. For
request URLs with a userinfo, two results change, so that they equal those of the same request
without it ([data-model.md §5](../data-model.md#5-type-test-typematch)).

| Case, request URL with a userinfo | Was (0.2.1) | Now (0.2.2) |
|-----------------------------------|-------------|-------------|
| Relative declaration, document with the absolute type on the server that answered | no match | match |
| Absolute declaration on the server that answered, document with the relative type | no match | match |

The doc comment drops "request" from "the request URL that Read resolved the problem's members
against", because a reader takes the request URL to include the credentials:

```go
// A problem is of type t when its type URI equals the URI of t. For a problem
// that [Read] read from a response to a request, a relative URI of t is first
// resolved against the URL that Read resolved the problem's members against.
```

The rest of the comment is unchanged.

### `(*Error).With`, `Write`, `Error.Type`, `Error.Instance`, `Error.Error`

No change to their rules. A problem derived from a problem read from a response keeps its URL
(FR-010). Its type URI, instance URI and message, and the document that `Write` produces from
it, hold neither the credentials nor the fragment of the request URL, because the problem holds
none.

### `Parse`

Unchanged: it never resolves (FR-009).

### Package documentation, `doc.go` (FR-012)

Section "Reading problem details":

```go
// [Key.Lookup] and [Error.Extensions] can read them. Read also resolves relative
// "type" and "instance" members against the URL that the request sent: the
// request URL without its userinfo and its fragment.
//
// [Type.Match] compares type URIs exactly. For a problem read with Read, it
// first resolves a relative declared URI against the same URL, so the
// declarations that a service shares with its client package match the problems
// in its responses, from whichever server answered. ...
```

### README (FR-012)

Section "Reading problem details":

- The paragraph after the example: relative URIs are resolved against the URL the request sent,
  the request URL without its credentials and its fragment.
- The next paragraph: `Match` resolves a relative type URI against the same URL as `Read`, instead
  of "the same request URL".

### `ExampleRead`

Its output does not change, because its request URL has no credentials. Its comment "resolved
against the request URL", shown in the godoc example, becomes "resolved against the URL the
request sent".

### CHANGELOG (FR-013)

The entry drafted in [research R-01](../research.md#r-01-release-level-and-changelog), under
`## [Unreleased]`, in a new `### Security` section, marked **Behavior change**.

## Expected `gorelease` report against `problem/v0.2.1`

The spike found the same exported API with `go doc -all`
([research R-01](../research.md#r-01-release-level-and-changelog)). The pull request workflow runs
`gorelease`, and is expected to report:

```text
# summary
Suggested version: v0.2.2 (with tag problem/v0.2.2)
```

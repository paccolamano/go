# Contract: Problem Document Wire Format

**Module**: `github.com/paccolamano/go/problem` | **RFC**: 9457 | **Media type**: `application/problem+json`

This contract fixes what `Write` sends and what `Read` and `Parse` accept. The Go API that
produces and reads it is in [public-api.md](public-api.md).

## Response produced by `Write`

| Part | Value |
|------|-------|
| Status line | the selected occurrence's status (400–599), or 500 for the generic server error |
| `Content-Type` | exactly `application/problem+json`, with no parameters; replaces any earlier value |
| Other headers | kept as the handler set them (for example `Retry-After`, `Content-Language`) |
| `Content-Length` | not set by the library (`net/http` sets it for small bodies) |
| Body | the document below; none for a HEAD request |

## Document produced

The document is compact JSON (no insignificant whitespace), UTF-8 encoded, with members in this
order:

| # | Member | Present when | JSON type |
|---|--------|--------------|-----------|
| 1 | `type` | always | string (URI reference) |
| 2 | `title` | the title is not empty | string |
| 3 | `status` | always; equal to the response status code | integer |
| 4 | `detail` | the detail is not empty | string |
| 5 | `instance` | the instance is not empty | string (URI reference) |
| 6… | extensions | always, one per member set | any JSON value |

- **Extension order**: extensions follow in ascending byte order of their names.
- **Extension values**: each value is the compact output of `encoding/json` for the Go value
  given to `WithExtension`. For a document that was read, it is the value as read, compacted.
- **Escaping**: strings are encoded by `encoding/json`. `"`, `\` and control characters are
  escaped, and `<`, `>` and `&` become `<`, `>` and `&`. Non-ASCII characters are
  written as UTF-8, and invalid UTF-8 becomes U+FFFD.
- **Determinism**: the same occurrence always produces the same bytes.

### Examples

Status alone, `Status(404)`:

```json
{"type":"about:blank","title":"Not Found","status":404}
```

The "out of credit" occurrence of [public-api.md](public-api.md#occurrences-fr-004-fr-005-fr-007-fr-009-fr-010-fr-018):

```json
{"type":"https://example.com/probs/out-of-credit","title":"You do not have enough credit.","status":403,"detail":"Your current balance is 30, but that costs 50.","instance":"/account/12345/msgs/abc","accounts":["/account/12345","/account/67890"],"balance":30}
```

A status without a recommended phrase, `Status(499)`:

```json
{"type":"about:blank","status":499}
```

The generic server error:

```json
{"type":"about:blank","title":"Internal Server Error","status":500}
```

## Conformance

Every produced document satisfies the JSON schema of RFC 9457 Appendix A:

- the document is an object;
- `type` and `instance` are strings in URI-reference format;
- `title` and `detail` are strings;
- `status` is an integer from 100 to 599.

It also follows the extension name rule of §3.2 (SC-001).

## Recognizing a problem response (`Read`)

- **Media type**: the `Content-Type` header is parsed as a media type. Its type and subtype are
  compared without regard to case, and its parameters are ignored. Only
  `application/problem+json` is accepted, so `Application/Problem+JSON; charset=utf-8` is.
- **Anything else**: a missing header, a header that cannot be parsed, any other type, or a nil
  response yields `ErrNotProblem`, and the body is not read.
- **Size**: the body is read up to 1 MiB (1,048,576 bytes). Reading stops after one more byte,
  and a longer body yields `ErrMalformed`.
- **Status code**: the response's status code is not consulted. The caller has it in
  `resp.StatusCode`, and the library does not reconcile it with the `status` member (FR-026).
- **Body ownership**: `Read` does not close the body.

## Reading a document (`Read`, `Parse`)

| Input | Result |
|-------|--------|
| more than 1 MiB | `ErrMalformed` |
| not valid JSON, truncated, or nested deeper than 10,000 levels | `ErrMalformed` |
| valid JSON that is not an object, including `null` | `ErrMalformed` |
| `{}` | a problem of type `about:blank` with no other member |

Members are matched by **exact** name: `TYPE` or `Status` are extensions, not standard members.

| Member | Accepted value | Otherwise |
|--------|----------------|-----------|
| `type` | a string that is a valid URI reference | `about:blank` |
| `title` | a string | absent |
| `status` | a number that is whole and between 100 and 599 (`403`, `403.0` and `4.03e2` are all 403) | absent (0) |
| `detail` | a string | absent |
| `instance` | a string that is a valid URI reference | absent |
| any other name | any JSON value | — |

- **Mistyped standard members**: a standard member with a rejected value is dropped. It does not
  become an extension.
- **Duplicate names**: when a name appears more than once, the last occurrence is used.
- **Extension values**: they are stored compacted. Numbers keep their exact text, so no
  floating-point rounding occurs.

### Relative references (`Read` only)

When the response carries its request (`resp.Request.URL`, which is the final URL after
redirects), a `type` or `instance` that is a relative reference is resolved against that URL,
as RFC 3986 §5 describes. `about:blank` and other absolute URIs are unchanged. `Parse` never
resolves.

Example: from `https://api.example.com/v1/messages`, `/probs/out-of-credit` becomes
`https://api.example.com/probs/out-of-credit`.

## Round trip

For every occurrence `e` that `Write` writes as itself, `Parse` of the body returns an
occurrence with the same type, title, status, detail and instance, and byte-identical extension
values (FR-028). `FuzzParse` checks the converse for every document `Parse` accepts whose status
is between 400 and 599: writing it and parsing the result gives the same occurrence.

# Contract: Wire Format Changes

**Module**: `github.com/paccolamano/go/problem` | **RFCs**: 9457, 6901, 3986 | **Media type**: `application/problem+json`

This contract amends [004 contracts/document.md](../../004-problem-details/contracts/document.md).
It replaces the table "Response produced by `Write`", and adds the format of the `errors`
member and of JSON Pointers. The rest of the 004 contract is unchanged: member order, escaping,
conformance, reading rules and round trip.

## Response produced by `Write`

| Part | Value |
|------|-------|
| Status line | the selected occurrence's status (400–599), or 500 for the generic server error |
| `Content-Type` | exactly `application/problem+json`, with no parameters; replaces any earlier value |
| **Removed** | `Content-Length`, `Content-Encoding`, `Content-Range`, `ETag`, `Last-Modified`, `Content-Disposition`, `Trailer`, with all their values, when the handler set them before `Write` |
| Other headers | kept as the handler set them (for example `Retry-After`, `Content-Language`, `Cache-Control`, `Vary`, `X-Content-Type-Options`) |
| `Content-Length` | not set by the library: `net/http` computes it for the problem document |
| Body | the document; none for a HEAD request |

The removal applies to every response that `Write` sends:

- an occurrence of a declared type;
- a status-only problem;
- the generic server error, for an invalid problem, an error that is not a problem, or nil;
- the response to a HEAD request.

### Example

A handler sets `Content-Length: 5`, `Content-Encoding: gzip`, `ETag: "v1"` and `Retry-After: 120`,
then fails with `problem.Status(503)`. The client receives:

```http
HTTP/1.1 503 Service Unavailable
Content-Type: application/problem+json
Retry-After: 120
Content-Length: 65

{"type":"about:blank","title":"Service Unavailable","status":503}
```

`Content-Length` here is computed by the server for the document. The handler's `5` is gone.

## The `errors` member

The validation items of RFC 9457 §3 are written as the extension member `errors`. Like every
extension, it comes after the standard members, in byte order of the extension names. Its value
is an array with one object per item, in the given order:

| # | Member | Present when | JSON type |
|---|--------|--------------|-----------|
| 1 | `detail` | always | string (possibly empty) |
| 2 | `pointer` | the pointer is not empty | string, written exactly as given |

Strings are escaped as in the rest of the document. With no items, the value is `[]`.

The RFC 9457 §3 example, as `Write` sends it:

```json
{"type":"https://example.net/validation-error","title":"Your request is not valid.","status":422,"errors":[{"detail":"must be a positive integer","pointer":"#/age"},{"detail":"must be 'green', 'red' or 'blue'","pointer":"#/profile/color"}]}
```

### Reading the `errors` member (`Errors`)

| Value of `errors` | Result |
|-------------------|--------|
| absent | not found |
| `null`, a string, a number, a boolean or an object | not found |
| `[]` | an empty list, found |
| an array in which some element is not an object (including `null`) | not found |
| an array of objects, where some `detail` or `pointer` is present but is not a string | not found |
| an array of objects, otherwise | one item per element, in order |

- `detail` and `pointer` are matched by their exact names. `Detail` or `POINTER` are other
  members.
- A missing `detail` or `pointer` reads as `""`.
- Other members of an item (for example `code`) are ignored by `Errors`, and stay in
  `Extensions()["errors"]` as read.
- When a name appears twice in an item, the last occurrence is used, as `Parse` does for the
  document.

## JSON Pointers built by `Pointer`

A pointer is the URI fragment form of RFC 6901 §6: `#` followed by one `/` and one encoded token
per reference token.

| Byte of a token | Written as |
|-----------------|------------|
| `~` | `~0` |
| `/` | `~1` |
| `A`–`Z`, `a`–`z`, `0`–`9`, `-`, `.`, `_` | itself |
| `!`, `$`, `&`, `'`, `(`, `)`, `*`, `+`, `,`, `;`, `=`, `:`, `@`, `?` | itself |
| any other byte, including `%`, space, `"`, `#`, `[`, `]`, `\`, `^`, `` ` ``, `{`, `\|`, `}`, control bytes and every byte ≥ 0x80 | `%` and two uppercase hexadecimal digits |

The examples of RFC 6901 §6, plus the cases that the module also pins:

| Tokens | Pointer |
|--------|---------|
| (none) | `#` |
| `foo` | `#/foo` |
| `foo`, `0` | `#/foo/0` |
| one empty token | `#/` |
| `a/b` | `#/a~1b` |
| `c%d` | `#/c%25d` |
| `e^f` | `#/e%5Ef` |
| `g\|h` | `#/g%7Ch` |
| `i\j` | `#/i%5Cj` |
| `k"l` | `#/k%22l` |
| a space | `#/%20` |
| `m~n` | `#/m~0n` |
| `città` | `#/citt%C3%A0` |
| `items`, `0`, `qty` | `#/items/0/qty` |
| `~1` | `#/~01` |
| `a?b:c@d` | `#/a?b:c@d` |

**Guarantees**, for any tokens, including bytes that are not valid UTF-8:

- the pointer is ASCII;
- it is a valid URI reference;
- removing the `#`, percent-decoding, splitting on `/`, then replacing `~1` with `/` and `~0`
  with `~` in each part gives back the tokens exactly.

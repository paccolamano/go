# Data Model: Read Resolves Against the URL the Request Sent (problem)

**Feature**: [spec.md](spec.md) | **Research**: [research.md](research.md)

This feature changes the value of one internal field of a problem read from a response. The model
of spec 004 ([004 data-model.md](../004-problem-details/data-model.md)), its changes in spec 005
([005 data-model.md](../005-problem-fixes/data-model.md)) and the `base` field of spec 012
([012 data-model.md](../012-fix-relative-type-match/data-model.md)) still apply. This document
describes only what changes. No field is added, and the wire format does not change.

## 1. Request URL

The URL of `resp.Request`, the last request of any redirect chain, as the caller's HTTP client
holds it. Owned by the caller, and never modified by `Read` (FR-005).

| Component | Sent on the wire | Example |
|-----------|------------------|---------|
| scheme, host, port | yes, in the connection and the `Host` header | `http`, `api.example.com` |
| path, query | yes, in the request-target | `/v1/accounts/42?api_key=XYZ` |
| userinfo | no: `http.Client` turns it into an `Authorization: Basic` header | `svc:s3cret` |
| fragment | no (RFC 9110 §7.1) | `frag` |

After a relative redirect, Go's client keeps the userinfo of the previous URL, and takes the
fragment of the `Location`, if any ([research R-04](research.md#r-04-redirects-fr-004)).

## 2. URL the request sent

The request URL without its userinfo and its fragment: the target URI of RFC 9110 §7.1, and the
base URI of the response's document (RFC 3986 §5.1.3).

**Derivation** (in `Read`): a copy of `*resp.Request.URL`, with these fields cleared:

| `url.URL` field | Value |
|-----------------|-------|
| `User` | nil: user name and password alike, and an empty userinfo (`http://@host`) |
| `Fragment`, `RawFragment` | `""` |
| every other field: `Scheme`, `Opaque`, `Host`, `Path`, `RawPath`, `OmitHost`, `ForceQuery`, `RawQuery` | kept |

**Rules**:

- **Owned**: the copy is new for each problem and nothing modifies it after `Read` returns. The
  caller's `*url.Userinfo` is not modified: the copy stops pointing to it.
- **Internal**: never written by `Write`, never returned by an accessor, never printed by
  `Error()` (Key Entities of the spec).
- **Identity on clean URLs**: for a request URL without userinfo and without a fragment, the copy
  equals the copy of 0.2.1, field for field (FR-007).

## 3. Problem (`*Error`)

| Field | Change |
|-------|--------|
| `typ`, `instance` | Resolved against the URL the request sent, instead of the request URL (FR-001) |
| `base` | Holds the URL the request sent, instead of a plain copy of the request URL (FR-003) |
| every other field | unchanged |

### How each source sets `base`

| Source | `base` | Change |
|--------|--------|--------|
| `Type.New`, `Status`, `Parse`, the generic server error of `Write` | nil | none |
| `Read`, `resp.Request` or its `URL` nil | nil | none |
| `Read`, with a request URL | **the URL the request sent** | was a plain copy of the request URL |
| `(*Error).With` | the original's `base` | none: a derived problem keeps the cleaned URL (FR-010) |

A problem is immutable once built, so `base` has no other state transition.

## 4. Resolution results (FR-006)

Request URL `http://svc:s3cret@api.example.com/v1/accounts/42?api_key=XYZ#frag`. The URL the
request sent is `http://api.example.com/v1/accounts/42?api_key=XYZ`.

| Member in the document | Takes from the base | `problem/v0.2.1` | Now |
|------------------------|---------------------|------------------|-----|
| type `/probs/out-of-credit` | scheme, authority | `http://svc:s3cret@api.example.com/probs/out-of-credit` | `http://api.example.com/probs/out-of-credit` |
| instance `#msg-1` | scheme, authority, path, query | `http://svc:s3cret@api.example.com/v1/accounts/42?api_key=XYZ#msg-1` | `http://api.example.com/v1/accounts/42?api_key=XYZ#msg-1` |
| instance `#` | scheme, authority, path, query, fragment | `http://svc:s3cret@api.example.com/v1/accounts/42?api_key=XYZ#frag` | `http://api.example.com/v1/accounts/42?api_key=XYZ` |
| instance `?page=2` | scheme, authority, path | `http://svc:s3cret@api.example.com/v1/accounts/42?page=2` | `http://api.example.com/v1/accounts/42?page=2` |
| instance `msgs/1` | scheme, authority, path | `http://svc:s3cret@api.example.com/v1/accounts/msgs/1` | `http://api.example.com/v1/accounts/msgs/1` |
| type `//cdn.example.com/probs/x` | scheme | `http://cdn.example.com/probs/x` | unchanged |
| type `https://other.example.com/probs/x` | nothing | as written | unchanged |
| no type | nothing | `about:blank` | unchanged |

"Authority" here is the host and port, plus the userinfo on 0.2.1. A member that is just `#` (or
`//`, which Go resolves alike) is the only one that takes the base's fragment
([research R-03](research.md#r-03-what-changes-and-what-does-not-fr-006-to-fr-010)).

## 5. Type test (`Type.Match`)

The comparison rule of spec 012 is unchanged: for a problem with a `base`, a relative declaration
is resolved against `base`, then compared exactly. `base` is now the URL the request sent, so both
sides of the comparison hold no credentials.

Results for the table of FR-007 of spec 012, read from `https://api.example.com/v1/messages` with
`svc:s3cret@` and `#frag` added (FR-008). The other rows do not read from a request URL, and do not
change.

| Row | Declared | Document type | `problem/v0.2.1`: type, match | Now: type, match |
|-----|----------|---------------|-------------------------------|------------------|
| 1 | `/probs/out-of-credit` | `/probs/out-of-credit` | `https://svc:s3cret@api.example.com/probs/out-of-credit`, yes | `https://api.example.com/probs/out-of-credit`, yes |
| 2 | `/probs/out-of-credit` | `https://api.example.com/probs/out-of-credit` | as written, **no** | as written, yes |
| 3 | `/probs/out-of-credit` | `https://other.example.com/probs/out-of-credit` | as written, no | as written, no |
| 4 | `/probs/out-of-credit` | `/probs/other` | `https://svc:s3cret@api.example.com/probs/other`, no | `https://api.example.com/probs/other`, no |
| 9 | `https://api.example.com/probs/out-of-credit` | `/probs/out-of-credit` | `https://svc:s3cret@api.example.com/probs/out-of-credit`, **no** | `https://api.example.com/probs/out-of-credit`, yes |

Every "now" column equals the result of the same row read from the URL without credentials and a
fragment, as FR-008 requires. The 0.2.1 column was observed in the reproduction
([research](research.md#evidence)).

## Invariants

- **No credentials in a problem**: for every problem read from a response, no field, exported or
  internal, holds the userinfo of the request URL (FR-003). Members that the server wrote with an
  authority of their own, such as `//user@cdn.example.com/x`, are the server's content, and are
  kept as written.
- **No request fragment in a problem**: no resolved member holds the fragment of the request URL.
  A member's own fragment, such as `#msg-1`, is kept.
- **Clean URLs unchanged**: for a request URL without userinfo and without a fragment, every type
  URI, instance URI and type test result equals that of `problem/v0.2.1` (FR-007).
- **One base**: the members and the type test use the same URL, so a declaration equal to the
  document's type as written always matches (spec 012, FR-015 of this spec).

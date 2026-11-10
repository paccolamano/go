# Data Model: Relative Problem Types Match Problems Read From a Response (problem)

**Feature**: [spec.md](spec.md) | **Research**: [research.md](research.md)

This feature adds one internal field to a problem, and changes how the type test compares. The
model of spec 004 ([004 data-model.md](../004-problem-details/data-model.md)) and its changes in
spec 005 ([005 data-model.md](../005-problem-fixes/data-model.md)) still apply. This document
describes only what changes. The wire format does not change.

## 1. Problem type declaration (`Type`)

Unchanged: a type URI, a title and a status code, checked by `NewType`. The type URI is an
absolute URI, or a URI reference that begins with "/" (FR-008). In this document, a declaration is
**relative** when its URI begins with "/", and **absolute** otherwise. No other kind exists.

## 2. Problem (`*Error`)

| Field | Kind | Change |
|-------|------|--------|
| `typ`, `title`, `status`, `detail`, `instance`, `ext`, `cause`, `invalid` | — | unchanged |
| **`base`** (new) | `*url.URL`, unexported | The URL that `Read` resolved `typ` and `instance` against, or nil |

Rules for `base`:

- **Owned**: it is a copy of `*resp.Request.URL`, made by `Read`. Nothing modifies it after
  `Read` returns, so it is safe for concurrent use, like the rest of `Error`.
- **Internal**: it is never written by `Write`. No accessor returns it, and `Error()` does not
  print it. It does not change any document, message or value that the module returns (FR-009).

### How each source sets it

| Source | `base` | `typ` |
|--------|--------|-------|
| `Type.New` | nil | the declared URI, as written |
| `Status` | nil | `about:blank` |
| `Parse` | nil | the document's type as written, or `about:blank` |
| `Read`, `resp.Request` or its `URL` nil | nil | as `Parse` |
| `Read`, with a request URL | copy of `*resp.Request.URL`: the last request of any redirect chain | the document's type, resolved against `base` |
| `(*Error).With` | the original's `base` (FR-006) | the original's `typ` |
| the generic server error of `Write` | nil | `about:blank` |

A problem is immutable once built, so `base` has no other state transition.

## 3. The type test (`Type.Match`)

The walk of the error tree is unchanged (FR-005). The zero `Type` still matches nothing. For each
`*Error` visited, the declared URI `uri` is compared with the problem's type:

```text
            problem.base != nil ?
             │no              │yes
             ▼                ▼
       typ == uri       uri begins with "/" ?
                         │no              │yes
                         ▼                ▼
                   typ == uri     typ == resolve(base, uri)
```

- `resolve` is the function that `Read` used for `typ`: RFC 3986 §5, with an absolute or empty
  reference returned unchanged.
- `==` is an exact comparison, with no other normalization (FR-004).

### Results (FR-007)

Request URL `https://api.example.com/v1/messages`. "Was" is `problem/v0.2.0`.

| Declared | Problem | `base` | Compared | Match | Was |
|----------|---------|--------|----------|-------|-----|
| `/probs/out-of-credit` | read, document `/probs/out-of-credit` | set | `https://api.example.com/probs/out-of-credit` both | yes | no |
| `/probs/out-of-credit` | read, document `https://api.example.com/probs/out-of-credit` | set | the same | yes | no |
| `/probs/out-of-credit` | read, document `https://other.example.com/probs/out-of-credit` | set | different hosts | no | no |
| `/probs/out-of-credit` | read, document `/probs/other` | set | different paths | no | no |
| `/probs/out-of-credit` | read without a request, document `/probs/out-of-credit` | nil | as written | yes | yes |
| `/probs/out-of-credit` | `Parse`, `/probs/out-of-credit` | nil | as written | yes | yes |
| `/probs/out-of-credit` | `Parse`, `https://api.example.com/probs/out-of-credit` | nil | as written | no | no |
| `/probs/out-of-credit` | `Type.New` | nil | as written | yes | yes |
| `https://api.example.com/probs/out-of-credit` | read, document `/probs/out-of-credit` | set | as written, against the resolved type | yes | yes |
| `https://api.example.com/probs/out-of-credit` | `Parse`, `/probs/out-of-credit` | nil | as written | no | no |

### Invariants

- **Shared declarations**: for a problem read from a response, a declaration whose URI equals the
  document's type as written always matches. Both sides are `resolve(base, uri)`, or both are the
  absolute URI as written (FR-015).
- **No new match without a base**: when `base` is nil, the result is that of `problem/v0.2.0`.
- **No new match for absolute declarations**: an absolute declaration takes the comparison of
  `problem/v0.2.0` for every problem.
- **No allocation** on the unchanged paths, `base` nil or absolute declaration (SC-003).

### Redirects

`base` is the URL of the last request of the chain, as for the resolution of the members. A
relative declaration matches the type written by the server that answered last. An absolute
declaration naming the host of the first request does not match a relative type written by the
host of the last one, as in 0.2.0.

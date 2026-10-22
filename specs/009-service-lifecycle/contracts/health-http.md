# Contract: Health probe responses

Behavior of `health.Handler`. The evaluation model is in
[../data-model.md](../data-model.md#evaluation), identifiers in [public-api.md](public-api.md).

## Methods

| Method | Checks run | Status | Body |
|--------|------------|--------|------|
| GET | yes | 200 or 503 | see below |
| HEAD | yes | 200 or 503 | none |
| any other | no | 405, with `Allow: GET, HEAD` | `method not allowed` |

## Headers

Every response carries:

```http
Content-Type: text/plain; charset=utf-8
Cache-Control: no-store
X-Content-Type-Options: nosniff
Content-Length: <n>
```

## Status

- 200 when every check passes, including when there are no checks.
- 503 when at least one check fails: an error, a timeout, a panic, or still running from an
  earlier evaluation.

## Body

Every line ends with a line feed. `verbose` means that the query has a `verbose` parameter, with
any value or none.

| Outcome | `verbose` | Body |
|---------|-----------|------|
| every check passes | absent | `ok` |
| every check passes | present | one line per check, then `ok` |
| a check fails | any | one line per check, then `failed` |

Check lines follow the order the checks were given to `New`:

```text
[+]<name> ok
[-]<name> failed: reason withheld
```

Example, readiness with `redis` down:

```http
HTTP/1.1 503 Service Unavailable
Content-Type: text/plain; charset=utf-8
Cache-Control: no-store
X-Content-Type-Options: nosniff
Content-Length: 71

[+]lifecycle ok
[+]postgres ok
[-]redis failed: reason withheld
failed
```

The response never contains an error message, a panic value, a stack trace or a duration.

## Evaluations

- A request that arrives while an evaluation is in progress waits for it and receives its result.
  Otherwise it starts a new one. A result is never reused after its evaluation has completed.
- Each check runs at most once at a time per handler. A check whose operation is still running
  from an earlier evaluation fails, without being started again.
- Every check of an evaluation runs concurrently, bounded by its own timeout, 1 s by default. The
  response is ready no later than the longest timeout, plus scheduling overhead.
- The checks' contexts carry the values of the first request's context. They are cancelled when
  their timeout expires, or when every waiting request has gone.
- A request whose context ends before the evaluation completes gets no response body written.

## Logging

With `WithLogger`, each completed evaluation logs every failed check once, at level Warn, with the
message `health: check failed` and the attributes:

| Attribute | Value |
|-----------|-------|
| `check` | the check's name |
| `reason` | `error`, `timeout`, `panic` or `still running` |
| `error` | the error, for `error` |
| `panic`, `stack` | the panic value and the stack trace, for `panic` |
| `timeout` | the check's timeout, for `timeout` |

An evaluation abandoned by every request logs nothing. Without a logger, nothing is logged.

## Probe configuration (documentation)

| Probe | Handler | Checks |
|-------|---------|--------|
| readiness | `/readyz` | `app.Ready` and the dependencies |
| liveness | `/livez` | none, or in-process checks only |

The probe's timeout, which defaults to 1 s in Kubernetes, must exceed the longest check timeout.

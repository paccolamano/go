# Contract: HTTP Semantics

**API**: [public-api.md](public-api.md) | **Data model**: [../data-model.md](../data-model.md) | **Research**: [../research.md](../research.md)

This contract fixes the observable behavior: which credentials each method reads, which outcome
it produces, and which status, headers and cookies reach the client. "invalid" is short for
`ErrInvalidCredentials` and "none" for `ErrNoCredentials`.

## 1. Authentication middleware (`auth.New`)

For each request:

1. The middleware calls the configured authenticator. For an `Any`, it walks the leaves in
   order and records the leaf that decided (R-04).
2. It maps the outcome to a response:

   | Outcome | Response | `Rejection.Err` |
   |---------|----------|-----------------|
   | principal | next handler, with the state in the context | none |
   | none, without `WithAnonymous` | 401 + challenges (§2) | `ErrNoCredentials` |
   | none, with `WithAnonymous` | next handler, with no principal | none |
   | `ErrTooManyAttempts` | 429, no challenge | the method's error |
   | invalid | 401 + challenges (§2), also with `WithAnonymous` | the method's error |
   | any other error, nil/nil, or an empty subject | 500, no challenge | the error, or an internal contract error |

3. To write a rejection, it sets `WWW-Authenticate` when it applies, then calls the
   `RejectFunc`:
   - if the function writes neither a status nor a body, the middleware sends the status with
     an empty body;
   - the default function writes `http.StatusText(status)` with
     `Content-Type: text/plain; charset=utf-8`;
   - nothing from `Err` is ever written by the default.

Construction errors, which wrap `ErrInvalidConfig`:

- a nil authenticator;
- an `Any` without methods, or with a nil method, at any depth;
- a nil `RejectFunc`.

## 2. Challenges

- Each leaf that implements `auth.Challenger` contributes `Challenge(ErrNoCredentials)`.
- The middleware keeps the non-empty values in leaf order, without exact duplicates. Each one
  becomes its own `WWW-Authenticate` field line.
- When leaf L returns invalid:
  - L's `Challenge(ErrInvalidCredentials)` takes the place of L's plain challenge;
  - every other challenge whose scheme equals L's is dropped. The scheme is the first token,
    compared without regard to case.
- When no leaf contributes a challenge, the header is omitted.

Challenges of the module's methods:

| Method | `ErrNoCredentials` | `ErrInvalidCredentials` | `ErrInsufficientMethods` |
|--------|--------------------|-------------------------|--------------------------|
| `basic` | `Basic realm="<realm>", charset="UTF-8"` | same | "" |
| `apikey` (Authorization) | `Bearer` | `Bearer error="invalid_token"` | "" |
| `apikey` (custom header H) | `ApiKey header="H"` | same | "" |
| `jwt` | `Bearer` | `Bearer error="invalid_token"` | `Bearer error="insufficient_user_authentication", error_description="A different authentication level is required"` |
| `session` | `Cookie cookie-name="__Host-<name>"` | same (never requested, see §6) | "" |

Parameter values are quoted strings, with `"` and `\` escaped.

Examples:

- `Any(sessions, keys, tokens)`, request without credentials: three field lines,
  `Cookie cookie-name="__Host-session"`, `Bearer` (keys and tokens deduplicated) and nothing
  else.
- The same combination with an expired token: `Cookie cookie-name="__Host-session"` and
  `Bearer error="invalid_token"`. The plain `Bearer` is dropped because it has the same scheme.

## 3. Requirement of methods (`auth.RequireMethods`)

| Condition | Response | `Rejection.Err` |
|-----------|----------|-----------------|
| no authentication state in the context | 500 with the default response | internal wiring error |
| no principal (anonymous) | 401 + the stored plain challenges, through the stored `RejectFunc` | `ErrNoCredentials` |
| all required methods present (or the list is empty) | next handler | none |
| a method is missing and the deciding leaf's `Challenge(ErrInsufficientMethods)` is not "" | 401 + that single challenge | `ErrInsufficientMethods` |
| a method is missing and the challenge is "" | 403, no challenge | `ErrInsufficientMethods` |

An empty string in the required list never matches.

## 4. HTTP Basic (`basic`)

| Request | Outcome |
|---------|---------|
| no `Authorization`, or a scheme other than `Basic` (compared without regard to case) | none |
| `Basic` with a value that is not valid standard base64 with padding | invalid |
| decoded value without `:`, an empty username, or invalid UTF-8 | invalid |
| `Basic` with credentials | `checker.Check(r.Context(), user, pass)`, whose result is returned unchanged |

The value is split at the first colon, so the password may contain colons. A successful check
has the methods `["pwd"]`. With the attempt limit (§8), a blocked username produces 429.

## 5. API keys (`apikey`)

Credential source:

- by default, `Authorization` with scheme `Bearer` (compared without regard to case), the token
  after the scheme;
- with `WithHeader(H)`, the whole value of header H, with surrounding spaces trimmed.

| Credential | Outcome |
|------------|---------|
| absent or empty | none |
| does not start with `<prefix>_` | none |
| starts with `<prefix>_`, but is not `<prefix>_<16 base32>_<52 base32>` | invalid |
| lookup returns `ErrNotFound` | invalid |
| lookup returns another error | that error, wrapped (500) |
| SHA-256 of the secret ≠ `Account.Hash` (constant-time) | invalid |
| `Account.ExpiresAt` is set and not after now | invalid |
| `Account.Subject` empty | contract error (500) |
| otherwise | principal: `Subject` = `Account.Subject`, no methods |

## 6. Sessions and pending logins (`session`)

### Cookies

| Cookie | Name | Value | Attributes |
|--------|------|-------|------------|
| session | `__Host-<name>` (default `__Host-session`) | 43-character base64url identifier | `Path=/; Max-Age=<lifetime>; Secure; HttpOnly; SameSite=Lax`, no `Domain` |
| pending | `__Host-<name>-pending` | same form | same, with `Max-Age=<pending lifetime>` |

To clear a cookie, the response sets it with the same name and attributes, an empty value and
`Max-Age=0`.

### Operations

| Operation | Effect | Errors |
|-----------|--------|--------|
| `Authenticate(r)` | reads the session cookie; a missing or malformed value is none, before any store access. Then one `Get`: not found is none; a decoded record with `now ≥ exp` or `now ≥ rw + idle` is none. If `now − rw ≥ idle/4`, one `Set` with `rw = now`. Result: principal (`sub`, `amr`). | `Get` or `Set` failure, or an undecodable record → error (500) |
| `Issue(w, r, p)` | `p` must have a subject. (1) deletes the session and the pending login the request's cookies identify; (2) writes a new record under a new identifier; (3) sets the session cookie and clears the pending cookie. | delete or write failure → error, no cookie set |
| `IssuePending(w, r, p)` | deletes the request's pending login, if any, writes a pending record under a new identifier, and sets the pending cookie | store failure → error, no cookie set |
| `Pending(r)` | reads the pending cookie and the record, and returns an `auth.Pending`. Absent, expired or cancelled → none. | store failure → error |
| `Revoke(w, r)` | deletes the session and the pending login of the request, and clears both cookies, always | the first delete error is returned after the cookies are cleared |

### `auth.Pending.Attempt(ctx, verify)`, as implemented by `session`

1. `n := Incr(pending-attempts key, pending lifetime)`. On a store error it returns the error;
   no attempt is counted.
2. If `n > 5`, it deletes the pending login and returns `ErrTooManyAttempts`.
3. Otherwise it computes `err := verify(ctx)`:
   - if `errors.Is(err, ErrInvalidCredentials)` and `n == 5`, it deletes the pending login and
     returns an error wrapping both `ErrTooManyAttempts` and `ErrInvalidCredentials`;
   - otherwise it returns `err` unchanged: nil, invalid, or a failure, which stays counted.

## 7. Second factor

### `totp.Verifier.Verify(ctx, pending, code)`

1. It calls `lookup(ctx, subject)`:
   - `ErrNotFound` → an error wrapping `auth.ErrNotFound`, not counted;
   - another error → wrapped, not counted.
2. It calls `pending.Attempt(ctx, check)`, where `check`:
   - rejects anything other than exactly 6 ASCII digits (invalid);
   - computes the codes for the steps t − 1, t and t + 1 and compares each in constant time;
     no match is invalid;
   - on a match at step s, calls `Incr(totp-used:<digest(subject)>:s, 2 min)`. A value above 1
     is invalid (replay), and an `Incr` failure is returned.
3. On success it returns `&auth.Principal{Subject, Methods: pending methods + "otp", "mfa"}`.

### `totp.Verifier.Confirm(ctx, subject, secret, code)`

- The same check as `Verify`, with the replay record, but without a pending login or attempt
  budget.
- A secret shorter than 16 bytes is an error.
- It returns nil on success, so that the developer can store the secret as active.

### `totp.Verifier.Enrolled(ctx, subject)`

| Lookup result | Result |
|---------------|--------|
| a secret | `true, nil` |
| `ErrNotFound` | `false, nil` |
| another error | `false`, the error wrapped |

### `recovery.Verifier.Verify(ctx, pending, code)`

1. It calls `lookup(ctx, subject)`. An error is returned and not counted.
2. It calls `pending.Attempt(ctx, check)`, where `check`:
   - normalizes the code (data model §7). Anything other than 16 alphabet characters is
     invalid;
   - hashes it and compares the hash with every stored hash in constant time, without stopping
     early. No match is invalid;
   - calls `Incr(recovery-used:<digest(subject, hash)>, 10 min)`. A value above 1 is invalid;
   - calls `consume(ctx, subject, hash)`. `ErrNotFound` is invalid, and another error is
     returned.
3. On success it returns `&auth.Principal{Subject, Methods: pending methods + "mfa"}`.

## 8. Password checker (`password.Checker.Check`)

1. An empty password, a password over the maximum length, or an empty username returns invalid.
2. It calls `lookup(ctx, username)`:
   - `ErrNotFound` selects the dummy hash;
   - another error is returned wrapped (500);
   - an empty `Subject` is a contract error.
3. With the attempt limit set, it calls `Incr(password-attempts:<digest(username)>, Window)`. A
   store error is returned. A value above `Max` returns `ErrTooManyAttempts`, without verifying.
4. It verifies against the stored hash, or against the dummy hash:
   - for argon2id, it derives the key with the stored parameters and salt, and compares it with
     the stored key using `subtle.ConstantTimeCompare` (FR-007);
   - for bcrypt, `bcrypt.CompareHashAndPassword` compares in constant time;
   - a malformed or out-of-bounds stored hash is an error, not invalid;
   - for a bcrypt hash, a password over 72 bytes fails after the comparison.
5. On success:
   - with the limit set, it deletes the counter; a delete failure is returned;
   - if the hash is outdated and `WithRehash` is set, it calls `RehashFunc(ctx, subject,
     newHash)`;
   - it returns `Methods: ["pwd"]`.
6. On failure, it returns invalid.

## 9. JSON Web Tokens (`jwt`)

### Credential shape

| `Authorization` | Outcome |
|-----------------|---------|
| absent, or a scheme other than `Bearer` | none |
| `Bearer` with a token containing exactly 2 `.` | candidate JWS, verified below |
| `Bearer` with a token containing exactly 4 `.` (JWE) | invalid |
| `Bearer` with any other token, such as an API key | none |

### Verification (every failure is invalid unless marked 500)

1. The token is parsed with the algorithm list `[RS256, PS256, ES256, EdDSA, HS256]`.
2. The header must not contain `crit`.
3. When `WithType(T)` is set, the header `typ` must equal T. Both values have any
   `application/` prefix removed and are compared without regard to case.
4. Key selection. The candidates are the keys bound to the header's `alg`, filtered by `kid`
   when the header has one:
   - exactly 1 candidate: continue;
   - more than 1: invalid;
   - 0 with a remote key set: a refresh is attempted when allowed (§9, Key set downloads), and
     the selection is retried. When still 0:
     - invalid if the last download succeeded or no download was allowed after a successful
       one;
     - **500** if the last download failed;
   - 0 with static keys: invalid.
5. The signature is verified with the selected key, and the claims are decoded.
6. `exp` must be present.
7. `iss`, `aud`, `exp`, `nbf` and `iat` are checked with the leeway, against the current time.
8. `sub` must not be empty.
9. `amr` must be absent or a list of strings.

On success the principal has `Subject = sub`, `Methods = amr` and `Extra` holding all the claims,
read through `jwt.Claims`.

### Key set downloads

| Situation | Action |
|-----------|--------|
| no keys yet, or the lifetime has elapsed | download if allowed; otherwise use the keys held, or return the last error (500) when there are none |
| unknown `kid` | download if allowed |
| download allowed | at least the refresh interval has passed since the previous attempt, whether it succeeded or failed |
| download already in flight | wait for it, or for the request's own context |

The download:

- `GET <url>`, with `Accept: application/jwk-set+json, application/json`;
- the context of the triggering request, without its cancellation, plus a 10-second timeout;
- status 200 only; any other status is an error;
- a body of at most 1 MiB; a larger body is an error;
- keys parsed one by one, skipping the unusable ones (R-18). A set with no usable key is an
  error;
- the new lifetime is `max-age` from `Cache-Control`. `no-cache` or `no-store` gives the
  minimum, and the value is clamped to 1 minute to 24 hours, or set to 1 hour when there is
  none;
- a failure keeps the previous keys and records the error.

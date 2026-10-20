# Feature Specification: HTTP Request Authentication (auth)

**Feature Branch**: `007-http-auth` (suggested; not yet created)

**Created**: 2026-10-19

**Status**: Draft

**Input**: User description: "Implementiamo un modulo auth per autenticare le richieste HTTP, con
un'interfaccia piccola e un sotto-package per ogni metodo di autenticazione." The full request is
reproduced verbatim in [Appendix: original request](#appendix-original-request).

## Context

Every service writes its own authentication, and the parts that matter are the ones that are easy
to get wrong:

- the response time reveals whether an account exists (user enumeration);
- an unreachable database answers 401, exactly like a wrong password;
- password hashes use weak algorithms or parameters. bcrypt uses only the first 72 bytes of a
  password, and the common Go implementation refuses longer passwords when hashing but accepts
  them when verifying;
- session cookies lack security attributes, the identifier does not change at login (session
  fixation), and the identifier is stored in clear;
- a one-time code is guessed by brute force when attempts are not limited, and reused when used
  codes are not tracked;
- a JSON Web Token is accepted with the algorithm named in its own header, including `none`, or
  with an RSA public key used as an HMAC secret;
- API keys are stored in clear, so anyone who reads a backup can use them.

None of these is solved by a function of a few lines, which is why they belong in a library.

This feature delivers a new, independent module in this repository (Constitution, Principle I),
named `auth`. It authenticates HTTP requests: it establishes who made a request and how they
proved it. It does not decide what they may do. It consists of a small core and one package per
authentication method:

- **core**: the contract every method implements, the combination of several methods, the
  middleware that admits or rejects requests, and the requirement of specific methods on sensitive
  routes;
- **store**: the contract of the key-value store that holds the library's short-lived state, and
  an in-memory implementation;
- **password**: hashing and checking passwords;
- **session**: server-side sessions carried by a cookie, including the half-completed login that
  waits for a second factor;
- **totp** and **recovery**: the second factor, with time-based one-time codes and single-use
  recovery codes;
- **basic**: HTTP Basic authentication;
- **apikey**: API keys;
- **jwt**: verification of JSON Web Tokens issued by an identity provider.

**The library does not know the developer's database.** It owns protocol and cryptography. The
developer provides two integration points, independent of each other:

1. **lookup functions** that read from the developer's database: accounts and password hashes,
   second-factor secrets, recovery code hashes, API keys;
2. **a key-value store with expiry** for the library's temporary state: sessions, pending logins
   and counters, for example on Redis.

Accounts can live in a relational database and sessions in a cache. A request authenticated with
a session reads only the store. A JSON Web Token needs neither a lookup nor the store: it is
verified with the issuer's keys.

The Go language, the standards cited below and the two third-party dependencies (FR-002) are given
by the request and by the constitution (v1.2.0). They are not design choices made here. They
appear because they are the observable contract of an authentication library. How the module is
built is left to `/speckit-plan`.

**Actors**:

- **Developer**: integrates the module in a service. Writes the lookups, the store adapter and the
  login, second-factor and logout handlers, and decides which routes require which methods.
- **End user**: signs in through a browser with a password and, if enrolled, a second factor.
- **API client**: a program that calls the service with an API key, a token or Basic credentials.
- **Identity provider**: issues the tokens the service verifies and publishes the keys that sign
  them.
- **Attacker**: guesses passwords and codes, probes for accounts, replays codes, forges tokens and
  reads backups.
- **Maintainer**: evolves the module, adds methods and publishes its releases.

### Clarifications

#### Session 2026-10-19

- Q: Should the password checker limit failed attempts per account, in addition to the per-IP
  rate limiting of `middleware/ratelimit`? → A: Yes, as an optional feature of the checker that
  uses the store. Attempts are counted for existing and nonexistent usernames alike; beyond the
  threshold the checker rejects without verifying the password until the window ends, with a
  distinct error answered with 429 (FR-030; User Story 2, scenarios 10 to 13).
- Q: Which status does the requirement of methods use when a required method is missing? → A: It
  depends on the method: 401 with `Bearer error="insufficient_user_authentication"` (RFC 9470)
  for tokens, 403 for sessions, API keys and Basic, with a distinct cause passed to the rejection
  function (FR-019).

#### Session 2026-10-20

- Q: May operations bound to an HTTP request take their context from the request instead of a
  first context parameter? → A: Yes. They use `r.Context()`, as `net/http` handlers do, and keep
  the request's signatures; the plan records the deviation from the constitution's
  "context as first parameter" rule (FR-009).
- Q: May a developer set a maximum password length below 256 bytes, which can reject passwords
  of 64 non-ASCII characters? → A: No. The configurable maximum ranges from 256 to 4,096 bytes,
  so every configuration accepts 64 characters in any script (FR-028).

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Protect routes and tell failures apart (Priority: P1)

A developer protects a group of routes with one middleware, built from one or more authentication
methods. A request with valid credentials reaches the handler, which can read who made it and how
they proved it. A request with no credentials, or with wrong ones, is rejected with 401 and a
challenge that tells the client how to authenticate. A request that cannot be judged, because a
database or the store is unreachable, is answered with 500: an outage is never reported as a wrong
password.

**Why this priority**: every other story plugs into this one. The three distinct outcomes are also
what makes methods combinable: missing credentials let the next method try, invalid credentials
stop the request.

**Independent Test**: build the middleware from stub methods that return each outcome, alone and
combined, and inspect the status, the challenge header, the arguments of the rejection function
and the principal the handler sees.

**Acceptance Scenarios**:

1. **Given** the middleware with one method, **When** a request carries valid credentials,
   **Then** the handler runs and reads the principal: its subject and the methods used.
2. **Given** methods A, B and C combined in this order, **When** A finds no credentials and B
   accepts the request, **Then** the handler runs with B's principal and C is not consulted.
3. **Given** the same combination, **When** A finds no credentials and B finds invalid
   credentials, **Then** the request is rejected with 401 and C is not consulted.
4. **Given** the same combination, **When** B's lookup fails because the database is unreachable,
   **Then** the response is 500, never 401, and C is not consulted.
5. **Given** a Basic method and a Bearer method combined, **When** a request carries no
   credentials, **Then** the 401 response carries both challenges.
6. **Given** the option that admits anonymous requests, **When** a request carries no credentials,
   **Then** the handler runs with no principal. **When** it carries invalid credentials, **Then**
   it is still rejected with 401.
7. **Given** a custom rejection function, **When** a request is rejected, **Then** the function
   receives the status and the cause, after the challenge header has been set, and can answer with
   an RFC 9457 problem document.
8. **Given** a route that requires the method "mfa", **When** the principal's methods include
   "mfa", **Then** the handler runs. **When** they do not, **Then** the request is rejected with
   the "insufficient methods" cause: 401 with the step-up challenge when the authenticating method
   defines one, 403 otherwise (FR-019).
9. **Given** an authentication method written outside the module, **When** it is combined with the
   module's methods, **Then** it behaves exactly like them, its challenge included, with no change
   to the core.
10. **Given** an invalid configuration, such as no method at all, **When** the middleware is
    built, **Then** construction returns an error that matches the invalid configuration error.

---

### User Story 2 - Hash and check passwords safely (Priority: P1)

At registration the developer hashes the new password and stores the result. At login, the
password checker reads the account through the developer's lookup and verifies the password. An
attacker cannot tell a nonexistent account from a wrong password, by the response or by its
timing. Hashes created by an older system keep working and are upgraded at the next successful
login. Overlong passwords are refused, never truncated.

**Why this priority**: passwords are the first factor of the most common login, and the mistakes
listed in the Context stay invisible until a breach.

**Independent Test**: a lookup over an in-memory map that holds current hashes, legacy bcrypt
hashes and hashes with outdated parameters. Check correct, wrong, overlong and unknown
credentials, make the lookup fail, and compare the verification work done in each case.

**Acceptance Scenarios**:

1. **Given** a password, **When** it is hashed, **Then** the result is an argon2id hash in PHC
   string format with a random salt. **When** the same password is hashed twice, **Then** the two
   strings differ and both verify.
2. **Given** an account with a current hash, **When** it is checked with the correct password,
   **Then** the result is a principal with the account's subject and the methods `["pwd"]`.
3. **Given** the same account, **When** it is checked with a wrong password, **Then** the result
   is the invalid credentials error.
4. **Given** a username the lookup does not find, **When** it is checked, **Then** the result is
   the same invalid credentials error, after the same verification work as for a wrong password.
5. **Given** a lookup that fails, **When** a username is checked, **Then** the result is an error
   that is not the invalid credentials error, so the middleware answers 500.
6. **Given** an account with a bcrypt hash, or an argon2id hash with outdated parameters, **When**
   it is checked with the correct password, **Then** the check succeeds and the optional callback
   receives a new hash with the current parameters. **When** the password is wrong, or the hash is
   already current, **Then** the callback is not called.
7. **Given** a password longer than the maximum length, **When** it is hashed, **Then** hashing
   reports that it is too long. **When** it is checked, **Then** the result is the invalid
   credentials error and no hash is computed.
8. **Given** an account with a bcrypt hash, **When** it is checked with a password longer than 72
   bytes whose first 72 bytes are correct, **Then** the result is the invalid credentials error.
9. **Given** a stored hash that is malformed, uses an unknown algorithm or has parameters outside
   the accepted bounds, **When** the account is checked, **Then** the result is an error that is
   not the invalid credentials error.
10. **Given** the attempt limit enabled with a threshold of 10, **When** a username fails 10
    checks within the window, **Then** the eleventh check is rejected with the "too many attempts"
    error, even with the correct password and without verifying it. The same happens for a
    username that does not exist. **When** the window ends, **Then** checks are verified again.
11. **Given** the attempt limit enabled, **When** a username fails 3 checks and then passes one,
    **Then** its count is cleared.
12. **Given** the attempt limit enabled and an unreachable store, **When** a username is checked,
    **Then** the result is an error that is neither the invalid credentials nor the too many
    attempts error.
13. **Given** the attempt limit enabled, **When** 50 checks of one username with wrong passwords
    run in parallel, **Then** at most 10 of them verify a password.

---

### User Story 3 - Keep a signed-in user in a session (Priority: P1)

The developer's login handler checks the password and issues a session. The browser's following
requests are authenticated through the session cookie, by reading only the store. Logout revokes
the session. The identifier changes at every login, never reaches the store in clear, travels in a
hardened cookie and expires after inactivity and after an absolute lifetime.

**Why this priority**: it is the standard way to keep a browser user signed in, and every one of
its details is a known attack when missed.

**Independent Test**: issue sessions for principals into the in-memory store, send requests with
the resulting cookies while advancing a fake clock, and inspect the cookie attributes and the
store's keys and values.

**Acceptance Scenarios**:

1. **Given** a principal, **When** a session is issued, **Then** the response sets a cookie whose
   name has the `__Host-` prefix and the attributes `Secure`, `HttpOnly`, `SameSite=Lax`,
   `Path=/` and no `Domain`. **When** a request carries that cookie, **Then** it is authenticated
   with the principal's subject and methods.
2. **Given** an issued session, **When** the store is inspected, **Then** no key and no value
   contains the session identifier: the key is derived from its SHA-256 hash.
3. **Given** a request that carries session S, **When** the developer issues a new session in the
   response (a new login), **Then** the cookie carries a new identifier and S no longer
   authenticates any request.
4. **Given** a session, **When** it is revoked, **Then** its entry is deleted from the store, the
   response clears the cookie, and the old cookie no longer authenticates any request.
5. **Given** a session with an idle timeout, **When** no request uses it for longer than that
   timeout, **Then** it no longer authenticates. **When** requests keep using it, **Then** it
   stays valid until its absolute lifetime ends, and then it no longer authenticates.
6. **Given** a session, **When** many requests use it within one renewal interval, **Then** each
   request reads the store once and none writes it. **When** a request arrives after the renewal
   interval, **Then** that request writes it once.
7. **Given** a request authenticated with a session, **When** it is served, **Then** no lookup
   function of the developer is called.
8. **Given** an unreachable store, **When** a request carries a session cookie, **Then** the
   response is 500.
9. **Given** a cookie whose identifier is unknown, expired, revoked or malformed, **When** a
   request carries it, **Then** the session method reports no credentials: another method may
   authenticate the request, and a route that admits anonymous requests serves it.

---

### User Story 4 - Require a second factor (Priority: P2)

For users who enrolled a second factor, the password alone is not enough. After the password, the
developer issues a pending login that lasts a few minutes and authenticates no route. On the next
page the user enters a time-based code, or a recovery code if the device is lost, and only then
gets a full session. Attempts are limited and codes cannot be replayed. Sensitive routes accept
only sessions that passed the second factor.

**Why this priority**: it is what makes a stolen password insufficient. It builds on User Stories
2 and 3.

**Independent Test**: secrets and recovery code hashes in in-memory maps, the in-memory store and
a fake clock. Drive login, pending login, code and session issuance, and submit codes
sequentially and in parallel.

**Acceptance Scenarios**:

1. **Given** an enrolled user whose password was checked, **When** the developer issues a pending
   login, **Then** a request with the pending cookie to a protected route is not authenticated,
   and the second-factor handler can read the pending principal.
2. **Given** a pending login, **When** the current time-based code is submitted, **Then** the
   result is a principal with the methods `["pwd", "otp", "mfa"]`. **When** the developer issues a
   session for it, **Then** the session has a new identifier and the pending login is deleted.
3. **Given** a pending login, **When** the code of the previous or of the next time step is
   submitted, **Then** it is accepted. **When** a code two steps away is submitted, **Then** it is
   rejected.
4. **Given** a code that was accepted, **When** it is submitted again, sequentially or in parallel,
   from the same or from another pending login of the same user, **Then** it is accepted exactly
   once.
5. **Given** a pending login, **When** 5 wrong codes are submitted, **Then** the pending login is
   cancelled: a sixth submission is rejected even with the correct code, and the user must start
   again from the password.
6. **Given** a pending login older than its lifetime, **When** any code is submitted, **Then** it
   is rejected and the user must start again from the password.
7. **Given** a pending login and an unused recovery code, **When** the recovery code is
   submitted, **Then** the result is a principal with the methods `["pwd", "mfa"]` and the
   developer's consume callback marks the code as used. **When** the same code is submitted again,
   sequentially or in parallel, **Then** it is accepted exactly once.
8. **Given** a pending login, **When** 3 wrong time-based codes and 2 wrong recovery codes are
   submitted, **Then** the pending login is cancelled: both kinds of code share the same 5
   attempts.
9. **Given** a user who starts enrollment, **When** the library generates a secret and its
   `otpauth://` URI, **Then** the user is not enrolled until the developer confirms the first
   code. **When** the first code is wrong, **Then** the secret is not confirmed.
10. **Given** a user whose secret lookup reports "not found", **When** enrollment is queried,
    **Then** the user is reported as not enrolled. **When** the lookup or the store fails during a
    verification, **Then** the result is an error that is not the invalid credentials error, and
    no attempt is counted.
11. **Given** a route that requires "mfa", **When** the request carries a session issued after the
    password only, **Then** it is rejected with 403 and the "insufficient methods" cause, so the
    developer's rejection function can redirect to the second-factor page. **When** the session
    was issued after the second factor, **Then** the handler runs.
12. **Given** a request to generate recovery codes, **When** they are generated, **Then** the
    developer receives the codes in clear, to show once, and their hashes, to store.

---

### User Story 5 - Authenticate API clients with API keys (Priority: P2)

The developer generates an API key for a client, shows it once, and stores only its public
identifier and a hash of its secret. Clients send the key with each request. A copied backup of the
database cannot be used to call the service. A key stands out to secret scanners because of its
application prefix.

**Why this priority**: service-to-service and developer access commonly use API keys. It is
independent of passwords and sessions.

**Independent Test**: generate keys, store their records in an in-memory map used by the lookup,
and send requests with valid, wrong, unknown, expired and foreign keys.

**Acceptance Scenarios**:

1. **Given** the prefix "acme", **When** a key is generated, **Then** the key has the form
   `acme_<id>_<secret>`, and the record to store holds the identifier and the SHA-256 hash of the
   secret, never the secret.
2. **Given** a stored key, **When** a request carries it in `Authorization: Bearer`, **Then** it is
   authenticated with the owner as subject and no methods.
3. **Given** a stored key, **When** a request carries the same identifier with a wrong secret, an
   unknown identifier, a key deleted from the database or a key past its expiry, **Then** it is
   rejected as invalid credentials.
4. **Given** a Bearer credential that does not start with `acme_`, such as a JSON Web Token,
   **When** the API key method examines it, **Then** it reports no credentials, so the next method
   can try.
5. **Given** a credential that starts with `acme_` but is malformed, **When** it is examined,
   **Then** it is rejected as invalid credentials.
6. **Given** a custom header configured for keys, **When** a request carries the key in that
   header, **Then** it is authenticated.
7. **Given** a lookup that fails, **When** a request carries a key, **Then** the response is 500.
8. **Given** a route that requires "mfa", **When** a request is authenticated with an API key,
   **Then** it is rejected with 403.

---

### User Story 6 - Accept access tokens from an identity provider (Priority: P2)

The service accepts access tokens issued by an identity provider. Each token is verified with the
provider's published keys, without any call to the developer's database, and its claims reach the
handler. Forged tokens are rejected however they are built. An unreachable key server is an
outage, not a reason to reject users.

**Why this priority**: it is the standard way to authenticate users of a separate identity
provider and calls between services. Independent of passwords, sessions and the store.

**Independent Test**: sign tokens with test keys, serve the key set from an in-memory HTTP
transport with chosen caching headers and failures, advance a fake clock, and send valid and
forged tokens.

**Acceptance Scenarios**:

1. **Given** a key set and a token signed with one of its keys, with the configured issuer and
   audience and an expiry in the future, **When** a request carries it, **Then** it is
   authenticated: the subject is the token's `sub`, the methods are its `amr`, and the handler can
   read the token's other claims, such as roles.
2. **Given** a token with `alg` set to `none`, a token signed with HS256 using an RSA public key as
   the secret, or a token whose `alg` differs from the algorithm of its key, **When** it is
   verified, **Then** it is rejected.
3. **Given** a token without `exp`, **When** it is verified, **Then** it is rejected. **Given** a
   token that expired more than the clock leeway ago, **Then** it is rejected. **Given** a token
   that expired less than the leeway ago, **Then** it is accepted.
4. **Given** a token whose issuer differs from the configured one, or whose audience does not
   contain the configured one, **When** it is verified, **Then** it is rejected. **Given** an
   audience list that contains the configured audience among others, **Then** it is accepted.
5. **Given** the option that requires the type `at+jwt`, **When** a token of type `JWT` (an ID
   token) is verified, **Then** it is rejected. **When** a token of type `application/at+jwt` is
   verified, **Then** it is accepted.
6. **Given** a token with a critical header parameter the module does not support, **When** it is
   verified, **Then** it is rejected.
7. **Given** a remote key set, **When** the authenticator is built, **Then** nothing is downloaded.
   **When** the first token is verified, **Then** the key set is downloaded once. **While** it is
   fresh according to its caching headers, **Then** no further download happens.
8. **Given** a cached key set, **When** a token names an unknown key identifier, **Then** the key
   set is downloaded again, at most once per refresh interval. **When** 1,000 tokens with different
   unknown identifiers arrive within one interval, **Then** at most one download happens. **When**
   the provider rotated its keys and the new key is in the new download, **Then** the token is
   accepted.
9. **Given** a key server that is unreachable, or that answers with an error or an invalid
   document, and no usable cached keys, **When** a token is verified, **Then** the response is
   500, not 401.
10. **Given** a token rejected for any reason, **When** the response is sent, **Then** it is 401
    with the challenge `Bearer error="invalid_token"`.
11. **Given** static keys, **When** tokens are verified, **Then** no network request is ever made.
12. **Given** a token whose header embeds a key or points to a key location, **When** it is
    verified, **Then** the embedded or referenced key is never used.
13. **Given** a route that requires "mfa", **When** a request carries a valid token whose `amr`
    lacks `mfa`, **Then** it is rejected with 401 and the challenge
    `Bearer error="insufficient_user_authentication"` (RFC 9470).

---

### User Story 7 - Serve simple clients with HTTP Basic (Priority: P3)

Scripts and simple tools authenticate with a username and password in HTTP Basic, verified with
the same password checker as the login form.

**Why this priority**: useful for internal tools and legacy clients, but less safe than the other
methods: every request costs a password verification and no second factor is possible.

**Independent Test**: a checker over an in-memory map, and requests with valid, wrong, malformed
and foreign Authorization headers.

**Acceptance Scenarios**:

1. **Given** valid Basic credentials, **When** a request carries them, **Then** it is
   authenticated with the methods `["pwd"]`.
2. **Given** a wrong password or an unknown username, **When** a request carries them, **Then** it
   is rejected with 401 and the challenge `Basic realm="api", charset="UTF-8"`.
3. **Given** a Basic header that is not valid base64 or has no colon, **When** it is examined,
   **Then** it is rejected as invalid credentials.
4. **Given** an Authorization header with another scheme, or none, **When** it is examined,
   **Then** the Basic method reports no credentials.
5. **Given** a password that contains colons and non-ASCII UTF-8 characters, **When** it is sent,
   **Then** it is verified as typed.
6. **Given** a route that requires "mfa", **When** a request is authenticated with Basic, **Then**
   it is rejected with 403.
7. **Given** the password checker with the attempt limit enabled, **When** a username reaches the
   limit, **Then** further Basic requests for it are rejected with 429.

### Edge Cases

- **Two credentials in one request**: when a request carries, for example, both a session cookie
  and a Bearer token, the first method in the configured order that finds credentials decides.
- **Shared Bearer scheme**: API keys and JSON Web Tokens both travel as Bearer credentials. The
  API key method claims only credentials with its prefix, and the token method claims only
  credentials shaped like a compact token, so their order in a combination does not matter.
- **Method that breaks the contract**: a method that returns neither a principal nor an error, or
  a principal with an empty subject, produces 500.
- **Wrapped outcomes**: errors that wrap the no-credentials or invalid-credentials errors are
  classified like them.
- **Cancelled request**: when the client goes away during a lookup, the result is an error, not
  invalid credentials, and no attempt is counted.
- **Cross-origin preflights**: preflight requests carry no credentials. The documentation says to
  place the CORS middleware before authentication, so that preflights are answered before it.
- **Legacy hashes and timing**: verifying a bcrypt hash takes a different time from verifying the
  argon2id dummy hash used for nonexistent accounts. Until an account's hash is upgraded at its
  next login, timing can reveal that the account exists. The documentation states this.
- **Hostile stored hashes**: a stored hash whose parameters would demand excessive memory or time
  is rejected as malformed (User Story 2, scenario 9), never computed.
- **Memory cost of password checks**: each verification uses the memory set by the hash
  parameters. The documentation recommends bounding concurrent requests and rate limiting the
  routes that check passwords.
- **Local development**: browsers accept `Secure` and `__Host-` cookies on `localhost` over plain
  HTTP. The documentation states this.
- **Pending cookie on a protected route**: it authenticates nothing; the session method reports no
  credentials.
- **Codes with leading zeros**: a time-based code such as `012345` is compared as six digits.
- **Malformed codes**: a time-based code that is not exactly six ASCII digits is rejected and
  counts as an attempt.
- **Recovery code input**: recovery codes are compared without regard to case and to separators
  such as spaces and hyphens, because users type them by hand.
- **Store failures on writes**: when the store fails while a session is issued, no cookie is set
  and an error is returned. When it fails while a session is revoked, the cookie is still cleared
  and the error is returned.
- **Store values of an unknown format**: a value the library cannot decode, for example written by
  an incompatible version, never authenticates a request and is reported as an error.
- **Key set content**: keys of unsupported types or algorithms are ignored, and so are symmetric
  keys. The other keys of the set remain usable.
- **Key rotation and the refresh interval**: a token signed with a brand-new key that arrives
  within the refresh interval after the previous download is rejected until the next download is
  allowed. The documentation states this delay.
- **Token without key identifier**: it is verified only when exactly one configured key is bound to
  its algorithm; otherwise it is rejected.
- **Token without `amr`**: the principal has no methods, so a route that requires "mfa" rejects it.
- **Encrypted tokens**: tokens in encrypted (JWE) form are rejected as invalid credentials.
- **Basic and enrolled users**: Basic verifies only the password. The documentation warns that an
  account enrolled in a second factor can still be used with Basic on routes that do not require
  "mfa".
- **Attempt limit and username forms**: attempts are counted for the username exactly as passed
  to the checker. When the developer's lookup ignores case, the documentation says to pass the
  canonical form, so that "Alice" and "alice" share one count.
- **Attempt limit and lockout**: an attacker who reaches the limit for a victim's username blocks
  the victim's logins until the window ends. The documentation states this trade-off and why the
  default window is short.
- **Requirement of no methods**: a route that requires an empty list of methods only requires an
  authenticated principal, which is useful together with the anonymous option.

## Requirements *(mandatory)*

### Functional Requirements

#### Module and shared conventions

- **FR-001**: The feature MUST be delivered as one independent module in this repository, in the
  directory `auth`, with one package for the core and one package for each of: in-memory store,
  passwords, sessions, time-based codes, recovery codes, HTTP Basic, API keys and JSON Web Tokens.
- **FR-002**: The module MUST depend directly on exactly two modules outside the standard
  library: `golang.org/x/crypto`, for argon2id and bcrypt, and `github.com/go-jose/go-jose/v4`,
  for JSON Web Tokens and key sets. Their own requirements may appear as indirect ones. It MUST
  NOT depend on any other module, including the other modules of this repository and any Redis
  client, in its code, tests and examples. Both dependencies are justified
  in the plan per Principle V.
- **FR-003**: Every constructor MUST validate its configuration and report an invalid one as an
  error that matches the module's invalid configuration error, never as a panic or a silent
  fallback. Configuration uses functional options, as in the `middleware` module.
- **FR-004**: The module MUST NOT panic on any input. Every component that reads external input
  MUST have fuzz tests: Authorization headers, cookies, time-based and recovery codes, PHC hash
  strings, API keys, tokens, key set documents and the key server's caching headers.
- **FR-005**: Every component MUST be safe for concurrent use. The module MUST NOT hold
  process-wide state, use a global logger, write to any output on its own, or start background
  work.
- **FR-006**: Every secret the module generates MUST come from a cryptographically secure random
  source: session and pending login identifiers, salts, API key identifiers and secrets,
  time-based code secrets and recovery codes.
- **FR-007**: Every comparison of a secret, a hash or a code with a submitted value MUST take a
  time that does not depend on where the two values differ.
- **FR-008**: No secret MUST reach logs or error messages. Every value returned by the module that
  carries a secret MUST render as `[REDACTED]` when logged with the standard structured logger.
  Error messages MUST NOT contain passwords, codes, keys, tokens, session or pending identifiers,
  secrets or hashes.
- **FR-009**: Operations that perform input or output MUST accept a context and honor its
  cancellation. Operations bound to an HTTP request, which receive the `*http.Request`
  (authenticating, issuing, reading a pending login and revoking), MUST take the context from
  the request and receive no separate context parameter.
- **FR-010**: The README MUST include:
  - a store adapter for Redis of about twenty lines, using `go-redis`, with an atomic increment
    whose expiry is set when the counter is created;
  - why cookie sessions require cross-site request forgery protection, with the standard library's
    cross-origin protection as the example;
  - the CPU and memory cost of HTTP Basic, which verifies a password at every request, and its
    lack of a second factor;
  - the order of the authentication middleware relative to the CORS, rate limiting and
    concurrency middleware of the `middleware` module.

  Every exported capability MUST have a runnable example.

#### Core: outcomes, combination and middleware

- **FR-011**: An authentication method MUST examine a request and produce exactly one of four
  results: a principal; "no credentials", when the request carries none for this method; "invalid
  credentials", when it carries some that are not valid; or any other error, when the method could
  not decide. Results are classified by error identity, so wrapped errors are classified like the
  errors they wrap.
- **FR-012**: A principal MUST carry a non-empty subject, the stable identifier of the user or
  client, and the list of methods used to authenticate, with values from RFC 8176 such as `pwd`,
  `otp` and `mfa`.
- **FR-013**: Developers MUST be able to combine methods in order. The combination tries each
  method in turn:
  - the first principal is the result;
  - "no credentials" moves on to the next method;
  - "invalid credentials" and any other error stop the combination and are its result.

  When every method reports "no credentials", so does the combination. A method that returns
  neither a principal nor an error, or a principal with an empty subject, is treated as an error.
- **FR-014**: The authentication middleware MUST:
  - store the principal in the request context, where handlers can read it, and call the next
    handler;
  - reject a request with no credentials with 401, unless the anonymous option is set, in which
    case it calls the next handler with no principal;
  - reject a request with invalid credentials with 401, also when the anonymous option is set;
  - reject a request refused by the attempt limit of FR-030 with 429;
  - answer 500 to any other error, which it passes to the rejection function and never writes to
    the response.
- **FR-015**: Every 401 response MUST carry the `WWW-Authenticate` header with at least one
  challenge (RFC 9110 §11.6.1), whenever at least one of the combined methods defines a challenge;
  every method of the module defines one. Each method contributes its own challenge, and a
  combination contributes the challenges of all its methods, each identical challenge once. When a
  method rejects invalid credentials, its challenge carries the error details its scheme defines,
  such as `error="invalid_token"` for Bearer (RFC 6750 §3), in place of its plain challenge and of
  any other challenge of the same scheme. Methods whose
  credentials do not travel in the `Authorization` header, such as the session cookie or an API
  key in a custom header, contribute a documented challenge of their own, since no registered
  scheme exists for them; its exact form is fixed in the plan.
- **FR-016**: The rejection response MUST be customizable with a rejection function that receives
  the status and the cause, as a value comparable with the module's sentinel errors, after the
  middleware has set the protocol headers. By default the response is the plain-text status. The
  rejection function and its argument have the same shape as those of the `middleware` module, so
  that a function that writes an RFC 9457 problem document can be written the same way for both.
- **FR-017**: A new authentication method MUST be addable as a new package, inside or outside the
  module, by implementing only the core contract and, optionally, a challenge. The core treats it
  exactly like the module's methods. Adding methods MUST NOT require incompatible changes to the
  core.
- **FR-018**: Developers MUST be able to read the principal of a request from its context, with an
  explicit indication when there is none.
- **FR-019**: Developers MUST be able to protect sensitive routes with a requirement of methods: a
  request passes only when its principal's methods include every required value. A request with
  no principal is rejected with 401 and the challenges of the authentication middleware. A request
  whose principal lacks a required method is rejected with a distinct "insufficient methods"
  cause, and the status depends on the method that authenticated it:
  - a method that defines a step-up challenge answers 401 with it. The token method defines
    `Bearer error="insufficient_user_authentication"` (RFC 9470), because the client can obtain
    a token with stronger authentication;
  - every other method, including sessions, API keys and Basic, answers 403: the request is
    authenticated, and no standard challenge asks for a second factor.

  The rejection response is customizable as in FR-016, so that a browser application can, for
  example, redirect to its second-factor page.

#### Store

- **FR-020**: The store contract MUST offer four operations on string keys and byte values:
  - read, which reports "not found" for a key that is absent or expired;
  - write with an expiry;
  - delete, for which an absent key is not an error;
  - atomic increment of a counter with an expiry, returning the new value.
- **FR-021**: The increment MUST be atomic: N concurrent increments of the same new key return
  each value from 1 to N exactly once. The expiry is set when the counter is created and is not
  extended by later increments. Attempt limits and replay protection rely on this.
- **FR-022**: The library MUST choose the keys and serialize the values, in a format that carries a
  version. Keys MUST NOT contain secrets in clear: a key derived from a secret uses its SHA-256
  hash, for example `session:<hash of the identifier>`. Values MUST NOT contain secrets in clear.
- **FR-023**: An in-memory store MUST implement the contract, for tests and single-instance
  deployments. It is safe for concurrent use, never returns an expired entry, and reclaims the
  memory of expired entries without background work.

#### Passwords

- **FR-024**: Hashing MUST produce argon2id hashes in PHC string format, with a random salt and the
  current parameters. The default parameters are documented and configurable. Hashing MUST reject
  an empty password and a password over the maximum length, with distinct errors.
- **FR-025**: The password checker MUST verify a username and a password through a lookup supplied
  by the developer:
  - success returns a principal with the account's subject and the methods `["pwd"]`;
  - a wrong password returns "invalid credentials";
  - a lookup that reports "not found" returns "invalid credentials";
  - any other lookup error is returned as an error, so that it becomes 500.
- **FR-026**: For a username the lookup does not find, the checker MUST verify the password
  against a dummy hash computed with the current parameters and return the same "invalid
  credentials" error, so that neither the result nor the work done distinguishes it from a wrong
  password.
- **FR-027**: The checker MUST verify argon2id hashes with any parameters within documented bounds,
  and bcrypt hashes. A stored hash that is malformed, uses another algorithm or has parameters
  outside the bounds MUST produce an error that is not "invalid credentials".
- **FR-028**: The maximum password length MUST be configurable in bytes, from 256 to 4,096, with
  a documented default, so that every configuration accepts at least 64 characters in any
  script. A longer password is rejected as "invalid credentials" before
  any hash is computed, and is never truncated. A password longer than 72 bytes never matches a
  bcrypt hash.
- **FR-029**: After a successful verification of a hash that uses another algorithm or outdated
  parameters, the checker MUST compute a new hash with the current parameters and pass it, with
  the subject, to an optional callback that stores it. A failure in the callback does not make the
  login fail. The callback is not called after a failed verification or for a current hash.
- **FR-030**: Developers MUST be able to enable, with an option that takes the store, a limit on
  failed password attempts per username, in addition to the per-IP rate limiting of
  `middleware/ratelimit`:
  - attempts are counted atomically in the store for the username passed to the checker, under a
    key derived from its hash, for existing and nonexistent usernames alike, so that the limit
    reveals nothing about which accounts exist;
  - once the configured number of failures is reached within the configured window, the checker
    rejects every further attempt for that username, correct passwords included, with a distinct
    "too many attempts" error and without verifying the password, until the window ends. The
    authentication middleware answers this error with 429;
  - a successful check clears the count;
  - a lookup or store failure is returned as an error and does not count as an attempt;
  - the threshold and the window are configurable, with documented defaults.

  Without the option, no attempt is counted.
- **FR-031**: The same checker MUST serve both the Basic method and the developer's login handler.

#### Sessions

- **FR-032**: Issuing a session MUST generate a new identifier of at least 256 bits, store the
  session under a key derived from the identifier's hash, set the session cookie, and delete any
  session or pending login the request carried (protection against session fixation). The session
  records the principal's subject and methods and the time of authentication.
- **FR-033**: The session cookie and the pending login cookie MUST have a name with the `__Host-`
  prefix and the attributes `Secure`, `HttpOnly`, `SameSite=Lax` and `Path=/`, and no `Domain`.
  The cookie's lifetime never exceeds the session's absolute lifetime.
- **FR-034**: The session method MUST authenticate a request by reading its cookie and the store,
  without calling any lookup function. It returns a principal with the subject and the methods
  recorded at issuance. A missing cookie, or one whose identifier is malformed, unknown, expired or
  revoked, MUST produce "no credentials". A store failure MUST produce an error.
- **FR-035**: A session MUST expire after an absolute lifetime and after an idle timeout, both
  configurable with documented defaults, whichever comes first.
- **FR-036**: Renewal after activity MUST write the store only when the renewal interval, a
  documented fraction of the idle timeout, has passed since the last write. A session therefore
  costs at most one store write per renewal interval, and every other request costs only reads.
  Renewal never extends a session beyond its absolute lifetime.
- **FR-037**: Revoking MUST delete the session from the store and clear the cookie. Revoking a
  request with no session is not an error, and the cookie is cleared anyway.
- **FR-038**: Developers MUST be able to issue a pending login for a principal that passed the
  first factor. It has its own cookie, a lifetime of a few minutes (configurable, with a
  documented default) and never authenticates a route.
- **FR-039**: Developers MUST be able to read the pending login of a request: its principal, or
  "no credentials" when it is absent, expired or cancelled.
- **FR-040**: Issuing a session for a request that carries a pending login MUST delete the pending
  login.
- **FR-041**: Sessions MUST be authenticated, renewed and revoked by the session method alone, so
  that accounts can live in the developer's database and sessions in the store.

#### Time-based codes

- **FR-042**: Time-based codes MUST follow RFC 6238 with HMAC-SHA-1: 6 digits, a 30-second step
  and a tolerance of one step before and after the current one. The implementation MUST pass the
  test vectors of RFC 4226 Appendix D and RFC 6238 Appendix B.
- **FR-043**: The secret MUST be read through a lookup by subject supplied by the developer. "Not
  found" means the user is not enrolled. Developers MUST be able to ask whether a subject is
  enrolled. Any other lookup error is returned as an error.
- **FR-044**: Verifying a code for a pending login MUST, on success, return a principal with the
  pending login's methods plus `otp` and `mfa`, that is `["pwd", "otp", "mfa"]` after a password.
- **FR-045**: Each pending login MUST allow at most 5 failed second-factor attempts, counted
  atomically in the store and shared between time-based and recovery codes. After the fifth
  failure the pending login is cancelled: every later code is rejected, including a correct one,
  and the user starts again from the password. A failure of the lookup, or of the store while the
  attempt is being counted, is returned as an error and does not count as an attempt. An attempt
  is counted before its code is evaluated, so that parallel requests never evaluate more than 5
  codes; a failure after that point is returned as an error and the attempt stays counted.
- **FR-046**: A code accepted for a subject and a time step MUST NOT be accepted again for that
  subject and step, even when two requests submit it in parallel. The protection is recorded with
  the store's atomic increment.
- **FR-047**: Enrollment MUST let the developer obtain a new secret of at least 160 bits and its
  `otpauth://` URI, with an issuer and an account name, and confirm it with a first code. The
  second factor becomes active only after a correct first code: the developer persists the secret
  as active only after confirmation. The library never writes the candidate secret to the store.

#### Recovery codes

- **FR-048**: Developers MUST be able to generate a set of single-use recovery codes, by default 10,
  each with at least 80 bits of entropy, in a format readable by people. The codes are returned in
  clear, to be shown once, together with their hashes, which are the only form the developer
  stores.
- **FR-049**: Verifying a recovery code for a pending login MUST read the subject's unused hashes
  through a lookup and, on a match, call the developer's consume callback to mark the code as used.
  A code MUST be accepted at most once, even when two requests submit it in parallel. When the
  consume callback reports the code as already used, the code is rejected.
- **FR-050**: A recovery code MUST count as the second factor: on success the principal has the
  pending login's methods plus `mfa`, that is `["pwd", "mfa"]` after a password. It is subject to
  the attempt limit of FR-045.

#### HTTP Basic

- **FR-051**: The Basic method MUST read credentials from the `Authorization` header as RFC 7617
  defines, with the scheme compared without regard to case and credentials decoded as UTF-8, and
  verify them with the password checker. The principal has the methods `["pwd"]`.
- **FR-052**: An `Authorization` header with another scheme, or none, MUST produce "no
  credentials". A Basic header that is not valid base64 or has no colon MUST produce "invalid
  credentials".
- **FR-053**: The Basic challenge MUST be `Basic realm="<realm>", charset="UTF-8"`. The realm is
  configurable, with a documented default. A realm that cannot be represented in a challenge is a
  configuration error.
- **FR-054**: The Basic method MUST NOT offer a second factor.

#### API keys

- **FR-055**: Developers MUST be able to generate API keys of the form `<prefix>_<id>_<secret>`,
  where the prefix is the application's, the identifier is public and random, and the secret has
  at least 256 bits. Generation returns the key in clear, to be shown once, and the record to
  store: the identifier and the SHA-256 hash of the secret. The prefix is validated at
  construction.
- **FR-056**: The API key method MUST read the key from `Authorization: Bearer` by default, or from
  a header configured by the developer. A credential that does not start with the configured
  prefix MUST produce "no credentials". A credential with the prefix that is malformed MUST
  produce "invalid credentials".
- **FR-057**: The method MUST find the key through a lookup by identifier and compare the hash of
  the submitted secret with the stored one. A key that the lookup does not find, a wrong secret
  and a key past its expiry MUST produce "invalid credentials". Any other lookup error is returned
  as an error.
- **FR-058**: An authenticated API key MUST produce a principal whose subject is the key's owner
  and whose methods are empty, since RFC 8176 has no value for API keys.

#### JSON Web Tokens

- **FR-059**: The token method MUST only verify tokens, in compact serialization, read from
  `Authorization: Bearer`. The accepted algorithms are RS256, PS256, ES256, EdDSA with Ed25519, and
  HS256 with a shared key, and this list is always passed to the token parser. A Bearer credential
  that does not have the shape of a compact token MUST produce "no credentials".
- **FR-060**: Each key MUST be bound to exactly one algorithm: the one it declares, or the only one
  its type allows, with RS256 for RSA keys that declare none. A token whose `alg` differs from its
  key's algorithm, or is `none`, MUST be rejected (RFC 8725). Keys embedded in a token's header or
  referenced by it MUST never be used. Symmetric keys from a remote key set MUST be ignored. A
  shared HS256 key shorter than 256 bits is a configuration error.
- **FR-061**: A token MUST be matched to a key by its key identifier. A token without one is
  verified only when exactly one key is bound to its algorithm.
- **FR-062**: Keys MUST come either from static configuration or from a remote key set at an HTTPS
  URL. A remote key set is downloaded at the first verification, not at construction, and cached
  for the lifetime its caching headers state, within documented minimum and maximum bounds, or for
  a documented default when they state none.
- **FR-063**: A token with an unknown key identifier MUST trigger a new download at most once per
  refresh interval, which is configurable with a documented default. Concurrent verifications
  share one download. The HTTP client is configurable, and the size of a downloaded document is
  bounded.
- **FR-064**: When no usable keys can be obtained, because the key server is unreachable or
  answers with an error or an invalid document, verification MUST return an error that becomes
  500, not "invalid credentials". While a refresh fails, the keys downloaded earlier remain in use.
- **FR-065**: The method MUST require the claims as follows:
  - `exp` is required;
  - `exp`, `nbf` and `iat` are checked with a configurable clock leeway, with a documented
    default;
  - `iss` must equal the configured issuer;
  - `aud` must contain the configured audience;
  - `sub` is required and becomes the principal's subject;
  - `amr`, when present, must be a list of strings and becomes the principal's methods.

  The issuer and the audience are required at construction.
- **FR-066**: An option MUST require a specific token type, such as `at+jwt` (RFC 9068), compared
  without regard to case and with the `application/` prefix optional, so that an ID token is not
  accepted as an access token.
- **FR-067**: Developers MUST be able to read, from a principal authenticated with a token, all the
  claims of that token, including custom ones such as roles. For any other principal the absence
  of claims is reported.
- **FR-068**: Every rejected token MUST produce 401 with the challenge `Bearer
  error="invalid_token"` (RFC 6750 §3). The challenge does not reveal why the token was rejected.
- **FR-069**: Tokens in encrypted (JWE) form MUST be rejected. The module supports no critical
  header parameter (RFC 7515 §4.1.11), so a token whose header contains `crit` MUST be rejected,
  whatever it lists.

#### Verification

- **FR-070**: Behavior that depends on time MUST be verified with a fake clock, without real
  waiting: session expiry and renewal, pending login lifetime, time steps, token expiry and leeway,
  key set caching and refresh interval, and expiry in the in-memory store. Key sets MUST be served
  from memory, without network access.
- **FR-071**: The atomicity of the increment MUST be verified with parallel requests, on the
  in-memory store and through the attempt limit and the replay protection that rely on it.
- **FR-072**: Benchmarks MUST measure the cost of a password check and of the verification of a
  session, an API key and a token.

### Key Entities

- **Authentication method**: a component that examines a request and produces a principal, "no
  credentials", "invalid credentials" or an error. It may contribute a challenge.
- **Principal**: the authenticated user or client: a stable subject and the methods used, as
  RFC 8176 values.
- **Challenge**: what a 401 response tells the client about how to authenticate, in the
  `WWW-Authenticate` header.
- **Rejection**: the status and cause of a refused request, passed to the rejection function.
- **Lookup function**: a function of the developer that reads an account, a secret, recovery code
  hashes or an API key from the developer's database, and reports "not found" distinctly from
  failures.
- **Store**: the developer's key-value store with expiry and an atomic increment, holding the
  library's short-lived state.
- **Password hash**: an argon2id hash in PHC string format, or a legacy bcrypt hash.
- **Session**: the server-side record of a signed-in principal, found through the hash of the
  identifier in its cookie, with an absolute lifetime and an idle timeout.
- **Pending login**: the short-lived record of a principal that passed the password and must pass
  the second factor. It carries its attempt counter.
- **Time-based code secret**: the per-user secret of the second factor, held in the developer's
  database, with its `otpauth://` URI at enrollment.
- **Recovery code**: a single-use code shown once; only its hash is stored, by the developer.
- **API key**: a credential of the form `<prefix>_<id>_<secret>`. Its stored record holds the
  identifier, the hash of the secret, the owner and an optional expiry.
- **Token** and **key set**: a signed access token from an identity provider, and the set of
  public keys that verifies it, static or downloaded and cached.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: For a nonexistent account and for a wrong password, the password checker performs
  the same verification work: one verification, with the same algorithm and parameters. In the
  module's benchmarks the two cases differ in time by at most 5%.
- **SC-002**: In 100% of the tested failures of a lookup, the store or a key server, across every
  method, the response is 500 and never 401, and no attempt counter changes.
- **SC-003**: After every flow of this spec runs against the in-memory store, a scan of all its
  keys and values finds 0 occurrences, in clear or encoded, of any password, session or pending
  identifier, API key or secret, time-based code secret or code.
- **SC-004**: When 50 requests submit the same valid time-based code, or the same recovery code,
  in parallel, exactly 1 is accepted. When 50 requests submit wrong codes in parallel for one
  pending login, at most 5 are evaluated, and a correct code submitted afterwards is rejected.
  With the password attempt limit set to 10, 50 parallel checks of one username with wrong
  passwords verify at most 10 passwords.
- **SC-005**: 100% of session issuances produce a new identifier, and an identifier carried before
  an issuance authenticates 0 requests afterwards.
- **SC-006**: A request authenticated with a session makes exactly one store read and 0 calls to
  lookup functions. A session used once per second for a full idle timeout causes at most as many
  store writes as there are renewal intervals in that timeout.
- **SC-007**: 100% of the tokens in the module's forgery corpus are rejected with 401: `alg` set to
  `none`, HS256 signed with an RSA public key, `alg` different from the key's, missing `exp`,
  expiry beyond the leeway, wrong issuer, missing audience, wrong type, unsupported critical
  parameter, embedded or referenced key, encrypted form. 100% of the valid control tokens are
  accepted.
- **SC-008**: 1,000 tokens with distinct unknown key identifiers within one refresh interval cause
  at most 1 download of the key set. While the cached key set is fresh, tokens with known key
  identifiers cause 0 downloads.
- **SC-009**: Verifying a session against the in-memory store, an API key or a token with cached
  keys each costs less than 1% of one password check, as measured by the module's benchmarks.
- **SC-010**: 100% of the test vectors of RFC 4226 Appendix D and RFC 6238 Appendix B pass.
- **SC-011**: When known secrets are sent through every method and every value that carries a
  secret is logged with the standard structured logger, the secrets appear 0 times in the log
  output and in the error messages.
- **SC-012**: Adopting the module adds exactly two third-party modules to a consumer's
  requirements, plus their own requirements, and none of the other modules of this repository.
- **SC-013**: An authentication method defined outside the module, in a test, is combined with the
  module's methods and contributes its challenge, with 0 changes to the core.
- **SC-014**: Every component that reads external input has a fuzz test, and none panics. 100% of
  the acceptance scenarios and edge cases of this spec are covered by automated tests, and
  statement coverage of the module is at least 80%.

## Assumptions

- **Module and versioning**: one module, `github.com/paccolamano/go/auth`, released with
  `auth/vX.Y.Z` tags. The first release is 0.1.0, and releases stay at 0.x until the API is
  considered stable.
- **Go version**: the minimum Go version is the oldest release the constitution supports (1.26 at
  the time of writing). It provides fake-clock testing and cross-origin protection in the standard
  library.
- **Rejection function**: the module defines its own rejection function and rejection type, with
  the same shape as those of the `middleware` module, instead of depending on a tagged version of
  `middleware`. The request allows exactly two dependencies, and `middleware` is at 0.x, so a
  change to its type would force a release of `auth`. A function that writes a problem document
  reads the same two fields, status and cause, in both modules.
- **Expired session cookies**: a session cookie that no longer identifies a live session reports
  "no credentials" rather than "invalid credentials" (FR-034). Browsers send cookies on their own,
  and a stale cookie must not lock a user out of routes that admit anonymous requests, nor stop
  another method from authenticating the request.
- **Recovery codes and RFC 8176**: RFC 8176 has no value for recovery codes, so a recovery code
  adds only `mfa` to the methods (FR-050), as API keys add none (FR-058). A route that requires
  `otp` therefore rejects a recovery login, and a route that requires `mfa` accepts it.
- **Shared attempt budget**: time-based and recovery codes share the 5 attempts of a pending login
  (FR-045). Separate budgets would give an attacker 10.
- **Time-based codes**: HMAC-SHA-1 and secrets of 160 bits are what authenticator apps support
  universally. The candidate secret during enrollment stays with the developer, so that the store
  never holds a secret (FR-022).
- **Passwords**: passwords are compared as the bytes submitted, without Unicode normalization,
  which would need a further dependency. Password strength rules and breached-password checks are
  the developer's.
- **Key sets**: remote key sets must use HTTPS. RSA keys that declare no algorithm are bound to
  RS256, the most common choice of identity providers that omit it.
- **Defaults chosen in the plan**: argon2id parameters and their accepted bounds, the maximum
  password length, the threshold and window of the password attempt limit, session lifetimes and the renewal fraction, the pending login lifetime, the
  cookie names, the challenge of methods without a registered scheme, the clock leeway, the key
  set cache bounds and refresh interval, the maximum key set size, and the format of identifiers
  and recovery codes are fixed and documented in `/speckit-plan`.
- **Out of scope**:
  - authorization (roles, permissions, policies): the application implements it on top of the
    principal;
  - pages, forms and handlers for login and logout: the developer writes them with the module's
    checking, issuing, pending and revoking operations;
  - QR code generation: the module provides the `otpauth://` URI;
  - issuing tokens: the module only verifies them;
  - encrypted tokens (JWE);
  - per-IP rate limiting: `middleware/ratelimit` provides it.
- **Tests**: tests are hermetic and deterministic (Principle III). Time is faked and key sets are
  served from memory (FR-070). Benchmarks back SC-001 and SC-009.

## Appendix: original request

The request, as submitted to `/speckit-specify`:

````text
Implementiamo un modulo auth per autenticare le richieste HTTP, con un'interfaccia piccola e un sotto-package per ogni metodo di autenticazione.

Il problema di partenza

Ogni servizio riscrive l'autenticazione a mano, e le parti che contano sono proprio quelle facili da sbagliare:

- dal tempo di risposta si capisce se un utente esiste (user enumeration);
- un database irraggiungibile risponde 401 come una password sbagliata;
- gli hash delle password usano algoritmi o parametri deboli. bcrypt usa solo i primi 72 byte della password: x/crypto/bcrypt rifiuta le password più lunghe quando crea un hash, ma le accetta quando lo verifica;
- i cookie di sessione mancano di flag, l'ID non cambia al login (session fixation) e nel database finisce l'ID in chiaro;
- un codice TOTP si indovina per forza bruta se i tentativi non sono limitati, e si riusa se manca la protezione dal replay;
- un JWT viene accettato con l'algoritmo indicato nel suo stesso header, compreso none, oppure con una chiave pubblica RSA usata come segreto HMAC;
- le API key vengono salvate in chiaro nel database: chi legge un backup può usarle.

Una funzione di poche righe non risolve nessuno di questi punti: è il motivo per cui ha senso farne una libreria.

Il principio: la libreria non conosce il database

La libreria si occupa di protocollo e crittografia. Lo sviluppatore fornisce due punti di integrazione, indipendenti tra loro:

1. funzioni di lookup che leggono dal suo database (account, hash delle password, segreti TOTP, API key);
2. uno Store key-value con scadenza, per lo stato temporaneo della libreria (sessioni, login a metà, contatori), per esempio su Redis.

Gli utenti possono stare su Postgres e le sessioni su Redis. A ogni richiesta autenticata con una sessione si interroga solo lo Store. I JWT non richiedono né lookup né Store: si verificano con le chiavi dell'emittente.

Metodi della prima fase

Login con password e sessione, secondo fattore TOTP con codici di recupero, HTTP Basic, API key e JWT. Il nucleo deve permettere di aggiungere altri metodi come nuovi sotto-package, senza modifiche incompatibili.

Cosa dovrebbe fare

1. Il nucleo (auth)

```go
type Authenticator interface {
	Authenticate(r *http.Request) (*Principal, error)
}

type Principal struct {
	Subject string   // identificativo stabile dell'utente
	Methods []string // metodi usati, con i valori di RFC 8176: "pwd", "otp", "mfa"
}
```

Authenticate ha tre esiti distinti, necessari per combinare più metodi:

- ErrNoCredentials: le credenziali mancano; auth.Any passa al metodo successivo.
- ErrInvalidCredentials: le credenziali ci sono ma non sono valide; 401 subito, senza ripiegare su un altro metodo.
- Qualsiasi altro errore (Store o database irraggiungibile): 500, mai 401.

```go
mw, err := auth.New(auth.Any(sessions, keys, tokens)) // func(http.Handler) http.Handler
p, ok := auth.FromContext(r.Context())
sensitive := auth.RequireMethods("mfa")            // rotte sensibili: serve il secondo fattore
```

- auth.New rifiuta le richieste non autenticate con 401 e l'header WWW-Authenticate (RFC 9110 §11.6.1). Ogni metodo fornisce il proprio challenge, e con Any i challenge si sommano. Un'opzione ammette le richieste anonime.
- Stesse convenzioni del modulo middleware: forma func(http.Handler) http.Handler, opzioni funzionali, ErrInvalidConfig, risposta di rifiuto personalizzabile con una RejectFunc, così chi usa problem risponde in formato RFC 9457.

2. Lo Store

```go
type Store interface {
	Get(ctx context.Context, key string) ([]byte, error) // ErrNotFound se assente o scaduta
	Set(ctx context.Context, key string, value []byte, ttl time.Duration) error
	Delete(ctx context.Context, key string) error
	Incr(ctx context.Context, key string, ttl time.Duration) (int64, error) // atomico
}
```

- La libreria sceglie le chiavi e serializza i valori con un formato versionato. Le chiavi non contengono mai segreti in chiaro: per esempio session:<sha256 dell'ID>.
- Incr deve essere atomico: limita i tentativi e blocca il replay anche con richieste parallele.
- Un sotto-package memstore fornisce un'implementazione in memoria, per i test e per le installazioni a istanza singola.
- Il README mostra un adattatore Redis di una ventina di righe con go-redis. Il modulo non dipende da go-redis.

3. Le password (password)

```go
hash, err := password.Hash(plain) // alla registrazione: stringa in formato PHC, argon2id

lookup := func(ctx context.Context, username string) (password.Account, error) {
	u, err := db.UserByUsername(ctx, username)
	if errors.Is(err, sql.ErrNoRows) {
		return password.Account{}, password.ErrNotFound
	}
	if err != nil {
		return password.Account{}, err // diventa 500, mai 401
	}
	return password.Account{Subject: u.ID, Hash: u.PasswordHash}, nil
}

checker, err := password.NewChecker(lookup)
p, err := checker.Check(ctx, username, plain) // p.Methods = ["pwd"]
```

- I nuovi hash usano argon2id. Gli hash bcrypt già presenti nel database si verificano comunque.
- Quando l'utente non esiste, Check verifica un hash fittizio con gli stessi parametri e restituisce lo stesso ErrInvalidCredentials.
- Quando un hash usa parametri superati, dopo una verifica riuscita Check ne calcola uno nuovo e lo passa a una callback facoltativa che lo salva.
- Lo stesso checker serve sia a basic sia all'handler di login dello sviluppatore.

4. Le sessioni (session)

```go
sessions, err := session.New(store)

// POST /login, scritto dallo sviluppatore
p, err := checker.Check(ctx, email, plain)
enrolled, err := otp.Enrolled(ctx, p.Subject)
if enrolled {
	sessions.IssuePending(w, r, p) // login a metà: non autentica nessuna rotta
	// redirect alla pagina del codice
	return
}
sessions.Issue(w, r, p)

// POST /logout
sessions.Revoke(w, r)
```

- session implementa Authenticator: legge il cookie e cerca la sessione nello Store.
- ID generati con crypto/rand, di almeno 256 bit. Nello Store finisce solo il loro hash.
- Issue genera sempre un ID nuovo e cancella la sessione precedente (session fixation).
- Cookie con prefisso __Host-, Secure, HttpOnly, SameSite=Lax.
- Scadenza assoluta e scadenza per inattività. Il rinnovo per inattività riscrive lo Store solo quando serve, non a ogni richiesta.
- Il login a metà dura pochi minuti e Authenticate lo ignora.
- Il README spiega che le sessioni basate su cookie richiedono una protezione CSRF e mostra http.CrossOriginProtection della libreria standard.

5. Il secondo fattore (totp e recovery)

```go
otp, err := totp.New(lookupSecret, store)

// POST /login/otp
pending, err := sessions.Pending(r)
p, err := otp.Verify(ctx, pending, r.FormValue("code")) // p.Methods = ["pwd","otp","mfa"]
sessions.Issue(w, r, p)                                 // nuovo ID; il login a metà viene cancellato
```

totp:

- RFC 6238 su crypto/hmac: 6 cifre, step di 30 secondi, tolleranza di ±1 step.
- Il segreto sta nel database dello sviluppatore e si legge con una lookup func(ctx, subject) ([]byte, error). ErrNotFound significa che il secondo fattore non è attivo.
- Massimo 5 tentativi per login a metà, contati con Incr. Poi il login a metà viene cancellato e si ricomincia dalla password.
- Protezione dal replay con Incr su utente e step: un codice già usato viene rifiutato anche se arrivano due richieste parallele.
- Attivazione: la libreria genera il segreto e l'URI otpauth://. Il secondo fattore diventa attivo solo dopo un primo codice corretto.

recovery:

- Genera un insieme di codici monouso, mostrati in chiaro una sola volta. Lo sviluppatore ne salva solo gli hash.
- La verifica usa una lookup degli hash e una callback Consume che segna il codice come usato nel database dello sviluppatore.
- Un codice di recupero vale come secondo fattore, con lo stesso limite di tentativi del TOTP.

6. HTTP Basic (basic)

```go
basicAuth, err := basic.New(checker, basic.WithRealm("api"))
```

- Usa il checker delle password. Challenge: Basic realm="api", charset="UTF-8" (RFC 7617).
- Il secondo fattore non è supportato: con Basic non c'è un secondo passo in cui chiederlo.
- Il README avverte che ogni richiesta esegue argon2id, che ha un costo di CPU.

7. Le API key (apikey)

```go
keys, err := apikey.New("acme", func(ctx context.Context, id string) (apikey.Account, error) {
	k, err := db.APIKeyByID(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return apikey.Account{}, apikey.ErrNotFound
	}
	if err != nil {
		return apikey.Account{}, err // diventa 500, mai 401
	}
	return apikey.Account{Subject: k.OwnerID, Hash: k.Hash, ExpiresAt: k.ExpiresAt}, nil
})

key, stored, err := keys.Generate() // key in chiaro, mostrata una sola volta: "acme_<id>_<segreto>"
// lo sviluppatore salva stored.ID, stored.Hash e il proprietario nel suo database
```

- Formato della chiave: prefisso dell'applicazione, ID pubblico e segreto generato con crypto/rand (almeno 256 bit). L'ID serve alla lookup. Il prefisso rende le chiavi riconoscibili dagli scanner di segreti.
- Nel database finisce solo lo SHA-256 del segreto: con 256 bit di entropia un hash lento non serve, e la verifica a ogni richiesta resta economica. Confronto a tempo costante.
- La chiave si legge da Authorization: Bearer, con header configurabile. Lo schema Bearer è condiviso con jwt: una credenziale senza il prefisso configurato restituisce ErrNoCredentials, così auth.Any passa al metodo successivo.
- Una chiave cancellata dal database o con ExpiresAt passato dà ErrInvalidCredentials.
- Methods resta vuoto: RFC 8176 non ha un valore per le API key, quindi RequireMethods("mfa") le rifiuta.

8. I JWT (jwt)

```go
tokens, err := jwt.New(
	jwt.WithIssuer("https://id.example.com"),
	jwt.WithAudience("orders-api"),
	jwt.WithJWKS("https://id.example.com/.well-known/jwks.json"),
)

claims, ok := jwt.Claims(p) // claim aggiuntivi, per esempio i ruoli
```

- Si appoggia a github.com/go-jose/go-jose/v4, che si occupa di: parsing della serializzazione compatta, verifica delle firme, parsing di JWK e JWKS, rifiuto degli header crit non supportati (RFC 7515 §4.1.11), validazione di iss, aud, nbf, iat ed exp con tolleranza sull'orologio. Il modulo aggiunge solo ciò che go-jose non fa, descritto nei punti seguenti.
- Solo verifica, in serializzazione compatta. Algoritmi ammessi: RS256, PS256, ES256, EdDSA (Ed25519) e HS256 con chiave condivisa. La lista viene sempre passata a go-jose.
- L'algoritmo è legato alla chiave e mai scelto dall'header: un alg diverso da quello della chiave, o none, viene rifiutato. È la difesa contro la confusione tra algoritmi descritta in RFC 8725.
- Chiavi statiche oppure JWKS remoto. Il JWKS si scarica al primo uso e resta in cache secondo Cache-Control. Un kid sconosciuto provoca un nuovo download al massimo una volta per intervallo, così token con kid inventati non diventano un attacco al server delle chiavi. Nessuna goroutine in background; client HTTP configurabile.
- Claim: exp obbligatorio, perché go-jose lo controlla solo quando è presente; tolleranza sull'orologio configurabile; iss uguale a quello configurato; aud deve contenere l'audience configurata. sub diventa Principal.Subject; amr, se presente, diventa Principal.Methods.
- Un'opzione richiede un typ specifico, per esempio at+jwt (RFC 9068), così un ID token non viene accettato come access token.
- Rifiuto con 401 e challenge Bearer error="invalid_token" (RFC 6750 §3). Un JWKS irraggiungibile è un errore di infrastruttura (500), non un 401.

Requisiti di correttezza

Sono il motivo per cui ha senso farne una libreria invece di copiare codice da un progetto all'altro:

- Tempo indistinguibile tra utente inesistente e password sbagliata. Confronti dei segreti a tempo costante.
- 401 solo per credenziali assenti o non valide. Un errore della lookup o dello Store produce 500 e non conta come tentativo fallito.
- Lunghezza massima della password, con rifiuto oltre il limite e mai troncamento: protegge argon2id dagli input enormi ed evita il troncamento di bcrypt a 72 byte.
- Segreti, ID di sessione e codici generati con crypto/rand. Nessun segreto in chiaro nello Store.
- Nessun segreto nei log o nei messaggi d'errore: i tipi che contengono segreti implementano slog.LogValuer e si rendono come [REDACTED].
- L'atomicità di Incr è verificata con richieste parallele, anche sul memstore.
- Vettori di test di RFC 4226 (Appendix D) e RFC 6238 (Appendix B).
- Fuzz test per tutto ciò che legge input esterno: header Authorization, cookie, codici, stringhe PHC, API key, token JWT, documenti JWKS.
- Scadenze, step TOTP, scadenza dei JWT e cache del JWKS verificati con testing/synctest, senza attese reali. Il JWKS si serve in memoria, senza rete.
- Benchmark del costo di Check e della verifica di una sessione, di una API key e di un JWT.

Dipendenze

Due dipendenze, da giustificare nel piano secondo il principio V della constitution:

- golang.org/x/crypto per argon2id e bcrypt: licenza BSD-3, mantenuto dal team Go. I package argon2 e bcrypt importano solo golang.org/x/sys/cpu, ma il go.mod di x/crypto richiede anche x/net e x/term.
- github.com/go-jose/go-jose/v4 per jwt: licenza Apache-2.0, nessuna dipendenza, usato anche da coreos/go-oidc. È preferito a golang-jwt/jwt perché include il parsing di JWK e JWKS, che altrimenti richiederebbe codice nostro o altre librerie.

Nessun'altra dipendenza.

Cosa lascerei fuori

- Autorizzazione (ruoli, permessi, policy): l'applicazione la fa con auth.FromContext.
- Pagine, form e handler di login e logout: li scrive lo sviluppatore usando Check, Issue, Pending e Revoke.
- La generazione del QR code: la libreria fornisce l'URI otpauth://.
- L'emissione dei JWT: la libreria li verifica soltanto, li emette un identity provider.
- I JWT cifrati (JWE).
- Il rate limiting per indirizzo IP: c'è già middleware/ratelimit.

Decisioni da prendere

1. Logout ovunque: revocare tutte le sessioni di un utente, per esempio dopo un cambio password, richiede un indice per utente e un metodo in più nello Store. Lo vogliamo?
2. Limite di tentativi per account sul login con password (con Incr), in aggiunta al ratelimit per IP?
3. Status di RequireMethods quando manca il secondo fattore. Per i token Bearer, RFC 9470 prevede 401 con error="insufficient_user_authentication". Per le sessioni: 401 o 403?
4. RejectFunc: riusare il tipo del modulo middleware, con una dipendenza da una sua versione taggata, oppure definirne uno identico in auth?
````

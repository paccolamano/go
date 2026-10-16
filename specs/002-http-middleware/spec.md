# Feature Specification: Reusable HTTP Middleware Module

**Feature Branch**: `002-http-middleware` (suggested; not yet created)

**Created**: 2026-10-16

**Status**: Draft

**Input**: User description: "Crea un nuovo package golang chiamato middleware. Questo package conterrà una serie di middleware riutilizzabili in vari progetti e per questo motivo dovranno tutti utilizzare l'option patttern per permettere l'aggiunta di nuove funzionalità pur mantenendo la retro-compatibilità delle api esposte. Ogni middleware avrà un suo subpackage dedicato mentre nella root del package middleware dovremmo inserire una chain che permette di concatenare piu middleware tra loro e nell'ordine scelto da chi utilizza l'api. i middleware da creare in fase iniziale saranno i seguenti: 1- bodylimit 2- accesslog 3- cors 4- recovery 5- requestid 6- secureheaders 7- timeout 8- realip 9- compress 10- decompress 10- etag 11- ratelimit 12- maxconcurrency 13- contenttype. Questo elenco contiene dei middleware che hanno bisogno di chiarimenti: ad esempio compress e decompress potrebbero essere uniti in un solo middleware? il middleware timeout è realmente necessario oppure http.Server della libreria standard permette già di configurare opzioni simili? di conseguenza non è da intendersi come lista definitiva ma solamente come spunto per quello che dovrà essere implementato"

## Context

Every HTTP service needs the same cross-cutting behaviors: request logging, panic recovery,
CORS, size and rate limits, compression, and so on. Each project re-implements them, often
with subtle security mistakes such as trusting forged client addresses or allowing every CORS
origin with credentials. This feature implements them once, as a new independent module named
`middleware` in this repository (Constitution, Principle I):

- the module root provides a **chain** that composes several middleware in the order chosen by
  the developer;
- each middleware lives in its own dedicated subpackage, so a project imports only what it uses;
- every middleware is configured through **options**, so later releases can add capabilities
  without breaking existing call sites (Principle II).

The Go language, the standard HTTP handler model and the module layout are constraints given by
the request and by the constitution (v1.2.0). They are not design choices made here. HTTP status
codes and header names appear in this spec because they are the observable contract of each
middleware. How each middleware is built is left to `/speckit-plan`.

**Actors**:

- **Developer**: builds an HTTP service and composes and configures the middleware.
- **Operator**: runs the service, reads its logs and configures the proxies or load balancers in
  front of it.
- **Client**: the browser or program that sends requests and observes the responses.
- **Maintainer**: evolves the library and publishes its releases.

### Scope review of the proposed list

The request presents the list as a starting point. Each item was checked against what the
standard library and its HTTP server already provide:

| Middleware | Decision | Rationale |
|------------|----------|-----------|
| bodylimit | Keep | The standard library can cap how much of a body is read, but it does not reject an oversized declared body up front with 413 and a customizable response. |
| accesslog | Keep | The standard server does no access logging. |
| cors | Keep | Not provided by the standard library. |
| recovery | Keep | The standard server survives a handler panic, but it drops the connection without a response and writes an unstructured message to its error log. This middleware returns a proper 500 and a structured log record that can be correlated with the request. |
| requestid | Keep | Not provided. |
| secureheaders | Keep | Not provided. |
| realip | Keep | Not provided, and getting it wrong is a security problem: other middleware such as ratelimit and accesslog rely on the client address. |
| compress, decompress | Keep, as two separate middleware (FR-053) | They work in opposite directions and carry different risks: response compression is almost always wanted, while request decompression is rarely needed and exposes the service to decompression bombs. Merging them would tie together two behaviors that must be enabled independently. |
| etag | Keep | The standard library evaluates conditional requests only for content it serves itself, and it never generates entity tags for dynamic responses. |
| ratelimit | Keep | Not provided. |
| maxconcurrency | Keep | The standard server has no limit on in-flight requests per handler. |
| contenttype | Keep, as validation of the request media type | Not provided. Other readings of the name are listed under Assumptions. |

The following were considered and excluded from this feature:

- CSRF protection: the standard library has provided cross-origin request protection since
  Go 1.25.
- Authentication and authorization: application-specific.
- Metrics and tracing: they depend on third-party ecosystems (Principle V).
- Rewriting the scheme or host from forwarding headers: a separate concern from the client
  address.
- HTTP method override.

### Clarifications

#### Session 2026-10-16

- Q: Should compress and decompress be merged into one middleware? → A: No. They are two
  separate middleware in two subpackages, `compress` and `decompress`, sharing codec support
  internally (FR-053).
- Q: Should developers be able to customize the responses that middleware send to clients? → A:
  Yes, through a single standard shared by every middleware that refuses or fails a request
  (FR-010; User Story 1, scenarios 8 and 9).
- Q: How much dedicated support should secureheaders give the content security policy? → A:
  Enforced and report-only policies plus a per-request nonce with an accessor (FR-044; User Story
  3, scenarios 6 and 7).
- Q: Which headers should be added to the secureheaders defaults? → A: Permissions-Policy and
  Cross-Origin-Embedder-Policy (FR-043).

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Compose a request pipeline in a chosen order (Priority: P1)

A developer building a service declares, in one place and in the order they want, the
middleware a group of routes goes through. They configure each middleware only where the
defaults do not fit. The base pipeline can be reused, and extended for some route groups, for
example with stricter limits on admin routes. Code written against one version keeps compiling
and behaving the same when later versions add new options.

**Why this priority**: every other story relies on the chain and on the shared conventions
(options, defaults, configuration errors, interoperability). Without them, the middleware are
just unrelated functions.

**Independent Test**: build a chain from three test middleware that record when they run and
apply it to a test handler. Check the order in which the request and the response pass through
each middleware. Extend the chain and check that the original is unchanged. Construct each
library middleware with its mandatory settings only, and check the documented defaults.

**Acceptance Scenarios**:

1. **Given** a chain of middleware A, B, C applied to handler H, **When** a request is served,
   **Then** the request passes through A, B, C, H in that order and the response passes back
   through C, B, A.
2. **Given** an empty chain applied to H, **When** a request is served, **Then** the result is
   identical to calling H directly.
3. **Given** a base chain (A, B) that is extended with D for admin routes, **When** requests hit
   an admin route and a public route, **Then** the admin request goes through A, B, D, H, the
   public request goes through A, B, H, and the base chain is unchanged.
4. **Given** any middleware of the library, **When** the developer constructs it with only its
   mandatory settings, **Then** it works with the documented defaults, and each option the
   developer adds changes only the setting it names.
5. **Given** consumer code written against one release, **When** a later minor release adds a
   new option to a middleware, **Then** the consumer code compiles and behaves the same without
   changes.
6. **Given** an invalid configuration, such as a negative size limit or a CORS policy that
   allows any origin with credentials, **When** the middleware is constructed, **Then** the
   developer receives an error that names the middleware and the invalid setting, before any
   request is served.
7. **Given** a third-party middleware with the standard shape, **When** it is placed between
   library middleware in a chain, **Then** it runs in its declared position like the others.
8. **Given** one function supplied by the developer that writes JSON error documents and is
   configured on every middleware that can refuse a request, **When** bodylimit, contenttype,
   ratelimit, maxconcurrency, decompress or recovery refuses or fails a request, **Then** the
   client receives that JSON document with the middleware's status code. The function can tell
   which condition caused the refusal, even when two conditions share a status code (415 from
   contenttype and from decompress).
9. **Given** a replacement response function that writes nothing, **When** a middleware refuses
   a request, **Then** the client still receives the middleware's status code, and the request
   never appears successful.

---

### User Story 2 - Keep the service observable and resilient (Priority: P1)

An operator wants every request to be logged with enough detail to diagnose it, correlated by
a request identifier and attributed to the real client address even behind proxies. A bug in
one handler must not take down the service or hide what happened.

**Why this priority**: these are the middleware every production service needs from day one
(recovery, requestid, accesslog, realip). Without them, the other behaviors cannot be diagnosed.

**Independent Test**: serve a chain of realip, requestid, accesslog and recovery, with a logger
that captures records. Use a handler that panics on one path, and send requests from trusted
and untrusted test peers, with and without forwarding headers and request identifiers.

**Acceptance Scenarios**:

1. **Given** a handler that panics before writing anything, **When** a request reaches it,
   **Then** the client receives 500 with a generic body, a log record contains the failure value,
   the stack trace and the request identifier, and the next request is served normally.
2. **Given** a handler that panics after it has started writing the response, **When** a
   request reaches it, **Then** the client can detect that the response is incomplete, and the
   failure is logged.
3. **Given** a request without an identifier, **When** it is served, **Then** a new identifier is
   generated, returned in the response header, available to the handler and included in the
   access log record.
4. **Given** a request carrying a valid identifier, **When** it is served, **Then** that
   identifier is reused. **Given** an identifier that is too long or contains forbidden
   characters, **Then** a new one is generated in its place.
5. **Given** a service behind a trusted proxy and a request whose forwarding header lists
   `forged, client`, **When** it is served, **Then** the client address used downstream is
   `client`.
6. **Given** a request sent directly by an untrusted peer with a forged forwarding header,
   **When** it is served, **Then** the header is ignored and the peer address is used.
7. **Given** any completed request, **When** the access log record is inspected, **Then** it
   contains the method, path, protocol, status, response size, duration, client address, user
   agent and request identifier. The query string and the header values are absent unless the
   developer enabled them.

---

### User Story 3 - Harden the HTTP surface (Priority: P2)

A developer wants safe browser-facing defaults and wants malformed or oversized input rejected
before it reaches application code: security headers, a correct CORS policy, a body size limit
and media-type validation.

**Why this priority**: high value for any service exposed to browsers or to the internet, but
each piece can be adopted on its own after the P1 baseline.

**Independent Test**: apply each middleware (secureheaders, cors, bodylimit, contenttype) on its
own to a test handler that records whether it was invoked. Then send compliant and
non-compliant requests and check the response statuses and headers.

**Acceptance Scenarios**:

1. **Given** secureheaders with defaults, **When** any response is sent, **Then** it carries the
   documented security headers and no Strict-Transport-Security. **Given** a handler that sets one
   of those headers itself, **Then** the handler's value is sent.
2. **Given** cors allowing `https://app.example.com`, **When** a preflight request arrives from
   that origin for an allowed method, **Then** the middleware answers 204 with the allowed methods,
   allowed headers and cache duration, and the handler is not invoked.
3. **Given** the same policy, **When** a request arrives from `https://evil.example`, **Then** the
   handler runs, but the response carries no CORS headers that grant access.
4. **Given** bodylimit of 1 MiB, **When** a request declares a 5 MiB body, **Then** it is refused
   with 413 and the handler is not invoked. **When** a request of undeclared size streams more
   than 1 MiB, **Then** the handler's read fails with a "body too large" condition it can
   recognize.
5. **Given** contenttype allowing `application/json`, **When** a request with a `text/plain` body
   arrives, **Then** it is refused with 415. **When** the body is `application/json;
   charset=utf-8`, **Then** it passes. **When** a GET request has no body, **Then** it passes.
6. **Given** secureheaders with a content security policy that references the nonce and the
   nonce enabled, **When** two requests are served, **Then** each response's policy carries a
   different nonce, and the handler of each request retrieves exactly the nonce of its own
   response.
7. **Given** secureheaders with an enforced policy and a stricter report-only policy, **When** a
   response is sent, **Then** it carries both policies: the browser enforces only the first and
   reports violations of the second.

---

### User Story 4 - Protect capacity under load and abuse (Priority: P2)

A developer wants to stop a single client from monopolizing the service, and to shed load when
too many requests are being processed at once, so the service degrades predictably instead of
collapsing.

**Why this priority**: important for production robustness, but it requires the client address
to be resolved correctly first (Story 2).

**Independent Test**: use a controllable clock and handlers that block until released. Exceed
the per-client rate and the concurrency limit, then check the refusals, the recovery of the
allowance over time, and that no slot is ever leaked.

**Acceptance Scenarios**:

1. **Given** ratelimit allowing 10 requests per minute per client, **When** one client sends an
   11th request within the minute, **Then** that request is refused with 429 and a Retry-After
   value, and another client's requests are unaffected.
2. **Given** a client that hit its limit, **When** enough time has passed, **Then** its requests
   are accepted again.
3. **Given** ratelimit keyed by API key instead of address, **When** two keys share one address,
   **Then** each key has its own allowance.
4. **Given** maxconcurrency of 2 and two requests in progress, **When** a third request arrives,
   **Then** it is refused immediately with 503. **When** one of the first two completes, **Then**
   a new request is accepted.
5. **Given** maxconcurrency with a waiting time configured, **When** a request arrives at
   capacity, **Then** it waits and proceeds if a slot frees up in time. It is refused with 503 if
   the wait expires, and it stops waiting without invoking the handler if the client goes away.
6. **Given** a handler that panics or whose client disconnects, **When** the request ends,
   **Then** its concurrency slot is released.

---

### User Story 5 - Reduce bandwidth and avoid redundant transfers (Priority: P3)

A developer wants to compress large textual responses, accept compressed uploads where clients
send them, and let clients skip downloading content they already have.

**Why this priority**: an optimization. It is valuable but not required for correctness or
safety, and it interacts with caching, so it builds on the earlier stories.

**Independent Test**: serve fixed textual and binary payloads of several sizes, with and without
Accept-Encoding and If-None-Match, plus compressed request bodies. Check encodings, Vary,
entity tags, 304 answers and decoded bodies.

**Acceptance Scenarios**:

1. **Given** a client accepting gzip, **When** it requests a 10 KiB JSON document, **Then** the
   response is gzip-encoded, declares its encoding and varies by Accept-Encoding.
2. **Given** the same client, **When** it requests a 200-byte response or a PNG image, **Then**
   the response is sent uncompressed and still varies by Accept-Encoding.
3. **Given** a handler streaming events and flushing after each one, **When** compression is
   active, **Then** each event reaches the client promptly, without waiting for the response to
   end.
4. **Given** request decompression enabled, **When** a request arrives with a gzip-encoded body,
   **Then** the handler reads the decoded body. **When** the request declares an unsupported
   encoding, **Then** it is refused with 415 and the response lists the supported encodings.
   **When** the decoded body would exceed the decoded-size limit, **Then** reading fails with a
   "body too large" condition.
5. **Given** etag, **When** a client fetches a resource and then repeats the request with
   If-None-Match set to the received tag, **Then** the second response is 304 with no body.
6. **Given** etag behind compression, **When** the same resource is served compressed and
   uncompressed, **Then** the two representations carry different validators.

### Edge Cases

- **Shared instances**: the same ratelimit or maxconcurrency instance placed in two chains shares
  its counters across both; separate instances are independent.
- **Chain order mistakes**: accesslog placed before realip logs the proxy address. Recovery only
  catches failures in middleware placed after it. CORS placed after ratelimit leaves 429
  responses without CORS headers, so browser scripts cannot read them. Each case MUST be covered
  by the ordering documentation (FR-012).
- **Informational responses**: when a handler sends informational (1xx) responses, for example
  Early Hints, middleware that observe or alter the response forward them unchanged and treat
  only the final response as the request's status.
- **Handler writes nothing**: accesslog reports 200 and 0 bytes, and compress adds no
  encoding.
- **Client disconnects mid-response**: accesslog still emits a record with the bytes delivered,
  and maxconcurrency releases the slot.
- **Long-lived connections**: protocol upgrades such as WebSocket, and server-sent events, keep a
  maxconcurrency slot until the handler returns. This MUST be documented so developers apply
  maxconcurrency only to suitable routes.
- **Repeated headers**: multiple request-identifier headers: only the first is considered.
  Multiple forwarding headers of the same name: read as one list in order of appearance.
- **Preflight for unknown paths**: cors answers preflight requests before routing, including for
  paths that do not exist.
- **Nonce pages and caches**: a response whose policy carries a nonce must not be served from a
  shared cache, or every visitor would see the same nonce. The documentation MUST say so. The
  middleware itself does not set caching headers.
- **Cross-origin embedder policy on HTML pages**: with the default embedder policy, a page cannot
  load cross-origin images or scripts unless they allow it (via resource policy or CORS). The
  documentation MUST point out how to relax or remove that header.
- **Requests without a body**: a declared length of 0 and no Content-Type is not "a request with
  a body" for contenttype, bodylimit or decompress.
- **Limits on compressed bodies**: bodylimit placed before decompress limits the encoded size,
  and decompress's own limit caps the decoded size.
- **Partial content**: range (206) responses are never compressed and never get a generated
  entity tag.
- **Failures outside the request flow**: a failure in background work that a handler starts
  concurrently cannot be recovered by any middleware. This MUST be documented.
- **Replacement response that writes nothing**: the middleware still sends its own status with an
  empty body, so a refused request never looks like a successful empty response.
- **Clients without a key**: ratelimit requests for which no key can be derived share one common
  allowance; they are not exempt.

## Requirements *(mandatory)*

### Functional Requirements

#### Module structure and shared conventions

- **FR-001**: The library MUST be delivered as one independent module named `middleware`. Its
  root package provides the chain. Each subpackage provides one middleware and is named after it:
  `bodylimit`, `accesslog`, `cors`, `recovery`, `requestid`, `secureheaders`, `realip`,
  `compress`, `decompress`, `etag`, `ratelimit`, `maxconcurrency`, `contenttype`.
- **FR-002**: Every middleware MUST have the same shape: it wraps a "next" handler and returns a
  handler of the standard HTTP handler model. As a result it works with the standard server and
  with any router built on that model, and it can be mixed in a chain with third-party middleware
  of the same shape.
- **FR-003**: Every middleware MUST be constructible with only its mandatory settings, which are
  listed per middleware below (several have none). Every other setting MUST be supplied as an
  optional, named option with a documented default.
- **FR-004**: Adding an option or a capability to a middleware in a later release MUST NOT
  require changes to existing call sites. It MUST NOT change the behavior of call sites that do
  not use it.
- **FR-005**: Invalid or contradictory configuration MUST be detected when the middleware is
  constructed, before it serves any request, and reported to the developer with the middleware
  and the setting at fault. A middleware MUST NOT silently replace an invalid value with a
  different one.
- **FR-006**: A constructed middleware MUST be safe to use for any number of simultaneous
  requests. Its configuration MUST NOT change after construction.
- **FR-007**: Several instances of the same middleware with different configurations MUST be
  able to coexist in one process without affecting each other. No middleware keeps process-wide
  state.
- **FR-008**: Middleware that emit log records MUST do so only through a structured logger
  supplied by the developer. No middleware writes to the standard output or error streams or
  uses a process-wide logger.
- **FR-009**: Middleware that observe or alter responses MUST preserve the capabilities of the
  underlying connection that handlers rely on: incremental delivery (flushing, streaming,
  server-sent events), protocol upgrades (e.g. WebSocket) and per-request connection controls
  such as read and write deadlines. When a middleware cannot apply its behavior to such a
  response, it MUST step aside for that response rather than break it.
- **FR-010**: Every middleware that refuses or fails a request (bodylimit, contenttype,
  ratelimit, maxconcurrency, decompress and recovery) MUST respond by default with the standard
  status code for that condition and a short, generic plain-text body. The developer MUST be able
  to replace that response, for example to produce JSON error documents, through an option that
  is identical in all of these middleware:
  - the replacement receives the status the middleware would send, and an identification of the
    condition that the developer can test reliably. One replacement can therefore serve the
    whole chain;
  - the protocol headers that belong to the condition (e.g. Retry-After) are already set when the
    replacement runs;
  - if the replacement writes no response, the middleware still sends its status;
  - the information given to the replacement can grow in later releases without breaking
    existing replacements (FR-004).
- **FR-011**: Responses produced by a middleware MUST NOT disclose internal details: failure
  values, stack traces, configuration or internal addresses.
- **FR-012**: The documentation of each middleware MUST state:
  - its mandatory settings;
  - every option, with its default;
  - its recommended position in a chain;
  - its interactions with the other middleware of the library;
  - at least one runnable usage example.

  The root package documentation MUST recommend an overall order for the full set and explain
  the ordering pitfalls listed under Edge Cases.

#### Chain (root package)

- **FR-013**: Developers MUST be able to build a chain from zero or more middleware listed in the
  order they choose. The first middleware listed is the outermost: it receives the request first
  and the response last.
- **FR-014**: Applying a chain to a final handler MUST produce a handler of the standard shape.
  Applying an empty chain MUST produce a handler that behaves exactly like the final handler.
- **FR-015**: Developers MUST be able to derive a new chain by appending middleware to an existing
  chain, or by concatenating two chains, without altering the originals. A chain MUST be reusable
  with any number of final handlers.
- **FR-016**: An absent middleware or an absent final handler MUST be reported when the chain is
  built or applied, never while a request is being served.

#### bodylimit

- **FR-017**: Mandatory setting: the maximum request body size in bytes. It must be positive.
- **FR-018**: A request whose declared body size exceeds the limit MUST be refused with 413
  Content Too Large. The next handler is not invoked and the body is not read.
- **FR-019**: When a request's body size is not declared in advance, reading past the limit MUST
  fail with a condition the handler can recognize as "body too large", so that it can answer 413.
  The remainder of the oversized body MUST NOT be delivered to the handler.
- **FR-020**: Requests within the limit and requests without a body MUST pass through unchanged.

#### accesslog

- **FR-021**: Mandatory setting: the structured logger.
- **FR-022**: Exactly one record MUST be emitted per request, after the response is completed. It
  contains at least:
  - the method, path and protocol version;
  - the status code and the response body size in bytes;
  - the duration;
  - the client address, as seen at accesslog's position in the chain;
  - the user agent;
  - the request identifier, when one was assigned earlier in the chain.
- **FR-023**: The query string MUST be excluded by default, because it may carry credentials, and
  included only when the developer opts in. Request header values MUST NOT be logged unless the
  developer names the specific headers to include.
- **FR-024**: When the handler writes a body without setting a status, the record MUST report
  200. When the downstream handler fails abruptly, the record MUST still be emitted, mark the
  request as failed and let the failure propagate unchanged.
- **FR-025**: The developer MUST be able to:
  - choose the log level per request (default: the same informational level for every request);
  - add custom fields derived from the request and the response;
  - exclude requests from logging, for example health checks.

#### cors

- **FR-026**: Mandatory setting: the allowed origins, as an explicit list. Allowing any origin
  MUST be an explicit choice. The developer MUST also be able to allow origins by a subdomain
  pattern (e.g. any subdomain of `example.com` over HTTPS) or by a decision function they supply.
- **FR-027**: Options and their defaults:
  - allowed methods: GET, HEAD, POST;
  - allowed request headers: none beyond the CORS-safelisted ones;
  - exposed response headers: none;
  - credentials: not allowed;
  - preflight cache duration: documented.
- **FR-028**: For a non-preflight request from an allowed origin, the response MUST carry the
  headers that grant that origin access, plus the credentials and exposed-headers headers when
  configured. The next handler MUST be invoked.
- **FR-029**: For a non-preflight request from a disallowed origin, or without an Origin header,
  the next handler MUST be invoked, and the response MUST NOT carry headers that grant CORS
  access.
- **FR-030**: A preflight request (OPTIONS carrying both Origin and Access-Control-Request-Method)
  MUST be answered by the middleware with 204, without invoking the next handler.
  - When the origin, the method and every requested header are allowed, the response MUST carry
    the allowed methods, the allowed headers, the credentials flag (if enabled) and the preflight
    cache duration.
  - Otherwise, it MUST carry no headers that grant access.

  An OPTIONS request without Access-Control-Request-Method is an ordinary request.
- **FR-031**: Whenever a response's CORS headers depend on the requesting origin, the response
  MUST tell caches that it varies by Origin. Preflight responses also vary by the requested
  method and headers.
- **FR-032**: Allowing any origin together with credentials MUST be rejected as an invalid
  configuration (FR-005). The `null` origin MUST never match "any origin" or a pattern; it is
  allowed only when listed explicitly. Origins and header names MUST be matched
  case-insensitively.

#### recovery

- **FR-033**: Mandatory setting: the structured logger, so that no recovered failure goes
  unreported.
- **FR-034**: When the downstream handler fails abruptly before the response has started, the
  middleware MUST answer 500 Internal Server Error with a generic body that the developer can
  replace (FR-010). The service MUST keep serving other and subsequent requests.
- **FR-035**: When the failure happens after the response has started, the middleware MUST abort
  the response so that the client can tell it is incomplete. The response MUST NOT appear to have
  completed successfully.
- **FR-036**: Each recovered failure MUST be logged with the failure value, the stack trace, the
  method, the path and the request identifier (if any). The developer MUST be able to supply an
  additional reporting function, for example to forward failures to an error tracker.
- **FR-037**: The signal the HTTP server uses to deliberately abort a response MUST be propagated
  unchanged and MUST NOT be logged as a failure.

#### requestid

- **FR-038**: No mandatory settings. By default the identifier travels in the `X-Request-ID`
  header and valid incoming identifiers are trusted.
- **FR-039**: When an incoming request carries a valid identifier in the configured header, that
  identifier MUST be reused. Otherwise a new one MUST be generated. A valid identifier:
  - is not empty;
  - is at most 128 characters long;
  - contains only printable ASCII characters other than space, so it cannot inject content into
    logs or headers.
- **FR-040**: The developer MUST be able to stop trusting incoming identifiers (always generate a
  new one), to choose the header name, and to supply their own generator.
- **FR-041**: Generated identifiers MUST be unique in practice, unpredictable, and valid under
  FR-039.
- **FR-042**: The effective identifier MUST be:
  - retrievable by downstream middleware and handlers through an accessor of the subpackage,
    which reports clearly when no identifier is present;
  - set on the response, in the configured header, before the handler writes anything.

#### secureheaders

- **FR-043**: No mandatory settings. With the defaults, every response MUST carry a documented set
  of security headers whose values follow the OWASP Secure Headers Project recommendations at
  implementation time. The set covers at least:
  - MIME-sniffing protection;
  - framing protection;
  - referrer policy;
  - a content security policy, with a restrictive default suited to API responses;
  - permissions policy, disabling sensitive browser features (sensors, camera, microphone,
    geolocation, payments, device access);
  - cross-origin embedder policy;
  - cross-origin opener policy;
  - cross-origin resource policy.
- **FR-044**: Each header MUST be individually overridable or removable through options. The
  content security policy additionally MUST support:
  - a **report-only** policy that the developer can set alone or alongside the enforced one, to
    trial a stricter policy without blocking anything;
  - a **per-request nonce**: when enabled, a fresh, unpredictable value is generated for every
    response and inserted wherever the policies reference it. Handlers and templates can
    retrieve it through an accessor of the subpackage;
  - validation: enabling the nonce when no policy references it, or referencing the nonce
    without enabling it, MUST be rejected as an invalid configuration (FR-005).
- **FR-045**: Strict-Transport-Security MUST be disabled by default, because browsers remember it
  and it can lock users out of hosts that are not served over HTTPS. When enabled, its duration,
  subdomain inclusion and preload flag MUST be configurable.
- **FR-046**: When the downstream handler sets one of these headers itself, the handler's value
  MUST take precedence.

#### realip

- **FR-047**: Mandatory setting: which proxies are trusted. They are declared either as network
  address ranges or as the number of proxy hops that every request passes through in front of
  the service.
- **FR-048**: In ranges mode, forwarding information MUST be used only when the immediate peer is
  in a trusted range. Otherwise the peer address MUST be used and every forwarding header
  ignored.
- **FR-049**: The client address is selected as follows:
  - Ranges mode: the rightmost address in the forwarding list that is not in a trusted range.
    When every address is trusted, the leftmost one is used.
  - Hop-count mode: the address at the configured position counting from the right. When the
    list is shorter than that position, the peer address is used.
- **FR-050**: Supported sources are X-Forwarded-For (the default), Forwarded and X-Real-IP, plus
  any header named by the developer that carries a single address (for example one set by a
  CDN). One instance consults exactly one source. Several occurrences of the same header MUST be
  read as one list, in order of appearance.
- **FR-051**: A malformed entry MUST never become the client address. When the entry that would
  be selected is malformed, the peer address MUST be used. Ports and IPv6 brackets MUST be
  removed, and addresses normalized (e.g. IPv4-mapped IPv6 addresses become IPv4).
- **FR-052**: The resolved address MUST become the client address seen by every downstream
  middleware and handler, so that accesslog, ratelimit and third-party code use it without extra
  configuration. The original peer address MUST remain retrievable through an accessor.

#### compress and decompress

- **FR-053**: Response compression and request decompression MUST be provided as two separate
  middleware in two subpackages, `compress` and `decompress`. The code they share MUST stay
  internal to the module, except what developers need in order to supply their own codings
  (FR-054). Each is enabled independently, and request decompression is active only where the
  developer explicitly applies it.
- **FR-054**: gzip and deflate MUST be supported out of the box. The developer MUST be able to
  add other codings they supply themselves (e.g. brotli, zstd), without the library depending on
  them. The server's preference order among codings MUST be configurable, with a documented
  default.
- **FR-055**: Response compression MUST negotiate the coding from the client's Accept-Encoding,
  honoring quality values, `*` and explicit exclusions. When no supported coding is acceptable,
  the response MUST be sent uncompressed.
- **FR-056**: A response MUST be compressed only when all of the following hold:
  - its media type is in the compressible list (default: textual types, JSON, JavaScript, XML
    and SVG; configurable);
  - its size reaches a minimum threshold (default 1 KiB, configurable). When the size is not
    known in advance, the decision is taken once enough content has been produced or the handler
    flushes;
  - it is not already encoded;
  - it is not partial content;
  - its status and the request method allow a body.
- **FR-057**: Compressed responses MUST declare their coding and MUST NOT declare the
  uncompressed length. Every response that passes through response compression MUST tell caches
  that it varies by Accept-Encoding.
- **FR-058**: When compressing a response that carries a strong entity tag, the middleware MUST
  make that validator distinguishable from the uncompressed representation's, for example by
  weakening it.
- **FR-059**: Flushes requested by the handler MUST deliver everything produced so far to the
  client promptly, in compressed form. The compression level MUST be configurable, with a
  documented default.
- **FR-060**: Request decompression MUST deliver to the handler the decoded body of a request
  whose body uses a supported coding. The request MUST then no longer declare the coding or the
  encoded length.
- **FR-061**: A request declaring an unsupported coding, or several stacked codings, MUST be
  refused with 415 Unsupported Media Type. The response MUST list the supported codings.
- **FR-062**: The decoded size MUST be capped, with a documented default and an option to change
  it. Exceeding the cap MUST surface the same recognizable "body too large" condition as FR-019.
  Corrupt encoded data MUST surface as a recognizable read failure.
- **FR-063**: Requests without a content coding, or with the identity coding, MUST pass through
  unchanged.

#### etag

- **FR-064**: For a 200 response to GET or HEAD that has no entity tag, the middleware MUST
  compute one from the response content, buffering at most a configurable size (default
  documented). Larger responses, responses the handler flushes early, and partial content MUST be
  delivered unchanged, without a generated tag.
- **FR-065**: When the request's If-None-Match matches the response's entity tag (weak
  comparison; lists and `*` supported), the middleware MUST answer 304 Not Modified with no body.
  It MUST keep the headers a 304 response must carry: the entity tag, the caching headers and
  Vary.
- **FR-066**: When the handler sets its own entity tag, the middleware MUST NOT replace it, but it
  MUST still apply FR-065.
- **FR-067**: Generated tags MUST be strong by default, and the developer MAY choose weak ones.
  Identical content MUST always produce the same tag, across instances and restarts, so that
  replicas of a service agree.
- **FR-068**: Other methods and statuses MUST pass through unchanged.

#### ratelimit

- **FR-069**: Mandatory settings: the number of requests allowed per time period. Options: the
  burst size (default: the same number as the limit) and the function that derives the client
  key.
- **FR-070**: Requests MUST be counted per client key. The default key is the client address
  seen at the middleware's position, so a realip placed earlier is honored. Requests for which no
  key can be derived MUST share one common allowance rather than being exempt.
- **FR-071**: A request that exceeds its key's allowance MUST be refused with 429 Too Many
  Requests, without invoking the handler. The response MUST carry Retry-After, set to the number
  of seconds until a request would be allowed. Other keys are unaffected.
- **FR-072**: Short bursts up to the burst size MUST be allowed while the long-run average rate is
  enforced. The allowance recovers continuously over time.
- **FR-073**: Memory use MUST stay bounded. The state of a key idle long enough to have fully
  recovered its allowance MUST be reclaimed. The middleware MUST NOT leave background activity
  running that the developer cannot stop.
- **FR-074**: Limits apply per middleware instance and per process. Coordination across replicas
  is out of scope.

#### maxconcurrency

- **FR-075**: Mandatory setting: the maximum number of requests processed at the same time. It
  must be positive.
- **FR-076**: By default, a request that arrives when the limit is reached MUST be refused
  immediately with 503 Service Unavailable, without invoking the handler. The response carries a
  Retry-After hint when the developer configures one.
- **FR-077**: As an option, a bounded number of requests MAY wait for a slot, up to a maximum
  waiting time. A waiting request MUST stop waiting when its client goes away, without invoking
  the handler. It MUST be refused with 503 when the wait expires or when the waiting queue is
  full.
- **FR-078**: A slot MUST be released when its request ends, however it ends: normal
  completion, abrupt failure or client disconnection.

#### contenttype

- **FR-079**: Mandatory setting: the allowed media types, a non-empty list. Each entry is an
  exact type (`application/json`) or a type wildcard (`text/*`).
- **FR-080**: A request carrying a body MUST be refused with 415 Unsupported Media Type, without
  invoking the handler, when its Content-Type is missing, unparseable or not allowed. Matching is
  case-insensitive and ignores parameters such as charset. A request carries a body when it
  declares a length greater than zero or a body of unknown length.
- **FR-081**: Requests without a body MUST pass regardless of their Content-Type.

### Key Entities

- **Middleware**: a reusable unit that wraps a request handler. It is constructed once from
  mandatory settings and options, and then serves many requests concurrently.
- **Chain**: an ordered, immutable sequence of middleware. It can be extended into new chains
  and applied to final handlers.
- **Option**: a named, optional configuration setting with a documented default, applied when a
  middleware is constructed.
- **Refusal**: a request that a middleware refuses or fails. It is described by the status the
  middleware would send and by the condition that caused it. The developer can replace how its
  response is written, with one replacement shared by every middleware.
- **Request identifier**: a correlation value attached to a request, propagated to the response,
  to logs and to downstream code.
- **Client address**: the address attributed to the requester. There are two of them: the
  *resolved* address, from trusted forwarding information, and the *peer* address, from the
  connection.
- **Client key and allowance**: the identity that rate limiting counts against, and its
  remaining allowance, which recovers over time.
- **Concurrency slot**: one unit of the in-flight request capacity, held from admission until the
  request ends.
- **Entity tag**: a validator that identifies a specific representation of a response; it is
  strong or weak.
- **Content coding**: a reversible transformation of a body (e.g. gzip), negotiated for
  responses or declared by requests.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: A developer new to the library protects an existing service with a chain of at
  least five middleware in under 15 minutes, using only the package documentation and its
  examples.
- **SC-002**: 100% of the middleware in scope can be used with only their mandatory settings in a
  single statement, and 100% of their options have a documented default.
- **SC-003**: From the first stable (1.0) release onward, 100% of minor releases that add options
  pass the repository's API compatibility check and require zero changes to consumer code.
- **SC-004**: 100% of the acceptance scenarios and edge cases in this spec are covered by
  automated tests, and statement coverage of the module is at least 80%.
- **SC-005**: A request passing through every non-buffering middleware with default settings
  incurs less than 0.1 ms of added processing time on a typical developer machine.
- **SC-006**: At 10 times the configured concurrency limit, the number of handlers running at
  once never exceeds the limit. After 10,000 requests that include panics and client
  disconnections, zero slots remain held.
- **SC-007**: Across tests with forged forwarding headers sent by untrusted peers, 0 requests are
  attributed to a forged address.
- **SC-008**: After 1,000,000 distinct clients stop sending requests, the rate limiter retains
  state for none of them within twice the time needed for an allowance to fully recover.
- **SC-009**: 100% of handler panics produce either a 500 response or a detectably aborted
  response, together with a log record, and the service answers 100% of subsequent requests.
- **SC-010**: For a representative 10 KiB JSON response, the compressed transfer size is at most
  30% of the uncompressed size.
- **SC-011**: 100% of repeated conditional requests for unchanged content are answered with 304
  and zero body bytes.
- **SC-012**: Adopting the library adds zero third-party dependencies to a consumer project.

## Assumptions

- **Single module**: the whole library is one module (`middleware`). It is versioned and released
  as a unit with `middleware/vX.Y.Z` tags. Releases stay at 0.x until the API is considered
  stable, then 1.0.0. From 1.0.0 the constitution's compatibility rules (Principle II) apply in
  full.
- **Go version**: the minimum Go version is the oldest release the constitution supports (1.26
  at the time of writing).
- **Dependencies**: only the standard library is needed (Principle V). This is why brotli and zstd
  are not built in; developers can add them themselves (FR-054).
- **Selective application**: different behavior on different routes comes from applying different
  chains to different routes or route groups. Per-middleware "skip" conditions are not in the
  initial scope, except for accesslog's exclusion option (FR-025). The option pattern allows them
  to be added later without breaking changes.
- **Rate limiting**: in-memory and per process. Distributed rate limiting (shared storage across
  replicas) is out of scope; it could be added later through an option.
- **contenttype**: interpreted as validation of the request's media type, as in common router
  libraries. Setting a default response Content-Type, and negotiating Accept (406), are out of
  scope.
- **realip**: resolves only the client address. Taking the scheme or host from forwarding headers
  is out of scope.
- **cors**: browser Private Network Access is out of scope.
- **requestid**: W3C Trace Context propagation is out of scope; it belongs with tracing.
- **Compression risks**: the risks of compressing secrets alongside attacker-controlled content
  (BREACH-style attacks) are documented. Developers mitigate them by excluding sensitive content
  types or routes from compression.
- **HTTP versions**: the middleware behave identically over HTTP/1.1 and HTTP/2.
- **Defaults chosen in the plan**: the default values this spec does not fix are chosen in
  `/speckit-plan` and documented per FR-012. They include the compressible types, the size
  thresholds, the preflight cache duration and the exact security header values.
- **Tests**: tests are hermetic and deterministic (Principle III). Time-dependent behavior
  (ratelimit, and waiting in maxconcurrency) is tested with controllable clocks, without real
  waiting.

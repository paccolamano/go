# Feature Specification: Building a Chain From the Constructors' Results (middleware 0.2.0)

**Feature Branch**: `014-middleware-chain-use` (suggested; not yet created)

**Created**: 2026-11-11

**Status**: Draft

**Input**: User description: "middleware v0.2.0: build a chain directly from the constructors'
results, and rename Extend to AppendChain". The full request, with the decisions already taken
and the intended use, is reproduced verbatim in
[Appendix: original request](#appendix-original-request).

## Context

The `middleware` module (spec `002-http-middleware`, released as `middleware/v0.1.0`) composes
HTTP server middleware into immutable chains. Each middleware lives in its own subpackage. Each
constructor returns a middleware and an error, because options can come from configuration
files: the constructor validates them once and reports invalid values as an error (FR-005 of
spec 002, R-04 of its research).

Building a chain therefore takes one error check per middleware. The module's first user has 8
such checks in the function that builds its routes. Each one wraps an error that already names
the package and the setting at fault, such as
`middleware: invalid configuration: cors: origins: ...`. The user cannot write a short helper to
remove them:

- a function's two results can be passed straight to another function only when that function
  takes exactly those two parameters;
- on error, the helper would still have to return a middleware, because building or appending to
  a chain with an absent middleware fails (FR-016 of spec 002).

With this feature, a chain takes a constructor's two results directly. The chain records the
errors. One check on the most derived chain reports all of them. Applying a chain whose errors
were never checked fails at startup, and the failure message lists the errors. The operation that
concatenates two chains is renamed, so that its name says what it does next to appending.

| Requirement wording | Name in the module or in the request |
|---------------------|--------------------------------------|
| a chain | `Chain` |
| building a chain from built middleware | `New` |
| appending built middleware | `Chain.Append` |
| adding a constructor's result | `Chain.Use` |
| the recorded errors of a chain | `Chain.Err` |
| appending a chain | `Chain.AppendChain`, formerly `Chain.Extend` |
| applying a chain to a handler | `Chain.Then` |
| a constructor | the `New` function of a subpackage, or any function that returns a middleware and an error |
| invalid configuration | an error wrapping `ErrInvalidConfig` |
| fails immediately | panics |

**Migration from `middleware/v0.1.0`** (the changelog entry of FR-021):

| 0.1.0 | 0.2.0 |
|-------|-------|
| `c.Extend(other)` | `c.AppendChain(other)` |
| `m, err := pkg.New(...)`, an error check, then `c = c.Append(m)` | optional: `c = c.Use(pkg.New(...))` for each constructor, then one `c.Err()` check on the most derived chain |

**Changes to spec 002**:

- FR-001 to FR-006 of this spec add a third way to derive a chain, next to the two of FR-015 of
  spec 002. The derived chain is a new chain, as FR-015 of spec 002 requires. R-02 of the
  research of spec 002 rejected a *mutable* builder also named `Use`, which changes the chain it
  is called on. The `Use` of this spec does not.
- FR-004 extends FR-016 of spec 002 to a constructor that returns neither a middleware nor an
  error.
- FR-011 extends FR-016 of spec 002: a configuration error that was never checked is reported
  when the chain is applied, never while a request is being served.
- FR-013 and FR-014 amend FR-015 of spec 002: the operation that concatenates two chains has a
  new name, and it carries the recorded errors as well as the middleware.
- R-04 of the research of spec 002 rejected constructor variants that fail instead of returning
  an error. This spec keeps that decision.

**Designs already rejected** (input for the research of the plan):

- A separate collector type that takes constructor results and returns their errors: two
  concepts for one job, next to the chain.
- A placeholder middleware that answers 500 when the errors are not checked: the failure appears
  only at the first request, only on some routes, and hides its cause.
- A builder whose final step returns a chain and an error: a second type, and one check per
  derived chain.
- Constructor variants that fail instead of returning an error: configuration often comes from
  files, and R-04 of the research of spec 002 already rejected them.
- Naming the renamed operation "Prepend": `c.Extend(other)` puts `other` after `c`, so the name
  would say the opposite of what the operation does.

**Actors**:

- **Developer**: builds the chains of a service at startup and registers its routes.
- **Maintainer**: evolves the module and publishes its releases.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - One check for the whole chain (Priority: P1)

A developer builds the chains of a service at startup, from constructors whose options come from
a configuration file. They pass each constructor's results straight to the chain, mix them with
middleware they built themselves, and derive route-specific chains from a shared base, as in the
intended use of the request (Appendix). One check on the most derived chain reports every
configuration error, of that chain and of the chains it derives from.

**Why this priority**: today each constructor takes its own error check, 8 in the first user's
route setup, and each check wraps an error that already says everything. Removing them is the
reason for the release.

**Independent Test**: build a base chain and two levels of derived chains from constructor
results, with valid and invalid settings, then read the errors of the most derived chain.

**Acceptance Scenarios**:

1. **Given** constructors with valid settings, **When** their results are added to a chain,
   **Then** the chain reports no error. Applied to a handler, it behaves exactly like the same
   middleware built one by one and listed in the same order.
2. **Given** a constructor with an invalid setting, **When** its result is added to a chain,
   **Then** the middleware is not added, the chain reports that error with its message
   unchanged, and a check for invalid configuration on the report succeeds.
3. **Given** a chain derived from a base, with an invalid CORS origin and then a body limit of 0
   added to it, **When** its errors are read, **Then** both errors are reported at once, in that
   order.
4. **Given** a base, a public chain derived from it and an authenticated chain derived from the
   public one, with an error added at each level, **When** the errors of the authenticated chain
   are read, **Then** the three errors are reported, in the order they were recorded.
5. **Given** constructor results and built middleware added in alternation, **When** the chain
   is applied, **Then** the middleware run in the order they were added, the first one
   outermost.
6. **Given** a constructor that returns neither a middleware nor an error, **When** its result is
   added to a chain, **Then** the call fails immediately, as appending an absent middleware does.
7. **Given** a function outside the module that returns a middleware and an error, **When** its
   result is added to a chain, **Then** it is handled exactly like a constructor of the module,
   whatever its error.

---

### User Story 2 - A forgotten check fails at startup, with its cause (Priority: P1)

A developer adds constructor results to a chain and forgets to read its errors. When the routes
are registered at startup, applying the chain fails, and the message lists every configuration
error. No request is ever served by a chain that lacks one of its middleware.

**Why this priority**: without this failure, a forgotten check silently removes a middleware,
such as CORS or the body limit, from a running service. A failure at startup shows the cause. A
failure at the first request would appear late, on some routes only, and hide the cause.

**Independent Test**: build a chain with two invalid constructor results, skip the check, and
apply it to a handler.

**Acceptance Scenarios**:

1. **Given** a chain with two recorded errors, **When** it is applied to a handler, **Then** the
   call fails immediately, with a message that starts with
   `middleware: chain has configuration errors:` and contains both errors, in the order they
   were recorded.
2. **Given** a chain whose errors were read and found present, **When** it is applied anyway,
   **Then** it still fails: reading the errors does not clear them.
3. **Given** a chain with recorded errors, **When** it is applied to several handlers, **Then**
   every application fails.
4. **Given** a chain built without adding any constructor result, **When** it is applied,
   **Then** it behaves exactly as in 0.1.0, including its failure on an absent handler.
5. **Given** a chain built from valid constructor results only, **When** it is applied, **Then**
   it does not fail, and serving a request through it costs the same as through the same
   middleware listed directly: the errors are checked when the chain is applied, never per
   request.

---

### User Story 3 - Route groups keep their errors apart (Priority: P2)

A developer shares a base chain between route groups: a readiness probe uses the base alone, and
the API adds CORS and a body limit to it. Errors added to a derived chain belong to that chain
and to the chains derived from it. The base, and any other chain derived from it, do not see
them.

**Why this priority**: chains are meant to be shared and specialized for route groups (spec
002). If errors leaked from one derived chain into its base or its siblings, two derivations
from the same base would interfere, which immutability forbids.

**Independent Test**: derive two chains from one base, add invalid constructor results to one of
them, then read the errors of the base and of the sibling, and apply both.

**Acceptance Scenarios**:

1. **Given** a valid base and a chain derived from it with an invalid CORS origin and a body
   limit of 0, **When** the errors of the base are read, **Then** there are none, and the base
   applies to a handler and serves requests.
2. **Given** two chains derived from the same base, one of them with a recorded error, **When**
   the errors of the other are read, **Then** there are none, and it contains only the base's
   middleware and its own.
3. **Given** a base with a recorded error, **When** two chains are derived from it, **Then** both
   report the base's error, and each reports, besides it, only its own errors.
4. **Given** several chains derived from the same base, in any order, **When** each one is
   applied, **Then** it runs the base's middleware followed by its own, and no derivation has
   altered another.

---

### User Story 4 - Appending a chain, and the upgrade from 0.1.0 (Priority: P2)

A developer concatenates two chains with the operation's new name, which says that the other
chain comes after this one. Upgrading from 0.1.0, code that only builds, appends and applies
chains compiles and behaves unchanged. Code that uses the former name fails to build, and the
changelog says what replaces it.

**Why this priority**: the rename breaks compatibility, so the upgrade path must be explicit.
Appending a chain must also carry errors, or concatenating a chain with errors would hide them
from the check and from the failure on application.

**Independent Test**: append chains with and without recorded errors, and compare the order of
the middleware with that of 0.1.0. Build code that uses the former name against this release.

**Acceptance Scenarios**:

1. **Given** two chains, **When** the first appends the second, **Then** the result runs the
   first chain's middleware, then the second's: the first chain's middleware stay outermost, as
   in 0.1.0.
2. **Given** a chain that appends a chain with recorded errors, **When** the errors of the result
   are read, **Then** they include the appended chain's errors, after the receiver's own, and
   applying the result fails.
3. **Given** two chains, **When** one appends the other, **Then** neither of them changes.
4. **Given** code that uses the former name of the operation, **When** it is built against this
   release, **Then** the build fails on that name, and the changelog maps it to the new one.
5. **Given** code that builds, appends and applies chains without the former name, **When** it
   is built against this release, **Then** it compiles unchanged and behaves as with 0.1.0.

---

### User Story 5 - The documentation shows the short way (Priority: P3)

A developer reads the README or the package documentation to build their first chain. The usage
adds constructor results directly and checks the errors once. The documentation explains that
applying a chain with unchecked errors fails.

**Why this priority**: the feature works without documentation, but a developer who copies the
current README writes one check per constructor, the exact problem this release removes.

**Independent Test**: read the README and the package documentation, and run the module's
examples as tests.

**Acceptance Scenarios**:

1. **Given** the README usage example, **When** a developer reads it, **Then** it builds the
   chain from constructor results and checks the errors once, with no check per constructor.
2. **Given** the package documentation, **When** a developer reads its overview of chains and its
   conventions, **Then** they find the three ways to derive a chain, the single error check
   before applying, and the failure of applying a chain with recorded errors.
3. **Given** the runnable examples, **When** they run as tests, **Then** one of them adds
   constructor results, reads the errors and appends a chain, and its output matches the
   documented output.
4. **Given** the module's documentation, examples and tests, **When** they are searched for the
   former name of the operation, **Then** only the changelog mentions it.

---

### Edge Cases

- **A constructor that returns both a middleware and an error**: the middleware is not added,
  and the error is recorded.
- **A constructor that returns neither a middleware nor an error**: the call fails immediately,
  naming the position the middleware would have had, as appending an absent middleware does.
- **The empty chain**: constructor results can be added to it. It has no recorded errors.
- **A chain built only by building and appending middleware**: it has no recorded errors.
- **An error that is not an invalid configuration**: it is recorded and reported in the same
  way. A check for invalid configuration on the report succeeds only if one of the recorded
  errors is one.
- **Reading the errors several times**: every read reports the same errors.
- **A chain that appends itself, or a chain derived from itself**: its middleware and its errors
  appear twice, once per occurrence, with no attempt to remove duplicates.
- **Appending an empty chain**: the result has the receiver's middleware and errors.
- **A chain with recorded errors applied to an absent handler**: the call fails with the recorded
  errors, because they occurred first.
- **Many recorded errors**: every one of them is reported, and every one appears in the failure
  message, with no limit.

## Requirements *(mandatory)*

### Functional Requirements

#### Adding a constructor's result

- **FR-001**: Developers MUST be able to add the two results of one middleware constructor, a
  middleware and an error, to a chain in a single call, without naming either result. The call
  MUST return a new chain and leave the original unchanged (FR-015 of spec 002).
- **FR-002**: When the error is absent, the new chain MUST contain the middleware of the original
  followed by the given middleware, exactly as appending it would.
- **FR-003**: When the error is present, the new chain MUST contain the same middleware as the
  original, the given middleware MUST NOT be added even when it is present, and the error MUST be
  recorded in the new chain.
- **FR-004**: When both results are absent, the call MUST fail immediately, as appending an
  absent middleware does (FR-016 of spec 002): the constructor has a bug.
- **FR-005**: Adding a constructor's result and appending built middleware MUST remain two
  distinct operations, and using one in place of the other MUST be rejected when the code is
  built. Appending built middleware is unchanged: it takes any number of middleware per call.
- **FR-006**: Any function that returns a middleware and an error MUST be accepted as a
  constructor, including functions outside this module, and any error MUST be recorded, not only
  invalid configuration.

#### Reading the recorded errors

- **FR-007**: Developers MUST be able to read, from a chain, every error recorded in it and in the
  chains it derives from, through any derivation: appending middleware, adding a constructor's
  result or appending a chain. The result MUST be absent when there is no recorded error.
- **FR-008**: The result MUST list the recorded errors in the order they were recorded. Each of
  them MUST remain identifiable: a check for invalid configuration, or for any specific error,
  MUST succeed on the result when one of the recorded errors matches it. The result MUST NOT add
  a message of its own to the recorded errors, which already name the package and the setting at
  fault (FR-005 of spec 002).
- **FR-009**: Errors recorded in a chain MUST NOT be visible from the chain it derives from, nor
  from any other chain derived from that one. A chain without recorded errors MUST stay usable
  whatever errors the chains derived from it have.
- **FR-010**: Reading the errors MUST NOT change the chain: errors are never cleared.

#### Applying a chain with recorded errors

- **FR-011**: Applying a chain that has recorded errors to a handler MUST fail immediately, with a
  message that starts with `middleware: chain has configuration errors:` followed by every
  recorded error, in the order they were recorded. This holds whatever the handler, an absent one
  included. It extends FR-016 of spec 002: an unchecked configuration error is reported when the
  chain is applied, never while a request is being served.
- **FR-012**: A chain without recorded errors MUST be applied exactly as in `middleware/v0.1.0`.
  Serving a request through a chain built from constructor results MUST cost the same as through
  the same middleware built one by one: the errors are checked when the chain is applied, never
  per request.

#### Appending a chain

- **FR-013**: Appending a chain MUST keep the semantics of the 0.1.0 operation: the new chain
  contains the receiver's middleware, then the other chain's, so the receiver's middleware stay
  outermost. The operation MUST carry its new name, and its former name MUST be removed with no
  deprecated alias.
- **FR-014**: Appending a chain MUST also carry the other chain's recorded errors, after the
  receiver's own.
- **FR-015**: Appending a chain MUST return a new chain and leave both chains unchanged. Later
  derivations of either chain MUST NOT affect the result, nor the result affect them.

#### Behavior kept

- **FR-016**: The middleware type, building a chain from built middleware, appending built
  middleware, and their failures on an absent middleware or an absent handler MUST be unchanged.
  The subpackages and their constructors MUST be unchanged.
- **FR-017**: The module MUST keep depending on the standard library alone, its tests and
  examples included (SC-012 of spec 002).

#### Documentation and release

- **FR-018**: The package documentation, in its overview of chains and in its conventions, and
  the README MUST show how to build a chain from constructor results and check its errors once,
  before applying it. They MUST state that applying a chain with recorded errors fails. The
  README usage example MUST no longer check each constructor's error separately.
- **FR-019**: The documentation of each new operation MUST state when it fails: adding the result
  of a constructor that returns neither a middleware nor an error, and applying a chain with
  recorded errors.
- **FR-020**: A runnable example MUST add constructor results, read the errors and append a
  chain. The module's documentation, examples and tests MUST NOT mention the former name of the
  operation, except the changelog.
- **FR-021**: The module's changelog MUST record, under the unreleased version:
  - added: adding a constructor's result, reading the recorded errors, appending a chain, and the
    failure of applying a chain with recorded errors;
  - removed, as a breaking change: the former name of the operation that appends a chain, with
    the migration "replace `Extend` with `AppendChain`".

### Key Entities

- **Chain**: an immutable, ordered list of middleware, the first one outermost, together with
  the errors recorded while it was built.
- **Constructor result**: the pair returned by a constructor: a middleware and an error. At most
  one of them is meant to be present.
- **Recorded error**: an error from a constructor result, kept in the chain where it was added
  and in every chain derived from it, in the order it was recorded.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: The intended use of the request, with seven constructors across three chains,
  needs exactly one error check, instead of seven. The README usage example has exactly one
  error check, whatever the number of constructors.
- **SC-002**: In 100% of the tested chains with recorded errors (one error, several errors,
  errors at up to three levels of derivation, and errors brought by appending a chain), one read
  on the most derived chain reports every recorded error, in the order recorded, and none from a
  sibling chain.
- **SC-003**: 100% of the tested chains with recorded errors fail when applied, before any
  request is served, with every recorded error in the failure message. 0 requests are served by a
  chain with recorded errors.
- **SC-004**: For chains built without constructor results, 100% of the chain tests of 0.1.0
  pass with no change other than the operation's new name.
- **SC-005**: Serving a request through a chain built from constructor results makes the same
  number of memory allocations, and takes at most 5% more time, as the same middleware built one
  by one, as measured by the module's benchmarks.
- **SC-006**: Against `middleware/v0.1.0`, the API compatibility check reports exactly the three
  additions and the one removal listed in the changelog.
- **SC-007**: 100% of the acceptance scenarios and edge cases of this spec are covered by
  automated tests. Statement coverage of the module is at least 80%. The module still adds zero
  dependencies beyond the standard library to a consumer project.

## Assumptions

- **Release**: the changes ship as `middleware/v0.2.0`. The module is at 0.x, where a minor
  release may break compatibility: the constitution requires a new major version, preceded by a
  deprecation in a minor version, only from v1.0.0 (Principle II). `slogx/v0.2.0` removed
  identifiers in the same way.
- **No user of the former name**: only the module's own tests call the operation that appends a
  chain. No other module of this repository imports `middleware`. So it is removed with no
  deprecated alias.
- **Failing when a chain is applied is consistent with Principle IV**: the recoverable condition,
  an invalid configuration, is still reported as an error, by reading the chain's errors. The
  failure covers only the programming error of never reading them, as the failure on an absent
  middleware already does for an ignored constructor error (R-03 of the research of spec 002).
  Chains are applied at startup, when the routes are registered.
- **Appending a chain needs no check for absent middleware**: every chain is built only through
  operations that already refuse an absent middleware, so a chain never contains one.
- **Duplicates are kept**: an error appears once per occurrence, as middleware do. Removing
  duplicates would require comparing errors, which have no reliable identity.
- **The design is proven**: a prototype, a local copy of the chain over the module's real
  constructors, gave the results of User Story 1 scenario 3, User Story 2 scenario 1 and User
  Story 3 scenario 1, with an invalid CORS origin and a body limit of 0.
- **Tests**: invalid settings of the module's own constructors, test handlers and in-memory
  requests produce every case, so the tests stay hermetic and deterministic (Principle III).

## Appendix: original request

The request, as submitted to `/speckit-specify`:

````text
middleware v0.2.0: build a chain directly from the constructors' results, and rename Extend to AppendChain.

Problem. Every constructor of the middleware subpackages returns (middleware.Middleware, error), because options can come from configuration files and New validates them once. Building a chain therefore takes one `if err != nil` block per middleware. The module's first user has 8 such blocks in one function, and each one wraps an error that already names the package and the option ("middleware: invalid configuration: cors: origins: ..."). Users cannot write a short helper for this. Go passes a function's two results straight to another function only when that function takes exactly those two parameters. And on error the helper would have to return something that is not nil, because New and Append panic on a nil middleware.

Decisions already taken (do not reopen them):

1. Chain.Use(mw Middleware, err error) Chain takes the two results of one constructor: chain.Use(requestid.New()). It returns a new chain, as Append does, so chains stay immutable. This is not the mutable builder `chain.Use(m)` that spec 002 R-02 rejected. When err is not nil, the middleware is not added and the error is recorded in the new chain. When err is nil and mw is nil, Use panics, as Append does on a nil middleware: the constructor has a bug.

2. Append is unchanged: it takes middleware already built, several per call. The compiler tells Use and Append apart: Append(requestid.New()) and Use(spanAttrs) do not compile.

3. Chain.Err() error returns every error recorded in the chain and in the chains it derives from (through Append, Use and AppendChain), joined in the order they were recorded. It returns nil when there is none. Chains derived from a common base do not see each other's errors: a base without errors stays usable when a chain derived from it has errors. One check on the most derived chain covers all its ancestors.

4. Then on a chain whose Err is not nil panics, with a message that lists every error ("middleware: chain has configuration errors:", followed by the errors). The documented path is to check Err before Then. The panic catches a forgotten check at startup, when the routes are registered, and shows the cause. The package already applies the same rule to a nil middleware, which means a constructor error was ignored. Chains built without Use behave exactly as in 0.1.0.

5. Extend(other Chain) is renamed AppendChain(other Chain), with the same semantics: the middleware of c, then those of other, so c's middleware stay outermost. AppendChain also carries other's recorded errors. Otherwise base.AppendChain(public) would lose public's errors, and Then would not catch a chain that lacks middleware. Extend is removed with no deprecated alias: the module is at 0.x, and only its own tests use it. AppendChain needs no nil check on other: a Chain never contains a nil middleware, because its list is unexported and New, Append and Use check every middleware.

6. Release: middleware/v0.2.0, a breaking minor release while the module is at 0.x. The CHANGELOG records:
   - Added: Use, Err, AppendChain, and the panic of Then on a chain with recorded errors.
   - Removed: Extend, with the migration "replace Extend with AppendChain".
   The package documentation (the Chain and Conventions sections) and the README show how to build a chain with Use and check Err. A runnable example covers Use, Err and AppendChain.

The intended use:

	var base middleware.Chain
	if len(trustedProxies) > 0 {
		base = base.Use(realip.New(realip.TrustRanges(trustedProxies...)))
	}
	base = base.
		Use(requestid.New()).
		Append(spanAttrs).
		Use(accesslog.New(logger)).
		Use(recovery.New(logger, recovery.WithRejectFunc(reject)))
	public := base.
		Use(secureheaders.New()).
		Use(cors.New(origins, cors.WithCredentials())).
		Use(bodylimit.New(1<<20, bodylimit.WithRejectFunc(reject)))
	authenticated := public.Append(authenticate)
	if err := authenticated.Err(); err != nil { // covers public and base too
		return nil, err
	}
	mux.Handle("GET /readiness", base.Then(readiness))
	mux.Handle("GET /api/v1/users/me", authenticated.Then(me))

A prototype of this design (a local copy of Chain over the real constructors) gave these results. With an invalid CORS origin and a bodylimit of 0:
- public.Err() reported both errors at once;
- base.Err() was nil, and base.Then worked;
- public.Then panicked with both errors in its message.

Alternatives considered and rejected, for the research of the plan:
- A separate Collector type with Collect(mw, err) and Err: two concepts for one job.
- A placeholder middleware that answers 500 when Err is not checked: the failure appears only at the first request, only on some routes, and hides its cause.
- A builder whose Build() returns (Chain, error): a second type, and one check per derived chain.
- Must variants: configuration often comes from files, and spec 002 R-04 already rejected them.
- Naming the renamed Extend "Prepend": it would say the opposite of what it does.

Unchanged:
- the Middleware alias, New and Append;
- the panics of New, Append and Then on nil;
- immutability: every Use and AppendChain allocates a new list, so chains derived from the same base never interfere, errors included;
- the subpackages and their constructors.
````

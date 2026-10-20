# Specification Quality Checklist: HTTP Request Authentication (auth)

**Purpose**: Validate specification completeness and quality before proceeding to planning
**Created**: 2026-10-19
**Feature**: [spec.md](../spec.md)

## Content Quality

- [x] No implementation details (languages, frameworks, APIs)
- [x] Focused on user value and business needs
- [x] Written for non-technical stakeholders
- [x] All mandatory sections completed

## Requirement Completeness

- [x] No [NEEDS CLARIFICATION] markers remain
- [x] Requirements are testable and unambiguous
- [x] Success criteria are measurable
- [x] Success criteria are technology-agnostic (no implementation details)
- [x] All acceptance scenarios are defined
- [x] Edge cases are identified
- [x] Scope is clearly bounded
- [x] Dependencies and assumptions identified

## Feature Readiness

- [x] All functional requirements have clear acceptance criteria
- [x] User scenarios cover primary flows
- [x] Feature meets measurable outcomes defined in Success Criteria
- [x] No implementation details leak into specification

## Notes

- Iteration 1: three [NEEDS CLARIFICATION] markers are open, for three of the four decisions the
  request leaves to the user:
  - FR-019: the status when a required method is missing (decision 3);
  - FR-030: a per-account limit on failed password attempts (decision 2);
  - a third marker on decision 1.

  Decision 4 (the rejection function type) was resolved with an informed default, recorded under
  Assumptions: `auth` defines its own type with the same shape as `middleware`'s, so that it keeps
  exactly the two dependencies the request allows. All other items passed.
- Iteration 2 (after the user's answers, session 2026-10-19, at the start of `/speckit-plan`):
  - decision 1 is out of the first release: its requirement and scenario were removed, and
    FR-043 to FR-073 were renumbered FR-042 to FR-072;
  - FR-030 now defines the optional per-username attempt limit; User Story 2 gains scenarios 10
    to 13, User Story 7 scenario 7, FR-014 the 429 status, and SC-004 the parallel bound;
  - FR-019 now defines the status per method; the scenarios of User Stories 1, 4, 5, 6 and 7
    state 401 with the RFC 9470 challenge for tokens and 403 for the other methods;
  - both answers are recorded under Context > Clarifications. All items pass.
- Iteration 3 (during `/speckit-plan`, from the spike in research.md):
  - **FR-015**: the guarantee of at least one challenge applies whenever a combined method defines
    one, since FR-017 makes challenges optional for methods written outside the module; the
    rejecting method's challenge also replaces other challenges of its scheme (research R-05);
  - **FR-045**: an attempt is counted before its code is evaluated, which is what bounds parallel
    requests to 5 evaluations; a failure after that point keeps the attempt counted (R-12);
  - **FR-069**: any `crit` header is rejected. The spike showed that go-jose accepts
    `crit: ["b64"]`, which this module does not support for tokens (R-17).

  No FR renumbering was needed, and all items still pass.
- Iteration 4 (after `/speckit-analyze` and `/speckit-clarify`, session 2026-10-20):
  - **FR-009**: request-bound methods take their context from `r.Context()`. The deviation from
    the constitution's "context as first parameter" rule is recorded in the plan's Complexity
    Tracking;
  - **FR-028**: the configurable maximum is 256 to 4,096 bytes, so that every configuration
    accepts 64 characters;
  - **FR-002**: "depend directly", since `x/sys` appears as an indirect requirement;
  - **FR-004**: the key server's caching headers join the fuzzed inputs.

  Both answers are recorded under Context > Clarifications. No FR renumbering was needed, and
  all items still pass.
- The Go language, the cited RFCs, `golang.org/x/crypto` and `go-jose/v4` appear because they are
  given by the request and the constitution (v1.2.0); they are the observable contract of an
  authentication library. The spec does not decide type names, function signatures, key layouts
  or data structures. These are left to `/speckit-plan`.
- "Non-technical stakeholders": the audience of a library is the developers who consume it, so
  the spec states observable behavior (statuses, challenges, cookie attributes, store contents,
  attempt counts) rather than mechanisms.

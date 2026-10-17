# Specification Quality Checklist: Error Wrapping and Error Detail Module

**Purpose**: Validate specification completeness and quality before proceeding to planning
**Created**: 2026-10-16
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

- Iteration 1: three [NEEDS CLARIFICATION] markers were open, all of them scope decisions.
- Iteration 2 (after the user's answers, session 2026-10-16):
  - FR-001 is resolved: the module is a drop-in replacement for the standard errors package, in
    the `errors` directory. The standard "unsupported operation" sentinel is reused as is.
    - User Story 1 gains scenario 8 (switching the import only).
    - SC-001 now requires zero other code changes.
    - There is a new edge case about mixing with the standard package, and an assumption about
      tracking future additions to the standard package.
  - FR-030 is resolved as an opt-in helper for the standard structured logger:
    - the default key can be replaced;
    - the detail is computed only when the record is emitted;
    - nothing is added for no error;
    - errors logged without the helper are unchanged.

    User Story 4 scenario 2 is rewritten, and scenario 3 (disabled level) is added, with a new
    edge case.
  - Both answers are recorded under Context > Clarifications. Requirements are now FR-001 to
    FR-030, and no renumbering was needed. All items pass.
- Iteration 3 (during `/speckit-plan`, from the design spike in research.md):
  - **SC-004**: the cost targets are now 3× the time (previously 2×) and still 2× the memory of
    the standard wrapping, and an origin must take under 1.5 µs (previously 1 µs). Capturing the
    caller's location alone costs about 170 ns, more than the whole `fmt.Errorf` wrapping (about
    155 ns). The old targets measured 1.9× and about 1.0 µs, which leaves no margin
    (research R-16).
  - **FR-023**: byte-identical output is now required for the string verbs (`%v`, `%s`, `%q`,
    `%x`, `%X`) with any flags. Other verbs format the message string, because a standard
    error's output for them depends on its internal representation (research R-12).
  - **FR-024**: the per-level full message is removed from the structured form. It would grow
    quadratically with depth, the reference's defect W3. The full message is the error's own
    message, and the slog attribute carries it (research R-10, R-14).
  - **Edge case "Empty context"**: it now states that an annotation over a level without a
    location is shown merged with that level (research R-10).

  No FR renumbering was needed, and all items still pass.
- Iteration 4 (after `/speckit-analyze` and the implementation, at the user's request):
  - **FR-008** and the edge case "Package initialization" now name the two documented
    exceptions that keep a location (research R-07).
  - **FR-019** now says that every type is shown except the library's own and the standard
    library's generic ones, as the contract and the tests already did.
  - **SC-009** now states how the two toolchain-dependent scenarios are covered: an opt-in test
    for the static check, and a test of the location rules plus a manual check for build-directory
    independence.
  - FR-005 is unchanged: the missing examples (`New`, `Wrapf`, `WithStack`, `DetailValue`) were
    added to the module instead.

  No FR renumbering was needed, and all items still pass.
- The Go language, the module layout and the standard-library baseline appear because they are
  given by the request or by the constitution (v1.2.0). Formatting verbs (`%v`, `%+v`, `%q`, `%w`)
  and JSON are the observable contract of an error library, not implementation choices. The spec
  does not decide types, function names, the mechanism used to detect package initialization,
  stack-capture techniques or data structures. These are left to `/speckit-plan`.
- "Non-technical stakeholders": the audience of a library is the developers who consume it. The
  spec therefore states observable behavior (messages, inspection results, detail contents, costs
  relative to the standard library) rather than mechanisms.
- The reference library's evidence (code references, probe outputs, benchmarks) is kept in
  [analysis.md](../analysis.md), so that the spec stays free of implementation detail.

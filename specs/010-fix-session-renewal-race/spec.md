# Feature Specification: Deleted Sessions Stay Deleted During Renewal (auth)

**Feature Branch**: `010-fix-session-renewal-race` (suggested; not yet created)

**Created**: 2026-10-22

**Status**: Draft

**Input**: User description: "Bug: una sessione revocata può tornare valida". The full request is
reproduced verbatim in [Appendix: original request](#appendix-original-request).

## Context

The `auth` module (spec `007-http-auth`, released as `auth/v0.1.0`) keeps signed-in users in
server-side sessions. When a session's renewal interval has passed (a quarter of the idle timeout,
7.5 minutes by default), authenticating a request reads the session from the store and writes it
back with a new time of last activity (FR-036 of spec 007). The read and the write are two
separate store operations.

A logout (FR-037 of spec 007), or a new login that deletes the session the request carried
(FR-032 of spec 007), can delete the session between that read and that write. The write then
creates the session again. The logout clears the user's cookie and reports success, so nothing
looks wrong. But anyone holding a copy of the identifier, for example from a stolen cookie, keeps
a valid session, which renews on each use until its absolute lifetime (8 hours by default).
Logout exists to stop exactly that case. A new login has the same flaw: the session it replaces
can come back too, which undoes the protection against session fixation.

The window is short: the time between one read and one write, at most once per renewal interval.
When a logout lands in it, the logout fails without any error. A deterministic test reproduces it
with virtual time and a store that runs the logout between the read and the write of a renewal.
The deleted session authenticates the next request ("session revoked during renewal is alive
again").

The fix adds a conditional write to the store contract: write only when the key exists. Renewal
uses it, so it can no longer create a session that is gone.

| Requirement wording | Name in the request or in the module |
|---------------------|--------------------------------------|
| the store contract | `auth.Store` |
| the conditional write | the new `auth.Store` operation that the request calls "scrivi solo se la chiave esiste"; the plan names it |
| the in-memory store | `memstore.Store` |
| authenticating a request with a session, and renewal | `(*session.Manager).Authenticate` |
| logout | `(*session.Manager).Revoke` |
| a new login | `(*session.Manager).Issue` |
| the Redis adapter | the adapter shown in the module README |

**Changes to spec 007**:

- FR-001 of this spec adds a fifth operation to the four that FR-020 of spec 007 lists.
- FR-006 of this spec refines FR-036 of spec 007: renewal writes only over a session that still
  exists.
- The rule that the module never reads a key it increments extends to the conditional write
  (FR-003).

**Actors**:

- **Developer**: builds a service with the module, and writes or copies the store adapter.
- **User**: signs in and out in a browser.
- **Attacker**: holds a copy of a session identifier, for example a stolen cookie.
- **Maintainer**: evolves the module and publishes its releases.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Logging out ends the session even while a request renews it (Priority: P1)

A user signs in, works, then logs out. At that moment another request with the same session is in
flight: a second tab, a page that polls, or an attacker who copied the session cookie. That
request happens to be the one that renews the session. However the store operations of the two
requests interleave, once the logout has completed, no one can use the session.

**Why this priority**: the defect is in released security behavior. It silently defeats logout,
which is the defense against a stolen session cookie.

**Independent Test**: with virtual time and the in-memory store, a store wrapper runs the logout
exactly between the read and the write of a renewal. A request with the same identifier is then
authenticated.

**Acceptance Scenarios**:

1. **Given** a session whose renewal interval has passed, **When** a logout deletes it between the
   read and the write of a request that renews it, **Then** that request is answered as having no
   credentials, and so is every later request carrying the identifier.
2. **Given** the same session, **When** the logout lands before the renewal's read, or after its
   write, **Then** every later request carrying the identifier has no credentials (the behavior
   of spec 007, kept).
3. **Given** a session whose renewal interval has passed, and no logout, **When** a request renews
   it, **Then** the request is authenticated, and the idle timeout starts again from that request
   (FR-036 of spec 007, kept).
4. **Given** a route that admits anonymous requests, **When** the renewing request finds its
   session deleted, **Then** it is served as anonymous, like any request with a stale cookie
   (FR-034 of spec 007).

---

### User Story 2 - A new login retires the previous session even while a request renews it (Priority: P1)

A user signs in again on a browser that already carries a session. Issuing the new session deletes
the old one, so that an identifier known before the login is worthless afterwards (FR-032 and
SC-005 of spec 007). The race of User Story 1 must not bring the old session back.

**Why this priority**: the same defect defeats the protection against session fixation, a
released security guarantee.

**Independent Test**: as for User Story 1, with a new login in place of the logout.

**Acceptance Scenarios**:

1. **Given** a session A whose renewal interval has passed, **When** a new login on a request
   carrying A deletes A between the read and the write of a request that renews A, **Then** the
   renewing request has no credentials, A authenticates 0 later requests, and the new session
   authenticates.

---

### User Story 3 - Store adapters provide the conditional write (Priority: P2)

A developer has written a store adapter, for example by copying the Redis adapter from the
README, and upgrades the module. The fix needs the store to write a value only when its key still
exists, checking and writing in one atomic step, so the store contract gains an operation. The
developer's build reports the missing operation until the adapter implements it. The README shows
the Redis implementation, one command, and the changelog explains the change. The in-memory
store already implements it.

**Why this priority**: User Stories 1 and 2 hold only with a store that provides the operation.
The fix can be shown with the in-memory store alone, so this story covers the upgrade path of
custom adapters.

**Independent Test**: exercise the conditional write of the in-memory store on absent, present
and expired keys, and concurrently with deletions of the same key.

**Acceptance Scenarios**:

1. **Given** an absent key, **When** a conditional write runs, **Then** it reports that it stored
   nothing, and the key stays absent.
2. **Given** a present, unexpired key, **When** a conditional write runs, **Then** the key holds
   the new value with the new expiry, and the write reports that it stored it.
3. **Given** a key whose expiry has passed, **When** a conditional write runs, **Then** it stores
   nothing, and the key stays absent.
4. **Given** a present key, **When** a conditional write and a deletion of that key run
   concurrently, **Then** the key is absent once both have completed, whichever runs first.
5. **Given** an adapter written for `auth/v0.1.0`, **When** the developer upgrades, **Then** the
   build reports the missing operation, and the changelog and the README show how to add it.

---

### Edge Cases

- Two requests renew the same session at once, with no deletion: both are authenticated, and the
  session stays valid.
- The session's expiry in the store passes between the renewal's read and write: the conditional
  write stores nothing, and the request has no credentials. The session had expired anyway.
- The store fails during the conditional write: the request fails with an error, answered with
  500, never with 401 and never as "no credentials" (FR-014 of spec 007).
- The request's context is cancelled during the conditional write: the request fails with an
  error, as for every store operation.
- Pending logins are written once, at issuance, then only read, counted and deleted. They are not
  exposed to this race, and the fix leaves them unchanged.
- Issuing a session or a pending login writes under a new identifier, never over a key it has
  read, so it keeps the unconditional write.

## Requirements *(mandatory)*

### Functional Requirements

#### Store contract

- **FR-001**: The store contract MUST offer a conditional write. It stores a value with an expiry
  under a key only when the key exists and has not expired, and reports whether it stored it. It
  never creates a key. Checking and writing MUST be one atomic step: a deletion of the key happens
  either before it, and nothing is stored, or after it, and the deletion removes the new value.
- **FR-002**: The conditional write MUST follow the rules of the other operations: its expiry is
  greater than zero, it honors the cancellation of its context, and any failure other than "key
  absent" is an error.
- **FR-003**: The module MUST NOT make a conditional write on a key it increments. The store
  contract states this next to the existing rule that the module never reads a key it increments.
- **FR-004**: The in-memory store MUST implement the conditional write under the rules of FR-001
  and FR-002. It stores a copy of the value, rejects an expiry of zero or less, never revives an
  expired entry, and is safe for concurrent use.
- **FR-005**: The Redis adapter in the README MUST implement the conditional write with a single
  command, `SET` with `XX` and an expiry, which is atomic and touches one key, so that the adapter
  keeps working with Redis Cluster. The README MUST state why one command is needed.

#### Sessions

- **FR-006**: Renewal MUST write the session back with the conditional write, never with the
  unconditional write. When the conditional write reports that the session no longer exists, the
  request MUST produce "no credentials", as for a revoked session (FR-034 of spec 007).
- **FR-007**: A session deleted by a logout or by a new login MUST authenticate 0 requests
  afterwards, whatever the order of the store operations of concurrent requests that renew it.
- **FR-008**: Wherever an operation of the module writes over a key that it read in the same
  operation, it MUST use the conditional write.
- **FR-009**: Renewal MUST keep its cost: one store read per request, and at most one store write
  per renewal interval (FR-036 and SC-006 of spec 007).
- **FR-010**: A store failure during the conditional write MUST produce an error, never "no
  credentials".

#### Documentation and release

- **FR-011**: The documentation of the store contract and the package documentation MUST describe
  the conditional write and its atomicity. The documentation of session authentication MUST state
  that a session deleted while a request renews it gives "no credentials".
- **FR-012**: The module's changelog MUST record, under the unreleased version, the added store
  operation as a breaking change, with how to implement it for Redis, and the fixed defect.
- **FR-013**: Regression tests MUST reproduce the interleavings of User Stories 1 and 2
  deterministically, with virtual time and a store that runs the logout or the new login between
  the read and the write of a renewal. They MUST fail without the fix (Constitution Principle
  III).

### Key Entities

- **Store**: the developer's key-value store with expiry that holds sessions, pending logins and
  counters. It gains the conditional write.
- **Session record**: the stored state of a session (subject, methods, times). Its format does
  not change.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: In 100% of the tested interleavings, the deleted session authenticates 0 later
  requests. The interleavings are a logout and a new login, each landing before the read of a
  renewal, between its read and its write, and after its write.
- **SC-002**: With at least 100 concurrent requests renewing one session while a logout runs,
  0 requests that start after the logout completes are authenticated, and race detection reports
  0 data races.
- **SC-003**: A session used once per second for a full idle timeout still costs exactly one store
  read per request, and at most as many store writes as there are renewal intervals in that
  timeout.
- **SC-004**: In 100% of the tested store failures during renewal, the response is 500, never
  401.
- **SC-005**: In 100% of the tested cases, the in-memory store's conditional write stores nothing
  on an absent or expired key, and stores the new value and expiry on a present key. After a
  concurrent conditional write and deletion of one key, repeated at least 100 times, the key is
  absent in 100% of the runs.
- **SC-006**: 100% of the acceptance scenarios and edge cases of this spec are covered by automated
  tests. Statement coverage of the module stays at least 80%. Against `auth/v0.1.0`, the API
  compatibility check reports one incompatible change only: the operation added to the store
  contract.

## Assumptions

- **Release**: the fix ships in the next minor release of the module (0.2.0). The module is at
  0.x, where a minor release may break compatibility. The constitution requires a new major
  version only from v1.0.0 (Principle II). The changelog marks the added operation as breaking.
- **A required operation**: the conditional write is a required part of the store contract, as
  the request decides. An optional operation, detected at run time, would let existing adapters
  build and keep the defect without any warning.
- **The renewing request is rejected**: a request whose renewal finds its session deleted is
  answered as having no credentials, even though the session existed when the request started.
  Once the logout has completed, the session no longer exists, and rejecting the request is the
  safe answer.
- **Other stores**: common key-value stores and databases offer the same atomic step, such as an
  update restricted to an existing key. Developers with custom adapters add one operation, and
  the build points them to it.
- **Tests**: interleavings are reproduced with virtual time and a store wrapper, which keeps the
  tests deterministic and hermetic (FR-070 of spec 007, Constitution Principle III). The
  concurrent tests of SC-002 and SC-005 assert outcomes that hold for every schedule.
- **Scope**: nothing else changes in sessions. Lifetimes, cookies, the record format and the cost
  of a request stay as spec 007 defines them.

## Appendix: original request

The request, as submitted to `/speckit-specify`:

````text
Bug: una sessione revocata può tornare valida
Quando una richiesta rinnova la sessione, Authenticate legge il record e poi lo riscrive (session.go:145). Se un Revoke cade tra la lettura e la scrittura, la scrittura ricrea la sessione appena cancellata. Il browser dell'utente non ha più il cookie, ma chi l'ha rubato può continuare a usarlo. Proprio quel caso è ciò da cui il logout dovrebbe proteggere. Lo stesso vale per la vecchia sessione che Issue cancella al login.

Ho scritto un test che riproduce l'interleaving in modo deterministico, con synctest e una copia del modulo nella scratchpad (il repo non l'ho toccato). Il test fallisce: session revoked during renewal is alive again. Il file è /tmp/claude-1000/-home-paccolamano-Personal-go/4f141ad9-5774-4a1d-865b-936eb060a3b5/scratchpad/authcopy/session/race_probe_test.go.

Correzione: aggiungere a auth.Store una scrittura condizionale, "scrivi solo se la chiave esiste". In Redis è SET key val XX PX ttl: un solo comando, atomico, compatibile con Cluster. Cambiare l'interfaccia rompe la compatibilità, ma alla 0.1.0 conviene farlo ora. All'adapter Redis del README si aggiunge una riga.
````

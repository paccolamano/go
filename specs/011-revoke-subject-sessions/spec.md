# Feature Specification: Revoke Every Session of a Subject (auth)

**Feature Branch**: `011-revoke-subject-sessions` (suggested; not yet created)

**Created**: 2026-10-23

**Status**: Draft

**Input**: User description: "Nel modulo auth, sotto-package session: revocare tutte le sessioni di
un utente (subject)." The full request is reproduced verbatim in
[Appendix: original request](#appendix-original-request).

## Context

The `auth` module (spec `007-http-auth`, released as `auth/v0.2.0`) keeps signed-in users in
server-side sessions. A session ends when it expires (8 hours of absolute lifetime or 30 idle
minutes, by default), or when the request that carries it logs out or logs in again (FR-032 and
FR-037 of spec 007). Nothing ends the *other* sessions of the same user.

The cases that need it are common: a user changes a stolen password, an administrator disables an
account, a user asks to log out of every device. In each case the sessions opened elsewhere, for
example by an attacker holding a stolen cookie, stay valid and keep renewing for up to 8 hours.
The developer cannot fix this from outside the module: the store holds only hashes of the session
identifiers, so the sessions of a user cannot be found, and the principal does not say when its
session began.

This feature adds one operation that revokes every session and pending login of a subject. The
module keeps, per subject, a revocation state that changes at each revocation. Each session and
pending login records the state it observed when it was issued, and it is valid only while that
state is still current. The outcome depends only on the order of the store operations, never on
the clocks of the machines that share the store. The state is checked at every request, so a
revocation takes effect at the next request, at the cost of one more store read.

| Requirement wording | Name in the request or in the module |
|---------------------|--------------------------------------|
| revoking a subject | a new operation of `session.Manager`; the request suggests `RevokeSubject(ctx, subject)` |
| revocation state | the per-subject value that the request calls "generazione"; the plan names it |
| authenticating a request with a session, and renewal | `(*session.Manager).Authenticate` |
| issuing a session | `(*session.Manager).Issue` |
| issuing and reading a pending login | `(*session.Manager).IssuePending` and `(*session.Manager).Pending` |
| logout | `(*session.Manager).Revoke` |
| the store contract | `auth.Store` |
| the in-memory store | `memstore.Store` |

**Changes to spec 007**:

- FR-034 of spec 007: authenticating a request with a session also reads the subject's revocation
  state, and a session of a revoked subject produces "no credentials" (FR-002 and FR-009 of this
  spec).
- SC-006 of spec 007: a request authenticated with a session makes exactly two store reads instead
  of one (FR-010). It still calls no lookup function, and its writes do not change.
- FR-039 of spec 007: a pending login of a revoked subject reads as cancelled (FR-003).
- FR-032 and FR-038 of spec 007: issuing a session or a pending login also reads the subject's
  revocation state and records it (FR-005).

### Clarifications

#### Session 2026-10-23

- Q: A login checks its first factor before a revocation, and issues its session after the
  revocation completes. Should that session survive? → A: Yes. The window between the developer's
  check and the issuance is accepted and documented. Closing it would need a comparison between
  the clocks of different instances (against FR-007), with a tolerance that reopens a smaller
  window. Closing it only for pending logins would leave password logins open (FR-006).

**Actors**:

- **Developer**: builds a service with the module, and calls the revocation from a password change
  handler, an administrative action or a background job.
- **User**: signs in on several browsers or devices, and changes their password.
- **Administrator**: disables accounts through the developer's application.
- **Attacker**: holds a stolen session cookie, or knows the user's password.
- **Maintainer**: evolves the module and publishes its releases.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - A password change ends the attacker's session (Priority: P1)

An attacker copied a user's session cookie. The user notices, changes their password and stays
signed in on their own browser. In the same request, the developer's handler updates the password,
revokes every session of the user and issues a new session for the current browser. From then on
the attacker's cookie no longer authenticates, on any instance, while the user's new session works.

**Why this priority**: it is the reason for the feature. Today a stolen session survives the
password change for up to 8 hours, and the developer has no way to end it.

**Independent Test**: with virtual time and the in-memory store, issue three sessions for one
subject and one for another subject. In one request, revoke the first subject and issue a new
session for it. Then authenticate a request with each session.

**Acceptance Scenarios**:

1. **Given** three sessions of a subject, **When** the subject is revoked, **Then** each of them
   produces "no credentials" at its next request.
2. **Given** a subject revoked in the current request, **When** a session is issued for it right
   after, in the same request, **Then** the new session authenticates, across renewals, until it
   expires.
3. **Given** sessions of two subjects, **When** one subject is revoked, **Then** the sessions of the
   other subject keep authenticating.
4. **Given** a revoked session and a route that admits anonymous requests, **When** a request
   carries the revoked session's cookie, **Then** it is served as anonymous, like any stale cookie
   (FR-034 of spec 007).
5. **Given** a session whose renewal interval has passed, **When** its subject is revoked while a
   request renews it, **Then** every request that starts after the revocation completes produces
   "no credentials".

---

### User Story 2 - Disabling an account ends its sessions at once (Priority: P1)

An administrator disables an account, or a background job reacts to a fraud signal. No request of
the user is at hand: the developer calls the revocation with a context and the subject. Every
session of the subject ends at its next request, on every instance that shares the store, and the
module calls no lookup function.

**Why this priority**: it is the same gap, triggered outside any request of the user. Sessions read
only the store (FR-041 of spec 007), so disabling the account in the developer's database does not
end them.

**Independent Test**: create two managers on one in-memory store, as two instances would. Issue a
session through each, revoke the subject through one manager from a context not bound to any
request, and authenticate both sessions.

**Acceptance Scenarios**:

1. **Given** sessions of a subject issued by two managers that share one store, **When** the subject
   is revoked through one of them, **Then** both sessions produce "no credentials".
2. **Given** a store that fails, **When** the developer revokes a subject, **Then** the revocation
   returns an error. **When** the developer revokes again after the store recovers, **Then** the
   sessions produce "no credentials".
3. **Given** a subject with no sessions, **When** it is revoked, **Then** the revocation succeeds,
   and sessions issued for the subject afterwards authenticate.
4. **Given** a store that fails while a request reads the revocation state, **When** the request is
   authenticated, **Then** it is answered with 500, never with 401.

---

### User Story 3 - A half-finished login does not outlive the revocation (Priority: P2)

An attacker who knows a user's password has passed the first factor, and holds a pending login
that waits for the second factor. The user changes the password, and the developer revokes the
user. The attacker's pending login is cancelled: a code submitted afterwards produces no session.

**Why this priority**: without it, the revocation leaves a gap of up to one pending login lifetime
for an attacker who already passed the first factor. It matters only for subjects enrolled in a
second factor, so it ranks after the sessions themselves.

**Independent Test**: with virtual time and the in-memory store, issue a pending login, revoke its
subject, then read the pending login.

**Acceptance Scenarios**:

1. **Given** a pending login of a subject, **When** the subject is revoked, **Then** reading the
   pending login produces "no credentials", so no second factor can be verified for it.
2. **Given** a subject just revoked, **When** a pending login is issued for it, **Then** it can be
   read and completed as usual.
3. **Given** pending logins of two subjects, **When** one subject is revoked, **Then** the pending
   login of the other subject is unaffected.

---

### User Story 4 - Upgrading logs nobody out (Priority: P3)

A developer upgrades from 0.2.0 a service with signed-in users, possibly one instance at a time.
Sessions and pending logins issued by 0.2.0 keep working until they expire or their subject is
revoked. If the developer rolls back, sessions issued by the new release keep working under 0.2.0,
which does not enforce revocations. The store contract does not change, so the developer's store
adapter needs no change.

**Why this priority**: the module is in use, and an upgrade that logs everyone out would be a
regression. It concerns the transition, not the feature itself.

**Independent Test**: authenticate, with this release, a session record in the format written by
0.2.0, before and after revoking its subject. Decode a record written by this release with the
rules of 0.2.0.

**Acceptance Scenarios**:

1. **Given** a session issued by the previous release, **When** this release authenticates it,
   **Then** it authenticates. **When** its subject is then revoked, **Then** it produces "no
   credentials".
2. **Given** a session issued by this release, **When** the previous release authenticates it,
   **Then** it authenticates.
3. **Given** a store adapter written for 0.2.0, **When** the developer upgrades, **Then** it builds
   and works unchanged.

---

### Edge Cases

- A subject is revoked twice, possibly at the same time: every session issued before either
  revocation produces "no credentials", and sessions issued after both authenticate.
- A revocation runs while a session of the same subject is being issued: when the issuance read
  the revocation state before the revocation changed it, the issued session authenticates 0
  requests (the prudent outcome); otherwise it authenticates. The issuance itself succeeds and sets
  its cookie either way.
- The first factor of a login is checked before a revocation, and its session is issued after the
  revocation completes: the session is valid (FR-006). The window lasts from the developer's check
  to the issuance, tens of milliseconds for a password; for a pending login, from reading it to
  the issuance.
- A request that read the revocation state before a revocation completed may still be served. The
  revocation applies to requests that start after it completes.
- The revocation state of a subject expires (FR-012): sessions issued after that revocation keep
  authenticating, and sessions issued before it have all expired by then.
- A store that discards keys before their expiry, for example under memory pressure, can lose a
  revocation state, and the revoked sessions that have not expired authenticate again. Attempt
  limits and replay protection already depend on the same property; the documentation states it
  (FR-018).
- The revocation clears no cookie: other browsers keep cookies that now produce "no credentials".
  The cookie of the current request changes only if the developer issues a new session or logs
  out.
- An empty subject: the revocation returns an error and does not touch the store.
- Two instances whose clocks differ: every outcome stays the same (FR-007).
- API keys, tokens and Basic credentials of the subject are not sessions, and the revocation does
  not affect them.
- During a rolling upgrade, or after a rollback, instances running 0.2.0 do not enforce
  revocations (FR-017).

## Requirements *(mandatory)*

### Functional Requirements

#### Revocation

- **FR-001**: Developers MUST be able to revoke every session and pending login of a subject with
  one operation that takes a context and the subject, and no HTTP request, so that it can run in a
  password change handler, an administrative action or a background job. An empty subject MUST be
  rejected with an error, without touching the store.
- **FR-002**: Once a revocation has completed, every session of the subject issued before it MUST
  produce "no credentials" at its next request, on every instance that shares the store, whatever
  the state of its renewal, without calling any lookup function.
- **FR-003**: Once a revocation has completed, every pending login of the subject issued before it
  MUST read as cancelled ("no credentials"), so that no second factor can be verified for it.
- **FR-004**: Sessions and pending logins issued after a revocation has completed, including a
  session issued in the same request right after it, MUST be unaffected by it. Sessions and pending
  logins of other subjects MUST be unaffected.
- **FR-005**: Issuing a session or a pending login MUST read the subject's current revocation state
  and record it. When a revocation changes the state after the issuance read it, the issued session
  or pending login MUST be treated as revoked.
- **FR-006**: A login whose first factor is checked before a revocation, and whose session is
  issued after the revocation completes, MUST be accepted: its session is valid, like any session
  issued after the revocation (FR-004). The same holds for a pending login read before the
  revocation and completed after it. The window between the developer's check and the issuance is
  a documented limit (FR-018).
- **FR-007**: Every outcome MUST depend only on the order of the operations on the store, never on
  the clocks of the instances that share it.
- **FR-008**: A store failure during a revocation MUST be reported as an error. Repeating a
  revocation MUST be harmless: it revokes what a single revocation at that moment would.

#### Checking

- **FR-009**: Authenticating a request with a session MUST check the subject's revocation state at
  every request, so that a revocation takes effect at the next request. The check MUST always be
  active; no option turns it off.
- **FR-010**: A request authenticated with a session MUST make exactly two store reads and call no
  lookup function; renewal keeps writing at most once per renewal interval (FR-036 of spec 007).
  Issuing a session, issuing a pending login and reading a pending login MUST each make exactly one
  store read more than in the previous release.
- **FR-011**: A store failure while reading the revocation state MUST produce an error, never "no
  credentials". A request is then answered with 500 (FR-014 of spec 007), and an issuance sets no
  cookie.

#### Revocation state

- **FR-012**: The revocation state of a subject MUST last at least as long as any session or
  pending login issued before the revocation can still be valid, and MUST then expire on its own,
  so that revocations do not make the store grow without bound.
- **FR-013**: A subject without a revocation state, because it was never revoked or because its
  state expired, MUST be treated as not revoked. A session issued after a revocation MUST keep
  authenticating after that revocation's state expires.
- **FR-014**: The feature MUST use the store contract unchanged: no new operation, and no change to
  existing adapters. The rule that the module never reads a key it increments MUST still hold.
- **FR-015**: The revocation state and the session and pending login records MUST NOT contain
  secrets in clear (FR-022 of spec 007).

#### Compatibility

- **FR-016**: Sessions and pending logins issued by the previous release MUST keep working after the
  upgrade until they expire, and a revocation of their subject MUST apply to them.
- **FR-017**: Records written by this release MUST remain readable by the previous release, so that
  a rollback logs nobody out. Instances that run the previous release, during a rolling upgrade or
  after a rollback, do not enforce revocations.

#### Documentation and release

- **FR-018**: The package documentation and the README MUST describe the revocation: the cases it
  serves (password change, disabled account, logging out of every device), the order that keeps
  the current user signed in (revoke, then issue a new session), its cost of one more store read
  per request, the window of FR-006 with the advice to issue the session right after checking the
  first factor, the behavior of FR-017 during upgrades and rollbacks, and the requirement that the
  store never discards a key before its expiry.
- **FR-019**: The module's changelog MUST record the new operation under the unreleased version, as
  an addition.
- **FR-020**: Tests MUST reproduce deterministically, with virtual time and a store wrapper, the
  interleavings of a revocation with an issuance, a renewal and the reading of a pending login, and
  MUST check the concurrent cases with race detection (Constitution Principle III).

### Key Entities

- **Revocation state**: per subject, a value that changes at each revocation and expires on its
  own. Its absence means that the subject was never revoked, or that its last revocation expired.
- **Session record**: the stored state of a session. It gains the revocation state observed at
  issuance. Its format version does not change; a record of the previous release reads as having
  observed no revocation.
- **Pending login record**: the stored state of a pending login. It gains the revocation state
  observed at issuance, like the session record.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: In 100% of the tested cases, after a revocation completes, every session of the
  subject issued before it authenticates 0 requests, and every pending login of the subject issued
  before it can be completed 0 times. The cases cover sessions that are fresh, due for renewal,
  and issued by the previous release.
- **SC-002**: In 100% of the tested cases, a session issued in the same request right after a
  revocation authenticates every request until it expires, and the sessions of other subjects are
  never rejected.
- **SC-003**: With at least 100 concurrent requests carrying sessions of a subject while it is
  revoked, 0 requests that start after the revocation completes are authenticated, and race
  detection reports 0 data races.
- **SC-004**: When a revocation and an issuance for the same subject happen at the same instant of
  virtual time, in either order, 100% of the outcomes follow the order of the operations: an
  issuance before the revocation is revoked, and an issuance after it is not.
- **SC-005**: A request authenticated with a session makes exactly two store reads and 0 calls to
  lookup functions. A session used once per second for a full idle timeout causes at most as many
  store writes as there are renewal intervals in that timeout. Verifying a session against the
  in-memory store still costs less than 1% of one password check (SC-009 of spec 007).
- **SC-006**: In 100% of the tested store failures while reading the revocation state, a request is
  answered with 500, never with 401, and an issuance sets no cookie.
- **SC-007**: Once the documented retention of a revocation state (FR-012) has passed since the last
  revocation, the in-memory store holds 0 entries for revocation states.
- **SC-008**: After every flow of this spec runs against the in-memory store, a scan of all its keys
  and values finds 0 secrets in clear (SC-003 of spec 007).
- **SC-009**: 100% of the acceptance scenarios and edge cases of this spec are covered by automated
  tests. Statement coverage of the module stays at least 80%. Against `auth/v0.2.0`, the API
  compatibility check reports compatible additions only.

## Assumptions

- **Release**: the feature ships in the next minor release of the module (0.3.0). It only adds to
  the API, and the store contract does not change.
- **Always active**: the request decides that the check runs at every request. No option turns it
  off: an option would save one read only by silently giving up revocation, and a revocation called
  with the check off would do nothing.
- **Prudent outcome on overlap**: the request decides that a session issued while a revocation
  runs is discarded when it observed the old state. The user signs in again; no attacker keeps a
  session.
- **Window between check and issuance**: the module does not see when the developer checks the
  first factor, so it cannot tell a login that checked the old password from one that checked the
  new one (FR-006). Only an attacker who knows the password, and logs in while the user changes it,
  can use the window; with a second factor, they also need a code at every login.
- **Sessions only**: API keys are read from the developer's database at every request, so deleting
  them takes effect at once. Tokens are valid until they expire, and revoking them belongs to the
  identity provider. Basic checks the password at every request. None of them is in scope.
- **No session listing and no "all but this one" variant**: revoking and then issuing a new session
  covers "log out of the other devices", and the current browser gets a new identifier. Listing the
  sessions of a subject would need an index in the store that the store contract does not offer.
- **The store keeps keys until they expire**: a store that discards keys early already weakens
  attempt limits and replay protection. This feature documents the requirement (FR-018) instead of
  working around it.
- **Tests**: interleavings are reproduced with virtual time and a store wrapper, which keeps the
  tests deterministic and hermetic (FR-070 of spec 007, Constitution Principle III). The concurrent
  tests of SC-003 assert outcomes that hold for every schedule.

## Appendix: original request

The request, as submitted to `/speckit-specify`:

````text
Nel modulo auth, sotto-package session: revocare tutte le sessioni di un utente (subject).

Problema: oggi una sessione resta valida fino alla scadenza (fino a 8 ore se usata) anche dopo un cambio password, un account disabilitato o un "esci da tutti i dispositivi". Lo sviluppatore non può rimediare da fuori: nello Store ci sono solo gli hash degli identificatori di sessione e il Principal non espone quando la sessione è nata. Esempio concreto: un attaccante ruba una sessione, la vittima cambia password, e l'attaccante resta dentro.

Decisioni già prese:
- Nuova operazione sul Manager, per esempio RevokeSubject(ctx, subject), che invalida tutte le sessioni del subject; devono restare invalide anche i login pending del subject, così un login a metà iniziato prima della revoca non produce una sessione valida dopo.
- Meccanismo a generazione per utente, senza confronti tra orologi: la revoca scrive un valore casuale nuovo in una chiave dello Store derivata dal subject, con TTL un po' più lungo della durata massima di una sessione; Issue legge la generazione corrente e la salva nel record; Authenticate scarta i record con una generazione diversa. Una chiave assente equivale a "mai revocato".
- Il controllo avviene a ogni richiesta autenticata con una sessione (una lettura in più dallo Store), così la revoca ha effetto immediato.
- Il caso comune deve funzionare: cambio password, revoca e nuova sessione per l'utente nella stessa richiesta.
- Se una revoca arriva mentre un Issue è in corso, la sessione emessa viene scartata (lato prudente).
- Un errore dello Store durante il controllo risponde 500, mai 401, come il resto del modulo.
- L'interfaccia auth.Store non cambia; nessun breaking change, release minore (0.3.0). Il nuovo campo del record non richiede di cambiare la versione del formato, perché il codice precedente ignora i campi JSON sconosciuti.
````

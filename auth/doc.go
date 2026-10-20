// Package auth authenticates HTTP requests. A small core combines
// authentication methods and turns their outcome into a response; each method
// lives in its own sub-package:
//
//   - password: argon2id hashing, and checking against the developer's
//     accounts, also legacy bcrypt hashes (needs a lookup);
//   - session: server-side sessions in a __Host- cookie and pending logins
//     (needs a Store);
//   - totp and recovery: the second factor (need a lookup and a Store);
//   - basic: HTTP Basic on the password checker;
//   - apikey: API keys stored as hashes (needs a lookup);
//   - jwt: tokens from an identity provider (needs neither: only its keys);
//   - memstore: an in-memory Store.
//
// # Outcomes
//
// An Authenticator returns a principal or one of three errors, compared with
// errors.Is:
//   - ErrNoCredentials: the request carries none for this method. Any moves on
//     to the next method, and the middleware answers 401, or lets the request
//     through with WithAnonymous;
//   - ErrInvalidCredentials: credentials are present but not valid. The
//     middleware answers 401 without trying other methods, also with
//     WithAnonymous;
//   - any other error: the method could not decide, for example because a
//     database or the Store is unreachable. The middleware answers 500, never
//     401.
//
// ErrTooManyAttempts, returned when an attempt limit is reached, is answered
// with 429.
//
// # Challenges
//
// Every 401 carries a WWW-Authenticate challenge per method that implements
// Challenger (RFC 9110 §11.6.1), one field line each and each identical
// challenge once. When a method rejects invalid credentials, its challenge
// for ErrInvalidCredentials, such as Bearer error="invalid_token", replaces
// its plain challenge and every other challenge of the same scheme.
//
// # Sensitive routes
//
// RequireMethods admits only principals whose Methods include the required
// RFC 8176 values, typically "mfa". A principal that lacks one is refused with
// ErrInsufficientMethods: 401 with the step-up challenge of RFC 9470 for
// tokens, and 403 for sessions, API keys and Basic. The RejectFunc configured
// with WithRejectFunc writes every refusal, for example as an RFC 9457 problem
// document.
//
// # Second factor
//
// After the password, session.Manager.IssuePending starts a pending login that
// authenticates nothing. totp.Verifier.Verify and recovery.Verifier.Verify
// check a code through Pending.Attempt, which allows five attempts per pending
// login, shared by both kinds of code, and counts each one before evaluating
// it, so that parallel requests cannot exceed the budget.
//
// # Store
//
// The Store holds sessions, pending logins and counters, for example in
// Redis. The library chooses every key, derives the keys of secrets from
// their SHA-256 hash, and never writes a secret in clear. Store.Incr must be
// atomic and must set the expiry only when it creates the counter.
//
// # Context
//
// Methods bound to an HTTP request, such as Authenticate, take their context
// from the request, as net/http handlers do. Every other operation that
// performs I/O takes a context.Context as its first parameter.
package auth

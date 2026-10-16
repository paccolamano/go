// Package middleware composes reusable HTTP server middleware.
//
// Every middleware of this module lives in its own subpackage and has the
// shape func(http.Handler) http.Handler ([Middleware]), so it works with
// net/http and with any router built on it, and mixes freely with middleware
// from other libraries. [Chain] composes middleware in the order chosen by the
// caller: the first one is the outermost, it receives the request first and
// the response last. Chains are immutable: [Chain.Append] and [Chain.Extend]
// return new chains, so a base chain can be shared and specialized for
// different route groups.
//
// # Conventions
//
// Every subpackage exposes a constructor New that takes the mandatory
// settings followed by functional options:
//
//	mw, err := ratelimit.New(100, time.Minute, ratelimit.WithBurst(20))
//
// Options have documented defaults, and new options can be added in later
// releases without breaking existing code. The configuration is validated
// once, by New: invalid or contradictory settings are reported as an error
// that matches [ErrInvalidConfig] with errors.Is, before any request is
// served.
//
// Every middleware that refuses or fails a request (bodylimit, contenttype,
// decompress, ratelimit, maxconcurrency and recovery) follows one standard to
// customize its response: a WithRejectFunc option that takes a [RejectFunc].
// The function receives a [Rejection] with the status the middleware would
// send and a sentinel error identifying the cause, after the protocol headers
// of the condition (such as Retry-After) have been set. One function can
// therefore serve the whole chain, for example to answer with JSON documents.
// If it writes nothing, the status is still sent. The default response is a
// short plain-text body.
//
// Middleware that store request-scoped data expose a typed accessor:
// requestid.FromContext, realip.PeerFromContext and
// secureheaders.NonceFromContext. Context keys are never exported.
//
// # Recommended order
//
// From the outermost to the innermost:
//
//  1. realip: every later middleware sees the real client address.
//  2. requestid: the identifier is available to logging.
//  3. accesslog: it records refusals and the 500 written by recovery.
//  4. recovery: it catches the panics of everything inside it.
//  5. secureheaders: refusals carry the headers too.
//  6. cors: preflights are answered before any limiting, and refusals such as
//     429 stay readable by browser scripts.
//  7. ratelimit
//  8. maxconcurrency
//  9. bodylimit: it bounds the encoded size.
//  10. decompress: it bounds the decoded size.
//  11. contenttype
//  12. compress
//  13. etag: it computes the tag on the uncompressed content, and compress
//     weakens it.
//
// # Ordering pitfalls
//
//   - accesslog placed before realip logs the proxy address instead of the
//     client address.
//   - recovery only catches panics raised by the middleware and handlers placed
//     after it. Panics raised in goroutines started by a handler cannot be
//     recovered by any middleware.
//   - cors placed after ratelimit (or any other refusing middleware) leaves
//     the refusals without CORS headers, so browser scripts cannot read them.
//   - The same ratelimit or maxconcurrency instance placed in two chains
//     shares its counters across them; separate instances are independent.
//   - Long-lived requests, such as WebSocket connections and server-sent
//     events, hold a maxconcurrency slot until their handler returns.
package middleware

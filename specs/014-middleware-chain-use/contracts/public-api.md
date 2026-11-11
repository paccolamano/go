# Contract: Public API of `middleware` 0.2.0

**Feature**: [spec.md](../spec.md) | **Plan**: [plan.md](../plan.md)

Only the root package `github.com/paccolamano/go/middleware` changes. The subpackages and
their constructors are unchanged (FR-016).

## Chain at 0.2.0

```go
// Chain is an immutable, ordered list of middleware. The first middleware is
// the outermost: it receives the request first and the response last.
//
// The zero value is an empty chain. Append, Use and AppendChain return new
// chains and never modify the receiver, so a base chain can be shared and
// specialized for different route groups.
//
// A chain also records the errors of the constructors added with Use. Check
// them once, with Err, on the most derived chain before calling Then: Then
// panics on a chain with recorded errors.
type Chain struct{ /* unexported: mws []Middleware, errs []error */ }

func New(mws ...Middleware) Chain                 // unchanged
func (c Chain) Append(mws ...Middleware) Chain    // unchanged signature; carries c's errors
func (c Chain) Use(mw Middleware, err error) Chain // NEW
func (c Chain) AppendChain(other Chain) Chain     // NEW, replaces Extend
func (c Chain) Err() error                        // NEW
func (c Chain) Then(h http.Handler) http.Handler  // unchanged signature; panics on recorded errors
```

Unchanged: `Middleware`, `Rejection`, `RejectFunc`, `ErrInvalidConfig`.

## Doc comments of the new and changed methods

```go
// Use returns a new chain with the result of one constructor added after the
// middleware of c. It takes the two results directly:
//
//	chain = chain.Use(requestid.New())
//
// When err is not nil, mw is not added and err is recorded in the new chain,
// to be reported by [Chain.Err]. When err is nil, Use panics if mw is nil: the
// constructor has a bug.
func (c Chain) Use(mw Middleware, err error) Chain

// AppendChain returns a new chain with the middleware of c followed by those
// of other, so the middleware of c stay outermost. The errors recorded in
// other follow those of c.
func (c Chain) AppendChain(other Chain) Chain

// Err returns the errors recorded by [Chain.Use] in c and in the chains it
// derives from, joined with [errors.Join] in the order they were recorded, or
// nil if there is none. Chains derived from a common base do not see each
// other's errors.
func (c Chain) Err() error

// Then applies the chain to h and returns the resulting handler. Each
// middleware is called once, here, and not on every request. For an empty
// chain Then returns h itself.
//
// It panics if c has recorded errors (see [Chain.Err]), listing them, and if
// h is nil.
func (c Chain) Then(h http.Handler) http.Handler
```

## Panics

All of them are programming errors, raised while chains are built or applied at startup, never
while a request is served.

| Call | Condition | Panic value |
|------|-----------|-------------|
| `New`, `Append` | a nil middleware (unchanged) | `string`: `middleware: nil middleware at position N` |
| `Use` | `mw == nil && err == nil` (**new**) | `string`: `middleware: nil middleware at position N`, N = `len(c.mws)` |
| `Then` | recorded errors (**new**, checked first) | `error`: `middleware: chain has configuration errors:\n<errors.Join of the recorded errors>`, wrapping them with `%w` |
| `Then` | `h == nil`, no recorded errors (unchanged) | `string`: `middleware: nil handler` |

## Removed

| 0.1.0 | 0.2.0 |
|-------|-------|
| `func (c Chain) Extend(other Chain) Chain` | `AppendChain`, same semantics, also carries `other`'s errors |

No deprecated alias (research R-01).

## gorelease report against `middleware/v0.1.0`

As produced by the spike ([research R-01](../research.md#r-01-release-level-and-api-compatibility)):

```text
## incompatible changes
Chain.Extend: removed
## compatible changes
Chain.AppendChain: added
Chain.Err: added
Chain.Use: added

# summary
Suggested version: v0.2.0 (with tag middleware/v0.2.0)
```

## CHANGELOG entry

Under `## [Unreleased]` of `middleware/CHANGELOG.md` (FR-021). The release commit
`chore(middleware): prepare release 0.2.0` renames the heading to `## [0.2.0] - <date>`.

```markdown
### Removed

- **Breaking**: `Chain.Extend`. Replace `Extend` with `AppendChain`.

### Added

- `Chain.Use`, which takes the two results of a constructor directly:
  `chain.Use(requestid.New())`. When the error is not nil, the middleware is not added and the
  error is recorded in the new chain.
- `Chain.Err`, which returns every error recorded in a chain and in the chains it derives from,
  joined in the order recorded. One check on the most derived chain covers all of them.
- `Chain.AppendChain`, the former `Extend`: the middleware of the chain, then those of the
  other chain. It also carries the other chain's recorded errors.
- `Chain.Then` panics on a chain with recorded errors, with every error in the message, so a
  forgotten `Err` check fails at startup. Chains built without `Use` behave as before.
```

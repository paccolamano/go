# Data Model: Building a Chain From the Constructors' Results (middleware 0.2.0)

**Feature**: [spec.md](spec.md) | **Plan**: [plan.md](plan.md) | **Date**: 2026-11-11

The feature has no storage. Its only state is the `Chain` value, built at startup and never
modified afterwards.

## Chain

An immutable, ordered list of middleware, with the errors recorded while it was built.

| Field | Type | Meaning |
|-------|------|---------|
| `mws` | `[]Middleware` (unexported) | Unchanged from 0.1.0. The first entry is the outermost. It never contains nil. |
| `errs` | `[]error` (unexported, **new**) | The errors recorded by `Use` in this chain and in the chains it derives from, in the order recorded. It never contains nil. |

**Invariants**:

1. **No nil middleware**: every entry of `mws` was checked by `New`, `Append` or `Use`. `AppendChain`
   copies entries from chains that hold the same invariant.
2. **No nil error**: `Use` records `err` only when it is not nil.
3. **Write once**: the arrays behind `mws` and `errs` are written only when the chain is created.
   A derivation that adds elements builds a new array with `slices.Concat`. A derivation that
   adds nothing to a slice reuses it as is. No code calls `append` on a slice of an existing
   chain ([research R-03](research.md#r-03-storing-the-recorded-errors)).
4. **Zero value**: `Chain{}` is an empty chain with no recorded errors. `New()` is equivalent.

## Derivations

Every operation returns a new `Chain` and leaves its receiver, and `other`, unchanged.

| Operation | `mws` of the result | `errs` of the result | Panics |
|-----------|---------------------|----------------------|--------|
| `New(mws...)` | copy of `mws` | none | a nil middleware: `middleware: nil middleware at position i` |
| `c.Append(mws...)` | `c.mws` + `mws` (new array) | `c.errs`, reused | a nil middleware: position `len(c.mws)+i` |
| `c.Use(mw, err)`, `err != nil` | `c.mws`, reused; `mw` ignored even when not nil | `c.errs` + `err` (new array) | never |
| `c.Use(mw, nil)`, `mw != nil` | `c.mws` + `mw` (new array) | `c.errs`, reused | never |
| `c.Use(nil, nil)` | | | `middleware: nil middleware at position len(c.mws)` |
| `c.AppendChain(other)` | `c.mws` + `other.mws` (new array) | `c.errs` + `other.errs` (new array) | never |

## Reading and applying

| Operation | Result | Panics |
|-----------|--------|--------|
| `c.Err()` | `nil` when `c.errs` is empty, otherwise `errors.Join(c.errs...)`. It does not change `c`. | never |
| `c.Then(h)`, `c.errs` not empty | | an `error` value: `middleware: chain has configuration errors:` then the joined errors, wrapped with `%w`. Checked before `h`. |
| `c.Then(nil)`, `c.errs` empty | | `middleware: nil handler` (unchanged) |
| `c.Then(h)`, otherwise | `h` wrapped by `c.mws`, from the innermost to the outermost (unchanged) | never |

## Relationships

Chains form a derivation graph, built at startup. Each chain sees the errors of its ancestors,
never those of its siblings or descendants:

```text
base ──Use(cors: error A)──▶ public ──Use(bodylimit: error B)──▶ api ──Append(authenticate)──▶ authenticated
errs: []                     errs: [A]                          errs: [A, B]                    errs: [A, B]
  │
  └──Use(ratelimit: error C)──▶ admin
                                errs: [C]
```

`authenticated.Err()` reports A then B. `admin.Err()` reports C only. `base.Err()` is nil, and
`base.Then` works.

`AppendChain` joins two branches: `x.AppendChain(y)` has `x.errs` followed by `y.errs`. When `y`
derives from `x`, the errors they share appear twice, as their middleware do.

## Validation rules (from the spec)

| Rule | Requirement |
|------|-------------|
| An error from a constructor is recorded, and its middleware is not added | FR-003, FR-006 |
| A constructor result with neither a middleware nor an error panics when added | FR-004 |
| `Err` lists every recorded error of the chain and of its ancestors, in order, with no text of its own | FR-007, FR-008 |
| Errors never flow to the base or to siblings | FR-009 |
| Reading errors never clears them | FR-010 |
| `Then` panics on recorded errors, before the nil-handler check | FR-011, edge case |
| `AppendChain` carries `other`'s errors after `c`'s | FR-014 |

# Implementation Plan: Reusable HTTP Middleware Module

**Branch**: `002-http-middleware` (suggested; work currently on `main`) | **Date**: 2026-10-16 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `specs/002-http-middleware/spec.md`

**Note**: This template is filled in by the `/speckit-plan` command; its definition describes the execution workflow.

## Summary

This feature adds a new independent module, `github.com/paccolamano/go/middleware`. It has
**a single `go.mod`**, at `middleware/go.mod`. Its root package provides an immutable **chain**.
Each middleware lives in its own folder under `middleware/`, as a package of that same module:
no folder has a `go.mod` of its own, so there are no nested modules. The thirteen middleware
packages are:

- bodylimit
- accesslog
- cors
- recovery
- requestid
- secureheaders
- realip
- compress
- decompress
- etag
- ratelimit
- maxconcurrency
- contenttype

Every middleware has the shape `func(http.Handler) http.Handler`, exposed as the alias
`middleware.Middleware`. Each is built by
`New(<mandatory...>, opts ...Option) (middleware.Middleware, error)`. The options are functional
and validated together when the middleware is built, and every invalid configuration wraps one
sentinel, `middleware.ErrInvalidConfig`.

Any response a middleware sends when it refuses or fails a request can be replaced through one
standard: `WithRejectFunc(middleware.RejectFunc)`. The function receives a
`middleware.Rejection`, which holds the status and a sentinel for the cause and can gain fields
in later releases ([contracts/rejection.md](contracts/rejection.md)).

Wrappers of the response writer preserve the standard library's flush, hijack and
per-request deadline controls (via `Unwrap`). Middleware that only touch headers or the request
do not wrap the writer.

Everything is built on the standard library only: `net/http`, `net/netip`, `log/slog`,
`compress/gzip`, `compress/zlib`, `crypto/rand`, `crypto/sha256`, `mime`, `hash/maphash`. Tests
use `testing`, `httptest`, `testing/synctest`, fuzzing and benchmarks.

## Technical Context

**Language/Version**: Go. The `go` directive is `go 1.26.0`, the oldest version the
constitution supports. CI also tests on 1.27.

**Primary Dependencies**: standard library only. Every third-party candidate was rejected:
`x/time/rate`, `klauspost/compress`, `andybalholm/brotli`, `httpsnoop` and `google/uuid` (see
[research.md R-23](research.md#r-23-dependencies)).

**Storage**: in-memory state only.

- ratelimit: a GCRA timestamp per client key, in 64 sharded maps.
- maxconcurrency: a semaphore.
- compress: one writer pool per instance.

**Testing**: the standard `testing` package, with table-driven subtests and
`httptest.NewRequestWithContext(t.Context(), …)`.

- `testing/synctest` for time-dependent behavior.
- `httptest.NewServer` on loopback for connection-level behavior.
- Fuzz tests for each parser of external input.
- Benchmarks with `b.Loop`.
- `Example` functions in every package.

Details: [research.md R-22](research.md#r-22-testing-approach-principle-iii).

**Target Platform**: any platform that runs Go HTTP servers. The code is pure Go with no build
constraints, and builds for linux, darwin and windows on amd64 and arm64.

**Project Type**: library. It is one Go module with a single `go.mod` (`middleware/go.mod`),
made of the root package, one folder per middleware (13) and 4 `internal/` folders. All of
them are packages of that one module, and none has its own `go.mod`.

**Performance Goals**:

- Under 0.1 ms added per request for the full non-buffering chain with defaults (SC-005),
  measured by `BenchmarkRecommendedChain`.
- Header-only middleware (requestid, secureheaders, cors) wrap nothing and allocate only the
  header values.
- compress reuses its writers from a pool.

**Constraints**:

- No package-level mutable state (`gochecknoglobals`); only sentinel errors are package-level.
- No `init`.
- No stdout/stderr output and no global logger; the `*slog.Logger` is injected.
- No goroutine outlives a request.
- `panic` appears only where R-03 and R-11 justify it, each with a `//nolint` that states the
  reason.
- Bounded memory: etag buffers at most 1 MiB, compress buffers at most `minSize` before
  deciding, decompress caps the decoded size at 10 MiB, and ratelimit reclaims idle keys.

**Scale/Scope**: about 13 middleware. The expected traffic is any production service, from
thousands of requests per second per instance upwards. ratelimit is sized for millions of keys
(SC-008).

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

| Rule | Assessment | Status |
|------|------------|--------|
| **I. Independent modules** | A single new top-level module `middleware/`. It has exactly one `go.mod` (module path `github.com/paccolamano/go/middleware`), one README.md and one CHANGELOG.md. The middleware folders are packages of this module, not nested modules. It has no `replace` and no dependency on other repository modules. It builds and tests with `GOWORK=off`. Its single purpose is reusable HTTP server middleware, which is not a grab-bag: the shared code lives in single-purpose `internal/` packages named after what they do (`respwriter`, `httpheader`, `mediatype`, `netaddr`), none of them called `utils` or similar. | ✅ |
| **II. SemVer & stable API** | Tags are `middleware/vX.Y.Z`, starting at v0.1.0. All extension happens through new `With*` option functions and new fields in result structs, both compatible changes for gorelease (FR-004). The exported surface is minimal: config structs, writers and parsers are unexported or internal, and `Middleware` is an alias rather than a new type. Every change goes into the CHANGELOG. | ✅ |
| **III. Test-first** | `tasks.md` will order test tasks before implementation tasks. The suite uses the standard `testing` package with table-driven subtests. Time is faked with `testing/synctest`: no `time.Sleep` synchronization and no wall-clock dependency. Network use is limited to in-process loopback `httptest` servers, with no external access. Each parser has a fuzz test, the hot paths have benchmarks, and each package has an `Example`. The coverage target is ≥ 80% for the module. All tests pass with `-race -shuffle=on`. | ✅ |
| **IV. Idiomatic Go** | The repository's golangci-lint config applies unchanged. Every exported identifier and package is documented. Configuration errors wrap `ErrInvalidConfig` (so `errors.Is` works). Each refusal cause is a package sentinel (`ErrTooLarge`, `ErrUnsupported`, `ErrUnsupportedEncoding`, `ErrLimitExceeded`, `ErrOverloaded`, `ErrPanic`), and so is decompress's `ErrInvalidBody`. Logging goes only through the injected `*slog.Logger`. There is no `init` and no global mutable state: pools, maps and semaphores live in each instance. No goroutine outlives a request: ratelimit sweeps lazily and maxconcurrency waits on the request goroutine. Request work honors `r.Context()`. **`panic`** is used in only two places, neither for a recoverable condition: (a) a nil middleware or handler passed to the chain, which is a programming error and follows the `net/http.ServeMux.Handle` precedent; (b) recovery re-raising `http.ErrAbortHandler`, which is the net/http abort protocol (FR-035, FR-037). Each use carries a `//nolint:forbidigo` with its reason. The `//nolint:wrapcheck` lines on pass-through I/O methods are required by the `io` contracts. | ✅ (justified `nolint`s) |
| **V. Simplicity & dependencies** | No third-party dependencies, test-only ones included. Every option traces back to a functional requirement. The extension points that exist are the ones the spec asks for: custom codings (FR-054), `RejectFunc` (FR-010), the ratelimit key function (FR-069) and the CORS origin function (FR-026). There is no clock injection API (synctest covers it) and no storage interface for ratelimit (out of scope). There is no cgo. | ✅ |
| Go toolchain support | `go 1.26.0` is not above `GO_VERSION_OLDEST=1.26`. The features used all exist in 1.26: `testing/synctest` (1.25), `t.Context` and `b.Loop` (1.24), `crypto/rand.Text` (1.24), `http.ResponseController` (1.20), `net/netip` (1.18). | ✅ |
| Platforms | The code is pure Go with no OS-specific code, and the `cross-build` status covers all six targets. There is no `.module-exceptions` entry. | ✅ |
| Repository layout | No tooling change. Running `make work` adds `./middleware` to `go.work`. The shared lint config is used without overrides. | ✅ |
| Dependency hygiene | With an empty `require` list, `go mod tidy -diff` is clean. | ✅ |
| Vulnerability scanning / Secrets | Trivy finds no dependencies to scan. Tests and examples contain no credentials: the example origins use `example.com`, and the proxy ranges are RFC 5737/1918 addresses. | ✅ |
| Local and PR gates | `make check MODULE=middleware` runs lint, then race and shuffle tests with the coverage gate. The PR statuses apply automatically, because the tooling discovers new modules on its own. | ✅ |
| Commits / Release | The PR title is `feat(middleware): …`. The release is `middleware/v0.1.0`, with a CHANGELOG section. | ✅ |

**Result (pre-research)**: PASS. The only deviations are the documented `//nolint`s, which the
constitution allows when they state their reason. Nothing needs a Complexity Tracking entry.

**Result (post-design re-check)**: PASS. The data model and the four contracts add no
dependency, no global state and no module exception. Every exported identifier in
[contracts/public-api.md](contracts/public-api.md) traces back to a functional requirement.

## Project Structure

### Documentation (this feature)

```text
specs/002-http-middleware/
├── plan.md              # This file
├── research.md          # Phase 0: decisions R-01 … R-24
├── data-model.md        # Phase 1: entities, configuration, validation rules, state machines
├── quickstart.md        # Phase 1: validation guide
├── contracts/
│   ├── public-api.md    # exported Go API of every package
│   ├── rejection.md     # standard for customizing refusal responses
│   ├── http-behavior.md # per-middleware HTTP behavior (statuses, headers) with FR mapping
│   └── log-records.md   # accesslog and recovery record schemas
├── checklists/
│   └── requirements.md  # spec quality checklist
└── tasks.md             # Phase 2 (/speckit-tasks, not created here)
```

### Source Code (repository root)

```text
go.work                          # + ./middleware (make work)

middleware/
├── go.mod                       # the ONLY go.mod: module github.com/paccolamano/go/middleware; go 1.26.0
├── README.md                    # purpose, go get, minimal example, min Go, recommended order
├── CHANGELOG.md                 # Keep a Changelog; [Unreleased] → [0.1.0]
├── doc.go                       # package doc: conventions, recommended order, pitfalls
├── middleware.go                # Middleware (alias), Rejection, RejectFunc, ErrInvalidConfig
├── chain.go                     # Chain, New, Append, Extend, Then
├── chain_test.go
├── example_test.go              # ExampleChain, ExampleNew (recommended chain)
├── integration_test.go          # cross-middleware scenarios (US2), ordering pitfalls
├── bench_test.go                # BenchmarkRecommendedChain (SC-005)
│
├── accesslog/                   # accesslog.go, options.go, *_test.go, example_test.go
├── bodylimit/
├── compress/                    # + writer.go (transforming wrapper, pools)
├── contenttype/
├── cors/                        # + origin.go (parsing/matching), fuzz_test.go
├── decompress/                  # + reader.go (lazy decoder)
├── etag/                        # + writer.go (buffering wrapper), fuzz_test.go
├── maxconcurrency/
├── ratelimit/                   # + gcra.go, store.go (shards, sweep)
├── realip/                      # + parse.go (XFF, Forwarded), fuzz_test.go
├── recovery/
├── requestid/                   # + fuzz_test.go (validation)
├── secureheaders/
│
└── internal/
    ├── respwriter/              # observing ResponseWriter: status, bytes, started, hijacked,
    │                            # 1xx handling, Unwrap/Flush/Hijack
    ├── httpheader/              # token lists, q-values, Accept-Encoding negotiation,
    │                            # Vary token add, entity-tag list parsing + fuzz tests
    ├── mediatype/               # media-type patterns (exact, type/*, type/*+suffix) + fuzz
    └── netaddr/                 # RemoteAddr → netip.Addr parsing, IPv6 /64 key helper
```

Each public subpackage follows the same layout:

- `<pkg>.go` holds `New` and the handler.
- `options.go` holds `Option`, the `With*` functions, the config struct, its defaults and its
  validation.
- `<pkg>_test.go` holds the behavior tests, mapped to the acceptance scenarios and functional
  requirements.
- `example_test.go` holds the `Example` functions.
- `fuzz_test.go` and `bench_test.go` exist where they apply.

**Structure Decision**: the feature is one new top-level module, `middleware/`, with the layout
above.

- `middleware/go.mod` is the module's only `go.mod`.
- Each middleware has a dedicated folder (`middleware/cors`, `middleware/ratelimit`, …). Each
  folder is a plain package of the module, imported as
  `github.com/paccolamano/go/middleware/<name>`.
- No folder contains a `go.mod`, so the whole library is built, tested, versioned and released
  as one unit (`middleware/vX.Y.Z`).
- Internal packages stay private to the module, so they can change without affecting
  compatibility.

Nothing else in the repository changes except `go.work`, which `make work` regenerates.

## Complexity Tracking

No constitution violations to justify. The `//nolint` directives listed under Principle IV are
allowed by the constitution as long as each names its linter and gives a reason.

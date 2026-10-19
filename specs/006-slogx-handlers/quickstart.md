# Quickstart: Validating the slogx Module

This guide proves the feature works end to end. It only says how to check it. The expected
behavior is defined in:

- [contracts/public-api.md](contracts/public-api.md);
- [contracts/record-semantics.md](contracts/record-semantics.md).

## Prerequisites

- Go 1.26 or newer, and a C toolchain for `-race`.
- golangci-lint at the version pinned in `versions.env`.
- From the repository root, run `make work` once so that `go.work` lists `./slogx`. This is only
  for editor navigation; no gate uses it.

## 1. Local gates (constitution)

```bash
make check MODULE=slogx       # golangci-lint with zero findings, then go test -race -shuffle=on with coverage ≥ 80%
```

**Expected**: a `PASS` line for `slogx` from both `lint` and `test`, with no `.module-exceptions`
entry.

## 2. Structure, tidy and cross-build (the same checks CI runs)

```bash
scripts/structure-check.sh slogx    # README, CHANGELOG, no replace, go directive ≤ 1.26
(cd slogx && GOWORK=off go mod tidy -diff)
for os in linux darwin windows; do for arch in amd64 arm64; do
  (cd slogx && GOWORK=off GOOS=$os GOARCH=$arch go vet ./...) || echo "FAIL $os/$arch"
done; done
```

**Expected**: the structure check reports `PASS` for `slogx`. Tidy prints nothing, and there are
no `FAIL` lines. `slogx/go.mod` has no `require` list (SC-010).

## 3. Scenario checks per user story

Run these from `slogx/` with `GOWORK=off`. Test names follow the `Test<Feature>_<Scenario>`
convention fixed in `tasks.md`.

| Story | Command | What must hold |
|-------|---------|----------------|
| Conformance | `go test -race -run 'Conformance' ./...` | `slogtest` passes for `LevelHandler`, `ContextHandler` (fast and slow path), `RedactHandler`, `SamplingHandler`, `New` and `Recorder` (FR-003, SC-002). |
| US1: correlation | `go test -race -run 'Context\|With' .` | `tenant` and `request_id` are at the top level, also under `WithGroup("db")`. `With` called twice keeps both attributes, and the parent context keeps only its own. A key attached twice appears once, with the latest value. An explicit `request_id` on the record, or added with `logger.With`, wins over the context's. `logger.Info` without a context adds nothing. |
| US2: redaction | `go test -race -run 'Redact\|Secret' .` | `password`, mixed-case `Authorization` in `header`, `token` inside a `LogValuer` group, in `logger.With`, and in a context (through `New`) all show `[REDACTED]`. `password_hint` is untouched. `Secret` shows `[REDACTED]` through the JSON and text handlers, every `fmt` verb, `json.Marshal` of a struct (exported and unexported fields), and `Reveal` returns the value. |
| US3: level | `go test -race -run 'Level' .` | A debug record with `WithLevel(ctx, Debug)` is written through a JSON handler at Info. Without a level it is not. A nested `WithLevel(…, Warn)` silences info records. `logger.Debug` without a context is not written. |
| US4: recorder | `go test -race -run 'Recorder\|Record' ./slogxtest` | `Last()` gives `panic recovered` with `request_id` = `abc`. `Attr("query","ms")` returns 12 after `With("component","db").WithGroup("query")`. `WithLevel(Warn)` captures nothing at Info. 100 goroutines give `Len() == 100`. `Last()` and `Attr` on missing data return `false`. |
| US5: composition | `go test -race -run 'New\|Compose\|Option' .` | `New` over a JSON handler at Info writes a debug record from a request with level Debug, with `request_id` and `token=[REDACTED]`. The output equals the manual composition. Invalid or inapplicable options return errors that wrap `ErrInvalidConfig`. |
| US6: sampling | `go test -race -run 'Sampl\|Flush' .` | Under `synctest`, 10,000 identical Warn records (50 goroutines) give 109 written records, then one summary with `dropped=9891` and no `request_id`. Error records all pass by default and are sampled with `WithSampleErrors`. With 10 records or fewer there is no summary. 1,000,000 distinct messages never exceed 1,024 tracked groups. `Flush` emits the pending summary. |
| Robustness | `go test -race -run 'Panic\|Depth\|Nil' .` | A panicking `LogValue` gives `LogValue panicked…` and the record is written. A panicking extraction function gives `!CONTEXTFUNC`. A self-referencing `LogValuer` gives `!DEPTH`. A nil context does not panic (FR-005, SC-007). |

## 4. Allocations and performance

```bash
go test -run 'Alloc' .                                        # AllocsPerRun == 0 for disabled records (FR-004)
go test -run '^$' -bench 'Disabled|Enabled' -benchmem -count 5 .
```

**Expected**:

- `BenchmarkDisabled/*` reports `0 B/op 0 allocs/op` for every handler and for `New` (SC-003).
- In `BenchmarkEnabled`, `composed` takes at most twice the time of `bare-same-attrs`. The spike
  measured 1.6× (SC-008, research R-14).

## 5. Fuzzing (optional, longer)

```bash
go test -run '^$' -fuzz FuzzRedact -fuzztime 60s .
```

**Expected**: no failure. A sentinel value under a matching key, in any case and at any depth,
never appears in the output (SC-004).

## 6. Manual end-to-end check

Write a small program next to the module (outside the repository, or in a temporary
`example_test.go`):

- build `slogx.New(slog.NewJSONHandler(os.Stdout, nil), ...)` with
  `WithContextFunc(requestIDAttrs)`, `WithRedactKeys("authorization", "password", "token", "cookie")`
  and `WithSampling(10, 100, time.Second)`;
- serve one request through `middleware/requestid` and `middleware/accesslog`, using a logger
  built on that handler;
- inside the handler, call `logger.InfoContext(ctx, "order created")` from a function that
  receives only `ctx`.

**Expected**:

- both records carry `request_id`, once each. The accesslog record keeps its own value
  (FR-015).
- With `accesslog.WithHeaders("Authorization")`, the access record shows
  `"header":{"Authorization":"[REDACTED]"}`.
- Calling `h.Flush(context.Background())` before exit after a loop of 1,000 identical
  `WarnContext` calls prints one `records dropped by sampling` summary.

## 7. Documentation

```bash
go doc github.com/paccolamano/go/slogx
go doc github.com/paccolamano/go/slogx/slogxtest
```

**Expected**: the package documentation covers the context-only limitation and `sloglint`, the
composition order, the redaction coverage and its limits, the sampling rules with the summary
shape, and the placeholders (FR-009). Every exported identifier has a doc comment and an
`Example` where it is a primary API.

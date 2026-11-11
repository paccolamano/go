# Contract: Documentation and Examples of `middleware` 0.2.0

**Feature**: [spec.md](../spec.md) | **Plan**: [plan.md](../plan.md)

FR-018 to FR-020. The examples below ran in the spike with exactly the output shown
([research R-12](../research.md#r-12-documentation-fr-018-to-fr-020)).

## `doc.go`

**Overview paragraph**: the sentence about immutability names the three derivations:

```go
// ... Chains are immutable: [Chain.Append], [Chain.Use] and
// [Chain.AppendChain] return new chains, so a base chain can be shared and
// specialized for different route groups.
```

**Conventions section**: after the paragraph that ends "before any request is served", add:

```go
// [Chain.Use] takes the two results of a constructor directly and records the
// error, so building a chain takes a single check, with [Chain.Err], on the
// most derived chain:
//
//	base := middleware.New().
//		Use(requestid.New()).
//		Use(recovery.New(logger))
//	api := base.
//		Use(cors.New(origins)).
//		Use(ratelimit.New(100, time.Minute))
//	if err := api.Err(); err != nil { // covers base too
//		return err
//	}
//	mux.Handle("GET /healthz", base.Then(health))
//	mux.Handle("/api/", api.Then(apiHandler))
//
// [Chain.Then] panics on a chain with recorded errors, listing them, so a
// forgotten check fails at startup and shows its cause. [Chain.Append] takes
// middleware that are already built, such as those of other libraries.
```

The doc comments of `Chain`, `Use`, `AppendChain`, `Err` and `Then` are in
[public-api.md](public-api.md#doc-comments-of-the-new-and-changed-methods).

## `README.md`

**Usage** replaces the three `if err != nil` blocks:

```go
logger := slog.Default()

base := middleware.New().
	Use(requestid.New()).
	Use(accesslog.New(logger)).
	Use(recovery.New(logger))
api := base.
	Use(cors.New([]string{"https://app.example.com"})).
	Use(bodylimit.New(1 << 20))
if err := api.Err(); err != nil { // reports the errors of base too
	log.Fatal(err)
}

mux := http.NewServeMux()
mux.Handle("GET /healthz", base.Then(healthHandler))
mux.Handle("/api/", api.Then(apiHandler))
```

**The paragraph after it**:

> `New`, `Append`, `Use` and `AppendChain` build immutable chains: the first middleware is the
> outermost. Every constructor validates its configuration and returns an error matching
> `middleware.ErrInvalidConfig` when a setting is invalid. `Use` takes a constructor's two
> results and records the error, and `Err` reports the errors of a chain and of the chains it
> derives from, so one check covers them all. `Then` panics on a chain with recorded errors, so
> a forgotten check fails at startup.

## Examples (`example_test.go`)

New imports: `errors`, `middleware/bodylimit`, `middleware/cors`, `middleware/requestid`. The
helpers `tag`, `hello` and `serveOnce` already exist.

```go
func ExampleChain_Use() {
	base := middleware.New().
		Use(requestid.New()).
		Append(tag("logging"))
	api := base.
		Use(cors.New([]string{"https://app.example.com"})).
		Use(bodylimit.New(1 << 20))
	if err := api.Err(); err != nil { // covers base too
		fmt.Println(err)
		return
	}
	serveOnce(api.Then(hello()))
	// Output:
	// enter logging
	// handler
	// leave logging
}

func ExampleChain_Err() {
	base := middleware.New().Use(requestid.New())
	api := base.
		Use(cors.New([]string{"app.example.com"})). // the scheme is missing
		Use(bodylimit.New(0))
	fmt.Println(base.Err())
	fmt.Println(api.Err())
	fmt.Println(errors.Is(api.Err(), middleware.ErrInvalidConfig))
	// Output:
	// <nil>
	// middleware: invalid configuration: cors: origins: "app.example.com" is not an origin (scheme://host[:port])
	// middleware: invalid configuration: bodylimit: maxBytes: must be greater than 0
	// true
}

func ExampleChain_AppendChain() {
	common := middleware.New(tag("common"))
	api := middleware.New(tag("api"))
	serveOnce(common.AppendChain(api).Then(hello()))

	broken := api.Use(bodylimit.New(0))
	fmt.Println(common.AppendChain(broken).Err())
	// Output:
	// enter common
	// enter api
	// handler
	// leave api
	// leave common
	// middleware: invalid configuration: bodylimit: maxBytes: must be greater than 0
}
```

`ExampleChain_AppendChain` alone adds a constructor result, reads the errors and appends a
chain (FR-020).

## `example_recommended_test.go`

`recommendedChain` returns a `middleware.Chain` built with one `Use` per constructor, and its
`add` helper goes. Today the helper passes a nil middleware to `New` when a constructor fails,
so `New` panics with "nil middleware at position N" and the error is never returned. The spike
reproduced this with `bodylimit.New(0)`. `ExampleNew_recommended` checks `chain.Err()` once,
and its output is unchanged.

## Former name

After the change, `grep -rn Extend middleware/` finds only the 0.1.0 entry of
`middleware/CHANGELOG.md` and the new **Removed** entry (FR-020).

# problem

Produces and reads RFC 9457 problem details (`application/problem+json`) for HTTP APIs, with no
dependency outside the standard library.

```bash
go get github.com/paccolamano/go/problem
```

Minimum Go version: 1.26

## Declaring problem types

Declare each problem type and each extension member once. `Must` turns a refused declaration
into a panic when the program starts, so a test of the package catches it.

```go
var (
	OutOfCredit = problem.Must(problem.NewType(
		"https://example.com/probs/out-of-credit",
		"You do not have enough credit.",
		http.StatusForbidden,
	))
	Balance  = problem.Must(problem.NewKey[int]("balance"))
	Accounts = problem.Must(problem.NewKey[[]string]("accounts"))
)
```

Type URIs are absolute or begin with `/`. Extension names start with a letter, use only
letters, digits and `_`, and are three characters or longer.

## Answering requests

Return a problem from any layer, as an error:

```go
return OutOfCredit.New(
	problem.WithDetail("Your current balance is 30, but that costs 50."),
	problem.WithInstance("/account/12345/msgs/abc"),
	problem.WithExtension(Balance, 30),
	problem.WithExtension(Accounts, []string{"/account/12345", "/account/67890"}),
	problem.WithCause(err), // for logs only, never sent
)
```

Answer with whatever error the handler received:

```go
if err := svc.Send(r.Context(), msg); err != nil {
	logger.ErrorContext(r.Context(), "send message", "err", err)
	_ = problem.Write(w, r, err)
	return
}
```

The client receives:

```http
HTTP/1.1 403 Forbidden
Content-Type: application/problem+json

{"type":"https://example.com/probs/out-of-credit","title":"You do not have enough credit.","status":403,"detail":"Your current balance is 30, but that costs 50.","instance":"/account/12345/msgs/abc","accounts":["/account/12345","/account/67890"],"balance":30}
```

- `Write` finds the problem through any wrapping, and never sends the wrapping context or the
  internal cause.
- Any error that is not a problem is answered with the generic server error:
  `{"type":"about:blank","title":"Internal Server Error","status":500}`.
- When the status code says it all, use `problem.Status(http.StatusNotFound)`.

## Reading problem details

```go
resp, err := client.Do(req)
if err != nil {
	return err
}
defer resp.Body.Close()

if resp.StatusCode >= 400 {
	p, err := problem.Read(resp)
	switch {
	case errors.Is(err, problem.ErrNotProblem):
		return fmt.Errorf("upstream: %s", resp.Status)
	case err != nil:
		return fmt.Errorf("upstream: %w", err)
	}
	if _, ok := OutOfCredit.Match(p); ok {
		balance, _ := Balance.Lookup(p)
		// ...
	}
	return p
}
```

`Read` follows the RFC's rules for consumers: members with the wrong JSON type are ignored, a
missing type is `about:blank`, unknown extensions are kept, and relative URIs are resolved
against the request URL. It reads at most 1 MiB.

## Using it with the middleware module

The refusal functions of [middleware](../middleware) answer refused requests with problem
details in two lines. Neither module depends on the other.

```go
reject := func(w http.ResponseWriter, r *http.Request, rej middleware.Rejection) {
	_ = problem.Write(w, r, problem.Status(rej.Status))
}

rl, err := ratelimit.New(100, time.Minute, ratelimit.WithRejectFunc(reject))
```

A refused request then receives, for example,
`{"type":"about:blank","title":"Too Many Requests","status":429}`, with its `Retry-After` header.

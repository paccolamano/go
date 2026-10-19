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
	if werr := problem.Write(w, r, err); werr != nil {
		logger.ErrorContext(r.Context(), "write problem", "err", werr)
	}
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

### When `Write` returns an error

Never discard the error that `Write` returns: it is the only sign that something went wrong.

- An error that wraps `problem.ErrInvalid` means that the problem was built incorrectly, and
  that `Write` sent the generic server error instead. For example, an instance containing a
  space, `problem.WithInstance("/msgs/a b")`, turns the response into a 500. This is a bug to
  fix in how the problem is built.
- Any other error usually means that the client went away while the response was being sent.

A small helper keeps the two apart in the logs:

```go
func writeProblem(w http.ResponseWriter, r *http.Request, err error) {
	werr := problem.Write(w, r, err)
	switch {
	case errors.Is(werr, problem.ErrInvalid): // a bug: fix how the problem is built
		logger.ErrorContext(r.Context(), "problem replaced by a generic 500", "err", werr)
	case werr != nil: // usually the client went away
		logger.DebugContext(r.Context(), "write problem", "err", werr)
	}
}
```

## Adding members when writing

A problem is often built by a service layer that knows nothing about the HTTP request, while the
HTTP layer knows the request ID or the trace ID that the client should quote to support. `With`
derives a copy of a problem with more members, so `writeProblem` can add them before writing:

```go
var RequestID = problem.Must(problem.NewKey[string]("request_id"))

func writeProblem(w http.ResponseWriter, r *http.Request, err error) {
	if p, ok := errors.AsType[*problem.Error](err); ok {
		if id, ok := requestIDFrom(r.Context()); ok {
			err = p.With(problem.WithExtension(RequestID, id))
		}
	}
	werr := problem.Write(w, r, err)
	switch {
	case errors.Is(werr, problem.ErrInvalid): // a bug: fix how the problem is built
		logger.ErrorContext(r.Context(), "problem replaced by a generic 500", "err", werr)
	case werr != nil: // usually the client went away
		logger.DebugContext(r.Context(), "write problem", "err", werr)
	}
}
```

The original problem is not modified, so the service can share it between requests. The copy
keeps the internal cause, so `errors.Is`, `errors.As` and `Type.Match` find the same errors in it.

## Validation errors

RFC 9457 §3 reports several errors of the same type in one problem, with an `errors` extension
that lists a `detail` and a JSON Pointer (RFC 6901) to the offending member for each error:

```go
var ValidationError = problem.Must(problem.NewType(
	"https://example.net/validation-error",
	"Your request is not valid.",
	http.StatusUnprocessableEntity,
))

func validate(req CreateUser) error {
	var items []problem.Item
	if req.Age <= 0 {
		items = append(items, problem.Item{Detail: "must be a positive integer", Pointer: problem.Pointer("age")})
	}
	if !slices.Contains([]string{"green", "red", "blue"}, req.Profile.Color) {
		items = append(items, problem.Item{
			Detail:  "must be 'green', 'red' or 'blue'",
			Pointer: problem.Pointer("profile", "color"),
		})
	}
	if len(items) > 0 {
		return ValidationError.New(problem.WithErrors(items...))
	}
	return nil
}
```

The client receives:

```json
{"type":"https://example.net/validation-error","title":"Your request is not valid.","status":422,"errors":[{"detail":"must be a positive integer","pointer":"#/age"},{"detail":"must be 'green', 'red' or 'blue'","pointer":"#/profile/color"}]}
```

`Pointer` escapes the member names for you, as RFC 6901 requires: `Pointer("a/b")` is
`#/a~1b`, and `Pointer("città")` is `#/citt%C3%A0`. Pass array indexes as text, such as
`Pointer("items", "0", "qty")`.

`WithErrors` owns the `errors` member. If a key named `errors` is also used on the same problem,
the option applied last wins.

## Translating domain errors

The library does not map arbitrary errors to problems: which errors a client may learn about is
the application's decision. The recommended pattern is one function, next to the handlers, that
recognizes the domain errors the client can act on and returns a problem for each:

```go
var (
	ErrNotFound   = errors.New("store: not found")
	ErrEmailTaken = errors.New("store: email already taken")
)

type InsufficientFundsError struct {
	Balance, Price int
}

func (e *InsufficientFundsError) Error() string {
	return fmt.Sprintf("insufficient funds: balance %d, price %d", e.Balance, e.Price)
}

func toProblem(err error) error {
	if errors.Is(err, ErrNotFound) {
		return problem.Status(http.StatusNotFound,
			problem.WithDetail("The message does not exist."),
			problem.WithCause(err))
	}
	if errors.Is(err, ErrEmailTaken) {
		return problem.Status(http.StatusConflict,
			problem.WithDetail("An account with this email address already exists."),
			problem.WithCause(err))
	}
	if fe, ok := errors.AsType[*InsufficientFundsError](err); ok {
		return OutOfCredit.New(
			problem.WithDetail(fmt.Sprintf("Your current balance is %d, but that costs %d.", fe.Balance, fe.Price)),
			problem.WithExtension(Balance, fe.Balance),
			problem.WithCause(err))
	}
	return err
}
```

The handler then calls `writeProblem(w, r, toProblem(err))`. Errors that `toProblem` does not
recognize are returned unchanged, and `Write` answers them with the generic server error. The
detail and the extensions come from the domain error's fields, as with `InsufficientFundsError`,
and the domain error stays as the internal cause, for the logs.

**Never put `err.Error()` in `WithDetail`.** An error's message is written for developers, and
can reveal internals, such as the name of a database constraint:

```go
// Wrong: the client receives
// "pq: duplicate key value violates unique constraint \"users_email_key\"".
problem.Status(http.StatusConflict, problem.WithDetail(err.Error()))

// Right: describe the problem to the client, and keep the error for the logs.
problem.Status(http.StatusConflict,
	problem.WithDetail("An account with this email address already exists."),
	problem.WithCause(err))
```

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
against the request URL. It reads at most 1 MiB. `p.Errors()` returns the validation errors of
a problem, when it has some.

## Using it with the middleware module

The refusal functions of [middleware](../middleware) answer refused requests with problem
details in a few lines. Neither module depends on the other.

```go
reject := func(w http.ResponseWriter, r *http.Request, rej middleware.Rejection) {
	if err := problem.Write(w, r, problem.Status(rej.Status, problem.WithCause(rej.Err))); err != nil {
		logger.ErrorContext(r.Context(), "write problem", "err", err)
	}
}

rl, err := ratelimit.New(100, time.Minute, ratelimit.WithRejectFunc(reject))
```

A refused request then receives, for example,
`{"type":"about:blank","title":"Too Many Requests","status":429}`, with its `Retry-After` header.
The refusal's error is the internal cause of the problem: it is there for the logs, and never
reaches the client.

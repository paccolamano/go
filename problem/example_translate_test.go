package problem_test

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"

	"github.com/paccolamano/go/problem"
)

// OutOfCredit is the problem type of a payment refused for lack of credit.
var OutOfCredit = problem.Must(problem.NewType(
	"https://example.com/probs/out-of-credit",
	"You do not have enough credit.",
	http.StatusForbidden,
))

// Balance is the extension member that carries the current balance.
var Balance = problem.Must(problem.NewKey[int]("balance"))

// Domain errors, returned by the service and storage layers.
var (
	ErrNotFound   = errors.New("store: not found")
	ErrEmailTaken = errors.New("store: email already taken")
)

// InsufficientFundsError reports a payment larger than the balance.
type InsufficientFundsError struct {
	Balance, Price int
}

func (e *InsufficientFundsError) Error() string {
	return fmt.Sprintf("insufficient funds: balance %d, price %d", e.Balance, e.Price)
}

// toProblem translates the domain errors that the client can act on into
// problems. The detail and the extensions come from the error's fields, never
// from its message, and the error stays as the internal cause, for the logs.
// Every other error is returned unchanged: Write answers it with the generic
// server error.
func toProblem(err error) error {
	if errors.Is(err, ErrNotFound) {
		return problem.Status(http.StatusNotFound,
			problem.WithDetail("The message does not exist."),
			problem.WithCause(err))
	}
	if errors.Is(err, ErrEmailTaken) {
		// Never problem.WithDetail(err.Error()): the message comes from the
		// database, and would send the client the name of a unique constraint.
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

func Example_translateDomainErrors() {
	errs := []error{
		fmt.Errorf("get message %q: %w", "abc", ErrNotFound),
		fmt.Errorf("send message: %w", &InsufficientFundsError{Balance: 30, Price: 50}),
		fmt.Errorf("create user: %w: %w", ErrEmailTaken,
			errors.New(`pq: duplicate key value violates unique constraint "users_email_key"`)),
		errors.New("pq: connection refused"),
	}

	var all strings.Builder
	for _, err := range errs {
		rec := httptest.NewRecorder()
		req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/msgs", nil)
		if werr := problem.Write(rec, req, toProblem(err)); werr != nil {
			slog.ErrorContext(req.Context(), "write problem", "err", werr)
		}
		fmt.Println(rec.Code)
		fmt.Println(rec.Body)
		all.WriteString(rec.Body.String())
	}
	body := all.String()
	fmt.Println("leaked:", strings.Contains(body, "users_email_key") || strings.Contains(body, "pq:") ||
		strings.Contains(body, "insufficient funds"))
	// Output:
	// 404
	// {"type":"about:blank","title":"Not Found","status":404,"detail":"The message does not exist."}
	// 403
	// {"type":"https://example.com/probs/out-of-credit","title":"You do not have enough credit.","status":403,"detail":"Your current balance is 30, but that costs 50.","balance":30}
	// 409
	// {"type":"about:blank","title":"Conflict","status":409,"detail":"An account with this email address already exists."}
	// 500
	// {"type":"about:blank","title":"Internal Server Error","status":500}
	// leaked: false
}

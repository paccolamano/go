package problem_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"

	"github.com/paccolamano/go/problem"
)

func ExampleRead() {
	outOfCredit := problem.Must(problem.NewType(
		"https://api.example.com/probs/out-of-credit",
		"You do not have enough credit.",
		http.StatusForbidden,
	))
	balance := problem.Must(problem.NewKey[int]("balance"))

	// A response from another API, as http.Client returns it.
	resp := &http.Response{
		StatusCode: http.StatusForbidden,
		Header:     http.Header{"Content-Type": {"application/problem+json"}},
		Body: io.NopCloser(strings.NewReader(
			`{"type":"/probs/out-of-credit","title":"You do not have enough credit.","balance":30}`)),
		Request: httptest.NewRequestWithContext(context.Background(), http.MethodPost,
			"https://api.example.com/v1/messages", nil),
	}
	defer func() { _ = resp.Body.Close() }() // Read does not close the body

	p, err := problem.Read(resp)
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(p.Type()) // resolved against the request URL
	if _, ok := outOfCredit.Match(p); ok {
		v, _ := balance.Lookup(p)
		fmt.Println("out of credit, balance", v)
	}
	// Output:
	// https://api.example.com/probs/out-of-credit
	// out of credit, balance 30
}

func ExampleParse() {
	p, err := problem.Parse([]byte(`{"title":"Not Found","status":"404","detail":"No such message."}`))
	if err != nil {
		fmt.Println(err)
		return
	}
	// A member with the wrong JSON type is ignored, and a missing type is
	// "about:blank" (RFC 9457 §3.1).
	fmt.Println(p.Type(), p.Status(), p.Title()+":", p.Detail())
	// Output:
	// about:blank 0 Not Found: No such message.
}

func ExampleWithCause() {
	unavailable := problem.Must(problem.NewType(
		"https://example.com/probs/service-unavailable",
		"The service is unavailable.",
		http.StatusServiceUnavailable,
	))
	e := unavailable.New(problem.WithCause(io.ErrUnexpectedEOF))

	// The cause is in the error tree and in the message, for logs...
	fmt.Println(errors.Is(e, io.ErrUnexpectedEOF))
	fmt.Println(e)

	// ...but never in the response.
	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/", nil)
	if err := problem.Write(rec, req, e); err != nil {
		slog.ErrorContext(req.Context(), "write problem", "err", err)
	}
	fmt.Println(rec.Body)
	// Output:
	// true
	// The service is unavailable.: unexpected EOF
	// {"type":"https://example.com/probs/service-unavailable","title":"The service is unavailable.","status":503}
}

func ExampleType_Match() {
	outOfCredit := problem.Must(problem.NewType(
		"https://example.com/probs/out-of-credit",
		"You do not have enough credit.",
		http.StatusForbidden,
	))
	err := fmt.Errorf("send message: %w", outOfCredit.New(problem.WithDetail("Balance 30, cost 50.")))

	if p, ok := outOfCredit.Match(err); ok {
		fmt.Println("out of credit:", p.Detail())
	}
	// Output:
	// out of credit: Balance 30, cost 50.
}

func ExampleWrite_error() {
	// An error that is not a problem never reaches the client.
	err := errors.New(`pq: password authentication failed for user "admin"`)

	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/", nil)
	if werr := problem.Write(rec, req, err); werr != nil {
		slog.ErrorContext(req.Context(), "write problem", "err", werr)
	}
	fmt.Println(rec.Code)
	fmt.Println(rec.Body)
	// Output:
	// 500
	// {"type":"about:blank","title":"Internal Server Error","status":500}
}

func ExampleStatus() {
	e := problem.Status(http.StatusNotFound)
	fmt.Println(e.Type())
	fmt.Println(e.Title())
	fmt.Println(e.Status())
	// Output:
	// about:blank
	// Not Found
	// 404
}

func ExampleNewType() {
	outOfCredit, err := problem.NewType(
		"https://example.com/probs/out-of-credit",
		"You do not have enough credit.",
		http.StatusForbidden,
	)
	if err != nil {
		fmt.Println(err)
		return
	}
	e := outOfCredit.New()
	fmt.Println(e.Type(), e.Status())
	// Output:
	// https://example.com/probs/out-of-credit 403
}

func ExampleNewType_refused() {
	// A relative type URI must include the full path (RFC 9457 §3.1.1).
	_, err := problem.NewType("out-of-credit", "You do not have enough credit.", http.StatusForbidden)
	fmt.Println(errors.Is(err, problem.ErrInvalid))
	fmt.Println(err)
	// Output:
	// true
	// problem: invalid: type URI "out-of-credit" is neither an absolute URI nor a reference beginning with "/"
}

func ExampleWithInstance() {
	e := problem.Status(http.StatusConflict, problem.WithInstance("/account/12345/msgs/abc"))
	fmt.Println(e.Instance())

	// An instance that is not a URI reference is reported by Write, which
	// sends the generic server error instead.
	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/", nil)
	err := problem.Write(rec, req, problem.Status(http.StatusConflict, problem.WithInstance("/msgs/a b")))
	fmt.Println(rec.Code, err)
	// Output:
	// /account/12345/msgs/abc
	// 500 problem: invalid: instance "/msgs/a b" is not a URI reference
}

func ExampleMust() {
	// Must is meant for package-level declarations, whose arguments are fixed
	// in the source code.
	outOfCredit := problem.Must(problem.NewType(
		"https://example.com/probs/out-of-credit",
		"You do not have enough credit.",
		http.StatusForbidden,
	))
	balance := problem.Must(problem.NewKey[int]("balance"))

	e := outOfCredit.New(problem.WithExtension(balance, 30))
	fmt.Println(e.Title())
	// Output:
	// You do not have enough credit.
}

func ExampleNewKey() {
	balance, err := problem.NewKey[int]("balance")
	if err != nil {
		fmt.Println(err)
		return
	}
	e := problem.Status(http.StatusForbidden, problem.WithExtension(balance, 30))
	fmt.Println(string(e.Extensions()["balance"]))
	// Output:
	// 30
}

func ExampleType_New() {
	outOfCredit := problem.Must(problem.NewType(
		"https://example.com/probs/out-of-credit",
		"You do not have enough credit.",
		http.StatusForbidden,
	))
	e := outOfCredit.New(
		problem.WithDetail("Your current balance is 30, but that costs 50."),
		problem.WithInstance("/account/12345/msgs/abc"),
	)
	fmt.Println(e)
	// Output:
	// You do not have enough credit.: Your current balance is 30, but that costs 50.
}

func ExampleWithDetail() {
	e := problem.Status(http.StatusConflict, problem.WithDetail("The message was already sent."))
	fmt.Println(e.Detail())
	// Output:
	// The message was already sent.
}

func ExampleWithTitle() {
	// Replace the title only to localize it.
	e := problem.Status(http.StatusNotFound, problem.WithTitle("Risorsa non trovata"))
	fmt.Println(e.Title())
	// Output:
	// Risorsa non trovata
}

func ExampleWithExtension() {
	accounts := problem.Must(problem.NewKey[[]string]("accounts"))
	e := problem.Status(http.StatusForbidden,
		problem.WithExtension(accounts, []string{"/account/12345", "/account/67890"}))
	fmt.Println(string(e.Extensions()["accounts"]))
	// Output:
	// ["/account/12345","/account/67890"]
}

func ExampleKey_Lookup() {
	balance := problem.Must(problem.NewKey[int]("balance"))
	e := problem.Status(http.StatusForbidden, problem.WithExtension(balance, 30))

	if v, ok := balance.Lookup(e); ok {
		fmt.Println("balance:", v)
	}
	// Output:
	// balance: 30
}

func ExampleWrite() {
	outOfCredit := problem.Must(problem.NewType(
		"https://example.com/probs/out-of-credit",
		"You do not have enough credit.",
		http.StatusForbidden,
	))
	send := func() error {
		return outOfCredit.New(problem.WithDetail("Your current balance is 30, but that costs 50."))
	}
	handler := func(w http.ResponseWriter, r *http.Request) {
		if err := send(); err != nil {
			if werr := problem.Write(w, r, fmt.Errorf("send message: %w", err)); werr != nil {
				slog.ErrorContext(r.Context(), "write problem", "err", werr)
			}
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}

	rec := httptest.NewRecorder()
	handler(rec, httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/msgs", nil))
	fmt.Println(rec.Code, rec.Header().Get("Content-Type"))
	fmt.Println(rec.Body)
	// Output:
	// 403 application/problem+json
	// {"type":"https://example.com/probs/out-of-credit","title":"You do not have enough credit.","status":403,"detail":"Your current balance is 30, but that costs 50."}
}

func ExamplePointer() {
	fmt.Println(problem.Pointer("items", "0", "qty"))
	fmt.Println(problem.Pointer())
	fmt.Println(problem.Pointer("a/b", "m~n"))
	fmt.Println(problem.Pointer("città"))
	// Output:
	// #/items/0/qty
	// #
	// #/a~1b/m~0n
	// #/citt%C3%A0
}

func ExampleError_Errors() {
	// A validation problem returned by another API.
	p, err := problem.Parse([]byte(`{"type":"https://example.net/validation-error","title":"Your request is not valid.",` +
		`"errors":[{"detail":"must be a positive integer","pointer":"#/age"},` +
		`{"detail":"must be 'green', 'red' or 'blue'","pointer":"#/profile/color"}]}`))
	if err != nil {
		fmt.Println(err)
		return
	}
	items, _ := p.Errors()
	for _, item := range items {
		fmt.Printf("%s: %s\n", item.Pointer, item.Detail)
	}
	// Output:
	// #/age: must be a positive integer
	// #/profile/color: must be 'green', 'red' or 'blue'
}

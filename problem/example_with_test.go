package problem_test

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"

	"github.com/paccolamano/go/problem"
)

// RequestID is the extension member that carries the ID of the request.
var RequestID = problem.Must(problem.NewKey[string]("request_id"))

type requestIDKey struct{}

// withRequestID returns ctx with the request ID id, as a request ID middleware
// would set it.
func withRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, requestIDKey{}, id)
}

// requestIDFrom returns the request ID stored in ctx.
func requestIDFrom(ctx context.Context) (string, bool) {
	id, ok := ctx.Value(requestIDKey{}).(string)
	return id, ok
}

// writeProblem answers r with err. When err contains a problem, the answer
// carries the request ID too, although the layer that built the problem knew
// nothing about the request.
func writeProblem(w http.ResponseWriter, r *http.Request, err error) {
	if p, ok := errors.AsType[*problem.Error](err); ok {
		if id, ok := requestIDFrom(r.Context()); ok {
			err = p.With(problem.WithExtension(RequestID, id))
		}
	}
	if werr := problem.Write(w, r, err); werr != nil {
		slog.ErrorContext(r.Context(), "write problem", "err", werr)
	}
}

func ExampleError_With() {
	// The service layer returns a problem, wrapped with some context.
	err := fmt.Errorf("send message: %w",
		problem.Status(http.StatusConflict, problem.WithDetail("The message was already sent.")))

	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(withRequestID(context.Background(), "req-42"),
		http.MethodPost, "/msgs", nil)
	writeProblem(rec, req, err)
	fmt.Println(rec.Body)
	// Output:
	// {"type":"about:blank","title":"Conflict","status":409,"detail":"The message was already sent.","request_id":"req-42"}
}

package health_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"

	"github.com/paccolamano/go/lifecycle/health"
)

// A failed check is listed by name; its error goes to the logger, never to the
// response.
func ExampleNew() {
	h, err := health.New([]health.Check{
		{Name: "postgres", Func: func(context.Context) error { return nil }},
		{Name: "redis", Func: func(context.Context) error {
			return errors.New("dial tcp cache.internal:6379: connection refused")
		}},
	})
	if err != nil {
		fmt.Println(err)
		return
	}

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/readyz", nil))
	fmt.Println(rec.Code)
	fmt.Print(rec.Body.String())
	// Output:
	// 503
	// [+]postgres ok
	// [-]redis failed: reason withheld
	// failed
}

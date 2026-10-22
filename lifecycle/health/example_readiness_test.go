package health_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"runtime"

	"github.com/paccolamano/go/lifecycle"
	"github.com/paccolamano/go/lifecycle/health"
)

// probe calls h as an orchestrator's probe would, and returns the status.
func probe(h http.Handler) int {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/", nil))
	return rec.Code
}

// The readiness of an App is one more check of a readiness handler. Liveness has
// no check: the process answers, so it is alive.
func Example_readiness() {
	app, err := lifecycle.New()
	if err != nil {
		fmt.Println(err)
		return
	}
	app.Add("worker", lifecycle.Func(func(ctx context.Context) error {
		<-ctx.Done()
		return nil
	}))

	readiness, err := health.New([]health.Check{
		{Name: "lifecycle", Func: app.Ready},
		// {Name: "postgres", Func: db.PingContext}, and the other dependencies
	})
	if err != nil {
		fmt.Println(err)
		return
	}
	liveness, err := health.New(nil)
	if err != nil {
		fmt.Println(err)
		return
	}

	fmt.Println("readiness before Run:", probe(readiness))

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- app.Run(ctx) }()
	for app.Ready(ctx) != nil { // an orchestrator polls the probe instead
		runtime.Gosched()
	}
	fmt.Println("readiness while running:", probe(readiness))
	fmt.Println("liveness while running:", probe(liveness))

	cancel()
	<-done
	fmt.Println("readiness after Run:", probe(readiness))
	fmt.Println("liveness after Run:", probe(liveness))
	// Output:
	// readiness before Run: 503
	// readiness while running: 200
	// liveness while running: 200
	// readiness after Run: 503
	// liveness after Run: 200
}

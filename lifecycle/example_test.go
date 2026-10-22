package lifecycle_test

import (
	"context"
	"fmt"

	"github.com/paccolamano/go/lifecycle"
)

// An App stops its components in the reverse order of their registration. Here
// the stop is triggered by cancelling the context passed to Run; a service would
// also use WithSignals.
func Example() {
	app, err := lifecycle.New()
	if err != nil {
		fmt.Println(err)
		return
	}
	for _, name := range []string{"db", "worker"} {
		app.Add(name, lifecycle.Func(func(ctx context.Context) error {
			<-ctx.Done()
			fmt.Println(name, "stopped")
			return nil
		}))
	}
	ready := make(chan struct{})
	app.Add("ready", lifecycle.Func(func(ctx context.Context) error {
		close(ready)
		<-ctx.Done()
		fmt.Println("ready stopped")
		return nil
	}))

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- app.Run(ctx) }()
	<-ready
	cancel()
	fmt.Println("run:", <-done)
	// Output:
	// ready stopped
	// worker stopped
	// db stopped
	// run: <nil>
}

// Closer accepts the three shapes of close methods: no result, an error, or a
// context and an error. Resources registered first are closed last.
func ExampleCloser() {
	app, err := lifecycle.New()
	if err != nil {
		fmt.Println(err)
		return
	}
	app.Add("logs", lifecycle.Closer(func(context.Context) error { // like slogx's Handler.Flush
		fmt.Println("logs flushed")
		return nil
	}))
	app.Add("db", lifecycle.Closer(func() error { // like sql.DB.Close
		fmt.Println("db closed")
		return nil
	}))
	app.Add("cache", lifecycle.Closer(func() { // like pgxpool.Pool.Close
		fmt.Println("cache closed")
	}))

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // stop at once: every component is launched, then stopped
	fmt.Println("run:", app.Run(ctx))
	// Output:
	// cache closed
	// db closed
	// logs flushed
	// run: <nil>
}

// StartStop fits services with separate start and stop operations, such as a
// job queue client or a scheduler.
func ExampleStartStop() {
	app, err := lifecycle.New()
	if err != nil {
		fmt.Println(err)
		return
	}
	app.Add("jobs", lifecycle.StartStop(
		func(context.Context) error {
			fmt.Println("started")
			return nil
		},
		func(context.Context) error {
			fmt.Println("stopped")
			return nil
		},
	))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	fmt.Println("run:", app.Run(ctx))
	// Output:
	// started
	// stopped
	// run: <nil>
}

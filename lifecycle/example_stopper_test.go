package lifecycle_test

import (
	"context"
	"fmt"

	"github.com/paccolamano/go/lifecycle"
)

// consumer processes the messages a broker has delivered to it. deliveries
// stands for the channel of a message broker client, which the broker closes
// once the subscription is cancelled, after the messages already in flight.
type consumer struct {
	deliveries chan int
	running    chan struct{}
}

// Run processes every delivery until the channel is closed, so that no message
// already received is lost. Its context is cancelled only if the deadline of the
// stop sequence expires first.
func (c *consumer) Run(ctx context.Context) error {
	close(c.running)
	for {
		select {
		case n, ok := <-c.deliveries:
			if !ok {
				return nil
			}
			fmt.Println("processed", n)
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// Stop stops intake: here it closes the channel, as a broker does after a
// cancellation. It returns at once; the App then waits for Run.
func (c *consumer) Stop(context.Context) error {
	close(c.deliveries)
	return nil
}

// A component that implements Stopper stops accepting work when its turn comes,
// and finishes what it has already received with a valid context.
func ExampleStopper() {
	c := &consumer{deliveries: make(chan int, 3), running: make(chan struct{})}
	for n := 1; n <= 3; n++ {
		c.deliveries <- n
	}

	app, err := lifecycle.New()
	if err != nil {
		fmt.Println(err)
		return
	}
	app.Add("orders", c)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- app.Run(ctx) }()
	<-c.running
	cancel()
	fmt.Println("run:", <-done)
	// Output:
	// processed 1
	// processed 2
	// processed 3
	// run: <nil>
}

// Package lifecycle runs the components of a service process, such as servers,
// workers, consumers and the clients of databases, from start to graceful
// shutdown: launched in registration order, stopped one at a time in reverse
// order, with a drain period and a bounded deadline.
//
// # The model
//
// A Component has a Run method that blocks while it works. Register components
// with App.Add in the order you open or start them, then call App.Run: the App
// launches them in that order and, when the stop is triggered, stops them in the
// reverse order. Resources are opened by your own code; the App only needs the
// way to close them, through Closer. A component whose Run returns before it was
// asked to stop stops the whole process, and Run reports it: nothing is
// restarted.
//
// The stop is triggered by the end of the context passed to Run, by a signal
// configured with WithSignals, or by a component that returns early.
//
// # The stop contract
//
// When a component's turn comes:
//
//   - a component that implements Stopper gets Stop, which means "stop accepting
//     new work". The App waits for Stop and Run to return, and its context stays
//     valid, so the work in progress completes. The context is cancelled only if
//     the deadline expires first;
//   - any other component gets the cancellation of its context, and the App waits
//     for Run.
//
// The deadline is the end of the drain delay (WithDrainDelay) plus the stop
// timeout (WithStopTimeout). When it expires, the remaining components still get
// their stop, without waiting, and Run returns. A second signal forces the stop at
// once.
//
// HTTPServer, Closer and StartStop adapt the common shapes: an http.Server, a
// Close or Shutdown method, and services with separate start and stop
// operations. Ready reports whether the App is running, in the shape of a check
// of package health.
//
// See Example, ExampleStopper, ExampleHTTPServer, ExampleCloser and
// ExampleStartStop.
package lifecycle

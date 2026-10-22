// Package health answers readiness and liveness probes by running named checks.
// It never writes why a check failed; the logger receives the reason. It does not
// depend on package lifecycle: an App's readiness is passed to it as one more
// check.
//
// Readiness and liveness use the same Handler with different checks. A readiness
// handler checks the App's state and the dependencies the process needs, so that
// a failing one removes the process from load balancing without restarting it. A
// liveness handler has no checks, or only checks of the process itself: a
// dependency checked by liveness would restart every replica while it is down.
// See ExampleNew and Example_readiness.
package health

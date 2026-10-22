# Changelog

All notable changes to this module are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this module adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- `App`, `New`, `Add` and `Run`, which launch components in registration order and stop them
  one at a time in reverse order when the context ends, a configured signal arrives, or a
  component returns before it was asked to stop.
- `Component`, the optional `Stopper` and `Func`. The stop contract: a `Stopper` stops accepting
  work and keeps a valid context until the deadline; other components get the cancellation of
  their context at their turn.
- `WithDrainDelay`, during which `Ready` fails while every component keeps working;
  `WithStopTimeout`, which bounds the stop sequence; `WithSignals`, opt-in, where a second signal
  forces the stop; `WithLogger`.
- `Ready`, the App's readiness in the shape of a health check.
- The adapters `HTTPServer` (with `WithListener` and TLS from the server's configuration),
  `Closer` (for close functions with no result, an error, or a context and an error) and
  `StartStop`.
- The errors `ErrInvalidConfig`, `ErrAlreadyStarted`, `ErrExitedEarly`, `ErrStopTimeout`,
  `ErrForced` and `ErrNotRunning`, and the types `ComponentError` and `PanicError`. Panics in
  `Run` and `Stop` are recovered.
- Package `health`: a probe handler built from named checks, with per-check timeouts, evaluations
  shared by concurrent requests, Kubernetes-style text responses that withhold failure reasons,
  and `WithLogger` for the reasons.

# Contract: Public Go API

**Module**: `github.com/paccolamano/go/lifecycle` | **Packages**: `lifecycle`,
`lifecycle/health` | **Minimum Go**: 1.26

This file lists the complete exported surface of the module at its first release. Anything not
listed here is unexported. Later releases may only **add** identifiers, options or fields
(Principle II). The exact doc-comment wording is decided at implementation time. Behavior is
specified in [lifecycle-semantics.md](lifecycle-semantics.md) and
[health-http.md](health-http.md).

## Package `lifecycle`

### Components (FR-008, FR-009, research R-03)

```go
// Component is a part of the process that runs until it is stopped. Run blocks
// while the component works and returns when it has stopped. Its context carries
// the values of the context passed to App.Run, and is cancelled only when the
// App's stop sequence says so.
type Component interface {
	Run(ctx context.Context) error
}

// Stopper is implemented by components that can stop accepting new work on
// request. Stop receives a context whose deadline is the end of the stop
// sequence. It may return at once or block until the component has stopped.
type Stopper interface {
	Stop(ctx context.Context) error
}

// Func adapts a function to a Component, like http.HandlerFunc.
type Func func(ctx context.Context) error

func (f Func) Run(ctx context.Context) error
```

### App (FR-010 to FR-029)

```go
// App runs components, launched in registration order and stopped in reverse
// order. An App runs once. It is safe for concurrent use.
type App struct{ /* unexported */ }

// New returns an App configured by opts. An invalid option is reported as an error
// wrapping ErrInvalidConfig.
func New(opts ...Option) (*App, error)

// Add registers c under name. Invalid registrations are reported by Run, before
// anything is launched. A registration made after Run has started is refused and
// reported when Run returns.
func (a *App) Add(name string, c Component)

// Run launches every component and blocks until they have been stopped, or until
// the deadline of the stop sequence has expired. It returns nil when the stop was
// triggered by ctx or by a signal and every component stopped cleanly.
func (a *App) Run(ctx context.Context) error

// Ready returns nil while the App is running, and otherwise an error wrapping
// ErrNotRunning that names the state. It has the shape of a health check.
func (a *App) Ready(ctx context.Context) error
```

### Options (FR-003, FR-016, FR-017, FR-023, FR-028, research R-05)

```go
// Option configures an App.
type Option func(*config)

// WithSignals makes Run handle sig while it runs: the first one received triggers
// the stop, the second forces it. By default the App handles no signal.
func WithSignals(sig ...os.Signal) Option

// WithDrainDelay sets the time between the trigger and the first stop, during
// which Ready fails and every component keeps working. Default: 0.
func WithDrainDelay(d time.Duration) Option

// WithStopTimeout sets the deadline for stopping every component, counted from
// the end of the drain. Default: 15 seconds.
func WithStopTimeout(d time.Duration) Option

// WithLogger sets the logger of the App's lifecycle events. Default: none.
func WithLogger(l *slog.Logger) Option
```

### Adapters (FR-030 to FR-034, research R-11 to R-13)

```go
// HTTPServer returns a component that serves srv, on its address or on the
// listener given with WithListener, with TLS when srv.TLSConfig provides a
// certificate. Its stop shuts srv down gracefully, and closes the remaining
// connections when the deadline expires.
func HTTPServer(srv *http.Server, opts ...HTTPOption) Component

// HTTPOption configures HTTPServer.
type HTTPOption func(*httpConfig)

// WithListener makes the server serve on ln instead of its address. The listener
// is closed when the server stops.
func WithListener(ln net.Listener) HTTPOption

// Closer returns a component whose stop calls f once: a function with no result,
// with an error, or with a context and an error, such as the Close or Shutdown
// method of a client.
func Closer[F func() | func() error | func(context.Context) error](f F) Component

// StartStop returns a component that calls start when it is launched and stop
// when its turn comes. A start error is a failure of the component.
func StartStop(start, stop func(context.Context) error) Component
```

### Errors (FR-026, FR-027, FR-029, research R-04)

```go
var (
	ErrInvalidConfig  = errors.New("lifecycle: invalid configuration")
	ErrAlreadyStarted = errors.New("lifecycle: already started")
	ErrExitedEarly    = errors.New("lifecycle: exited before being stopped")
	ErrStopTimeout    = errors.New("lifecycle: not stopped in time")
	ErrForced         = errors.New("lifecycle: stop forced by a second signal")
	ErrNotRunning     = errors.New("lifecycle: not running")
)

// ComponentError reports an outcome of the named component: a failure, an early
// exit, a stop error, a timeout or a refused registration.
type ComponentError struct {
	Name string
	Err  error
}

func (e *ComponentError) Error() string // lifecycle: component "<name>": <err, without a leading "lifecycle: ">

func (e *ComponentError) Unwrap() error

// PanicError reports a panic recovered from a component's Run or Stop.
type PanicError struct {
	Value any
	Stack []byte
}

func (e *PanicError) Error() string // panic: <value>
```

## Package `lifecycle/health`

### Handler (FR-035 to FR-044, research R-16 to R-18)

```go
// Check is a named operation that tells whether something the process needs is
// available. Timeout bounds each run of Func; zero means 1 second.
type Check struct {
	Name    string
	Func    func(ctx context.Context) error
	Timeout time.Duration
}

// Handler answers probes by running its checks. Requests that arrive while the
// checks run share their result. It is safe for concurrent use.
type Handler struct{ /* unexported */ }

// New returns a Handler that runs checks, in the order given in responses. An
// invalid check or option is reported as an error wrapping ErrInvalidConfig.
func New(checks []Check, opts ...Option) (*Handler, error)

// ServeHTTP answers 200 when every check passes and 503 otherwise. It never
// writes the reason of a failure; the logger receives it.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request)

// Option configures a Handler.
type Option func(*config)

// WithLogger sets the logger that receives the reasons of failed checks.
// Default: none.
func WithLogger(l *slog.Logger) Option

var ErrInvalidConfig = errors.New("health: invalid configuration")
```

## Traceability

| Identifier | Requirements |
|------------|--------------|
| `Component`, `Func` | FR-008, FR-009, FR-021 |
| `Stopper` | FR-008, FR-020 |
| `App`, `New`, `Add` | FR-004, FR-010, FR-012 |
| `Run` | FR-011 to FR-028 |
| `Ready` | FR-017, FR-029, FR-038 |
| `Option`, `WithSignals` | FR-003, FR-006, FR-015, FR-016, FR-025 |
| `WithDrainDelay` | FR-003, FR-017 |
| `WithStopTimeout` | FR-003, FR-023, FR-024 |
| `WithLogger` | FR-003, FR-028 |
| `HTTPServer`, `HTTPOption`, `WithListener` | FR-030, FR-031, FR-034 |
| `Closer` | FR-032, FR-034 |
| `StartStop` | FR-033, FR-034 |
| `ErrInvalidConfig` | FR-003, FR-010 |
| `ErrAlreadyStarted` | FR-011 |
| `ErrExitedEarly` | FR-026 |
| `ErrStopTimeout` | FR-024, FR-026 |
| `ErrForced` | FR-025, FR-026 |
| `ErrNotRunning` | FR-029 |
| `ComponentError` | FR-022, FR-026 |
| `PanicError` | FR-005, FR-027 |
| `health.Check`, `health.New` | FR-035, FR-036, FR-037 |
| `health.Handler`, `ServeHTTP` | FR-004, FR-038 to FR-043 |
| `health.Option`, `health.WithLogger` | FR-044 |
| `health.ErrInvalidConfig` | FR-036 |

The module exports 37 identifiers.

- `lifecycle`: 8 types (`Component`, `Stopper`, `Func`, `App`, `Option`, `HTTPOption`,
  `ComponentError`, `PanicError`), 9 functions (`New`,
  `WithSignals`, `WithDrainDelay`, `WithStopTimeout`, `WithLogger`, `HTTPServer`,
  `WithListener`, `Closer`, `StartStop`), 7 methods (`Func.Run`, `App.Add`, `App.Run`,
  `App.Ready`, `ComponentError.Error`, `ComponentError.Unwrap`, `PanicError.Error`) and 6 error
  variables.
- `health`: 3 types (`Check`, `Handler`, `Option`), 2 functions (`New`, `WithLogger`), 1 method
  (`ServeHTTP`) and 1 error variable.

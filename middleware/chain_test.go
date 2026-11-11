package middleware_test

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/paccolamano/go/middleware"
	"github.com/paccolamano/go/middleware/bodylimit"
	"github.com/paccolamano/go/middleware/cors"
	"github.com/paccolamano/go/middleware/requestid"
)

// recorder returns a middleware that appends "<name>>" before and "<<name"
// after calling the next handler.
func recorder(name string, log *[]string) middleware.Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			*log = append(*log, name+">")
			next.ServeHTTP(w, r)
			*log = append(*log, "<"+name)
		})
	}
}

func handler(log *[]string) http.Handler {
	return http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		*log = append(*log, "H")
	})
}

func serve(t *testing.T, h http.Handler) {
	t.Helper()
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil))
}

// recovered calls f and returns the value it panicked with, or nil.
func recovered(f func()) (v any) {
	defer func() { v = recover() }()
	f()
	return nil
}

func TestChain_Order(t *testing.T) {
	var log []string
	h := middleware.New(recorder("A", &log), recorder("B", &log), recorder("C", &log)).Then(handler(&log))
	serve(t, h)
	want := []string{"A>", "B>", "C>", "H", "<C", "<B", "<A"}
	if !slices.Equal(log, want) {
		t.Errorf("order = %v, want %v", log, want)
	}
}

func TestChain_Empty(t *testing.T) {
	h := http.NotFoundHandler()
	for name, c := range map[string]middleware.Chain{"New()": middleware.New(), "zero value": {}} {
		t.Run(name, func(t *testing.T) {
			got := c.Then(h)
			if got == nil {
				t.Fatal("Then returned nil")
			}
			rec := httptest.NewRecorder()
			got.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil))
			if rec.Code != http.StatusNotFound {
				t.Errorf("status = %d, want the final handler's 404", rec.Code)
			}
		})
	}
}

func TestChain_AppendDoesNotAlias(t *testing.T) {
	var log []string
	base := middleware.New(recorder("A", &log), recorder("B", &log))
	admin := base.Append(recorder("D", &log))
	other := base.Append(recorder("E", &log))

	cases := []struct {
		chain middleware.Chain
		want  []string
	}{
		{base, []string{"A>", "B>", "H", "<B", "<A"}},
		{admin, []string{"A>", "B>", "D>", "H", "<D", "<B", "<A"}},
		{other, []string{"A>", "B>", "E>", "H", "<E", "<B", "<A"}},
	}
	for _, c := range cases {
		log = nil
		serve(t, c.chain.Then(handler(&log)))
		if !slices.Equal(log, c.want) {
			t.Errorf("order = %v, want %v", log, c.want)
		}
	}
}

func TestChain_NewCopiesInput(t *testing.T) {
	var log []string
	mws := []middleware.Middleware{recorder("A", &log)}
	c := middleware.New(mws...)
	mws[0] = recorder("X", &log)
	serve(t, c.Then(handler(&log)))
	if !slices.Equal(log, []string{"A>", "H", "<A"}) {
		t.Errorf("order = %v, the chain was affected by mutating the input slice", log)
	}
}

// TestChain_AppendChain checks that the middleware of the receiver stay
// outermost, that both chains are unchanged, and that the errors of the
// appended chain are carried (US4 scenarios 1 to 3).
func TestChain_AppendChain(t *testing.T) {
	var log []string
	first := middleware.New(recorder("A", &log))
	second := middleware.New(recorder("B", &log), recorder("C", &log))
	serve(t, first.AppendChain(second).Then(handler(&log)))
	want := []string{"A>", "B>", "C>", "H", "<C", "<B", "<A"}
	if !slices.Equal(log, want) {
		t.Errorf("order = %v, want %v", log, want)
	}
	log = nil
	serve(t, first.Then(handler(&log)))
	if !slices.Equal(log, []string{"A>", "H", "<A"}) {
		t.Errorf("AppendChain modified the receiver: %v", log)
	}
	log = nil
	serve(t, second.Then(handler(&log)))
	if !slices.Equal(log, []string{"B>", "C>", "H", "<C", "<B"}) {
		t.Errorf("AppendChain modified the other chain: %v", log)
	}

	t.Run("empty chain", func(t *testing.T) {
		for name, c := range map[string]middleware.Chain{
			"appended":    first.AppendChain(middleware.Chain{}),
			"as receiver": middleware.Chain{}.AppendChain(first),
		} {
			log = nil
			serve(t, c.Then(handler(&log)))
			if !slices.Equal(log, []string{"A>", "H", "<A"}) {
				t.Errorf("%s: order = %v", name, log)
			}
			if err := c.Err(); err != nil {
				t.Errorf("%s: Err() = %v, want nil", name, err)
			}
		}
	})

	t.Run("errors", func(t *testing.T) {
		withA := middleware.New().Use(nil, errors.New("a failed"))
		withB := middleware.New().Use(nil, errors.New("b failed"))
		if got := withA.AppendChain(withB).Err(); got == nil || got.Error() != "a failed\nb failed" {
			t.Errorf("Err() = %v, want the receiver's error, then the other's", got)
		}
		if withA.Err().Error() != "a failed" || withB.Err().Error() != "b failed" {
			t.Errorf("AppendChain modified a chain: %v, %v", withA.Err(), withB.Err())
		}
		if _, ok := recovered(func() { first.AppendChain(withB).Then(handler(&log)) }).(error); !ok {
			t.Error("Then did not panic on a chain with appended errors")
		}
	})

	t.Run("self-append keeps duplicates", func(t *testing.T) {
		self := first.Use(nil, errors.New("a failed"))
		if got := self.AppendChain(self).Err(); got == nil || got.Error() != "a failed\na failed" {
			t.Errorf("Err() = %v, want the error twice", got)
		}
		log = nil
		serve(t, first.AppendChain(first).Then(handler(&log)))
		if want := []string{"A>", "A>", "H", "<A", "<A"}; !slices.Equal(log, want) {
			t.Errorf("order = %v, want %v", log, want)
		}
	})
}

func TestChain_ThenBuildsOnce(t *testing.T) {
	builds := 0
	counting := func(next http.Handler) http.Handler {
		builds++
		return next
	}
	h := middleware.New(counting).Then(http.NotFoundHandler())
	for range 3 {
		serve(t, h)
	}
	if builds != 1 {
		t.Errorf("middleware built %d times, want 1", builds)
	}
}

func TestChain_NilPanics(t *testing.T) {
	var log []string
	tests := []struct {
		name string
		call func()
		want string
	}{
		{"New", func() { middleware.New(recorder("A", &log), nil) }, "middleware: nil middleware at position 1"},
		{"Append", func() { middleware.New().Append(nil) }, "middleware: nil middleware at position 0"},
		{"Use", func() { middleware.New(recorder("A", &log)).Use(nil, nil) }, "middleware: nil middleware at position 1"},
		{"Then", func() { middleware.New().Then(nil) }, "middleware: nil handler"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defer func() {
				v := recover()
				msg, _ := v.(string)
				if !strings.Contains(msg, tt.want) {
					t.Errorf("panic = %v, want %q", v, tt.want)
				}
			}()
			tt.call()
		})
	}
}

func TestChain_ThirdPartyFuncs(t *testing.T) {
	var log []string
	thirdParty := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			log = append(log, "T")
			next.ServeHTTP(w, r)
		})
	}
	plain := []func(http.Handler) http.Handler{thirdParty, recorder("A", &log)}
	serve(t, middleware.New(plain...).Then(handler(&log)))
	if !slices.Equal(log, []string{"T", "A>", "H", "<A"}) {
		t.Errorf("order = %v", log)
	}
}

// TestChain_Use checks that valid constructor results run exactly as built
// middleware, in the order they were added (US1 scenarios 1, 5 and 7).
func TestChain_Use(t *testing.T) {
	var log []string
	outside := func() (func(http.Handler) http.Handler, error) { return recorder("T", &log), nil }
	tests := []struct {
		name  string
		chain middleware.Chain
		want  []string
	}{
		{
			name:  "as New",
			chain: middleware.New().Use(recorder("A", &log), nil).Use(recorder("B", &log), nil),
			want:  []string{"A>", "B>", "H", "<B", "<A"},
		},
		{
			name: "mixed with Append",
			chain: middleware.New().
				Use(recorder("A", &log), nil).
				Append(recorder("B", &log)).
				Use(recorder("C", &log), nil).
				Append(recorder("D", &log)),
			want: []string{"A>", "B>", "C>", "D>", "H", "<D", "<C", "<B", "<A"},
		},
		{
			name:  "constructor outside the module",
			chain: middleware.Chain{}.Use(outside()),
			want:  []string{"T>", "H", "<T"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.chain.Err(); err != nil {
				t.Fatalf("Err() = %v, want nil", err)
			}
			log = nil
			serve(t, tt.chain.Then(handler(&log)))
			if !slices.Equal(log, tt.want) {
				t.Errorf("order = %v, want %v", log, tt.want)
			}
		})
	}
}

// TestChain_Err checks which errors a chain reports, in which order, and that
// they stay identifiable (US1 scenarios 2 and 4; FR-006 to FR-008, FR-010).
func TestChain_Err(t *testing.T) {
	var log []string
	errA, errB, errC := errors.New("a failed"), errors.New("b failed"), errors.New("c failed")
	errConfig := fmt.Errorf("%w: test: setting: invalid", middleware.ErrInvalidConfig)
	base := middleware.New().Use(nil, errA)
	public := base.Use(nil, errB).Append(recorder("A", &log))
	authenticated := public.Append(recorder("B", &log)).Use(nil, errC)

	tests := []struct {
		name  string
		chain middleware.Chain
		want  []error
	}{
		{"zero chain", middleware.Chain{}, nil},
		{"New and Append", middleware.New(recorder("A", &log)).Append(recorder("B", &log)), nil},
		{"one error", base, []error{errA}},
		{"three levels", authenticated, []error{errA, errB, errC}},
		{"both results present", middleware.New().Use(recorder("A", &log), errA), []error{errA}},
		{"invalid configuration", middleware.New().Use(nil, errConfig), []error{errConfig}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.chain.Err()
			if tt.want == nil {
				if err != nil {
					t.Errorf("Err() = %v, want nil", err)
				}
				return
			}
			msgs := make([]string, len(tt.want))
			for i, w := range tt.want {
				msgs[i] = w.Error()
				if !errors.Is(err, w) {
					t.Errorf("errors.Is(Err(), %q) = false", w)
				}
			}
			if want := strings.Join(msgs, "\n"); err == nil || err.Error() != want {
				t.Errorf("Err() = %q, want %q", err, want)
			}
			if got, want := errors.Is(err, middleware.ErrInvalidConfig), slices.Contains(tt.want, errConfig); got != want {
				t.Errorf("errors.Is(Err(), ErrInvalidConfig) = %v, want %v", got, want)
			}
			if again := tt.chain.Err(); again == nil || again.Error() != err.Error() {
				t.Errorf("second Err() = %v, want %v", again, err)
			}
		})
	}
}

// TestChain_UseWithConstructors reproduces the prototype of the request with
// the module's constructors: an invalid CORS origin and a body limit of 0
// (US1 scenarios 2 and 3, US3 scenario 1).
func TestChain_UseWithConstructors(t *testing.T) {
	var log []string
	origins := []string{"not an origin"}
	base := middleware.New().
		Use(requestid.New()).
		Append(recorder("span", &log))
	public := base.
		Use(cors.New(origins, cors.WithCredentials())).
		Use(bodylimit.New(0))

	_, corsErr := cors.New(origins, cors.WithCredentials())
	_, limitErr := bodylimit.New(0)
	err := public.Err()
	if want := corsErr.Error() + "\n" + limitErr.Error(); err == nil || err.Error() != want {
		t.Errorf("public.Err() = %q, want %q", err, want)
	}
	if !errors.Is(err, middleware.ErrInvalidConfig) {
		t.Errorf("errors.Is(public.Err(), ErrInvalidConfig) = false")
	}
	if err := base.Err(); err != nil {
		t.Fatalf("base.Err() = %v, want nil", err)
	}
	serve(t, base.Then(handler(&log)))
	if want := []string{"span>", "H", "<span"}; !slices.Equal(log, want) {
		t.Errorf("order = %v, want %v", log, want)
	}
}

// TestChain_ThenWithErrorsPanics checks that applying a chain with recorded
// errors fails at once, with every error, and builds no middleware (US2
// scenarios 1 to 3, SC-003, edge case "absent handler").
func TestChain_ThenWithErrorsPanics(t *testing.T) {
	builds := 0
	counting := func(next http.Handler) http.Handler {
		builds++
		return next
	}
	errA := errors.New("a failed")
	errB := fmt.Errorf("%w: test: setting: invalid", middleware.ErrInvalidConfig)
	c := middleware.New(counting).Use(nil, errA).Use(nil, errB)
	want := "middleware: chain has configuration errors:\n" + errA.Error() + "\n" + errB.Error()

	check := func(t *testing.T, h http.Handler) {
		t.Helper()
		v := recovered(func() { c.Then(h) })
		err, ok := v.(error)
		if !ok {
			t.Fatalf("panic value = %#v, want an error", v)
		}
		if err.Error() != want {
			t.Errorf("panic = %q, want %q", err, want)
		}
		if !errors.Is(err, errA) || !errors.Is(err, middleware.ErrInvalidConfig) {
			t.Errorf("panic value %v does not wrap the recorded errors", err)
		}
	}
	t.Run("handler", func(t *testing.T) { check(t, http.NotFoundHandler()) })
	t.Run("nil handler", func(t *testing.T) { check(t, nil) })
	t.Run("after Err was read", func(t *testing.T) {
		if c.Err() == nil {
			t.Fatal("Err() = nil")
		}
		check(t, http.NotFoundHandler())
	})
	t.Run("every application", func(t *testing.T) {
		for range 3 {
			check(t, http.NotFoundHandler())
		}
	})
	if builds != 0 {
		t.Errorf("middleware built %d times, want 0", builds)
	}
}

// TestChain_UseSameAllocs checks that a request through a chain built with
// Use allocates as much as through the same chain built with New (US2
// scenario 5, SC-005).
func TestChain_UseSameAllocs(t *testing.T) {
	origins := []string{"https://app.example.com"}
	withNew := middleware.New(must(t)(requestid.New()), must(t)(bodylimit.New(1<<20)), must(t)(cors.New(origins)))
	withUse := middleware.New().Use(requestid.New()).Use(bodylimit.New(1 << 20)).Use(cors.New(origins))
	allocs := func(c middleware.Chain) float64 {
		h := c.Then(http.NotFoundHandler())
		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil)
		req.Header.Set("Origin", origins[0])
		return testing.AllocsPerRun(100, func() { h.ServeHTTP(httptest.NewRecorder(), req) })
	}
	if n, u := allocs(withNew), allocs(withUse); n != u {
		t.Errorf("allocations per request: New %v, Use %v", n, u)
	}
}

// TestChain_ErrIsolation checks that chains derived from one base keep their
// errors and their middleware apart (US3). The base records three errors: if
// Use appended to the base's slice instead of copying it, both siblings would
// write into its spare capacity, and one would report the other's error.
func TestChain_ErrIsolation(t *testing.T) {
	var log []string
	base := middleware.New(recorder("base", &log)).
		Use(nil, errors.New("e1")).
		Use(nil, errors.New("e2")).
		Use(nil, errors.New("e3"))
	a := base.Use(recorder("a", &log), nil).Use(nil, errors.New("a failed"))
	b := base.Use(nil, errors.New("b failed")).Append(recorder("b", &log))
	for name, tt := range map[string]struct {
		chain middleware.Chain
		want  string
	}{
		"base":      {base, "e1\ne2\ne3"},
		"sibling a": {a, "e1\ne2\ne3\na failed"},
		"sibling b": {b, "e1\ne2\ne3\nb failed"},
	} {
		if err := tt.chain.Err(); err == nil || err.Error() != tt.want {
			t.Errorf("%s: Err() = %q, want %q", name, err, tt.want)
		}
	}

	clean := middleware.New(recorder("base", &log))
	broken := clean.Use(nil, errors.New("x failed"))
	x := clean.Append(recorder("x", &log))
	y := clean.Use(recorder("y", &log), nil)
	if broken.Err() == nil {
		t.Fatal("broken.Err() = nil")
	}
	for name, tt := range map[string]struct {
		chain middleware.Chain
		want  []string
	}{
		"clean base": {clean, []string{"base>", "H", "<base"}},
		"sibling x":  {x, []string{"base>", "x>", "H", "<x", "<base"}},
		"sibling y":  {y, []string{"base>", "y>", "H", "<y", "<base"}},
	} {
		if err := tt.chain.Err(); err != nil {
			t.Errorf("%s: Err() = %v, want nil", name, err)
		}
		log = nil
		serve(t, tt.chain.Then(handler(&log)))
		if !slices.Equal(log, tt.want) {
			t.Errorf("%s: order = %v, want %v", name, log, tt.want)
		}
	}
}

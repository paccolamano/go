package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/paccolamano/go/middleware"
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

func TestChain_Extend(t *testing.T) {
	var log []string
	first := middleware.New(recorder("A", &log))
	second := middleware.New(recorder("B", &log), recorder("C", &log))
	serve(t, first.Extend(second).Then(handler(&log)))
	want := []string{"A>", "B>", "C>", "H", "<C", "<B", "<A"}
	if !slices.Equal(log, want) {
		t.Errorf("order = %v, want %v", log, want)
	}
	log = nil
	serve(t, first.Then(handler(&log)))
	if !slices.Equal(log, []string{"A>", "H", "<A"}) {
		t.Errorf("Extend modified the receiver: %v", log)
	}
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

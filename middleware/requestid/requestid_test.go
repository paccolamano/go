package requestid_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/paccolamano/go/middleware"
	"github.com/paccolamano/go/middleware/requestid"
)

type observed struct {
	fromContext string
	ok          bool
	headerSeen  string
}

// run serves one request through the middleware and returns what the handler
// observed and the response.
func run(t *testing.T, mw middleware.Middleware, header string, incoming ...string) (observed, *httptest.ResponseRecorder) {
	t.Helper()
	var obs observed
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		obs.fromContext, obs.ok = requestid.FromContext(r.Context())
		obs.headerSeen = w.Header().Get(header)
	}))
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil)
	for _, v := range incoming {
		req.Header.Add(header, v)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return obs, rec
}

func mustNew(t *testing.T, opts ...requestid.Option) middleware.Middleware {
	t.Helper()
	mw, err := requestid.New(opts...)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return mw
}

func valid(id string) bool {
	if len(id) < 1 || len(id) > 128 {
		return false
	}
	for i := range len(id) {
		if id[i] < 0x21 || id[i] > 0x7e {
			return false
		}
	}
	return true
}

func TestRequestID_InvalidConfig(t *testing.T) {
	tests := []struct {
		name string
		opt  requestid.Option
	}{
		{"empty header name", requestid.WithHeader("")},
		{"header name with space", requestid.WithHeader("X Request")},
		{"nil generator", requestid.WithGenerator(nil)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mw, err := requestid.New(tt.opt)
			if !errors.Is(err, middleware.ErrInvalidConfig) || mw != nil {
				t.Fatalf("New = %v, %v; want nil, ErrInvalidConfig", mw, err)
			}
			if !strings.HasPrefix(err.Error(), "middleware: invalid configuration: requestid: ") {
				t.Errorf("error message = %q", err)
			}
		})
	}
}

func TestRequestID_GeneratesWhenMissing(t *testing.T) {
	obs, rec := run(t, mustNew(t), "X-Request-ID")
	id := rec.Header().Get("X-Request-ID")
	if len(id) != 26 || !valid(id) {
		t.Fatalf("generated id = %q, want 26 valid characters", id)
	}
	if obs.fromContext != id || !obs.ok {
		t.Errorf("FromContext = %q, %v; want %q, true", obs.fromContext, obs.ok, id)
	}
	if obs.headerSeen != id {
		t.Errorf("response header seen by the handler = %q, want %q (set before next)", obs.headerSeen, id)
	}
	_, rec2 := run(t, mustNew(t), "X-Request-ID")
	if rec2.Header().Get("X-Request-ID") == id {
		t.Error("two requests got the same generated id")
	}
}

func TestRequestID_Incoming(t *testing.T) {
	long := strings.Repeat("a", 128)
	tests := []struct {
		name     string
		incoming []string
		reused   string // "" means a new id must be generated
	}{
		{"valid", []string{"abc-123"}, "abc-123"},
		{"uuid", []string{"0f8fad5b-d9cb-469f-a165-70867728950e"}, "0f8fad5b-d9cb-469f-a165-70867728950e"},
		{"128 bytes", []string{long}, long},
		{"first value only", []string{"first", "second"}, "first"},
		{"129 bytes", []string{long + "a"}, ""},
		{"space", []string{"a b"}, ""},
		{"carriage return", []string{"a\rb"}, ""},
		{"non ascii", []string{"caf\xc3\xa9"}, ""},
		{"empty", []string{""}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			obs, rec := run(t, mustNew(t), "X-Request-ID", tt.incoming...)
			got := rec.Header().Get("X-Request-ID")
			if tt.reused != "" && got != tt.reused {
				t.Errorf("id = %q, want reused %q", got, tt.reused)
			}
			if tt.reused == "" && (got == "" || got == tt.incoming[0] || !valid(got)) {
				t.Errorf("id = %q, want a newly generated valid id", got)
			}
			if obs.fromContext != got {
				t.Errorf("FromContext = %q, want %q", obs.fromContext, got)
			}
		})
	}
}

func TestRequestID_DoNotTrustIncoming(t *testing.T) {
	_, rec := run(t, mustNew(t, requestid.WithTrustIncoming(false)), "X-Request-ID", "abc")
	if got := rec.Header().Get("X-Request-ID"); got == "abc" || !valid(got) {
		t.Errorf("id = %q, want a new id", got)
	}
}

func TestRequestID_CustomHeader(t *testing.T) {
	_, rec := run(t, mustNew(t, requestid.WithHeader("x-correlation-id")), "X-Correlation-Id", "corr-1")
	if got := rec.Header().Get("X-Correlation-Id"); got != "corr-1" {
		t.Errorf("X-Correlation-Id = %q, want corr-1", got)
	}
	if rec.Header().Get("X-Request-ID") != "" {
		t.Error("the default header must not be set when a custom one is configured")
	}
}

func TestRequestID_Generator(t *testing.T) {
	t.Run("valid output is used", func(t *testing.T) {
		_, rec := run(t, mustNew(t, requestid.WithGenerator(func() string { return "fixed-id" })), "X-Request-ID")
		if got := rec.Header().Get("X-Request-ID"); got != "fixed-id" {
			t.Errorf("id = %q, want fixed-id", got)
		}
	})
	t.Run("invalid output falls back", func(t *testing.T) {
		_, rec := run(t, mustNew(t, requestid.WithGenerator(func() string { return "bad id" })), "X-Request-ID")
		if got := rec.Header().Get("X-Request-ID"); len(got) != 26 || !valid(got) {
			t.Errorf("id = %q, want a 26-character fallback id", got)
		}
	})
}

func TestRequestID_FromContextWithoutMiddleware(t *testing.T) {
	if id, ok := requestid.FromContext(t.Context()); ok || id != "" {
		t.Errorf("FromContext = %q, %v; want \"\", false", id, ok)
	}
}

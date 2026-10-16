package middleware_test

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/paccolamano/go/middleware"
	"github.com/paccolamano/go/middleware/bodylimit"
	"github.com/paccolamano/go/middleware/contenttype"
	"github.com/paccolamano/go/middleware/decompress"
	"github.com/paccolamano/go/middleware/maxconcurrency"
	"github.com/paccolamano/go/middleware/ratelimit"
	"github.com/paccolamano/go/middleware/recovery"
)

// refusalCase builds one refusing middleware with fn and triggers a refusal.
type refusalCase struct {
	name       string
	status     int
	cause      error
	protocol   string // header that must be set before fn runs
	build      func(t *testing.T, fn middleware.RejectFunc) middleware.Middleware
	trigger    func(t *testing.T, h http.Handler) *httptest.ResponseRecorder
	handler    http.Handler
	causeLabel string
}

func newPost(t *testing.T, body string, header http.Header) *http.Request {
	t.Helper()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/", strings.NewReader(body))
	for k, v := range header {
		req.Header[k] = v
	}
	return req
}

func serveRec(h http.Handler, req *http.Request) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func refusalCases() []refusalCase {
	okHandler := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})
	return []refusalCase{
		{
			name: "bodylimit", status: http.StatusRequestEntityTooLarge, cause: bodylimit.ErrTooLarge, causeLabel: "too_large",
			build: func(t *testing.T, fn middleware.RejectFunc) middleware.Middleware {
				t.Helper()
				return must(t)(bodylimit.New(4, bodylimit.WithRejectFunc(fn)))
			},
			trigger: func(t *testing.T, h http.Handler) *httptest.ResponseRecorder {
				t.Helper()
				return serveRec(h, newPost(t, "too long", nil))
			},
			handler: okHandler,
		},
		{
			name: "contenttype", status: http.StatusUnsupportedMediaType, cause: contenttype.ErrUnsupported, causeLabel: "unsupported_media_type",
			build: func(t *testing.T, fn middleware.RejectFunc) middleware.Middleware {
				t.Helper()
				return must(t)(contenttype.New([]string{"application/json"}, contenttype.WithRejectFunc(fn)))
			},
			trigger: func(t *testing.T, h http.Handler) *httptest.ResponseRecorder {
				t.Helper()
				return serveRec(h, newPost(t, "x", http.Header{"Content-Type": {"text/plain"}}))
			},
			handler: okHandler,
		},
		{
			name: "decompress", status: http.StatusUnsupportedMediaType, cause: decompress.ErrUnsupportedEncoding, causeLabel: "unsupported_encoding",
			protocol: "Accept-Encoding",
			build: func(t *testing.T, fn middleware.RejectFunc) middleware.Middleware {
				t.Helper()
				return must(t)(decompress.New(decompress.WithRejectFunc(fn)))
			},
			trigger: func(t *testing.T, h http.Handler) *httptest.ResponseRecorder {
				t.Helper()
				return serveRec(h, newPost(t, "x", http.Header{"Content-Encoding": {"br"}}))
			},
			handler: okHandler,
		},
		{
			name: "ratelimit", status: http.StatusTooManyRequests, cause: ratelimit.ErrLimitExceeded, causeLabel: "rate_limited",
			protocol: "Retry-After",
			build: func(t *testing.T, fn middleware.RejectFunc) middleware.Middleware {
				t.Helper()
				return must(t)(ratelimit.New(1, time.Hour, ratelimit.WithRejectFunc(fn)))
			},
			trigger: func(t *testing.T, h http.Handler) *httptest.ResponseRecorder {
				t.Helper()
				serveRec(h, newPost(t, "", nil))
				return serveRec(h, newPost(t, "", nil))
			},
			handler: okHandler,
		},
		maxConcurrencyCase(),
		{
			name: "recovery", status: http.StatusInternalServerError, cause: recovery.ErrPanic, causeLabel: "internal",
			build: func(t *testing.T, fn middleware.RejectFunc) middleware.Middleware {
				t.Helper()
				return must(t)(recovery.New(slog.New(slog.DiscardHandler), recovery.WithRejectFunc(fn)))
			},
			trigger: func(t *testing.T, h http.Handler) *httptest.ResponseRecorder {
				t.Helper()
				return serveRec(h, newPost(t, "", nil))
			},
			handler: http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic("boom") }),
		},
	}
}

// maxConcurrencyCase holds the only slot with a request marked X-Hold, then
// triggers a refusal.
func maxConcurrencyCase() refusalCase {
	started, release := make(chan struct{}), make(chan struct{})
	return refusalCase{
		name: "maxconcurrency", status: http.StatusServiceUnavailable, cause: maxconcurrency.ErrOverloaded, causeLabel: "overloaded",
		protocol: "Retry-After",
		build: func(t *testing.T, fn middleware.RejectFunc) middleware.Middleware {
			t.Helper()
			return must(t)(maxconcurrency.New(1, maxconcurrency.WithRetryAfter(time.Second), maxconcurrency.WithRejectFunc(fn)))
		},
		trigger: func(t *testing.T, h http.Handler) *httptest.ResponseRecorder {
			t.Helper()
			done := make(chan struct{})
			go func() {
				defer close(done)
				serveRec(h, newPost(t, "", http.Header{"X-Hold": {"1"}}))
			}()
			<-started
			rec := serveRec(h, newPost(t, "", nil))
			close(release)
			<-done
			return rec
		},
		handler: http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
			if r.Header.Get("X-Hold") != "" {
				close(started)
				<-release
			}
		}),
	}
}

func must(tb testing.TB) func(middleware.Middleware, error) middleware.Middleware {
	tb.Helper()
	return func(mw middleware.Middleware, err error) middleware.Middleware {
		tb.Helper()
		if err != nil {
			tb.Fatalf("New: %v", err)
		}
		return mw
	}
}

func TestReject_SharedFunc(t *testing.T) {
	type observed struct {
		rej      middleware.Rejection
		protocol map[string]string
	}
	for _, tc := range refusalCases() {
		t.Run(tc.name, func(t *testing.T) {
			var obs observed
			shared := func(w http.ResponseWriter, _ *http.Request, rej middleware.Rejection) {
				obs.rej = rej
				obs.protocol = map[string]string{
					"Retry-After":     w.Header().Get("Retry-After"),
					"Accept-Encoding": w.Header().Get("Accept-Encoding"),
				}
				code := "error"
				for _, c := range refusalCases() {
					if errors.Is(rej.Err, c.cause) {
						code = c.causeLabel
					}
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(rej.Status)
				_ = json.NewEncoder(w).Encode(map[string]string{"code": code})
			}
			rec := tc.trigger(t, tc.build(t, shared)(tc.handler))
			if rec.Code != tc.status || rec.Header().Get("Content-Type") != "application/json" {
				t.Fatalf("response = %d %q, want %d JSON", rec.Code, rec.Header().Get("Content-Type"), tc.status)
			}
			var body map[string]string
			if err := json.NewDecoder(rec.Body).Decode(&body); err != nil || body["code"] != tc.causeLabel {
				t.Errorf("body = %v, %v; want code %q", body, err, tc.causeLabel)
			}
			if !errors.Is(obs.rej.Err, tc.cause) || obs.rej.Status != tc.status {
				t.Errorf("rejection = %+v", obs.rej)
			}
			if tc.protocol != "" && obs.protocol[tc.protocol] == "" {
				t.Errorf("%s was not set before the reject func ran", tc.protocol)
			}
		})
	}
}

func TestReject_FallbackStatus(t *testing.T) {
	silent := func(http.ResponseWriter, *http.Request, middleware.Rejection) {}
	for _, tc := range refusalCases() {
		t.Run(tc.name, func(t *testing.T) {
			rec := tc.trigger(t, tc.build(t, silent)(tc.handler))
			if rec.Code != tc.status {
				t.Errorf("status = %d, want %d although the reject func wrote nothing", rec.Code, tc.status)
			}
			if body, _ := io.ReadAll(rec.Body); len(body) != 0 {
				t.Errorf("body = %q, want empty", body)
			}
		})
	}
}

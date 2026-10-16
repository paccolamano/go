package etag_test

import (
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/paccolamano/go/middleware"
	"github.com/paccolamano/go/middleware/etag"
)

func mustNew(t *testing.T, opts ...etag.Option) middleware.Middleware {
	t.Helper()
	mw, err := etag.New(opts...)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return mw
}

func tagOf(body string) string {
	sum := sha256.Sum256([]byte(body))
	return `"` + base64.RawURLEncoding.EncodeToString(sum[:16]) + `"`
}

func page(body string, header http.Header, status int) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		for k, v := range header {
			w.Header()[k] = v
		}
		if status != 0 {
			w.WriteHeader(status)
		}
		_, _ = w.Write([]byte(body))
	})
}

func get(t *testing.T, h http.Handler, method string, ifNoneMatch ...string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequestWithContext(t.Context(), method, "/", nil)
	for _, v := range ifNoneMatch {
		req.Header.Add("If-None-Match", v)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestETag_InvalidConfig(t *testing.T) {
	for _, n := range []int{0, -1} {
		mw, err := etag.New(etag.WithMaxSize(n))
		if !errors.Is(err, middleware.ErrInvalidConfig) || mw != nil {
			t.Errorf("WithMaxSize(%d): New = %v, %v; want nil, ErrInvalidConfig", n, mw, err)
		} else if !strings.HasPrefix(err.Error(), "middleware: invalid configuration: etag: ") {
			t.Errorf("error message = %q", err)
		}
	}
}

func TestETag_Generated(t *testing.T) {
	const body = `{"id":1,"name":"widget"}`
	rec := get(t, mustNew(t)(page(body, nil, 0)), http.MethodGet)
	want := tagOf(body)
	if got := rec.Header().Get("ETag"); got != want || len(got) != 24 {
		t.Errorf("ETag = %q, want %q", got, want)
	}
	if rec.Code != http.StatusOK || rec.Body.String() != body {
		t.Errorf("response = %d %q", rec.Code, rec.Body.String())
	}
	other := get(t, mustNew(t)(page(body, nil, 0)), http.MethodGet)
	if other.Header().Get("ETag") != want {
		t.Error("two instances produced different tags for the same content")
	}
	weak := get(t, mustNew(t, etag.WithWeak())(page(body, nil, 0)), http.MethodGet)
	if got := weak.Header().Get("ETag"); got != "W/"+want {
		t.Errorf("weak ETag = %q, want W/%s", got, want)
	}
}

func TestETag_NotModified(t *testing.T) {
	const body = "hello"
	header := http.Header{
		"Content-Type":     {"text/plain"},
		"Content-Encoding": {"identity"},
		"Cache-Control":    {"max-age=60"},
		"Vary":             {"Accept"},
		"Expires":          {"Thu, 01 Jan 2099 00:00:00 GMT"},
		"Content-Location": {"/hello"},
	}
	h := mustNew(t)(page(body, header, 0))
	tag := tagOf(body)
	for name, inm := range map[string][]string{
		"exact":       {tag},
		"weak":        {"W/" + tag},
		"list":        {`"other", ` + tag},
		"star":        {"*"},
		"two headers": {`"other"`, tag},
	} {
		t.Run(name, func(t *testing.T) {
			rec := get(t, h, http.MethodGet, inm...)
			if rec.Code != http.StatusNotModified || rec.Body.Len() != 0 {
				t.Fatalf("response = %d with %d body bytes, want 304 and no body", rec.Code, rec.Body.Len())
			}
			for _, k := range []string{"Content-Type", "Content-Length", "Content-Encoding"} {
				if rec.Header().Get(k) != "" {
					t.Errorf("%s present on a 304", k)
				}
			}
			for _, k := range []string{"ETag", "Cache-Control", "Vary", "Expires", "Content-Location"} {
				if rec.Header().Get(k) == "" {
					t.Errorf("%s missing on a 304", k)
				}
			}
		})
	}
	if rec := get(t, h, http.MethodGet, `"other"`); rec.Code != http.StatusOK || rec.Body.String() != body {
		t.Errorf("non-matching If-None-Match = %d %q, want the full response", rec.Code, rec.Body.String())
	}
	for range 3 {
		if rec := get(t, h, http.MethodGet, tag); rec.Code != http.StatusNotModified || rec.Body.Len() != 0 {
			t.Errorf("repeated conditional request = %d with %d bytes", rec.Code, rec.Body.Len())
		}
	}
}

func TestETag_HandlerTag(t *testing.T) {
	h := mustNew(t)(page("content", http.Header{"Etag": {`"v42"`}}, 0))
	if rec := get(t, h, http.MethodGet); rec.Header().Get("ETag") != `"v42"` || rec.Body.String() != "content" {
		t.Errorf("handler ETag was replaced: %v", rec.Header())
	}
	if rec := get(t, h, http.MethodGet, `W/"v42"`); rec.Code != http.StatusNotModified || rec.Body.Len() != 0 {
		t.Errorf("conditional request with the handler's tag = %d", rec.Code)
	}
}

func TestETag_PassThrough(t *testing.T) {
	big := strings.Repeat("x", 2048)
	tests := []struct {
		name    string
		handler http.Handler
		method  string
		opts    []etag.Option
		body    string
	}{
		{"over max size", page(big, nil, 0), http.MethodGet, []etag.Option{etag.WithMaxSize(1024)}, big},
		{"non-200", page("missing", nil, http.StatusNotFound), http.MethodGet, nil, "missing"},
		{"content range", page("part", http.Header{"Content-Range": {"bytes 0-3/10"}}, http.StatusPartialContent), http.MethodGet, nil, "part"},
		{"content range on 200", page("part", http.Header{"Content-Range": {"bytes 0-3/4"}}, 0), http.MethodGet, nil, "part"},
		{"post", page("created", nil, 0), http.MethodPost, nil, "created"},
		{"flush", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte("stream"))
			_ = http.NewResponseController(w).Flush()
		}), http.MethodGet, nil, "stream"},
		{"head without body", http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}), http.MethodHead, nil, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := get(t, mustNew(t, tt.opts...)(tt.handler), tt.method)
			if rec.Header().Get("ETag") != "" {
				t.Errorf("ETag = %q, want none", rec.Header().Get("ETag"))
			}
			if rec.Body.String() != tt.body {
				t.Errorf("body = %q, want %q", rec.Body.String(), tt.body)
			}
		})
	}
	// HEAD with the same body as GET gets the same tag.
	if rec := get(t, mustNew(t)(page("same", nil, 0)), http.MethodHead); rec.Header().Get("ETag") != tagOf("same") {
		t.Errorf("HEAD ETag = %q, want %q", rec.Header().Get("ETag"), tagOf("same"))
	}
}

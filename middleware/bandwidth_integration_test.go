package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/paccolamano/go/middleware"
	"github.com/paccolamano/go/middleware/compress"
	"github.com/paccolamano/go/middleware/etag"
)

func TestIntegration_ETagBehindCompress(t *testing.T) {
	comp, err := compress.New()
	if err != nil {
		t.Fatal(err)
	}
	tags, err := etag.New()
	if err != nil {
		t.Fatal(err)
	}
	doc := `{"items":[` + strings.Repeat(`{"name":"item","price":10},`, 200) + `{}]}`
	h := middleware.New(comp, tags).Then(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(doc))
	}))
	get := func(acceptEncoding, ifNoneMatch string) *httptest.ResponseRecorder {
		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil)
		if acceptEncoding != "" {
			req.Header.Set("Accept-Encoding", acceptEncoding)
		}
		if ifNoneMatch != "" {
			req.Header.Set("If-None-Match", ifNoneMatch)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}

	plain := get("", "")
	compressed := get("gzip", "")
	strong := plain.Header().Get("ETag")
	weak := compressed.Header().Get("ETag")
	if compressed.Header().Get("Content-Encoding") != "gzip" {
		t.Fatal("the response was not compressed")
	}
	if !strings.HasPrefix(strong, `"`) || weak != "W/"+strong {
		t.Fatalf("validators = %q (identity) and %q (gzip), want a strong tag and its weak form", strong, weak)
	}
	if rec := get("gzip", weak); rec.Code != http.StatusNotModified || rec.Body.Len() != 0 {
		t.Errorf("conditional request with the weak tag = %d with %d bytes, want 304", rec.Code, rec.Body.Len())
	}
	if rec := get("", strong); rec.Code != http.StatusNotModified {
		t.Errorf("conditional request with the strong tag = %d, want 304", rec.Code)
	}
}

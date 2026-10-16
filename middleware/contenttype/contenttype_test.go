package contenttype_test

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/paccolamano/go/middleware"
	"github.com/paccolamano/go/middleware/contenttype"
)

func mustNew(t *testing.T, allowed []string, opts ...contenttype.Option) middleware.Middleware {
	t.Helper()
	mw, err := contenttype.New(allowed, opts...)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return mw
}

func TestContentType_InvalidConfig(t *testing.T) {
	tests := []struct {
		name    string
		allowed []string
		opts    []contenttype.Option
	}{
		{"empty list", nil, nil},
		{"uppercase", []string{"Application/JSON"}, nil},
		{"no subtype", []string{"application"}, nil},
		{"parameters", []string{"application/json; charset=utf-8"}, nil},
		{"wildcard type only", []string{"*/json"}, nil},
		{"structured suffix pattern", []string{"application/*+json"}, nil},
		{"nil reject func", []string{"application/json"}, []contenttype.Option{contenttype.WithRejectFunc(nil)}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mw, err := contenttype.New(tt.allowed, tt.opts...)
			if !errors.Is(err, middleware.ErrInvalidConfig) || mw != nil {
				t.Fatalf("New = %v, %v; want nil, ErrInvalidConfig", mw, err)
			}
			if !strings.HasPrefix(err.Error(), "middleware: invalid configuration: contenttype: ") {
				t.Errorf("error message = %q", err)
			}
		})
	}
}

func TestContentType_Requests(t *testing.T) {
	mw := mustNew(t, []string{"application/json", "text/*"})
	tests := []struct {
		name          string
		method        string
		body          io.Reader
		contentLength int64 // applied when not 0
		contentType   string
		allowed       bool
	}{
		{"allowed", http.MethodPost, strings.NewReader("{}"), 0, "application/json", true},
		{"parameters ignored", http.MethodPost, strings.NewReader("{}"), 0, "application/json; charset=utf-8", true},
		{"case-insensitive", http.MethodPost, strings.NewReader("{}"), 0, "APPLICATION/JSON", true},
		{"wildcard", http.MethodPut, strings.NewReader("hi"), 0, "text/csv", true},
		{"disallowed", http.MethodPost, strings.NewReader("x"), 0, "application/xml", false},
		{"missing", http.MethodPost, strings.NewReader("x"), 0, "", false},
		{"unparseable", http.MethodPost, strings.NewReader("x"), 0, "json", false},
		{"chunked body", http.MethodPost, strings.NewReader("x"), -1, "image/png", false},
		{"no body ignores type", http.MethodGet, nil, 0, "image/png", true},
		{"declared empty body", http.MethodPost, http.NoBody, 0, "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			invoked := false
			var rej middleware.Rejection
			h := mustNew(t, []string{"application/json", "text/*"}, contenttype.WithRejectFunc(
				func(w http.ResponseWriter, _ *http.Request, r middleware.Rejection) {
					rej = r
					w.WriteHeader(r.Status)
				}))(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { invoked = true }))
			req := httptest.NewRequestWithContext(t.Context(), tt.method, "/", tt.body)
			if tt.contentLength != 0 {
				req.ContentLength = tt.contentLength
			}
			if tt.contentType != "" {
				req.Header.Set("Content-Type", tt.contentType)
			}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if tt.allowed {
				if !invoked {
					t.Errorf("request refused with %d", rec.Code)
				}
				return
			}
			if invoked || rec.Code != http.StatusUnsupportedMediaType {
				t.Errorf("invoked=%v code=%d, want 415", invoked, rec.Code)
			}
			if !errors.Is(rej.Err, contenttype.ErrUnsupported) {
				t.Errorf("rejection = %+v, want ErrUnsupported", rej)
			}
		})
	}
	// The default response is the plain-text reason phrase.
	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/", strings.NewReader("x"))
	mw(http.NotFoundHandler()).ServeHTTP(rec, req)
	if rec.Code != http.StatusUnsupportedMediaType || !strings.Contains(rec.Body.String(), "Unsupported Media Type") {
		t.Errorf("default response = %d %q", rec.Code, rec.Body.String())
	}
}

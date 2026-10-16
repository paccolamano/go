package bodylimit_test

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/paccolamano/go/middleware"
	"github.com/paccolamano/go/middleware/bodylimit"
)

// countingBody reports how many bytes were read from it.
type countingBody struct {
	r    io.Reader
	read int
}

func (b *countingBody) Read(p []byte) (int, error) {
	n, err := b.r.Read(p)
	b.read += n
	return n, err
}

func (b *countingBody) Close() error { return nil }

func mustNew(t *testing.T, limit int64, opts ...bodylimit.Option) middleware.Middleware {
	t.Helper()
	mw, err := bodylimit.New(limit, opts...)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return mw
}

func TestBodyLimit_InvalidConfig(t *testing.T) {
	tests := []struct {
		name  string
		limit int64
		opts  []bodylimit.Option
	}{
		{"zero limit", 0, nil},
		{"negative limit", -1, nil},
		{"nil reject func", 10, []bodylimit.Option{bodylimit.WithRejectFunc(nil)}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mw, err := bodylimit.New(tt.limit, tt.opts...)
			if !errors.Is(err, middleware.ErrInvalidConfig) || mw != nil {
				t.Fatalf("New = %v, %v; want nil, ErrInvalidConfig", mw, err)
			}
			if !strings.HasPrefix(err.Error(), "middleware: invalid configuration: bodylimit: ") {
				t.Errorf("error message = %q", err)
			}
		})
	}
}

func TestBodyLimit_DeclaredTooLarge(t *testing.T) {
	body := &countingBody{r: strings.NewReader(strings.Repeat("x", 100))}
	invoked := false
	h := mustNew(t, 10)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { invoked = true }))
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/", body)
	req.ContentLength = 100
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge || invoked {
		t.Fatalf("code=%d invoked=%v, want 413 without invoking next", rec.Code, invoked)
	}
	if strings.TrimSpace(rec.Body.String()) != http.StatusText(http.StatusRequestEntityTooLarge) {
		t.Errorf("body = %q, want the plain-text reason", rec.Body.String())
	}
	if body.read != 0 {
		t.Errorf("read %d body bytes, want 0", body.read)
	}
}

func TestBodyLimit_RejectFunc(t *testing.T) {
	var got middleware.Rejection
	h := mustNew(t, 10, bodylimit.WithRejectFunc(func(w http.ResponseWriter, _ *http.Request, rej middleware.Rejection) {
		got = rej
		w.WriteHeader(rej.Status)
	}))(http.NotFoundHandler())
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/", strings.NewReader(strings.Repeat("x", 11)))
	h.ServeHTTP(httptest.NewRecorder(), req)
	if got.Status != http.StatusRequestEntityTooLarge || !errors.Is(got.Err, bodylimit.ErrTooLarge) {
		t.Errorf("rejection = %+v, want 413 with ErrTooLarge", got)
	}
}

func TestBodyLimit_UndeclaredTooLarge(t *testing.T) {
	var readErr error
	h := mustNew(t, 10)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		_, readErr = io.ReadAll(r.Body)
	}))
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/", strings.NewReader(strings.Repeat("x", 100)))
	req.ContentLength = -1
	h.ServeHTTP(httptest.NewRecorder(), req)
	var mbe *http.MaxBytesError
	if !errors.As(readErr, &mbe) || mbe.Limit != 10 {
		t.Errorf("read error = %v, want *http.MaxBytesError with limit 10", readErr)
	}
}

func TestBodyLimit_PassThrough(t *testing.T) {
	tests := []struct {
		name string
		body io.Reader
		want string
	}{
		{"within the limit", strings.NewReader("0123456789"), "0123456789"},
		{"no body", http.NoBody, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got string
			var readErr error
			h := mustNew(t, 10)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
				b, err := io.ReadAll(r.Body)
				got, readErr = string(b), err
			}))
			h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/", tt.body))
			if got != tt.want || readErr != nil {
				t.Errorf("body = %q, %v; want %q", got, readErr, tt.want)
			}
		})
	}
}

package compress_test

import (
	"bytes"
	"compress/gzip"
	"compress/zlib"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/paccolamano/go/middleware"
	"github.com/paccolamano/go/middleware/compress"
)

func mustNew(t *testing.T, opts ...compress.Option) middleware.Middleware {
	t.Helper()
	mw, err := compress.New(opts...)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return mw
}

func fixture(t *testing.T) []byte {
	t.Helper()
	b, err := os.ReadFile("testdata/fixture.json")
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// respond returns a handler that sets the given headers, optionally a status,
// and writes body.
func respond(status int, header http.Header, body []byte) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		for k, v := range header {
			w.Header()[k] = v
		}
		if status != 0 {
			w.WriteHeader(status)
		}
		_, _ = w.Write(body)
	})
}

func do(t *testing.T, h http.Handler, method, acceptEncoding string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequestWithContext(t.Context(), method, "/", nil)
	if acceptEncoding != "" {
		req.Header.Set("Accept-Encoding", acceptEncoding)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func gunzip(t *testing.T, b []byte) []byte {
	t.Helper()
	zr, err := gzip.NewReader(bytes.NewReader(b))
	if err != nil {
		t.Fatalf("gzip.NewReader: %v", err)
	}
	out, err := io.ReadAll(zr)
	if err != nil {
		t.Fatalf("decompress: %v", err)
	}
	return out
}

func jsonHeader() http.Header { return http.Header{"Content-Type": {"application/json"}} }

func TestCompress_InvalidConfig(t *testing.T) {
	gz := func(w io.Writer) (compress.Writer, error) { return gzip.NewWriter(w), nil }
	tests := []struct {
		name string
		opts []compress.Option
	}{
		{"level too low", []compress.Option{compress.WithLevel(-3)}},
		{"level too high", []compress.Option{compress.WithLevel(10)}},
		{"negative min size", []compress.Option{compress.WithMinSize(-1)}},
		{"no content types", []compress.Option{compress.WithContentTypes()}},
		{"bad content type", []compress.Option{compress.WithContentTypes("JSON")}},
		{"coding name uppercase", []compress.Option{compress.WithCoding("Br", gz)}},
		{"coding name not a token", []compress.Option{compress.WithCoding("b r", gz)}},
		{"coding identity", []compress.Option{compress.WithCoding("identity", gz)}},
		{"nil coding constructor", []compress.Option{compress.WithCoding("br", nil)}},
		{"empty preference", []compress.Option{compress.WithPreference()}},
		{"unknown preference", []compress.Option{compress.WithPreference("br")}},
		{"duplicate preference", []compress.Option{compress.WithPreference("gzip", "gzip")}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mw, err := compress.New(tt.opts...)
			if !errors.Is(err, middleware.ErrInvalidConfig) || mw != nil {
				t.Fatalf("New = %v, %v; want nil, ErrInvalidConfig", mw, err)
			}
			if !strings.HasPrefix(err.Error(), "middleware: invalid configuration: compress: ") {
				t.Errorf("error message = %q", err)
			}
		})
	}
}

func TestCompress_Gzip(t *testing.T) {
	body := fixture(t)
	rec := do(t, mustNew(t)(respond(0, jsonHeader(), body)), http.MethodGet, "gzip")
	if rec.Header().Get("Content-Encoding") != "gzip" {
		t.Fatalf("Content-Encoding = %q, want gzip", rec.Header().Get("Content-Encoding"))
	}
	if rec.Header().Get("Content-Length") != "" {
		t.Error("Content-Length present on a compressed response")
	}
	if got := rec.Header().Values("Vary"); len(got) != 1 || got[0] != "Accept-Encoding" {
		t.Errorf("Vary = %q, want exactly [Accept-Encoding]", got)
	}
	if !bytes.Equal(gunzip(t, rec.Body.Bytes()), body) {
		t.Error("the decompressed body differs from the original")
	}
	if ratio := float64(rec.Body.Len()) / float64(len(body)); ratio > 0.30 {
		t.Errorf("compressed size is %.0f%% of the original, want at most 30%%", ratio*100)
	}
}

func TestCompress_Deflate(t *testing.T) {
	body := fixture(t)
	rec := do(t, mustNew(t)(respond(0, jsonHeader(), body)), http.MethodGet, "deflate")
	if rec.Header().Get("Content-Encoding") != "deflate" {
		t.Fatalf("Content-Encoding = %q, want deflate", rec.Header().Get("Content-Encoding"))
	}
	zr, err := zlib.NewReader(rec.Body)
	if err != nil {
		t.Fatal(err)
	}
	if out, err := io.ReadAll(zr); err != nil || !bytes.Equal(out, body) {
		t.Errorf("zlib round trip failed: %v", err)
	}
}

func TestCompress_NotCompressed(t *testing.T) {
	big := bytes.Repeat([]byte("a"), 4096)
	mw := mustNew(t)
	tests := []struct {
		name    string
		handler http.Handler
		method  string
		accept  string
	}{
		{"below min size", respond(0, jsonHeader(), []byte(`{"ok":true}`)), http.MethodGet, "gzip"},
		{"not compressible", respond(0, http.Header{"Content-Type": {"image/png"}}, big), http.MethodGet, "gzip"},
		{"no accept-encoding", respond(0, jsonHeader(), big), http.MethodGet, ""},
		{"identity only", respond(0, jsonHeader(), big), http.MethodGet, "identity"},
		{"gzip excluded", respond(0, jsonHeader(), big), http.MethodGet, "gzip;q=0"},
		{"already encoded", respond(0, http.Header{"Content-Type": {"application/json"}, "Content-Encoding": {"br"}}, big), http.MethodGet, "gzip"},
		{"partial content", respond(http.StatusPartialContent, http.Header{"Content-Type": {"application/json"}, "Content-Range": {"bytes 0-4095/9000"}}, big), http.MethodGet, "gzip"},
		{"content range", respond(0, http.Header{"Content-Type": {"application/json"}, "Content-Range": {"bytes 0-4095/4096"}}, big), http.MethodGet, "gzip"},
		{"no content", respond(http.StatusNoContent, jsonHeader(), nil), http.MethodGet, "gzip"},
		{"not modified", respond(http.StatusNotModified, jsonHeader(), nil), http.MethodGet, "gzip"},
		{"head", respond(0, jsonHeader(), big), http.MethodHead, "gzip"},
		{"no-transform", respond(0, http.Header{"Content-Type": {"application/json"}, "Cache-Control": {"public, no-transform"}}, big), http.MethodGet, "gzip"},
		{"declared length below min size", respond(0, http.Header{"Content-Type": {"application/json"}, "Content-Length": {"11"}}, []byte(`{"ok":true}`)), http.MethodGet, "gzip"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := do(t, mw(tt.handler), tt.method, tt.accept)
			if got := rec.Header().Get("Content-Encoding"); got != "" && got != "br" {
				t.Errorf("Content-Encoding = %q, want none", got)
			}
			if got := rec.Header().Values("Vary"); len(got) != 1 || got[0] != "Accept-Encoding" {
				t.Errorf("Vary = %q, want [Accept-Encoding]", got)
			}
		})
	}
}

func TestCompress_DeclaredLengthAboveMinSize(t *testing.T) {
	body := fixture(t)
	h := respond(0, http.Header{"Content-Type": {"application/json"}, "Content-Length": {strconv.Itoa(len(body))}}, body)
	rec := do(t, mustNew(t)(h), http.MethodGet, "gzip")
	if rec.Header().Get("Content-Encoding") != "gzip" || rec.Header().Get("Content-Length") != "" {
		t.Errorf("headers = %v, want gzip without Content-Length", rec.Header())
	}
}

func TestCompress_WeakensETag(t *testing.T) {
	h := http.Header{"Content-Type": {"application/json"}, "Etag": {`"v1"`}}
	rec := do(t, mustNew(t)(respond(0, h, fixture(t))), http.MethodGet, "gzip")
	if got := rec.Header().Get("ETag"); got != `W/"v1"` {
		t.Errorf("ETag = %q, want W/\"v1\"", got)
	}
	rec = do(t, mustNew(t)(respond(0, h, fixture(t))), http.MethodGet, "")
	if got := rec.Header().Get("ETag"); got != `"v1"` {
		t.Errorf("uncompressed ETag = %q, want \"v1\"", got)
	}
}

func TestCompress_SniffsContentType(t *testing.T) {
	page := []byte("<!DOCTYPE html><html><body>" + strings.Repeat("<p>hello</p>", 200) + "</body></html>")
	rec := do(t, mustNew(t)(respond(0, nil, page)), http.MethodGet, "gzip")
	if ct := rec.Header().Get("Content-Type"); ct != "text/html; charset=utf-8" {
		t.Errorf("Content-Type = %q, want the type sniffed from the uncompressed body", ct)
	}
	if rec.Header().Get("Content-Encoding") != "gzip" || !bytes.Equal(gunzip(t, rec.Body.Bytes()), page) {
		t.Error("the sniffed HTML page was not compressed correctly")
	}
}

func TestCompress_CustomCodingAndPreference(t *testing.T) {
	custom := func(w io.Writer) (compress.Writer, error) { return gzip.NewWriterLevel(w, gzip.BestSpeed) }
	big := fixture(t)
	rec := do(t, mustNew(t, compress.WithCoding("x-custom", custom))(respond(0, jsonHeader(), big)), http.MethodGet, "gzip, x-custom")
	if got := rec.Header().Get("Content-Encoding"); got != "x-custom" {
		t.Errorf("Content-Encoding = %q, want the custom coding preferred", got)
	}
	rec = do(t, mustNew(t, compress.WithPreference("gzip"))(respond(0, jsonHeader(), big)), http.MethodGet, "deflate")
	if got := rec.Header().Get("Content-Encoding"); got != "" {
		t.Errorf("Content-Encoding = %q, want deflate disabled", got)
	}
	rec = do(t, mustNew(t, compress.WithContentTypes("image/*"))(respond(0, http.Header{"Content-Type": {"image/bmp"}}, big)), http.MethodGet, "gzip")
	if got := rec.Header().Get("Content-Encoding"); got != "gzip" {
		t.Errorf("Content-Encoding = %q, want gzip for a configured type", got)
	}
	rec = do(t, mustNew(t, compress.WithMinSize(0))(respond(0, jsonHeader(), []byte("{}"))), http.MethodGet, "gzip")
	if got := rec.Header().Get("Content-Encoding"); got != "gzip" {
		t.Errorf("Content-Encoding = %q, want gzip with WithMinSize(0)", got)
	}
}

// fakeWriter records every status code, unlike httptest.ResponseRecorder,
// which treats 1xx codes as final.
type fakeWriter struct {
	header http.Header
	codes  []int
	body   bytes.Buffer
}

func (f *fakeWriter) Header() http.Header         { return f.header }
func (f *fakeWriter) WriteHeader(code int)        { f.codes = append(f.codes, code) }
func (f *fakeWriter) Write(p []byte) (int, error) { return f.body.Write(p) }

func TestCompress_ForwardsInformational(t *testing.T) {
	fake := &fakeWriter{header: http.Header{}}
	h := mustNew(t)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Link", "</style.css>; rel=preload")
		w.WriteHeader(http.StatusEarlyHints)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(fixture(t))
	}))
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	h.ServeHTTP(fake, req)
	if len(fake.codes) != 2 || fake.codes[0] != http.StatusEarlyHints || fake.codes[1] != http.StatusOK {
		t.Errorf("codes = %v, want [103 200]", fake.codes)
	}
	if fake.header.Get("Content-Encoding") != "gzip" {
		t.Error("the final response was not compressed")
	}
}

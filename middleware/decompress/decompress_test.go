package decompress_test

import (
	"bytes"
	"compress/gzip"
	"compress/zlib"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/paccolamano/go/middleware"
	"github.com/paccolamano/go/middleware/decompress"
)

func mustNew(t *testing.T, opts ...decompress.Option) middleware.Middleware {
	t.Helper()
	mw, err := decompress.New(opts...)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return mw
}

func gzipped(t *testing.T, s string) []byte {
	t.Helper()
	var b bytes.Buffer
	zw := gzip.NewWriter(&b)
	if _, err := io.WriteString(zw, s); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func zlibbed(t *testing.T, s string) []byte {
	t.Helper()
	var b bytes.Buffer
	zw := zlib.NewWriter(&b)
	if _, err := io.WriteString(zw, s); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

type seen struct {
	invoked       bool
	body          string
	readErr       error
	encoding      string
	contentLength int64
	lengthHeader  string
}

func send(t *testing.T, mw middleware.Middleware, body []byte, encoding string) (seen, *httptest.ResponseRecorder) {
	t.Helper()
	var s seen
	h := mw(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		s.invoked = true
		b, err := io.ReadAll(r.Body)
		s.body, s.readErr = string(b), err
		s.encoding = r.Header.Get("Content-Encoding")
		s.contentLength = r.ContentLength
		s.lengthHeader = r.Header.Get("Content-Length")
	}))
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/", bytes.NewReader(body))
	if encoding != "" {
		req.Header.Set("Content-Encoding", encoding)
	}
	req.Header.Set("Content-Length", "123")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return s, rec
}

func TestDecompress_InvalidConfig(t *testing.T) {
	dec := func(r io.Reader) (io.ReadCloser, error) { return io.NopCloser(r), nil }
	tests := []struct {
		name string
		opts []decompress.Option
	}{
		{"zero max size", []decompress.Option{decompress.WithMaxSize(0)}},
		{"negative max size", []decompress.Option{decompress.WithMaxSize(-1)}},
		{"decoder name uppercase", []decompress.Option{decompress.WithDecoder("Br", dec)}},
		{"decoder name not a token", []decompress.Option{decompress.WithDecoder("b r", dec)}},
		{"decoder identity", []decompress.Option{decompress.WithDecoder("identity", dec)}},
		{"nil decoder", []decompress.Option{decompress.WithDecoder("br", nil)}},
		{"nil reject func", []decompress.Option{decompress.WithRejectFunc(nil)}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mw, err := decompress.New(tt.opts...)
			if !errors.Is(err, middleware.ErrInvalidConfig) || mw != nil {
				t.Fatalf("New = %v, %v; want nil, ErrInvalidConfig", mw, err)
			}
			if !strings.HasPrefix(err.Error(), "middleware: invalid configuration: decompress: ") {
				t.Errorf("error message = %q", err)
			}
		})
	}
}

func TestDecompress_Decodes(t *testing.T) {
	const payload = `{"message":"hello, compressed world"}`
	for name, body := range map[string][]byte{"gzip": gzipped(t, payload), "deflate": zlibbed(t, payload)} {
		t.Run(name, func(t *testing.T) {
			s, _ := send(t, mustNew(t), body, name)
			if s.readErr != nil || s.body != payload {
				t.Fatalf("body = %q, %v; want the decoded payload", s.body, s.readErr)
			}
			if s.encoding != "" || s.lengthHeader != "" || s.contentLength != -1 {
				t.Errorf("Content-Encoding=%q Content-Length=%q ContentLength=%d; want removed and -1",
					s.encoding, s.lengthHeader, s.contentLength)
			}
		})
	}
}

func TestDecompress_Unsupported(t *testing.T) {
	for _, encoding := range []string{"br", "gzip, gzip", "gzip, deflate"} {
		t.Run(encoding, func(t *testing.T) {
			var rej middleware.Rejection
			var acceptSeen string
			mw := mustNew(t, decompress.WithRejectFunc(func(w http.ResponseWriter, _ *http.Request, r middleware.Rejection) {
				rej = r
				acceptSeen = w.Header().Get("Accept-Encoding")
				w.WriteHeader(r.Status)
			}))
			s, rec := send(t, mw, []byte("whatever"), encoding)
			if s.invoked || rec.Code != http.StatusUnsupportedMediaType {
				t.Fatalf("invoked=%v code=%d, want 415", s.invoked, rec.Code)
			}
			if !errors.Is(rej.Err, decompress.ErrUnsupportedEncoding) {
				t.Errorf("rejection = %+v", rej)
			}
			if acceptSeen != "gzip, deflate" {
				t.Errorf("Accept-Encoding seen by the reject func = %q, want gzip, deflate", acceptSeen)
			}
		})
	}
	_, rec := send(t, mustNew(t), []byte("x"), "br")
	if rec.Code != http.StatusUnsupportedMediaType || !strings.Contains(rec.Body.String(), "Unsupported Media Type") {
		t.Errorf("default response = %d %q", rec.Code, rec.Body.String())
	}
}

func TestDecompress_MaxSize(t *testing.T) {
	bomb := gzipped(t, strings.Repeat("A", 1<<20))
	s, _ := send(t, mustNew(t, decompress.WithMaxSize(1024)), bomb, "gzip")
	var mbe *http.MaxBytesError
	if !errors.As(s.readErr, &mbe) {
		t.Errorf("read error = %v, want *http.MaxBytesError", s.readErr)
	}
	if len(s.body) > 1024 {
		t.Errorf("handler read %d bytes, more than the cap", len(s.body))
	}
}

func TestDecompress_InvalidBody(t *testing.T) {
	good := gzipped(t, strings.Repeat("payload ", 100))
	corrupt := bytes.Clone(good)
	corrupt[len(corrupt)/2] ^= 0xff
	tests := map[string][]byte{
		"bad header": []byte("this is not gzip at all"),
		"truncated":  good[:len(good)/2],
		"corrupt":    corrupt,
	}
	for name, body := range tests {
		t.Run(name, func(t *testing.T) {
			s, _ := send(t, mustNew(t), body, "gzip")
			if !s.invoked {
				t.Fatal("the handler was not invoked: corrupt data must surface as a read error")
			}
			if !errors.Is(s.readErr, decompress.ErrInvalidBody) {
				t.Errorf("read error = %v, want ErrInvalidBody", s.readErr)
			}
		})
	}
}

func TestDecompress_PassThrough(t *testing.T) {
	for _, encoding := range []string{"", "identity"} {
		t.Run("encoding "+encoding, func(t *testing.T) {
			s, _ := send(t, mustNew(t), []byte("plain"), encoding)
			if s.body != "plain" || s.encoding != encoding || s.contentLength != 5 {
				t.Errorf("seen = %+v, want the request unchanged", s)
			}
		})
	}
	t.Run("no body", func(t *testing.T) {
		var s seen
		h := mustNew(t)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
			s.invoked = true
			s.encoding = r.Header.Get("Content-Encoding")
		}))
		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil)
		req.Header.Set("Content-Encoding", "br")
		h.ServeHTTP(httptest.NewRecorder(), req)
		if !s.invoked || s.encoding != "br" {
			t.Errorf("seen = %+v, want the request unchanged", s)
		}
	})
}

func TestDecompress_CustomDecoder(t *testing.T) {
	upper := func(r io.Reader) (io.ReadCloser, error) {
		b, err := io.ReadAll(r)
		if err != nil {
			return nil, err
		}
		return io.NopCloser(strings.NewReader(strings.ToUpper(string(b)))), nil
	}
	var accept string
	mw := mustNew(t, decompress.WithDecoder("x-upper", upper), decompress.WithRejectFunc(
		func(w http.ResponseWriter, _ *http.Request, r middleware.Rejection) {
			accept = w.Header().Get("Accept-Encoding")
			w.WriteHeader(r.Status)
		}))
	if s, _ := send(t, mw, []byte("shout"), "x-upper"); s.body != "SHOUT" {
		t.Errorf("body = %q, want SHOUT", s.body)
	}
	send(t, mw, []byte("x"), "br")
	if accept != "gzip, deflate, x-upper" {
		t.Errorf("Accept-Encoding = %q, want the custom coding listed", accept)
	}
}

func TestDecompress_BodyClose(t *testing.T) {
	var closeErr error
	h := mustNew(t)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		_, _ = io.ReadAll(r.Body)
		closeErr = r.Body.Close()
	}))
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/", bytes.NewReader(gzipped(t, "payload")))
	req.Header.Set("Content-Encoding", "gzip")
	h.ServeHTTP(httptest.NewRecorder(), req)
	if closeErr != nil {
		t.Errorf("Close = %v, want nil", closeErr)
	}
}

package problem_test

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/paccolamano/go/problem"
)

const maxDocumentSize = 1 << 20

// response returns a response to a GET of https://api.example.com/v1/messages.
func response(tb testing.TB, code int, contentType string, body io.Reader) *http.Response {
	tb.Helper()
	h := make(http.Header)
	if contentType != "" {
		h.Set("Content-Type", contentType)
	}
	return &http.Response{
		StatusCode: code,
		Header:     h,
		Body:       io.NopCloser(body),
		Request:    newRequest(tb, http.MethodGet, "https://api.example.com/v1/messages"),
	}
}

// padded returns a valid problem document of exactly size bytes.
func padded(size int) string {
	const head, tail = `{"type":"about:blank","pad":"`, `"}`
	return head + strings.Repeat("x", size-len(head)-len(tail)) + tail
}

func TestRead_OutOfCredit(t *testing.T) {
	p, err := problem.Read(response(t, http.StatusForbidden, problem.MediaType, strings.NewReader(creditDocument)))
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	// The relative instance is resolved against the request URL (FR-025).
	want := creditOccurrence()
	if p.Type() != want.Type() || p.Title() != want.Title() || p.Status() != want.Status() || p.Detail() != want.Detail() {
		t.Errorf("members = (%q, %q, %d, %q)", p.Type(), p.Title(), p.Status(), p.Detail())
	}
	if p.Instance() != "https://api.example.com/account/12345/msgs/abc" {
		t.Errorf("Instance() = %q", p.Instance())
	}
	if v, ok := balance.Lookup(p); !ok || v != 30 {
		t.Errorf("balance = %d, %v", v, ok)
	}
	if v, ok := accounts.Lookup(p); !ok || !slices.Equal(v, []string{"/account/12345", "/account/67890"}) {
		t.Errorf("accounts = %v, %v", v, ok)
	}
	if _, ok := outOfCredit.Match(p); !ok {
		t.Error("the problem read does not match the declared type")
	}
}

func TestRead_MistypedMembers(t *testing.T) {
	doc := `{"type":"https://example.com/probs/out-of-credit","status":"403","title":42,"detail":"d"}`
	p, err := problem.Read(response(t, http.StatusForbidden, problem.MediaType, strings.NewReader(doc)))
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if p.Status() != 0 || p.Title() != "" || p.Detail() != "d" {
		t.Errorf("status %d, title %q, detail %q; want 0, \"\", \"d\"", p.Status(), p.Title(), p.Detail())
	}
	if ext := p.Extensions(); len(ext) != 0 {
		t.Errorf("mistyped members became extensions: %v", ext)
	}
}

func TestRead_MissingType(t *testing.T) {
	p, err := problem.Read(response(t, http.StatusBadRequest, problem.MediaType, strings.NewReader(`{"title":"x"}`)))
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if p.Type() != "about:blank" {
		t.Errorf("Type() = %q, want about:blank", p.Type())
	}
}

func TestRead_Resolve(t *testing.T) {
	const doc = `{"type":"/probs/out-of-credit","instance":"/account/12345/msgs/abc"}`
	t.Run("relative references", func(t *testing.T) {
		p, err := problem.Read(response(t, http.StatusForbidden, problem.MediaType, strings.NewReader(doc)))
		if err != nil {
			t.Fatalf("Read: %v", err)
		}
		if p.Type() != "https://api.example.com/probs/out-of-credit" {
			t.Errorf("Type() = %q", p.Type())
		}
		if p.Instance() != "https://api.example.com/account/12345/msgs/abc" {
			t.Errorf("Instance() = %q", p.Instance())
		}
	})
	t.Run("absolute references", func(t *testing.T) {
		abs := `{"type":"https://example.com/x","instance":"urn:example:1"}`
		p, err := problem.Read(response(t, http.StatusForbidden, problem.MediaType, strings.NewReader(abs)))
		if err != nil {
			t.Fatalf("Read: %v", err)
		}
		if p.Type() != "https://example.com/x" || p.Instance() != "urn:example:1" {
			t.Errorf("Type() = %q, Instance() = %q", p.Type(), p.Instance())
		}
		blank, err := problem.Read(response(t, http.StatusForbidden, problem.MediaType, strings.NewReader(`{}`)))
		if err != nil || blank.Type() != "about:blank" {
			t.Errorf("Type() = %q, %v; want about:blank", blank.Type(), err)
		}
	})
	t.Run("no request", func(t *testing.T) {
		resp := response(t, http.StatusForbidden, problem.MediaType, strings.NewReader(doc))
		resp.Request = nil
		p, err := problem.Read(resp)
		if err != nil {
			t.Fatalf("Read: %v", err)
		}
		if p.Type() != "/probs/out-of-credit" || p.Instance() != "/account/12345/msgs/abc" {
			t.Errorf("Type() = %q, Instance() = %q; want them unresolved", p.Type(), p.Instance())
		}
	})
	t.Run("Parse never resolves", func(t *testing.T) {
		p, err := problem.Parse([]byte(doc))
		if err != nil {
			t.Fatalf("Parse: %v", err)
		}
		if p.Type() != "/probs/out-of-credit" || p.Instance() != "/account/12345/msgs/abc" {
			t.Errorf("Type() = %q, Instance() = %q; want them unresolved", p.Type(), p.Instance())
		}
	})
}

func TestRead_UnknownExtensions(t *testing.T) {
	doc := `{"type":"about:blank","foo":{ "a" : [1, 2] },"bar":null}`
	p, err := problem.Read(response(t, http.StatusBadRequest, problem.MediaType, strings.NewReader(doc)))
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	ext := p.Extensions()
	if len(ext) != 2 || string(ext["foo"]) != `{"a":[1,2]}` || string(ext["bar"]) != "null" {
		t.Errorf("Extensions() = %s", fmtExtensions(p))
	}
}

func TestRead_NotProblem(t *testing.T) {
	const body = "<html><body>Bad Gateway</body></html>"
	for _, ct := range []string{"text/html", "application/json", "", "/"} {
		t.Run(ct, func(t *testing.T) {
			counter := &countingReader{r: strings.NewReader(body)}
			resp := response(t, http.StatusBadGateway, ct, counter)
			_, err := problem.Read(resp)
			if !errors.Is(err, problem.ErrNotProblem) {
				t.Fatalf("Read = %v, want ErrNotProblem", err)
			}
			if counter.n != 0 {
				t.Errorf("Read consumed %d bytes of the body", counter.n)
			}
			rest, _ := io.ReadAll(resp.Body)
			if string(rest) != body {
				t.Errorf("body after Read = %q", rest)
			}
		})
	}
	t.Run("nil response", func(t *testing.T) {
		if _, err := problem.Read(nil); !errors.Is(err, problem.ErrNotProblem) {
			t.Errorf("Read(nil) = %v, want ErrNotProblem", err)
		}
	})
}

func TestRead_Limits(t *testing.T) {
	t.Run("too large", func(t *testing.T) {
		counter := &countingReader{r: strings.NewReader(padded(maxDocumentSize + 1000))}
		_, err := problem.Read(response(t, http.StatusBadRequest, problem.MediaType, counter))
		if !errors.Is(err, problem.ErrMalformed) {
			t.Errorf("Read = %v, want ErrMalformed", err)
		}
		if counter.n > maxDocumentSize+1 {
			t.Errorf("Read consumed %d bytes, want at most %d", counter.n, maxDocumentSize+1)
		}
	})
	t.Run("exactly the maximum", func(t *testing.T) {
		doc := padded(maxDocumentSize)
		if _, err := problem.Read(response(t, http.StatusBadRequest, problem.MediaType, strings.NewReader(doc))); err != nil {
			t.Errorf("Read of %d bytes: %v", len(doc), err)
		}
	})
	t.Run("nil body", func(t *testing.T) {
		resp := response(t, http.StatusBadRequest, problem.MediaType, nil)
		resp.Body = nil
		if _, err := problem.Read(resp); !errors.Is(err, problem.ErrMalformed) {
			t.Errorf("Read = %v, want ErrMalformed", err)
		}
	})
	t.Run("failing body", func(t *testing.T) {
		_, err := problem.Read(response(t, http.StatusBadRequest, problem.MediaType, failingReader{}))
		if !errors.Is(err, errRead) || errors.Is(err, problem.ErrMalformed) {
			t.Errorf("Read = %v, want the read error and not ErrMalformed", err)
		}
	})
}

func TestRead_StatusDisagreement(t *testing.T) {
	resp := response(t, http.StatusServiceUnavailable, problem.MediaType, strings.NewReader(`{"status":500}`))
	p, err := problem.Read(resp)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if p.Status() != http.StatusInternalServerError || resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("Status() = %d, StatusCode = %d; want 500 and 503", p.Status(), resp.StatusCode)
	}
}

func TestRead_RoundTrip(t *testing.T) {
	for name, e := range map[string]*problem.Error{
		"out of credit":      creditOccurrence(),
		"validation":         validationError.New(problem.WithExtension(fieldErrors, twoFieldErrors())),
		"status only":        problem.Status(http.StatusNotFound),
		"no phrase":          problem.Status(499),
		"special characters": specialCharactersOccurrence(),
		"empty values":       emptyValuesOccurrence(),
	} {
		t.Run(name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			if err := problem.Write(rec, newRequest(t, http.MethodGet, "/"), e); err != nil {
				t.Fatalf("Write: %v", err)
			}
			got, err := problem.Read(rec.Result())
			if err != nil {
				t.Fatalf("Read: %v", err)
			}
			sameProblem(t, got, e)
		})
	}
}

func TestParse_Edge(t *testing.T) {
	parse := func(t *testing.T, doc string) *problem.Error {
		t.Helper()
		p, err := problem.Parse([]byte(doc))
		if err != nil {
			t.Fatalf("Parse(%.40q): %v", doc, err)
		}
		return p
	}
	t.Run("capitalized names are extensions", func(t *testing.T) {
		p := parse(t, `{"TYPE":"x","Status":1}`)
		ext := p.Extensions()
		if p.Type() != "about:blank" || p.Status() != 0 || string(ext["TYPE"]) != `"x"` || string(ext["Status"]) != "1" {
			t.Errorf("type %q, status %d, extensions %s", p.Type(), p.Status(), fmtExtensions(p))
		}
	})
	t.Run("last duplicate wins", func(t *testing.T) {
		if p := parse(t, `{"status":400,"status":404}`); p.Status() != http.StatusNotFound {
			t.Errorf("Status() = %d, want 404", p.Status())
		}
	})
	for raw, want := range map[string]int{
		"403": 403, "403.0": 403, "4.03e2": 403,
		"403.5": 0, "99": 0, "600": 0, "1e400": 0, `"403"`: 0, "true": 0, "null": 0,
	} {
		t.Run("status "+raw, func(t *testing.T) {
			if p := parse(t, `{"status":`+raw+`}`); p.Status() != want {
				t.Errorf("Status() = %d, want %d", p.Status(), want)
			}
		})
	}
	t.Run("invalid URI references", func(t *testing.T) {
		p := parse(t, `{"type":"/a b","instance":"/a b"}`)
		if p.Type() != "about:blank" || p.Instance() != "" {
			t.Errorf("Type() = %q, Instance() = %q; want about:blank and empty", p.Type(), p.Instance())
		}
	})
	t.Run("empty object", func(t *testing.T) {
		p := parse(t, `{}`)
		if p.Type() != "about:blank" || p.Title() != "" || p.Status() != 0 || p.Detail() != "" ||
			p.Instance() != "" || len(p.Extensions()) != 0 {
			t.Errorf("unexpected members: %v", p)
		}
	})
	malformed := map[string]string{
		"array":       `[]`,
		"null":        `null`,
		"string":      `"x"`,
		"truncated":   `{`,
		"empty":       ``,
		"too deep":    `{"a":` + strings.Repeat("[", 10001) + strings.Repeat("]", 10001) + `}`,
		"too large":   padded(maxDocumentSize + 1),
		"not JSON":    `type=about:blank`,
		"trailing":    `{} {}`,
		"bad element": `{"a":[1,]}`,
	}
	for name, doc := range malformed {
		t.Run("malformed "+name, func(t *testing.T) {
			if _, err := problem.Parse([]byte(doc)); !errors.Is(err, problem.ErrMalformed) {
				t.Errorf("Parse = %v, want ErrMalformed", err)
			}
		})
	}
}

func TestRead_EdgeMediaTypeVariants(t *testing.T) {
	for _, ct := range []string{
		"Application/Problem+JSON; charset=utf-8",
		"application/problem+json;charset=UTF-8",
		"application/problem+json",
	} {
		t.Run(strconv.Quote(ct), func(t *testing.T) {
			if _, err := problem.Read(response(t, http.StatusNotFound, ct, strings.NewReader(`{}`))); err != nil {
				t.Errorf("Read with %q: %v", ct, err)
			}
		})
	}
}

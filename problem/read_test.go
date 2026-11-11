package problem_test

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
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

// TestRead_RequestURL resolves the members of the table of FR-006 (spec 015)
// against a request URL with credentials, a query and a fragment, and the edge
// cases of other request URLs.
func TestRead_RequestURL(t *testing.T) {
	cred := withCredentials(t, "http://api.example.com/v1/accounts/42?api_key=XYZ")
	tests := []struct {
		name          string
		u             *url.URL
		member, value string
		want          string
	}{
		// The rows of FR-006.
		{"type, absolute path", cred, "type", "/probs/out-of-credit", "http://api.example.com/probs/out-of-credit"},
		{"instance, fragment", cred, "instance", "#msg-1", "http://api.example.com/v1/accounts/42?api_key=XYZ#msg-1"},
		{"instance, empty fragment", cred, "instance", "#", "http://api.example.com/v1/accounts/42?api_key=XYZ"},
		{"instance, query", cred, "instance", "?page=2", "http://api.example.com/v1/accounts/42?page=2"},
		{"instance, relative path", cred, "instance", "msgs/1", "http://api.example.com/v1/accounts/msgs/1"},
		{"type, network path", cred, "type", "//cdn.example.com/probs/x", "http://cdn.example.com/probs/x"},
		{"type, absolute", cred, "type", "https://other.example.com/probs/x", "https://other.example.com/probs/x"},
		{"no type", cred, "type", "", "about:blank"},
		// Edge cases.
		{
			"user name only",
			&url.URL{Scheme: "http", User: url.User("svc"), Host: "api.example.com", Path: "/v1"},
			"type", "/probs/out-of-credit", "http://api.example.com/probs/out-of-credit",
		},
		{
			"empty userinfo", // http://@api.example.com/v1
			&url.URL{Scheme: "http", User: url.User(""), Host: "api.example.com", Path: "/v1"},
			"type", "/probs/out-of-credit", "http://api.example.com/probs/out-of-credit",
		},
		{
			"not hierarchical", // as in problem/v0.2.1
			&url.URL{Scheme: "mailto", Opaque: "a@b"},
			"type", "/probs/x", "mailto:///probs/x",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc := `{}`
			if tt.value != "" {
				doc = fmt.Sprintf(`{%q:%q}`, tt.member, tt.value)
			}
			p := readFrom(t, tt.u, doc)
			got := p.Type()
			if tt.member == "instance" {
				got = p.Instance()
			}
			if got != tt.want {
				t.Errorf("%s %q from %s: got %q, want %q", tt.member, tt.value, tt.u.Redacted(), got, tt.want)
			}
		})
	}

	t.Run("caller's URL", func(t *testing.T) {
		p := readFrom(t, cred, `{"type":"/probs/out-of-credit"}`)
		if want := "http://api.example.com/probs/out-of-credit"; p.Type() != want {
			t.Errorf("Type() = %q, want %q", p.Type(), want)
		}
		if cred.User == nil || cred.Fragment != "frag" {
			t.Errorf("request URL = %s, want it with its userinfo and the fragment frag", cred.Redacted())
		}
	})
}

// TestRead_Credentials reads problems through http.Client, from request URLs
// that hold credentials and a fragment, and checks that neither reaches the
// problem, its message or the document written again from it (FR-014 of spec
// 015).
func TestRead_Credentials(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/old" {
			http.Redirect(w, r, "/v2/accounts/42?page=2#moved", http.StatusFound)
			return
		}
		// http.Client forwards Test-Doc across the redirect, to the same host.
		w.Header().Set("Content-Type", problem.MediaType)
		w.WriteHeader(http.StatusForbidden)
		_, _ = io.WriteString(w, r.Header.Get("Test-Doc"))
	}))
	defer srv.Close()

	userOnly, err := url.Parse(srv.URL + "/v1/accounts/42")
	if err != nil {
		t.Fatalf("url.Parse: %v", err)
	}
	userOnly.User = url.User("svc")
	targets := []struct {
		name     string
		u        *url.URL
		sent     string // the URL the request sent
		fragment string // the fragment that the client's last request URL still holds
	}{
		{
			"user and password, query and fragment", withCredentials(t, srv.URL+"/v1/accounts/42?api_key=XYZ"),
			srv.URL + "/v1/accounts/42?api_key=XYZ", "frag",
		},
		{"user name only", userOnly, srv.URL + "/v1/accounts/42", ""},
		{"relative redirect", withCredentials(t, srv.URL+"/old"), srv.URL + "/v2/accounts/42?page=2", "moved"},
	}
	// The members of the table of FR-006, each in a document that Write can send.
	docs := []struct{ name, typ, instance string }{
		{"instance #msg-1", "/probs/out-of-credit", "#msg-1"},
		{"instance #", "/probs/out-of-credit", "#"},
		{"instance ?page=2", "/probs/out-of-credit", "?page=2"},
		{"instance msgs/1", "/probs/out-of-credit", "msgs/1"},
		{"network-path type", "//cdn.example.com/probs/x", ""},
		{"absolute type", "https://other.example.com/probs/x", ""},
		{"no type", "", ""},
	}
	markers := []string{"svc", "s3cret", "frag", "moved"}

	// get sends a GET of u that asks the server for doc, and returns the problem
	// read from the response and the URL of the last request.
	get := func(t *testing.T, u *url.URL, doc string) (*problem.Error, *url.URL) {
		t.Helper()
		req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, u.String(), nil)
		if err != nil {
			t.Fatalf("NewRequest: %v", err)
		}
		req.Header.Set("Test-Doc", doc)
		resp, err := srv.Client().Do(req)
		if err != nil {
			t.Fatalf("Do: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusForbidden {
			t.Fatalf("status = %d, want 403", resp.StatusCode)
		}
		p, err := problem.Read(resp)
		if err != nil {
			t.Fatalf("Read: %v", err)
		}
		return p, resp.Request.URL
	}

	for _, tt := range targets {
		t.Run(tt.name, func(t *testing.T) {
			for _, d := range docs {
				t.Run(d.name, func(t *testing.T) {
					doc := `{"status":403`
					if d.typ != "" {
						doc += fmt.Sprintf(`,"type":%q`, d.typ)
					}
					if d.instance != "" {
						doc += fmt.Sprintf(`,"instance":%q`, d.instance)
					}
					doc += "}"
					p, last := get(t, tt.u, doc)

					// Nothing of the request URL beyond the URL it sent.
					rec := httptest.NewRecorder()
					if err := problem.Write(rec, newRequest(t, http.MethodGet, "/"), p); err != nil {
						t.Fatalf("Write: %v", err)
					}
					for _, v := range []struct{ what, s string }{
						{"Type()", p.Type()},
						{"Instance()", p.Instance()},
						{"Error()", p.Error()},
						{"the written document", rec.Body.String()},
					} {
						for _, m := range markers {
							if strings.Contains(v.s, m) {
								t.Errorf("%s = %q contains %q", v.what, v.s, m)
							}
						}
					}

					switch d.instance {
					case "#msg-1":
						if want := tt.sent + "#msg-1"; p.Instance() != want {
							t.Errorf("Instance() = %q, want %q", p.Instance(), want)
						}
					case "#":
						if p.Instance() != tt.sent {
							t.Errorf("Instance() = %q, want %q", p.Instance(), tt.sent)
						}
					}
					if d.typ == "/probs/out-of-credit" {
						if want := srv.URL + "/probs/out-of-credit"; p.Type() != want {
							t.Errorf("Type() = %q, want %q", p.Type(), want)
						}
						if got, ok := sharedCredit.Match(p); !ok || got != p {
							t.Errorf("Match = %v, %v for the type %q; want the problem, true", got, ok, p.Type())
						}
						if _, ok := sharedCredit.Match(p.With(problem.WithDetail("derived"))); !ok {
							t.Errorf("no match for the derived problem of type %q", p.Type())
						}
					}

					// The caller's request is not modified (FR-005).
					if last.User == nil || last.Fragment != tt.fragment {
						t.Errorf("last request URL = %s, want it with its userinfo and the fragment %q",
							last.Redacted(), tt.fragment)
					}
				})
			}
		})
	}
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

// TestRead_WriteAgain writes a problem read from a response: the document is the
// one problem/v0.2.0 writes, with the resolved members and no other trace of the
// request URL (FR-009).
func TestRead_WriteAgain(t *testing.T) {
	const (
		doc  = `{"type":"/probs/out-of-credit","title":"You do not have enough credit.","status":403,"detail":"Your current balance is 30, but that costs 50.","instance":"/account/12345/msgs/abc","balance":30}`
		want = `{"type":"https://api.example.com/probs/out-of-credit","title":"You do not have enough credit.","status":403,"detail":"Your current balance is 30, but that costs 50.","instance":"https://api.example.com/account/12345/msgs/abc","balance":30}`
	)
	p, err := problem.Read(response(t, http.StatusForbidden, problem.MediaType, strings.NewReader(doc)))
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	rec := httptest.NewRecorder()
	if err := problem.Write(rec, newRequest(t, http.MethodGet, "/"), p); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if got := rec.Body.String(); got != want {
		t.Errorf("document =\n%s\nwant\n%s", got, want)
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

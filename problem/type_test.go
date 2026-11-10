package problem_test

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/paccolamano/go/problem"
)

func TestNewType_Rules(t *testing.T) {
	accepted := []string{
		"https://example.com/probs/out-of-credit",
		"/probs/out-of-credit",
		"//example.com/probs/x",
		"tag:example.com,2026:out-of-credit",
		"urn:example:out-of-credit",
	}
	for _, uri := range accepted {
		t.Run("accepts "+uri, func(t *testing.T) {
			if _, err := problem.NewType(uri, "t", http.StatusForbidden); err != nil {
				t.Errorf("NewType(%q): %v", uri, err)
			}
		})
	}
	refused := []struct {
		name    string
		uri     string
		title   string
		status  int
		element string
	}{
		{"relative without full path", "out-of-credit", "t", http.StatusForbidden, "type URI"},
		{"relative path", "probs/x", "t", http.StatusForbidden, "type URI"},
		{"fragment only", "#x", "t", http.StatusForbidden, "type URI"},
		{"query only", "?x", "t", http.StatusForbidden, "type URI"},
		{"malformed host", "http://[::1", "t", http.StatusForbidden, "type URI"},
		{"space", "/a b", "t", http.StatusForbidden, "type URI"},
		{"non-ASCII", "/é", "t", http.StatusForbidden, "type URI"},
		{"bad escape", "%zz", "t", http.StatusForbidden, "type URI"},
		{"about:blank", "about:blank", "t", http.StatusForbidden, "type URI"},
		{"empty URI", "", "t", http.StatusForbidden, "type URI"},
		{"empty title", "/x", "", http.StatusForbidden, "title"},
		{"status 0", "/x", "t", 0, "status"},
		{"status 200", "/x", "t", http.StatusOK, "status"},
		{"status 302", "/x", "t", http.StatusFound, "status"},
		{"status 399", "/x", "t", 399, "status"},
		{"status 600", "/x", "t", 600, "status"},
	}
	for _, tt := range refused {
		t.Run("refuses "+tt.name, func(t *testing.T) {
			_, err := problem.NewType(tt.uri, tt.title, tt.status)
			if !errors.Is(err, problem.ErrInvalid) {
				t.Fatalf("NewType(%q, %q, %d) = %v, want ErrInvalid", tt.uri, tt.title, tt.status, err)
			}
			if !strings.Contains(err.Error(), tt.element) {
				t.Errorf("error %q does not name %q", err, tt.element)
			}
		})
	}
	for _, code := range []int{http.StatusBadRequest, 599} {
		if _, err := problem.NewType("/x", "t", code); err != nil {
			t.Errorf("NewType with status %d: %v", code, err)
		}
	}
}

func TestMust_Refused(t *testing.T) {
	defer func() {
		v := recover()
		err, ok := v.(error)
		if !ok || !errors.Is(err, problem.ErrInvalid) {
			t.Errorf("recovered %v, want an error matching ErrInvalid", v)
		}
	}()
	problem.Must(problem.NewType("out-of-credit", "t", http.StatusForbidden))
	t.Error("Must did not panic")
}

func TestNewType_Independent(t *testing.T) {
	a := outOfCredit.New(problem.WithDetail("a"), problem.WithExtension(balance, 1))
	b := outOfCredit.New(problem.WithDetail("b"), problem.WithExtension(balance, 2))
	for _, tc := range []struct {
		e      *problem.Error
		detail string
		bal    int
	}{{a, "a", 1}, {b, "b", 2}} {
		rec, err := write(t, tc.e)
		if err != nil {
			t.Fatalf("Write: %v", err)
		}
		want := fmt.Sprintf(`{"type":"https://example.com/probs/out-of-credit","title":"You do not have enough credit.","status":403,"detail":%q,"balance":%d}`,
			tc.detail, tc.bal)
		wantResponse(t, rec, http.StatusForbidden, want)
	}
	c := outOfCredit.New()
	if c.Type() != "https://example.com/probs/out-of-credit" || c.Title() != "You do not have enough credit." ||
		c.Status() != http.StatusForbidden || c.Detail() != "" || len(c.Extensions()) != 0 {
		t.Errorf("the declaration changed: %q %q %d %q %v", c.Type(), c.Title(), c.Status(), c.Detail(), c.Extensions())
	}
}

func TestNewType_Undeclared(t *testing.T) {
	rec, err := write(t, problem.Type{}.New())
	wantResponse(t, rec, http.StatusInternalServerError, serverDocument)
	if !errors.Is(err, problem.ErrInvalid) || !strings.Contains(err.Error(), "undeclared type") {
		t.Errorf("Write returned %v, want ErrInvalid with \"undeclared type\"", err)
	}
}

func TestMatch_Tree(t *testing.T) {
	occurrence := creditOccurrence()
	plain := errors.New("plain")
	found := []struct {
		name string
		err  error
	}{
		{"itself", occurrence},
		{"wrapped twice", fmt.Errorf("send message: %w", fmt.Errorf("charge message: %w", occurrence))},
		{"joined with a plain error", errors.Join(plain, occurrence)},
		{"after a problem of another type", errors.Join(serviceUnavailable.New(), occurrence)},
		{"as the cause of another problem", serviceUnavailable.New(problem.WithCause(occurrence))},
	}
	for _, tt := range found {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := outOfCredit.Match(tt.err)
			if !ok || got != occurrence {
				t.Errorf("Match = %v, %v; want the occurrence, true", got, ok)
			}
		})
	}
	notFound := []struct {
		name string
		err  error
	}{
		{"nil", nil},
		{"plain error", plain},
		{"another type", serviceUnavailable.New()},
		{"about:blank with the same status", problem.Status(http.StatusForbidden)},
		{"wrapped nil problem", fmt.Errorf("x: %w", (*problem.Error)(nil))},
	}
	for _, tt := range notFound {
		t.Run(tt.name, func(t *testing.T) {
			if got, ok := outOfCredit.Match(tt.err); ok || got != nil {
				t.Errorf("Match = %v, %v; want nil, false", got, ok)
			}
		})
	}
	t.Run("zero Type", func(t *testing.T) {
		if _, ok := (problem.Type{}).Match(occurrence); ok {
			t.Error("the zero Type matched")
		}
	})
}

// TestMatch_ReadResponse applies the type test to problems read from a response
// to a request for apiTarget, and to problems from the other sources of FR-007.
func TestMatch_ReadResponse(t *testing.T) {
	cdnCredit := problem.Must(problem.NewType(
		"//cdn.example.com/probs/out-of-credit", "You do not have enough credit.", http.StatusForbidden))
	parse := func(doc string) *problem.Error {
		p, err := problem.Parse([]byte(doc))
		if err != nil {
			t.Fatalf("Parse: %v", err)
		}
		return p
	}
	// The rows of FR-007 (spec 012). Only the first two changed from problem/v0.2.0.
	tests := []struct {
		name     string
		typ      problem.Type
		p        *problem.Error
		wantType string
		want     bool
	}{
		{
			"relative, same relative type", sharedCredit,
			readProblem(t, apiTarget, creditDoc("/probs/out-of-credit")),
			"https://api.example.com/probs/out-of-credit", true,
		},
		{
			"relative, same absolute type", sharedCredit,
			readProblem(t, apiTarget, creditDoc("https://api.example.com/probs/out-of-credit")),
			"https://api.example.com/probs/out-of-credit", true,
		},
		{
			"relative, other host", sharedCredit,
			readProblem(t, apiTarget, creditDoc("https://other.example.com/probs/out-of-credit")),
			"https://other.example.com/probs/out-of-credit", false,
		},
		{
			"relative, other path", sharedCredit,
			readProblem(t, apiTarget, creditDoc("/probs/other")),
			"https://api.example.com/probs/other", false,
		},
		{
			"relative, read without a request", sharedCredit,
			readProblem(t, "", creditDoc("/probs/out-of-credit")),
			"/probs/out-of-credit", true,
		},
		{
			"relative, parsed relative type", sharedCredit,
			parse(creditDoc("/probs/out-of-credit")),
			"/probs/out-of-credit", true,
		},
		{
			"relative, parsed absolute type", sharedCredit,
			parse(creditDoc("https://api.example.com/probs/out-of-credit")),
			"https://api.example.com/probs/out-of-credit", false,
		},
		{
			"relative, built locally", sharedCredit,
			sharedCredit.New(problem.WithExtension(balance, 30)),
			"/probs/out-of-credit", true,
		},
		{
			"absolute, read relative type", apiCredit,
			readProblem(t, apiTarget, creditDoc("/probs/out-of-credit")),
			"https://api.example.com/probs/out-of-credit", true,
		},
		{
			"absolute, parsed relative type", apiCredit,
			parse(creditDoc("/probs/out-of-credit")),
			"/probs/out-of-credit", false,
		},
		// Spec edge cases.
		{
			"network path", cdnCredit,
			readProblem(t, apiTarget, creditDoc("//cdn.example.com/probs/out-of-credit")),
			"https://cdn.example.com/probs/out-of-credit", true,
		},
		{
			"document without a type", sharedCredit,
			readProblem(t, apiTarget, `{"status":403}`),
			"about:blank", false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.p.Type(); got != tt.wantType {
				t.Errorf("Type() = %q, want %q", got, tt.wantType)
			}
			got, ok := tt.typ.Match(tt.p)
			if ok != tt.want {
				t.Fatalf("Match = %v, want %v (type %q)", ok, tt.want, tt.p.Type())
			}
			if !tt.want {
				if got != nil {
					t.Errorf("Match = %v, want nil", got)
				}
				return
			}
			if got != tt.p {
				t.Errorf("Match returned %v, not the problem", got)
			}
			// Every matching row has the balance of creditDoc.
			if v, ok := balance.Lookup(got); !ok || v != 30 {
				t.Errorf("balance = %d, %v; want 30, true", v, ok)
			}
		})
	}
}

// TestMatch_ReadFromServer writes problems of relative types from a loopback
// server, reads them as a client does, and tests them against the declarations
// that the server wrote them from (SC-002).
func TestMatch_ReadFromServer(t *testing.T) {
	// No two of these resolve alike.
	decls := []struct{ name, uri string }{
		{"full path", "/problems/user-not-registered"},
		{"query", "/probs/out-of-credit?lang=en"},
		{"fragment", "/probs#out-of-credit"},
		{"percent-encoded space", "/probs/out%20of%20credit"},
		{"percent-encoded UTF-8", "/probs/caf%C3%A9"},
		{"percent-encoded slash", "/probs/a%2Fb"},
		{"percent-encoded tilde", "/probs/%7Euser"},
		{"dot segments", "/probs/./x/../out-of-credit"},
		{"network path", "//cdn.example.com/probs/out-of-credit"},
		{"root", "/"},
		{"root with query", "/?type=credit"},
		{"upper case", "/PROBS/Out-Of-Credit"},
	}
	types := make(map[string]problem.Type, len(decls))
	for _, d := range decls {
		types[d.uri] = problem.Must(problem.NewType(d.uri, "You do not have enough credit.", http.StatusForbidden))
	}

	// write answers with an occurrence of the type named by the Test-Type header.
	write := func(w http.ResponseWriter, r *http.Request) {
		if err := problem.Write(w, r, types[r.Header.Get("Test-Type")].New()); err != nil {
			t.Errorf("Write: %v", err)
		}
	}
	b := httptest.NewServer(http.HandlerFunc(write))
	defer b.Close()
	a := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/redirect" {
			http.Redirect(w, r, b.URL+"/after/redirect?x=1", http.StatusTemporaryRedirect)
			return
		}
		write(w, r)
	}))
	defer a.Close()

	// Different paths, a second server on another port, and a redirect from one
	// server to the other.
	targets := []string{
		a.URL + "/api/v1/users/me",
		a.URL + "/",
		a.URL + "/deep/a/b/c/?q=1",
		b.URL + "/other/port",
		a.URL + "/redirect",
		a.URL + "/x;p?q#frag",
	}
	get := func(t *testing.T, target, uri string) *problem.Error {
		t.Helper()
		req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, target, nil)
		if err != nil {
			t.Fatalf("NewRequest: %v", err)
		}
		req.Header.Set("Test-Type", uri) // http.Client forwards it across the redirect
		resp, err := a.Client().Do(req)
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
		return p
	}

	for _, d := range decls {
		t.Run(d.name, func(t *testing.T) {
			for _, target := range targets {
				p := get(t, target, d.uri)
				if got, ok := types[d.uri].Match(p); !ok || got != p {
					t.Errorf("%s via %s: Match = %v, %v for the type %q; want the problem, true",
						d.uri, target, got, ok, p.Type())
				}
				if _, ok := types[d.uri].Match(p.With(problem.WithDetail("derived"))); !ok {
					t.Errorf("%s via %s: no match for the derived problem", d.uri, target)
				}
				for _, o := range decls {
					if _, ok := types[o.uri].Match(p); ok && o.uri != d.uri {
						t.Errorf("%s via %s: %s matched the type %q", d.uri, target, o.uri, p.Type())
					}
				}
			}
		})
	}

	t.Run("redirect", func(t *testing.T) {
		const uri = "/problems/user-not-registered"
		p := get(t, a.URL+"/redirect", uri)
		if want := b.URL + uri; p.Type() != want {
			t.Errorf("Type() = %q, want %q", p.Type(), want)
		}
		first := problem.Must(problem.NewType(a.URL+uri, "t", http.StatusForbidden))
		if _, ok := first.Match(p); ok {
			t.Error("the absolute type on the first server matched")
		}
		last := problem.Must(problem.NewType(b.URL+uri, "t", http.StatusForbidden))
		if _, ok := last.Match(p); !ok {
			t.Error("the absolute type on the last server did not match")
		}
	})
}

// TestMatch_ReadTree finds a problem read from a response anywhere in an error
// tree, comparing each problem with the URL it was read from.
func TestMatch_ReadTree(t *testing.T) {
	p := readProblem(t, apiTarget, creditDoc("/probs/out-of-credit"))
	found := []struct {
		name string
		err  error
	}{
		{"itself", p},
		{"wrapped twice", fmt.Errorf("send message: %w", fmt.Errorf("charge message: %w", p))},
		{"joined with a plain error", errors.Join(errors.New("plain"), p)},
		{"after a problem of another type", errors.Join(serviceUnavailable.New(), p)},
		{"as the cause of another problem", serviceUnavailable.New(problem.WithCause(p))},
	}
	for _, tt := range found {
		t.Run(tt.name, func(t *testing.T) {
			if got, ok := sharedCredit.Match(tt.err); !ok || got != p {
				t.Errorf("Match = %v, %v; want the problem, true", got, ok)
			}
		})
	}

	t.Run("two services", func(t *testing.T) {
		pa := readProblem(t, "https://a.example.com/v1", creditDoc("/probs/out-of-credit"))
		pb := readProblem(t, "https://b.example.com/v1", creditDoc("/probs/out-of-credit"))
		pOther := readProblem(t, "https://a.example.com/v1", creditDoc("/probs/other"))
		bCredit := problem.Must(problem.NewType(
			"https://b.example.com/probs/out-of-credit", "You do not have enough credit.", http.StatusForbidden))
		tests := []struct {
			name string
			typ  problem.Type
			err  error
			want *problem.Error
		}{
			{"first match wins", sharedCredit, errors.Join(serviceUnavailable.New(), pa, pb), pa},
			{"past a problem of another type", sharedCredit, errors.Join(pOther, pb), pb},
			{"each with its own URL", bCredit, errors.Join(pa, pb), pb},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				if got, ok := tt.typ.Match(tt.err); !ok || got != tt.want {
					t.Errorf("Match = %v (type %q), %v; want the problem of type %q, true",
						got, typeOf(got), ok, tt.want.Type())
				}
			})
		}
	})

	t.Run("not found", func(t *testing.T) {
		if got, ok := (problem.Type{}).Match(p); ok || got != nil {
			t.Errorf("zero Type: Match = %v, %v; want nil, false", got, ok)
		}
		if got, ok := sharedCredit.Match(nil); ok || got != nil {
			t.Errorf("nil error: Match = %v, %v; want nil, false", got, ok)
		}
	})
}

// TestMatch_Allocs checks that the comparisons that spec 012 does not change
// allocate nothing (SC-003). A relative declaration against a problem read from
// a response is deliberately absent: that path resolves, and allocates.
func TestMatch_Allocs(t *testing.T) {
	parsed, err := problem.Parse([]byte(creditDoc("/probs/out-of-credit")))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	tests := []struct {
		name string
		typ  problem.Type
		err  error
		want bool
	}{
		{"relative, built locally", sharedCredit, sharedCredit.New(), true},
		{"relative, parsed", sharedCredit, parsed, true},
		{
			"relative, read without a request", sharedCredit,
			readProblem(t, "", creditDoc("/probs/out-of-credit")), true,
		},
		{
			"relative, tree", sharedCredit,
			fmt.Errorf("send: %w", errors.Join(errors.New("plain"), serviceUnavailable.New(), sharedCredit.New())), true,
		},
		{
			"absolute, read from a response", apiCredit,
			readProblem(t, apiTarget, creditDoc("/probs/out-of-credit")), true,
		},
		{
			"absolute, read from a response, other type", apiCredit,
			readProblem(t, apiTarget, creditDoc("/probs/other")), false,
		},
		{"absolute, built locally from the relative declaration", apiCredit, sharedCredit.New(), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, ok := tt.typ.Match(tt.err); ok != tt.want {
				t.Fatalf("Match = %v, want %v", ok, tt.want)
			}
			if n := testing.AllocsPerRun(100, func() { _, _ = tt.typ.Match(tt.err) }); n != 0 {
				t.Errorf("Match allocates %v times, want 0", n)
			}
		})
	}
}

// typeOf returns the type URI of p, or "" when p is nil.
func typeOf(p *problem.Error) string {
	if p == nil {
		return ""
	}
	return p.Type()
}

func TestMust_Basic(t *testing.T) {
	t.Run("returns the value", func(t *testing.T) {
		if got := problem.Must(7, nil); got != 7 {
			t.Errorf("Must(7, nil) = %d", got)
		}
	})
	t.Run("panics with the error", func(t *testing.T) {
		errX := errors.New("x")
		defer func() {
			v := recover()
			err, ok := v.(error)
			if !ok || !errors.Is(err, errX) {
				t.Errorf("recovered %v, want an error matching errX", v)
			}
		}()
		problem.Must(0, errX)
		t.Error("Must did not panic")
	})
}

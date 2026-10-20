package session_test

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"

	"github.com/paccolamano/go/auth/internal/storetest"
	"github.com/paccolamano/go/auth/memstore"
)

func FuzzSessionCookie(f *testing.F) {
	rec := &storetest.Recorder{Next: memstore.New()}
	m := mustManager(f, rec)
	issued := issue(f, m, alice()).Value
	f.Add(issued)
	f.Add("")
	f.Add("short")
	f.Add(issued[:42] + "=")
	f.Add("\x00\xff")
	shape := regexp.MustCompile(`^[A-Za-z0-9_-]{43}$`)
	f.Fuzz(func(t *testing.T, v string) {
		r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil)
		r.Header.Set("Cookie", sessionCookie+"="+v)
		before := len(rec.Calls())
		p, err := m.Authenticate(r)
		if err == nil && (p == nil || v != issued) {
			t.Fatalf("value %q authenticated", v)
		}
		// The shape is checked on the value the cookie parser yields, which
		// trims surrounding spaces.
		parsed := ""
		if c, err := r.Cookie(sessionCookie); err == nil {
			parsed = c.Value
		}
		if !shape.MatchString(parsed) && len(rec.Calls()) != before {
			t.Fatalf("value %q (parsed %q) reached the store", v, parsed)
		}
	})
}

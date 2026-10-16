package requestid_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/paccolamano/go/middleware/requestid"
)

func FuzzRequestID(f *testing.F) {
	for _, s := range []string{"abc", "", "a b", "\r\n", "0f8fad5b-d9cb-469f-a165-70867728950e"} {
		f.Add(s)
	}
	mw, err := requestid.New()
	if err != nil {
		f.Fatal(err)
	}
	f.Fuzz(func(t *testing.T, incoming string) {
		var seen string
		h := mw(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
			seen, _ = requestid.FromContext(r.Context())
		}))
		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil)
		req.Header["X-Request-Id"] = []string{incoming}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		got := rec.Header().Get("X-Request-ID")
		if !valid(got) || seen != got {
			t.Fatalf("incoming %q produced id %q (handler saw %q)", incoming, got, seen)
		}
	})
}

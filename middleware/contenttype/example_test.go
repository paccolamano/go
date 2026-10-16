package contenttype_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"

	"github.com/paccolamano/go/middleware/contenttype"
)

func ExampleNew() {
	mw, err := contenttype.New([]string{"application/json"})
	if err != nil {
		panic(err)
	}
	h := mw(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	for _, ct := range []string{"application/json; charset=utf-8", "text/plain"} {
		req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/", strings.NewReader("{}"))
		req.Header.Set("Content-Type", ct)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		fmt.Println(ct, "→", rec.Code)
	}
	// Output:
	// application/json; charset=utf-8 → 200
	// text/plain → 415
}

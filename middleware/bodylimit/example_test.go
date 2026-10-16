package bodylimit_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"

	"github.com/paccolamano/go/middleware/bodylimit"
)

func ExampleNew() {
	mw, err := bodylimit.New(1 << 20) // 1 MiB
	if err != nil {
		panic(err)
	}
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := io.ReadAll(r.Body); err != nil {
			// A body of undeclared size went past the limit.
			if mbe := new(http.MaxBytesError); errors.As(err, &mbe) {
				http.Error(w, "body too large", http.StatusRequestEntityTooLarge)
				return
			}
			http.Error(w, "bad request", http.StatusBadRequest)
		}
	}))

	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/upload",
		strings.NewReader(strings.Repeat("x", 2<<20)))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	fmt.Println(rec.Code) // refused before reading: the size was declared
	// Output: 413
}

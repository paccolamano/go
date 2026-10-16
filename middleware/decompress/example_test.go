package decompress_test

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"

	"github.com/paccolamano/go/middleware/decompress"
)

func ExampleNew() {
	mw, err := decompress.New(decompress.WithMaxSize(1 << 20))
	if err != nil {
		panic(err)
	}
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		var mbe *http.MaxBytesError
		switch {
		case errors.As(err, &mbe):
			http.Error(w, "body too large", http.StatusRequestEntityTooLarge)
			return
		case errors.Is(err, decompress.ErrInvalidBody):
			http.Error(w, "invalid compressed body", http.StatusBadRequest)
			return
		case err != nil:
			http.Error(w, "read error", http.StatusBadRequest)
			return
		}
		fmt.Printf("handler received %q\n", body)
	}))

	var compressed bytes.Buffer
	zw := gzip.NewWriter(&compressed)
	_, _ = io.WriteString(zw, `{"hello":"world"}`)
	_ = zw.Close()
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/", &compressed)
	req.Header.Set("Content-Encoding", "gzip")
	h.ServeHTTP(httptest.NewRecorder(), req)
	// Output: handler received "{\"hello\":\"world\"}"
}
